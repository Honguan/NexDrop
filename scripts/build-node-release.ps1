param(
	[string]$Version = '1.0.0',
	[string]$Commit = 'development',
	[string]$OutputDirectory = 'dist'
)

$ErrorActionPreference = 'Stop'
$repo = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$output = Join-Path $repo $OutputDirectory
New-Item -ItemType Directory -Force -Path $output | Out-Null

foreach ($architecture in @('amd64', 'arm64')) {
	$stage = Join-Path $output "nexdrop-node_${Version}_linux_${architecture}"
	New-Item -ItemType Directory -Force -Path $stage | Out-Null
	$env:CGO_ENABLED = '0'
	$env:GOOS = 'linux'
	$env:GOARCH = $architecture
	go build -trimpath -ldflags "-s -w -X nexdrop/internal/version.ProductVersion=$Version -X nexdrop/internal/version.BuildCommit=$Commit" -o (Join-Path $stage 'nexdrop') ./cmd/nexdrop
	if ($LASTEXITCODE -ne 0) { throw "Node $architecture build failed" }
	Copy-Item -Recurse migrations (Join-Path $stage 'migrations')
	tar -czf "$stage.tar.gz" -C $output (Split-Path $stage -Leaf)
	Remove-Item -Recurse -Force -LiteralPath $stage
}
