# OrcheRoute Server для Windows

Пакет содержит единый Go control plane, официальный Mihomo и нативные Go
helper-ы сети, подписок, квалификации, GeoIP/GeoSite и обновления компонентов.

Запустите PowerShell от имени администратора:

```powershell
Set-ExecutionPolicy -Scope Process Bypass
.\Initialize-OrcheRoute.ps1 -InstallService
```

Установщик:

- размещает runtime в `%ProgramData%\OrcheRoute`;
- создаёт случайные API secrets и PBKDF2-SHA256 пароль WebUI;
- определяет действующий Windows-интерфейс и создаёт стартовый сетевой профиль;
- устанавливает автоматическую службу `OrcheRoute` и demand-службу
  `OrcheRouteMihomo`;
- не включает TUN до явного применения настроек пользователем.

WebUI доступен локально по адресу `http://127.0.0.1:19110`. Публикация в LAN
должна настраиваться явно через Management CIDR и Windows Firewall.

Вариант `-DesktopOnly` не запускает HTTP/HTTPS WebUI listeners. Его использует
Desktop-пакет: Wails обращается к API только через loopback.

Обновление Mihomo выполняется транзакционно: официальный asset проверяется по
SHA-256 из GitHub release metadata, candidate тестирует действующую
конфигурацию, а при неудачном старте восстанавливается предыдущий бинарник.
