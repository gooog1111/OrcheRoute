"use client";

import { useMemo, useState } from "react";
import { actions, type ReverseVPNClient, type ReverseVPNConfig, type ReverseVPNState } from "../lib/api";

type Runner = (operation: () => Promise<unknown>, success: string, options?: { title?: string }) => Promise<boolean>;

export function ReverseVPNPanel({ state, interfaces, busy, run, onReload }: {
	state: ReverseVPNState | null;
	interfaces: string[];
	busy: boolean;
	run: Runner;
	onReload: () => Promise<void>;
}) {
	const [config, setConfig] = useState<ReverseVPNConfig | null>(state?.config ?? null);
	const [name, setName] = useState("");
	const [expires, setExpires] = useState("");
	const [limitGB, setLimitGB] = useState("");
	const changed = useMemo(() => Boolean(config && state && JSON.stringify(config) !== JSON.stringify(state.config)), [config, state]);
	if (!config || !state) return <div className="settings-section"><p className="empty-state">Модуль VPN-сервера недоступен в этой сборке.</p></div>;
	const update = <K extends keyof ReverseVPNConfig>(key: K, value: ReverseVPNConfig[K]) => setConfig({ ...config, [key]: value });
	const save = async () => {
		if (!changed) return;
		if (await run(() => actions.saveReverseVPN(config), "Настройки VPN-сервера сохранены.", { title: "Сохраняем VPN-сервер" })) await onReload();
	};
	const toggle = async () => {
		if (await run(() => actions.setReverseVPNEnabled(!config.enabled), config.enabled ? "VPN-сервер остановлен." : "VPN-сервер запущен.", { title: config.enabled ? "Останавливаем VPN-сервер" : "Запускаем VPN-сервер" })) await onReload();
	};
	const create = async () => {
		const expiresAt = expires ? Math.floor(new Date(expires).getTime() / 1000) : undefined;
		const limit = limitGB ? Math.round(Number(limitGB) * 1024 ** 3) : undefined;
		if (await run(() => actions.createReverseVPNClient({ name, expires_at: expiresAt, traffic_limit_bytes: limit }), "Клиент и персональная подписка созданы.", { title: "Создаём VPN-клиента" })) {
			setName(""); setExpires(""); setLimitGB(""); await onReload();
		}
	};
	return <div className="settings-section reverse-vpn-settings">
		<div className="section-heading with-action"><div><span>Входящий VPN · beta</span><h3>VPN-сервер OrcheRoute</h3><p>Персональные подписки для конечных устройств, сроки действия и учёт трафика. Этот модуль не меняет исходящий VPN OrcheRoute.</p></div><span className={`reverse-vpn-state ${state.status.active ? "active" : ""}`}>{state.status.active ? "Работает" : config.enabled ? "Не запущен" : "Выключен"}</span></div>
		{state.status.last_error && <p className="inline-feedback error">{reverseVPNError(state.status.last_error)}</p>}
		<div className="access-panel">
			<div className="form-grid two">
				<label className="form-field"><span>Публичный адрес</span><input value={config.public_endpoint ?? ""} onChange={e => update("public_endpoint", e.target.value)} placeholder="vpn.example.ru:51820" disabled={busy || config.enabled}/><small>Домен или IP и UDP-порт, доступные клиентам.</small></label>
				<label className="form-field"><span>Адрес подписок</span><input type="url" value={config.subscription_base_url ?? ""} onChange={e => update("subscription_base_url", e.target.value)} placeholder="https://vpn.example.ru" disabled={busy || config.enabled}/><small>HTTPS-адрес WebUI без пути.</small></label>
				<label className="form-field"><span>Исходящий интерфейс</span><select value={config.outbound_interface ?? ""} onChange={e => update("outbound_interface", e.target.value)} disabled={busy || config.enabled}><option value="">Определять автоматически</option>{interfaces.map(item => <option key={item} value={item}>{item}</option>)}</select></label>
				<label className="form-field"><span>Сеть клиентов</span><input value={config.server_cidr} onChange={e => update("server_cidr", e.target.value)} disabled={busy || config.enabled}/></label>
				<label className="form-field"><span>UDP-порт</span><input type="number" min={1} max={65535} value={config.listen_port} onChange={e => update("listen_port", Number(e.target.value))} disabled={busy || config.enabled}/></label>
				<label className="form-field"><span>DNS клиентов</span><input value={config.dns.join(", ")} onChange={e => update("dns", e.target.value.split(",").map(v => v.trim()).filter(Boolean))} disabled={busy || config.enabled}/></label>
				<label className="form-field"><span>MTU</span><input type="number" min={576} max={9000} value={config.mtu} onChange={e => update("mtu", Number(e.target.value))} disabled={busy || config.enabled}/></label>
			</div>
			{config.enabled && <small>Чтобы изменить сетевые параметры, сначала остановите VPN-сервер. Клиенты и их лимиты можно менять во время работы.</small>}
			<div className="action-bar"><button className="secondary-button" type="button" disabled={busy || !changed || config.enabled} onClick={() => void save()}>Сохранить настройки</button><button className={config.enabled ? "danger-button" : "primary-button"} type="button" disabled={busy || changed || (!config.enabled && !config.public_endpoint)} onClick={() => void toggle()}>{config.enabled ? "Остановить сервер" : "Запустить сервер"}</button></div>
		</div>
		<div className="access-panel">
			<div className="access-panel-heading"><div><strong>Новый клиент</strong><small>Нулевой срок или лимит означает отсутствие ограничения.</small></div></div>
			<div className="form-grid two">
				<label className="form-field"><span>Имя</span><input value={name} onChange={e => setName(e.target.value)} placeholder="Телефон Ивана" disabled={busy}/></label>
				<label className="form-field"><span>Действует до</span><input type="datetime-local" value={expires} onChange={e => setExpires(e.target.value)} disabled={busy}/></label>
				<label className="form-field"><span>Лимит, ГБ</span><input type="number" min={0} step="0.1" value={limitGB} onChange={e => setLimitGB(e.target.value)} placeholder="Без лимита" disabled={busy}/></label>
			</div>
			<div className="action-bar"><button className="primary-button" type="button" disabled={busy || !name.trim() || (Boolean(limitGB) && (!Number.isFinite(Number(limitGB)) || Number(limitGB) < 0))} onClick={() => void create()}>Создать подписку</button></div>
		</div>
		<div className="reverse-client-list">
			{config.clients.length === 0 ? <p className="empty-state">Клиентов пока нет.</p> : config.clients.map(client => <ReverseVPNClientCard key={JSON.stringify(client)} client={client} busy={busy} run={run} onReload={onReload}/>) }
		</div>
	</div>;
}

