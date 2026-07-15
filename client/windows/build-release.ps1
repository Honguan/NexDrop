param(
    [string]$Flutter = 'flutter'
)

$ErrorActionPreference = 'Stop'
$repo = (Resolve-Path (Join-Path $PSScriptRoot '..\..')).Path
$client = Join-Path $repo 'client'

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

Write-Output "Release created at $release"
