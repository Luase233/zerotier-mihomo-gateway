#requires -Version 5.1
#requires -RunAsAdministrator
[CmdletBinding()]
param(
    [string]$GatewayRoot = 'C:\ZeroTierGateway',
    [string]$UserId = [Security.Principal.WindowsIdentity]::GetCurrent().Name
)
$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
$GatewayRoot = [IO.Path]::GetFullPath($GatewayRoot).TrimEnd('\')
if ($GatewayRoot.Contains('"') -or $GatewayRoot.Contains("`n") -or $GatewayRoot.Contains("`r")) { throw 'Invalid gateway path.' }
foreach ($required in @('gateway.json','scripts\Manage.ps1','bin\zt-gateway.exe')) {
    if (-not (Test-Path -LiteralPath (Join-Path $GatewayRoot $required))) { throw "Install and configure the gateway first: missing $required" }
}
$source = Join-Path $root 'bin\zerobridge-desktop.exe'
if (-not (Test-Path -LiteralPath $source)) { throw 'Run scripts\Build-Desktop.ps1 first.' }
$target = Join-Path $GatewayRoot 'bin\zerobridge-desktop.exe'
if ([IO.Path]::GetFullPath($source) -ne $target) { Copy-Item -LiteralPath $source -Destination $target -Force }
$action = New-ScheduledTaskAction -Execute $target -Argument "--root `"$GatewayRoot`" --minimized" -WorkingDirectory $GatewayRoot
$trigger = New-ScheduledTaskTrigger -AtLogOn -User $UserId
$principal = New-ScheduledTaskPrincipal -UserId $UserId -LogonType Interactive -RunLevel Limited
$settings = New-ScheduledTaskSettingsSet -MultipleInstances IgnoreNew -ExecutionTimeLimit ([TimeSpan]::Zero) -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries
Register-ScheduledTask -TaskName 'ZeroBridgeMihomoTray' -Action $action -Trigger $trigger -Principal $principal -Settings $settings -Description 'ZeroBridge Mihomo dashboard and notification-area icon; does not start or stop the gateway.' -Force | Out-Null
Write-Output "Desktop installed: $target"
Write-Output "Logon task: ZeroBridgeMihomoTray ($UserId, interactive, limited privileges)"
Write-Output 'Launch the desktop from your ordinary user session. The existing gateway task was not modified.'
