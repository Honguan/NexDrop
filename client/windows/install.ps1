param(
    [Parameter(Mandatory = $true)]
    [string]$AppPath,

    [switch]$StartWithWindows
)

$ErrorActionPreference = 'Stop'
$resolvedApp = (Resolve-Path -LiteralPath $AppPath).Path
if ([IO.Path]::GetFileName($resolvedApp) -ne 'NexDrop.exe') {
    throw 'AppPath must point to NexDrop.exe'
}

$verbs = @(
    'HKCU:\Software\Classes\*\shell\NexDrop',
    'HKCU:\Software\Classes\Directory\shell\NexDrop'
)
foreach ($verb in $verbs) {
    New-Item -Path $verb -Force | Out-Null
    Set-Item -LiteralPath $verb -Value 'Send with NexDrop'
    Set-ItemProperty -Path $verb -Name 'Icon' -Value $resolvedApp
    Set-ItemProperty -Path $verb -Name 'MultiSelectModel' -Value 'Player'
    $command = Join-Path $verb 'command'
    New-Item -Path $command -Force | Out-Null
    Set-Item -LiteralPath $command -Value ('"{0}" --share %*' -f $resolvedApp)
}

if ($StartWithWindows) {
    $run = 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Run'
    New-Item -Path $run -Force | Out-Null
    Set-ItemProperty -Path $run -Name 'NexDrop' -Value ('"{0}"' -f $resolvedApp)
}

Write-Output 'NexDrop Windows integration installed.'
