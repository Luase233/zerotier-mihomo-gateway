#requires -Version 5.1
[CmdletBinding()]
param([switch]$PhoneDefaultRouteIsOff)
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'Common.ps1')
if (-not $PhoneDefaultRouteIsOff) { throw 'Turn iPhone Enable Default Route OFF, then pass -PhoneDefaultRouteIsOff. Restoring NAT while it is ON could permit direct egress.' }
Assert-Elevated
$mutex = Enter-GatewayLock
try {
    $manifest = Read-GatewayManifest
    if ($null -ne $manifest) {
        # Validate both identities and foreign children before terminating either one.
        $null = Get-OwnedProcess $manifest.Proxy
        $null = Get-OwnedProcess $manifest.Guard
        $recorded = @()
        if ($null -ne $manifest.Proxy) { $recorded += [int]$manifest.Proxy.Pid }
        if ($null -ne $manifest.Guard) { $recorded += [int]$manifest.Guard.Pid }
        Assert-NoUnrecordedGateway -RecordedPids $recorded
        Stop-OwnedProcess $manifest.Proxy
        Stop-OwnedProcess $manifest.Guard
        $manifest.Status='stopped-before-nat-restore'
        Write-GatewayJson $gatewayManifest $manifest
    }
    else { Assert-NoUnrecordedGateway }
    $originalBackup = Join-Path $gatewayState 'ZeroTierNAT.original.clixml'
    if (Test-Path -LiteralPath $originalBackup -PathType Leaf) { & (Join-Path $PSScriptRoot 'Nat.ps1') -Action Restore -PhoneDefaultRouteIsOff }
    elseif ($null -ne $manifest) { throw 'Original NAT backup is missing. Gateway processes stopped, but NAT was not recreated from assumptions.' }
    else { Write-Output 'No recorded gateway run or NAT backup exists; nothing to stop or restore.'; return }
    if ($null -ne $manifest) {
        $manifest.Status='stopped-and-nat-restored'
        $manifest | Add-Member -NotePropertyName StoppedUtc -NotePropertyValue ([DateTime]::UtcNow.ToString('o')) -Force
        Write-GatewayJson (Join-Path $gatewayState ('completed-' + $manifest.RunId + '.json')) $manifest
        Remove-Item -LiteralPath $gatewayManifest -Force
    }
    Write-Output 'Owned proxy stopped, then guard stopped, and original NAT configuration verified. Keep the iPhone default route OFF until another validated run.'
    Write-Output 'Default routes, forwarding, firewall rules, adapters, and Clash settings were not changed.'
}
finally { $mutex.ReleaseMutex(); $mutex.Dispose() }
