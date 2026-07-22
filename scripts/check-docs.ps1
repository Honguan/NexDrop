$ErrorActionPreference = 'Stop'
$repo = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$version = (Get-Content -Raw (Join-Path $repo 'VERSION')).Trim()
$goVersion = ([regex]::Match((Get-Content -Raw (Join-Path $repo 'go.mod')), '(?m)^go\s+([^\s]+)\r?$')).Groups[1].Value

if ((Get-Content -Raw (Join-Path $repo 'web/package.json') | ConvertFrom-Json).version -ne $version) { throw 'web/package.json version mismatch' }
if ((Get-Content -Raw (Join-Path $repo 'extension/package.json') | ConvertFrom-Json).version -ne $version) { throw 'extension/package.json version mismatch' }
foreach ($manifest in @('extension/manifest.json', 'extension/manifests/chrome.json', 'extension/manifests/edge.json')) {
    if ((Get-Content -Raw -Encoding UTF8 (Join-Path $repo $manifest) | ConvertFrom-Json).version -ne $version) { throw "$manifest version mismatch" }
}
if (-not (Select-String -Quiet -Path (Join-Path $repo 'client/pubspec.yaml') -Pattern "^version: $([regex]::Escape($version))\+")) { throw 'client/pubspec.yaml version mismatch' }
if (-not $goVersion -or -not (Select-String -Quiet -Path (Join-Path $repo 'cmd/README.md') -SimpleMatch "Go $goVersion+")) { throw 'cmd/README.md Go version mismatch' }
$escapedVersion = [regex]::Escape($version)
if (-not (Select-String -Quiet -Path (Join-Path $repo 'CHANGELOG.md') -Pattern "^## \[$escapedVersion\]")) { throw 'CHANGELOG.md version mismatch' }
$releaseNotes = Join-Path $repo "docs/release-notes-v$version.md"
if (-not (Test-Path -LiteralPath $releaseNotes -PathType Leaf)) { throw "missing release notes for $version" }
if (-not (Select-String -Quiet -Path $releaseNotes -Pattern "^# NexDrop $escapedVersion$")) { throw 'release notes version mismatch' }

$releaseWorkflow = Join-Path $repo '.github/workflows/release.yml'
if (-not (Select-String -Quiet -Path $releaseWorkflow -SimpleMatch "tags: ['v*']")) { throw 'release workflow must support version tags' }
if (-not (Select-String -Quiet -Path $releaseWorkflow -SimpleMatch 'gh release create')) { throw 'release workflow must create a GitHub release' }

$problems = @()
Get-ChildItem -LiteralPath $repo -Recurse -Filter '*.md' -File |
    Where-Object { $_.FullName -notmatch '[\\/](node_modules|build|dist)[\\/]' } |
    ForEach-Object {
        $document = $_
        $content = Get-Content -Raw -Encoding UTF8 $document.FullName
        $insideFence = $false
        $lineNumber = 0
        foreach ($line in ($content -split "\r?\n")) {
            $lineNumber++
            if ($line -match '^\s*```') {
                $insideFence = -not $insideFence
                continue
            }
            if (-not $insideFence -and $line -match '[ \t]+$') { $problems += "$($document.FullName):$lineNumber trailing whitespace" }
            if (-not $insideFence -and $line -match '^#{1,6}[^ #]') { $problems += "$($document.FullName):$lineNumber malformed heading" }
        }
        if ($insideFence) { $problems += "$($document.FullName): unclosed code fence" }
        foreach ($match in [regex]::Matches($content, '!?(?<!\!)\[[^\]]*\]\(([^)]+)\)')) {
            $target = $match.Groups[1].Value.Trim().Split('#')[0]
            if (-not $target -or $target -match '^(https?|mailto):') { continue }
            $target = [Uri]::UnescapeDataString($target.Trim('<', '>'))
            $resolved = [IO.Path]::GetFullPath((Join-Path $document.DirectoryName $target))
            if (-not (Test-Path -LiteralPath $resolved)) { $problems += "$($document.FullName): missing link $target" }
        }
    }

if ($problems.Count) { throw "Documentation validation failed:`n$($problems -join "`n")" }
& (Join-Path $repo 'scripts/check-automation.ps1')
if ($LASTEXITCODE -ne 0) { throw 'Automation validation failed' }
Write-Output "Documentation links and version $version are valid."
