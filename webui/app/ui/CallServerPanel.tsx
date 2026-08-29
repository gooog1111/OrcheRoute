"use client";

import { useMemo, useState } from "react";
import { actions, type CallServerClient, type CallServerConfig, type CallServerState } from "../lib/api";
import { copyText } from "../lib/clipboard";

type Runner = (operation: () => Promise<unknown>, success: string, options?: { title?: string }) => Promise<boolean>;

export function CallServerPanel({ state, busy, run, onReload }: {
	state: CallServerState | null;
	busy: boolean;
	run: Runner;
	onReload: () => Promise<void>;
}) {
	const [config, setConfig] = useState<CallServerConfig | null>(state?.config ?? null);
	const [invitation, setInvitation] = useState("");
	const [name, setName] = useState("");
	const [expires, setExpires] = useState("");
	const [limitGB, setLimitGB] = useState("");
	const changed = useMemo(() => Boolean(config && state && (
		invitation.trim() || JSON.stringify(config) !== JSON.stringify(state.config)
	)), [config, invitation, state]);

	if (!config || !state) return <div className="settings-section"><p className="empty-state">Модуль VPN-сервера недоступен в этой сборке.</p></div>;

	const update = <K extends keyof CallServerConfig>(key: K, value: CallServerConfig[K]) => setConfig({ ...config, [key]: value });
	const save = async () => {
		if (!changed) return;
		const payload = {
			version: config.version,
			enabled: config.enabled,
			listen_address: config.listen_address,
			public_endpoint: config.public_endpoint,
			backend_address: config.backend_address,
			subscription_base_url: config.subscription_base_url,
			...(invitation.trim() ? { invitation_url: invitation.trim() } : {}),
		};
		if (await run(() => actions.saveCallServer(payload), "Настройки VPN-сервера сохранены.", { title: "Сохраняем VPN-сервер" })) {
			setInvitation("");
			await onReload();
		}
	};
	const toggle = async () => {
		const enabling = !config.enabled;
		if (await run(() => actions.setCallServerEnabled(enabling), enabling ? "VPN-сервер запущен." : "VPN-сервер остановлен.", { title: enabling ? "Запускаем VPN-сервер" : "Останавливаем VPN-сервер" })) await onReload();
	};
	const create = async () => {
		const expiresAt = expires ? Math.floor(new Date(expires).getTime() / 1000) : undefined;
		const limit = limitGB ? Math.round(Number(limitGB) * 1024 ** 3) : undefined;
		if (await run(() => actions.createCallServerClient({ name, expires_at: expiresAt, traffic_limit_bytes: limit }), "Клиент и персональная подписка созданы.", { title: "Создаём VPN-клиента" })) {
			setName(""); setExpires(""); setLimitGB(""); await onReload();
		}
	};
	const ready = Boolean(config.public_endpoint && config.invitation_configured && config.clients.some(client => client.available));

	return <div className="settings-section call-server-settings">
		<div className="section-heading with-action"><div><span>Входящий VPN · beta</span><h3>OrcheRoute Call Server</h3><p>Персональные профили Xray/VLESS поверх транспорта звонка VK. Срок и лимит применяются отдельно к каждому клиенту.</p></div><span className={`reverse-vpn-state ${state.status.active ? "active" : ""}`}>{state.status.active ? "Работает" : config.enabled ? "Не запущен" : "Выключен"}</span></div>
		{state.status.last_error && <p className="inline-feedback error">{callServerError(state.status.last_error)}</p>}
		<div className="access-panel">
			<div className="form-grid two">
				<label className="form-field"><span>Публичный UDP-адрес</span><input value={config.public_endpoint ?? ""} onChange={event => update("public_endpoint", event.target.value)} placeholder="203.0.113.25:4443" disabled={busy}/><small>Внешний IP сервера и проброшенный UDP-порт. DNS-имя здесь не используется.</small></label>
				<label className="form-field"><span>Адрес подписок</span><input type="url" value={config.subscription_base_url ?? ""} onChange={event => update("subscription_base_url", event.target.value)} placeholder="https://vpn.example.ru" disabled={busy}/><small>Публичный HTTPS-адрес этой панели без пути.</small></label>
				<label className="form-field"><span>Слушать DTLS</span><input value={config.listen_address} onChange={event => update("listen_address", event.target.value)} placeholder="0.0.0.0:4443" disabled={busy}/></label>
				<label className="form-field"><span>Локальный VLESS</span><input value={config.backend_address} onChange={event => update("backend_address", event.target.value)} placeholder="127.0.0.1:18443" disabled={busy}/><small>Только loopback-адрес встроенного Xray.</small></label>
				<label className="form-field form-field-wide"><span>Ссылка-приглашение VK Call</span><input type="url" value={invitation} onChange={event => setInvitation(event.target.value)} placeholder={config.invitation_configured ? "Ссылка уже сохранена; оставьте пустым, чтобы не менять" : "https://vk.com/call/join/..."} disabled={busy}/><small>{config.invitation_configured ? "Действующая ссылка скрыта и не будет очищена при сохранении." : "Используется клиентом для получения краткоживущих TURN-реквизитов."}</small></label>
			</div>
			<div className="action-bar"><button className="secondary-button" type="button" disabled={busy || !changed} onClick={() => void save()}>Сохранить настройки</button><button className={config.enabled ? "danger-button" : "primary-button"} type="button" disabled={busy || changed || (!config.enabled && !ready)} onClick={() => void toggle()}>{config.enabled ? "Остановить сервер" : "Запустить сервер"}</button></div>
		</div>
		<div className="access-panel">
			<div className="access-panel-heading"><div><strong>Новый клиент</strong><small>Нулевой срок или лимит означает отсутствие ограничения.</small></div></div>
			<div className="form-grid two">
				<label className="form-field"><span>Имя</span><input value={name} onChange={event => setName(event.target.value)} placeholder="Телефон Ивана" disabled={busy}/></label>
				<label className="form-field"><span>Действует до</span><input type="datetime-local" value={expires} onChange={event => setExpires(event.target.value)} disabled={busy}/></label>
				<label className="form-field"><span>Лимит, ГБ</span><input type="number" min={0} step="0.1" value={limitGB} onChange={event => setLimitGB(event.target.value)} placeholder="Без лимита" disabled={busy}/></label>
			</div>
			<div className="action-bar"><button className="primary-button" type="button" disabled={busy || !name.trim() || !config.public_endpoint || !config.invitation_configured || (Boolean(limitGB) && (!Number.isFinite(Number(limitGB)) || Number(limitGB) < 0))} onClick={() => void create()}>Создать подписку</button></div>
		</div>
		<div className="reverse-client-list">
			{config.clients.length === 0 ? <p className="empty-state">Клиентов пока нет.</p> : config.clients.map(client => <CallServerClientCard key={client.id} client={client} busy={busy} run={run} onReload={onReload}/>) }
		</div>
	</div>;
}

