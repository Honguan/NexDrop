$ErrorActionPreference = 'Stop'

$paths = @(
    'HKCU:\Software\Classes\*\shell\NexDrop',
    'HKCU:\Software\Classes\Directory\shell\NexDrop'
)
foreach ($path in $paths) {
    Remove-Item -LiteralPath $path -Recurse -Force -ErrorAction SilentlyContinue
}
Remove-ItemProperty `
    -Path 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Run' `
    -Name 'NexDrop' `
    -Force `
    -ErrorAction SilentlyContinue

Write-Output 'NexDrop Windows integration removed.'
