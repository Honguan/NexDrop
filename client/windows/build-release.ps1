param(
	[string]$Flutter = 'flutter',
	[string]$Version = '1.0.0',
	[string]$OutputDirectory = '',
	[string]$InnoSetup = 'C:\Program Files (x86)\Inno Setup 6\ISCC.exe',
	[string]$CertificatePath = '',
	[string]$CertificatePassword = ''
)

$ErrorActionPreference = 'Stop'
$repo = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path
$client = Join-Path $repo 'client'
if (-not $OutputDirectory) { $OutputDirectory = Join-Path $repo 'dist' }
New-Item -ItemType Directory -Force -Path $OutputDirectory | Out-Null

Push-Location $client
try {
    & $Flutter build windows --release
    if ($LASTEXITCODE -ne 0) { throw 'Flutter Windows build failed' }
} finally {
    Pop-Location
}

$release = Join-Path $client 'build\windows\x64\runner\Release'
Push-Location $repo
try {
    go build -trimpath -o (Join-Path $release 'nexdrop-desktop-service.exe') ./cmd/nexdrop-desktop-service
    if ($LASTEXITCODE -ne 0) { throw 'Desktop service build failed' }
    go build -trimpath -o (Join-Path $release 'nexdrop-bridge.exe') ./cmd/nexdrop-bridge
    if ($LASTEXITCODE -ne 0) { throw 'Native bridge build failed' }
} finally {
    Pop-Location
}

if ($CertificatePath) {
	Get-ChildItem -LiteralPath $release -Filter '*.exe' -File | ForEach-Object {
		signtool.exe sign /fd SHA256 /td SHA256 /tr http://timestamp.digicert.com /f $CertificatePath /p $CertificatePassword $_.FullName
		if ($LASTEXITCODE -ne 0) { throw "Signing failed: $($_.Name)" }
	}
}

$portable = Join-Path $OutputDirectory "NexDrop-Desktop_${Version}_windows_x64.zip"
Compress-Archive -Path (Join-Path $release '*') -DestinationPath $portable -Force

if (-not (Test-Path -LiteralPath $InnoSetup)) { throw "Inno Setup not found: $InnoSetup" }
& $InnoSetup "/DMyAppVersion=$Version" "/DSourceDir=$release" "/DOutputDir=$OutputDirectory" (Join-Path $PSScriptRoot 'installer.iss')
if ($LASTEXITCODE -ne 0) { throw 'Windows installer build failed' }

if ($CertificatePath) {
	$installer = Join-Path $OutputDirectory "NexDrop-Desktop_${Version}_windows_x64.exe"
	signtool.exe sign /fd SHA256 /td SHA256 /tr http://timestamp.digicert.com /f $CertificatePath /p $CertificatePassword $installer
	if ($LASTEXITCODE -ne 0) { throw 'Installer signing failed' }
}

Get-ChildItem -LiteralPath $OutputDirectory -File | Get-FileHash -Algorithm SHA256 |
	ForEach-Object { "$($_.Hash.ToLower())  $($_.Path | Split-Path -Leaf)" } |
	Set-Content -Encoding ascii (Join-Path $OutputDirectory 'checksums-windows-sha256.txt')

Write-Output "Release created at $OutputDirectory"
