# OrcheRoute for iOS and macOS

Один SwiftUI-клиент и Packet Tunnel Extension используют общий Go-пакет
`mobilecore`, встроенный Mihomo и тот же формат proxy/routes/DNS, что Android.

## Требования

- macOS с актуальным Xcode;
- Go и `gomobile`;
- `xcodegen` (`brew install xcodegen`);
- Apple Developer Team с разрешёнными App Groups и Network Extension
  `packet-tunnel-provider` для четырёх bundle ID из `project.yml`.

## Сборка

```sh
cp Config/Signing.xcconfig.example Config/Signing.xcconfig
# впишите Team ID
../scripts/build-apple.sh all
```

Результаты появятся в `dist/apple`. Без Team ID скрипт выполняет только
неподписанную проверку компиляции для симулятора/macOS там, где это возможно.

Packet Tunnel получает системный `utun`, применяет IPv4/IPv6 default routes и
DNS interception, затем передаёт fd общему Mihomo/gVisor runtime. Root не нужен.
Для установки на устройство обязательны подпись, provisioning profiles,
App Group и Network Extension entitlement.
