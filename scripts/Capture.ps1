#requires -Version 5.1
[CmdletBinding()]
param([ValidateRange(1,300)][int]$Seconds = 90)
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'Common.ps1')
Assert-Elevated
foreach ($file in @($gatewayExe, $gatewayConfig)) { if (-not (Test-Path -LiteralPath $file -PathType Leaf)) { throw "Required file missing: $file" } }
$mutex = Enter-GatewayLock
try {
    if ($null -ne (Read-GatewayManifest)) { throw 'A gateway run is recorded. Capture-only mode is intended before Start; finish rollback before using this wrapper.' }
    Assert-NoUnrecordedGateway
    $runDirectory = Join-Path $gatewayState ('captures\' + [DateTime]::UtcNow.ToString('yyyyMMddTHHmmssZ') + '-' + [Guid]::NewGuid().ToString('N').Substring(0,8))
    [IO.Directory]::CreateDirectory($runDirectory) | Out-Null
    Write-Output 'Capture is a bounded SNIFF-only diagnostic. It does not proxy or block traffic and does not change NAT, routes, firewall, or forwarding.'
    Write-Output "Do not infer that capture is a protected gateway. Logs: $runDirectory"
    $ready = Join-Path $runDirectory 'capture.ready.json'
    $arguments = "capture -config `"$gatewayConfig`" -duration $($Seconds)s -ready-file `"$ready`""
    $capture = Invoke-BoundedGatewayProcess -Arguments $arguments -RunDirectory $runDirectory -LogName 'capture' -TimeoutMilliseconds (($Seconds + 15) * 1000) -IdentityPath (Join-Path $runDirectory 'capture.process.json')
    if ($capture.ExitCode -ne 0) { throw "Capture failed with exit code $($capture.ExitCode). Inspect its stderr log." }
    Write-Output "Capture completed. Logs: $runDirectory"
}
finally { $mutex.ReleaseMutex(); $mutex.Dispose() }
