#requires -Version 5.1
[CmdletBinding()]
param()
$ErrorActionPreference = 'Stop'
$root = Split-Path $PSScriptRoot -Parent
$compiler = Join-Path $env:SystemRoot 'Microsoft.NET\Framework64\v4.0.30319\csc.exe'
if (-not (Test-Path -LiteralPath $compiler)) { throw '.NET Framework 4.x C# compiler is required.' }
$output = Join-Path $root 'bin\zerobridge-desktop.exe'
New-Item -ItemType Directory -Force -Path (Split-Path $output) | Out-Null
$sources = @(Get-ChildItem -LiteralPath (Join-Path $root 'desktop') -Filter '*.cs' | ForEach-Object FullName)
& $compiler /nologo /target:winexe /platform:x64 /optimize+ /codepage:65001 /reference:System.dll /reference:System.Core.dll /reference:System.Drawing.dll /reference:System.Windows.Forms.dll /reference:System.Web.Extensions.dll "/win32manifest:$(Join-Path $root 'desktop\app.manifest')" "/out:$output" @sources
if ($LASTEXITCODE -ne 0) { throw 'Desktop build failed.' }
Write-Output $output
