param(
    [ValidateSet('patch', 'minor', 'major')]
    [string]$Bump = 'patch',
    [string]$Version = '',
    [string]$Summary = 'Maintenance and dependency updates',
    [string]$SummaryZhTW = 'Maintenance and dependency updates',
    [string]$RepositoryRoot = (Split-Path -Parent $PSScriptRoot),
    [switch]$CheckOnly
)

$ErrorActionPreference = 'Stop'
$RepositoryRoot = [IO.Path]::GetFullPath($RepositoryRoot)
$utf8 = [Text.UTF8Encoding]::new($false)

function Read-RepositoryText([string]$RelativePath) {
    $path = Join-Path $RepositoryRoot $RelativePath
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
        throw "Missing release source: $RelativePath"
    }
    return [IO.File]::ReadAllText($path)
}

function Write-RepositoryText([string]$RelativePath, [string]$Content) {
    $path = Join-Path $RepositoryRoot $RelativePath
    [IO.File]::WriteAllText($path, $Content, $utf8)
}

function Assert-Contains([string]$RelativePath, [string]$Expected) {
    if (-not (Read-RepositoryText $RelativePath).Contains($Expected)) {
        throw "$RelativePath does not contain expected value: $Expected"
    }
}

function Assert-LiteralCount([string]$RelativePath, [string]$Expected, [int]$Count) {
    $actual = [regex]::Matches((Read-RepositoryText $RelativePath), [regex]::Escape($Expected)).Count
    if ($actual -ne $Count) {
        throw "$RelativePath contains $actual matching version fields; expected $Count"
    }
}

$currentVersion = (Read-RepositoryText 'VERSION').Trim()
$currentMatch = [regex]::Match($currentVersion, '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$')
if (-not $currentMatch.Success) {
    throw "VERSION must be stable SemVer: $currentVersion"
}

$major = [int]$currentMatch.Groups[1].Value
$minor = [int]$currentMatch.Groups[2].Value
$patch = [int]$currentMatch.Groups[3].Value

if ([string]::IsNullOrWhiteSpace($Version)) {
    switch ($Bump) {
        'major' { $major++; $minor = 0; $patch = 0 }
        'minor' { $minor++; $patch = 0 }
        'patch' { $patch++ }
    }
    $nextVersion = "$major.$minor.$patch"
} else {
    $nextVersion = $Version.Trim()
    if ($nextVersion -notmatch '^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$') {
        throw "Version must be stable SemVer: $nextVersion"
    }
    if ([version]$nextVersion -le [version]$currentVersion) {
        throw "Version must be newer than ${currentVersion}: $nextVersion"
    }
}

$nextMatch = [regex]::Match($nextVersion, '^([0-9]+)\.([0-9]+)\.([0-9]+)$')
$nextMajor = [int]$nextMatch.Groups[1].Value
$nextMinor = [int]$nextMatch.Groups[2].Value
$nextPatch = [int]$nextMatch.Groups[3].Value

$versionFiles = @(
    '.env.example',
    'Dockerfile',
    'README.md',
    'compose.yaml',
    'deploy/README.md',
    'docs/configuration.md',
    'scripts/build-node-release.ps1',
    'internal/version/compatibility.go',
    'internal/version/compatibility_test.go',
    'extension/scripts/package-release.ps1',
    'client/lib/core/lan_service.dart',
    'client/windows/build-release.ps1',
    'client/windows/installer.iss'
)

foreach ($relativePath in $versionFiles) {
    Assert-Contains $relativePath $currentVersion
}

$jsonVersionFiles = @(
    'web/package.json',
    'extension/package.json',
    'extension/manifest.json',
    'extension/manifests/chrome.json',
    'extension/manifests/edge.json'
)
$lockVersionFiles = @('web/package-lock.json', 'extension/package-lock.json')
$jsonVersionField = "  `"version`": `"$currentVersion`""
foreach ($relativePath in $jsonVersionFiles) {
    Assert-LiteralCount $relativePath $jsonVersionField 1
}
foreach ($relativePath in $lockVersionFiles) {
    Assert-LiteralCount $relativePath $jsonVersionField 2
}

$pubspec = Read-RepositoryText 'client/pubspec.yaml'
$pubspecMatch = [regex]::Match($pubspec, "(?m)^version: $([regex]::Escape($currentVersion))\+([0-9]+)\r?$")
if (-not $pubspecMatch.Success) {
    throw 'client/pubspec.yaml version is not synchronized with VERSION'
}
$nextBuild = [int]$pubspecMatch.Groups[1].Value + 1

