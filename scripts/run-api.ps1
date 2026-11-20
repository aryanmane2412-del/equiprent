<#
.SYNOPSIS
  Runs the Go API (or the worker) natively, using the repo .env.
.EXAMPLE
  .\scripts\run-api.ps1             # API on http://localhost:8080
  .\scripts\run-api.ps1 -Worker     # background worker
#>
param([switch]$Worker)
. "$PSScriptRoot\_common.ps1"
Add-DevToolsToPath
Assert-Command go 'Install Go 1.26+.'

$target = if ($Worker) { './cmd/worker' } else { './cmd/api' }
Push-Location $BackendDir
try {
    # The Go config loader reads ../.env itself; nothing to export here.
    go run $target
} finally { Pop-Location }
