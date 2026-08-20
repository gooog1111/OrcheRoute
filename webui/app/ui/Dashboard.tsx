"use client";

import { useCallback, useEffect, useMemo, useState } from "react";
import { actions, isAndroidRuntime, loadDashboard, type DashboardData, type Node } from "../lib/api";
import { ChevronIcon, CloseIcon, GlobeIcon, PowerIcon, RefreshIcon, RouteIcon, ServerIcon, SettingsIcon } from "./Icons";
import { SettingsModal as EditableSettingsModal, type SettingsTab } from "./SettingsModal";
import { OperationPanel, type OperationView } from "./OperationPanel";
import { MatrixRain } from "./MatrixRain";


const statusText: Record<string, string> = {
  proxy_ok: "Защищено",
  manual_proxy_ok: "Защищено · ручной узел",
  proxy_degraded: "Соединение нестабильно",
  manual_proxy_degraded: "Ручной узел нестабилен",
  manual_target_unavailable: "Узел недоступен",
  emergency_proxy_ok: "Защищено · аварийный пул",
  emergency_proxy_degraded: "Аварийный пул недоступен",
  internet_down: "Нет интернета",
  recovery_grace: "Восстановление соединения",
  starting: "Запуск",
  disabled: "Выключено",
  controller_error: "Ошибка контроллера",
  mobile_direct: "TUN работает · прямой режим",
};

const captureText: Record<string, string> = {
  system: "Вся система",
  interfaces: "Выбранные интерфейсы",
};

function formatTime(timestamp: number) {
  if (!timestamp) return "—";
  return new Intl.DateTimeFormat("ru-RU", {
    day: "2-digit",
    month: "2-digit",
    year: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  }).format(timestamp * 1000);
}

function routeCount(data: DashboardData | null) {
  if (!data?.routes?.stats) return 0;
  return Object.values(data.routes.stats).reduce((sum, item) => sum + (item.compiled_rules ?? item.entries ?? 0), 0);
}

