# OrcheRoute Desktop

> Общий статус всего проекта: [PROJECT_STATUS.md](../PROJECT_STATUS.md).

Нативная оболочка использует тот же статический React/Next.js интерфейс, что и
серверный WebUI, но не открывает HTTP-порт. Wails отображает встроенные файлы в
системном WebView, а запросы `/api/v1/*` передаются внутреннему OrcheRoute API
через Go handler.

По умолчанию приложение подключается к `http://127.0.0.1:19100`. Linux Desktop
запускает API только на loopback без авторизации и не читает root-only
`/etc/orcheroute/runtime.env`. Токен остаётся доступен как явное переопределение
для удалённого API:

- `ORCHEROUTE_API_URL`;
- `ORCHEROUTE_API_TOKEN`;
- `ORCHEROUTE_RUNTIME_ENV`;
- аргументы `--api-url` и `--runtime-env`.

Сборка:

```shell
cd webui
npm install
npm run build
node ../desktop/scripts/sync-frontend.mjs
cd ../desktop
wails build
```

Production-бинарник содержит интерфейс внутри себя и не запускает dev server.
Текущая Windows x64 сборка находится в `build/bin/OrcheRoute.exe`, а копия для
передачи — в `dist/OrcheRoute-Windows-Desktop.exe`.

Linux Desktop собран нативно для `amd64` с GTK 3 и WebKitGTK 4.0. Пакет
`dist/OrcheRoute-Linux-Desktop-0.5.3-16-amd64.deb` автономно содержит Go runtime,
Mihomo и GUI. Он не зависит от Server-пакета, не открывает внешние HTTP/HTTPS
WebUI listeners и оставляет локальный API на loopback для встроенного интерфейса.
GUI запускается без `sudo`, не имеет доступа к системным секретам и при закрытии
окна сворачивается в системный трей.

Самодостаточный Windows Desktop-пакет находится в
`dist/OrcheRoute-Windows-Desktop-0.5.3.zip`. Он устанавливает локальные службы
Go control plane и Mihomo без HTTP/HTTPS WebUI listeners; само Wails-приложение
остаётся непривилегированным интерфейсом, а системный TUN обслуживает Windows
Service.

## Windows Server

Отдельный архив `dist/OrcheRoute-Windows-Server-0.5.3.zip` содержит WebUI,
официальный Mihomo и все Go helper-ы. Windows-конфигурация Mihomo и сетевой
preview проверены локально без включения TUN.

SHA-256 сборок 0.5.3:

- Server: `E2346753339649794E59CDB7A85F15C7857DDCCB41814D1EED8E69A919E0791D`;
- Desktop: `69D84F2EE5D209B17916D8DE2E39A029C4F3D30F71F77510953FC6686E6E9D78`.
