<#
.SYNOPSIS
  Runs the Flutter app against the local API.
.DESCRIPTION
  -Device chrome|edge  Flutter web on http://localhost:5000 (default: chrome)
  -Device android      First connected Android device/emulator
  -Device <id>         Any id from `flutter devices`
  -ApiBaseUrl          Override the API origin. A physical phone must use your
                       PC's LAN IP, e.g. http://192.168.1.20:8080 (the emulator
                       default http://10.0.2.2:8080 is applied automatically).
.EXAMPLE
  .\scripts\run-app.ps1
  .\scripts\run-app.ps1 -Device android -ApiBaseUrl http://192.168.1.20:8080
#>
param(
    [string]$Device = 'chrome',
    [string]$ApiBaseUrl,
    [int]$WebPort = 5000
)
. "$PSScriptRoot\_common.ps1"
Add-DevToolsToPath
Assert-Command flutter 'Install Flutter and add C:\flutter\bin to PATH.'

$flutterArgs = @('run')
$isWeb = $Device -in @('chrome', 'edge', 'web-server')
if ($Device -eq 'android') {
    $id = (flutter devices --machine | ConvertFrom-Json | Where-Object { $_.targetPlatform -like 'android*' } | Select-Object -First 1).id
    if (-not $id) { throw 'No Android device found. Connect a phone with USB debugging or start an emulator.' }
    $flutterArgs += @('-d', $id)
} else {
    $flutterArgs += @('-d', $Device)
}
if ($isWeb) {
    # CanvasKit from the CDN loads very slowly on this network; serve it locally.
    $flutterArgs += @('--web-port', "$WebPort", '--no-web-resources-cdn')
}
if ($ApiBaseUrl) { $flutterArgs += "--dart-define=API_BASE_URL=$ApiBaseUrl" }

Push-Location $AppDir
try {
    Write-Host "flutter $($flutterArgs -join ' ')"
    & flutter @flutterArgs
} finally { Pop-Location }
