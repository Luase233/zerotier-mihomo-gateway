# Shared operational helpers. Dot-source only; this file starts no processes.
Set-StrictMode -Version 2
$gatewayRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
if (Test-Path -LiteralPath (Join-Path $gatewayRoot 'DEPLOYED.json')) { throw 'This workspace copy was migrated. Use C:\ZeroTierGateway\scripts\Manage.ps1; do not operate the archived instance.' }
$gatewayExe = Join-Path $gatewayRoot 'bin\zt-gateway.exe'
$gatewayConfig = Join-Path $gatewayRoot 'gateway.json'
$gatewayState = Join-Path $gatewayRoot 'state'
$gatewayManifest = Join-Path $gatewayState 'runtime.json'

function Assert-Elevated {
    $identity = [Security.Principal.WindowsIdentity]::GetCurrent()
    $principal = New-Object Security.Principal.WindowsPrincipal($identity)
    if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) { throw 'Run this script from an elevated PowerShell window. No network changes were made.' }
}

function Enter-GatewayLock {
    $mutex = New-Object Threading.Mutex($false, 'Global\ZtSocksGateway')
    $acquired = $false
    try {
        try { $acquired = $mutex.WaitOne(0) }
        catch [Threading.AbandonedMutexException] { $acquired = $true }
        if (-not $acquired) { throw 'Another gateway operational script is running. Wait for it to finish.' }
        return $mutex
    }
    catch { $mutex.Dispose(); throw }
}

function Write-GatewayJson {
    param([string]$Path, $Value)
    $directory = Split-Path -Parent $Path
    [IO.Directory]::CreateDirectory($directory) | Out-Null
    $temporary = Join-Path $directory ([Guid]::NewGuid().ToString('N') + '.tmp')
    try {
        [IO.File]::WriteAllText($temporary, ($Value | ConvertTo-Json -Depth 10), (New-Object Text.UTF8Encoding($false)))
        if (Test-Path -LiteralPath $Path -PathType Leaf) { [IO.File]::Replace($temporary, $Path, [NullString]::Value) }
        else { [IO.File]::Move($temporary, $Path) }
    }
    finally { if (Test-Path -LiteralPath $temporary -PathType Leaf) { Remove-Item -LiteralPath $temporary -Force } }
}

function Read-GatewayManifest {
    if (-not (Test-Path -LiteralPath $gatewayManifest -PathType Leaf)) { return $null }
    $manifest = Get-Content -LiteralPath $gatewayManifest -Raw -Encoding UTF8 | ConvertFrom-Json
    if ($manifest.SchemaVersion -ne 1 -or $manifest.Task -cne 'zt-socks-gateway' -or $manifest.RunId -cnotmatch '^\d{8}T\d{6}Z-[a-f0-9]{8}$') { throw 'Unknown runtime manifest. Refusing to infer ownership of running processes.' }
    return $manifest
}

function Get-GatewayProcessPath {
    param([Diagnostics.Process]$Process)
    # .NET's immediate MainModule/Path snapshot may be empty just after a
    # SYSTEM child is created. Retry fresh snapshots, then query its image path.
    for ($attempt=0; $attempt -lt 10; $attempt++) {
        if ($Process.HasExited) { throw 'Gateway process exited during path verification.' }
        $imagePath=$null
        try { $fresh=Get-Process -Id $Process.Id -ErrorAction Stop; $imagePath=$fresh.Path } catch { }
        if ([string]::IsNullOrWhiteSpace($imagePath)) {
            $native=Get-CimInstance Win32_Process -Filter ('ProcessId = ' + $Process.Id) -ErrorAction Stop
            if ($null -ne $native) { $imagePath=$native.ExecutablePath }
        }
        if (-not [string]::IsNullOrWhiteSpace($imagePath)) {
            if ($Process.HasExited) { throw 'Gateway process exited during image lookup.' }
            return [IO.Path]::GetFullPath($imagePath)
        }
        [Threading.Thread]::Sleep(100)
    }
    throw 'Could not verify gateway executable path; process identity is not assumed.'
}

