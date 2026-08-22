"use client";
/* eslint-disable react-hooks/set-state-in-effect */

import {
  memo,
  startTransition,
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
} from "react";
import QRCode from "qrcode";
import {
  actions,
  canOpenTextFile,
  canScanQr,
  checkAndroidAppUpdate,
  getAndroidAppUpdateStatus,
  installAndroidAppUpdate,
  isEmbeddedRuntime,
  isAndroidRuntime,
  loadOperations,
  openTextFile,
  saveTextFile,
  scanQr,
	setAndroidAppUpdateBetaEnabled,
  type DashboardData,
  type DnsConfig,
  type NetworkProfile,
  type Node,
  type RouteState,
  type Subscription,
} from "../lib/api";
import { ChevronIcon, CloseIcon } from "./Icons";
import { OperationPanel, type OperationView } from "./OperationPanel";

export type SettingsTab =
  "general" | "access" | "network" | "routes" | "sources" | "components";

type Props = {
  data: DashboardData | null;
  activeTab: SettingsTab;
  onTab: (tab: SettingsTab) => void;
  onClose: () => void;
  onReload: () => Promise<void>;
};

type RunOptions = {
  title?: string;
  waitFor?: "network" | "subscriptions" | "servers" | "components";
  revision?: number;
};
type Runner = (
  operation: () => Promise<unknown>,
  success: string,
  options?: RunOptions,
) => Promise<boolean>;

const lines = (value: string) =>
  value
    .split(/\r?\n/)
    .map((item) => item.trim())
    .filter(Boolean);
const canonicalConfig = (value: unknown): unknown => {
  if (Array.isArray(value)) return value.map(canonicalConfig);
  if (value && typeof value === "object") {
    return Object.keys(value as Record<string, unknown>)
      .sort()
      .reduce<Record<string, unknown>>((result, key) => {
        result[key] = canonicalConfig(
          (value as Record<string, unknown>)[key],
        );
        return result;
      }, {});
  }
  return value;
};
const sameConfig = (left: unknown, right: unknown) =>
  JSON.stringify(canonicalConfig(left)) === JSON.stringify(canonicalConfig(right));
const COUNTRY_CODES =
  "AD AE AF AG AI AL AM AO AQ AR AS AT AU AW AX AZ BA BB BD BE BF BG BH BI BJ BL BM BN BO BQ BR BS BT BV BW BY BZ CA CC CD CF CG CH CI CK CL CM CN CO CR CU CV CW CX CY CZ DE DJ DK DM DO DZ EC EE EG EH ER ES ET FI FJ FK FM FO FR GA GB GD GE GF GG GH GI GL GM GN GP GQ GR GS GT GU GW GY HK HM HN HR HT HU ID IE IL IM IN IO IQ IR IS IT JE JM JO JP KE KG KH KI KM KN KP KR KW KY KZ LA LB LC LI LK LR LS LT LU LV LY MA MC MD ME MF MG MH MK ML MM MN MO MP MQ MR MS MT MU MV MW MX MY MZ NA NC NE NF NG NI NL NO NP NR NU NZ OM PA PE PF PG PH PK PL PM PN PR PS PT PW PY QA RE RO RS RU RW SA SB SC SD SE SG SH SI SJ SK SL SM SN SO SR SS ST SV SX SY SZ TC TD TF TG TH TJ TK TL TM TN TO TR TT TV TW TZ UA UG UM US UY UZ VA VC VE VG VI VN VU WF WS YE YT ZA ZM ZW".split(
    " ",
  );
const regionNames =
  typeof Intl !== "undefined" && "DisplayNames" in Intl
    ? new Intl.DisplayNames(["ru"], { type: "region" })
    : null;

function errorText(reason: unknown) {
  if (!(reason instanceof Error)) return "Неизвестная ошибка";
  const translations: Record<string, string> = {
    network_revision_conflict:
      "Профиль уже изменился. Закройте настройки и откройте снова.",
    route_revision_conflict:
      "Списки уже изменились. Обновите данные перед сохранением.",
    foreign_tun_active:
      "Применение заблокировано: сейчас работает другой TUN-интерфейс.",
    foreign_capture_stack_active:
      "Применение заблокировано: в системе остались правила другого TUN/VPN.",
    orcheroute_tun_not_created:
      "OrcheRoute не смог создать свой TUN. Предыдущая конфигурация восстановлена.",
    tun_route_conflict:
      "TUN не создан: в системе уже заняты необходимые маршруты. Предыдущая конфигурация восстановлена.",
    system_capture_confirmation_required:
      "Нужно подтвердить включение системного захвата.",
    network_apply_in_progress:
      "Настройки сети уже применяются. Дождитесь завершения текущей операции.",
    network_apply_interrupted:
      "Применение сети было прервано. Действующая конфигурация не заменена.",
    network_apply_start_failed: "Сервер не смог запустить задачу применения.",
    operation_timeout:
      "Сервер не подтвердил результат применения за отведённое время.",
    operation_stalled:
      "Операция не сообщает о ходе выполнения более трёх минут.",
    update_interrupted: "Обновление подписок было прервано.",
    subscription_update_start_failed:
      "Сервер не смог запустить обновление подписок.",
    duplicate_subscription: "Такая подписка уже добавлена.",
    duplicate_servers:
      "Все серверы из списка уже существуют в других локальных источниках.",
    subscription_returned_no_supported_links:
      "Не найдено поддерживаемых серверов или конфигураций WireGuard/AmneziaWG.",
    invalid_wireguard_config:
      "Конфигурация WireGuard/AmneziaWG неполная или имеет неверный формат.",
    invalid_country_code: "Коды стран должны состоять из двух латинских букв.",
    invalid_webui_username:
      "Логин: 3–64 символа, латинские буквы, цифры и знаки . _ @ -.",
    invalid_webui_password: "Пароль должен содержать не менее 12 символов.",
    invalid_tls_mode: "Неизвестный режим TLS.",
    invalid_custom_tls_paths:
      "Укажите абсолютные пути к сертификату и закрытому ключу.",
    custom_certificate_not_found:
      "Сертификат или закрытый ключ не найден на сервере.",
    configuration_write_failed:
      "Сервер не смог записать конфигурацию доступа. Изменения не применены.",
    invalid_port: "Порт должен быть числом от 1 до 65535.",
    invalid_port_range:
      "Диапазон портов должен быть в формате 1000-2000 и находиться в пределах 1–65535.",
    invalid_port_rule:
      "Используйте формат tcp:443, udp:53, any:80,443 или udp:*.",
    invalid_geo_rule:
      "Используйте формат geoip:RU или geosite:category-ads-all.",
    invalid_traffic_preset: "Некорректная запись пресета трафика.",
    unknown_traffic_preset: "Такой пресет трафика не поддерживается.",
    exact_domain_cannot_use_wildcard:
      "Точный домен с префиксом = не может содержать * или ?.",
    component_update_in_progress: "Компоненты уже обновляются.",
    unsupported_mihomo_architecture:
      "Для архитектуры этого устройства нет подходящей сборки Mihomo.",
    mihomo_checksum_mismatch:
      "Контрольная сумма Mihomo не совпала. Установка отменена.",
    mihomo_candidate_version_mismatch:
      "Загруженный бинарник имеет неожиданную версию. Установка отменена.",
    mihomo_candidate_not_executable:
      "Загруженный Mihomo не удалось запустить для проверки. Установка отменена.",
    mihomo_runtime_verification_failed:
      "Новое ядро не прошло проверку запуска и VPN-трафика.",
    mihomo_update_rolled_back:
      "Новое ядро не прошло проверку. Рабочая версия автоматически восстановлена.",
    mihomo_update_and_rollback_failed:
      "Обновление не удалось, требуется проверить состояние ядра и отката.",
    subscription_update_in_progress:
      "Обновление подписок уже выполняется. Дождитесь его завершения.",
    orcheroute_must_be_enabled:
      "Для обновления компонентов сначала включите OrcheRoute.",
  };
  if (translations[reason.message]) return translations[reason.message];
  const detailed = Object.entries(translations).find(([code]) =>
    reason.message.startsWith(`${code}:`),
  );
  if (detailed) {
    const detail = reason.message.slice(detailed[0].length + 1).trim();
    return detail ? `${detailed[1]} ${detail}` : detailed[1];
  }
  return reason.message;
}

export function SettingsModal({
  data,
  activeTab,
  onTab,
  onClose,
  onReload,
}: Props) {
  const [busy, setBusy] = useState(false);
  const [message, setMessage] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [operationView, setOperationView] = useState<OperationView | null>(
    null,
  );
  const [operationCancellable, setOperationCancellable] = useState(false);
  const [operationCancelling, setOperationCancelling] = useState(false);
  const desktopMode = isEmbeddedRuntime();
  const touchStart = useRef<{ x: number; y: number; interactive: boolean } | null>(null);
  const tabs = useMemo<SettingsTab[]>(
    () => desktopMode
      ? ["general", "network", "routes", "sources", "components"]
      : ["general", "access", "network", "routes", "sources", "components"],
    [desktopMode],
  );

  useEffect(() => {
    if (operationView?.state !== "success") return;
    const timer = window.setTimeout(() => {
      setOperationView(null);
      setMessage(null);
    }, 2600);
    return () => window.clearTimeout(timer);
  }, [operationView?.state]);

  useEffect(() => {
    if (!busy) return;
    const blockEscape = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        event.preventDefault();
        event.stopImmediatePropagation();
      }
    };
    window.addEventListener("keydown", blockEscape, true);
    return () => window.removeEventListener("keydown", blockEscape, true);
  }, [busy]);

  useEffect(() => {
    if (!isAndroidRuntime()) return;
    let revealTimer: number | null = null;
    const revealFocusedField = (event: FocusEvent) => {
      const target = event.target;
      if (!(target instanceof HTMLElement) || !target.matches("input, textarea, [contenteditable='true']")) return;
      if (revealTimer !== null) window.clearTimeout(revealTimer);
      // Wait until Android finishes the IME animation and reports the resized viewport.
      revealTimer = window.setTimeout(() => {
        target.scrollIntoView({ block: "center", inline: "nearest", behavior: "smooth" });
        revealTimer = null;
      }, 280);
    };
    document.addEventListener("focusin", revealFocusedField);
    return () => {
      document.removeEventListener("focusin", revealFocusedField);
      if (revealTimer !== null) window.clearTimeout(revealTimer);
    };
  }, []);

  const run: Runner = async (operation, success, options = {}) => {
    const steps =
      options.waitFor === "subscriptions"
        ? ["Запускаем обновление", "Загружаем подписки", "Сохраняем серверы"]
        : options.waitFor === "servers"
          ? ["Запускаем проверку", "Тестируем серверы", "Обновляем рабочий список серверов"]
        : options.waitFor === "components"
          ? [
              "Запускаем обновление",
              "Загружаем компоненты",
              "Проверяем результат",
            ]
          : options.waitFor === "network"
            ? [
                "Отправляем профиль",
                "Применяем настройки сети",
                "Проверяем связь",
              ]
            : ["Сохраняем изменения", "Проверяем ответ", "Обновляем интерфейс"];
    setBusy(true);
    setOperationCancelling(false);
    setOperationCancellable(isAndroidRuntime() && (options.waitFor === "subscriptions" || options.waitFor === "servers"));
    setMessage(null);
    setError(null);
    setOperationView({
      state: "running",
      title: options.title ?? "Применяем настройки",
      detail: "Передаём команду серверу…",
      step: 0,
      steps,
    });
    try {
      await operation();
      setOperationView(
        (current) =>
          current && {
            ...current,
            step: 1,
            detail:
              options.waitFor === "subscriptions"
                ? "Команда принята. Загружаем источники…"
                : options.waitFor === "servers"
                  ? "Команда принята. Проверяем сохранённые серверы…"
                : options.waitFor === "components"
                  ? "Команда принята. Ожидаем загрузку компонентов…"
                  : options.waitFor === "network"
                    ? "Команда принята. Ожидаем применение сетевого профиля…"
                    : "Сервер принял изменения. Проверяем ответ…",
          },
      );
      if (options.waitFor) {
        let activityKey = "";
        let inactiveSeconds = 0;
        while (true) {
          const snapshot = await loadOperations();
          if (options.waitFor === "subscriptions" || options.waitFor === "servers") {
            const update = snapshot.subscription_update;
            const nextActivityKey = `${update.status}:${update.updated_at ?? ""}:${update.current ?? ""}:${update.total ?? ""}:${update.message ?? ""}`;
            if (nextActivityKey === activityKey) inactiveSeconds += 1;
            else {
              activityKey = nextActivityKey;
              inactiveSeconds = 0;
            }
            if (["queued", "running", "cancelling"].includes(update.status)) {
              setOperationView(
                (current) =>
                  current && {
                    ...current,
                    step: 1,
                    detail:
                      update.message ?? (options.waitFor === "servers" ? "Проверяем сохранённые VPN-серверы…" : "Обновляем подписки…"),
                    progress:
                      typeof update.current === "number" && typeof update.total === "number" && update.total > 0
                        ? { current: update.current, total: update.total }
                        : undefined,
                  },
              );
            } else if (update.status === "cancelled") {
              await onReload();
              setMessage(update.message ?? "Операция остановлена");
              setOperationView((current) => current && ({ ...current, state: "success", detail: update.message ?? "Операция остановлена", progress: undefined }));
              return true;
            } else if (update.status === "success") {
              setOperationView(
                (current) =>
                  current && {
                    ...current,
                    step: 2,
                    detail: update.message ?? "Сверяем обновлённые списки серверов…",
                    progress: undefined,
                  },
              );
              break;
            } else if (update.status === "warning") {
              throw new Error(update.error ?? update.message ??
                `Обновление завершено с предупреждениями${update.failures?.length ? `: ${update.failures.join(", ")}` : "."}`);
            } else if (update.status === "error") {
              throw new Error(update.error ?? "update_interrupted");
            }
          } else if (options.waitFor === "components") {
            const update = snapshot.component_update;
            const nextActivityKey = `${update.status}:${update.updated_at ?? ""}:${update.current ?? ""}:${update.total ?? ""}:${update.message ?? ""}`;
            if (nextActivityKey === activityKey) inactiveSeconds += 1;
            else {
              activityKey = nextActivityKey;
              inactiveSeconds = 0;
            }
            if (["queued", "running"].includes(update.status)) {
              setOperationView(
                (current) =>
                  current && {
                    ...current,
                    step: 1,
                    detail: update.message ?? "Обновляем компоненты…",
                    progress:
                      typeof update.current === "number" && typeof update.total === "number" && update.total > 0
                        ? { current: update.current, total: update.total }
                        : undefined,
                  },
              );
            } else if (update.status === "success") {
              setOperationView(
                (current) =>
                  current && {
                    ...current,
                    step: 2,
                    detail: update.message ?? "Проверяем версии…",
                    progress: undefined,
                  },
              );
              break;
            } else if (update.status === "error") {
              throw new Error(update.error ?? "component_update_interrupted");
            }
          } else {
            const apply = snapshot.network_apply;
            const nextActivityKey = `${apply.status ?? ""}:${apply.updated_at ?? ""}:${apply.revision ?? ""}:${apply.message ?? ""}`;
            if (nextActivityKey === activityKey) inactiveSeconds += 1;
            else {
              activityKey = nextActivityKey;
              inactiveSeconds = 0;
            }
            if (
              apply.revision === options.revision &&
              ["queued", "applying"].includes(apply.status ?? "")
            ) {
              setOperationView(
                (current) =>
                  current && {
                    ...current,
                    step: 1,
                    detail:
                      apply.status === "queued"
                        ? "Задача ожидает запуска…"
                        : "Применяем профиль и запускаем сетевой интерфейс…",
                  },
              );
            }
            if (
              apply.revision === options.revision &&
              ["applied", "failed", "blocked"].includes(apply.status ?? "")
            ) {
              if (apply.status !== "applied")
                throw new Error(apply.error ?? "network_apply_failed");
              setOperationView(
                (current) =>
                  current && {
                    ...current,
                    step: 2,
                    detail: "Проверяем связь после применения…",
                  },
              );
              break;
            }
          }
          if (inactiveSeconds >= 180) throw new Error("operation_stalled");
          await new Promise((resolve) => window.setTimeout(resolve, 1000));
        }
      }
      await onReload();
      setMessage(success);
      setOperationView(
        (current) =>
          current && {
            ...current,
            state: "success",
            step: current.steps.length,
            detail: success,
            progress: undefined,
          },
      );
      return true;
    } catch (reason) {
      const detail = errorText(reason);
      setError(detail);
      setOperationView(
        (current) =>
          current && {
            ...current,
            state: "error",
            detail,
            progress: undefined,
          },
      );
      return false;
    } finally {
      setBusy(false);
      setOperationCancellable(false);
      setOperationCancelling(false);
    }
  };

  const cancelSubscriptionOperation = async () => {
    if (operationCancelling) return;
    setOperationCancelling(true);
    setOperationView((current) => current && ({ ...current, detail: "Останавливаем после завершения текущей группы тестов…" }));
    try {
      await actions.cancelSubscriptionUpdate();
    } catch (reason) {
      setOperationCancelling(false);
      setError(errorText(reason));
    }
  };

  const desiredCapture = data?.network?.desired.capture.mode;
  const networkPending = Boolean(data?.network && !data.network.in_sync);
  const lastApplyFailed = Boolean(
    data?.network?.apply &&
    data.network.apply.revision === data.network.desired.revision &&
    ["failed", "blocked"].includes(data.network.apply.status ?? ""),
  );
  const baseOperation: OperationView = lastApplyFailed
    ? {
        state: "error",
        title: "Не удалось применить настройки",
        detail: errorText(
          new Error(data?.network?.apply.error ?? "network_apply_interrupted"),
        ),
        step: 0,
        steps: [],
      }
    : networkPending
      ? {
          state: "pending",
          title: "Системный режим готов к применению",
          detail:
            "Применение изменит маршрутизацию устройства. Если уже работает другой TUN/VPN, сначала остановите его — OrcheRoute не управляет сторонними программами.",
          step: 0,
          steps: [],
        }
      : {
          state: "idle",
          title: "Настройки применены",
          detail: "Сохранённый сетевой профиль совпадает с действующим.",
          step: 0,
          steps: [],
        };
  const displayedOperation = operationView ?? baseOperation;
  const applyNetwork = () => {
    if (!data?.network || data.network.in_sync) return;
    void run(
      () => actions.applyNetwork(data.network!.desired.revision, true),
      "Сетевой профиль применён и связь проверена.",
      {
        title: "Применяем сетевой профиль",
        waitFor: "network",
        revision: data.network.desired.revision,
      },
    );
  };
  const onTouchStart = (event: React.TouchEvent<HTMLDivElement>) => {
    const touch = event.touches[0];
    const target = event.target as HTMLElement;
    touchStart.current = {
      x: touch.clientX,
      y: touch.clientY,
      interactive: Boolean(target.closest("input, textarea, select, button, a, details, [contenteditable='true']")),
    };
  };
  const onTouchEnd = (event: React.TouchEvent<HTMLDivElement>) => {
    const start = touchStart.current;
    touchStart.current = null;
    if (!start || start.interactive || busy) return;
    const touch = event.changedTouches[0];
    const dx = touch.clientX - start.x;
    const dy = touch.clientY - start.y;
    if (Math.abs(dx) < 64 || Math.abs(dx) < Math.abs(dy) * 1.25) return;
    const current = tabs.indexOf(activeTab);
    const next = dx < 0 ? current + 1 : current - 1;
    if (next >= 0 && next < tabs.length) onTab(tabs[next]);
  };

  return (
    <div
      className="modal-backdrop has-operation"
      role="presentation"
      onMouseDown={(event) =>
        !busy && event.target === event.currentTarget && onClose()
      }
    >
      <section
        className="settings-modal settings-modal-wide"
        role="dialog"
        aria-modal="true"
        aria-labelledby="settings-title"
      >
        <header className="modal-header">
          <div>
            <span>OrcheRoute</span>
            <h2 id="settings-title">Настройки</h2>
          </div>
          <button
            className="icon-button"
            type="button"
            onClick={onClose}
            disabled={busy}
            aria-label="Закрыть настройки"
          >
            <CloseIcon />
          </button>
        </header>
        <div
          className={`settings-layout ${busy ? "is-locked" : ""}`}
          aria-busy={busy}
          inert={busy ? true : undefined}
        >
          <nav className="settings-nav" aria-label="Разделы настроек">
            <Tab
              active={activeTab === "general"}
              onClick={() => onTab("general")}
              label="Основное"
            />
            {!desktopMode && (
              <Tab
                active={activeTab === "access"}
                onClick={() => onTab("access")}
                label="Доступ"
              />
            )}
            <Tab
              active={activeTab === "network"}
              onClick={() => onTab("network")}
              label="Сеть и DNS"
            />
            <Tab
              active={activeTab === "routes"}
              onClick={() => onTab("routes")}
              label="Маршруты"
            />
            <Tab
              active={activeTab === "sources"}
              onClick={() => onTab("sources")}
              label="Подписки"
            />
            <Tab
              active={activeTab === "components"}
              onClick={() => onTab("components")}
              label="Обновления"
            />
          </nav>
          <div className="settings-content" onTouchStart={onTouchStart} onTouchEnd={onTouchEnd}>
            {activeTab === "general" && (
              <GeneralForm data={data} busy={busy} run={run} />
            )}
            {activeTab === "access" && <AccessPanel data={data} busy={busy} />}
            {activeTab === "network" &&
              (isAndroidRuntime() ? (
                <AndroidNetworkForm data={data} busy={busy} run={run} />
              ) : (
                <NetworkForm data={data} busy={busy} run={run} />
              ))}
            {activeTab === "routes" && (
              <RoutesForm data={data} busy={busy} run={run} />
            )}
            {activeTab === "sources" && (
              <SubscriptionsForm data={data} busy={busy} run={run} />
            )}
            {activeTab === "components" && (
              <ComponentsForm data={data} busy={busy} run={run} />
            )}
          </div>
        </div>
        {(message || error) && (
          <div
            className={`settings-feedback ${error ? "error" : "success"}`}
            role="status"
          >
            {error ?? message}
          </div>
        )}
        <OperationPanel
          operation={displayedOperation}
          onDismiss={operationView ? () => setOperationView(null) : undefined}
          action={
            operationCancellable && operationView?.state === "running"
              ? {
                  label: operationCancelling ? "Останавливаем" : "Остановить",
                  onClick: () => void cancelSubscriptionOperation(),
                  disabled: operationCancelling,
                }
              : networkPending
              ? {
                  label: "Применить изменения",
                  onClick: applyNetwork,
                  disabled: busy,
                }
              : undefined
          }
        />
      </section>
    </div>
  );
}

