param(
    [ValidateSet("all", "windows", "windows-server", "windows-desktop", "linux", "linux-server", "linux-desktop", "android", "web", "common")]
    [string]$Target = "all",
    [string]$WslDistro = "Ubuntu-24.04"
)

$ErrorActionPreference = "Stop"
$root = Split-Path -Parent $PSScriptRoot
Push-Location $root
try {
    go run ./cmd/orcheroute-build -target $Target -wsl-distro $WslDistro
    if ($LASTEXITCODE -ne 0) {
        throw "Unified build failed with exit code $LASTEXITCODE"
    }
} finally {
    Pop-Location
}
