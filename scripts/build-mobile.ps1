param(
    [ValidateSet("android", "ios")]
    [string]$Target = "android"
)

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
$dist = Join-Path $root "dist"
New-Item -ItemType Directory -Path $dist -Force | Out-Null

if (-not (Get-Command gomobile -ErrorAction SilentlyContinue)) {
    throw "gomobile не установлен: go install golang.org/x/mobile/cmd/gomobile@latest"
}

Push-Location $root
try {
    if ($Target -eq "android") {
        if (-not $env:ANDROID_HOME -and -not $env:ANDROID_SDK_ROOT) {
            throw "Для Android требуется ANDROID_HOME или ANDROID_SDK_ROOT"
        }
        gomobile bind -target=android -androidapi=21 -o (Join-Path $dist "OrcheRouteCore.aar") ./mobilecore
    } else {
        if (-not $IsMacOS) {
            throw "iOS XCFramework собирается только на macOS с Xcode"
        }
        gomobile bind -target=ios -o (Join-Path $dist "OrcheRouteCore.xcframework") ./mobilecore
    }
    if ($LASTEXITCODE -ne 0) {
        throw "gomobile завершился с кодом $LASTEXITCODE"
    }
} finally {
    Pop-Location
}
