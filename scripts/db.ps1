<#
.SYNOPSIS
  Native PostgreSQL for development when Docker is not available.
.DESCRIPTION
  Creates a private cluster (separate from any installed PostgreSQL service)
  under %LOCALAPPDATA%\equiprent\pgdata, outside OneDrive, listening on
  localhost:5434 with the same user/databases as docker-compose.yml.

  Uses trust authentication on localhost only - development use only.

  The migrations need PostGIS. `db.ps1 postgis` builds a private copy of the
  installed PostgreSQL 18 with the PostGIS bundle overlaid, under
  %LOCALAPPDATA%\equiprent\pgsql (no admin rights, Program Files untouched).
  Every script then prefers it automatically (see _common.ps1).
.EXAMPLE
  .\scripts\db.ps1 postgis    # once: PostgreSQL + PostGIS runtime
  .\scripts\db.ps1 start
  .\scripts\db.ps1 status
  .\scripts\db.ps1 stop
  .\scripts\db.ps1 reset    # deletes all local data
#>
param(
    [Parameter(Mandatory, Position = 0)]
    [ValidateSet('start', 'stop', 'status', 'reset', 'postgis')]
    [string]$Action,
    [int]$Port = 5434,
    # postgis: the OSGeo Windows bundle zip (downloaded if absent).
    [string]$BundleZip = (Join-Path $env:LOCALAPPDATA 'equiprent\downloads\postgis-bundle-pg18-3.7.0x64.zip'),
    [string]$BundleUrl = 'https://download.osgeo.org/postgis/windows/pg18/postgis-bundle-pg18-3.7.0x64.zip'
)
. "$PSScriptRoot\_common.ps1"
Add-DevToolsToPath
Assert-Command pg_ctl 'Install PostgreSQL 18 (bin folder C:\Program Files\PostgreSQL\18\bin) or use Docker instead.'

$base = Join-Path $env:LOCALAPPDATA 'equiprent'
$data = Join-Path $base 'pgdata'
$log = Join-Path $base 'postgres.log'
$user = 'equiprent'

function Test-Running {
    $ErrorActionPreference = 'Continue'
    & pg_ctl -D $data status *> $null
    return ($LASTEXITCODE -eq 0)
}

switch ($Action) {
    'postgis' {
        $pgHome = 'C:\Program Files\PostgreSQL8'
        $runtime = Join-Path $base 'pgsql'
        if (-not (Test-Path (Join-Path $pgHome 'bin\postgres.exe'))) { throw "PostgreSQL 18 not found in $pgHome" }
        if (Test-Running) { throw 'Stop the database first: .\scripts\db.ps1 stop' }
        if (-not (Test-Path $BundleZip)) {
            New-Item -ItemType Directory -Force (Split-Path $BundleZip) | Out-Null
            Write-Host "Downloading PostGIS bundle (~80 MB) from $BundleUrl"
            Invoke-WebRequest -Uri $BundleUrl -OutFile $BundleZip -UseBasicParsing
        }
        $extract = Join-Path $base 'postgis-extract'
        if (Test-Path $extract) { Remove-Item -Recurse -Force $extract }
        Write-Host 'Extracting bundle'
        Expand-Archive -Path $BundleZip -DestinationPath $extract
        $bundle = Get-ChildItem $extract -Directory | Select-Object -First 1
        Write-Host "Building runtime in $runtime"
        if (Test-Path $runtime) { Remove-Item -Recurse -Force $runtime }
        New-Item -ItemType Directory -Force $runtime | Out-Null
        foreach ($dir in 'bin', 'lib', 'share') {
            Copy-Item -Recurse (Join-Path $pgHome $dir) (Join-Path $runtime $dir)
            # Overlay without replacing PostgreSQL's own files.
            Get-ChildItem (Join-Path $bundle.FullName $dir) -Recurse -File | ForEach-Object {
                $dest = Join-Path (Join-Path $runtime $dir) $_.FullName.Substring((Join-Path $bundle.FullName $dir).Length + 1)
                if (-not (Test-Path $dest)) {
                    New-Item -ItemType Directory -Force (Split-Path $dest) | Out-Null
                    Copy-Item $_.FullName $dest
                }
            }
        }
        Remove-Item -Recurse -Force $extract
        Write-Host "PostgreSQL + PostGIS ready in $runtime. Now: .\scripts\db.ps1 start" -ForegroundColor Green
    }
    'start' {
        if (-not (Test-Path (Join-Path $data 'PG_VERSION'))) {
            New-Item -ItemType Directory -Force $base | Out-Null
            Write-Host "Initialising cluster in $data"
            Invoke-Native { initdb -D $data -U $user -A trust -E UTF8 --no-locale *> (Join-Path $base 'initdb.log') } 'initdb'
        }
        if (Test-Running) { Write-Host "Already running on port $Port"; break }
        # Launch detached via ShellExecute (Start-Process without -NoNewWindow)
        # so the long-running postmaster does not inherit this shell's pipe
        # handles; otherwise anything piping this script's output would hang
        # until the database is stopped.
        $pgCtl = (Get-Command pg_ctl).Source
        Start-Process -FilePath $pgCtl -WindowStyle Hidden -ArgumentList @(
            '-D', "`"$data`"", '-l', "`"$log`"", '-o', "`"-p $Port -c listen_addresses=localhost`"", 'start'
        ) | Out-Null
        $ready = $false
        for ($i = 0; $i -lt 60 -and -not $ready; $i++) {
            Start-Sleep -Milliseconds 500
            & pg_isready -h localhost -p $Port -q
            $ready = ($LASTEXITCODE -eq 0)
        }
        if (-not $ready) { throw "PostgreSQL did not become ready on port $Port. See $log" }
        foreach ($db in 'equiprent', 'equiprent_test') {
            $exists = Invoke-Native { psql -h localhost -p $Port -U $user -d postgres -tAc "SELECT 1 FROM pg_database WHERE datname = '$db'" } 'psql'
            if ("$exists".Trim() -ne '1') {
                Invoke-Native { createdb -h localhost -p $Port -U $user $db } "createdb $db"
                Write-Host "Created database $db"
            }
        }
        Write-Host "PostgreSQL running on localhost:$Port (user $user). Matches DATABASE_URL in .env.example." -ForegroundColor Green
    }
    'stop' {
        if (Test-Running) { Invoke-Native { pg_ctl -D $data -w stop } 'pg_ctl stop' } else { Write-Host 'Not running' }
    }
    'status' {
        if (Test-Running) { Write-Host "Running ($data, port $Port)" } else { Write-Host 'Not running' }
    }
    'reset' {
        if (Test-Running) { Invoke-Native { pg_ctl -D $data -w stop } 'pg_ctl stop' }
        if (Test-Path $data) { Remove-Item -Recurse -Force $data; Write-Host "Deleted $data" }
    }
}
