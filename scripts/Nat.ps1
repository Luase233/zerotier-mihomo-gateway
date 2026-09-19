#requires -Version 5.1
[CmdletBinding(SupportsShouldProcess = $true, ConfirmImpact = 'Medium')]
param(
    [Parameter(Mandatory = $true)]
    [ValidateSet('Backup', 'Remove', 'Restore')]
    [string]$Action,
    # Required on every mutation; this is an explicit operator acknowledgement,
    # not a claim that the script can inspect the iPhone's toggle remotely.
    [switch]$PhoneDefaultRouteIsOff,
    # Backup-only: requires operator confirmation that no external pools were
    # manually configured. Port-block shape alone never enables this policy.
    [switch]$ExternalPoolsAreAutomatic
)

$ErrorActionPreference = 'Stop'
if (Test-Path -LiteralPath (Join-Path $PSScriptRoot '..\DEPLOYED.json')) { throw 'This workspace copy was migrated. Use C:\ZeroTierGateway\scripts\Manage.ps1; do not restore NAT from the archived instance.' }
Set-StrictMode -Version 2
$expectedName = 'ZeroTierNAT'
$expectedPrefix = '10.147.20.0/24'
$stateDir = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..\state'))
$backupPath = Join-Path $stateDir 'ZeroTierNAT.original.clixml'
$createKeys = @('Name', 'InternalIPInterfaceAddressPrefix', 'ExternalIPInterfaceAddressPrefix', 'InternalRoutingDomainId')
$settingKeys = @('IcmpQueryTimeout', 'TcpEstablishedConnectionTimeout', 'TcpTransientConnectionTimeout', 'TcpFilteringBehavior', 'UdpFilteringBehavior', 'UdpIdleSessionTimeout', 'UdpInboundRefresh')
if ($ExternalPoolsAreAutomatic -and $Action -ne 'Backup') { throw '-ExternalPoolsAreAutomatic is a Backup-only acknowledgement; Remove/Restore use the immutable saved policy.' }

function Require-Administrator {
    $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = New-Object Security.Principal.WindowsPrincipal($identity)
    if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) { throw 'This operation requires an elevated PowerShell window. No system changes were made.' }
}

function Get-MachineFingerprint {
    $machineGuid = [string](Get-ItemPropertyValue -LiteralPath 'HKLM:\SOFTWARE\Microsoft\Cryptography' -Name MachineGuid)
    $sha = [Security.Cryptography.SHA256]::Create()
    try { return ([BitConverter]::ToString($sha.ComputeHash([Text.Encoding]::UTF8.GetBytes($machineGuid)))).Replace('-', '') }
    finally { $sha.Dispose() }
}

function Get-ExactNat {
    $matches = @(Get-NetNat | Where-Object { $_.Name -ceq $expectedName })
    if ($matches.Count -gt 1) { throw 'More than one exact-name NAT exists; refusing to proceed.' }
    if ($matches.Count -eq 0) { return $null }
    if ([string]$matches[0].InternalIPInterfaceAddressPrefix -cne $expectedPrefix) { throw 'ZeroTierNAT has an unexpected prefix; refusing to change it.' }
    return $matches[0]
}

function Get-Arguments {
    param($Nat, [string[]]$Keys, [string]$CommandName)
    $command = Get-Command $CommandName -ErrorAction Stop
    $arguments = @{}
    foreach ($key in $Keys) {
        $property = $Nat.PSObject.Properties[$key]
        if ($null -eq $property -or $null -eq $property.Value -or [string]$property.Value -eq '') { continue }
        if (-not $command.Parameters.ContainsKey($key)) { throw "$CommandName cannot preserve existing property $key on this Windows build." }
        $value = $property.Value
        if ($value -is [Enum]) { $value = $value.ToString() }
        $arguments[$key] = $value
    }
    return $arguments
}

function Assert-SameArguments {
    param([System.Collections.IDictionary]$Actual, [System.Collections.IDictionary]$Expected, [string]$Label)
    if ($Actual.Count -ne $Expected.Count) { throw "$Label differs from the original backup; refusing to proceed." }
    foreach ($key in $Expected.Keys) {
        if (-not $Actual.Contains($key) -or [string]$Actual[$key] -cne [string]$Expected[$key]) { throw "$Label property $key differs from the original backup; refusing to proceed." }
    }
}

