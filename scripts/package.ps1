$ErrorActionPreference = 'Stop'
& "$PSScriptRoot/build.ps1"
if ($LASTEXITCODE) { throw 'Build failed' }
$project = Split-Path $PSScriptRoot -Parent
$files = @('DuoReader.exe', 'README.md', 'THIRD_PARTY.md', 'TASK.md', 'CHANGELOG.md', 'licenses', 'docs') | ForEach-Object { Join-Path "$project/dist" $_ }
Compress-Archive -Path $files -DestinationPath "$project/dist/DuoReader-0.1.0-win-x64.zip" -Force
Write-Host 'Ready: dist/DuoReader-0.1.0-win-x64.zip'
