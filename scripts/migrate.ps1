<#
.SYNOPSIS
  Database migrations with golang-migrate (backend/migrations).
.EXAMPLE
  .\scripts\migrate.ps1 up                  # apply all pending
  .\scripts\migrate.ps1 down 1              # roll back one
  .\scripts\migrate.ps1 version
  .\scripts\migrate.ps1 create add_users    # new up/down pair
  .\scripts\migrate.ps1 force 1             # clear a dirty state after a failed migration
  .\scripts\migrate.ps1 up -Test            # against TEST_DATABASE_URL
#>
param(
    [Parameter(Mandatory, Position = 0)]
    [ValidateSet('up', 'down', 'version', 'create', 'force')]
    [string]$Command,
    [Parameter(Position = 1)]
    [string]$Arg,
    [switch]$Test
)
. "$PSScriptRoot\_common.ps1"
Add-DevToolsToPath
Import-DotEnv
Assert-Command migrate "Run .\scripts\setup.ps1 (installs golang-migrate)."

# golang-migrate cannot parse absolute Windows paths (C:\...) in its file://
# source URL, so always run from backend/ with a relative path.
$dir = 'migrations'
Push-Location $BackendDir
try {
    if ($Command -eq 'create') {
        if (-not $Arg) { throw 'Usage: migrate.ps1 create <name>' }
        Invoke-Native { migrate create -ext sql -dir $dir -seq -digits 6 $Arg } 'migrate create'
        return
    }

    $url = if ($Test) { $env:TEST_DATABASE_URL } else { $env:DATABASE_URL }
    if (-not $url) { throw "$(if ($Test) {'TEST_DATABASE_URL'} else {'DATABASE_URL'}) is not set (see .env)." }
    $db = ConvertTo-PgxUrl $url

    switch ($Command) {
        'up'      { if ($Arg) { Invoke-Native { migrate -path $dir -database $db up $Arg } 'migrate up' } else { Invoke-Native { migrate -path $dir -database $db up } 'migrate up' } }
        'down'    { $n = if ($Arg) { $Arg } else { '1' }; Invoke-Native { migrate -path $dir -database $db down $n } 'migrate down' }
        'version' { Invoke-Native { migrate -path $dir -database $db version } 'migrate version' }
        'force'   { if (-not $Arg) { throw 'Usage: migrate.ps1 force <version>' }; Invoke-Native { migrate -path $dir -database $db force $Arg } 'migrate force' }
    }
} finally { Pop-Location }