function Get-ExternalPoolPolicy {
    param($Saved)
    if ($Saved.SchemaVersion -eq 1) { return 'RejectExternalPools' }
    if ($Saved.ExternalAddressPolicy -cnotin @('RejectExternalPools','AutomaticAcknowledged')) { throw 'Unrecognized external-address policy in backup.' }
    if ($Saved.ExternalAddressPolicy -ceq 'AutomaticAcknowledged' -and $Saved.ExternalPoolAcknowledgement.OperatorConfirmedNoManualExternalPools -ne $true) { throw 'Automatic pool handling lacks an operator acknowledgement.' }
    return [string]$Saved.ExternalAddressPolicy
}

function Assert-ExternalPoolRows {
    param([object[]]$Addresses, [string]$Policy, [string[]]$LocalIPs, [string]$ExternalPrefix)
    if ($Policy -ceq 'RejectExternalPools') {
        if ($Addresses.Count -gt 0) { throw 'External address pools exist. They cannot be classified automatically: a confirmed no-manual-pools history and -ExternalPoolsAreAutomatic on Backup are required.' }
        return
    }
    if (-not [string]::IsNullOrWhiteSpace($ExternalPrefix)) { throw 'Automatic pool policy requires the original unrestricted external prefix; this NAT has an explicit external prefix.' }
    foreach ($address in $Addresses) {
        $ip = $null
        if ($address.NatName -cne $expectedName -or -not [Net.IPAddress]::TryParse([string]$address.IPAddress, [ref]$ip) -or $ip.AddressFamily -ne [Net.Sockets.AddressFamily]::InterNetwork -or $LocalIPs -cnotcontains [string]$address.IPAddress) { throw 'An external pool does not match a local IPv4 address; refusing to infer it is an automatic allocation.' }
        $low = [uint32]$address.PortStart
        $high = [uint32]$address.PortEnd
        if ($low -lt 1 -or $high -gt 65535 -or $low -gt $high) { throw 'An external pool has an invalid port range.' }
    }
}

function Assert-NoDependentObjects {
    param([string]$Policy = 'RejectExternalPools', $Nat)
    $maps = @(Get-NetNatStaticMapping | Where-Object { $_.NatName -ceq $expectedName })
    $addresses = @(Get-NetNatExternalAddress | Where-Object { $_.NatName -ceq $expectedName })
    if ($maps.Count -gt 0) { throw 'This NAT has static mappings. The bounded rollback script does not manage those resources; refusing to remove it.' }
    $localIPs = @()
    if ($Policy -ceq 'AutomaticAcknowledged') { $localIPs = @(Get-NetIPAddress -AddressFamily IPv4 | Select-Object -ExpandProperty IPAddress) }
    $externalPrefix = ''
    if ($null -ne $Nat) { $externalPrefix = [string]$Nat.ExternalIPInterfaceAddressPrefix }
    Assert-ExternalPoolRows -Addresses $addresses -Policy $Policy -LocalIPs $localIPs -ExternalPrefix $externalPrefix
}

function Read-OriginalBackup {
    if (-not (Test-Path -LiteralPath $backupPath -PathType Leaf)) { throw "Original backup is missing. Run -Action Backup first: $backupPath" }
    $saved = Import-Clixml -LiteralPath $backupPath
    if ($saved.SchemaVersion -notin @(1,2) -or $saved.Task -cne 'zt-socks-gateway') { throw 'Unrecognized backup format.' }
    if ($saved.MachineFingerprint -cne (Get-MachineFingerprint)) { throw 'Backup belongs to a different Windows installation.' }
    if ($saved.CreateArguments.Name -cne $expectedName -or $saved.CreateArguments.InternalIPInterfaceAddressPrefix -cne $expectedPrefix) { throw 'Backup does not identify the exact permitted NAT.' }
    foreach ($key in $saved.CreateArguments.Keys) { if ($createKeys -cnotcontains $key) { throw "Unexpected creation argument in backup: $key" } }
    foreach ($key in $saved.SetArguments.Keys) { if ($settingKeys -cnotcontains $key) { throw "Unexpected setting in backup: $key" } }
    if (@($saved.StaticMappings).Count -gt 0) { throw 'Backup includes static NAT mappings requiring a separate restore plan.' }
    $policy = Get-ExternalPoolPolicy $saved
    $localIPs = @()
    if ($saved.SchemaVersion -eq 2) { $localIPs = @($saved.LocalIPv4AddressSnapshot) }
    Assert-ExternalPoolRows -Addresses @($saved.ExternalAddresses) -Policy $policy -LocalIPs $localIPs -ExternalPrefix ([string]$saved.CreateArguments['ExternalIPInterfaceAddressPrefix'])
    return $saved
}

