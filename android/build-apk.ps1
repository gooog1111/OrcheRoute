$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot

Push-Location $root
try {
    go run ./cmd/orcheroute-build -target android
    if ($LASTEXITCODE -ne 0) {
        throw "Unified Android build failed with exit code $LASTEXITCODE"
    }
} finally {
    Pop-Location
}

$apk = Join-Path $PSScriptRoot "app\build\outputs\apk\debug\app-debug.apk"
if (-not (Test-Path -LiteralPath $apk)) {
    throw "Unified build did not create $apk"
}

$gradleConfig = Get-Content -LiteralPath (Join-Path $PSScriptRoot "app\build.gradle") -Raw
$versionMatch = [regex]::Match($gradleConfig, 'versionName\s+"([^"]+)"')
if (-not $versionMatch.Success) {
    throw "Unable to read versionName from app/build.gradle"
}

$dist = Join-Path $PSScriptRoot "dist"
New-Item -ItemType Directory -Force -Path $dist | Out-Null
$output = Join-Path $dist ("OrcheRoute-{0}-debug.apk" -f $versionMatch.Groups[1].Value)
Copy-Item -LiteralPath $apk -Destination $output -Force
Write-Output "APK: $output"
