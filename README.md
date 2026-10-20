# OrcheRoute

[English](README.en.md)

VPN-клиент для Android и VPN-контроллер с веб-интерфейсом для Linux Server на базе [Mihomo](https://github.com/MetaCubeX/mihomo). Поддерживает подписки, проверку серверов, DNS и маршруты `direct`, `proxy`, `block`.

## Скачать

- [Android 0.8.3 — APK, arm64](https://github.com/gooog1111/OrcheRoute/releases/download/v0.8.3/OrcheRoute-Android-0.8.3-code93-arm64.apk)
- [Linux Server 0.8.3 — DEB, amd64](https://github.com/gooog1111/OrcheRoute/releases/download/v0.8.3/OrcheRoute-Linux-Server-0.8.3-amd64.deb)
- [Все выпуски](https://github.com/gooog1111/OrcheRoute/releases)

## Android

Требуется Android 8.0 или новее; root не нужен.

1. Установите APK и подтвердите разрешение на создание VPN.
2. Добавьте подписку, импортируйте файл, ссылку или QR-код. При необходимости включите встроенные аварийные источники.
3. Обновите подписку и проверьте серверы — отдельно по подписке или весь список.
4. Выберите сервер вручную и нажмите «Включить».

Android использует один основной список с группировкой по подпискам. Автоматического выбора сервера и переключения по статусу сети нет. Для FreeTURN может потребоваться подтверждение VK CAPTCHA; окно можно перемещать за заголовок.

В разделе «Обновления» можно проверить и установить новую версию. Флажок «Бета-версии» выбирает тестовый канал; для стабильных обновлений выключите его.

Настройки и подписки хранятся в данных приложения. Удаление приложения удаляет эти данные — заранее экспортируйте важные подписки. Разрешите уведомления и при необходимости исключите приложение из энергосбережения для работы VPN в фоне.

## Linux Server

Установка в Debian/Ubuntu:

```bash
sudo apt install ./OrcheRoute-Linux-Server-0.8.3-amd64.deb
```

Откройте `http://127.0.0.1:19110/` на сервере либо `http://АДРЕС_СЕРВЕРА:19110/` с другого устройства. Установщик выводит данные первого входа. Авторизация и настройки сохраняются при обычном обновлении пакета.

Проверить состояние службы:

```bash
systemctl status orcheroute-go --no-pager
```

Добавьте подписки в веб-интерфейсе, задайте интервалы обновления, проверьте серверы и включите VPN. Плановое обновление загружает подписки без полной квалификации; после ошибки загрузки действует пауза перед повтором.

В автоматическом режиме используется основной список, при его недоступности — аварийный. При ограниченном доступе формируется отдельный список доступных серверов. Во время неподтверждённой смены состояния внешней сети выбор сервера удерживается. Автоматика может перейти на заметно более рейтинговый основной сервер после повторного наблюдения; небольшая разница рейтинга не вызывает переключение. Ручной выбор остаётся закреплённым.

### Квалификация серверов

Проверки включают TCP, открытие контрольных URL, регион и скорость. Рейтинг учитывает задержку, скорость и стабильность. Можно проверить одну подписку, не пересчитывая остальные.

В разделе «Квалификация» настраиваются контрольные URL, таймауты и фильтры. Дополнительный фильтр открытия адреса поддерживает три режима:

- выключен;
- допускать сервер, если адрес открывается;
- отклонять сервер, если адрес открывается.

Адрес, например `youtube.com`, проверяется через каждый VPN-сервер после обычного URL-test. Успешным считается HTTP `2xx`; `403` не подходит. Этот дополнительный фильтр доступен в Linux Server.

### Собственный VPN-сервер

Раздел «VPN-сервер» позволяет выдавать персональные подписки с FreeTURN, VLESS Reality, Trojan TLS и Hysteria2. Для подписки можно задать срок действия, лимит трафика и доступ к локальной сети сервера: наследовать общую настройку, разрешить или запретить отдельно.

Стандартные порты: FreeTURN UDP `4443`, VLESS TCP `24443`, Trojan TCP `24444`, Hysteria2 UDP `24445`. Откройте используемые порты в своём firewall. Публичный адрес может быть IP или доменным именем; адрес HTTPS-подписки задаётся отдельно. Стандартный SNI — `m.vk.ru`.

Учёт трафика VLESS/Trojan/Hysteria2 основан на периодическом опросе соединений и может недосчитывать короткие сессии. Не используйте его как точный биллинговый счётчик.

### Обновление и удаление

Обновление доступно в разделе «Обновления» веб-интерфейса или установкой нового DEB поверх предыдущего. Перед сменой настроек сети сохраните доступ к самому серверу.

```bash
sudo apt remove orcheroute  # сохранить настройки и авторизацию
sudo apt purge orcheroute   # удалить настройки, состояние и резервные копии
```

## Маршруты и GEO

Правила поддерживают IP, CIDR, диапазоны адресов, домены, GeoIP/GeoSite, протоколы и порты. Действия: `direct` — напрямую, `proxy` — через VPN, `block` — блокировать.

Источник GeoIP/GeoSite и расписание обновлений выбираются в настройках. Доступны:

- [MetaCubeX/meta-rules-dat](https://github.com/MetaCubeX/meta-rules-dat): [GeoIP](https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/geoip.dat), [GeoSite](https://github.com/MetaCubeX/meta-rules-dat/releases/download/latest/geosite.dat);
- [RunetFreedom/russia-v2ray-rules-dat](https://github.com/runetfreedom/russia-v2ray-rules-dat): [GeoIP](https://raw.githubusercontent.com/runetfreedom/russia-v2ray-rules-dat/release/geoip.dat), [GeoSite](https://raw.githubusercontent.com/runetfreedom/russia-v2ray-rules-dat/release/geosite.dat);
- [Loyalsoldier/v2ray-rules-dat](https://github.com/Loyalsoldier/v2ray-rules-dat): [GeoIP](https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geoip.dat), [GeoSite](https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geosite.dat).

Можно указать свои HTTPS-ссылки на совместимые базы.

## Источники и лицензии

- [MetaCubeX/mihomo](https://github.com/MetaCubeX/mihomo) — сетевое ядро, протоколы, DNS и маршрутизация.
- [samosvalishe/free-turn-proxy](https://github.com/samosvalishe/free-turn-proxy) — транспорт FreeTURN.
- [EbraSha/free-v2ray-public-list](https://github.com/ebrasha/free-v2ray-public-list) — [встроенный список серверов](https://raw.githubusercontent.com/ebrasha/free-v2ray-public-list/refs/heads/main/V2Ray-Config-By-EbraSha.txt).
- [Au1rxx/free-vpn-subscriptions](https://github.com/Au1rxx/free-vpn-subscriptions) — [встроенная подписка](https://raw.githubusercontent.com/Au1rxx/free-vpn-subscriptions/main/output/v2ray-base64.txt).

Встроенные подписки можно отключить. Доступность публичных серверов не гарантируется; для постоянного использования предпочтительны доверенные источники. Защищайте чувствительные данные прикладным шифрованием, например HTTPS.

Авторы, лицензии и сведения о сторонних компонентах: [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md). Лицензия проекта: [LICENSE](LICENSE).
