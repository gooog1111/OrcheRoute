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
и стандартный WireGuard-интерфейс Linux. Следующая реализация заменяет этот
тракт на Xray/VLESS и собственные транспортные слои OrcheRoute.

Алгоритм получения краткоживущих TURN-реквизитов VK и схема
DTLS → KCP → smux спроектированы после изучения:

- [cacggghp/vk-turn-proxy](https://github.com/cacggghp/vk-turn-proxy) — авторы
  и участники проекта, GPLv3, изученная ревизия
  `e8a96967dc66f3dbd631596ea6a8b9fe03f9be69`;
- [openlibrecommunity/olcrtc](https://github.com/openlibrecommunity/olcrtc) —
  Open Libre Community и участники проекта, WTFPL, изученная ревизия
  `f616f57bb3a90740f1755922ffeaa7acc5cfe4ed`.

Код транспортов написан в структуре OrcheRoute; монолитные бинарники этих
проектов не включаются. Реализация использует поддерживаемые библиотеки Pion
DTLS/TURN и уже применяемые Mihomo библиотеки KCP/smux; их лицензии включаются
в состав исходного дистрибутива согласно их условиям.
