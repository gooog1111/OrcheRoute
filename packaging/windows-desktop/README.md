# OrcheRoute Desktop для Windows

Запустите PowerShell от имени администратора:

```powershell
Set-ExecutionPolicy -Scope Process Bypass
.\Install-OrcheRoute-Desktop.ps1
```

Установщик создаёт локальные службы Go control plane и Mihomo, но отключает
HTTP/HTTPS WebUI listeners. Интерфейс доступен только во встроенном Wails
окне; API слушает loopback и не публикуется в LAN.
