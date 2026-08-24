# Единая сборка OrcheRoute

`cmd/orcheroute-build` — единственная точка входа для локальной проверки.
Она проверяет общий Go-код и WebUI, после чего собирает платформенные слои из
этих же исходников.

Текущая обязательная матрица:

| Цель | Проверки |
| --- | --- |
| Windows Server | Go-тесты, служебные бинарники и актуальный WebUI |
| Windows Desktop | Go-тесты Desktop, актуальный WebUI, Wails build |
| Linux Server | Linux Go-тесты, служебные бинарники и актуальный WebUI |
| Linux Desktop | Go-тесты Desktop, актуальный WebUI, нативный Wails build |
| Android | Go-тесты, новый `mobilecore.aar`, актуальный WebUI, debug APK |

macOS и iOS пока не входят в обязательную матрицу.

## Одна команда на Windows

```powershell
.\scripts\build-all.ps1
```

Она последовательно проверяет Windows, Android и Linux через дистрибутив
`Ubuntu-24.04` в WSL. Результаты складываются в игнорируемые каталоги
`dist/verify`, `desktop/build/bin` и `android/app/build`.

Для ограниченной проверки:

```powershell
.\scripts\build-all.ps1 -Target common
.\scripts\build-all.ps1 -Target web
.\scripts\build-all.ps1 -Target windows
.\scripts\build-all.ps1 -Target windows-desktop
.\scripts\build-all.ps1 -Target android
```

На Linux:

```bash
./scripts/build-all.sh linux
```

`-skip-web` является внутренней оптимизацией для WSL: Linux получает уже
проверенный `webui/out` из Windows-этапа. Обычный отдельный запуск Linux всегда
самостоятельно собирает и тестирует WebUI.

В этом локальном репозитории `core.hooksPath` настроен на `.githooks`. Перед
коммитом общих или платформенных исходников hook автоматически запускает полную
матрицу. Коммит не создаётся, если хотя бы одна цель завершилась ошибкой.

## Защита от рассинхрона

- Общие Go-тесты выполняются нативно и на Windows, и на Linux.
- Android AAR всегда генерируется заново из `mobilecore`, а Gradle получает его
  через `orcherouteMobileCoreAar`; сохранённый старый AAR не используется.
- Android получает WebUI напрямую из свежего `webui/out` через
  `orcherouteWebAssets`.
- Desktop перед Wails build синхронизируется с тем же свежим `webui/out`.
- Server-цели включают тот же проверенный `webui/out` в свой результат сборки.
- `release.json` является единым источником версии и канала `stable`/`beta`.
  Общий модуль `webui/app/platform/release.ts` применяет цвет, подпись и favicon
  одновременно для Server, Desktop и Android. Нативная Android-обвязка читает
  тот же манифест для имени приложения и значка.
- Основной интерфейс и бизнес-функции находятся в общих модулях. Допустимые
  платформенные различия описываются только через
  `webui/app/platform/runtime.ts`: например, серверная авторизация или Android
  `VpnService`.

GitHub Actions для проекта не используются. Все сборки, тесты и публикации
выполняются только локально.

Успешная матрица подтверждает компиляцию и автоматические тесты. Она не заменяет
проверку установки пакетов, обновления, системных служб и реальной VPN-сети.
