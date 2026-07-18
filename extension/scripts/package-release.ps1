param(
    [string]$Version = '1.0.2',
    [string]$OutputDirectory = '..\dist'
)

$ErrorActionPreference = 'Stop'
if ($Version -cnotmatch '^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z]+(?:[.-][0-9A-Za-z]+)*)?$') { throw "Invalid release version: $Version" }
$extension = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$output = [IO.Path]::GetFullPath((Join-Path $extension $OutputDirectory))
New-Item -ItemType Directory -Force -Path $output | Out-Null

function Get-OutputChild([string]$Base, [string]$Name) {
    $basePath = [IO.Path]::GetFullPath($Base).TrimEnd([IO.Path]::DirectorySeparatorChar, [IO.Path]::AltDirectorySeparatorChar)
    $candidate = [IO.Path]::GetFullPath((Join-Path $basePath $Name))
    if (-not $candidate.StartsWith($basePath + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) {
        throw "Release path escaped the output directory: $candidate"
    }
    return $candidate
}

function Test-ExtensionPackage([string]$Root, [string]$Browser, [string]$ExpectedVersion) {
    $expectedPermissions = @('activeTab', 'contextMenus', 'storage') | Sort-Object
    $expectedOptionalHosts = @('https://*/*', 'http://localhost/*', 'http://127.0.0.1/*') | Sort-Object
    $manifestPath = Join-Path $Root 'manifest.json'
    $manifest = Get-Content -LiteralPath $manifestPath -Raw -Encoding UTF8 | ConvertFrom-Json
    if ($manifest.manifest_version -ne 3) { throw "$Browser package must use Manifest V3" }
    if ($manifest.version -cne $ExpectedVersion) { throw "$Browser package version does not match $ExpectedVersion" }
    $permissions = @($manifest.permissions | Sort-Object)
    if (($permissions -join "`n") -cne ($expectedPermissions -join "`n")) { throw "$Browser package permissions are not approved" }
    if ($null -ne $manifest.host_permissions -and @($manifest.host_permissions).Count -ne 0) { throw "$Browser package must not request permanent host permissions" }
    $optionalHosts = @($manifest.optional_host_permissions | Sort-Object)
    if (($optionalHosts -join "`n") -cne ($expectedOptionalHosts -join "`n")) { throw "$Browser package optional host permissions are not approved" }
    foreach ($field in @('key', 'oauth2', 'update_url')) {
        if ($manifest.PSObject.Properties.Name -contains $field) { throw "$Browser package manifest contains forbidden field: $field" }
    }
    $packageFiles = Get-ChildItem -LiteralPath $Root -File -Recurse
    $sensitive = Select-String -LiteralPath $packageFiles.FullName -Encoding UTF8 -Pattern '-----BEGIN [A-Z ]*PRIVATE KEY-----|(?i)(client_secret|api_key)\s*[:=]'
    if ($sensitive) { throw "$Browser package contains a potential secret" }
}

Push-Location $extension
try {
    npm run build
    if ($LASTEXITCODE -ne 0) { throw 'Extension build failed' }
    foreach ($browser in @('Chrome', 'Edge')) {
        $stage = Get-OutputChild $output (".stage-$($browser.ToLowerInvariant())-" + [guid]::NewGuid().ToString('N'))
        $verification = Get-OutputChild $output (".verify-$($browser.ToLowerInvariant())-" + [guid]::NewGuid().ToString('N'))
        $temporaryArchive = Get-OutputChild $output (".archive-$($browser.ToLowerInvariant())-" + [guid]::NewGuid().ToString('N') + '.zip')
        $archive = Get-OutputChild $output "NexDrop-Extension-${browser}_${Version}.zip"
        try {
            Copy-Item -Recurse (Join-Path $extension 'dist') $stage
            $browserManifest = Join-Path $extension "manifests/$($browser.ToLowerInvariant()).json"
            Copy-Item -LiteralPath $browserManifest -Destination (Join-Path $stage 'manifest.json') -Force
            Test-ExtensionPackage $stage $browser $Version
            Compress-Archive -Path (Join-Path $stage '*') -DestinationPath $temporaryArchive
            Expand-Archive -LiteralPath $temporaryArchive -DestinationPath $verification
            Test-ExtensionPackage $verification $browser $Version
            Move-Item -LiteralPath $temporaryArchive -Destination $archive -Force
        } finally {
            if (Test-Path -LiteralPath $stage) { Remove-Item -LiteralPath $stage -Recurse -Force }
            if (Test-Path -LiteralPath $verification) { Remove-Item -LiteralPath $verification -Recurse -Force }
            if (Test-Path -LiteralPath $temporaryArchive) { Remove-Item -LiteralPath $temporaryArchive -Force }
        }
    }
} finally {
    Pop-Location
}
