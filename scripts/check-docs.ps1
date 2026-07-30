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
if (-not (Select-String -Quiet -Path (Join-Path $repo 'CHANGELOG.zh-TW.md') -Pattern "^## \[$escapedVersion\]")) { throw 'CHANGELOG.zh-TW.md version mismatch' }
$releaseNotes = Join-Path $repo "docs/release-notes-v$version.md"
$releaseNotesZhTW = Join-Path $repo "docs/release-notes-v$version.zh-TW.md"
if (-not (Test-Path -LiteralPath $releaseNotes -PathType Leaf)) { throw "missing release notes for $version" }
if (-not (Test-Path -LiteralPath $releaseNotesZhTW -PathType Leaf)) { throw "missing Traditional Chinese release notes for $version" }
if (-not (Select-String -Quiet -Path $releaseNotes -Pattern "^# NexDrop $escapedVersion$")) { throw 'release notes version mismatch' }
if (-not (Select-String -Quiet -Path $releaseNotesZhTW -Pattern "^# NexDrop $escapedVersion$")) { throw 'Traditional Chinese release notes version mismatch' }

$releaseWorkflow = Join-Path $repo '.github/workflows/release.yml'
if (-not (Select-String -Quiet -Path $releaseWorkflow -SimpleMatch "tags: ['v*']")) { throw 'release workflow must support version tags' }
if (-not (Select-String -Quiet -Path $releaseWorkflow -SimpleMatch 'gh release create')) { throw 'release workflow must create a GitHub release' }

$bilingualDocs = @(
    @('README.md', 'README.zh-TW.md'),
    @('CHANGELOG.md', 'CHANGELOG.zh-TW.md'),
    @('SECURITY.md', 'SECURITY.zh-TW.md'),
    @('CONTRIBUTING.md', 'CONTRIBUTING.zh-TW.md'),
    @('CODE_OF_CONDUCT.md', 'CODE_OF_CONDUCT.zh-TW.md'),
    @('docs/README.md', 'docs/README.zh-TW.md'),
    @('cmd/README.md', 'cmd/README.zh-TW.md'),
    @('web/README.md', 'web/README.zh-TW.md'),
    @('extension/README.md', 'extension/README.zh-TW.md'),
    @('client/README.md', 'client/README.zh-TW.md'),
    @('client/android/README.md', 'client/android/README.zh-TW.md'),
    @('client/windows/README.md', 'client/windows/README.zh-TW.md'),
    @('deploy/README.md', 'deploy/README.zh-TW.md'),
    @('docs/architecture.md', 'docs/architecture.zh-TW.md'),
    @('docs/deployment.md', 'docs/deployment.zh-TW.md'),
    @('docs/configuration.md', 'docs/configuration.zh-TW.md'),
    @('docs/security.md', 'docs/security.zh-TW.md'),
    @('docs/api.md', 'docs/api.zh-TW.md'),
    @('docs/compatibility-matrix.md', 'docs/compatibility-matrix.zh-TW.md'),
    @('docs/data-model.md', 'docs/data-model.zh-TW.md'),
    @('docs/performance.md', 'docs/performance.zh-TW.md'),
    @('docs/troubleshooting.md', 'docs/troubleshooting.zh-TW.md'),
    @('docs/operations/slo.md', 'docs/operations/slo.zh-TW.md'),
    @('docs/operations/diagnostics.md', 'docs/operations/diagnostics.zh-TW.md'),
    @('docs/operations/diagnostics-privacy.md', 'docs/operations/diagnostics-privacy.zh-TW.md'),
    @('docs/operations/transfer-events.md', 'docs/operations/transfer-events.zh-TW.md'),
    @('docs/testing/failure-injection.md', 'docs/testing/failure-injection.zh-TW.md'),
    @('docs/release-process.md', 'docs/release-process.zh-TW.md'),
    @('docs/release-readiness-v1.0.0.md', 'docs/release-readiness-v1.0.0.zh-TW.md'),
    @('docs/adr/README.md', 'docs/adr/README.zh-TW.md'),
    @('docs/adr/001-modular-monolith.md', 'docs/adr/001-modular-monolith.zh-TW.md'),
    @('docs/adr/002-hybrid-transfer.md', 'docs/adr/002-hybrid-transfer.zh-TW.md'),
    @('docs/adr/003-client-encryption.md', 'docs/adr/003-client-encryption.zh-TW.md'),
    @('docs/adr/004-desktop-bridge.md', 'docs/adr/004-desktop-bridge.zh-TW.md'),
    @('docs/adr/005-independent-extension.md', 'docs/adr/005-independent-extension.zh-TW.md'),
    @('docs/adr/006-observability-and-diagnostics.md', 'docs/adr/006-observability-and-diagnostics.zh-TW.md'),
    @('docs/protocols/lan-discovery.md', 'docs/protocols/lan-discovery.zh-TW.md'),
    @('docs/protocols/lan-transfer.md', 'docs/protocols/lan-transfer.zh-TW.md'),
    @('docs/protocols/node-transfer.md', 'docs/protocols/node-transfer.zh-TW.md'),
    @('docs/protocols/capability-negotiation.md', 'docs/protocols/capability-negotiation.zh-TW.md')
)
foreach ($pair in $bilingualDocs) {
    $englishPath = Join-Path $repo $pair[0]
    $chinesePath = Join-Path $repo $pair[1]
    if (-not (Test-Path -LiteralPath $chinesePath -PathType Leaf)) { throw "missing Traditional Chinese document: $($pair[1])" }
    $english = Get-Content -Raw -Encoding UTF8 $englishPath
    $chinese = Get-Content -Raw -Encoding UTF8 $chinesePath
    $englishName = Split-Path -Leaf $pair[0]
    $chineseName = Split-Path -Leaf $pair[1]
    if (-not $english.Contains("($chineseName)")) { throw "$($pair[0]) must link to Traditional Chinese" }
    if (-not $chinese.Contains("($englishName)")) { throw "$($pair[1]) must link to English" }
    $languageSwitch = '(?m)^\[[^\]]+\]\(' + [regex]::Escape($chineseName) + '\)\r?\n?'
    $englishBody = $english -replace $languageSwitch, ''
    if ($englishBody -match '[\u3400-\u9fff]') { throw "$($pair[0]) must remain English-first" }
}