export function Dashboard() {
  const [data, setData] = useState<DashboardData | null>(null);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [tab, setTab] = useState<SettingsTab>("general");
  const [operationView, setOperationView] = useState<OperationView | null>(null);
  const [stoppingWhitelist, setStoppingWhitelist] = useState(false);

  const refresh = useCallback(async (quiet = false) => {
    if (!quiet) setLoading(true);
    try {
      const next = await loadDashboard();
      setData(next);
      setError(null);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Не удалось получить состояние");
    } finally {
      if (!quiet) setLoading(false);
    }
  }, []);

  useEffect(() => {
    if (settingsOpen) return;
    const first = window.setTimeout(() => void refresh(), 0);
    const timer = window.setInterval(() => void refresh(true), 5000);
    return () => {
      window.clearTimeout(first);
      window.clearInterval(timer);
    };
  }, [refresh, settingsOpen]);

  useEffect(() => {
    if (!settingsOpen) return;
    const close = (event: KeyboardEvent) => event.key === "Escape" && setSettingsOpen(false);
    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    window.addEventListener("keydown", close);
    return () => {
      document.body.style.overflow = previousOverflow;
      window.removeEventListener("keydown", close);
    };
  }, [settingsOpen]);

  useEffect(() => {
    if (operationView?.state !== "success") return;
    const timer = window.setTimeout(() => setOperationView(null), 2600);
    return () => window.clearTimeout(timer);
  }, [operationView?.state]);

  const enabled = data?.status.service?.enabled ?? data?.status.connectivity !== "disabled";
  const healthy = enabled && data?.status.wan.available === true && !data?.status.stale;
  const activePool = data?.pools.find((pool) => pool.selected);
  const allAliveNodes = data?.pools.reduce((sum, pool) => sum + pool.alive, 0) ?? 0;
  const allTotalNodes = data?.pools.reduce((sum, pool) => sum + pool.total, 0) ?? 0;
  const aliveNodes = activePool?.alive ?? allAliveNodes;
  const totalNodes = activePool?.total ?? allTotalNodes;
  const currentNode = data?.nodes.find((node) => node.selected);
  const whitelistPool = data?.pools.find((pool) => pool.id === "whitelist");
  const whitelistUpdate = data?.operations?.subscription_update;
  const whitelistScanning = Boolean(
    whitelistUpdate?.active && whitelistUpdate.allowlist_scan,
  );
  const whitelistOperation: OperationView | null = whitelistScanning
    ? {
        state: "running",
        title: "Формируем пул белых списков",
        detail: stoppingWhitelist
          ? "Останавливаем после завершения текущей группы тестов…"
          : whitelistUpdate?.message ?? "Перепроверяем сохранённые серверы в ограниченной сети…",
        step: 1,
        steps: ["Определяем режим сети", "Проверяем все серверы", "Формируем устойчивый пул"],
        progress:
          typeof whitelistUpdate?.current === "number" &&
          typeof whitelistUpdate?.total === "number" &&
          whitelistUpdate.total > 0
            ? { current: whitelistUpdate.current, total: whitelistUpdate.total }
            : undefined,
      }
    : null;
  const stopWhitelistScan = async () => {
    if (stoppingWhitelist) return;
    setStoppingWhitelist(true);
    try {
      await actions.cancelSubscriptionUpdate();
      for (let attempt = 0; attempt < 30; attempt += 1) {
        await new Promise((resolve) => window.setTimeout(resolve, 300));
        const next = await loadDashboard();
        setData(next);
        if (!next.operations?.subscription_update.active) {
          setStoppingWhitelist(false);
          break;
        }
      }
    } catch (reason) {
      setStoppingWhitelist(false);
      setError(reason instanceof Error ? reason.message : "Не удалось остановить проверку");
    }
  };
  const startWhitelistScan = async () => {
    try {
      await actions.scanWhitelistPool();
      await refresh(true);
    } catch (reason) {
      setError(reason instanceof Error ? reason.message : "Не удалось запустить формирование пула");
    }
  };
  const stateLabel = enabled ? (statusText[data?.status.connectivity ?? "starting"] ?? "Проверка") : "Готов к запуску";

  const toggle = async () => {
    if (!data || busy) return;
    const target = !enabled;
    setBusy(true);
    setError(null);
    setOperationView({ state: "running", title: target ? "Включаем OrcheRoute" : "Выключаем OrcheRoute", detail: "Передаём команду контроллеру…", step: 0, steps: ["Отправляем команду", "Контроллер меняет режим", "Проверяем соединение"] });
    try {
      await actions.setEnabled(target);
      setOperationView((current) => current && ({ ...current, step: 1, detail: target ? "Контроллер выбирает рабочий VPN-сервер…" : "Переводим трафик на прямой выход…" }));
      let next: DashboardData | null = null;
      for (let attempt = 0; attempt < 90; attempt += 1) {
        next = await loadDashboard();
        setData(next);
        if (next.status.connectivity === "controller_error") {
          throw new Error(next.status.mobile?.message || "Мобильный VPN runtime не запущен.");
        }
        if (next.status.mobile?.state === "permission_required") {
          setOperationView((current) => current && ({ ...current, detail: "Подтвердите создание VPN в системном окне Android." }));
        }
        if (next.status.mobile?.state === "stopping") {
          setOperationView((current) => current && ({ ...current, detail: "Закрываем TUN и системный VPN-профиль Android…" }));
        }
        const nextEnabled = next.status.service?.enabled ?? next.status.connectivity !== "disabled";
        const ready = target
          ? nextEnabled && ["proxy_ok", "manual_proxy_ok", "emergency_proxy_ok", "mobile_direct"].includes(next.status.connectivity)
          : !nextEnabled && next.status.connectivity === "disabled";
        if (ready) break;
        await new Promise((resolve) => window.setTimeout(resolve, 1000));
        if (attempt === 89) throw new Error("Контроллер не подтвердил новое состояние вовремя.");
      }
      setOperationView((current) => current && ({ ...current, step: 2, detail: "Связь проверена.", state: "success" }));
    } catch (reason) {
      const detail = reason instanceof Error ? reason.message : "Команда не выполнена";
      setError(detail);
      setOperationView((current) => current && ({ ...current, state: "error", detail }));
    } finally {
      setBusy(false);
    }
  };

  return (
    <main className="app-shell">
      {!settingsOpen && <MatrixRain />}
      <header className="topbar">
        <a className="brand" href="#" aria-label="OrcheRoute">
          <span className="brand-mark"><span /><span /><span /></span>
          <span><strong>OrcheRoute <em className="brand-beta">BETA</em></strong><small>traffic orchestration</small></span>
        </a>
        <div className="topbar-actions">
          <button className="icon-button" type="button" disabled={busy} onClick={() => setSettingsOpen(true)} aria-label="Открыть настройки">
            <SettingsIcon />
          </button>
        </div>
      </header>

      <section className="dashboard">
        <div className="hero-copy">
          <div className={`status-line ${healthy ? "is-healthy" : enabled ? "is-warning" : "is-off"}`}>
            <span className="status-dot" />
            {loading ? "Подключение к серверу" : stateLabel}
          </div>
          <h1>{enabled ? "Соединение под контролем" : "OrcheRoute выключен"}</h1>
          {enabled && <p>
            {data?.status.mobile?.state === "direct_test"
              ? "Mihomo обрабатывает трафик через системный TUN в диагностическом режиме DIRECT. VPN-сервер пока не выбран."
              : currentNode
                ? `Активен ${currentNode.display_name}. Автоматика продолжит работу до отказа узла.`
                : "Контроллер выбирает доступный сервер."}
          </p>}
        </div>

        <div className="power-stage">
          <div className={`power-halo ${enabled ? "is-on" : ""}`}>
            <button
              className={`power-button ${enabled ? "is-on" : ""}`}
              type="button"
              onClick={toggle}
              disabled={loading || busy || !data}
              aria-pressed={enabled}
              aria-label={enabled ? "Выключить OrcheRoute" : "Включить OrcheRoute"}
            >
              <PowerIcon />
              <span>{busy ? "Подождите" : enabled ? "Выключить" : "Включить"}</span>
            </button>
          </div>
          <small>{captureText[data?.status.network.capture_mode ?? ""] ?? "Режим не определён"}</small>
        </div>

        <div className="metrics-grid" aria-label="Состояние системы">
          <Metric icon={<GlobeIcon />} label="Интернет" value={data?.status.wan.mode === "allowlist" ? "Белые списки" : data?.status.wan.available === true ? "Доступен" : data?.status.wan.available === false ? "Недоступен" : "Проверка"} detail={data?.status.wan.interface ?? "—"} tone={data?.status.wan.mode === "allowlist" ? "neutral" : data?.status.wan.available === true ? "good" : "neutral"} />
          <Metric icon={<ServerIcon />} label="Серверы" value={`${aliveNodes} из ${totalNodes}`} detail={activePool?.id === "primary" ? "Основной пул" : activePool?.id === "emergency" ? "Аварийный пул" : activePool?.id === "whitelist" ? "Пул белых списков" : "Все пулы"} tone={aliveNodes > 0 ? "good" : "neutral"} />
          <Metric icon={<RouteIcon />} label="Маршруты" value={String(routeCount(data))} detail="direct · proxy · block" tone="neutral" />
          <Metric icon={<SettingsIcon />} label="Управление" value={data?.status.proxy.mode === "manual" ? "Ручное" : data?.status.proxy.mode === "emergency" ? "Только аварийный" : "Автоматически"} detail={<>Переключение<time>{formatTime(data?.status.proxy.last_switch ?? 0)}</time></>} tone="neutral" />
        </div>
        {(data?.status.wan.mode === "allowlist" || whitelistScanning || (whitelistPool?.total ?? 0) > 0) && (
          <div className={`whitelist-strip ${whitelistScanning ? "is-scanning" : ""}`}>
            <span className="status-dot" />
            <div>
              <strong>{whitelistScanning ? "Формируется пул белых списков" : "Пул белых списков"}</strong>
              <small>
                {whitelistScanning
                  ? whitelistUpdate?.message ?? "Проверяем все сохранённые серверы"
                  : `${whitelistPool?.alive ?? 0} доступных из ${whitelistPool?.total ?? 0}${whitelistPool?.selected ? " · используется сейчас" : ""}`}
              </small>
            </div>
            {isAndroidRuntime() && (
              <button
                className="whitelist-action"
                type="button"
                disabled={busy || stoppingWhitelist}
                onClick={() => void (whitelistScanning ? stopWhitelistScan() : startWhitelistScan())}
              >
                {whitelistScanning ? (stoppingWhitelist ? "Останавливаем" : "Остановить") : "Сформировать"}
              </button>
            )}
          </div>
        )}
      </section>

      {error && <div className="toast" role="alert"><span>{error}</span><button type="button" onClick={() => setError(null)}>Закрыть</button></div>}
      <OperationPanel
        operation={operationView ?? whitelistOperation}
        onDismiss={operationView ? () => setOperationView(null) : undefined}
        action={isAndroidRuntime() && whitelistOperation && !operationView ? {
          label: stoppingWhitelist ? "Останавливаем" : "Остановить",
          onClick: () => void stopWhitelistScan(),
          disabled: stoppingWhitelist,
        } : undefined}
      />
      {settingsOpen && (
        <EditableSettingsModal
          data={data}
          activeTab={tab}
          onTab={setTab}
          onClose={() => setSettingsOpen(false)}
          onReload={() => refresh(true)}
        />
      )}
    </main>
  );
}

