<#
.SYNOPSIS
  Regenerates type-safe Go from backend/sql/queries into backend/internal/db.
.EXAMPLE
  .\scripts\sqlc.ps1
#>
. "$PSScriptRoot\_common.ps1"
Add-DevToolsToPath
Assert-Command sqlc 'Run .\scripts\setup.ps1 (installs sqlc).'

Push-Location $BackendDir
try {
    Invoke-Native { sqlc vet } 'sqlc vet'
    Invoke-Native { sqlc generate } 'sqlc generate'
    Write-Host 'sqlc: generated backend/internal/db' -ForegroundColor Green
} finally { Pop-Location }