$runnerResource = Read-RepositoryText 'client/windows/runner/Runner.rc'
$currentNumber = "$($currentMatch.Groups[1].Value),$($currentMatch.Groups[2].Value),$($currentMatch.Groups[3].Value),$($pubspecMatch.Groups[1].Value)"
Assert-Contains 'client/windows/runner/Runner.rc' $currentNumber
Assert-Contains 'client/windows/runner/Runner.rc' "`"$currentVersion`""
Assert-Contains 'CHANGELOG.md' '## [Unreleased]'
Assert-Contains 'CHANGELOG.zh-TW.md' '## [Unreleased]'
Assert-Contains 'docs/release-notes-template.md' '{{VERSION}}'
Assert-Contains 'docs/release-notes-template.md' '{{SUMMARY}}'
Assert-Contains 'docs/release-notes-template.zh-TW.md' '{{VERSION}}'
Assert-Contains 'docs/release-notes-template.zh-TW.md' '{{SUMMARY}}'

$releaseNotesPath = "docs/release-notes-v$nextVersion.md"
$releaseNotesZhTWPath = "docs/release-notes-v$nextVersion.zh-TW.md"
foreach ($path in @($releaseNotesPath, $releaseNotesZhTWPath)) {
    if (Test-Path -LiteralPath (Join-Path $RepositoryRoot $path)) {
        throw "Release notes already exist: $path"
    }
}

if ($CheckOnly) {
    Write-Output "Release metadata is synchronized at $currentVersion; next $Bump version is $nextVersion."
    exit 0
}

$summaryLine = ($Summary.Trim() -replace '[\r\n]+', ' ')
if ([string]::IsNullOrWhiteSpace($summaryLine)) {
    throw 'Summary must not be empty'
}
$summaryZhTWLine = ($SummaryZhTW.Trim() -replace '[\r\n]+', ' ')
if ([string]::IsNullOrWhiteSpace($summaryZhTWLine)) {
    throw 'Traditional Chinese summary must not be empty'
}

foreach ($relativePath in $versionFiles) {
    $content = Read-RepositoryText $relativePath
    Write-RepositoryText $relativePath ($content.Replace($currentVersion, $nextVersion))
}
foreach ($relativePath in ($jsonVersionFiles + $lockVersionFiles)) {
    $content = Read-RepositoryText $relativePath
    Write-RepositoryText $relativePath ($content.Replace($jsonVersionField, "  `"version`": `"$nextVersion`""))
}

$pubspec = $pubspec.Replace("version: $currentVersion+$($pubspecMatch.Groups[1].Value)", "version: $nextVersion+$nextBuild")
Write-RepositoryText 'client/pubspec.yaml' $pubspec

$nextNumber = "$nextMajor,$nextMinor,$nextPatch,$nextBuild"
$runnerResource = $runnerResource.Replace($currentNumber, $nextNumber).Replace("`"$currentVersion`"", "`"$nextVersion`"")
Write-RepositoryText 'client/windows/runner/Runner.rc' $runnerResource
Write-RepositoryText 'VERSION' "$nextVersion`n"

$date = [DateTime]::UtcNow.ToString('yyyy-MM-dd')
$changelog = Read-RepositoryText 'CHANGELOG.md'
$changelogSection = "## [Unreleased]`n`n## [$nextVersion] - $date`n`n### Changed`n`n- $summaryLine"
$changelog = $changelog.Replace('## [Unreleased]', $changelogSection)
Write-RepositoryText 'CHANGELOG.md' $changelog

$changelogZhTW = Read-RepositoryText 'CHANGELOG.zh-TW.md'
$changelogZhTWSection = "## [Unreleased]`n`n## [$nextVersion] - $date`n`n### Changed`n`n- $summaryZhTWLine"
$changelogZhTW = $changelogZhTW.Replace('## [Unreleased]', $changelogZhTWSection)
Write-RepositoryText 'CHANGELOG.zh-TW.md' $changelogZhTW

$releaseNotes = (Read-RepositoryText 'docs/release-notes-template.md').Replace('{{VERSION}}', $nextVersion).Replace('{{SUMMARY}}', $summaryLine)
if ($releaseNotes.Contains('{{VERSION}}') -or $releaseNotes.Contains('{{SUMMARY}}')) {
    throw 'Release notes template contains unresolved placeholders'
}
Write-RepositoryText $releaseNotesPath ($releaseNotes + "`n")

$releaseNotesZhTW = (Read-RepositoryText 'docs/release-notes-template.zh-TW.md').Replace('{{VERSION}}', $nextVersion).Replace('{{SUMMARY}}', $summaryZhTWLine)
if ($releaseNotesZhTW.Contains('{{VERSION}}') -or $releaseNotesZhTW.Contains('{{SUMMARY}}')) {
    throw 'Traditional Chinese release notes template contains unresolved placeholders'
}
Write-RepositoryText $releaseNotesZhTWPath ($releaseNotesZhTW + "`n")

Write-Output "Prepared NexDrop $nextVersion (build $nextBuild)."