function Metric({ icon, label, value, detail, tone }: { icon: React.ReactNode; label: string; value: string; detail: React.ReactNode; tone: "good" | "neutral" }) {
  return <article className="metric-card"><div className={`metric-icon ${tone}`}>{icon}</div><div><span>{label}</span><strong>{value}</strong><small>{detail}</small></div></article>;
}

// Kept temporarily as a compatibility reference while the editable modal is split into its own module.
// eslint-disable-next-line @typescript-eslint/no-unused-vars
function LegacySettingsModal({ data, activeTab, busy, onTab, onClose, onAction }: {
  data: DashboardData | null;
  activeTab: SettingsTab;
  busy: boolean;
  onTab: (tab: SettingsTab) => void;
  onClose: () => void;
  onAction: (action: () => Promise<unknown>) => Promise<void>;
}) {
  const nodesByPool = useMemo(() => ({
    primary: data?.nodes.filter((node) => node.pool === "primary") ?? [],
    emergency: data?.nodes.filter((node) => node.pool === "emergency") ?? [],
  }), [data]);

  return (
    <div className="modal-backdrop" role="presentation" onMouseDown={(event) => event.target === event.currentTarget && onClose()}>
      <section className="settings-modal" role="dialog" aria-modal="true" aria-labelledby="settings-title">
        <header className="modal-header">
          <div><span>OrcheRoute</span><h2 id="settings-title">Настройки</h2></div>
          <button className="icon-button" type="button" onClick={onClose} aria-label="Закрыть настройки"><CloseIcon /></button>
        </header>
        <div className="settings-layout">
          <nav className="settings-nav" aria-label="Разделы настроек">
            <TabButton active={activeTab === "general"} onClick={() => onTab("general")} label="Основное" />
            <TabButton active={activeTab === "network"} onClick={() => onTab("network")} label="Сеть и DNS" />
            <TabButton active={activeTab === "sources"} onClick={() => onTab("sources")} label="Подписки" />
          </nav>
          <div className="settings-content">
            {activeTab === "general" && <GeneralSettings data={data} nodesByPool={nodesByPool} busy={busy} onAction={onAction} />}
            {activeTab === "network" && <NetworkSettings data={data} />}
            {activeTab === "sources" && <SourceSettings data={data} busy={busy} onAction={onAction} />}
          </div>
        </div>
      </section>
    </div>
  );
}

