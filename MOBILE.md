# Единый OrcheRoute на Android и Apple

> Актуальный общий статус и готовый Android APK: [PROJECT_STATUS.md](PROJECT_STATUS.md).

На 13.08.2026 Android-клиент уже собран: package
`online.gooog1111.orcheroute`, версия `0.4.7`, minSdk 26. Общий WebUI работает
в `WebViewAssetLoader`, safe area учитывается через `WindowInsets`. VPN runtime
содержит локальный API bridge, foreground `VpnService`, встроенный Mihomo,
gVisor TUN и защиту исходящих сокетов от зацикливания. Реализованы локальные
подписки, их обновление и разбор, выбор узла и proxy-конфигурация Mihomo.
`MATCH,DIRECT` остаётся безопасным диагностическим режимом, когда узлов нет.
Встроенный аварийный пул содержит EbraSha и Au1rxx; кандидаты проходят два
реальных HTTPS URL-test. Мобильные Direct/Proxy/Block-маршруты сохраняются и
компилируются тем же Go-парсером, что серверные правила.
GeoIP и GeoSite обновляются вручную или периодической задачей Android с
валидацией и откатом. Версия Mihomo читается из реально связанного Go-модуля;
само ядро обновляется вместе с APK, а не как отдельный исполняемый файл.
Сетевой профиль позволяет выбрать автоматический транспорт, Wi-Fi, мобильную
сеть или Ethernet без root, а отдельные Direct/Proxy/Underlay/Bootstrap DNS
передаются встроенному Mihomo. IPv6 включается вместе с IPv6-маршрутом Android
TUN. Недоступные узлы скрыты по умолчанию во всех оболочках общего интерфейса.

OrcheRoute использует один сетевой движок — Mihomo — на сервере, desktop и
мобильных устройствах. VLESS, VMess, Trojan и Shadowsocks являются форматами
узлов подписки, а не отдельными движками.

## Общая схема

```text
WebUI / Android UI / iOS UI / desktop UI
                    │
                    ▼
          единый OrcheRoute Go-core
                    │
        ┌───────────┼───────────┐
        │           │           │
     маршруты    подписки    пулы/состояния
        │           │           │
        └───────────┼───────────┘
                    ▼
                  Mihomo
                    │
                    ▼
          платформенный TUN и lifecycle
```

В Go-core едины парсеры, API-модели, генерация конфигурации Mihomo, правила,
whitelist-автомат и серверная логика переключения. Android Java-код остаётся
платформенным адаптером для `VpnService`, разрешений, уведомлений и сохранения
локального состояния; дальнейшее дублирование предметной логики в нём не
допускается.

## Mihomo

Mihomo остаётся единственным runtime. Серверная версия запускает официальный
бинарник как процесс. Android использует мобильную сборку Mihomo. Для Apple
собирается обёртка/XCFramework из того же ядра и с тем же форматом конфигурации.

Готового официального iOS-артефакта в releases Mihomo сейчас нет, поэтому Apple
binding является нашей задачей сборки, а не поводом вводить второй движок.

Источники:

- https://github.com/MetaCubeX/mihomo/releases
- https://github.com/MetaCubeX/ClashMetaForAndroid

## Android

- минимальная версия текущего Gradle-проекта — Android 8 / API 26;
- TUN создаётся наследником `android.net.VpnService`;
- мобильная сборка Mihomo обрабатывает трафик из системного VPN;
- сокеты ядра защищаются от повторного попадания в TUN;
- выбранный узел применяется перезагрузкой Mihomo/TUN без нового запроса разрешения;
- начиная с Android 8 служба переводится в foreground;
- общая модель уже поддерживает full-device; per-app режим запланирован.

Раздача VPN на другие устройства проектируется только в no-root режиме.
OrcheRoute не будет требовать Magisk, изменять системные firewall-таблицы или
обещать прозрачный перехват tethering-трафика, который обычный `VpnService` не
контролирует. Совместимый вариант — HTTP/SOCKS-прокси, доступный клиентам
Wi-Fi/USB-точки доступа, с показом адреса, порта и диагностикой подключения.
Приложение не управляет самой точкой доступа без доступного публичного Android
API и соответствующих системных разрешений.

Системный контракт: https://developer.android.com/develop/connectivity/vpn

## iOS и macOS

- приложение содержит Packet Tunnel Provider extension;
- системная точка входа — `NEPacketTunnelProvider`;
- требуется entitlement `com.apple.developer.networking.networkextension`;
- маршруты и DNS задаются через `NEPacketTunnelNetworkSettings`;
- Mihomo и OrcheRoute Go-core собираются в XCFramework на macOS с Xcode;
- исходный Apple-клиент находится в `apple/`: общая SwiftUI-оболочка, отдельные
  iOS/macOS Packet Tunnel Extension, App Group и XcodeGen-проект;
- `scripts/build-apple.sh all` собирает XCFramework и обе Apple-цели; без Team
  ID выполняется неподписанная compile-проверка, с Team ID создаются архивы;
- Apple-движок больше не stub: build tag `ios` включает тот же Mihomo/gVisor,
  который получает системный `utun` от `NEPacketTunnelProvider`;
- секреты подписок хранятся в Keychain/App Group.

Системный контракт:
https://developer.apple.com/documentation/networkextension/nepackettunnelprovider

## OrcheRoute binding

`mobilecore` — небольшой стабильный binding управляющей логики. Он не содержит
второго VPN-движка и экспортирует только JSON и примитивные типы.

Android:

```shell
gomobile bind -target=android -androidapi=21 -o dist/OrcheRouteCore.aar ./mobilecore
```

Apple (только macOS с Xcode):

```shell
gomobile bind -target=ios -o dist/OrcheRouteCore.xcframework ./mobilecore
```

Текущий binding предоставляет:

- `Capabilities()`;
- `ParseLink(link, source, index)`;
- `ParseSubscription(linksJSON, source)`;
- `DecodeSubscriptionBody(body)`;
- `FetchSubscription(parser, secret, stateDir)`;
- `BuildMobileProxyConfig(proxyJSON)`;
- `BuildMobileProxyConfigWithRoutes(proxyJSON, routesJSON)`;
- `BuildMobileProxyConfigWithNetwork(proxyJSON, routesJSON, dnsJSON)`;
- `EngineTestProxies(proxiesJSON, testURL, timeoutMs, concurrency)` (Android);
- `ValidateSubscription(payloadJSON, partial)`;
- `AggregateSubscriptions(sourcesJSON)`;
- `WhitelistTransition(stateJSON, commandJSON)` — единый автомат производного
  пула для ограниченных сетей, включая pending/confirm/fail без гонки выбора;
- `ValidateQualificationPolicy(policyJSON)`;
- `EffectiveQualificationPolicy(policyJSON, pool)`;
- `CompileRoutes(listsJSON)`;
- `GenerateMihomoConfig(inputJSON)`;
- `PreviewNetworkProfile(profileJSON, topologyJSON)`;
- `PreviewDNS(dnsJSON)`.

HTTP и BlackTemple-загрузка выполняются общим binding-ом, чтобы протоколы не
расходились между платформами. Секреты и разобранные proxy-параметры хранит
платформенный адаптер в своём приватном хранилище; списочные API их не отдают.
В production это хранилище нужно перевести на Android Keystore и Apple Keychain.

В Android-сборке поле `embedded_engine` равно `true`: Mihomo и gVisor встроены и
проверены на реальном телефоне. Stub-сборки для неподдерживаемых целей честно
возвращают `false`.
