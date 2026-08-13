[CmdletBinding()]
param(
    [string]$InstallRoot = "$env:ProgramData\OrcheRoute",
    [switch]$InstallService,
    [switch]$DesktopOnly
)

$ErrorActionPreference = "Stop"

function New-RandomHex([int]$Bytes) {
    $buffer = New-Object byte[] $Bytes
    $random = [System.Security.Cryptography.RandomNumberGenerator]::Create()
    try { $random.GetBytes($buffer) } finally { $random.Dispose() }
    return ([System.BitConverter]::ToString($buffer)).Replace("-", "").ToLowerInvariant()
}

function ConvertTo-PlainText([Security.SecureString]$Secure) {
    $pointer = [Runtime.InteropServices.Marshal]::SecureStringToBSTR($Secure)
    try { return [Runtime.InteropServices.Marshal]::PtrToStringBSTR($pointer) }
    finally { [Runtime.InteropServices.Marshal]::ZeroFreeBSTR($pointer) }
}

function New-PasswordHash([string]$Password) {
    $salt = New-Object byte[] 16
    $random = [System.Security.Cryptography.RandomNumberGenerator]::Create()
    try { $random.GetBytes($salt) } finally { $random.Dispose() }
    $derive = New-Object System.Security.Cryptography.Rfc2898DeriveBytes(
        [Text.Encoding]::UTF8.GetBytes($Password),
        $salt,
        310000,
        [Security.Cryptography.HashAlgorithmName]::SHA256
    )
    try { $digest = $derive.GetBytes(32) } finally { $derive.Dispose() }
    $saltHex = ([BitConverter]::ToString($salt)).Replace("-", "").ToLowerInvariant()
    $digestHex = ([BitConverter]::ToString($digest)).Replace("-", "").ToLowerInvariant()
    return "pbkdf2_sha256`$310000`$$saltHex`$$digestHex"
}

$principal = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())
if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    throw "Запустите PowerShell от имени администратора."
}

$username = Read-Host "Имя пользователя WebUI (по умолчанию admin)"
if ([string]::IsNullOrWhiteSpace($username)) { $username = "admin" }
$securePassword = Read-Host "Пароль WebUI (не менее 12 символов)" -AsSecureString
$password = ConvertTo-PlainText $securePassword
if ($password.Length -lt 12) { throw "Пароль должен содержать не менее 12 символов." }

New-Item -ItemType Directory -Force -Path $InstallRoot, "$InstallRoot\state", "$InstallRoot\state\providers", "$InstallRoot\state\rules", "$InstallRoot\bin" | Out-Null
Copy-Item -Force "$PSScriptRoot\orcheroute-server.exe" "$InstallRoot\orcheroute-server.exe"
if (Test-Path "$PSScriptRoot\bin") {
    Copy-Item -Force "$PSScriptRoot\bin\*" "$InstallRoot\bin"
}
if (-not $DesktopOnly -and (Test-Path "$PSScriptRoot\webui")) {
    Copy-Item -Recurse -Force "$PSScriptRoot\webui" "$InstallRoot\webui"
}

$runtime = @(
    "api_token=$(New-RandomHex 32)"
    "controller_secret=$(New-RandomHex 32)"
    "webui_username=$username"
    "webui_password_hash=$(New-PasswordHash $password)"
    "webui_tls_mode=disabled"
) -join "`r`n"
[IO.File]::WriteAllText("$InstallRoot\runtime.env", $runtime + "`r`n", (New-Object Text.UTF8Encoding($false)))

$defaultRoute = Get-NetRoute -AddressFamily IPv4 -DestinationPrefix "0.0.0.0/0" -ErrorAction SilentlyContinue |
    Sort-Object RouteMetric, InterfaceMetric | Select-Object -First 1
if (-not $defaultRoute) { throw "Не найден активный IPv4-интерфейс с маршрутом по умолчанию." }
$interface = $defaultRoute.InterfaceAlias
$gateway = if ($defaultRoute.NextHop -and $defaultRoute.NextHop -ne "0.0.0.0") { $defaultRoute.NextHop } else { $null }
$interfaceAddress = Get-NetIPAddress -InterfaceAlias $interface -AddressFamily IPv4 -ErrorAction Stop |
    Where-Object { $_.IPAddress -notlike "169.254.*" } | Select-Object -First 1
