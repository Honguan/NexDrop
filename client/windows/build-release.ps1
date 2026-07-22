param(
	[string]$Flutter = 'flutter',
	[string]$Version = '2.0.4',
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

function Test-ExecutableStarts([string]$Path, [string]$StateRoot) {
    New-Item -ItemType Directory -Force -Path $StateRoot | Out-Null
    $previousLocalAppData = $env:LOCALAPPDATA
    $previousBridgePort = $env:NEXDROP_DESKTOP_BRIDGE_PORT
    $process = $null
    $servicePath = [IO.Path]::GetFullPath((Join-Path (Split-Path -Parent $Path) 'nexdrop-desktop-service.exe'))
    try {
        $env:LOCALAPPDATA = $StateRoot
        $env:NEXDROP_DESKTOP_BRIDGE_PORT = '0'
        $process = Start-Process -FilePath $Path -WindowStyle Hidden -PassThru
        $env:LOCALAPPDATA = $previousLocalAppData
        $env:NEXDROP_DESKTOP_BRIDGE_PORT = $previousBridgePort
        Start-Sleep -Seconds 5
        if ($process.HasExited) { throw "Packaged application exited during startup verification: $Path" }
        $serviceProcesses = @(Get-CimInstance Win32_Process -Filter "Name = 'nexdrop-desktop-service.exe'" | Where-Object {
            $_.ExecutablePath -and [IO.Path]::GetFullPath($_.ExecutablePath).Equals($servicePath, [StringComparison]::OrdinalIgnoreCase)
        })
        if ($serviceProcesses.Count -ne 1) { throw "Packaged desktop service did not start exactly once: $servicePath" }
    } finally {
        $env:LOCALAPPDATA = $previousLocalAppData
        $env:NEXDROP_DESKTOP_BRIDGE_PORT = $previousBridgePort
        if ($process -and -not $process.HasExited) {
            Stop-Process -Id $process.Id -Force
            $process.WaitForExit()
        }
        @(Get-CimInstance Win32_Process -Filter "Name = 'nexdrop-desktop-service.exe'" -ErrorAction SilentlyContinue | Where-Object {
            $_.ExecutablePath -and [IO.Path]::GetFullPath($_.ExecutablePath).Equals($servicePath, [StringComparison]::OrdinalIgnoreCase)
        }) | ForEach-Object {
            Stop-Process -Id $_.ProcessId -Force
            Wait-Process -Id $_.ProcessId -ErrorAction SilentlyContinue
        }
        if (Test-Path -LiteralPath $StateRoot) {
            Remove-Item -LiteralPath $StateRoot -Recurse -Force
        }
    }
}

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
	Get-ChildItem -LiteralPath $release,$installer -Filter '*.exe' -File | ForEach-Object {
		$signature = Get-AuthenticodeSignature -LiteralPath $_.FullName
		if ($signature.Status -ne 'Valid') { throw "Invalid Authenticode signature: $($_.Name) ($($signature.Status))" }
	}
}