if ($Action -eq 'Backup') {
    if (Test-Path -LiteralPath $backupPath) {
        $saved = Read-OriginalBackup
        if ($ExternalPoolsAreAutomatic -and (Get-ExternalPoolPolicy $saved) -cne 'AutomaticAcknowledged') { throw 'The immutable original backup uses a different external-pool policy; it will not be rewritten.' }
        Write-Output "Original backup already exists and was validated; it was not overwritten: $backupPath"
        return
    }
    $nat = Get-ExactNat
    if ($null -eq $nat) { throw 'The exact original NAT is absent. Refusing to invent an original state or overwrite a prior backup.' }
    $policy = if ($ExternalPoolsAreAutomatic) { 'AutomaticAcknowledged' } else { 'RejectExternalPools' }
    Assert-NoDependentObjects -Policy $policy -Nat $nat
    $localIPs = @()
    if ($ExternalPoolsAreAutomatic) { $localIPs = @(Get-NetIPAddress -AddressFamily IPv4 | Select-Object -ExpandProperty IPAddress) }
    $saved = [pscustomobject]@{
        SchemaVersion = 2
        Task = 'zt-socks-gateway'
        CreatedUtc = [DateTime]::UtcNow.ToString('o')
        MachineFingerprint = Get-MachineFingerprint
        CreateArguments = Get-Arguments $nat $createKeys 'New-NetNat'
        SetArguments = Get-Arguments $nat $settingKeys 'Set-NetNat'
        OriginalNat = $nat
        StaticMappings = @(Get-NetNatStaticMapping | Where-Object { $_.NatName -ceq $expectedName })
        ExternalAddresses = @(Get-NetNatExternalAddress | Where-Object { $_.NatName -ceq $expectedName })
        ExternalAddressPolicy = $policy
        ExternalPoolAcknowledgement = [pscustomobject]@{ OperatorConfirmedNoManualExternalPools=[bool]$ExternalPoolsAreAutomatic; RecordedUtc=[DateTime]::UtcNow.ToString('o'); Basis='Explicit operator acknowledgement via -ExternalPoolsAreAutomatic; never inferred from port-block shape'; RestoreSemantics='Recreate original New-NetNat/Set-NetNat configuration; let Windows reallocate automatic address/port pools. Saved external rows are an informational snapshot, not Add-NetNatExternalAddress commands.' }
        LocalIPv4AddressSnapshot = $localIPs
        AllNatSummary = @(Get-NetNat | Select-Object Name, InternalIPInterfaceAddressPrefix, ExternalIPInterfaceAddressPrefix, InternalRoutingDomainId)
        IPv4Routes = @(Get-NetRoute -AddressFamily IPv4)
        IPv4Interfaces = @(Get-NetIPInterface -AddressFamily IPv4)
    }
    if ($PSCmdlet.ShouldProcess($backupPath, 'Create original local rollback snapshot; no system networking changes')) {
        [IO.Directory]::CreateDirectory($stateDir) | Out-Null
        $temporary = Join-Path $stateDir ([Guid]::NewGuid().ToString('N') + '.tmp.clixml')
        try {
            $saved | Export-Clixml -LiteralPath $temporary -Depth 8
            # Two-argument File.Move fails if destination exists. It never overwrites.
            [IO.File]::Move($temporary, $backupPath)
        }
        finally { if (Test-Path -LiteralPath $temporary -PathType Leaf) { Remove-Item -LiteralPath $temporary -Force } }
        Write-Output "Saved original NAT settings and read-only route/interface snapshots: $backupPath"
    }
    return
}

