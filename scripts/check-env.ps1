<#
.SYNOPSIS
  Verifies the EQUIPRENT development toolchain and prints what is missing.
.EXAMPLE
  .\scripts\check-env.ps1
#>
. "$PSScriptRoot\_common.ps1"
$ErrorActionPreference = 'Continue'
Add-DevToolsToPath

$results = [System.Collections.Generic.List[object]]::new()
function Check {
    param([string]$Name, [bool]$Required, [scriptblock]$Probe, [string]$Fix)
    $version = $null
    try { $version = (& $Probe 2>$null | Select-Object -First 1) } catch { $version = $null }
    $ok = -not [string]::IsNullOrWhiteSpace("$version")
    $results.Add([pscustomobject]@{
        Tool     = $Name
        Status   = if ($ok) { 'OK' } elseif ($Required) { 'MISSING' } else { 'optional' }
        Version  = if ($ok) { "$version".Trim() } else { '' }
        Fix      = if ($ok) { '' } else { $Fix }
    })
}

Check 'Go'          $true  { (go version) }                                    'Install Go 1.26+ from https://go.dev/dl'
Check 'Git'         $true  { (git --version) }                                 'Install Git for Windows'
Check 'Flutter'     $true  { (flutter --version --suppress-analytics | Select-Object -First 1) } 'Install Flutter (C:\flutter) and add C:\flutter\bin to PATH'
Check 'Dart'        $true  { (dart --version 2>&1) }                           'Comes with Flutter'
Check 'sqlc'        $true  { "sqlc $(sqlc version)" }                          'go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest'
Check 'migrate'     $true  { if (Get-Command migrate -ErrorAction SilentlyContinue) { 'golang-migrate (installed)' } } "go install -tags 'pgx5' github.com/golang-migrate/migrate/v4/cmd/migrate@v4.20.1"
Check 'Docker CLI'  $false { (docker --version) }                              'Install Docker Desktop'
Check 'Docker engine' $false { $v = docker version --format '{{.Server.Version}}' 2>$null; if ($LASTEXITCODE -eq 0 -and $v) { "engine $v" } } 'Install WSL 2 (admin: wsl --install, reboot), then start Docker Desktop'
Check 'PostgreSQL (native)' $false { (psql --version) }                        'Optional: only needed for scripts\db.ps1 when Docker is unavailable'
Check 'Java (Android builds)' $false { $j = if ($env:JAVA_HOME -and (Test-Path "$env:JAVA_HOME\bin\java.exe")) { "$env:JAVA_HOME\bin\java.exe" } else { 'java' }; (& $j -version 2>&1 | Select-Object -First 1) } 'winget install EclipseAdoptium.Temurin.17.JDK, then set JAVA_HOME'
Check 'Android SDK cmdline-tools' $false { $sdk = if ($env:ANDROID_HOME) { $env:ANDROID_HOME } else { "$env:LOCALAPPDATA\Android\Sdk" }; if (Test-Path "$sdk\cmdline-tools") { "found in $sdk" } } 'Android Studio > SDK Manager > SDK Tools > Android SDK Command-line Tools'

foreach ($r in $results) {
    $color = switch ($r.Status) { 'OK' { 'Green' } 'MISSING' { 'Red' } default { 'Yellow' } }
    Write-Host ("[{0,-8}] {1,-26} {2}" -f $r.Status, $r.Tool, $r.Version) -ForegroundColor $color
    if ($r.Fix) { Write-Host ("           fix: {0}" -f $r.Fix) }
}

$free = [math]::Round((Get-PSDrive C).Free / 1GB, 1)
if ($free -lt 20) { Write-Warning "Only $free GB free on C:. Docker images, Gradle and emulators need 20-30 GB." }
if ($RepoRoot -like '*OneDrive*') { Write-Warning 'The repo is inside OneDrive. Sync can lock build folders; consider moving it to C:\dev\equiprent.' }

$missing = @($results | Where-Object Status -eq 'MISSING')
if ($missing.Count -gt 0) {
    Write-Host "`n$($missing.Count) required tool(s) missing." -ForegroundColor Red
    exit 1
}
Write-Host "`nAll required tools found." -ForegroundColor Green
