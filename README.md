# OrcheRoute

OrcheRoute — кроссплатформенный контроллер маршрутизации трафика на базе
Mihomo: подписки и пулы узлов, автоматический failover, правила
`direct`/`proxy`/`block`, DNS, WebUI и клиентские оболочки.

Текущая архитектура, фактическая готовность Linux/WebUI/Desktop/Android,
ограничения и следующие этапы собраны в
[PROJECT_STATUS.md](PROJECT_STATUS.md).

Основные документы:

- [ARCHITECTURE_AUDIT.md](ARCHITECTURE_AUDIT.md) — фактический аудит
  кроссплатформенной архитектуры, рассинхронов и release-процесса;
- [API.md](API.md) — HTTP API;
- [GO_MIGRATION.md](GO_MIGRATION.md) — завершённая унификация runtime на Go;
- [MOBILE.md](MOBILE.md) — Android и iOS;
- [webui/README.md](webui/README.md) — общий интерфейс;
- [desktop/README.md](desktop/README.md) — Wails Desktop;
- [android/README.md](android/README.md) — Android APK.
