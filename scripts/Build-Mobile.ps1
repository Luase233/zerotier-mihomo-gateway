#requires -Version 5.1
[CmdletBinding()]
param([string]$Go = 'go')
$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
New-Item -ItemType Directory -Force -Path (Join-Path $root 'bin') | Out-Null
& $Go -C $root build -trimpath -o bin/zerobridge-mobile.exe ./cmd/zerobridge-mobile
if ($LASTEXITCODE -ne 0) { throw 'Mobile build failed.' }
