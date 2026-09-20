#requires -Version 5.1
#requires -RunAsAdministrator
[CmdletBinding()]
param(
    [string]$GatewayRoot = 'C:\ZeroTierGateway',
    [int]$Port = 8787,
    [string]$Pipe = '\\.\pipe\verge-mihomo',
    [string]$UserId = [Security.Principal.WindowsIdentity]::GetCurrent().Name
)
$ErrorActionPreference = 'Stop'
$repo = Split-Path $PSScriptRoot -Parent
$GatewayRoot = [IO.Path]::GetFullPath($GatewayRoot).TrimEnd('\')
if ($GatewayRoot.Contains('"') -or $GatewayRoot.Contains("`n") -or $GatewayRoot.Contains("`r")) { throw 'Invalid root path.' }
if ($Port -lt 1024 -or $Port -gt 65535) { throw 'Use a port between 1024 and 65535.' }
$gateway = Get-Content -Raw -LiteralPath (Join-Path $GatewayRoot 'gateway.json') | ConvertFrom-Json
$address = Get-NetIPAddress -AddressFamily IPv4 -InterfaceIndex $gateway.interface_index | Where-Object IPAddress -eq $gateway.windows_ip
if (-not $address) { throw 'The configured ZeroTier address is not assigned to the configured interface.' }
foreach($ip in @($gateway.windows_ip,$gateway.source_ip)) {
    $parsed = [Net.IPAddress]::Parse($ip)
    if($parsed.AddressFamily -ne [Net.Sockets.AddressFamily]::InterNetwork) { throw 'IPv4 addresses are required.' }
}
$state = Join-Path $GatewayRoot 'state\mobile'
New-Item -ItemType Directory -Force -Path $state | Out-Null
$sid = (New-Object Security.Principal.NTAccount($UserId)).Translate([Security.Principal.SecurityIdentifier]).Value
& icacls.exe $state /grant "*$($sid):(OI)(CI)M" | Out-Null
if($LASTEXITCODE -ne 0){throw 'Cannot grant the desktop user access to mobile state.'}
$configPath = Join-Path $GatewayRoot 'mobile.json'
if (Test-Path -LiteralPath $configPath) {
    $existing = Get-Content -Raw -LiteralPath $configPath | ConvertFrom-Json
    $token = [string]$existing.token
} else {
    $bytes = New-Object byte[] 32
    $rng = [Security.Cryptography.RandomNumberGenerator]::Create()
    try { $rng.GetBytes($bytes) } finally { $rng.Dispose() }
    $token = [Convert]::ToBase64String($bytes).TrimEnd('=').Replace('+','-').Replace('/','_')
}
$config = [ordered]@{listen=($gateway.windows_ip+':'+$Port);allowed_clients=@($gateway.source_ip,$gateway.windows_ip);token=$token;pipe=$Pipe;state_file=(Join-Path $state 'status.json')}
[IO.File]::WriteAllText($configPath,($config|ConvertTo-Json -Depth 4),(New-Object Text.UTF8Encoding($false)))
$source = Join-Path $repo 'bin\zerobridge-mobile.exe'
$target = Join-Path $GatewayRoot 'bin\zerobridge-mobile.exe'
if ([IO.Path]::GetFullPath($source) -ne $target) { Copy-Item -LiteralPath $source -Destination $target -Force }
$runner = Join-Path $GatewayRoot 'scripts\Run-Mobile.ps1'
if ([IO.Path]::GetFullPath((Join-Path $PSScriptRoot 'Run-Mobile.ps1')) -ne $runner) {Copy-Item -LiteralPath (Join-Path $PSScriptRoot 'Run-Mobile.ps1') -Destination $runner -Force}
& $target --config $configPath --check
if ($LASTEXITCODE -ne 0) { throw 'Clash named-pipe controller check failed.' }
$ruleName = 'ZeroBridgeMihomoMobile'
Get-NetFirewallRule -Name $ruleName -ErrorAction SilentlyContinue | Remove-NetFirewallRule
$interfaceAlias = [System.Management.Automation.WildcardPattern]::Escape([string]$address.InterfaceAlias)
New-NetFirewallRule -Name $ruleName -DisplayName 'ZeroBridge Mobile - authorized ZeroTier phone only' -Direction Inbound -Action Allow -Protocol TCP -LocalAddress $gateway.windows_ip -LocalPort $Port -RemoteAddress $gateway.source_ip -InterfaceAlias $interfaceAlias -Program $target -Profile Any | Out-Null
$ps = Join-Path $env:SystemRoot 'System32\WindowsPowerShell\v1.0\powershell.exe'
$action = New-ScheduledTaskAction -Execute $ps -Argument "-NoProfile -NonInteractive -WindowStyle Hidden -ExecutionPolicy Bypass -File `"$runner`"" -WorkingDirectory $GatewayRoot
$trigger = New-ScheduledTaskTrigger -AtLogOn -User $UserId
$principal = New-ScheduledTaskPrincipal -UserId $UserId -LogonType Interactive -RunLevel Limited
$settings = New-ScheduledTaskSettingsSet -MultipleInstances IgnoreNew -ExecutionTimeLimit ([TimeSpan]::Zero) -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries -RestartCount 999 -RestartInterval (New-TimeSpan -Minutes 1)
Register-ScheduledTask -TaskName 'ZeroBridgeMihomoMobile' -Action $action -Trigger $trigger -Principal $principal -Settings $settings -Description 'Private ZeroTier mobile proxy-selector control and desktop command synchronization.' -Force | Out-Null
Start-ScheduledTask -TaskName 'ZeroBridgeMihomoMobile'
Write-Output ('Mobile control: http://'+$config.listen+'/')
Write-Output 'Pair using the QR code in the desktop Mobile control window. Keep it private.'