function Get-ProcessIdentity {
    param([Diagnostics.Process]$Process)
    $Process.Refresh()
    if ($Process.HasExited) { throw 'Gateway child exited before its identity could be recorded.' }
    $path = Get-GatewayProcessPath $Process
    if (-not [string]::Equals($path, $gatewayExe, [StringComparison]::OrdinalIgnoreCase)) { throw 'Started process has an unexpected executable path.' }
    return [pscustomobject]@{ Pid = $Process.Id; Executable = $path; StartUtcTicks = $Process.StartTime.ToUniversalTime().Ticks.ToString() }
}

function Get-OwnedProcess {
    param($Identity)
    if ($null -eq $Identity) { return $null }
    if ([int]$Identity.Pid -le 0 -or -not [string]::Equals([string]$Identity.Executable, $gatewayExe, [StringComparison]::OrdinalIgnoreCase)) { throw 'Invalid process identity in runtime manifest; no process was stopped.' }
    $process = Get-Process -Id ([int]$Identity.Pid) -ErrorAction SilentlyContinue
    if ($null -eq $process) { return $null }
    if (-not [string]::Equals((Get-GatewayProcessPath $process), $gatewayExe, [StringComparison]::OrdinalIgnoreCase) -or $process.StartTime.ToUniversalTime().Ticks.ToString() -cne [string]$Identity.StartUtcTicks) { throw "PID $($Identity.Pid) was reused or belongs to another process. Refusing to stop it." }
    return $process
}

function Stop-OwnedProcess {
    param($Identity)
    $process = Get-OwnedProcess $Identity
    if ($null -eq $process) { return }
    # Kill on this opened Process object, after verifying image path and start time.
    $process.Kill()
    if (-not $process.WaitForExit(10000)) { throw "PID $($process.Id) did not stop; later rollback steps were not attempted." }
}

function Assert-NoUnrecordedGateway {
    param([int[]]$RecordedPids = @())
    foreach ($process in @(Get-Process -Name 'zt-gateway' -ErrorAction SilentlyContinue)) {
        if ($RecordedPids -contains $process.Id) { continue }
        if ($process.HasExited) { continue }
        if ([string]::Equals((Get-GatewayProcessPath $process), $gatewayExe, [StringComparison]::OrdinalIgnoreCase)) { throw "Unrecorded gateway PID $($process.Id) exists. Refusing to start or restore NAT until its ownership is reviewed." }
    }
}

function Wait-GatewayReady {
    param($Identity, [string]$ReadyPath)
    $deadline = [DateTime]::UtcNow.AddSeconds(30)
    do {
        $process = Get-OwnedProcess $Identity
        if ($null -eq $process) { throw "Gateway PID $($Identity.Pid) exited before readiness. See its stderr log." }
        if (Test-Path -LiteralPath $ReadyPath -PathType Leaf) {
            $ready = $null
            try { $ready = Get-Content -LiteralPath $ReadyPath -Raw -Encoding UTF8 | ConvertFrom-Json } catch { }
            if ($null -ne $ready -and $null -ne $ready.PSObject.Properties['pid'] -and [int]$ready.pid -eq [int]$Identity.Pid) { return }
        }
        [Threading.Thread]::Sleep(100)
    } while ([DateTime]::UtcNow -lt $deadline)
    throw "Gateway PID $($Identity.Pid) did not report readiness within 30 seconds. See its stderr log."
}

