#requires -Version 5.1
[CmdletBinding()]
param([switch]$PhoneDefaultRouteIsOff, [switch]$ExternalPoolsAreAutomatic)
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'Common.ps1')
if (-not $PhoneDefaultRouteIsOff) { throw 'Keep iPhone Enable Default Route OFF and explicitly pass -PhoneDefaultRouteIsOff before starting.' }
Assert-Elevated
foreach ($file in @($gatewayExe, $gatewayConfig)) { if (-not (Test-Path -LiteralPath $file -PathType Leaf)) { throw "Required file missing: $file" } }
$mutex = Enter-GatewayLock
$manifest = $null
try {
    if ($null -ne (Read-GatewayManifest)) { throw 'A runtime manifest already exists. Run Stop.ps1 with the phone default route OFF before another start; the previous run will not be overwritten.' }
    Assert-NoUnrecordedGateway
    $runId = [DateTime]::UtcNow.ToString('yyyyMMddTHHmmssZ') + '-' + [Guid]::NewGuid().ToString('N').Substring(0,8)
    $runDirectory = Join-Path $gatewayState ('runs\' + $runId)
    [IO.Directory]::CreateDirectory($runDirectory) | Out-Null
    & (Join-Path $PSScriptRoot 'Inspect.ps1') -AsJson | Out-File -LiteralPath (Join-Path $runDirectory 'before.json') -Encoding utf8
    # check must perform read-only config / SOCKS / system validation and load no driver.
    $checkArguments = "check -config `"$gatewayConfig`""
    $check = Invoke-BoundedGatewayProcess -Arguments $checkArguments -RunDirectory $runDirectory -LogName 'check' -TimeoutMilliseconds 45000
    if ($check.ExitCode -ne 0) { throw "Gateway preflight failed (exit $($check.ExitCode)). No NAT was changed. Logs: $runDirectory" }
    # Bounded SNIFF|RECV_ONLY smoke test validates actual DLL/driver startup and
    # receive shutdown while the phone stays OFF, before any NAT mutation.
    $smokeArguments = "capture -config `"$gatewayConfig`" -duration 1s"
    $smoke = Invoke-BoundedGatewayProcess -Arguments $smokeArguments -RunDirectory $runDirectory -LogName 'driver-smoke' -TimeoutMilliseconds 30000
    if ($smoke.ExitCode -ne 0) { throw "Read-only driver smoke test failed (exit $($smoke.ExitCode)). No NAT was changed. Logs: $runDirectory" }
    & (Join-Path $PSScriptRoot 'Nat.ps1') -Action Backup -ExternalPoolsAreAutomatic:$ExternalPoolsAreAutomatic
    $manifest = [pscustomobject]@{ SchemaVersion=1; Task='zt-socks-gateway'; RunId=$runId; StartedUtc=[DateTime]::UtcNow.ToString('o'); Status='prepared'; RunDirectory=$runDirectory; Guard=$null; Proxy=$null }
    Write-GatewayJson $gatewayManifest $manifest
    & (Join-Path $PSScriptRoot 'Nat.ps1') -Action Remove -PhoneDefaultRouteIsOff
    $manifest.Status='nat-removed'
    Write-GatewayJson $gatewayManifest $manifest
    $guard = Start-GatewayChild -Mode guard -RunDirectory $runDirectory
    $manifest.Guard=$guard.Identity
    $manifest.Status='guard-starting'
    Write-GatewayJson $gatewayManifest $manifest
    Wait-GatewayReady $guard.Identity $guard.ReadyPath
    $manifest.Status='guard-ready'
    Write-GatewayJson $gatewayManifest $manifest
    $proxy = Start-GatewayChild -Mode proxy -RunDirectory $runDirectory -GuardPid $guard.Identity.Pid
    $manifest.Proxy=$proxy.Identity
    $manifest.Status='proxy-starting'
    Write-GatewayJson $gatewayManifest $manifest
    Wait-GatewayReady $proxy.Identity $proxy.ReadyPath
    # Both identities must still be present when reporting readiness.
    if ($null -eq (Get-OwnedProcess $manifest.Guard) -or $null -eq (Get-OwnedProcess $manifest.Proxy)) { throw 'A gateway child exited during startup.' }
    $manifest.Status='ready'
    Write-GatewayJson $gatewayManifest $manifest
    Write-Output "Guard and proxy reported ready. Run: $runId"
    Write-Output "Logs and pre-start snapshot: $runDirectory"
    Write-Output 'This confirms process startup only. Phone connectivity, DNS, UDP, and proxy egress still require live validation.'
}
catch {
    if ($null -ne $manifest) {
        $manifest.Status='startup-failed'
        try { Write-GatewayJson $gatewayManifest $manifest } catch { Write-Warning 'Could not update runtime manifest; retain this window and logs for process ownership review.' }
        Write-Warning 'Startup failed. Any started guard/proxy are intentionally left running; NAT is not automatically restored, which could permit direct egress.'
        Write-Warning "Keep the iPhone default route OFF. Roll back with: & '$PSScriptRoot\Stop.ps1' -PhoneDefaultRouteIsOff"
        Write-Warning ("Recorded guard/proxy identity: " + (($manifest | Select-Object Guard,Proxy) | ConvertTo-Json -Depth 4 -Compress))
    }
    throw
}
finally { $mutex.ReleaseMutex(); $mutex.Dispose() }
