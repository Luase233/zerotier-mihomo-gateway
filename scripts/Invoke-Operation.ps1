#requires -Version 5.1
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)]
    [ValidateSet('Start', 'Stop', 'Capture')]
    [string]$Operation,
    [switch]$PhoneDefaultRouteIsOff,
    [switch]$ExternalPoolsAreAutomatic,
    [ValidateRange(1,300)][int]$Seconds = 90,
    # Internal parameters used only by this script's elevated worker.
    [switch]$ElevatedWorker,
    [string]$OperationId
)
$ErrorActionPreference = 'Stop'
. (Join-Path $PSScriptRoot 'Common.ps1')
if ($Operation -ne 'Capture' -and -not $PhoneDefaultRouteIsOff) { throw 'For Start/Stop, first set the iPhone default route OFF and pass -PhoneDefaultRouteIsOff.' }
if ($ExternalPoolsAreAutomatic -and $Operation -ne 'Start') { throw '-ExternalPoolsAreAutomatic is only used when starting a run and creating its original NAT backup.' }

if ($ElevatedWorker) {
    Assert-Elevated
    if ($OperationId -cnotmatch '^\d{8}T\d{6}Z-[a-f0-9]{8}$') { throw 'Invalid operation identifier.' }
    $operationDirectory = Join-Path $gatewayState ('operations\' + $OperationId)
    $requestPath = Join-Path $operationDirectory 'request.json'
    if (-not (Test-Path -LiteralPath $requestPath -PathType Leaf)) { throw 'The originating operation request is missing.' }
    $request = Get-Content -LiteralPath $requestPath -Raw -Encoding UTF8 | ConvertFrom-Json
    if ($request.Operation -cne $Operation -or $request.OperationId -cne $OperationId -or [bool]$request.PhoneDefaultRouteIsOff -ne [bool]$PhoneDefaultRouteIsOff -or [bool]$request.ExternalPoolsAreAutomatic -ne [bool]$ExternalPoolsAreAutomatic -or [int]$request.Seconds -ne $Seconds) { throw 'The elevated operation does not match its originating request.' }
    $resultPath = Join-Path $operationDirectory 'result.json'
    $transcriptPath = Join-Path $operationDirectory 'transcript.log'
    $result = [pscustomobject]@{ SchemaVersion=1; Task='zt-socks-gateway'; OperationId=$OperationId; Operation=$Operation; Status='running'; WorkerPid=$PID; StartedUtc=[DateTime]::UtcNow.ToString('o'); FinishedUtc=$null; Success=$null; Error=$null; Transcript=$transcriptPath }
    $transcribing = $false
    $failed = $false
    try {
        Write-GatewayJson $resultPath $result
        Start-Transcript -LiteralPath $transcriptPath -Force | Out-Null
        $transcribing = $true
        if ($Operation -eq 'Capture') { & (Join-Path $PSScriptRoot 'Capture.ps1') -Seconds $Seconds }
        elseif ($Operation -eq 'Start') { & (Join-Path $PSScriptRoot 'Start.ps1') -PhoneDefaultRouteIsOff -ExternalPoolsAreAutomatic:$ExternalPoolsAreAutomatic }
        else { & (Join-Path $PSScriptRoot 'Stop.ps1') -PhoneDefaultRouteIsOff }
        $result.Status='completed'
        $result.Success=$true
    }
    catch {
        $failed = $true
        $result.Status='failed'
        $result.Success=$false
        $result.Error=$_.Exception.Message
        Write-Host ('Operation failed: ' + $_.Exception.Message)
    }
    finally {
        if ($transcribing) { try { Stop-Transcript | Out-Null } catch { } }
        $result.FinishedUtc=[DateTime]::UtcNow.ToString('o')
        Write-GatewayJson $resultPath $result
    }
    if ($failed) { exit 1 }
    exit 0
}

if (-not [string]::IsNullOrWhiteSpace($OperationId)) { throw 'OperationId is reserved for the elevated worker.' }
$OperationId = [DateTime]::UtcNow.ToString('yyyyMMddTHHmmssZ') + '-' + [Guid]::NewGuid().ToString('N').Substring(0,8)
$operationDirectory = Join-Path $gatewayState ('operations\' + $OperationId)
[IO.Directory]::CreateDirectory($operationDirectory) | Out-Null
$resultPath = Join-Path $operationDirectory 'result.json'
$request = [pscustomobject]@{ SchemaVersion=1; Task='zt-socks-gateway'; OperationId=$OperationId; Operation=$Operation; PhoneDefaultRouteIsOff=[bool]$PhoneDefaultRouteIsOff; ExternalPoolsAreAutomatic=[bool]$ExternalPoolsAreAutomatic; Seconds=$Seconds; RequestedUtc=[DateTime]::UtcNow.ToString('o') }
Write-GatewayJson (Join-Path $operationDirectory 'request.json') $request
$result = [pscustomobject]@{ SchemaVersion=1; Task='zt-socks-gateway'; OperationId=$OperationId; Operation=$Operation; Status='awaiting-elevation'; WorkerPid=$null; StartedUtc=$null; FinishedUtc=$null; Success=$null; Error=$null; Transcript=(Join-Path $operationDirectory 'transcript.log') }
Write-GatewayJson $resultPath $result
$scriptPath = Join-Path $PSScriptRoot 'Invoke-Operation.ps1'
$powershellExe = Join-Path ([Environment]::GetFolderPath('System')) 'WindowsPowerShell\v1.0\powershell.exe'
$arguments = "-NoProfile -NonInteractive -ExecutionPolicy Bypass -File `"$scriptPath`" -Operation $Operation -ElevatedWorker -OperationId $OperationId -Seconds $Seconds"
if ($PhoneDefaultRouteIsOff) { $arguments += ' -PhoneDefaultRouteIsOff' }
if ($ExternalPoolsAreAutomatic) { $arguments += ' -ExternalPoolsAreAutomatic' }
try {
    # Explicit RunAs produces one UAC prompt. Logging happens inside the worker;
    # Start-Process redirection is intentionally not combined with -Verb RunAs.
    $worker = Start-Process -FilePath $powershellExe -ArgumentList $arguments -WorkingDirectory $gatewayRoot -Verb RunAs -WindowStyle Hidden -PassThru
    # Do not overwrite result.json here: a fast worker may already have completed.
    [pscustomobject]@{ Operation=$Operation; OperationId=$OperationId; WorkerPid=$worker.Id; ResultPath=$resultPath; TranscriptPath=(Join-Path $operationDirectory 'transcript.log') } | ConvertTo-Json
}
catch {
    $result.Status='elevation-failed'
    $result.Success=$false
    $result.Error=$_.Exception.Message
    $result.FinishedUtc=[DateTime]::UtcNow.ToString('o')
    Write-GatewayJson $resultPath $result
    throw "The elevated operation did not launch. No gateway operation was run. Result: $resultPath. $($_.Exception.Message)"
}
