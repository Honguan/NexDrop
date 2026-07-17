$ErrorActionPreference = 'Stop'
$repo = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$version = (Get-Content -Raw (Join-Path $repo 'VERSION')).Trim()
$goVersion = ([regex]::Match((Get-Content -Raw (Join-Path $repo 'go.mod')), '(?m)^go\s+([^\s]+)$')).Groups[1].Value

if ((Get-Content -Raw (Join-Path $repo 'web/package.json') | ConvertFrom-Json).version -ne $version) { throw 'web/package.json version mismatch' }
if ((Get-Content -Raw (Join-Path $repo 'extension/package.json') | ConvertFrom-Json).version -ne $version) { throw 'extension/package.json version mismatch' }
if (-not (Select-String -Quiet -Path (Join-Path $repo 'client/pubspec.yaml') -Pattern "^version: $([regex]::Escape($version))\+")) { throw 'client/pubspec.yaml version mismatch' }
if (-not $goVersion -or -not (Select-String -Quiet -Path (Join-Path $repo 'cmd/README.md') -SimpleMatch "Go $goVersion+")) { throw 'cmd/README.md Go version mismatch' }

$releaseWorkflow = Join-Path $repo '.github/workflows/release.yml'
$releaseDocumentation = Join-Path $repo 'docs/release-process.md'
foreach ($secret in @(
    'ANDROID_KEYSTORE_BASE64',
    'ANDROID_STORE_PASSWORD',
    'ANDROID_KEY_ALIAS',
    'ANDROID_KEY_PASSWORD',
    'WINDOWS_CERTIFICATE_BASE64',
    'WINDOWS_CERTIFICATE_PASSWORD'
)) {
    if (-not (Select-String -Quiet -Path $releaseWorkflow -SimpleMatch $secret)) { throw "release workflow missing $secret" }
    if (-not (Select-String -Quiet -Path $releaseDocumentation -SimpleMatch $secret)) { throw "release documentation missing $secret" }
}

$missing = @()
Get-ChildItem -LiteralPath $repo -Recurse -Filter '*.md' -File |
    Where-Object { $_.FullName -notmatch '[\\/](node_modules|build|dist)[\\/]' } |
    ForEach-Object {
        $document = $_
        $content = Get-Content -Raw -Encoding UTF8 $document.FullName
        foreach ($match in [regex]::Matches($content, '!?(?<!\!)\[[^\]]*\]\(([^)]+)\)')) {
            $target = $match.Groups[1].Value.Trim().Split('#')[0]
            if (-not $target -or $target -match '^(https?|mailto):') { continue }
            $target = [Uri]::UnescapeDataString($target.Trim('<', '>'))
            $resolved = [IO.Path]::GetFullPath((Join-Path $document.DirectoryName $target))
            if (-not (Test-Path -LiteralPath $resolved)) { $missing += "$($document.FullName): $target" }
        }
    }

if ($missing.Count) { throw "Missing documentation links:`n$($missing -join "`n")" }
Write-Output "Documentation links and version $version are valid."