$temporaryRoot = [IO.Path]::GetFullPath([IO.Path]::GetTempPath())
$verificationRoot = Join-Path $temporaryRoot ("nexdrop-release-" + [guid]::NewGuid().ToString('N'))
if (-not ([IO.Path]::GetFullPath($verificationRoot)).StartsWith($temporaryRoot, [StringComparison]::OrdinalIgnoreCase)) {
    throw 'Release verification path escaped the temporary directory'
}
New-Item -ItemType Directory -Force -Path $verificationRoot | Out-Null
$installedDirectory = Join-Path $verificationRoot 'installed'
$verificationGroup = 'NexDrop Release Verification ' + [guid]::NewGuid().ToString('N')
$startMenuGroup = Join-Path $env:APPDATA "Microsoft\Windows\Start Menu\Programs\$verificationGroup"
$startMenuShortcut = Join-Path $startMenuGroup 'NexDrop.lnk'
$uninstaller = Join-Path $installedDirectory 'unins000.exe'
$uninstallCompleted = $false
try {
    $portableDirectory = Join-Path $verificationRoot 'portable'
    Expand-Archive -LiteralPath $portable -DestinationPath $portableDirectory
    Test-ExecutableStarts (Join-Path $portableDirectory 'NexDrop.exe') (Join-Path $verificationRoot 'portable-state')

    $verificationAppId = '{{' + [guid]::NewGuid().ToString().ToUpperInvariant() + '}'
    & $InnoSetup "/DMyAppVersion=$Version" "/DMyAppId=$verificationAppId" "/DSourceDir=$release" "/DOutputDir=$verificationRoot" (Join-Path $PSScriptRoot 'installer.iss')
    if ($LASTEXITCODE -ne 0) { throw 'Verification installer build failed' }
    $installer = Join-Path $verificationRoot "NexDrop-Desktop_${Version}_windows_x64.exe"
    if ($CertificatePath) {
        signtool.exe sign /fd SHA256 /td SHA256 /tr http://timestamp.digicert.com /f $CertificatePath /p $CertificatePassword $installer
        if ($LASTEXITCODE -ne 0) { throw 'Verification installer signing failed' }
        $verificationSignature = Get-AuthenticodeSignature -LiteralPath $installer
        if ($verificationSignature.Status -ne 'Valid') { throw "Invalid verification installer signature: $($verificationSignature.Status)" }
    }
    $install = Start-Process -FilePath $installer -WindowStyle Hidden -Wait -PassThru -ArgumentList @(
        '/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART', "/DIR=`"$installedDirectory`"", "/GROUP=`"$verificationGroup`""
    )
    if ($install.ExitCode -ne 0) { throw "Installer verification failed with exit code $($install.ExitCode)" }
    $installedExecutable = Join-Path $installedDirectory 'NexDrop.exe'
    if (-not (Test-Path -LiteralPath $startMenuShortcut)) { throw 'Installer did not create the NexDrop Start Menu shortcut' }
    $shell = New-Object -ComObject WScript.Shell
    try {
        $shortcutTarget = $shell.CreateShortcut($startMenuShortcut).TargetPath
        if ([IO.Path]::GetFullPath($shortcutTarget) -ne [IO.Path]::GetFullPath($installedExecutable)) {
            throw "Start Menu shortcut target is invalid: $shortcutTarget"
        }
    } finally {
        [Runtime.InteropServices.Marshal]::FinalReleaseComObject($shell) | Out-Null
    }
    Test-ExecutableStarts $installedExecutable (Join-Path $verificationRoot 'installed-state')
    if (-not (Test-Path -LiteralPath $uninstaller)) { throw 'Installer verification did not produce an uninstaller' }
    $uninstall = Start-Process -FilePath $uninstaller -WindowStyle Hidden -Wait -PassThru -ArgumentList @('/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART')
    if ($uninstall.ExitCode -ne 0) { throw "Uninstaller verification failed with exit code $($uninstall.ExitCode)" }
    $uninstallCompleted = $true
    for ($attempt = 0; $attempt -lt 20 -and ((Test-Path -LiteralPath $installedDirectory) -or (Test-Path -LiteralPath $startMenuShortcut)); $attempt++) {
        Start-Sleep -Milliseconds 500
    }
    if ((Test-Path -LiteralPath $installedDirectory) -or (Test-Path -LiteralPath $startMenuShortcut)) {
        throw 'Uninstaller verification left installed files or shortcuts behind'
    }
} finally {
    $cleanupFailure = $null
    try {
        if (-not $uninstallCompleted -and (Test-Path -LiteralPath $uninstaller)) {
            $cleanup = Start-Process -FilePath $uninstaller -WindowStyle Hidden -Wait -PassThru -ArgumentList @('/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART')
            if ($cleanup.ExitCode -ne 0) { throw "Release verification cleanup failed with exit code $($cleanup.ExitCode)" }
        }
    } catch {
        $cleanupFailure = $_
    }
    if (Test-Path -LiteralPath $startMenuGroup) {
        Remove-Item -LiteralPath $startMenuGroup -Recurse -Force
    }
    if (Test-Path -LiteralPath $verificationRoot) {
        Remove-Item -LiteralPath $verificationRoot -Recurse -Force
    }
    if ($cleanupFailure) { throw $cleanupFailure }
}

Get-ChildItem -LiteralPath $OutputDirectory -File | Get-FileHash -Algorithm SHA256 |
	ForEach-Object { "$($_.Hash.ToLower())  $($_.Path | Split-Path -Leaf)" } |
	Set-Content -Encoding ascii (Join-Path $OutputDirectory 'checksums-windows-sha256.txt')

Write-Output "Release created at $OutputDirectory"
