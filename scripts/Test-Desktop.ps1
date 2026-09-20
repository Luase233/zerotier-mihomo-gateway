#requires -Version 5.1
$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
$compiler = Join-Path $env:SystemRoot 'Microsoft.NET\Framework64\v4.0.30319\csc.exe'
$output = Join-Path $root 'bin\desktop-model-tests.exe'
New-Item -ItemType Directory -Force -Path (Split-Path $output) | Out-Null
& $compiler /nologo /target:exe /codepage:65001 /reference:System.Web.Extensions.dll "/out:$output" (Join-Path $root 'desktop\Model.cs') (Join-Path $root 'tests\DesktopModelTests.cs')
if ($LASTEXITCODE -ne 0) { throw 'Desktop tests failed to compile.' }
& $output
if ($LASTEXITCODE -ne 0) { throw 'Desktop tests failed.' }
