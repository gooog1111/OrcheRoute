$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
$web = Join-Path $root "webui\out"
$assets = Join-Path $PSScriptRoot "app\src\main\assets\web"
$aar = Join-Path $PSScriptRoot "app\libs\mobilecore.aar"
$sdk = Join-Path $env:LOCALAPPDATA "Android\Sdk"
$gobin = Join-Path (go env GOPATH) "bin"
$env:ANDROID_HOME = $sdk
$env:ANDROID_SDK_ROOT = $sdk
$env:PATH = "$gobin;$sdk\platform-tools;$env:PATH"

if (-not (Test-Path -LiteralPath (Join-Path $web "index.html"))) {
    throw "webui/out is missing; run npm run build in webui"
}

New-Item -ItemType Directory -Force -Path $assets, (Split-Path -Parent $aar) | Out-Null
$androidRoot = [IO.Path]::GetFullPath($PSScriptRoot).TrimEnd('\') + '\'
$resolvedAssets = [IO.Path]::GetFullPath($assets).TrimEnd('\') + '\'
if (-not $resolvedAssets.StartsWith($androidRoot, [StringComparison]::OrdinalIgnoreCase)) {
    throw "Unsafe Android assets path: $resolvedAssets"
}
Get-ChildItem -LiteralPath $assets -Force | Remove-Item -Recurse -Force
Copy-Item -Path (Join-Path $web "*") -Destination $assets -Recurse -Force

Push-Location $root
try {
    gomobile bind -tags="with_gvisor,cmfa" -target=android/arm64 -androidapi 26 -o $aar ./mobilecore
    if ($LASTEXITCODE -ne 0) { throw "gomobile bind failed" }
} finally {
    Pop-Location
}

Push-Location $PSScriptRoot
try {
    & (Join-Path $PSScriptRoot "gradlew.bat") --no-daemon clean assembleDebug
    if ($LASTEXITCODE -ne 0) { throw "Gradle build failed" }
} finally {
    Pop-Location
}

$apk = Join-Path $PSScriptRoot "app\build\outputs\apk\debug\app-debug.apk"
$analyzer = Join-Path $sdk "cmdline-tools\latest\bin\apkanalyzer.bat"
$packagedFiles = & $analyzer files list $apk
if ($LASTEXITCODE -ne 0 -or -not ($packagedFiles -match "/assets/web/_next/static/chunks/.+\.css")) {
    throw "APK is missing WebUI _next assets"
}

$dist = Join-Path $PSScriptRoot "dist"
New-Item -ItemType Directory -Force -Path $dist | Out-Null
$gradleConfig = Get-Content -LiteralPath (Join-Path $PSScriptRoot "app\build.gradle") -Raw
$versionMatch = [regex]::Match($gradleConfig, 'versionName\s+"([^"]+)"')
if (-not $versionMatch.Success) { throw "Unable to read versionName from app/build.gradle" }
$output = Join-Path $dist ("OrcheRoute-{0}-debug.apk" -f $versionMatch.Groups[1].Value)
Copy-Item -LiteralPath $apk -Destination $output -Force
Write-Output "APK: $output"
