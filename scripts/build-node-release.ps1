param(
	[string]$Version = '1.0.1',
	[string]$Commit = 'development',
	[string]$OutputDirectory = 'dist'
)

$ErrorActionPreference = 'Stop'
if ($Version -cnotmatch '^[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z]+(?:[.-][0-9A-Za-z]+)*)?$') { throw "Invalid release version: $Version" }
$repo = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$output = [IO.Path]::GetFullPath((Join-Path $repo $OutputDirectory))
New-Item -ItemType Directory -Force -Path $output | Out-Null

function Get-OutputChild([string]$Base, [string]$Name) {
	$basePath = [IO.Path]::GetFullPath($Base).TrimEnd([IO.Path]::DirectorySeparatorChar, [IO.Path]::AltDirectorySeparatorChar)
	$candidate = [IO.Path]::GetFullPath((Join-Path $basePath $Name))
	if (-not $candidate.StartsWith($basePath + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) {
		throw "Release path escaped the output directory: $candidate"
	}
	return $candidate
}

$previousCGOEnabled = $env:CGO_ENABLED
$previousGOOS = $env:GOOS
$previousGOARCH = $env:GOARCH
try {
	foreach ($architecture in @('amd64', 'arm64')) {
		$packageName = "nexdrop-node_${Version}_linux_${architecture}"
		$work = Get-OutputChild $output (".build-node-" + [guid]::NewGuid().ToString('N'))
		$stage = Join-Path $work $packageName
		$verification = Get-OutputChild $output (".verify-node-" + [guid]::NewGuid().ToString('N'))
		$temporaryArchive = Get-OutputChild $output (".archive-node-" + [guid]::NewGuid().ToString('N') + '.tar.gz')
		$archive = Get-OutputChild $output "$packageName.tar.gz"
		try {
			New-Item -ItemType Directory -Force -Path $stage,$verification | Out-Null
			$env:CGO_ENABLED = '0'
			$env:GOOS = 'linux'
			$env:GOARCH = $architecture
			go build -trimpath -ldflags "-s -w -X nexdrop/internal/version.ProductVersion=$Version -X nexdrop/internal/version.BuildCommit=$Commit" -o (Join-Path $stage 'nexdrop') ./cmd/nexdrop
			if ($LASTEXITCODE -ne 0) { throw "Node $architecture build failed" }
			Copy-Item -Recurse migrations (Join-Path $stage 'migrations')
			tar -czf $temporaryArchive -C $work $packageName
			if ($LASTEXITCODE -ne 0) { throw "Node $architecture archive creation failed" }
			tar -xzf $temporaryArchive -C $verification
			if ($LASTEXITCODE -ne 0) { throw "Node $architecture archive extraction failed" }
			$extracted = Join-Path $verification (Split-Path $stage -Leaf)
			$binary = Join-Path $extracted 'nexdrop'
			if (-not (Test-Path -LiteralPath $binary -PathType Leaf)) { throw "Node $architecture archive is missing nexdrop" }
			$migrationDirectory = Join-Path $extracted 'migrations'
			if (-not (Test-Path -LiteralPath $migrationDirectory -PathType Container)) { throw "Node $architecture archive is missing migrations" }
			$expectedMigrations = @(Get-ChildItem -LiteralPath (Join-Path $repo 'migrations') -Filter '*.sql' -File | ForEach-Object Name | Sort-Object)
			$actualMigrations = @(Get-ChildItem -LiteralPath $migrationDirectory -Filter '*.sql' -File | ForEach-Object Name | Sort-Object)
			if ($expectedMigrations.Count -eq 0 -or ($actualMigrations -join "`n") -cne ($expectedMigrations -join "`n")) {
				throw "Node $architecture archive migrations do not match the source"
			}
			if ($IsLinux) {
				& /usr/bin/test -x $binary
				if ($LASTEXITCODE -ne 0) { throw "Node $architecture binary is not executable" }
				$description = (& file --brief $binary) -join "`n"
				$machine = if ($architecture -eq 'amd64') { 'x86-64' } else { 'ARM aarch64' }
				if ($description -notmatch 'ELF 64-bit' -or $description -notmatch [regex]::Escape($machine)) {
					throw "Node $architecture binary has an unexpected format: $description"
				}
			}
			Move-Item -LiteralPath $temporaryArchive -Destination $archive -Force
		} finally {
			if (Test-Path -LiteralPath $work) { Remove-Item -Recurse -Force -LiteralPath $work }
			if (Test-Path -LiteralPath $verification) { Remove-Item -Recurse -Force -LiteralPath $verification }
			if (Test-Path -LiteralPath $temporaryArchive) { Remove-Item -Force -LiteralPath $temporaryArchive }
		}
	}
} finally {
	$env:CGO_ENABLED = $previousCGOEnabled
	$env:GOOS = $previousGOOS
	$env:GOARCH = $previousGOARCH
}
