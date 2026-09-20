#requires -Version 5.1
$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
$log = Join-Path $root 'state\mobile\server.log'
if ((Test-Path -LiteralPath $log) -and (Get-Item -LiteralPath $log).Length -gt 1048576) { Move-Item -LiteralPath $log -Destination ($log + '.old') -Force }
& (Join-Path $root 'bin\zerobridge-mobile.exe') --config (Join-Path $root 'mobile.json') 2>> $log
exit $LASTEXITCODE
