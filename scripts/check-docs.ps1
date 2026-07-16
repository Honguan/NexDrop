$ErrorActionPreference = 'Stop'
$repo = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$version = (Get-Content -Raw (Join-Path $repo 'VERSION')).Trim()

if ((Get-Content -Raw (Join-Path $repo 'web/package.json') | ConvertFrom-Json).version -ne $version) { throw 'web/package.json version mismatch' }
if ((Get-Content -Raw (Join-Path $repo 'extension/package.json') | ConvertFrom-Json).version -ne $version) { throw 'extension/package.json version mismatch' }
if (-not (Select-String -Quiet -Path (Join-Path $repo 'client/pubspec.yaml') -Pattern "^version: $([regex]::Escape($version))\+")) { throw 'client/pubspec.yaml version mismatch' }

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
