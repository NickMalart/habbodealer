# build_all.ps1 -- rebuild all apps fresh from workspace root
param([switch]$SkipLauncher)
$root = $PSScriptRoot

Write-Host ('
=== [1/3] roll-origins (wails build) ===') -ForegroundColor Cyan
Remove-Item (Join-Path $root 'build\bin\roll-origins.exe') -Force -ErrorAction SilentlyContinue
Remove-Item (Join-Path $root 'build\bin\Gamba-Suite.exe') -Force -ErrorAction SilentlyContinue
Push-Location $root
wails build
if ($LASTEXITCODE -ne 0) { Write-Host '  FAILED' -ForegroundColor Red } else { Write-Host '  OK' -ForegroundColor Green }
Pop-Location

Write-Host ('
=== [2/3] wave-timer-app (go build) ===') -ForegroundColor Cyan
Remove-Item (Join-Path $root 'wave-timer-app\wave-timer-app.exe') -Force -ErrorAction SilentlyContinue
Remove-Item (Join-Path $root 'wave-timer-app\wave-timer.exe') -Force -ErrorAction SilentlyContinue
Push-Location (Join-Path $root 'wave-timer-app')
go build .
if ($LASTEXITCODE -ne 0) { Write-Host '  FAILED' -ForegroundColor Red } else { Write-Host '  OK' -ForegroundColor Green }
Pop-Location

if (-not $SkipLauncher) {
  Write-Host ('
=== [3/3] app-launcher (go build) ===') -ForegroundColor Cyan
  Remove-Item (Join-Path $root 'app-launcher\app-launcher.exe') -Force -ErrorAction SilentlyContinue
  Push-Location (Join-Path $root 'app-launcher')
  go build .
  if ($LASTEXITCODE -ne 0) { Write-Host '  FAILED' -ForegroundColor Red } else { Write-Host '  OK' -ForegroundColor Green }
  Pop-Location
}

Write-Host ('
Done.') -ForegroundColor Green