function Tab({
  active,
  onClick,
  label,
}: {
  active: boolean;
  onClick: () => void;
  label: string;
}) {
  return (
    <button type="button" className={active ? "active" : ""} onClick={onClick}>
      <span>{label}</span>
      <ChevronIcon />
    </button>
  );
}

function GeneralForm({
  data,
  busy,
  run,
}: {
  data: DashboardData | null;
  busy: boolean;
  run: Runner;
}) {
  const policy = data?.qualification?.policy;
  const currentNode = data?.nodes.find((node) => node.selected && node.alive);
  const [countries, setCountries] = useState<string[]>([]);
  const [speed, setSpeed] = useState("10");
  const [stability, setStability] = useState("65");
  const [allowlistProbeURL, setAllowlistProbeURL] = useState("https://ya.ru/");
  const [openInternetProbeURL, setOpenInternetProbeURL] = useState("https://www.cloudflare.com/cdn-cgi/trace");
  const [emergencyTop, setEmergencyTop] = useState("100");

  useEffect(() => {
    if (!policy) return;
    setCountries(policy.defaults.excluded_countries);
    setSpeed(String(policy.defaults.min_speed_mbps));
    setStability(String(Math.round(policy.defaults.stability_ratio * 100)));
    setAllowlistProbeURL(policy.defaults.allowlist_probe_url);
    setOpenInternetProbeURL(policy.defaults.open_internet_probe_url);
    setEmergencyTop(String(policy.pools.emergency.speed_candidates_per_source ?? 100));
  }, [policy]);

  const policyDraft = policy
    ? {
        defaults: {
          excluded_countries: countries,
          min_speed_mbps: Number(speed),
          stability_ratio: Number(stability) / 100,
          allowlist_probe_url: allowlistProbeURL.trim(),
          open_internet_probe_url: openInternetProbeURL.trim(),
        },
        emergency_top: Number(emergencyTop),
      }
    : null;
  const savedPolicy = policy
    ? {
        defaults: policy.defaults,
        emergency_top: policy.pools.emergency.speed_candidates_per_source ?? 100,
      }
    : null;
  const policyChanged = Boolean(
    policyDraft && savedPolicy && !sameConfig(policyDraft, savedPolicy),
  );

  const savePolicy = () =>
    run(
      () =>
        actions.updateQualification(
          {
            excluded_countries: countries,
            min_speed_mbps: Number(speed),
            stability_ratio: Number(stability) / 100,
            allowlist_probe_url: allowlistProbeURL.trim(),
            open_internet_probe_url: openInternetProbeURL.trim(),
          },
          {
            emergency: {
              ...policy!.pools.emergency,
              speed_candidates_per_source: Number(emergencyTop),
            },
          },
        ),
      "Политика проверки сохранена. Она начнёт действовать при следующем обновлении подписок.",
    );

  return (
    <div className="settings-section">
      <Heading
        eyebrow="Переключение"
        title="Выбор VPN-сервера"
      />
      <div className="mode-choice-grid control-modes">
        <button
          type="button"
          className={`mode-card ${data?.status.proxy.mode === "auto" ? "selected" : ""}`}
          disabled={busy}
          onClick={() =>
            void run(actions.setAuto, "Включён автоматический выбор сервера.")
          }
        >
          <span className="mode-card-radio" />
          <strong>Автоматически</strong>
          <small>
            Контроллер сам выбирает узел, переключается только после отказа и
            возвращается в основной список серверов.
          </small>
        </button>
        <button
          type="button"
          className={`mode-card ${data?.status.proxy.mode === "manual" ? "selected" : ""}`}
          disabled={busy || !currentNode}
          onClick={() =>
            currentNode &&
            void run(
              () => actions.setManual(currentNode.id),
              `Ручной режим включён. Закреплён ${currentNode.display_name}.`,
            )
          }
        >
          <span className="mode-card-radio" />
          <strong>Ручной режим</strong>
          <small>
            {currentNode
              ? `Закрепить текущий сервер ${currentNode.display_name}. Другой сервер можно выбрать ниже.`
              : "Станет доступен после подключения к рабочему серверу."}
          </small>
        </button>
      </div>
      {(["primary", "emergency"] as const).map((pool) => (
        <PoolNodes key={pool} data={data} pool={pool} busy={busy} run={run} />
      ))}
      <PoolNodes data={data} pool="whitelist" busy={busy} run={run} />
      <Heading
        eyebrow="Квалификация"
        title="Фильтрация серверов"
        text="Параметры применяются одинаково к основным и аварийным подпискам."
        compact
      />
      <CountryPicker value={countries} onChange={setCountries} />
      <div className="form-grid two">
        <Field
          label="Резервный порог"
          hint="Используется только если не удалось измерить WAN; обычно порог равен 10% скорости канала"
          suffix="Мбит/с"
        >
          <input
            type="number"
            min="0.1"
            max="10000"
            step="0.1"
            value={speed}
            onChange={(event) => setSpeed(event.target.value)}
          />
        </Field>
        <Field label="Стабильность" suffix="%">
          <input
            type="number"
            min="10"
            max="100"
            value={stability}
            onChange={(event) => setStability(event.target.value)}
          />
        </Field>
      </div>
      <div className="form-grid two">
        <Field
          label="Speed-test аварийной подписки"
          hint="Проверять скорость только у лучших по URL-test серверов каждого источника"
          suffix="серверов"
        >
          <input
            type="number"
            min="1"
            max="10000"
            value={emergencyTop}
            onChange={(event) => setEmergencyTop(event.target.value)}
          />
        </Field>
      </div>
      <div className="form-grid two">
        <Field
          label="Доступно при белых списках"
          hint="URL, который точно открывается при ограниченном доступе"
        >
          <input
            type="url"
            value={allowlistProbeURL}
            onChange={(event) => setAllowlistProbeURL(event.target.value)}
            placeholder="https://доступный-сайт.example/ping"
          />
        </Field>
        <Field
          label="Доступно в обычном интернете"
          hint="URL вне белых списков, который точно отвечает при полном доступе"
        >
          <input
            type="url"
            value={openInternetProbeURL}
            onChange={(event) => setOpenInternetProbeURL(event.target.value)}
            placeholder="https://внешний-сайт.example/generate_204"
          />
        </Field>
      </div>
      <ActionBar>
        <button
          className="primary-button"
          type="button"
          disabled={busy || !policy || !policyChanged}
          onClick={() => void savePolicy()}
        >
          Сохранить
        </button>
      </ActionBar>
    </div>
  );
}