$releaseNotesTemplate = Get-Content -Raw -Encoding UTF8 (Join-Path $repo 'docs/release-notes-template.md')
$releaseNotesTemplateZhTW = Get-Content -Raw -Encoding UTF8 (Join-Path $repo 'docs/release-notes-template.zh-TW.md')
if (-not $releaseNotesTemplate.Contains('(release-notes-v{{VERSION}}.zh-TW.md)')) { throw 'English release notes template must link to its Traditional Chinese edition' }
if (-not $releaseNotesTemplateZhTW.Contains('(release-notes-v{{VERSION}}.md)')) { throw 'Traditional Chinese release notes template must link to its English edition' }

$compatibilitySource = Get-Content -Raw -Encoding UTF8 (Join-Path $repo 'internal/version/compatibility.go')
$currentProtocol = ([regex]::Match($compatibilitySource, '(?m)^\s*CurrentProtocol\s*=\s*"([^"]+)"')).Groups[1].Value
$compatibilityMatrix = Get-Content -Raw -Encoding UTF8 (Join-Path $repo 'docs/compatibility-matrix.md')
$compatibilityMatrixZh = Get-Content -Raw -Encoding UTF8 (Join-Path $repo 'docs/compatibility-matrix.zh-TW.md')
if (-not $currentProtocol -or -not $compatibilityMatrix.Contains("Current protocol: ``$currentProtocol``")) {
    throw 'compatibility matrix does not document the current protocol'
}
$capabilityDocument = Get-Content -Raw -Encoding UTF8 (Join-Path $repo 'docs/protocols/capability-negotiation.md')
$contractSources = @(
    $compatibilitySource,
    (Get-Content -Raw -Encoding UTF8 (Join-Path $repo 'internal/presence/hub.go')),
    (Get-Content -Raw -Encoding UTF8 (Join-Path $repo 'internal/lan/discovery.go')),
    (Get-Content -Raw -Encoding UTF8 (Join-Path $repo 'internal/lan/transfer.go'))
)
$contractText = ($contractSources -join "`n---`n") -replace "`r`n", "`n"
$contractBytes = [Text.Encoding]::UTF8.GetBytes($contractText.Trim())
$contractHash = ([BitConverter]::ToString([Security.Cryptography.SHA256]::Create().ComputeHash($contractBytes))).Replace('-', '').ToLowerInvariant()
if (-not $compatibilityMatrix.Contains("Compatibility contract fingerprint: ``$contractHash``")) {
    throw 'compatibility matrix fingerprint is stale for the protocol contract'
}
if (-not $compatibilityMatrixZh.Contains($contractHash)) {
    throw 'Traditional Chinese compatibility matrix fingerprint is stale for the protocol contract'
}
$constantValues = @{}
foreach ($match in [regex]::Matches($compatibilitySource, '(?m)^\s*([A-Za-z][A-Za-z0-9]+)\s*=\s*"([^"]+)"')) {
    $constantValues[$match.Groups[1].Value] = $match.Groups[2].Value
}
foreach ($match in [regex]::Matches($compatibilitySource, '\{ID:\s*([A-Za-z][A-Za-z0-9]+),')) {
    $identifier = $constantValues[$match.Groups[1].Value]
    if (-not $identifier -or -not $capabilityDocument.Contains("``$identifier``")) {
        throw "capability document is missing registry identifier $($match.Groups[1].Value)"
    }
    foreach ($clientPath in @('client/lib/core/api_client.dart', 'web/src/api.ts', 'extension/src/direct.ts')) {
        $clientSource = Get-Content -Raw -Encoding UTF8 (Join-Path $repo $clientPath)
        if (-not $clientSource.Contains($identifier)) {
            throw "$clientPath is missing registry capability $identifier"
        }
    }
}

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
            if ($target -match '\{\{[^}]+\}\}') { continue }
            $target = [Uri]::UnescapeDataString($target.Trim('<', '>'))
            $resolved = [IO.Path]::GetFullPath((Join-Path $document.DirectoryName $target))
            if (-not (Test-Path -LiteralPath $resolved)) { $problems += "$($document.FullName): missing link $target" }
        }
    }

if ($problems.Count) { throw "Documentation validation failed:`n$($problems -join "`n")" }
& (Join-Path $repo 'scripts/check-automation.ps1')
if ($LASTEXITCODE -ne 0) { throw 'Automation validation failed' }
Write-Output "Documentation links and version $version are valid."
