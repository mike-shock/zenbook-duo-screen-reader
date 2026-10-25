# Package an already compiled EXE (also useful after a cross-build in WSL).
$ErrorActionPreference = 'Stop'
$project = Split-Path $PSScriptRoot -Parent
Copy-Item "$project/README.md", "$project/THIRD_PARTY.md", "$project/TASK.md", "$project/CHANGELOG.md" "$project/dist"
Copy-Item "$project/docs" "$project/dist" -Recurse -Force
Get-ChildItem "$project/internal/ui/vendor/pdfjs" -Recurse -File -Filter 'LICENSE*' | ForEach-Object {
 $name = $_.FullName.Substring((Join-Path $project 'internal/ui/vendor/pdfjs').Length + 1).Replace('\', '-')
 Copy-Item $_.FullName "$project/dist/licenses/PDFjs-$name"
}
$files = @('DuoReader.exe', 'README.md', 'THIRD_PARTY.md', 'TASK.md', 'CHANGELOG.md', 'licenses', 'docs') | ForEach-Object { Join-Path "$project/dist" $_ }
Compress-Archive -Path $files -DestinationPath "$project/dist/DuoReader-0.1.0-win-x64.zip" -Force
Write-Host 'Ready: dist/DuoReader-0.1.0-win-x64.zip'