if (-not $PhoneDefaultRouteIsOff) { throw 'Confirm the iPhone Enable Default Route switch is OFF, then pass -PhoneDefaultRouteIsOff. This script cannot inspect that switch.' }
Require-Administrator
$saved = Read-OriginalBackup
$policy = Get-ExternalPoolPolicy $saved
$nat = Get-ExactNat

if ($Action -eq 'Remove') {
    if ($null -eq $nat) { Write-Output 'ZeroTierNAT is already absent; no changes made.'; return }
    Assert-SameArguments (Get-Arguments $nat $createKeys 'New-NetNat') $saved.CreateArguments 'NAT creation parameters'
    Assert-SameArguments (Get-Arguments $nat $settingKeys 'Set-NetNat') $saved.SetArguments 'NAT settings'
    Assert-NoDependentObjects -Policy $policy -Nat $nat
    if ($PSCmdlet.ShouldProcess("$expectedName ($expectedPrefix)", 'Remove this exact backed-up NAT only')) {
        Remove-NetNat -InputObject $nat -Confirm:$false -ErrorAction Stop
        if ($null -ne (Get-ExactNat)) { throw 'NAT removal did not complete.' }
        Write-Output 'Removed only the backed-up ZeroTierNAT. No routes, forwarding settings, firewall rules, adapters, or other NATs were changed.'
    }
    return
}

if ($null -ne $nat) {
    Assert-SameArguments (Get-Arguments $nat $createKeys 'New-NetNat') $saved.CreateArguments 'Existing NAT creation parameters'
    Assert-SameArguments (Get-Arguments $nat $settingKeys 'Set-NetNat') $saved.SetArguments 'Existing NAT settings'
    Assert-NoDependentObjects -Policy $policy -Nat $nat
    Write-Output 'The exact original NAT and settings are already present; no duplicate was created.'
    return
}

$otherNats = @(Get-NetNat)
foreach ($other in $otherNats) {
    # Other NATs are never removed, including WSL/Hyper-V NATs. Refuse a new conflict.
    if ([string]$other.InternalIPInterfaceAddressPrefix -ceq $expectedPrefix) { throw "Another NAT ($($other.Name)) already uses the ZeroTier prefix; refusing to alter it or create a duplicate." }
    $originalOther = @($saved.AllNatSummary | Where-Object { $_.Name -ceq $other.Name -and $_.InternalIPInterfaceAddressPrefix -ceq $other.InternalIPInterfaceAddressPrefix })
    if ($originalOther.Count -eq 0) { throw "Another NAT ($($other.Name)) appeared or changed after backup. Review that state before restoring; no NAT was changed." }
}

if ($PSCmdlet.ShouldProcess("$expectedName ($expectedPrefix)", 'Recreate original NAT and restore timeout/filtering settings')) {
    $create = @{}
    foreach ($key in $saved.CreateArguments.Keys) { $create[$key] = $saved.CreateArguments[$key] }
    $settings = @{}
    foreach ($key in $saved.SetArguments.Keys) { $settings[$key] = $saved.SetArguments[$key] }
    $created = New-NetNat @create -ErrorAction Stop
    try {
        if ($settings.Count -gt 0) { Set-NetNat -Name $expectedName @settings -Confirm:$false -ErrorAction Stop }
        $restored = Get-ExactNat
        Assert-SameArguments (Get-Arguments $restored $createKeys 'New-NetNat') $saved.CreateArguments 'Restored NAT creation parameters'
        Assert-SameArguments (Get-Arguments $restored $settingKeys 'Set-NetNat') $saved.SetArguments 'Restored NAT settings'
    }
    catch {
        # Only undo the NAT created by this invocation if restore is incomplete.
        Remove-NetNat -InputObject $created -Confirm:$false -ErrorAction Stop
        throw "NAT settings could not be restored. The incomplete NAT created by this invocation was removed; the original backup is retained. $($_.Exception.Message)"
    }
    Write-Output 'Restored original ZeroTierNAT configuration. Active connection/session state cannot be restored; reconnect clients when appropriate.'
    if ($policy -ceq 'AutomaticAcknowledged') { Write-Output 'Windows manages the automatic external address/port allocations. Their IDs and port ranges may differ from the informational original snapshot; no external pools were manually added.' }
}
