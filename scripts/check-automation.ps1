$ErrorActionPreference = 'Stop'
$repo = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path

function Read-RepoFile([string]$RelativePath) {
    $path = Join-Path $repo $RelativePath
    if (-not (Test-Path -LiteralPath $path -PathType Leaf)) {
        throw "Missing automation file: $RelativePath"
    }
    return [IO.File]::ReadAllText($path)
}

function Assert-MatchCount([string]$Content, [string]$Pattern, [int]$Expected, [string]$Message) {
    $actual = [regex]::Matches($Content, $Pattern).Count
    if ($actual -ne $Expected) {
        throw "$Message (expected $Expected, found $actual)"
    }
}

$dependabot = Read-RepoFile '.github/dependabot.yml'
Assert-MatchCount $dependabot '(?m)^multi-ecosystem-groups:\s*$' 1 'Dependabot must define one multi-ecosystem group section'
Assert-MatchCount $dependabot '(?m)^\s{4}multi-ecosystem-group: nexdrop-dependencies\s*$' 5 'Every package ecosystem must use the shared dependency PR'
Assert-MatchCount $dependabot '(?m)^\s{4}patterns: \["\*"\]\s*$' 5 'Every package ecosystem must include all version updates'
Assert-MatchCount $dependabot '(?m)^\s{4}open-pull-requests-limit: 1\s*$' 5 'Each ecosystem must limit open version update PRs'
Assert-MatchCount $dependabot '(?m)^\s{4}rebase-strategy: auto\s*$' 5 'Each ecosystem must rebase generated updates'
foreach ($ecosystem in @('gomod', 'pub', 'github-actions')) {
    if (-not $dependabot.Contains("package-ecosystem: $ecosystem")) {
        throw "Dependabot ecosystem is missing: $ecosystem"
    }
}
Assert-MatchCount $dependabot '(?m)^\s{2}- package-ecosystem: npm\s*$' 2 'Dependabot must update both npm workspaces'

$autoMerge = Read-RepoFile '.github/workflows/auto-merge-generated-pr.yml'
if ($autoMerge.Contains('check_suite')) { throw 'Generated PR auto-merge must not merge from partial check_suite events' }
foreach ($required in @('pull_request_target:', 'auto-merge', '--auto --squash --delete-branch')) {
    if (-not $autoMerge.Contains($required)) { throw "Generated PR auto-merge is missing: $required" }
}

$releaseGuard = Read-RepoFile '.github/workflows/functional-release-guard.yml'
if ($releaseGuard.Contains('|scripts|')) { throw 'Release guard must not treat every maintenance script as a product change' }
foreach ($required in @('scripts/(build-node-release', 'sign-android-release')) {
    if (-not $releaseGuard.Contains($required)) { throw "Release guard is missing product packaging script: $required" }
}

$requiredWorkflows = @(
    'server-ci.yml',
    'web-ci.yml',
    'extension-ci.yml',
    'flutter-ci.yml',
    'integration-test.yml',
    'security.yml',
    'docs.yml',
    'docker-build.yml'
)
foreach ($workflow in $requiredWorkflows) {
    $content = Read-RepoFile ".github/workflows/$workflow"
    if (-not $content.Contains('workflow_dispatch:')) {
        throw "$workflow must support release PR dispatch"
    }
}

$releasePackage = Read-RepoFile '.github/workflows/release-package.yml'
foreach ($required in @(
    'scripts/prepare-release.ps1',
    'gh pr update-branch',
    'gh pr merge "$PR" --auto --squash --delete-branch',
    'git tag -a "$TAG"',
    'gh workflow run release.yml --ref "$TAG"',
    'gh run watch "$run_id" --exit-status'
)) {
    if (-not $releasePackage.Contains($required)) {
        throw "One-click release workflow is missing: $required"
    }
}
foreach ($workflow in $requiredWorkflows) {
    if (-not $releasePackage.Contains($workflow)) {
        throw "One-click release workflow does not dispatch: $workflow"
    }
}

& (Join-Path $repo 'scripts/prepare-release.ps1') -CheckOnly
if ($LASTEXITCODE -ne 0) { throw 'Release metadata preflight failed' }

Write-Output 'Dependency and release automation are valid.'