function CallServerClientCard({ client, busy, run, onReload }: { client: CallServerClient; busy: boolean; run: Runner; onReload: () => Promise<void> }) {
	const [name, setName] = useState(client.name);
	const [enabled, setEnabled] = useState(client.enabled);
	const [expires, setExpires] = useState(client.expires_at ? localDateTime(client.expires_at) : "");
	const [limitGB, setLimitGB] = useState(client.traffic_limit_bytes ? String(Math.round(client.traffic_limit_bytes / 1024 ** 3 * 10) / 10) : "");
	const [copyStatus, setCopyStatus] = useState<"idle" | "copied" | "error">("idle");
	const payload = (extra: { reset_traffic?: boolean; rotate_token?: boolean } = {}) => ({ name, enabled, expires_at: expires ? Math.floor(new Date(expires).getTime() / 1000) : 0, traffic_limit_bytes: limitGB ? Math.round(Number(limitGB) * 1024 ** 3) : 0, ...extra });
	const reloadAfter = async (operation: () => Promise<unknown>, message: string, title: string) => { if (await run(operation, message, { title })) await onReload(); };
	const copy = async () => {
		try {
			const value = await actions.callServerSubscription(client.id);
			await copyText(value.subscription_url.startsWith("https://") ? value.subscription_url : new URL(value.subscription_path, window.location.origin).toString());
			setCopyStatus("copied");
		} catch { setCopyStatus("error"); }
		window.setTimeout(() => setCopyStatus("idle"), 1800);
	};
	const download = async () => { const value = await actions.callServerProfile(client.id); const url = URL.createObjectURL(new Blob([value.profile], { type: "text/plain" })); const anchor = document.createElement("a"); anchor.href = url; anchor.download = `${safeFilename(client.name)}.txt`; anchor.click(); URL.revokeObjectURL(url); };
	return <details className="reverse-client-card">
		<summary><span className={`node-status ${client.available ? "alive" : ""}`}/><span><strong>{client.name}</strong><small>{formatBytes(client.traffic_used_bytes)}{client.traffic_limit_bytes ? ` из ${formatBytes(client.traffic_limit_bytes)}` : ""}</small></span><em>{client.available ? "Доступен" : "Отключён"}</em></summary>
		<div className="reverse-client-body">
			<div className="form-grid two"><label className="form-field"><span>Имя</span><input value={name} onChange={event => setName(event.target.value)} disabled={busy}/></label><label className="choice-row"><input type="checkbox" checked={enabled} onChange={event => setEnabled(event.target.checked)} disabled={busy}/><span><strong>Клиент включён</strong></span></label><label className="form-field"><span>Действует до</span><input type="datetime-local" value={expires} onChange={event => setExpires(event.target.value)} disabled={busy}/></label><label className="form-field"><span>Лимит, ГБ</span><input type="number" min={0} step="0.1" value={limitGB} onChange={event => setLimitGB(event.target.value)} disabled={busy}/></label></div>
			<div className="pool-audit"><div><span>Получено <strong>{formatBytes(client.traffic_rx_bytes)}</strong></span><span>Отправлено <strong>{formatBytes(client.traffic_tx_bytes)}</strong></span><span>Последняя связь <strong>{client.last_seen_at ? new Date(client.last_seen_at * 1000).toLocaleString("ru-RU") : "—"}</strong></span></div></div>
			<div className="reverse-client-actions"><button type="button" onClick={() => void copy()} disabled={busy}>{copyStatus === "copied" ? "Скопировано" : copyStatus === "error" ? "Ошибка копирования" : "Копировать подписку"}</button><button type="button" onClick={() => void download()} disabled={busy || !client.available}>Скачать профиль</button><button type="button" onClick={() => void reloadAfter(() => actions.updateCallServerClient(client.id, payload()), "Клиент обновлён.", "Обновляем клиента")} disabled={busy || !name.trim()}>Сохранить</button><button type="button" onClick={() => void reloadAfter(() => actions.updateCallServerClient(client.id, payload({ reset_traffic: true })), "Счётчик трафика сброшен.", "Сбрасываем трафик")} disabled={busy}>Сбросить трафик</button><button type="button" onClick={() => void reloadAfter(() => actions.updateCallServerClient(client.id, payload({ rotate_token: true })), "Ссылка подписки заменена.", "Меняем ссылку")} disabled={busy}>Сменить ссылку</button><button className="danger" type="button" onClick={() => confirm(`Удалить ${client.name}?`) && void reloadAfter(() => actions.deleteCallServerClient(client.id), "Клиент удалён.", "Удаляем клиента")} disabled={busy}>Удалить</button></div>
		</div>
	</details>;
}

const localDateTime = (timestamp: number) => { const date = new Date(timestamp * 1000); date.setMinutes(date.getMinutes() - date.getTimezoneOffset()); return date.toISOString().slice(0, 16); };
const formatBytes = (value: number) => value >= 1024 ** 3 ? `${(value / 1024 ** 3).toFixed(2)} ГБ` : value >= 1024 ** 2 ? `${(value / 1024 ** 2).toFixed(1)} МБ` : `${Math.round(value / 1024)} КБ`;
const safeFilename = (value: string) => value.replace(/[^a-zA-Z0-9а-яА-Я._-]+/g, "-").replace(/^-+|-+$/g, "") || "orcheroute-client";
const callServerError = (value: string) => ({
	call_server_no_active_clients: "Нет ни одного активного клиента.",
	call_server_not_configured: "Сначала сохраните публичный адрес и ссылку-приглашение VK Call.",
	call_server_xray_stats_unavailable: "Встроенный Xray запущен без поддержки учёта трафика.",
}[value] ?? value);
