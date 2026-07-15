param(
    [Parameter(Mandatory = $true)]
    [ValidatePattern('^[a-p]{32}$')]
    [string]$ExtensionId,

    [Parameter(Mandatory = $true)]
    [string]$HostPath,

    [switch]$Uninstall
)

$ErrorActionPreference = 'Stop'
$name = 'com.nexdrop.bridge'
$manifestDirectory = Join-Path $env:LOCALAPPDATA 'NexDrop\NativeMessaging'
$manifestPath = Join-Path $manifestDirectory "$name.json"
$registryPaths = @(
    "HKCU:\Software\Google\Chrome\NativeMessagingHosts\$name",
    "HKCU:\Software\Microsoft\Edge\NativeMessagingHosts\$name"
)

if ($Uninstall) {
    foreach ($registryPath in $registryPaths) {
        Remove-Item -LiteralPath $registryPath -Recurse -Force -ErrorAction SilentlyContinue
    }
    Remove-Item -LiteralPath $manifestPath -Force -ErrorAction SilentlyContinue
    exit 0
}

$resolvedHost = (Resolve-Path -LiteralPath $HostPath).Path
if ([IO.Path]::GetExtension($resolvedHost) -ne '.exe') {
    throw 'HostPath must point to nexdrop-bridge.exe'
}

New-Item -ItemType Directory -Path $manifestDirectory -Force | Out-Null
$manifest = [ordered]@{
    name = $name
    description = 'NexDrop Desktop native messaging bridge'
    path = $resolvedHost
    type = 'stdio'
    allowed_origins = @("chrome-extension://$ExtensionId/")
}
$utf8 = New-Object System.Text.UTF8Encoding($false)
[IO.File]::WriteAllText($manifestPath, ($manifest | ConvertTo-Json -Depth 4), $utf8)

foreach ($registryPath in $registryPaths) {
    New-Item -Path $registryPath -Force | Out-Null
    Set-Item -LiteralPath $registryPath -Value $manifestPath
}

Write-Output "Installed $name for Chrome and Edge."
