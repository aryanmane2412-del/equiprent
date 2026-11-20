# Shared helpers for EQUIPRENT dev scripts. Dot-source it:  . "$PSScriptRoot\_common.ps1"
Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$script:RepoRoot = (Resolve-Path (Join-Path $PSScriptRoot '..')).Path
$script:BackendDir = Join-Path $RepoRoot 'backend'
$script:AppDir = Join-Path $RepoRoot 'frontend\equiprent_app'

function Add-DevToolsToPath {
    # Tools installed on this machine but not always on PATH.
    $candidates = @(
        'C:\flutter\bin',
        'C:\Program Files\Git\cmd',
        'C:\Program Files\Go\bin',
        'C:\Program Files\PostgreSQL\18\bin',
        (Join-Path $env:USERPROFILE 'go\bin'),
        # Private PostgreSQL 18 + PostGIS runtime built by `scripts\db.ps1 postgis`.
        # Listed last so it is prepended last and wins over the plain install.
        (Join-Path $env:LOCALAPPDATA 'equiprent\pgsql\bin')
    )
    if ($env:FLUTTER_ROOT) { $candidates = @((Join-Path $env:FLUTTER_ROOT 'bin')) + $candidates }
    foreach ($dir in $candidates) {
        if ((Test-Path $dir) -and -not (($env:PATH -split ';') -contains $dir)) {
            $env:PATH = "$dir;$env:PATH"
        }
    }
}

function Import-DotEnv {
    # Loads KEY=VALUE pairs from the repo .env into this process, without
    # overriding variables that are already set.
    param([string]$Path = (Join-Path $RepoRoot '.env'))
    if (-not (Test-Path $Path)) {
        Write-Warning ".env not found. Run scripts\setup.ps1 (or copy .env.example to .env)."
        return
    }
    foreach ($line in Get-Content $Path) {
        $trimmed = $line.Trim()
        if ($trimmed -eq '' -or $trimmed.StartsWith('#')) { continue }
        $eq = $trimmed.IndexOf('=')
        if ($eq -lt 1) { continue }
        $key = $trimmed.Substring(0, $eq).Trim()
        $value = $trimmed.Substring($eq + 1).Trim()
        if ($value -match '^"(.*)"$' -or $value -match "^'(.*)'$") {
            $value = $Matches[1]
        } else {
            $value = ($value -replace '\s+#.*$', '').Trim()   # inline comment
        }
        if (-not [Environment]::GetEnvironmentVariable($key, 'Process')) {
            [Environment]::SetEnvironmentVariable($key, $value, 'Process')
        }
    }
}

function Assert-Command {
    param([string]$Name, [string]$Hint)
    if (-not (Get-Command $Name -ErrorAction SilentlyContinue)) {
        throw "'$Name' not found. $Hint"
    }
}

function ConvertTo-PgxUrl {
    # golang-migrate's pgx v5 driver uses the pgx5:// scheme.
    param([string]$Url)
    return ($Url -replace '^postgres(ql)?://', 'pgx5://')
}

function Invoke-Native {
    # Runs a native command and throws if its exit code is non-zero.
    # Windows PowerShell 5.1 turns redirected stderr of native tools into
    # terminating errors under 'Stop', so the exit code is the only signal.
    param([scriptblock]$Command, [string]$What)
    $previous = $ErrorActionPreference
    $ErrorActionPreference = 'Continue'
    try { & $Command } finally { $ErrorActionPreference = $previous }
    if ($LASTEXITCODE -ne 0) { throw "$What failed (exit $LASTEXITCODE)" }
}