$managementCIDR = "$($interfaceAddress.IPAddress)/32"
$now = [DateTimeOffset]::UtcNow.ToUnixTimeSeconds()
$profilePath = "$InstallRoot\state\network-profile.json"
$activePath = "$InstallRoot\state\network-active.json"
if (-not (Test-Path $profilePath)) {
    $role = @{ interface = $interface; gateway = $gateway; source = $null }
    $profile = @{
        version = 1; revision = 1; updated_at = $now
        roles = @{ direct = $role.Clone(); vpn_underlay = $role.Clone() }
        capture = @{ mode = "system"; interfaces = @(); bypass_local = $true; bypass_cidrs = @(); management_cidrs = @("127.0.0.0/8", $managementCIDR); dns_hijack = $true; strict_route = $true }
        dns = @{
            direct = @("1.1.1.1", "8.8.8.8")
            proxy = @("https://1.1.1.1/dns-query", "https://dns.google/dns-query")
            vpn_underlay = @("1.1.1.1", "8.8.8.8")
            bootstrap = @("1.1.1.1", "8.8.8.8")
            cache_algorithm = "arc"; prefer_h3 = $false; use_hosts = $true; ipv6 = $false
        }
    }
    $profileJSON = $profile | ConvertTo-Json -Depth 12
    [IO.File]::WriteAllText($profilePath, $profileJSON + "`r`n", (New-Object Text.UTF8Encoding($false)))
    [IO.File]::WriteAllText($activePath, $profileJSON + "`r`n", (New-Object Text.UTF8Encoding($false)))
}

if (-not (Test-Path "$InstallRoot\state\routes.json")) {
    $routes = @{ revision = 0; updated_at = $now; default = "proxy"; lists = @{ direct = @(); proxy = @(); block = @() }; stats = @{} } | ConvertTo-Json -Depth 8
    [IO.File]::WriteAllText("$InstallRoot\state\routes.json", $routes + "`r`n", (New-Object Text.UTF8Encoding($false)))
}
foreach ($name in @("direct", "proxy", "block")) {
    if (-not (Test-Path "$InstallRoot\state\rules\$name.txt")) { [IO.File]::WriteAllText("$InstallRoot\state\rules\$name.txt", "# empty`r`n") }
}
foreach ($name in @("primary", "emergency")) {
    if (-not (Test-Path "$InstallRoot\state\providers\$name.json")) { [IO.File]::WriteAllText("$InstallRoot\state\providers\$name.json", "{`"proxies`":[]}`r`n") }
}

if ($InstallService) {
    & sc.exe stop OrcheRouteMihomo 2>$null | Out-Null
    & sc.exe stop OrcheRoute 2>$null | Out-Null
    & sc.exe delete OrcheRouteMihomo 2>$null | Out-Null
    & sc.exe delete OrcheRoute 2>$null | Out-Null
    $serverCommand = "`"$InstallRoot\orcheroute-server.exe`""
    if ($DesktopOnly) { $serverCommand += " --web-listen= --web-tls-listen=" }
    & sc.exe create OrcheRoute "binPath= $serverCommand" "start= auto" "DisplayName= OrcheRoute Control Plane" | Out-Null
    & sc.exe description OrcheRoute "OrcheRoute API and WebUI server" | Out-Null
    $mihomoCommand = "`"$InstallRoot\bin\mihomo.exe`" -d `"$InstallRoot\state`" -f `"$InstallRoot\config.json`""
    & sc.exe create OrcheRouteMihomo "binPath= $mihomoCommand" "start= demand" "DisplayName= OrcheRoute Mihomo" | Out-Null
    & sc.exe description OrcheRouteMihomo "OrcheRoute Mihomo TUN runtime" | Out-Null
    & sc.exe start OrcheRoute | Out-Null
}

Write-Host "OrcheRoute установлен в $InstallRoot"
if (-not $DesktopOnly) { Write-Host "WebUI: http://127.0.0.1:19110" }
if (-not $InstallService) {
    Write-Host "Запуск: & '$InstallRoot\orcheroute-server.exe'"
}