function TabButton({ active, onClick, label }: { active: boolean; onClick: () => void; label: string }) {
  return <button type="button" className={active ? "active" : ""} onClick={onClick}><span>{label}</span><ChevronIcon /></button>;
}

function GeneralSettings({ data, nodesByPool, busy, onAction }: {
  data: DashboardData | null;
  nodesByPool: Record<"primary" | "emergency", Node[]>;
  busy: boolean;
  onAction: (action: () => Promise<unknown>) => Promise<void>;
}) {
  return <div className="settings-section">
    <div className="section-heading"><span>Переключение</span><h3>Как выбирать сервер</h3><p>Автоматический режим держит текущий узел до отказа и возвращается в основной пул после стабильной проверки.</p></div>
    <label className="choice-row">
      <input type="radio" name="control-mode" checked={data?.status.proxy.mode !== "manual"} disabled={busy} onChange={() => void onAction(actions.setAuto)} />
      <span><strong>Автоматически</strong><small>Рекомендуемый режим для сервера</small></span>
    </label>
    {(["primary", "emergency"] as const).map((pool) => (
      <div className="node-group" key={pool}>
        <span className="field-label">{pool === "primary" ? "Основной пул" : "Аварийный пул"}</span>
        <div className="node-list">
          {nodesByPool[pool].map((node) => (
            <button type="button" className={`node-row ${node.selected ? "selected" : ""}`} key={node.id} disabled={!node.alive || busy} onClick={() => void onAction(() => actions.setManual(node.id))}>
              <span className={`node-status ${node.alive ? "alive" : ""}`} />
              <span><strong>{node.id}</strong><small>{node.alive ? "Доступен" : "Недоступен"}</small></span>
              <em>{node.delay_ms ? `${node.delay_ms} мс` : "—"}</em>
            </button>
          ))}
          {!nodesByPool[pool].length && <p className="empty-state">В пуле пока нет серверов.</p>}
        </div>
      </div>
    ))}
  </div>;
}

