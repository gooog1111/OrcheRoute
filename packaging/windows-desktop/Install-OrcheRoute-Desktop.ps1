[CmdletBinding()]
param(
    [string]$InstallRoot = "$env:ProgramFiles\OrcheRoute",
    [string]$RuntimeRoot = "$env:ProgramData\OrcheRoute"
)

$ErrorActionPreference = "Stop"
$principal = New-Object Security.Principal.WindowsPrincipal([Security.Principal.WindowsIdentity]::GetCurrent())
if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
    throw "Запустите PowerShell от имени администратора."
}

if (-not (Test-Path "$PSScriptRoot\OrcheRoute.exe")) { throw "OrcheRoute.exe отсутствует в пакете." }
if (-not (Test-Path "$PSScriptRoot\Runtime\Initialize-OrcheRoute.ps1")) { throw "Runtime отсутствует в пакете." }

& "$PSScriptRoot\Runtime\Initialize-OrcheRoute.ps1" -InstallRoot $RuntimeRoot -InstallService -DesktopOnly

New-Item -ItemType Directory -Force -Path $InstallRoot | Out-Null
Copy-Item -Force "$PSScriptRoot\OrcheRoute.exe" "$InstallRoot\OrcheRoute.exe"

$shell = New-Object -ComObject WScript.Shell
$shortcut = $shell.CreateShortcut("$env:ProgramData\Microsoft\Windows\Start Menu\Programs\OrcheRoute.lnk")
$shortcut.TargetPath = "$InstallRoot\OrcheRoute.exe"
$shortcut.WorkingDirectory = $InstallRoot
$shortcut.Save()

Start-Process "$InstallRoot\OrcheRoute.exe"
Write-Host "OrcheRoute Desktop установлен. WebUI-порт не опубликован; приложение использует локальный Go API."