function ReverseVPNClientCard({ client, busy, run, onReload }: { client: ReverseVPNClient; busy: boolean; run: Runner; onReload: () => Promise<void> }) {
	const [name, setName] = useState(client.name);
	const [enabled, setEnabled] = useState(client.enabled);
	const [expires, setExpires] = useState(client.expires_at ? localDateTime(client.expires_at) : "");
	const [limitGB, setLimitGB] = useState(client.traffic_limit_bytes ? String(Math.round(client.traffic_limit_bytes / 1024 ** 3 * 10) / 10) : "");
	const payload = (extra: { reset_traffic?: boolean; rotate_token?: boolean } = {}) => ({ name, enabled, expires_at: expires ? Math.floor(new Date(expires).getTime() / 1000) : 0, traffic_limit_bytes: limitGB ? Math.round(Number(limitGB) * 1024 ** 3) : 0, ...extra });
	const reloadAfter = async (operation: () => Promise<unknown>, message: string, title: string) => { if (await run(operation, message, { title })) await onReload(); };
	const copy = async () => { const value = await actions.reverseVPNSubscription(client.id); await navigator.clipboard.writeText(value.subscription_url.startsWith("https://") ? value.subscription_url : new URL(value.subscription_path, window.location.origin).toString()); };
	const download = async () => { const value = await actions.reverseVPNProfile(client.id); const url = URL.createObjectURL(new Blob([value.profile], { type: "text/plain" })); const anchor = document.createElement("a"); anchor.href = url; anchor.download = `${safeFilename(client.name)}.conf`; anchor.click(); URL.revokeObjectURL(url); };
	return <details className="reverse-client-card">
		<summary><span className={`node-status ${client.available ? "alive" : ""}`}/><span><strong>{client.name}</strong><small>{client.address} · {formatBytes(client.traffic_used_bytes)}{client.traffic_limit_bytes ? ` из ${formatBytes(client.traffic_limit_bytes)}` : ""}</small></span><em>{client.available ? "Доступен" : "Отключён"}</em></summary>
		<div className="reverse-client-body">
			<div className="form-grid two"><label className="form-field"><span>Имя</span><input value={name} onChange={e => setName(e.target.value)} disabled={busy}/></label><label className="choice-row"><input type="checkbox" checked={enabled} onChange={e => setEnabled(e.target.checked)} disabled={busy}/><span><strong>Клиент включён</strong></span></label><label className="form-field"><span>Действует до</span><input type="datetime-local" value={expires} onChange={e => setExpires(e.target.value)} disabled={busy}/></label><label className="form-field"><span>Лимит, ГБ</span><input type="number" min={0} step="0.1" value={limitGB} onChange={e => setLimitGB(e.target.value)} disabled={busy}/></label></div>
			<div className="pool-audit"><div><span>Получено <strong>{formatBytes(client.traffic_rx_bytes)}</strong></span><span>Отправлено <strong>{formatBytes(client.traffic_tx_bytes)}</strong></span><span>Последняя связь <strong>{client.last_seen_at ? new Date(client.last_seen_at * 1000).toLocaleString("ru-RU") : "—"}</strong></span></div></div>
			<div className="reverse-client-actions"><button type="button" onClick={() => void copy()} disabled={busy}>Копировать подписку</button><button type="button" onClick={() => void download()} disabled={busy || !client.available}>Скачать профиль</button><button type="button" onClick={() => void reloadAfter(() => actions.updateReverseVPNClient(client.id, payload()), "Клиент обновлён.", "Обновляем клиента")} disabled={busy || !name.trim()}>Сохранить</button><button type="button" onClick={() => void reloadAfter(() => actions.updateReverseVPNClient(client.id, payload({ reset_traffic: true })), "Счётчик трафика сброшен.", "Сбрасываем трафик")} disabled={busy}>Сбросить трафик</button><button type="button" onClick={() => void reloadAfter(() => actions.updateReverseVPNClient(client.id, payload({ rotate_token: true })), "Ссылка подписки заменена.", "Меняем ссылку")} disabled={busy}>Сменить ссылку</button><button className="danger" type="button" onClick={() => confirm(`Удалить ${client.name}?`) && void reloadAfter(() => actions.deleteReverseVPNClient(client.id), "Клиент удалён.", "Удаляем клиента")} disabled={busy}>Удалить</button></div>
		</div>
	</details>;
}

const localDateTime = (timestamp: number) => { const date = new Date(timestamp * 1000); date.setMinutes(date.getMinutes() - date.getTimezoneOffset()); return date.toISOString().slice(0, 16); };
const formatBytes = (value: number) => value >= 1024 ** 3 ? `${(value / 1024 ** 3).toFixed(2)} ГБ` : value >= 1024 ** 2 ? `${(value / 1024 ** 2).toFixed(1)} МБ` : `${Math.round(value / 1024)} КБ`;
const safeFilename = (value: string) => value.replace(/[^a-zA-Z0-9а-яА-Я._-]+/g, "-").replace(/^-+|-+$/g, "") || "orcheroute-client";
const reverseVPNError = (value: string) => value.startsWith("dependency_missing:")
	? `Не установлен компонент ${value.slice("dependency_missing:".length)}. Выполните: sudo apt install wireguard-tools iptables.`
	: value;
