param(
    [string]$Version = '1.0.0',
    [string]$OutputDirectory = '..\dist'
)

$ErrorActionPreference = 'Stop'
$extension = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$output = [IO.Path]::GetFullPath((Join-Path $extension $OutputDirectory))
New-Item -ItemType Directory -Force -Path $output | Out-Null

Push-Location $extension
try {
    npm run build
    if ($LASTEXITCODE -ne 0) { throw 'Extension build failed' }
    foreach ($browser in @('Chrome', 'Edge')) {
        $archive = Join-Path $output "NexDrop-Extension-${browser}_${Version}.zip"
        Compress-Archive -Path (Join-Path $extension 'dist\*') -DestinationPath $archive -Force
    }
} finally {
    Pop-Location
}
