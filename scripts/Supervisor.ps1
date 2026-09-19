#requires -Version 5.1
# Boot supervisor: never removes/restores NAT, changes routes, or asserts phone OFF.
$ErrorActionPreference = 'Stop'
$env:PSModulePath = Join-Path $env:SystemRoot 'System32\WindowsPowerShell\v1.0\Modules'
. (Join-Path $PSScriptRoot 'Common.ps1')
Assert-Elevated
$mutex = Enter-GatewayLock
$supervisorLog = Join-Path $gatewayState 'supervisor.log'
function Write-SupervisorLog([string]$Message) {
    if ((Test-Path -LiteralPath $supervisorLog) -and (Get-Item -LiteralPath $supervisorLog).Length -gt 5MB) {
        $oldLog = $supervisorLog + '.previous'
        if (Test-Path -LiteralPath $oldLog) { Remove-Item -LiteralPath $oldLog -Force }
        Move-Item -LiteralPath $supervisorLog -Destination $oldLog
    }
    Add-Content -LiteralPath $supervisorLog -Encoding UTF8 -Value (([DateTime]::UtcNow.ToString('o')) + ' ' + $Message)
}
function Set-GatewayHigh($Identity) {
    $owned = Get-OwnedProcess $Identity
    if ($null -eq $owned) { throw 'Owned gateway process exited before priority assignment.' }
    $owned.PriorityClass = [Diagnostics.ProcessPriorityClass]::High
}
try {
    [IO.Directory]::CreateDirectory($gatewayState) | Out-Null
    (Get-Process -Id $PID).PriorityClass = [Diagnostics.ProcessPriorityClass]::High
    $boot = (Get-CimInstance Win32_OperatingSystem).LastBootUpTime.ToUniversalTime()
    $manifest = Read-GatewayManifest
    if ($null -ne $manifest -and [DateTime]::Parse($manifest.StartedUtc).ToUniversalTime() -lt $boot) {
        # A previous boot cannot own any current PID, even if Windows reused it.
        Assert-NoUnrecordedGateway
        Write-GatewayJson (Join-Path $gatewayState ('previous-boot-' + $manifest.RunId + '.json')) $manifest
        Remove-Item -LiteralPath $gatewayManifest -Force
        $manifest = $null
    }
    if ($null -eq $manifest) {
        Assert-NoUnrecordedGateway
        $runId = [DateTime]::UtcNow.ToString('yyyyMMddTHHmmssZ') + '-' + [Guid]::NewGuid().ToString('N').Substring(0,8)
        $runDirectory = Join-Path $gatewayState ('runs\' + $runId)
        [IO.Directory]::CreateDirectory($runDirectory) | Out-Null
        $manifest = [pscustomobject]@{SchemaVersion=1;Task='zt-socks-gateway';RunId=$runId;StartedUtc=[DateTime]::UtcNow.ToString('o');Status='waiting';RunDirectory=$runDirectory;Guard=$null;Proxy=$null}
        Write-GatewayJson $gatewayManifest $manifest
    }
    foreach ($property in @('GuardReadyPath','GuardDirectory','ProxyDirectory','SupervisorPid','LastError','ProxyGuardPid')) {
        if ($null -eq $manifest.PSObject.Properties[$property]) { $manifest | Add-Member -NotePropertyName $property -NotePropertyValue $null }
    }
    if ([string]::IsNullOrWhiteSpace($manifest.GuardReadyPath) -and $null -ne $manifest.Guard) {
        $manifest.GuardReadyPath = Join-Path $manifest.RunDirectory 'guard.ready.json'
    }
    if ($null -eq $manifest.ProxyGuardPid -and $null -ne $manifest.Guard -and $null -ne $manifest.Proxy) { $manifest.ProxyGuardPid = $manifest.Guard.Pid }
    $manifest.SupervisorPid = $PID
    Write-SupervisorLog 'Supervisor started. High priority; NAT/route configuration is never changed here.'
    $lastProbeUtc = [DateTime]::MinValue
    while ($true) {
        try {
            $recorded = @()
            foreach ($identity in @($manifest.Guard,$manifest.Proxy)) { if ($null -ne $identity) { $recorded += [int]$identity.Pid } }
            Assert-NoUnrecordedGateway -RecordedPids $recorded
            $guardProcess = Get-OwnedProcess $manifest.Guard
            $proxyProcess = Get-OwnedProcess $manifest.Proxy
            if ($null -eq $guardProcess) {
                # An old proxy may already be in DROP mode. Keep its handle until
                # the replacement guard has independently reported readiness.
                $childDirectory = Join-Path $manifest.RunDirectory ('guard-' + [Guid]::NewGuid().ToString('N').Substring(0,8))
                [IO.Directory]::CreateDirectory($childDirectory) | Out-Null
                $child = Start-GatewayChild -Mode guard -RunDirectory $childDirectory
                $manifest.Guard = $child.Identity
                $manifest.GuardReadyPath = $child.ReadyPath
                $manifest.GuardDirectory = $childDirectory
                $manifest.Status = 'guard-starting'
                Write-GatewayJson $gatewayManifest $manifest
                Set-GatewayHigh $manifest.Guard
                Wait-GatewayReady $manifest.Guard $manifest.GuardReadyPath
                Stop-OwnedProcess $manifest.Proxy
                $manifest.Proxy = $null
                $proxyProcess = $null
                $manifest.Status = 'guard-ready'
                Write-GatewayJson $gatewayManifest $manifest
                Write-SupervisorLog 'Guard ready. Waiting for working Mihomo SOCKS before proxy startup.'
            }
            Wait-GatewayReady $manifest.Guard $manifest.GuardReadyPath
            if ($null -ne $proxyProcess -and $manifest.ProxyGuardPid -ne $manifest.Guard.Pid) {
                Stop-OwnedProcess $manifest.Proxy
                $manifest.Proxy = $null
                $proxyProcess = $null
                Write-GatewayJson $gatewayManifest $manifest
            }
            if ($null -eq $proxyProcess) {
                if (([DateTime]::UtcNow - $lastProbeUtc).TotalSeconds -lt 30) { Start-Sleep -Seconds 5; continue }
                $lastProbeUtc = [DateTime]::UtcNow
                $childDirectory = Join-Path $manifest.RunDirectory ('proxy-' + [Guid]::NewGuid().ToString('N').Substring(0,8))
                [IO.Directory]::CreateDirectory($childDirectory) | Out-Null
                $check = Invoke-BoundedGatewayProcess -Arguments "check -config `"$gatewayConfig`"" -RunDirectory $childDirectory -LogName 'check' -TimeoutMilliseconds 45000
                if ($check.ExitCode -ne 0) { throw "Mihomo/system check failed; guard retained. Logs: $childDirectory" }
                # Start-GatewayChild expects guard readiness beside the proxy.
                # Copy only the verified live guard's proof; Go verifies its PID,
                # executable, creation time, configuration hash and source again.
                $null = Get-OwnedProcess $manifest.Guard
                Copy-Item -LiteralPath $manifest.GuardReadyPath -Destination (Join-Path $childDirectory 'guard.ready.json')
                $child = Start-GatewayChild -Mode proxy -RunDirectory $childDirectory -GuardPid $manifest.Guard.Pid
                $manifest.Proxy = $child.Identity
                $manifest.ProxyGuardPid = $manifest.Guard.Pid
                $manifest.ProxyDirectory = $childDirectory
                $manifest.Status = 'proxy-starting'
                Write-GatewayJson $gatewayManifest $manifest
                Set-GatewayHigh $manifest.Proxy
                Wait-GatewayReady $manifest.Proxy $child.ReadyPath
                Write-SupervisorLog 'Proxy ready. Both children use High process priority.'
            }
            $proxyReadyDirectory = $manifest.ProxyDirectory
            if ([string]::IsNullOrWhiteSpace($proxyReadyDirectory)) { $proxyReadyDirectory = $manifest.RunDirectory }
            Wait-GatewayReady $manifest.Proxy (Join-Path $proxyReadyDirectory 'proxy.ready.json')
            Set-GatewayHigh $manifest.Guard
            Set-GatewayHigh $manifest.Proxy
            $manifest.Status = 'ready'
            $manifest.LastError = $null
            Write-GatewayJson $gatewayManifest $manifest
            Start-Sleep -Seconds 5
        }
        catch {
            $manifest.Status = 'waiting-or-recovering'
            $manifest.LastError = $_.Exception.Message
            Write-GatewayJson $gatewayManifest $manifest
            Write-SupervisorLog $manifest.LastError
            # Keep surviving handles; never restore NAT or release protection as
            # a side effect of a failed upstream check or startup attempt.
            Start-Sleep -Seconds 30
        }
    }
}
finally { $mutex.ReleaseMutex(); $mutex.Dispose() }
