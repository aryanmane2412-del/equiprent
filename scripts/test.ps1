<#
.SYNOPSIS
  Runs all checks: Go vet + tests, sqlc vet, Flutter analyze + tests.
.DESCRIPTION
  -Integration also runs backend/tests against TEST_DATABASE_URL (from .env).
  Without it, those tests are reported as skipped.
.EXAMPLE
  .\scripts\test.ps1
  .\scripts\test.ps1 -Integration
  .\scripts\test.ps1 -BackendOnly
#>
param([switch]$Integration, [switch]$BackendOnly, [switch]$FrontendOnly)
. "$PSScriptRoot\_common.ps1"
Add-DevToolsToPath
Import-DotEnv

if (-not $FrontendOnly) {
    Push-Location $BackendDir
    try {
        if ($Integration) {
            if (-not $env:TEST_DATABASE_URL) { throw 'TEST_DATABASE_URL is not set (see .env.example).' }
            Write-Host "Integration tests use TEST_DATABASE_URL (migrations are applied and rolled back there)."
        } else {
            Remove-Item Env:TEST_DATABASE_URL -ErrorAction SilentlyContinue
        }
        Write-Host '== go vet' -ForegroundColor Cyan
        Invoke-Native { go vet ./... } 'go vet'
        if (Get-Command sqlc -ErrorAction SilentlyContinue) {
            Write-Host '== sqlc vet' -ForegroundColor Cyan
            Invoke-Native { sqlc vet } 'sqlc vet'
        }
        Write-Host '== go test' -ForegroundColor Cyan
        Invoke-Native { go test -count=1 -p 1 ./... } 'go test'
        if (-not $Integration) { Write-Host 'Note: backend/tests integration tests were SKIPPED (use -Integration).' -ForegroundColor Yellow }
    } finally { Pop-Location }
}

if (-not $BackendOnly) {
    Push-Location $AppDir
    try {
        Write-Host '== flutter analyze' -ForegroundColor Cyan
        Invoke-Native { flutter analyze } 'flutter analyze'
        Write-Host '== flutter test' -ForegroundColor Cyan
        Invoke-Native { flutter test } 'flutter test'
    } finally { Pop-Location }
}

Write-Host "`nAll checks passed." -ForegroundColor Green
