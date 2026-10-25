param([switch]$Console)
$ErrorActionPreference = 'Stop'
$project = Split-Path $PSScriptRoot -Parent
Push-Location $project
try {
 New-Item -ItemType Directory -Force dist | Out-Null
 go test ./...
 if ($LASTEXITCODE) { throw 'Tests failed' }
 $linkFlags = '-s -w'
 if (-not $Console) { $linkFlags += ' -H windowsgui' }
 go build -buildvcs=false -trimpath "-ldflags=$linkFlags" -o dist/DuoReader.exe ./cmd/duoreader
 if ($LASTEXITCODE) { throw 'Build failed' }
 Copy-Item README.md, THIRD_PARTY.md, TASK.md, CHANGELOG.md -Destination dist
 Copy-Item docs -Destination dist -Recurse -Force
 New-Item -ItemType Directory -Force dist/licenses | Out-Null
 Get-ChildItem internal/ui/vendor/pdfjs -Recurse -File -Filter 'LICENSE*' | ForEach-Object {
  $name = $_.FullName.Substring((Join-Path $project 'internal/ui/vendor/pdfjs').Length + 1).Replace('\', '-')
  Copy-Item $_.FullName "dist/licenses/PDFjs-$name"
 }
 foreach ($module in @('github.com/jchv/go-webview2', 'github.com/jchv/go-winloader', 'golang.org/x/net', 'golang.org/x/sys')) {
  $moduleDir = go list -m -f '{{.Dir}}' $module
  if ($LASTEXITCODE) { throw "Cannot locate $module" }
  $name = $module.Replace('/', '-')
  $license = Get-ChildItem -Path $moduleDir -File -Filter 'LICENSE*' | Select-Object -First 1
  if (-not $license) { throw "Cannot locate license for $module" }
  Copy-Item $license.FullName "dist/licenses/$name-LICENSE"
 }
 Write-Host 'Ready: dist/DuoReader.exe'
} finally { Pop-Location }
