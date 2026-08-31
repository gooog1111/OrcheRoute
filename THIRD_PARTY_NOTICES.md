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

Beta-реализация входящего VPN использует собственные транспортные слои
OrcheRoute и встроенный Xray Core с VLESS-inbound.

- проект: `github.com/XTLS/Xray-core`;
- версия интеграции: `v1.251202.0`;
- лицензия: Mozilla Public License 2.0;
- исходный код: https://github.com/XTLS/Xray-core/tree/v1.251202.0.

Xray Core запускается внутри Linux Server и не включается в Android-сборку.
Неизменённый исходный код соответствующей версии доступен по ссылке выше;
полный текст MPL 2.0 находится в репозитории Xray Core.

Android-сборка заменяет устаревший `github.com/wlynxg/anet`, обращающийся к
удалённым приватным символам Go, совместимым адаптером
`third_party/anet` на публичном API стандартного пакета `net`.

## Free TURN Proxy

Android-клиент и Linux Server используют библиотечную и серверную части
[samosvalishe/free-turn-proxy](https://github.com/samosvalishe/free-turn-proxy)
как изолированную Go-зависимость. Зафиксирована upstream-ревизия
`fa9549e6e089`; версия интеграции — `v3.2.0`. Проект распространяется на
условиях HBL, полный текст лицензии включён в исходную зависимость и пакет.

- закреплённая ревизия: `fa9549e6e08916ca36ae03d6c1a9a66231210c31`;
- Go-модуль: `v1.8.1-0.20260825130423-fa9549e6e089`;
- лицензия: Happy Bunny License (HBL), текст включён в исходный репозиторий
  проекта;
- авторы и участники: samosvalishe/free-turn-proxy contributors.

Адаптер собирается вместе с `mobilecore` в один AAR, чтобы Android-приложение
не содержало два конфликтующих экземпляра Go runtime. Граф зависимости
закреплён в отдельном модуле `components/freeturnbridge` и не изменяет
зависимости серверного или общего ядра OrcheRoute.

Текст лицензии HBL:

> Happy Bunny License (HBL)
>
> Copyright (c) 2026
>
> Permission is hereby granted, free of charge, to any person obtaining a copy
> of this software and associated documentation files (the "Software"), to
> deal in the Software without restriction, including without limitation the
> rights to use, copy, modify, merge, publish, distribute, sublicense, and/or
> sell copies of the Software, and to permit persons to whom the Software is
> furnished to do so, subject to the following conditions:
>
> 1. You must be kind to bunnies.
> 2. The above copyright notice and this permission notice shall be included
> in all copies or substantial portions of the Software.
>
> THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
> IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
> FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
> AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
> LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING
> FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS
> IN THE SOFTWARE.