function NetworkSettings({ data }: { data: DashboardData | null }) {
  const stats = data?.routes?.stats ?? {};
  return <div className="settings-section">
    <div className="section-heading"><span>Текущая конфигурация</span><h3>Сеть и DNS</h3><p>Здесь показано активное состояние. Редактирование профиля будет подключено к тем же API без изменения интерфейса.</p></div>
    <div className="detail-grid">
      <Detail label="Захват" value={captureText[data?.status.network.capture_mode ?? ""] ?? "—"} />
      <Detail label="Direct" value={data?.status.network.direct_interface ?? "—"} />
      <Detail label="VPN underlay" value={data?.status.network.vpn_underlay_interface ?? "—"} />
      <Detail label="DNS" value={data?.dns?.in_sync ? "Синхронизирован" : "Есть изменения"} />
    </div>
    <div className="config-block"><span className="field-label">DNS-каналы</span><ConfigLine label="Direct" value={data?.dns?.active.direct.join(", ") ?? "—"} /><ConfigLine label="Proxy" value={data?.dns?.active.proxy.join(", ") ?? "—"} /><ConfigLine label="Underlay" value={data?.dns?.active.vpn_underlay.join(", ") ?? "—"} /></div>
    <div className="config-block"><span className="field-label">Правила маршрутизации</span>{["direct", "proxy", "block"].map((name) => <ConfigLine key={name} label={name} value={String(stats[name]?.compiled_rules ?? stats[name]?.entries ?? 0)} />)}</div>
  </div>;
}

function SourceSettings({ data, busy, onAction }: { data: DashboardData | null; busy: boolean; onAction: (action: () => Promise<unknown>) => Promise<void> }) {
  return <div className="settings-section">
    <div className="section-heading with-action"><div><span>Источники серверов</span><h3>Подписки</h3><p>Секретные ссылки хранятся только на сервере и не возвращаются в интерфейс.</p></div><button className="secondary-button" type="button" disabled={busy} onClick={() => void onAction(actions.refreshSubscriptions)}><RefreshIcon /> Обновить</button></div>
    <div className="subscription-list">
      {data?.subscriptions.map((subscription) => <article className="subscription-row" key={subscription.id}><span className={`node-status ${subscription.enabled && subscription.last_status !== "error" ? "alive" : ""}`} /><div><strong>{subscription.name}</strong><small>{subscription.group === "primary" ? "Основная" : "Аварийная"} · {subscription.parser} · {subscription.last_links} ссылок</small></div><em>{subscription.enabled ? subscription.last_status : "выключена"}</em></article>)}
      {!data?.subscriptions.length && <p className="empty-state">Подписок пока нет.</p>}
    </div>
  </div>;
}

function Detail({ label, value }: { label: string; value: string }) { return <div className="detail"><span>{label}</span><strong>{value}</strong></div>; }
function ConfigLine({ label, value }: { label: string; value: string }) { return <div className="config-line"><span>{label}</span><code>{value}</code></div>; }
