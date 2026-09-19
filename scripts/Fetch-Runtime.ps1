#requires -Version 5.1
[CmdletBinding()]
param([string]$Proxy)
$ErrorActionPreference='Stop'
$root=[IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$runtime=Join-Path $root 'runtime'
$manifest=Get-Content -LiteralPath (Join-Path $runtime 'manifest.json') -Raw -Encoding UTF8|ConvertFrom-Json
$cache=Join-Path $root ('.cache\runtime-'+[Guid]::NewGuid().ToString('N'))
[IO.Directory]::CreateDirectory($cache)|Out-Null
$zip=Join-Path $cache 'WinDivert.zip'
$request=@{Uri=$manifest.source;OutFile=$zip;UseBasicParsing=$true;TimeoutSec=120}
if(-not [string]::IsNullOrWhiteSpace($Proxy)){$request.Proxy=$Proxy}
Invoke-WebRequest @request
if((Get-FileHash -LiteralPath $zip -Algorithm SHA256).Hash -ine $manifest.archive_sha256){throw 'Official archive SHA256 mismatch; runtime not installed.'}
Expand-Archive -LiteralPath $zip -DestinationPath (Join-Path $cache 'extracted')
$dlls=@(Get-ChildItem -LiteralPath (Join-Path $cache 'extracted') -Filter 'WinDivert.dll' -Recurse|Where-Object {$_.Directory.Name -eq 'x64'})
if($dlls.Count -ne 1){throw 'Expected exactly one official x64 DLL.'}
$dll=$dlls[0].FullName
$driver=Join-Path $dlls[0].Directory.FullName 'WinDivert64.sys'
if((Get-FileHash -LiteralPath $dll).Hash -ine $manifest.dll_sha256 -or (Get-FileHash -LiteralPath $driver).Hash -ine $manifest.driver_sha256){throw 'Official runtime file hash mismatch.'}
if((Get-AuthenticodeSignature -LiteralPath $driver).Status -ne 'Valid'){throw 'Official driver Authenticode signature is not valid.'}
foreach($file in @($dll,$driver)){
    $target=Join-Path $runtime ([IO.Path]::GetFileName($file))
    if(Test-Path -LiteralPath $target){
        if((Get-FileHash -LiteralPath $target).Hash -ine (Get-FileHash -LiteralPath $file).Hash){throw 'A different runtime already exists; refusing overwrite.'}
    }else{Copy-Item -LiteralPath $file -Destination $target}
}
Write-Output 'Official x64 DLL/driver downloaded and verified. No driver was loaded or network setting changed.'
Write-Output 'Downloaded archive is retained in the ignored .cache folder.'
