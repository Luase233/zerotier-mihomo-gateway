#requires -Version 5.1
[CmdletBinding()]
param([switch]$AsJson)

# Read-only. No driver loading, route/firewall changes, or configuration-file reads.
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version 2

function Read-Section {
    param([scriptblock]$Query)
    try { [pscustomobject]@{ Ok = $true; Data = @(& $Query); Error = $null } }
    catch { [pscustomobject]@{ Ok = $false; Data = @(); Error = $_.Exception.Message } }
}

$identity = [Security.Principal.WindowsIdentity]::GetCurrent()
$principal = New-Object Security.Principal.WindowsPrincipal($identity)
$snapshot = [ordered]@{
    CapturedUtc = [DateTime]::UtcNow.ToString('o')
    Elevated = $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)
    Scope = 'Read-only IPv4 gateway diagnostics; no proxy profiles, credentials, command lines, or physical MAC addresses.'
    Expected = [ordered]@{ Gateway = '10.147.20.1'; Client = '10.147.20.2'; Socks = '127.0.0.1:7897'; NatName = 'ZeroTierNAT'; NatPrefix = '10.147.20.0/24' }
    Adapters = Read-Section { Get-NetAdapter -IncludeHidden | Select-Object ifIndex, Name, InterfaceDescription, Status, LinkSpeed }
    IPv4Addresses = Read-Section { Get-NetIPAddress -AddressFamily IPv4 | Select-Object InterfaceIndex, InterfaceAlias, IPAddress, PrefixLength, AddressState }
    IPv4Forwarding = Read-Section { Get-NetIPInterface -AddressFamily IPv4 | Select-Object InterfaceIndex, InterfaceAlias, ConnectionState, Forwarding, InterfaceMetric, NlMtu, WeakHostSend, WeakHostReceive }
    IPv4Routes = Read-Section { Get-NetRoute -AddressFamily IPv4 | Select-Object InterfaceIndex, InterfaceAlias, DestinationPrefix, NextHop, RouteMetric, Protocol, PolicyStore }
    IPv4DnsServers = Read-Section { Get-DnsClientServerAddress -AddressFamily IPv4 | Select-Object InterfaceIndex, InterfaceAlias, ServerAddresses }
    IpEnableRouter = Read-Section { Get-ItemPropertyValue -LiteralPath 'HKLM:\SYSTEM\CurrentControlSet\Services\Tcpip\Parameters' -Name IPEnableRouter }
    Nat = Read-Section { Get-NetNat | Select-Object Name, InternalIPInterfaceAddressPrefix, ExternalIPInterfaceAddressPrefix, InternalRoutingDomainId, Active }
    Tcp7897 = Read-Section { Get-NetTCPConnection -State Listen | Where-Object { $_.LocalPort -eq 7897 } | Select-Object LocalAddress, LocalPort, OwningProcess }
    Udp7897 = Read-Section { Get-NetUDPEndpoint | Where-Object { $_.LocalPort -eq 7897 } | Select-Object LocalAddress, LocalPort, OwningProcess }
    RelevantProcesses = Read-Section { Get-Process | Where-Object { $_.ProcessName -match 'mihomo|clash|zerotier|radmin|zt-socks|gateway' } | Select-Object Id, ProcessName }
}

if ($AsJson) { $snapshot | ConvertTo-Json -Depth 8 }
else {
    foreach ($entry in $snapshot.GetEnumerator()) {
        Write-Output ("`n[{0}]" -f $entry.Key)
        if ($entry.Value -is [System.Collections.IDictionary]) { $entry.Value | Format-Table -AutoSize | Out-String -Width 220 | Write-Output }
        elseif ($entry.Value -is [pscustomobject] -and $null -ne $entry.Value.PSObject.Properties['Ok']) {
            if ($entry.Value.Ok) { $entry.Value.Data | Format-Table -AutoSize | Out-String -Width 220 | Write-Output }
            else { Write-Output ('UNAVAILABLE: ' + $entry.Value.Error) }
        }
        else { Write-Output $entry.Value }
    }
}
