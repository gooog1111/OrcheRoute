# Third-party notices

OrcheRoute распространяется по GNU General Public License v3.0. Полный текст
находится в файле `LICENSE`.

## Mihomo

Android-сборка OrcheRoute включает Mihomo и его зависимости.

- проект: `github.com/MetaCubeX/mihomo`;
- версия интеграции: `v1.19.30`;
- лицензия: GNU General Public License v3.0;
- исходный код: https://github.com/MetaCubeX/mihomo/tree/v1.19.30.

Исходный код OrcheRoute и полный текст GPLv3 публикуются вместе с выпусками.

## Проекты, использованные при проектировании входящего VPN

Первая beta-реализация входящего VPN использует собственный Go-код OrcheRoute
и стандартный WireGuard-интерфейс Linux. При проектировании транспортных
адаптеров изучались следующие проекты; их код пока не включён в бинарник:

- [cacggghp/vk-turn-proxy](https://github.com/cacggghp/vk-turn-proxy) — авторы
  и участники проекта, GPLv3;
- [openlibrecommunity/olcrtc](https://github.com/openlibrecommunity/olcrtc) —
  Open Libre Community и участники проекта, WTFPL.

Если код этих проектов будет включён в следующие beta-версии, здесь будут
зафиксированы точная ревизия, включённые файлы и условия распространения.
