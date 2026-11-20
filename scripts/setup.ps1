<#
.SYNOPSIS
  One-time project setup: creates .env, installs Go CLI tools (sqlc, migrate),
  downloads Go modules and Flutter packages.
.EXAMPLE
  .\scripts\setup.ps1
#>
. "$PSScriptRoot\_common.ps1"
Add-DevToolsToPath

$envFile = Join-Path $RepoRoot '.env'
if (-not (Test-Path $envFile)) {
    Copy-Item (Join-Path $RepoRoot '.env.example') $envFile
    Write-Host 'Created .env from .env.example'
} else {
    Write-Host '.env already exists (left unchanged)'
}

Assert-Command go 'Install Go 1.26+ from https://go.dev/dl'
if (-not (Get-Command sqlc -ErrorAction SilentlyContinue)) {
    Write-Host 'Installing sqlc...'
    $env:CGO_ENABLED = '0'
    Invoke-Native { go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest } 'sqlc install'
}
if (-not (Get-Command migrate -ErrorAction SilentlyContinue)) {
    Write-Host 'Installing golang-migrate (pgx5 driver)...'
    Invoke-Native { go install -tags 'pgx5' github.com/golang-migrate/migrate/v4/cmd/migrate@v4.20.1 } 'migrate install'
}

Push-Location $BackendDir
try {
    Write-Host 'Downloading Go modules...'
    Invoke-Native { go mod download } 'go mod download'
} finally { Pop-Location }

Assert-Command flutter 'Install Flutter and add its bin folder to PATH (C:\flutter\bin)'
Push-Location $AppDir
try {
    Write-Host 'Fetching Flutter packages...'
    Invoke-Native { flutter pub get } 'flutter pub get'
} finally { Pop-Location }

Write-Host "`nSetup complete. Next: start a database (docker compose up -d  OR  .\scripts\db.ps1 start), then .\scripts\migrate.ps1 up" -ForegroundColor Green
