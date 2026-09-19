#requires -Version 5.1
[CmdletBinding()]
param([ValidateSet('Status','Start','Stop')][string]$Action='Status', [switch]$PhoneDefaultRouteIsOff, [switch]$ExternalPoolsAreAutomatic)
$ErrorActionPreference='Stop'
$env:PSModulePath = Join-Path $env:SystemRoot 'System32\WindowsPowerShell\v1.0\Modules'
. (Join-Path $PSScriptRoot 'Common.ps1')
$taskName='ZeroTierGateway'
if ($ExternalPoolsAreAutomatic -and $Action -ne 'Start') { throw '-ExternalPoolsAreAutomatic is only valid for Start after explicit confirmation of NAT history.' }
if ($Action -eq 'Status') {
    $task=Get-ScheduledTask -TaskName $taskName -ErrorAction SilentlyContinue
    $taskState=if($null -eq $task){'UnavailableWithoutElevation'}else{[string]$task.State}
    $heartbeatAge=$null
    if(Test-Path -LiteralPath $gatewayManifest){$heartbeatAge=[Math]::Round(([DateTime]::UtcNow-(Get-Item -LiteralPath $gatewayManifest).LastWriteTimeUtc).TotalSeconds,1)}
    [pscustomobject]@{TaskState=$taskState;RuntimeHeartbeatAgeSeconds=$heartbeatAge;Runtime=(Read-GatewayManifest)} | ConvertTo-Json -Depth 8
    return
}
if (-not $PhoneDefaultRouteIsOff) { throw 'Turn the phone Default Route OFF before Start/Stop, then pass -PhoneDefaultRouteIsOff.' }
$principal=New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())
if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    $ps=Join-Path $env:SystemRoot 'System32\WindowsPowerShell\v1.0\powershell.exe'
    $arguments="-NoProfile -NonInteractive -ExecutionPolicy Bypass -File `"$PSCommandPath`" -Action $Action -PhoneDefaultRouteIsOff"
    if ($ExternalPoolsAreAutomatic) { $arguments+=' -ExternalPoolsAreAutomatic' }
    Start-Process -FilePath $ps -ArgumentList $arguments -Verb RunAs -WindowStyle Hidden | Out-Null
    Write-Output 'Elevated operation requested. Read state\manage-result.json for completion.'
    return
}
$result=[pscustomobject]@{Action=$Action;Status='running';Error=$null;UpdatedUtc=[DateTime]::UtcNow.ToString('o')}
Write-GatewayJson (Join-Path $gatewayState 'manage-result.json') $result
try {
    if ($Action -eq 'Stop') {
        Disable-ScheduledTask -TaskName $taskName | Out-Null
        Stop-ScheduledTask -TaskName $taskName
        $deadline=[DateTime]::UtcNow.AddSeconds(30)
        while ((Get-ScheduledTask -TaskName $taskName).State -eq 'Running') {
            if ([DateTime]::UtcNow -gt $deadline) { throw 'Supervisor task did not stop; NAT was not restored.' }
            Start-Sleep -Milliseconds 500
        }
        & (Join-Path $PSScriptRoot 'Stop.ps1') -PhoneDefaultRouteIsOff
    }
    else {
        if ((Get-ScheduledTask -TaskName $taskName).State -eq 'Running') { throw 'Gateway task already running; inspect Status instead.' }
        & (Join-Path $PSScriptRoot 'Start.ps1') -PhoneDefaultRouteIsOff -ExternalPoolsAreAutomatic:$ExternalPoolsAreAutomatic
        Enable-ScheduledTask -TaskName $taskName | Out-Null
        Start-ScheduledTask -TaskName $taskName
    }
    $result.Status='completed'
}
catch { $result.Status='failed';$result.Error=$_.Exception.Message;throw }
finally { $result.UpdatedUtc=[DateTime]::UtcNow.ToString('o');Write-GatewayJson (Join-Path $gatewayState 'manage-result.json') $result }