function Start-GatewayChild {
    param([ValidateSet('guard','proxy')][string]$Mode, [string]$RunDirectory, [int]$GuardPid = 0)
    $readyPath = Join-Path $RunDirectory ($Mode + '.ready.json')
    # Windows file paths cannot contain quotes; quote each file argument for CRT parsing.
    $arguments = "$Mode -config `"$gatewayConfig`" -ready-file `"$readyPath`""
    if ($Mode -eq 'proxy') {
        $guardReadyPath = Join-Path $RunDirectory 'guard.ready.json'
        $arguments += " -duration 0 -guard-pid $GuardPid -guard-ready-file `"$guardReadyPath`""
    }
    $process = Start-Process -FilePath $gatewayExe -ArgumentList $arguments -WorkingDirectory $gatewayRoot -WindowStyle Hidden -PassThru -RedirectStandardOutput (Join-Path $RunDirectory ($Mode + '.stdout.log')) -RedirectStandardError (Join-Path $RunDirectory ($Mode + '.stderr.log'))
    try { return [pscustomobject]@{ Identity = Get-ProcessIdentity $process; ReadyPath = $readyPath } }
    catch {
        # The retained Process owns the launched handle. Do not leave an
        # unrecorded child if image verification unexpectedly fails.
        if (-not $process.HasExited) { $process.Kill(); $null=$process.WaitForExit(10000) }
        throw
    }
}

function Invoke-BoundedGatewayProcess {
    param([string]$Arguments, [string]$RunDirectory, [string]$LogName, [int]$TimeoutMilliseconds, [string]$IdentityPath)
    # Start-Process -PassThru may lose the exit handle on Windows PowerShell 5.1.
    # Retain our own Process and redirect both streams concurrently to local files.
    $info = New-Object Diagnostics.ProcessStartInfo
    $info.FileName = $gatewayExe
    $info.Arguments = $Arguments
    $info.WorkingDirectory = $gatewayRoot
    $info.UseShellExecute = $false
    $info.CreateNoWindow = $true
    $info.WindowStyle = [Diagnostics.ProcessWindowStyle]::Hidden
    $info.RedirectStandardOutput = $true
    $info.RedirectStandardError = $true
    $process = New-Object Diagnostics.Process
    $process.StartInfo = $info
    $stdout = $null
    $stderr = $null
    $started = $false
    try {
        $stdout = [IO.File]::Open((Join-Path $RunDirectory ($LogName + '.stdout.log')), [IO.FileMode]::CreateNew, [IO.FileAccess]::Write, [IO.FileShare]::Read)
        $stderr = [IO.File]::Open((Join-Path $RunDirectory ($LogName + '.stderr.log')), [IO.FileMode]::CreateNew, [IO.FileAccess]::Write, [IO.FileShare]::Read)
        $started = $process.Start()
        if (-not $started) { throw 'Native process did not start.' }
        $processId = $process.Id
        if (-not [string]::IsNullOrWhiteSpace($IdentityPath)) {
            # This object retains its original native handle even if a very fast
            # failure has already exited; no PID lookup is needed for ownership.
            $identity = [pscustomobject]@{ Pid=$processId; Executable=$gatewayExe; StartUtcTicks=$process.StartTime.ToUniversalTime().Ticks.ToString() }
            Write-GatewayJson $IdentityPath $identity
        }
        $outCopy = $process.StandardOutput.BaseStream.CopyToAsync($stdout)
        $errCopy = $process.StandardError.BaseStream.CopyToAsync($stderr)
        if (-not $process.WaitForExit($TimeoutMilliseconds)) {
            $process.Kill()
            if (-not $process.WaitForExit(10000)) { throw "$LogName did not stop after its runtime limit." }
            throw "$LogName exceeded its bounded runtime and was stopped. See its logs."
        }
        $exitCode = $process.ExitCode
        $copies = [Threading.Tasks.Task[]]@($outCopy, $errCopy)
        if (-not [Threading.Tasks.Task]::WaitAll($copies, 5000)) { throw "$LogName output did not close within 5 seconds after exit." }
        return [pscustomobject]@{ ExitCode = [int]$exitCode; Pid = $processId }
    }
    catch {
        # The original Process owns the handle, so cleanup cannot target a reused PID.
        if ($started -and -not $process.HasExited) { $process.Kill(); $null = $process.WaitForExit(10000) }
        throw
    }
    finally {
        if ($null -ne $stdout) { $stdout.Dispose() }
        if ($null -ne $stderr) { $stderr.Dispose() }
        $process.Dispose()
    }
}