function PoolNodes({
  data,
  pool,
  busy,
  run,
}: {
  data: DashboardData | null;
  pool: "primary" | "emergency" | "whitelist";
  busy: boolean;
  run: Runner;
}) {
  const [showUnavailable, setShowUnavailable] = useState(false);
  const [deletingNode, setDeletingNode] = useState<Node | null>(null);
  const canEditPool = isAndroidRuntime();
  const allNodes = data?.nodes.filter((node) => node.pool === pool) ?? [];
  const unavailable = allNodes.filter((node) => !node.alive).length;
  const nodes = showUnavailable
    ? allNodes
    : allNodes.filter((node) => node.alive);
  const report = pool === "whitelist" ? null : data?.qualification?.reports?.[pool];
  const sourceNames = Object.fromEntries(
    (data?.subscriptions ?? [])
      .filter((item) => pool === "whitelist" || item.group === pool)
      .map((item) => [item.id, item.name]),
  );
  const sources = Object.fromEntries(
    Object.values(sourceNames).map((name) => [name, 0]),
  ) as Record<string, number>;
  nodes.forEach((node) => {
    const name =
      node.source_name || node.source_id || "Источник прежнего формата";
    sources[name] = (sources[name] ?? 0) + 1;
  });
  return (
    <div className="node-group">
      <div className="node-group-heading">
        <span className="field-label">
          {pool === "primary" ? "Основной список серверов" : pool === "emergency" ? "Аварийный список серверов" : "Список серверов для белых списков"}
        </span>
        {pool === "whitelist" && canEditPool && (
          <button
            type="button"
            disabled={busy}
            onClick={() => void run(
              () => actions.scanWhitelistPool(),
              "Список серверов для белых списков сформирован.",
              { title: "Формируем список серверов", waitFor: "subscriptions" },
            )}
          >
            Сформировать
          </button>
        )}
        {unavailable > 0 && (
          <button
            type="button"
            onClick={() => setShowUnavailable((current) => !current)}
          >
            {showUnavailable
              ? "Скрыть недоступные"
              : `Недоступные · ${unavailable}`}
          </button>
        )}
      </div>
      <div className="node-list">
        {nodes.map((node) => (
          <div
            className={`node-editor-row ${node.selected ? "selected" : ""}`}
            key={node.id}
          >
            <button
              type="button"
              className="node-editor-main"
              disabled={!node.alive || busy || pool === "whitelist"}
              onClick={() => pool !== "whitelist" &&
                void run(
                  () => actions.setManual(node.id),
                  `Узел ${node.display_name} закреплён вручную.`,
                )
              }
            >
              <span className={`node-status ${node.alive ? "alive" : ""}`} />
              <span>
                <strong>{node.display_name}</strong>
                <small>
                  {node.source_name ||
                    node.source_id ||
                    "Источник прежнего формата"}{" "}
                  · {node.alive ? "доступен" : "недоступен"}
                </small>
              </span>
              <em>{node.delay_ms ? `${node.delay_ms} мс` : "—"}</em>
            </button>
            {canEditPool && (
              <button
                type="button"
                className="node-delete-button"
                disabled={busy}
                onClick={() => setDeletingNode(node)}
                aria-label={`Удалить ${node.display_name} из текущего списка`}
              >
                Удалить
              </button>
            )}
          </div>
        ))}
        {!nodes.length && (
          <p className="empty-state">
            {unavailable
              ? `Рабочих серверов нет. Недоступных: ${unavailable}.`
              : "В списке пока нет квалифицированных серверов."}
          </p>
        )}
      </div>
      {deletingNode && (
        <div
          className="picker-dialog-backdrop"
          role="presentation"
          onMouseDown={() => !busy && setDeletingNode(null)}
        >
          <section
            className="picker-dialog subscription-delete-dialog"
            role="alertdialog"
            aria-modal="true"
            aria-labelledby="delete-pool-node-title"
            onMouseDown={(event) => event.stopPropagation()}
          >
            <header>
              <div>
                <strong id="delete-pool-node-title">Удалить сервер из списка?</strong>
                <small>Удаляется только текущая копия</small>
              </div>
              <button type="button" disabled={busy} onClick={() => setDeletingNode(null)} aria-label="Закрыть">×</button>
            </header>
            <div className="subscription-delete-body">
              <p>«{deletingNode.display_name}» исчезнет из текущего списка.</p>
              <small>Следующая проверка или обновление подписки сможет добавить сервер снова. Если он сейчас активен, OrcheRoute выберет следующий доступный сервер.</small>
            </div>
            <footer>
              <button className="secondary-button" type="button" disabled={busy} onClick={() => setDeletingNode(null)}>Отмена</button>
              <button
                className="danger-button"
                type="button"
                disabled={busy}
                onClick={() =>
                  void (async () => {
                    const ok = await run(
                      () => actions.deleteNode(deletingNode.id),
                      "Сервер удалён из текущего списка. Обновление подписки сможет добавить его снова.",
                      { title: "Удаляем сервер из списка" },
                    );
                    if (ok) setDeletingNode(null);
                  })()
                }
              >
                Удалить
              </button>
            </footer>
          </section>
        </div>
      )}
      <div className="pool-audit">
        <div>
          {Object.entries(sources).map(([name, count]) => (
            <span key={name}>
              <strong>{name}</strong>
              {count}
            </span>
          ))}
        </div>
        {report && (
          <small>
            Последняя проверка: {report.input} кандидатов → TCP{" "}
            {report.tcp_alive} → URL {report.url_alive} → speed-test{" "}
            {report.speed_tested} → принято {report.retained}
            {report.threshold_mbps
              ? ` · WAN ${report.baseline_mbps?.toFixed(1) ?? "—"} Мбит/с · порог ${report.threshold_mbps.toFixed(1)} Мбит/с`
              : ""}
          </small>
        )}
        {report?.sources && (
          <div className="source-funnels">
            {Object.entries(report.sources).map(([id, value]) => (
              <small key={id}>
                <strong>{sourceNames[id] || id}</strong>
                {value.input} → TCP {value.tcp_alive} → URL {value.url_alive} →
                speed {value.speed_tested} → принято {value.retained}
              </small>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

function AndroidNetworkForm({
  data,
  busy,
  run,
}: {
  data: DashboardData | null;
  busy: boolean;
  run: Runner;
}) {
  const desired = data?.network?.desired;
  const activeDns = data?.dns?.active;
  const [transport, setTransport] = useState("auto");
  const [dns, setDns] = useState<DnsConfig | null>(null);
  const [dnsText, setDnsText] = useState<
    Record<"direct" | "proxy" | "vpn_underlay" | "bootstrap", string>
  >({ direct: "", proxy: "", vpn_underlay: "", bootstrap: "" });
  useEffect(() => {
    if (desired) setTransport(desired.roles.vpn_underlay.interface || "auto");
  }, [desired]);
  useEffect(() => {
    if (!desired?.dns && !activeDns) return;
    const next = structuredClone(desired?.dns ?? activeDns!);
    setDns(next);
    setDnsText({
      direct: next.direct.join("\n"),
      proxy: next.proxy.join("\n"),
      vpn_underlay: next.vpn_underlay.join("\n"),
      bootstrap: next.bootstrap.join("\n"),
    });
  }, [desired, activeDns]);
  if (!desired || !dns || !data?.network)
    return <p className="empty-state">Транспортный профиль недоступен.</p>;
  const transportChanged =
    desired.capture.mode !== "system" ||
    desired.roles.direct.interface !== transport ||
    desired.roles.vpn_underlay.interface !== transport;
  const dnsDraft = {
    ...dns,
    direct: lines(dnsText.direct),
    proxy: lines(dnsText.proxy),
    vpn_underlay: lines(dnsText.vpn_underlay),
    bootstrap: lines(dnsText.bootstrap),
  };
  const dnsChanged = !sameConfig(dnsDraft, desired.dns ?? activeDns);
  const transports = data.interfaces.filter((item) =>
    ["auto", "wifi", "cellular", "ethernet"].includes(item.name),
  );
  const saveTransport = () => {
    const profile = structuredClone(desired);
    profile.capture.mode = "system";
    profile.roles.direct.interface = transport;
    profile.roles.vpn_underlay.interface = transport;
    void run(async () => {
      await actions.validateNetwork(profile);
      await actions.saveNetwork(desired.revision, profile);
    }, "Транспорт сохранён. Нажмите «Применить изменения».");
  };
  const saveDns = () => {
    void run(async () => {
      await actions.validateDns(dnsDraft);
      await actions.saveDns(desired.revision, dnsDraft);
    }, "DNS-профиль сохранён. Нажмите «Применить изменения».");
  };
  return (
    <div className="settings-section android-network-form">
      <Heading
        eyebrow="Транспорт Android"
        title="Через какую сеть подключаться"
        text="Автоматический режим следует за системной сетью. Фиксированный режим удерживает VPN на Wi-Fi, мобильной сети или Ethernet и сообщает, если выбранный транспорт исчез."
      />
      <div className="transport-choice-grid">
        {transports.map((item) => (
          <button
            type="button"
            key={item.name}
            className={`mode-card ${transport === item.name ? "selected" : ""}`}
            onClick={() => setTransport(item.name)}
          >
            <span className="mode-card-radio" />
            <strong>{item.kind}</strong>
            <small>
              {item.name === "auto"
                ? "Системный выбор и автоматическое переключение"
                : item.state === "up"
                  ? `Доступен${item.addresses.length ? ` · ${item.addresses.map((address) => address.cidr).join(", ")}` : ""}`
                  : "Сейчас недоступен"}
            </small>
          </button>
        ))}
      </div>
      <ActionBar>
        <span>
          Активен: {data.network.active.roles.vpn_underlay.interface || "auto"}
        </span>
        <button
          className="secondary-button"
          type="button"
          disabled={busy || !transportChanged}
          onClick={saveTransport}
        >
          Сохранить транспорт
        </button>
      </ActionBar>
      <Heading
        eyebrow="DNS"
        title="Раздельные резолверы"
        text="Bootstrap разрешает имена DNS-сервисов, Underlay — адрес VPN-сервера, Direct и Proxy обслуживают соответствующие правила маршрутизации."
        compact
      />
      <div className="form-grid two">
        <Field label="Direct DNS" hint="IP, DoH или DoT · по одному в строке">
          <textarea
            rows={3}
            value={dnsText.direct}
            onChange={(event) =>
              setDnsText((current) => ({
                ...current,
                direct: event.target.value,
              }))
            }
          />
        </Field>
        <Field label="Proxy DNS" hint="По одному в строке">
          <textarea
            rows={3}
            value={dnsText.proxy}
            onChange={(event) =>
              setDnsText((current) => ({
                ...current,
                proxy: event.target.value,
              }))
            }
          />
        </Field>
        <Field label="DNS VPN-сервера" hint="Рекомендуются числовые IP">
          <textarea
            rows={3}
            value={dnsText.vpn_underlay}
            onChange={(event) =>
              setDnsText((current) => ({
                ...current,
                vpn_underlay: event.target.value,
              }))
            }
          />
        </Field>
        <Field label="Bootstrap" hint="Числовые IP">
          <textarea
            rows={3}
            value={dnsText.bootstrap}
            onChange={(event) =>
              setDnsText((current) => ({
                ...current,
                bootstrap: event.target.value,
              }))
            }
          />
        </Field>
        <Field label="Кеш">
          <select
            value={dns.cache_algorithm}
            onChange={(event) =>
              setDns((current) =>
                current
                  ? {
                      ...current,
                      cache_algorithm: event.target.value as "arc" | "lru",
                    }
                  : current,
              )
            }
          >
            <option value="arc">ARC</option>
            <option value="lru">LRU</option>
          </select>
        </Field>
      </div>
      <div className="toggle-grid">
        <Toggle
          checked={dns.prefer_h3}
          onChange={(value) =>
            setDns((current) =>
              current ? { ...current, prefer_h3: value } : current,
            )
          }
          label="Предпочитать HTTP/3"
        />
        <Toggle
          checked={dns.use_hosts}
          onChange={(value) =>
            setDns((current) =>
              current ? { ...current, use_hosts: value } : current,
            )
          }
          label="Использовать hosts"
        />
        <Toggle
          checked={dns.ipv6}
          onChange={(value) =>
            setDns((current) =>
              current ? { ...current, ipv6: value } : current,
            )
          }
          label="Перехватывать IPv6"
        />
      </div>
      <div className={`context-note ${dns.ipv6 ? "" : "danger"}`}>
        <strong>
          {dns.ipv6 ? "IPv4 и IPv6 идут через VPN" : "IPv6 выключен"}
        </strong>
        <span>
          {dns.ipv6
            ? "Android TUN получит маршруты обоих семейств."
            : "IPv6-трафик не будет добавлен в TUN. Используйте это только если сеть или сервер не поддерживают IPv6."}
        </span>
      </div>
      <ActionBar>
        <span>
          DNS: {data.dns?.in_sync ? "применён" : "ожидает применения"}
        </span>
        <button
          className="secondary-button"
          type="button"
          disabled={busy || !dnsChanged}
          onClick={saveDns}
        >
          Сохранить DNS
        </button>
      </ActionBar>
    </div>
  );
}

function NetworkForm({
  data,
  busy,
  run,
}: {
  data: DashboardData | null;
  busy: boolean;
  run: Runner;
}) {
  const desired = data?.network?.desired;
  const [profile, setProfile] = useState<NetworkProfile | null>(null);
  const [dns, setDns] = useState<DnsConfig | null>(null);
  const [dnsText, setDnsText] = useState<
    Record<"direct" | "proxy" | "vpn_underlay" | "bootstrap", string>
  >({ direct: "", proxy: "", vpn_underlay: "", bootstrap: "" });

  useEffect(() => {
    if (desired) setProfile(structuredClone(desired));
  }, [desired]);
  useEffect(() => {
    if (data?.dns?.active) {
      const next = structuredClone(
        data.network?.desired.dns ?? data.dns.active,
      );
      setDns(next);
      setDnsText({
        direct: next.direct.join("\n"),
        proxy: next.proxy.join("\n"),
        vpn_underlay: next.vpn_underlay.join("\n"),
        bootstrap: next.bootstrap.join("\n"),
      });
    }
  }, [data?.dns, data?.network]);

  if (!profile || !dns || !data?.network)
    return <p className="empty-state">Сетевой профиль недоступен.</p>;
  const profileChanged = !sameConfig(profile, data.network.desired);
  const dnsDraft = {
    ...dns,
    direct: lines(dnsText.direct),
    proxy: lines(dnsText.proxy),
    vpn_underlay: lines(dnsText.vpn_underlay),
    bootstrap: lines(dnsText.bootstrap),
  };
  const dnsChanged = !sameConfig(
    dnsDraft,
    data.network.desired.dns ?? data.dns?.active,
  );
  const interfaces = data.interfaces.filter((item) => !item.loopback);
  const patchRole = (
    role: "direct" | "vpn_underlay",
    key: "interface" | "gateway" | "source",
    value: string,
  ) =>
    setProfile((current) =>
      current
        ? {
            ...current,
            roles: {
              ...current.roles,
              [role]: { ...current.roles[role], [key]: value.trim() || null },
            },
          }
        : current,
    );
  const patchCapture = <K extends keyof NetworkProfile["capture"]>(
    key: K,
    value: NetworkProfile["capture"][K],
  ) =>
    setProfile((current) =>
      current
        ? { ...current, capture: { ...current.capture, [key]: value } }
        : current,
    );
  const patchDns = <K extends keyof DnsConfig>(key: K, value: DnsConfig[K]) =>
    setDns((current) => (current ? { ...current, [key]: value } : current));

  const saveProfile = () =>
    run(async () => {
      await actions.validateNetwork(profile);
      await actions.saveNetwork(data.network!.desired.revision, profile);
    }, "Сетевой профиль проверен и сохранён как желаемый. Действующая сеть пока не менялась.");
  const saveDns = () =>
    run(
      async () => {
        await actions.validateDns(dnsDraft);
        await actions.saveDns(data.network!.desired.revision, dnsDraft);
      },
      "DNS проверен и сохранён. Нажмите «Применить», чтобы активировать изменения.",
      { title: "Применяем настройки" },
    );
  return (
    <div className="settings-section">
      <Heading
        eyebrow="Интерфейсы"
        title="Каналы подключения"
        text="Прямой выход используется правилами Direct. Канал к VPN-серверам нужен самому OrcheRoute для подключения к узлам; при одном WAN они обычно совпадают."
      />
      <div className="role-cards">
        {(["direct", "vpn_underlay"] as const).map((role) => (
          <div className="role-card" key={role}>
            <strong>
              {role === "direct" ? "Прямой выход" : "Канал к VPN-серверам"}
            </strong>
            <Field label="Интерфейс">
              <select
                value={profile.roles[role].interface}
                onChange={(event) =>
                  patchRole(role, "interface", event.target.value)
                }
              >
                {interfaces.map((item) => (
                  <option key={item.name} value={item.name}>
                    {item.name} · {item.state}
                  </option>
                ))}
              </select>
            </Field>
            <div className="form-grid two">
              <Field label="Шлюз" hint="Пусто = автоматически">
                <input
                  value={profile.roles[role].gateway ?? ""}
                  onChange={(event) =>
                    patchRole(role, "gateway", event.target.value)
                  }
                  placeholder="Автоматически"
                />
              </Field>
              <Field label="Исходный IP" hint="Пусто = автоматически">
                <input
                  value={profile.roles[role].source ?? ""}
                  onChange={(event) =>
                    patchRole(role, "source", event.target.value)
                  }
                  placeholder="Автоматически"
                />
              </Field>
            </div>
          </div>
        ))}
      </div>
      <Heading
        eyebrow="Захват"
        title="Область действия"
        text="Сохранение не меняет сеть. Применение выполняется отдельной кнопкой и может быть заблокировано активным чужим TUN."
        compact
      />
      <div className="mode-choice-grid capture-modes">
        <ModeCard
          selected={profile.capture.mode === "interfaces"}
          title="По интерфейсам"
          text="Через VPN идут устройства только с выбранных LAN-интерфейсов."
          onClick={() => patchCapture("mode", "interfaces")}
        />
        <ModeCard
          selected={profile.capture.mode === "system"}
          title="Вся система"
          text="OrcheRoute перехватывает локальный и маршрутизируемый трафик устройства."
          onClick={() => patchCapture("mode", "system")}
          warning
        />
      </div>
      {profile.capture.mode === "interfaces" && (
        <InterfacePicker
          interfaces={interfaces}
          value={profile.capture.interfaces}
          onChange={(value) => patchCapture("interfaces", value)}
        />
      )}
      <div className="capture-list-grid">
        <ListEditor
          title="Исключения из VPN"
          technical="Bypass CIDR"
          description="Эти адреса и сети всегда идут напрямую, даже когда захват включён."
          values={profile.capture.bypass_cidrs}
          onChange={(value) => patchCapture("bypass_cidrs", value)}
          placeholder="Например, 192.168.50.0/24"
        />
        <ListEditor
          title="Доступ к управлению"
          technical="Management CIDR"
          description={
            profile.capture.mode === "system"
              ? "Обязательный список для системного режима. Добавьте сеть, из которой подключаетесь по SSH или открываете панель."
              : "Адреса администрирования, которые нельзя перехватывать VPN."
          }
          values={profile.capture.management_cidrs}
          onChange={(value) => patchCapture("management_cidrs", value)}
          placeholder="Например, 10.42.0.0/24"
          required={profile.capture.mode === "system"}
        />
      </div>
      <div className="toggle-grid">
        <Toggle
          checked={profile.capture.bypass_local}
          onChange={(value) => patchCapture("bypass_local", value)}
          label="Обход локальных сетей"
        />
        <Toggle
          checked={profile.capture.dns_hijack}
          onChange={(value) => patchCapture("dns_hijack", value)}
          label="Защита DNS · TCP/UDP 53"
        />
        <Toggle
          checked={profile.capture.strict_route}
          onChange={(value) => patchCapture("strict_route", value)}
          label="Строгая маршрутизация"
        />
      </div>
      {(!profile.capture.dns_hijack || !profile.capture.strict_route) && (
        <div className="context-note danger">
          <strong>DNS может обойти защищённый канал</strong>
        </div>
      )}
      <ActionBar>
        <span>
          Профиль:{" "}
          {data.network.in_sync ? "применён" : "есть неприменённые изменения"}
        </span>
        <button
          className="secondary-button"
          type="button"
          disabled={busy || !profileChanged}
          onClick={() => void saveProfile()}
        >
          Сохранить
        </button>
      </ActionBar>
      <Heading
        eyebrow="DNS"
        title="Раздельные резолверы"
        text="Каждый канал автоматически привязывается к соответствующему outbound."
        compact
      />
      <div className="form-grid two">
        <Field label="Direct DNS" hint="По одному в строке">
          <textarea
            rows={3}
            value={dnsText.direct}
            onChange={(event) =>
              setDnsText((current) => ({
                ...current,
                direct: event.target.value,
              }))
            }
          />
        </Field>
        <Field label="Proxy DNS" hint="DoH/DoT/UDP">
          <textarea
            rows={3}
            value={dnsText.proxy}
            onChange={(event) =>
              setDnsText((current) => ({
                ...current,
                proxy: event.target.value,
              }))
            }
          />
        </Field>
        <Field label="DNS для подключения к VPN">
          <textarea
            rows={3}
            value={dnsText.vpn_underlay}
            onChange={(event) =>
              setDnsText((current) => ({
                ...current,
                vpn_underlay: event.target.value,
              }))
            }
          />
        </Field>
        <Field label="Bootstrap" hint="Только IPv4">
          <textarea
            rows={3}
            value={dnsText.bootstrap}
            onChange={(event) =>
              setDnsText((current) => ({
                ...current,
                bootstrap: event.target.value,
              }))
            }
          />
        </Field>
        <Field label="Кеш">
          <select
            value={dns.cache_algorithm}
            onChange={(event) =>
              patchDns("cache_algorithm", event.target.value as "arc" | "lru")
            }
          >
            <option value="arc">ARC</option>
            <option value="lru">LRU</option>
          </select>
        </Field>
      </div>
      <div className="toggle-grid">
        <Toggle
          checked={dns.prefer_h3}
          onChange={(value) => patchDns("prefer_h3", value)}
          label="Предпочитать HTTP/3"
        />
        <Toggle
          checked={dns.use_hosts}
          onChange={(value) => patchDns("use_hosts", value)}
          label="Использовать hosts"
        />
      </div>
      <ActionBar>
        <span>
          DNS: {data.dns?.in_sync ? "применён" : "ожидает применения"}
        </span>
        <button
          className="secondary-button"
          type="button"
          disabled={busy || !dnsChanged}
          onClick={() => void saveDns()}
        >
          Сохранить
        </button>
      </ActionBar>
    </div>
  );
}

function AccessPanel({
  data,
  busy,
}: {
  data: DashboardData | null;
  busy: boolean;
}) {
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [confirmation, setConfirmation] = useState("");
  const [tlsMode, setTlsMode] = useState<"auto" | "custom" | "disabled">(
    "auto",
  );
  const [certPath, setCertPath] = useState("");
  const [keyPath, setKeyPath] = useState("");
  const [certificateName, setCertificateName] = useState("");
  const [message, setMessage] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [saving, setSaving] = useState(false);
  useEffect(
    () => setUsername(data?.access?.username ?? ""),
    [data?.access?.username],
  );
  useEffect(() => {
    if (!data?.access) return;
    setTlsMode(data.access.https.mode);
    setCertPath(data.access.https.cert_path);
    setKeyPath(data.access.https.key_path);
    setCertificateName(data.access.https.certificate_name ?? "");
  }, [data?.access]);
  if (!data?.access)
    return (
      <div className="settings-section">
        <Heading
          eyebrow="Доступ"
          title="Параметры панели недоступны"
          text="Шлюз не вернул конфигурацию доступа. Обновите страницу или проверьте службу OrcheRoute Web."
        />
      </div>
    );
  const saveCredentials = async () => {
    setError(null);
    setMessage(null);
    if (password !== confirmation) {
      setError("Пароли не совпадают.");
      return;
    }
    setSaving(true);
    try {
      await actions.updateWebAccess(username.trim(), password);
      setMessage(
        "Данные сохранены. Сейчас браузер заново запросит логин и пароль.",
      );
      window.setTimeout(() => window.location.reload(), 1400);
    } catch (reason) {
      setError(errorText(reason));
      setSaving(false);
    }
  };
  const saveTls = async () => {
    setSaving(true);
    setError(null);
    setMessage(null);
    try {
      await actions.updateWebTls({
        mode: tlsMode,
        ...(tlsMode === "custom"
          ? {
              cert_path: certPath.trim(),
              key_path: keyPath.trim(),
              certificate_name: certificateName.trim(),
            }
          : {}),
      });
      setMessage(
        "TLS сохранён. Веб-служба перезапускается, страница обновится автоматически.",
      );
      window.setTimeout(() => window.location.reload(), 2200);
    } catch (reason) {
      setError(errorText(reason));
      setSaving(false);
    }
  };
  const lanAddresses = data.access.addresses.filter(
    (item) => item.interface !== "lo",
  );
  return (
    <div className="settings-section access-settings">
      <Heading
        eyebrow="Доступ"
        title="Авторизация панели"
        text="Эти данные используются при открытии WebUI из браузера. После изменения потребуется войти заново."
      />
      <div className="access-panel credential-editor">
        <div className="form-grid two">
          <Field label="Логин">
            <input
              autoComplete="username"
              value={username}
              onChange={(event) => setUsername(event.target.value)}
            />
          </Field>
          <Field label="Новый пароль" hint="Минимум 12 символов">
            <input
              type="password"
              autoComplete="new-password"
              value={password}
              onChange={(event) => setPassword(event.target.value)}
            />
          </Field>
          <Field label="Повторите пароль">
            <input
              type="password"
              autoComplete="new-password"
              value={confirmation}
              onChange={(event) => setConfirmation(event.target.value)}
            />
          </Field>
        </div>
        {(message || error) && (
          <div className={`inline-feedback ${error ? "error" : "success"}`}>
            {error ?? message}
          </div>
        )}
        <ActionBar>
          <span>Пароль хранится только в виде PBKDF2-SHA256-хеша.</span>
          <button
            className="primary-button"
            type="button"
            disabled={
              busy ||
              saving ||
              !username.trim() ||
              password.length < 12 ||
              password !== confirmation
            }
            onClick={() => void saveCredentials()}
          >
            {saving ? "Сохраняем…" : "Сохранить авторизацию"}
          </button>
        </ActionBar>
      </div>
      <Heading
        eyebrow="TLS"
        title="Шифрование WebUI"
        text="Локальный сертификат работает без домена. После установки локального CA устройство будет доверять автоматически перевыпускаемым сертификатам OrcheRoute."
        compact
      />
      <div className="mode-choice-grid capture-modes">
        <ModeCard
          selected={tlsMode === "auto"}
          title="Локальный CA"
          text="Автоматический сертификат для hostname и IP-адресов устройства. Рекомендуется для LAN и автономной установки."
          onClick={() => setTlsMode("auto")}
        />
        <ModeCard
          selected={tlsMode === "custom"}
          title="Свой сертификат"
          text="Сертификат от ACME, организации или reverse proxy, уже размещённый на устройстве."
          onClick={() => setTlsMode("custom")}
        />
        <ModeCard
          selected={tlsMode === "disabled"}
          title="Без HTTPS"
          text="Оставляет только HTTP. Используйте лишь на loopback или в доверенной изолированной сети."
          onClick={() => setTlsMode("disabled")}
          warning
        />
      </div>
      {tlsMode === "custom" && (
        <div className="access-panel">
          <div className="form-grid two">
            <Field label="Путь к сертификату">
              <input
                value={certPath}
                onChange={(event) => setCertPath(event.target.value)}
                placeholder="/etc/letsencrypt/live/example/fullchain.pem"
              />
            </Field>
            <Field label="Путь к закрытому ключу">
              <input
                value={keyPath}
                onChange={(event) => setKeyPath(event.target.value)}
                placeholder="/etc/letsencrypt/live/example/privkey.pem"
              />
            </Field>
            <Field label="Имя сертификата" hint="Для ссылки в интерфейсе">
              <input
                value={certificateName}
                onChange={(event) => setCertificateName(event.target.value)}
                placeholder="router.example.org"
              />
            </Field>
          </div>
        </div>
      )}
      <div
        className={`context-note ${data.access.https.error ? "danger" : ""}`}
      >
        <strong>
          {data.access.https.enabled
            ? `HTTPS включён · ${data.access.https.mode === "auto" ? "локальный CA" : "свой сертификат"}`
            : "HTTPS выключен"}
        </strong>
        <span>
          {data.access.https.error
            ? `Ошибка: ${data.access.https.error}`
            : data.access.https.certificate_name
              ? `Сертификат: ${data.access.https.certificate_name}`
              : "После применения веб-служба автоматически перезапустится."}
        </span>
        {data.access.https.ca_download_url && (
          <a className="inline-link" href={data.access.https.ca_download_url}>
            Скачать корневой сертификат OrcheRoute
          </a>
        )}
      </div>
      <ActionBar>
        <span>Настройка относится только к WebUI и не перезапускает VPN.</span>
        <button
          className="secondary-button"
          type="button"
          disabled={
            busy ||
            saving ||
            (tlsMode === "custom" && (!certPath.trim() || !keyPath.trim()))
          }
          onClick={() => void saveTls()}
        >
          {saving ? "Применяем…" : "Применить TLS"}
        </button>
      </ActionBar>
      <Heading
        eyebrow="Сети"
        title="Доступные адреса"
        text={`Панель принимает подключения только из применённых Management CIDR: ${data.access.management_cidrs.join(", ") || "только loopback"}.`}
        compact
      />
      <div className="access-addresses">
        {data.access.https.canonical_url && (
          <a
            href={data.access.https.canonical_url}
            target="_blank"
            rel="noreferrer"
          >
            <span>Защищённый адрес</span>
            <strong>{data.access.https.canonical_url}</strong>
            <small>Сертификат: {data.access.https.certificate_name}</small>
          </a>
        )}
        {lanAddresses.map((item) => (
          <a
            href={
              item.https_url && item.certificate_matches
                ? item.https_url
                : item.http_url
            }
            key={`${item.interface}-${item.address}`}
            target="_blank"
            rel="noreferrer"
          >
            <span>{item.interface}</span>
            <strong>
              {item.https_url && item.certificate_matches
                ? item.https_url
                : item.http_url}
            </strong>
            <small>{item.cidr}</small>
          </a>
        ))}
        {!lanAddresses.length && (
          <p className="empty-state">
            В применённых сетях управления пока нет адресов интерфейсов.
          </p>
        )}
      </div>
    </div>
  );
}

type RouteTarget = "direct" | "proxy" | "block";
const ROUTE_TARGETS: RouteTarget[] = ["direct", "proxy", "block"];
const ROUTE_LABELS: Record<
  RouteTarget,
  { title: string; short: string; text: string }
> = {
  direct: {
    title: "Напрямую",
    short: "Direct",
    text: "В обход VPN через прямой выход",
  },
  proxy: {
    title: "Через VPN",
    short: "Proxy",
    text: "Через выбранный активный сервер",
  },
  block: {
    title: "Блокировать",
    short: "Block",
    text: "Отклонять совпавшие соединения",
  },
};
const GEOSITE_OPTIONS = [
  "private",
  "cn",
  "geolocation-!cn",
  "category-ads-all",
  "category-games",
  "tracker",
  "youtube",
  "google",
  "github",
  "microsoft",
  "onedrive",
  "apple",
  "openai",
  "telegram",
  "twitter",
  "netflix",
  "spotify",
  "tiktok",
  "steam",
  "proxy",
  "proxymedia",
  "biliintl",
];
const GEOIP_SPECIAL = [
  "private",
  "cloudflare",
  "cloudfront",
  "facebook",
  "fastly",
  "google",
  "netflix",
  "telegram",
  "twitter",
];
const GEOIP_OPTIONS = [
  ...GEOIP_SPECIAL.map((value) => ({
    value: value.toUpperCase(),
    label: value === "private" ? "Локальные и частные сети" : value,
  })),
  ...COUNTRY_CODES.map((value) => ({
    value,
    label: regionNames?.of(value) ?? value,
  })),
];
const GEOSITE_PICKER_OPTIONS = GEOSITE_OPTIONS.map((value) => ({
  value,
  label: value,
}));
const TRAFFIC_PRESET_OPTIONS = [
  { value: "tcp:*", label: "TCP · весь трафик" },
  { value: "udp:*", label: "UDP · весь трафик" },
  { value: "preset:http", label: "HTTP · TCP 80, 8080, 8000–8008" },
  { value: "preset:https", label: "HTTPS / TLS · TCP 443, 8443" },
  { value: "preset:quic", label: "QUIC / HTTP3 · UDP 443, 8443" },
  { value: "preset:dns", label: "DNS · TCP/UDP 53" },
  { value: "preset:dot", label: "DNS over TLS · TCP 853" },
  { value: "preset:doq", label: "DNS over QUIC · UDP 853" },
  { value: "preset:torrent", label: "BitTorrent · типовые TCP/UDP 6881–6889" },
  { value: "preset:ssh", label: "SSH · TCP 22" },
  { value: "preset:ftp", label: "FTP · TCP 20–21" },
  { value: "preset:mail", label: "Почта · SMTP, IMAP, POP3" },
  { value: "preset:ntp", label: "NTP · UDP 123" },
  { value: "preset:stun", label: "STUN / TURN · UDP 3478–3481" },
  { value: "preset:rdp", label: "RDP · TCP/UDP 3389" },
  { value: "preset:openvpn", label: "OpenVPN · типовой порт 1194" },
  { value: "preset:wireguard", label: "WireGuard · типовой порт 51820" },
];
const isPortRule = (value: string) => /^:/.test(value) || /^any:/i.test(value);
const isProtocolRule = (value: string) => /^(tcp|udp):\*$/i.test(value);
const isPresetRule = (value: string) => /^preset:/i.test(value);
const isGeoIpRule = (value: string) => /^geoip:/i.test(value);
const isGeoSiteRule = (value: string) => /^geosite:/i.test(value);
const isStructuredRoute = (value: string) =>
  isPortRule(value) ||
  isProtocolRule(value) ||
  isPresetRule(value) ||
  isGeoIpRule(value) ||
  isGeoSiteRule(value);

const RulePicker = memo(function RulePicker({
  title,
  hint,
  values,
  options,
  onChange,
  allowCustom = false,
}: {
  title: string;
  hint: string;
  values: string[];
  options: { value: string; label: string }[];
  onChange: (values: string[]) => void;
  allowCustom?: boolean;
}) {
  const [query, setQuery] = useState("");
  const [custom, setCustom] = useState("");
  const [open, setOpen] = useState(false);
  const normalizedQuery = query.trim().toLocaleLowerCase("ru");
  const matches = useMemo(
    () =>
      options.filter(
        (item) =>
          !normalizedQuery ||
          `${item.label} ${item.value}`
            .toLocaleLowerCase("ru")
            .includes(normalizedQuery),
      ),
    [normalizedQuery, options],
  );
  const visibleLimit = normalizedQuery ? 250 : 120;
  const visible = useMemo(
    () => matches.slice(0, visibleLimit),
    [matches, visibleLimit],
  );
  const selected = useMemo(() => new Set(values), [values]);
  const toggle = (value: string) =>
    onChange(
      values.includes(value)
        ? values.filter((item) => item !== value)
        : [...values, value],
    );
  const addCustom = () => {
    const value = custom.trim().toLowerCase();
    if (value && /^[a-z0-9_@.+!-]+$/.test(value)) {
      onChange([...new Set([...values, value])]);
      setCustom("");
    }
  };
  useEffect(() => {
    if (!open) return;
    const close = (event: KeyboardEvent) => {
      if (event.key === "Escape") setOpen(false);
    };
    window.addEventListener("keydown", close);
    return () => window.removeEventListener("keydown", close);
  }, [open]);
  return (
    <div className="route-picker">
      <div className="route-picker-head">
        <div>
          <strong>{title}</strong>
          <small>{hint}</small>
        </div>
        <span>{values.length || "Не выбрано"}</span>
      </div>
      {values.length > 0 && (
        <div className="route-tags">
          {values.map((value) => (
            <button type="button" key={value} onClick={() => toggle(value)}>
              <span>
                {options.find((item) => item.value === value)?.label ?? value}
              </span>
              ×
            </button>
          ))}
        </div>
      )}
      <button
        className="picker-trigger"
        type="button"
        onClick={() => setOpen(true)}
      >
        Выбрать из списка<span>Открыть</span>
      </button>
      {open && (
        <div
          className="picker-dialog-backdrop"
          role="presentation"
          onMouseDown={() => setOpen(false)}
        >
          <section
            className="picker-dialog"
            role="dialog"
            aria-modal="true"
            aria-label={title}
            onMouseDown={(event) => event.stopPropagation()}
          >
            <header>
              <div>
                <strong>{title}</strong>
                <small>
                  {values.length ? `Выбрано: ${values.length}` : hint}
                </small>
              </div>
              <button
                type="button"
                onClick={() => setOpen(false)}
                aria-label="Закрыть"
              >
                ×
              </button>
            </header>
            <div className="route-picker-menu">
              <div className="input-wrap">
                <input
                  autoFocus
                  value={query}
                  onChange={(event) => setQuery(event.target.value)}
                  placeholder="Поиск"
                />
              </div>
              <div className="route-option-list">
                {visible.map((item) => (
                  <label
                    key={item.value}
                    className={selected.has(item.value) ? "selected" : ""}
                  >
                    <input
                      type="checkbox"
                      checked={selected.has(item.value)}
                      onChange={() => toggle(item.value)}
                    />
                    <span>{item.label}</span>
                    <em>{item.value}</em>
                  </label>
                ))}
              </div>
              {matches.length > visible.length && (
                <small className="route-help">
                  Показано {visible.length} из {matches.length}. Уточните запрос
                  в поиске.
                </small>
              )}
              {allowCustom && (
                <div className="route-custom-row">
                  <div className="input-wrap">
                    <input
                      value={custom}
                      onChange={(event) => setCustom(event.target.value)}
                      onKeyDown={(event) => {
                        if (event.key === "Enter") {
                          event.preventDefault();
                          addCustom();
                        }
                      }}
                      placeholder="Другая категория"
                    />
                  </div>
                  <button
                    className="secondary-button"
                    type="button"
                    onClick={addCustom}
                    disabled={!custom.trim()}
                  >
                    Добавить
                  </button>
                </div>
              )}
            </div>
            <footer>
              <button
                className="primary-button"
                type="button"
                onClick={() => setOpen(false)}
              >
                Готово
              </button>
            </footer>
          </section>
        </div>
      )}
    </div>
  );
});

function RouteTargetEditor({
  target,
  values,
  onChange,
  geoIPOptions,
  geoSiteOptions,
}: {
  target: RouteTarget;
  values: string[];
  onChange: (values: string[]) => void;
  geoIPOptions: { value: string; label: string }[];
  geoSiteOptions: { value: string; label: string }[];
}) {
  const [port, setPort] = useState("");
  const plain = useMemo(
    () => values.filter((value) => !isStructuredRoute(value)),
    [values],
  );
  const plainSignature = plain.join("\n");
  const [plainText, setPlainText] = useState(plainSignature);
  const ports = useMemo(
    () =>
      values.filter(isPortRule).map((value) => value.replace(/^(any)?:/i, "")),
    [values],
  );
  const trafficTypes = useMemo(
    () =>
      values
        .filter((value) => isProtocolRule(value) || isPresetRule(value))
        .map((value) => value.toLowerCase()),
    [values],
  );
  const geoip = useMemo(
    () =>
      values
        .filter(isGeoIpRule)
        .map((value) => value.slice(value.indexOf(":") + 1).toUpperCase()),
    [values],
  );
  const geosite = useMemo(
    () =>
      values
        .filter(isGeoSiteRule)
        .map((value) => value.slice(value.indexOf(":") + 1).toLowerCase()),
    [values],
  );
  const replace = useCallback(
    (predicate: (value: string) => boolean, replacements: string[]) =>
      onChange([
        ...values.filter((value) => !predicate(value)),
        ...replacements,
      ]),
    [onChange, values],
  );
  useEffect(() => {
    // The modal can render once with an empty draft before the polled route
    // state arrives. Hydrate that first value without resetting active edits.
    setPlainText((current) => current || plainSignature);
  }, [plainSignature]);
  const addPort = () => {
    const normalized = port.trim().replace(/^:/, "").replace(/\s+/g, "");
    if (!normalized || !/^\d+(?:-\d+)?(?:,\d+(?:-\d+)?)*$/.test(normalized))
      return;
    replace(
      isPortRule,
      [...new Set([...ports, normalized])].map((value) => `:${value}`),
    );
    setPort("");
  };
  const updatePlainText = useCallback(
    (value: string) => {
      setPlainText(value);
      startTransition(() =>
        replace((item) => !isStructuredRoute(item), lines(value)),
      );
    },
    [replace],
  );
  return (
    <div className="route-target-editor">
      <div className="route-editor-grid">
        <div className="route-rule-card route-plain">
          <div className="route-rule-card-head">
            <div>
              <strong>Домены и адреса</strong>
              <small>Один элемент в строке</small>
            </div>
            <span>{plain.length}</span>
          </div>
          <textarea
            rows={8}
            value={plainText}
            onChange={(event) => updatePlainText(event.target.value)}
            placeholder={
              target === "direct"
                ? ".ru\nvk.com\n10.0.0.0/8"
                : target === "proxy"
                  ? "google.com\n8.8.8.8"
                  : "ads.example.com"
            }
            spellCheck={false}
          />
          <small className="route-help">
            Домен включает поддомены. Для точного совпадения используйте{" "}
            <code>=example.com</code>. Здесь также можно оставить расширенные
            правила.
          </small>
        </div>
        <div className="route-rule-card">
          <div className="route-rule-card-head">
            <div>
              <strong>Порты</strong>
              <small>Не зависят от протокола</small>
            </div>
            <span>{ports.length}</span>
          </div>
          <div className="route-custom-row">
            <div className="input-wrap">
              <input
                value={port}
                onChange={(event) => setPort(event.target.value)}
                onKeyDown={(event) => {
                  if (event.key === "Enter") {
                    event.preventDefault();
                    addPort();
                  }
                }}
                placeholder="5000,5002,5005 или 5000-6000"
              />
            </div>
            <button
              className="secondary-button"
              type="button"
              onClick={addPort}
              disabled={!port.trim()}
            >
              Добавить
            </button>
          </div>
          <div className="route-tags">
            {ports.map((value) => (
              <button
                type="button"
                key={value}
                onClick={() =>
                  replace(
                    isPortRule,
                    ports
                      .filter((item) => item !== value)
                      .map((item) => `:${item}`),
                  )
                }
              >
                <span>:{value}</span>×
              </button>
            ))}
          </div>
          <small className="route-help">
            <code>:5000</code> — любой трафик на порт; запятые задают отдельные
            порты, дефис — диапазон.
          </small>
        </div>
        <RulePicker
          title="Протоколы и типы трафика"
          hint="TCP/UDP или готовые пресеты типовых портов"
          values={trafficTypes}
          options={TRAFFIC_PRESET_OPTIONS}
          onChange={(next) =>
            replace(
              (value) => isProtocolRule(value) || isPresetRule(value),
              next,
            )
          }
        />
        <RulePicker
          title="GeoIP"
          hint="Категории из установленного GeoIP.dat"
          values={geoip}
          options={geoIPOptions}
          onChange={(next) =>
            replace(
              isGeoIpRule,
              next.map((value) => `geoip:${value}`),
            )
          }
        />
        <RulePicker
          title="GeoSite"
          hint="Категории из установленного GeoSite.dat"
          values={geosite}
          options={geoSiteOptions}
          onChange={(next) =>
            replace(
              isGeoSiteRule,
              next.map((value) => `geosite:${value}`),
            )
          }
          allowCustom
        />
      </div>
      <div className="context-note">
        <strong>О пресетах</strong>
        <span>
          HTTP, DNS, SSH и другие пункты преобразуются в правила протокола и
          типовых портов. BitTorrent может использовать случайные порты,
          шифрование и DHT, поэтому пресет 6881–6889 не гарантирует обнаружение
          всего торрент-трафика на маршрутизаторе.
        </span>
      </div>
    </div>
  );
}

function RoutesForm({
  data,
  busy,
  run,
}: {
  data: DashboardData | null;
  busy: boolean;
  run: Runner;
}) {
  const routes = data?.routes;
  const [defaultAction, setDefaultAction] =
    useState<RouteState["default"]>("proxy");
  const [activeTarget, setActiveTarget] = useState<RouteTarget>("direct");
  const [drafts, setDrafts] = useState<RouteState["lists"]>({
    direct: [],
    proxy: [],
    block: [],
  });
  useEffect(() => {
    if (routes) {
      setDefaultAction(routes.default ?? "proxy");
      setDrafts(structuredClone(routes.lists));
    }
  }, [routes]);
  const routesChanged = Boolean(
    routes &&
      (defaultAction !== routes.default || !sameConfig(drafts, routes.lists)),
  );
  const updateActiveDraft = useCallback(
    (values: string[]) =>
      setDrafts((current) => ({ ...current, [activeTarget]: values })),
    [activeTarget],
  );
  const geoIPOptions = useMemo(() => {
    const values = data?.components?.catalog?.geoip;
    if (!values?.length) return GEOIP_OPTIONS;
    return values.map((value) => ({
      value: value.toUpperCase(),
      label:
        value.length === 2
          ? (regionNames?.of(value.toUpperCase()) ?? value.toUpperCase())
          : value,
    }));
  }, [data?.components?.catalog?.geoip]);
  const geoSiteOptions = useMemo(() => {
    const values = data?.components?.catalog?.geosite;
    return values?.length
      ? values.map((value) => ({ value: value.toLowerCase(), label: value }))
      : GEOSITE_PICKER_OPTIONS;
  }, [data?.components?.catalog?.geosite]);
  const save = () =>
    routes &&
    run(
      async () => {
        await actions.validateRoutes(defaultAction, drafts);
        await actions.saveRoutes(routes.revision, defaultAction, drafts);
      },
      "Маршруты проверены и сохранены. При запущенном VPN они применяются сразу, иначе — при следующем запуске.",
      { title: "Применяем маршруты" },
    );
  return (
    <div className="settings-section routes-builder">
      <Heading
        eyebrow="Маршрутизация"
        title="Что делать с трафиком"
        text="Сначала проверяются Block → Direct → Proxy. Если ни одно правило не совпало, применяется действие по умолчанию."
      />
      <div className="route-default">
        <div className="route-block-heading">
          <div>
            <strong>Действие по умолчанию</strong>
            <small>Для всего трафика, который не совпал с правилами ниже</small>
          </div>
        </div>
        <div className="mode-choice-grid route-default-grid">
          {ROUTE_TARGETS.map((target) => (
            <ModeCard
              key={target}
              selected={defaultAction === target}
              title={ROUTE_LABELS[target].title}
              text={ROUTE_LABELS[target].text}
              onClick={() => setDefaultAction(target)}
              warning={target === "block"}
            />
          ))}
        </div>
      </div>
      <div className="route-target-tabs">
        {ROUTE_TARGETS.map((target) => (
          <button
            type="button"
            className={activeTarget === target ? "active" : ""}
            key={target}
            onClick={() => setActiveTarget(target)}
          >
            <span>{ROUTE_LABELS[target].short}</span>
            <strong>{drafts[target].length}</strong>
          </button>
        ))}
      </div>
      <div className="route-active-heading">
        <div>
          <strong>{ROUTE_LABELS[activeTarget].title}</strong>
          <small>{ROUTE_LABELS[activeTarget].text}</small>
        </div>
      </div>
      <RouteTargetEditor
        key={`${activeTarget}-${routes?.revision ?? 0}`}
        target={activeTarget}
        values={drafts[activeTarget]}
        onChange={updateActiveDraft}
        geoIPOptions={geoIPOptions}
        geoSiteOptions={geoSiteOptions}
      />
      <ActionBar>
        <span>
          Ревизия {routes?.revision ?? "—"} · по умолчанию{" "}
          {ROUTE_LABELS[defaultAction].short}
        </span>
        <button
          className="primary-button"
          type="button"
          disabled={busy || !routes || !routesChanged}
          onClick={() => void save()}
        >
          Сохранить
        </button>
      </ActionBar>
    </div>
  );
}

function SubscriptionsForm({
  data,
  busy,
  run,
}: {
  data: DashboardData | null;
  busy: boolean;
  run: Runner;
}) {
  const [editing, setEditing] = useState<Subscription | null>(null);
  const [adding, setAdding] = useState(false);
  const [exporting, setExporting] = useState<Subscription | null>(null);
  const [deleting, setDeleting] = useState<Subscription | null>(null);
  const [copiedID, setCopiedID] = useState<string | null>(null);
  const androidRuntime = isAndroidRuntime();
  const subscriptionUpdate = data?.operations?.subscription_update;
  const updateRunning = Boolean(
    subscriptionUpdate?.active ||
    ["queued", "running"].includes(subscriptionUpdate?.status ?? ""),
  );
  const changesLocked = busy || updateRunning;
  const defaults = (data?.subscriptions ?? []).filter(
    (subscription) => subscription.builtin_default,
  );
  const custom = (data?.subscriptions ?? []).filter(
    (subscription) => !subscription.builtin_default,
  );
  const [selectedDefaults, setSelectedDefaults] = useState<string[]>([]);
  useEffect(() => {
    setSelectedDefaults(
      defaults
        .filter((subscription) => subscription.enabled)
        .map((subscription) => subscription.id),
    );
  }, [data?.subscriptions]);
  const toggleDefault = (id: string) =>
    setSelectedDefaults((current) =>
      current.includes(id)
        ? current.filter((value) => value !== id)
        : [...current, id],
    );
  const savedDefaults = defaults
    .filter((subscription) => subscription.enabled)
    .map((subscription) => subscription.id)
    .sort();
  const defaultsChanged =
    JSON.stringify([...selectedDefaults].sort()) !==
    JSON.stringify(savedDefaults);
  return (
    <div className="settings-section">
      <div className="section-heading with-action">
        <div>
          <span>Источники</span>
          <h3>Подписки</h3>
        </div>
        <div className="heading-actions">
          <button
            className="secondary-button"
            type="button"
            disabled={changesLocked}
            onClick={() =>
              void run(
                actions.refreshSubscriptions,
                "Подписки обновлены. Сохранённые серверы не удалены.",
                { title: "Обновляем подписки", waitFor: "subscriptions" },
              )
            }
          >
            Обновить подписки
          </button>
          <button
            className="secondary-button"
            type="button"
            disabled={changesLocked}
            onClick={() =>
              void run(
                actions.checkServers,
                "Проверка завершена, рабочие списки серверов обновлены.",
                { title: "Ищем доступные серверы", waitFor: "servers" },
              )
            }
          >
            Проверить серверы
          </button>
          <button
            className="primary-button"
            type="button"
            disabled={changesLocked}
            onClick={() => {
              setEditing(null);
              setAdding(true);
            }}
          >
            Добавить
          </button>
        </div>
      </div>
      {updateRunning && (
        <div className="context-note">
          <strong>Обновление уже выполняется</strong>
          <span>
            {subscriptionUpdate?.message ??
              "Подписки и серверы проверяются. Новое обновление не будет запущено; дождитесь завершения текущего."}
          </span>
        </div>
      )}
      <div className="default-source-card">
        <div className="default-source-head">
          <div>
            <strong>Встроенный аварийный список серверов</strong>
            <small>Скачиваются и проверяются только отмеченные источники</small>
          </div>
          <span>
            {selectedDefaults.length} из {defaults.length}
          </span>
        </div>
        <div className="default-source-list">
          {defaults.map((subscription) => (
            <label
              key={subscription.id}
              className={
                selectedDefaults.includes(subscription.id) ? "selected" : ""
              }
            >
              <input
                type="checkbox"
                checked={selectedDefaults.includes(subscription.id)}
                disabled={changesLocked}
                onChange={() => toggleDefault(subscription.id)}
              />
              <span>
                <strong>{subscription.name}</strong>
                <em>
                  {subscription.enabled
                    ? `${subscription.last_links} ссылок · ${subscription.last_status}`
                    : "не загружается"}
                </em>
              </span>
              <a
                href={subscription.repository}
                target="_blank"
                rel="noreferrer"
                onClick={(event) => event.stopPropagation()}
              >
                GitHub ↗
              </a>
            </label>
          ))}
        </div>
        <ActionBar>
          <span>Снятый флажок исключает источник из загрузки и проверки.</span>
          <button
            className="primary-button"
            type="button"
            disabled={changesLocked || !defaultsChanged}
            onClick={() =>
              void run(
                () => actions.updateDefaultEmergency(selectedDefaults),
                androidRuntime
                  ? "Набор аварийных источников сохранён. Для загрузки нажмите «Обновить»."
                  : "Набор аварийных источников сохранён, список серверов пересобран.",
                androidRuntime
                  ? { title: "Сохраняем аварийные источники" }
                  : { title: "Обновляем аварийный список серверов", waitFor: "subscriptions" },
              )
            }
          >
            Применить набор
          </button>
        </ActionBar>
      </div>
      {(adding || editing) && (
        <SubscriptionEditor
          item={editing}
          busy={changesLocked}
          onCancel={() => {
            setAdding(false);
            setEditing(null);
          }}
          onSave={async (payloads) => {
            const payload = payloads[0];
            const needsPoolUpdate = Boolean(
              editing &&
              (payload.group !== editing.group ||
                payload.parser !== editing.parser ||
                Boolean(payload.secret?.trim())),
            );
            const ok = await run(
              async () => {
                if (editing)
                  return actions.updateSubscription(editing.id, payload);
                return actions.importSubscriptions(payloads);
              },
              editing
                ? needsPoolUpdate
                  ? "Подписка сохранена. Для проверки нажмите «Обновить»."
                  : "Настройки подписки сохранены без повторной проверки списка серверов."
                : `Добавлено источников: ${payloads.length}. Для проверки нажмите «Обновить».`,
              { title: editing ? "Сохраняем подписку" : "Добавляем источники" },
            );
            if (ok) {
              setAdding(false);
              setEditing(null);
            }
          }}
        />
      )}
      {exporting && (
        <SubscriptionExportDialog
          item={exporting}
          onClose={() => setExporting(null)}
        />
      )}
      {deleting && (
        <div
          className="picker-dialog-backdrop"
          role="presentation"
          onMouseDown={() => !busy && setDeleting(null)}
        >
          <section
            className="picker-dialog subscription-delete-dialog"
            role="alertdialog"
            aria-modal="true"
            aria-labelledby="delete-subscription-title"
            onMouseDown={(event) => event.stopPropagation()}
          >
            <header>
              <div>
                <strong id="delete-subscription-title">Удалить подписку?</strong>
                <small>Это действие нельзя отменить</small>
              </div>
              <button type="button" disabled={busy} onClick={() => setDeleting(null)} aria-label="Закрыть">×</button>
            </header>
            <div className="subscription-delete-body">
              <p>«{deleting.name}» и сохранённые для неё серверы будут удалены.</p>
              <small>Рабочий список останется действующим до следующей проверки серверов.</small>
            </div>
            <footer>
              <button className="secondary-button" type="button" disabled={busy} onClick={() => setDeleting(null)}>Отмена</button>
              <button
                className="danger-button"
                type="button"
                disabled={busy}
                onClick={() =>
                  void (async () => {
                    const ok = await run(
                      () => actions.deleteSubscription(deleting.id),
                    "Подписка удалена. Список серверов будет пересобран при следующей проверке.",
                      { title: "Удаляем подписку" },
                    );
                    if (ok) setDeleting(null);
                  })()
                }
              >
                Удалить
              </button>
            </footer>
          </section>
        </div>
      )}
      <div className="subscription-list editable">
        {custom.map((subscription) => (
          <article className="subscription-row" key={subscription.id}>
            <span
              className={`node-status ${subscription.enabled && subscription.last_status !== "error" ? "alive" : ""}`}
            />
            <div className="subscription-main">
              <strong>{subscription.name}</strong>
              <small>
                {subscription.group === "primary" ? "Основная" : "Аварийная"} ·{" "}
                {subscription.parser === "inline"
                  ? "готовые серверы"
                  : subscription.parser === "wireguard"
                    ? "WireGuard / AmneziaWG"
                    : subscription.parser === "blacktemple"
                      ? "автоматически · BlackTemple"
                      : "автоматически"}{" "}
                · {subscription.last_links} ссылок · каждые{" "}
                {Math.round(subscription.interval_seconds / 60)} мин
              </small>
              {Boolean(subscription.last_attempt) && (
                <small className="subscription-check-result">
                  Последняя проверка: {subscription.last_tested ?? subscription.last_links} · доступно {subscription.last_available ?? "—"}
                </small>
              )}
              <div className="row-actions">
                <button
                  type="button"
                  disabled={changesLocked || !subscription.enabled}
                  onClick={() =>
                    void run(
                      () => actions.checkSubscription(subscription.id),
                      `Сохранённые серверы «${subscription.name}» проверены без обновления подписки.`,
                      { title: "Проверяем подписку", waitFor: "servers" },
                    )
                  }
                >
                  Проверить
                </button>
                <button
                  type="button"
                  disabled={changesLocked || !subscription.enabled}
                  onClick={() =>
                    void run(
                      () => actions.refreshSubscription(subscription.id),
                      `Подписка «${subscription.name}» обновлена и проверена.`,
                      { title: "Обновляем подписку", waitFor: "subscriptions" },
                    )
                  }
                >
                  Обновить
                </button>
                <button
                  type="button"
                  disabled={busy}
                  onClick={async () => {
                    try {
                      const result = await actions.revealSubscriptionSecret(
                        subscription.id,
                      );
                      await copyText(result.secret);
                      setCopiedID(subscription.id);
                      window.setTimeout(
                        () =>
                          setCopiedID((current) =>
                            current === subscription.id ? null : current,
                          ),
                        1600,
                      );
                    } catch {
                      setCopiedID(null);
                    }
                  }}
                >
                  {copiedID === subscription.id
                    ? "Скопировано"
                    : "Копировать ссылку"}
                </button>
                <button
                  type="button"
                  disabled={busy}
                  onClick={() => setExporting(subscription)}
                >
                  Экспорт
                </button>
                <button
                  type="button"
                  disabled={changesLocked}
                  onClick={() =>
                    void run(
                      () =>
                        actions.updateSubscription(subscription.id, {
                          enabled: !subscription.enabled,
                        }),
                      subscription.enabled
                        ? "Подписка выключена."
                        : "Подписка включена. Для проверки нажмите «Обновить».",
                      {
                        title: subscription.enabled
                          ? "Выключаем подписку"
                          : "Включаем подписку",
                      },
                    )
                  }
                >
                  {subscription.enabled ? "Выключить" : "Включить"}
                </button>
                <button
                  type="button"
                  disabled={changesLocked}
                  onClick={() => {
                    setAdding(false);
                    setEditing(subscription);
                  }}
                >
                  Изменить
                </button>
                <button
                  className="danger"
                  type="button"
                  disabled={changesLocked}
                  onClick={() => setDeleting(subscription)}
                >
                  Удалить
                </button>
              </div>
            </div>
          </article>
        ))}
      </div>
    </div>
  );
}

async function copyText(value: string) {
  if (navigator.clipboard && window.isSecureContext) {
    await navigator.clipboard.writeText(value);
    return;
  }
  const field = document.createElement("textarea");
  field.value = value;
  field.setAttribute("readonly", "");
  field.style.position = "fixed";
  field.style.opacity = "0";
  document.body.appendChild(field);
  field.select();
  const copied = document.execCommand("copy");
  field.remove();
  if (!copied)
    throw new Error(
      "Не удалось скопировать ссылку. Откройте панель через HTTPS или SSH-туннель.",
    );
}

function ComponentsForm({
  data,
  busy,
  run,
}: {
  data: DashboardData | null;
  busy: boolean;
  run: Runner;
}) {
  const components = data?.components;
  const embedded = isAndroidRuntime();
  const [geoEnabled, setGeoEnabled] = useState(true);
  const [geoInterval, setGeoInterval] = useState(24);
  const [geoSource, setGeoSource] = useState("metacubex");
  const [geoIPURL, setGeoIPURL] = useState("");
  const [geoSiteURL, setGeoSiteURL] = useState("");
  const [appUpdate, setAppUpdate] = useState(() =>
    embedded ? getAndroidAppUpdateStatus() : null,
  );
	const [betaWarning, setBetaWarning] = useState(false);
  useEffect(() => {
    if (!embedded) return;
    const refresh = () => setAppUpdate(getAndroidAppUpdateStatus());
    refresh();
    const timer = window.setInterval(refresh, 750);
    return () => window.clearInterval(timer);
  }, [embedded]);
  useEffect(() => {
    if (components) {
      setGeoEnabled(components.auto_update);
      setGeoInterval(components.interval_hours);
      setGeoSource(components.geo_source || "metacubex");
      setGeoIPURL(components.geoip_url ?? "");
      setGeoSiteURL(components.geosite_url ?? "");
    }
  }, [
    components?.auto_update,
    components?.interval_hours,
    components?.geo_source,
    components?.geoip_url,
    components?.geosite_url,
  ]);
  const settingsPayload = {
    geo_auto_update: geoEnabled,
    geo_interval_hours: geoInterval,
    geo_source: geoSource,
    ...(geoSource === "custom"
      ? { geoip_url: geoIPURL.trim(), geosite_url: geoSiteURL.trim() }
      : {}),
  };
  const sourceChanged = Boolean(
    components &&
    (geoSource !== components.geo_source ||
      (geoSource === "custom" &&
        (geoIPURL.trim() !== (components.geoip_url ?? "") ||
          geoSiteURL.trim() !== (components.geosite_url ?? "")))),
  );
  const scheduleChanged = Boolean(
    components &&
    (geoEnabled !== components.auto_update ||
      geoInterval !== components.interval_hours),
  );
  const geoSettingsChanged = sourceChanged || scheduleChanged;
  const customSourceValid =
    geoSource !== "custom" ||
    (geoIPURL.trim().startsWith("https://") &&
      geoSiteURL.trim().startsWith("https://"));
  const date = (timestamp: number) =>
    timestamp
      ? new Intl.DateTimeFormat("ru-RU", {
          dateStyle: "medium",
          timeStyle: "short",
        }).format(timestamp * 1000)
      : "Не загружено";
  const update = (
    component: "check" | "geo" | "core" | "all",
    title: string,
    success = "Компоненты обновлены и проверены.",
  ) =>
    void run(() => actions.updateComponents(component), success, {
      title,
      waitFor: "components",
    });
  const versionState = !components?.mihomo.latest_version
    ? "Версия ещё не проверялась"
    : components.mihomo.update_available
      ? `Доступна ${components.mihomo.latest_version}`
      : `Актуальная ${components.mihomo.installed_version}`;
  return (
    <div className="settings-section">
      <Heading
        eyebrow="Компоненты"
        title="Ядро и геобазы"
        text={
          embedded
            ? "Mihomo встроен в приложение и обновляется вместе с новой подписанной APK. GeoIP и GeoSite обновляются отдельно с проверкой и откатом."
            : "Проверка версии не меняет систему. Обновление Mihomo применяется только после проверки SHA-256 и конфигурации; нерабочая версия автоматически откатывается."
        }
      />
      <div className="detail-grid">
        <Detail
          label="Mihomo"
          value={components?.mihomo.installed_version || "Недоступно"}
        />
        <Detail label="Последняя версия" value={versionState} />
        <Detail
          label="GeoIP"
          value={
            components?.geoip.installed
              ? date(components.geoip.updated_at)
              : "Не загружено"
          }
        />
        <Detail
          label="GeoSite"
          value={
            components?.geosite.installed
              ? date(components.geosite.updated_at)
              : "Не загружено"
          }
        />
        <Detail
          label="Источник GEO"
          value={components?.installed_geo_source?.name || "Не определён"}
        />
        <Detail
          label="Следующее обновление GEO"
          value={
            components?.auto_update
              ? components.next_geo_update
                ? date(components.next_geo_update)
                : "Планируется"
              : "Выключено"
          }
        />
      </div>
      {embedded && (
        <div className="editor-card">
          <strong>Обновление OrcheRoute</strong>
          <p className="route-help">
            {appUpdate?.message ?? "Проверка обновлений доступна через GitHub Releases."}
          </p>
          {appUpdate?.state === "error" && appUpdate.error && (
            <p className="field-error">{appUpdate.error}</p>
          )}
          {appUpdate?.total && appUpdate.state === "downloading" ? (
            <div className="operation-progress-wrap">
              <div className="operation-progress">
                <span style={{ width: `${Math.min(100, Math.round(((appUpdate.current ?? 0) / appUpdate.total) * 100))}%` }} />
              </div>
              <small>{Math.round(((appUpdate.current ?? 0) / appUpdate.total) * 100)}%</small>
            </div>
          ) : null}
          <label className={`toggle-row app-update-channel ${appUpdate?.beta_enabled ? "selected" : ""}`}>
            <input
              type="checkbox"
              checked={Boolean(appUpdate?.beta_enabled)}
              disabled={Boolean(appUpdate?.active)}
              onChange={(event) => {
                if (!event.target.checked) {
                  setAndroidAppUpdateBetaEnabled(false);
                  return;
                }
                if (appUpdate?.current_prerelease) {
                  setAndroidAppUpdateBetaEnabled(true);
                  return;
                }
                setBetaWarning(true);
              }}
            />
            <span>
              <strong>Beta-версии</strong>
              <small>Проверять обновления в тестовом канале</small>
            </span>
          </label>
          <ActionBar>
            <span>
              Установлена {appUpdate?.current_version ?? "—"}
              {appUpdate?.latest_version ? ` · latest ${appUpdate.latest_version}` : ""}
            </span>
            <button
              className="secondary-button"
              type="button"
              disabled={Boolean(appUpdate?.active)}
              onClick={() => checkAndroidAppUpdate()}
            >
              Проверить обновление
            </button>
            <button
              className="primary-button"
              type="button"
              disabled={Boolean(appUpdate?.active) || appUpdate?.state !== "available"}
              onClick={() => installAndroidAppUpdate()}
            >
              Скачать и установить
            </button>
          </ActionBar>
        </div>
      )}
	  {embedded && betaWarning && (
		<div className="picker-dialog-backdrop" onClick={() => setBetaWarning(false)}>
		  <div
			className="picker-dialog subscription-delete-dialog"
			role="alertdialog"
			aria-modal="true"
			aria-labelledby="beta-update-title"
			onClick={(event) => event.stopPropagation()}
		  >
			<strong id="beta-update-title">Включить Beta-версии?</strong>
			<p className="route-help">
			  Это тестовая сборка с непроверенными изменениями VPN-автоматики. Возможны
			  обрывы соединения и ошибки переключения сети. Перед установкой рекомендуется
			  сохранить важные подписки и маршруты. После подтверждения используйте
			  обычные кнопки проверки и установки обновления.
			</p>
			<ActionBar>
			  <button className="secondary-button" type="button" onClick={() => setBetaWarning(false)}>
				Отмена
			  </button>
			  <button
				className="danger"
				type="button"
				onClick={() => {
				  setBetaWarning(false);
				  setAndroidAppUpdateBetaEnabled(true);
				}}
			  >
				Понимаю риск, включить Beta
			  </button>
			</ActionBar>
		  </div>
		</div>
	  )}
      <div className="editor-card geo-settings-card">
        <strong>GeoIP и GeoSite</strong>
        <p className="route-help">
          Выберите один совместимый набор. Новые файлы применятся после кнопки
          «Обновить геобазы», а списки категорий в маршрутах будут прочитаны
          прямо из них.
        </p>
        <div className="geo-source-list">
          {(components?.geo_sources ?? []).map((source) => (
            <label
              key={source.id}
              className={geoSource === source.id ? "selected" : ""}
            >
              <input
                type="radio"
                name="geo-source"
                value={source.id}
                checked={geoSource === source.id}
                onChange={() => setGeoSource(source.id)}
              />
              <span>
                <strong>{source.name}</strong>
                <small>{source.description}</small>
              </span>
            </label>
          ))}
        </div>
        {geoSource === "custom" && (
          <div className="form-grid two">
            <Field label="Прямая ссылка GeoIP.dat" hint="Только HTTPS">
              <input
                type="url"
                value={geoIPURL}
                onChange={(event) => setGeoIPURL(event.target.value)}
                placeholder="https://example.org/geoip.dat"
              />
            </Field>
            <Field label="Прямая ссылка GeoSite.dat" hint="Только HTTPS">
              <input
                type="url"
                value={geoSiteURL}
                onChange={(event) => setGeoSiteURL(event.target.value)}
                placeholder="https://example.org/geosite.dat"
              />
            </Field>
          </div>
        )}
        <div className="geo-settings-schedule">
          <strong>Расписание обновления</strong>
          <div className="form-grid two">
            <Toggle
              checked={geoEnabled}
              onChange={setGeoEnabled}
              label="Автоматическое обновление"
            />
            <Field label="Интервал">
              <select
                value={geoInterval}
                disabled={!geoEnabled}
                onChange={(event) => setGeoInterval(Number(event.target.value))}
              >
                <option value={6}>Каждые 6 часов</option>
                <option value={12}>Каждые 12 часов</option>
                <option value={24}>Раз в сутки</option>
                <option value={48}>Раз в 2 дня</option>
                <option value={72}>Раз в 3 дня</option>
                <option value={168}>Раз в неделю</option>
              </select>
            </Field>
          </div>
          <p className="route-help">
            {embedded
              ? "Android запустит фоновую задачу при доступной сети; точное время может оптимизироваться системой."
              : "После сохранения systemd пересчитает время следующего запуска."}
          </p>
        </div>
        <ActionBar>
          <span>
            {geoSettingsChanged
              ? "Есть несохранённые настройки GEO."
              : "Настройки GEO сохранены."}
          </span>
          <button
            className="secondary-button"
            type="button"
            disabled={
              busy || !components || !geoSettingsChanged || !customSourceValid
            }
            onClick={() =>
              void run(
                () => actions.updateComponentSettings(settingsPayload),
                "Настройки GEO сохранены.",
                { title: "Сохраняем настройки GEO" },
              )
            }
          >
            Сохранить настройки
          </button>
          <button
            className="primary-button"
            type="button"
            disabled={busy || !components || !customSourceValid}
            onClick={() =>
              void run(
                async () => {
                  if (geoSettingsChanged) {
                    await actions.updateComponentSettings(settingsPayload);
                  }
                  await actions.updateComponents("geo");
                },
                "GeoIP и GeoSite обновлены.",
                { title: "Обновляем GeoIP и GeoSite", waitFor: "components" },
              )
            }
          >
            Обновить геобазы
          </button>
        </ActionBar>
      </div>
      {!embedded && (
        <ActionBar>
          <button
            className="primary-button"
            type="button"
            disabled={busy || !components?.mihomo.update_available}
            onClick={() => update("core", "Безопасно обновляем Mihomo")}
          >
            Обновить Mihomo
          </button>
        </ActionBar>
      )}
    </div>
  );
}

type SubscriptionPayload = {
  name: string;
  group: "primary" | "emergency";
  parser: string;
  secret: string;
  enabled: boolean;
  interval_seconds: number;
};
const proxyLinkPattern =
  /^(vless|vmess|trojan|ss|hysteria2|hy2|wireguard|wg|amneziawg|awg):\/\//i;
const subscriptionURLPattern = /^https?:\/\//i;
const wireGuardConfigPattern = /\[\s*interface\s*\][\s\S]*\[\s*peer\s*\]/i;
const blackTempleSchemePattern = /^blacktemple:\/\//i;
const blackTempleIntentPattern = /^intent:\/\/[\s\S]*[;#]scheme=blacktemple(?:;|$)/i;

function looksLikeBlackTemple(value: string) {
  return lines(value).some(
    (entry) =>
      blackTempleSchemePattern.test(entry) ||
      blackTempleIntentPattern.test(entry),
  );
}

function decodePossibleBase64(value: string) {
  try {
    const compact = value
      .replace(/\s+/g, "")
      .replace(/-/g, "+")
      .replace(/_/g, "/");
    return decodeURIComponent(
      Array.from(atob(compact.padEnd(Math.ceil(compact.length / 4) * 4, "=")))
        .map(
          (character) =>
            `%${character.charCodeAt(0).toString(16).padStart(2, "0")}`,
        )
        .join(""),
    );
  } catch {
    return "";
  }
}

function prepareSubscriptionImport(
  name: string,
  group: "primary" | "emergency",
  parser: string,
  secret: string,
  interval_seconds: number,
  enabled: boolean,
) {
  const base = name.trim();
  const trimmed = secret.trim();
  if (parser === "blacktemple" || (parser === "auto" && looksLikeBlackTemple(trimmed)))
    return {
      payloads: [
        {
          name: base || "BlackTemple",
          group,
          parser: "blacktemple",
          secret: trimmed,
          interval_seconds,
          enabled,
        },
      ],
      duplicates: 0,
      servers: 0,
      urls: 1,
    };
  if (parser === "wireguard" || wireGuardConfigPattern.test(trimmed)) {
    return {
      payloads: [
        {
          name: base || "WireGuard",
          group,
          parser: "wireguard",
          secret: trimmed,
          interval_seconds,
          enabled,
        },
      ],
      duplicates: 0,
      servers: 1,
      urls: 0,
    };
  }
  if (parser === "inline") {
    const raw = lines(trimmed).filter((value) => proxyLinkPattern.test(value));
    const decoded = raw.length
      ? raw
      : lines(decodePossibleBase64(trimmed)).filter((value) =>
          proxyLinkPattern.test(value),
        );
    const unique = [...new Set(decoded)];
    return {
      payloads: unique.length
        ? [
            {
              name: base || "Добавленные серверы",
              group,
              parser: "inline",
              secret: unique.join("\n"),
              interval_seconds,
              enabled,
            },
          ]
        : [],
      duplicates: decoded.length - unique.length,
      servers: unique.length,
      urls: 0,
    };
  }
  const inputLines = lines(trimmed);
  const urls = [
    ...new Set(
      inputLines.filter((value) => subscriptionURLPattern.test(value)),
    ),
  ];
  let servers = inputLines.filter((value) => proxyLinkPattern.test(value));
  if (!urls.length && !servers.length)
    servers = lines(decodePossibleBase64(trimmed)).filter((value) =>
      proxyLinkPattern.test(value),
    );
  const uniqueServers = [...new Set(servers)];
  const duplicateCount =
    inputLines.filter((value) => subscriptionURLPattern.test(value)).length -
    urls.length +
    servers.length -
    uniqueServers.length;
  const payloads: SubscriptionPayload[] = urls.map((url, index) => {
    let suffix = String(index + 1);
    try {
      suffix = new URL(url).hostname;
    } catch {
      /* keep sequence */
    }
    const multiple = urls.length + (uniqueServers.length ? 1 : 0) > 1;
    return {
      name: base ? (multiple ? `${base} · ${suffix}` : base) : suffix,
      group,
      parser: "standard",
      secret: url,
      interval_seconds,
      enabled,
    };
  });
  if (uniqueServers.length)
    payloads.push({
      name: base
        ? urls.length
          ? `${base} · серверы`
          : base
        : "Добавленные серверы",
      group,
      parser: "inline",
      secret: uniqueServers.join("\n"),
      interval_seconds,
      enabled,
    });
  return {
    payloads,
    duplicates: duplicateCount,
    servers: uniqueServers.length,
    urls: urls.length,
  };
}

function SubscriptionEditor({
  item,
  busy,
  onCancel,
  onSave,
}: {
  item: Subscription | null;
  busy: boolean;
  onCancel: () => void;
  onSave: (payloads: SubscriptionPayload[]) => Promise<void>;
}) {
  const [name, setName] = useState(item?.name ?? "");
  const [group, setGroup] = useState<"primary" | "emergency">(
    item?.group ?? "primary",
  );
  const [secret, setSecret] = useState("");
  const [interval, setInterval] = useState(
    String(Math.round((item?.interval_seconds ?? 3600) / 60)),
  );
  const [loadingSecret, setLoadingSecret] = useState(Boolean(item));
  const [scanError, setScanError] = useState("");
  const fileInput = useRef<HTMLInputElement>(null);
  useEffect(() => {
    if (!item) return;
    actions
      .revealSubscriptionSecret(item.id)
      .then((result) => setSecret(result.secret))
      .finally(() => setLoadingSecret(false));
  }, [item]);
  useEffect(() => {
    const receive = (event: Event) => {
      const value = (event as CustomEvent<{ value?: string }>).detail?.value?.trim();
      if (!value) return;
      setScanError("");
      setSecret((current) => (current.trim() ? `${current.trim()}\n${value}` : value));
    };
    const fail = (event: Event) => {
      const message = (event as CustomEvent<{ message?: string }>).detail?.message;
      setScanError(message || "Не удалось считать QR-код.");
    };
    const receiveFile = (event: Event) => {
      const detail = (event as CustomEvent<{ content?: string }>).detail;
      if (typeof detail?.content !== "string") return;
      setScanError("");
      setSecret(detail.content);
    };
    const failFile = (event: Event) => {
      const message = (event as CustomEvent<{ message?: string }>).detail?.message;
      setScanError(message || "Не удалось прочитать выбранный файл.");
    };
    window.addEventListener("orcheroute:qr-scan", receive);
    window.addEventListener("orcheroute:qr-error", fail);
    window.addEventListener("orcheroute:file-open", receiveFile);
    window.addEventListener("orcheroute:file-open-error", failFile);
    return () => {
      window.removeEventListener("orcheroute:qr-scan", receive);
      window.removeEventListener("orcheroute:qr-error", fail);
      window.removeEventListener("orcheroute:file-open", receiveFile);
      window.removeEventListener("orcheroute:file-open-error", failFile);
    };
  }, []);
  const preview = useMemo(
    () =>
      prepareSubscriptionImport(
        name,
        group,
        "auto",
        secret,
        Number(interval) * 60,
        item?.enabled ?? true,
      ),
    [group, interval, item?.enabled, name, secret],
  );
  const submit = () => {
    void onSave(item ? preview.payloads.slice(0, 1) : preview.payloads);
  };
  const paste = async () => setSecret(await navigator.clipboard.readText());
  const loadFile = async (file?: File) => {
    if (file) setSecret(await file.text());
  };
  return (
    <div
      className="picker-dialog-backdrop"
      role="presentation"
      onMouseDown={onCancel}
    >
      <section
        className="picker-dialog subscription-editor-dialog"
        role="dialog"
        aria-modal="true"
        aria-label={item ? "Изменить подписку" : "Добавить подписку"}
        onMouseDown={(event) => event.stopPropagation()}
      >
        <header>
          <div>
            <strong>
              {item ? "Изменить подписку" : "Добавить подписки и серверы"}
            </strong>
            <small>
              {item
                ? "Ссылка отображается полностью и сохраняется только после подтверждения."
                : "Вставьте ссылки, URI серверов либо конфигурацию WireGuard/AmneziaWG."}
            </small>
          </div>
          <button type="button" onClick={onCancel} aria-label="Закрыть">
            ×
          </button>
        </header>
        <div className="subscription-editor-body">
          <div className="form-grid two">
            <Field label="Название" hint="Необязательно — определим автоматически">
              <input
                value={name}
                onChange={(event) => setName(event.target.value)}
                placeholder="Например, основной провайдер"
              />
            </Field>
            <Field label="Группа">
              <select
                value={group}
                onChange={(event) =>
                  setGroup(event.target.value as "primary" | "emergency")
                }
              >
                <option value="primary">Основная</option>
                <option value="emergency">Аварийная</option>
              </select>
            </Field>
            <Field label="Интервал" suffix="мин">
              <input
                type="number"
                min="5"
                max="10080"
                value={interval}
                onChange={(event) => setInterval(event.target.value)}
              />
            </Field>
          </div>
          <Field
            label="Ссылки или данные"
            hint="Один элемент в строке"
          >
            <textarea
              className="subscription-secret-editor"
              rows={9}
              spellCheck={false}
              value={secret}
              disabled={loadingSecret}
              onChange={(event) => setSecret(event.target.value)}
              placeholder={
                loadingSecret
                  ? "Загружаем…"
                  : "https://example.org/subscription\nvless://…\ntrojan://…"
              }
            />
          </Field>
          <div className="import-actions">
            <button
              className="secondary-button"
              type="button"
              onClick={() => void paste()}
            >
              Вставить из буфера
            </button>
            <button
              className="secondary-button"
              type="button"
              onClick={() => {
                if (!openTextFile()) fileInput.current?.click();
              }}
            >
              Загрузить файл
            </button>
            {canScanQr() && (
              <button
                className="secondary-button"
                type="button"
                onClick={() => {
                  setScanError("");
                  scanQr();
                }}
              >
                Сканировать QR
              </button>
            )}
            {!canOpenTextFile() && (
              <input
                ref={fileInput}
                type="file"
                accept=".txt,.conf,.list,.yaml,.yml,.json"
                hidden
                onChange={(event) => void loadFile(event.target.files?.[0])}
              />
            )}
            <span>
              {item
                ? `${lines(secret).length} строк`
                : `${preview.urls} подписок · ${preview.servers} серверов${preview.duplicates ? ` · дублей отброшено: ${preview.duplicates}` : ""}`}
            </span>
          </div>
          {secret.trim() && !preview.payloads.length && (
            <div className="inline-feedback error">
              Поддерживаются HTTP/HTTPS-подписки, URI прокси и конфигурации
              WireGuard/AmneziaWG. В файле подходящих данных не найдено.
            </div>
          )}
          {scanError && <div className="inline-feedback error">{scanError}</div>}
        </div>
        <footer>
          <button className="secondary-button" type="button" onClick={onCancel}>
            Отмена
          </button>
          <button
            className="primary-button"
            type="button"
            disabled={
              busy ||
              loadingSecret ||
              !secret.trim() ||
              !preview.payloads.length
            }
            onClick={submit}
          >
            {item
              ? "Сохранить"
              : `Добавить${preview.payloads.length > 1 ? ` · ${preview.payloads.length}` : ""}`}
          </button>
        </footer>
      </section>
    </div>
  );
}

function downloadText(filename: string, value: string) {
  const safeFilename = filename.replace(/[^a-zа-яё0-9_.-]+/gi, "-");
  if (saveTextFile(safeFilename, value)) return;
  const link = document.createElement("a");
  link.href = URL.createObjectURL(
    new Blob([value], { type: "text/plain;charset=utf-8" }),
  );
  link.download = safeFilename;
  link.click();
  URL.revokeObjectURL(link.href);
}

function SubscriptionExportDialog({
  item,
  onClose,
}: {
  item: Subscription;
  onClose: () => void;
}) {
  const [payload, setPayload] = useState<{
    secret: string;
    links: string[];
  } | null>(null);
  const [mode, setMode] = useState<"source" | "servers">(
    item.parser === "inline" ? "servers" : "source",
  );
  const [serverIndex, setServerIndex] = useState(0);
  const [qr, setQr] = useState("");
  const [error, setError] = useState("");
  useEffect(() => {
    const fail = (event: Event) => {
      const detail = (event as CustomEvent<{ message?: string }>).detail;
      setError(detail?.message ?? "Не удалось сохранить файл.");
    };
    window.addEventListener("orcheroute:file-save-error", fail);
    return () => window.removeEventListener("orcheroute:file-save-error", fail);
  }, []);
  useEffect(() => {
    actions
      .exportSubscription(item.id)
      .then(setPayload)
      .catch((reason) => setError(errorText(reason)));
  }, [item.id]);
  const content =
    mode === "source"
      ? (payload?.secret ?? "")
      : (payload?.links ?? []).join("\n");
  const qrValue =
    mode === "source"
      ? (payload?.secret ?? "")
      : (payload?.links?.[serverIndex] ?? "");
  useEffect(() => {
    setQr("");
    setError("");
    if (!qrValue) return;
    QRCode.toDataURL(qrValue, {
      width: 300,
      margin: 2,
      errorCorrectionLevel: "M",
      color: { dark: "#07110f", light: "#f4fffb" },
    })
      .then(setQr)
      .catch(() => setError("Эти данные слишком велики для одного QR-кода."));
  }, [qrValue]);
  const filename = `${item.name}-${mode === "source" ? "source" : "servers"}.txt`;
  return (
    <div
      className="picker-dialog-backdrop"
      role="presentation"
      onMouseDown={onClose}
    >
      <section
        className="picker-dialog subscription-export-dialog"
        role="dialog"
        aria-modal="true"
        aria-label={`Экспорт ${item.name}`}
        onMouseDown={(event) => event.stopPropagation()}
      >
        <header>
          <div>
            <strong>Экспорт · {item.name}</strong>
            <small>Данные обрабатываются локально в браузере.</small>
          </div>
          <button type="button" onClick={onClose} aria-label="Закрыть">
            ×
          </button>
        </header>
        <div className="subscription-export-body">
          <div className="route-target-tabs export-tabs">
            <button
              type="button"
              className={mode === "source" ? "active" : ""}
              onClick={() => setMode("source")}
            >
              <span>
                {item.parser === "inline"
                  ? "Исходный список"
                  : item.parser === "wireguard"
                    ? "Конфигурация"
                    : "Ссылка подписки"}
              </span>
            </button>
            <button
              type="button"
              className={mode === "servers" ? "active" : ""}
              onClick={() => setMode("servers")}
            >
              <span>Готовые серверы</span>
              <strong>{payload?.links.length ?? 0}</strong>
            </button>
          </div>
          {!payload && !error && (
            <p className="empty-state">Загружаем данные…</p>
          )}
          {payload && (
            <>
              <div className="export-preview">
                <code>
                  {content ||
                    "Нет готовых серверов. Сначала обновите подписку."}
                </code>
              </div>
              {mode === "servers" && payload.links.length > 1 && (
                <Field
                  label="Сервер для QR"
                  hint={`${serverIndex + 1} из ${payload.links.length}`}
                >
                  <select
                    value={serverIndex}
                    onChange={(event) =>
                      setServerIndex(Number(event.target.value))
                    }
                  >
                    {payload.links.map((link, index) => (
                      <option
                        value={index}
                        key={`${index}-${link.slice(0, 24)}`}
                      >
                        {index + 1}. {link.slice(0, 70)}
                      </option>
                    ))}
                  </select>
                </Field>
              )}
              {qr && (
                <div className="qr-preview">
                  <img src={qr} alt="QR-код экспортируемых данных" />
                  <a
                    className="secondary-button"
                    href={qr}
                    download={`${item.name}-qr.png`}
                  >
                    Скачать QR
                  </a>
                </div>
              )}
            </>
          )}
          {error && <div className="inline-feedback error">{error}</div>}
        </div>
        <footer>
          <button
            className="secondary-button"
            type="button"
            disabled={!content}
            onClick={() => void copyText(content)}
          >
            В буфер
          </button>
          <button
            className="secondary-button"
            type="button"
            disabled={!content}
            onClick={() => downloadText(filename, content)}
          >
            Скачать файл
          </button>
          <button className="primary-button" type="button" onClick={onClose}>
            Готово
          </button>
        </footer>
      </section>
    </div>
  );
}

function CountryPicker({
  value,
  onChange,
}: {
  value: string[];
  onChange: (countries: string[]) => void;
}) {
  const [query, setQuery] = useState("");
  const normalizedQuery = query.trim().toLocaleLowerCase("ru");
  const countries = COUNTRY_CODES.map((code) => ({
    code,
    name: regionNames?.of(code) ?? code,
  }))
    .filter(
      (item) =>
        !normalizedQuery ||
        item.code.toLowerCase().includes(normalizedQuery) ||
        item.name.toLocaleLowerCase("ru").includes(normalizedQuery),
    )
    .sort((left, right) => left.name.localeCompare(right.name, "ru"));
  const toggle = (code: string) =>
    onChange(
      value.includes(code)
        ? value.filter((item) => item !== code)
        : [...value, code].sort(),
    );
  return (
    <div className="country-picker">
      <div className="country-picker-head">
        <div>
          <strong>Исключённые страны</strong>
          <small>Узлы из выбранных регионов не пройдут квалификацию</small>
        </div>
        <span>{value.length} выбрано</span>
      </div>
      <div className="input-wrap country-search">
        <input
          value={query}
          onChange={(event) => setQuery(event.target.value)}
          placeholder="Найти страну или код"
        />
      </div>
      {value.length > 0 && (
        <div className="selected-countries">
          {value.map((code) => (
            <button type="button" key={code} onClick={() => toggle(code)}>
              {regionNames?.of(code) ?? code}
              <span>{code} ×</span>
            </button>
          ))}
        </div>
      )}
      <div className="country-list">
        {countries.map((country) => (
          <label
            key={country.code}
            className={value.includes(country.code) ? "selected" : ""}
          >
            <input
              type="checkbox"
              checked={value.includes(country.code)}
              onChange={() => toggle(country.code)}
            />
            <span>{country.name}</span>
            <em>{country.code}</em>
          </label>
        ))}
      </div>
    </div>
  );
}

function ModeCard({
  selected,
  title,
  text,
  onClick,
  warning = false,
}: {
  selected: boolean;
  title: string;
  text?: string;
  onClick: () => void;
  warning?: boolean;
}) {
  return (
    <button
      type="button"
      className={`mode-card ${selected ? "selected" : ""} ${warning ? "warning" : ""}`}
      onClick={onClick}
    >
      <span className="mode-card-radio" />
      <strong>{title}</strong>
      <small>{text}</small>
    </button>
  );
}

function InterfacePicker({
  interfaces,
  value,
  onChange,
}: {
  interfaces: DashboardData["interfaces"];
  value: string[];
  onChange: (interfaces: string[]) => void;
}) {
  const toggle = (name: string) =>
    onChange(
      value.includes(name)
        ? value.filter((item) => item !== name)
        : [...value, name],
    );
  return (
    <div className={`capture-editor ${!value.length ? "required" : ""}`}>
      <div className="capture-editor-head">
        <div>
          <strong>Откуда принимать трафик</strong>
          <small>Интерфейсы захвата</small>
        </div>
        <span>{value.length ? `${value.length} выбрано` : "Обязательно"}</span>
      </div>
      <p>
        Выберите LAN-интерфейсы, с которых приходят устройства. WAN и текущий
        VPN-интерфейс обычно выбирать не нужно.
      </p>
      <div className="interface-check-list">
        {interfaces.map((item) => (
          <label
            key={item.name}
            className={value.includes(item.name) ? "selected" : ""}
          >
            <input
              type="checkbox"
              checked={value.includes(item.name)}
              onChange={() => toggle(item.name)}
            />
            <span>
              <strong>{item.name}</strong>
              <small>
                {item.addresses.map((address) => address.cidr).join(", ") ||
                  "Без адреса"}
              </small>
            </span>
            <em className={item.state === "up" ? "up" : ""}>{item.state}</em>
          </label>
        ))}
      </div>
    </div>
  );
}

function ListEditor({
  title,
  technical,
  description,
  values,
  onChange,
  placeholder,
  required = false,
}: {
  title: string;
  technical: string;
  description: string;
  values: string[];
  onChange: (values: string[]) => void;
  placeholder: string;
  required?: boolean;
}) {
  const [draft, setDraft] = useState("");
  const add = () => {
    const additions = draft
      .split(/[\s,;]+/)
      .map((item) => item.trim())
      .filter(Boolean);
    if (!additions.length) return;
    onChange([...new Set([...values, ...additions])]);
    setDraft("");
  };
  return (
    <div
      className={`capture-editor ${required && !values.length ? "required" : ""}`}
    >
      <div className="capture-editor-head">
        <div>
          <strong>{title}</strong>
          <small>{technical}</small>
        </div>
        {required && <span>{values.length ? "Заполнено" : "Обязательно"}</span>}
      </div>
      <p>{description}</p>
      <div className="list-add-row">
        <div className="input-wrap">
          <input
            value={draft}
            onChange={(event) => setDraft(event.target.value)}
            onKeyDown={(event) => {
              if (event.key === "Enter") {
                event.preventDefault();
                add();
              }
            }}
            placeholder={placeholder}
          />
        </div>
        <button
          className="secondary-button"
          type="button"
          onClick={add}
          disabled={!draft.trim()}
        >
          Добавить
        </button>
      </div>
      <div className="editable-value-list">
        {values.map((item) => (
          <div key={item}>
            <code>{item}</code>
            <button
              type="button"
              onClick={() => onChange(values.filter((value) => value !== item))}
              aria-label={`Удалить ${item}`}
            >
              Удалить
            </button>
          </div>
        ))}
        {!values.length && <span>Список пуст</span>}
      </div>
    </div>
  );
}

function Heading({
  eyebrow,
  title,
  text,
  compact = false,
}: {
  eyebrow: string;
  title: string;
  text?: string;
  compact?: boolean;
}) {
  return (
    <div className={`section-heading ${compact ? "compact" : ""}`}>
      <span>{eyebrow}</span>
      <h3>{title}</h3>
      {text && <p>{text}</p>}
    </div>
  );
}
function Detail({ label, value }: { label: string; value: string }) {
  return (
    <div className="detail">
      <span>{label}</span>
      <strong>{value}</strong>
    </div>
  );
}
function Field({
  label,
  hint,
  suffix,
  children,
}: {
  label: string;
  hint?: string;
  suffix?: string;
  children: React.ReactNode;
}) {
  return (
    <label className="form-field">
      <span>
        {label}
        {hint && <small>{hint}</small>}
      </span>
      <div className="input-wrap">
        {children}
        {suffix && <em>{suffix}</em>}
      </div>
    </label>
  );
}
function Toggle({
  checked,
  onChange,
  label,
}: {
  checked: boolean;
  onChange: (value: boolean) => void;
  label: string;
}) {
  return (
    <label className="toggle-row">
      <input
        type="checkbox"
        checked={checked}
        onChange={(event) => onChange(event.target.checked)}
      />
      <span>{label}</span>
    </label>
  );
}
function ActionBar({ children }: { children: React.ReactNode }) {
  return <div className="action-bar">{children}</div>;
}
