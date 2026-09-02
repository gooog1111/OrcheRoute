"use client";

import { useMemo, useState } from "react";
import QRCode from "qrcode";
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
	const [additionalInvitations, setAdditionalInvitations] = useState("");
	const [name, setName] = useState("");
	const [expires, setExpires] = useState("");
	const [limitGB, setLimitGB] = useState("");
	const changed = useMemo(() => Boolean(config && state && (
		invitation.trim() || additionalInvitations.trim() || JSON.stringify(config) !== JSON.stringify(state.config)
	)), [additionalInvitations, config, invitation, state]);

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
			ordinary_enabled: config.ordinary_enabled,
			vless_listen_address: config.vless_listen_address,
			trojan_listen_address: config.trojan_listen_address,
			hysteria2_listen_address: config.hysteria2_listen_address,
			fake_sni: config.fake_sni,
			...(invitation.trim() ? { invitation_url: invitation.trim() } : {}),
			...(additionalInvitations.trim() ? { invitation_urls: additionalInvitations.split(/\s+/).map(value => value.trim()).filter(Boolean) } : {}),
		};
		if (await run(() => actions.saveCallServer(payload), "Настройки VPN-сервера сохранены.", { title: "Сохраняем VPN-сервер" })) {
			setInvitation("");
			setAdditionalInvitations("");
			await onReload();
		}
	};
	const toggle = async () => {
		const enabling = !config.enabled;
		if (await run(() => actions.setCallServerEnabled(enabling), enabling ? "VPN-сервер запущен." : "VPN-сервер остановлен.", { title: enabling ? "Запускаем VPN-сервер" : "Останавливаем VPN-сервер" })) await onReload();
	};
	const autoConfigure = async () => {
		const result: { current?: Awaited<ReturnType<typeof actions.autoConfigureCallServer>> } = {};
		const ok = await run(async () => { result.current = await actions.autoConfigureCallServer(window.location.origin); }, "Сетевой тракт построен.", { title: "Определяем сеть и строим тракт" });
		const detected = result.current;
		if (!ok || !detected) return;
		await onReload();
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
		<div className="section-heading with-action"><div><span>Входящий VPN · beta</span><h3>OrcheRoute VPN Server</h3><p>Основной тракт: FreeTURN UDP → встроенный AWG. Дополнительно подписка может содержать VLESS, Trojan и Hysteria2. Fake SNI: {config.fake_sni}.</p></div><span className={`call-server-state ${state.status.active ? "active" : ""}`}>{state.status.active ? "Работает" : config.enabled ? "Не запущен" : "Выключен"}</span></div>
		{state.status.last_error && <p className="inline-feedback error">{callServerError(state.status.last_error)}</p>}
		<div className="access-panel call-auto-setup">
			<div className="access-panel-heading"><div><strong>Автоматическая настройка</strong><small>OrcheRoute определит внешний Direct IP и настроит FreeTURN UDP со встроенным AWG. Дополнительные VLESS, Trojan и Hysteria2 настраиваются отдельно. IP позже можно заменить доменным именем.</small></div></div>
			<div className="action-bar"><button className="primary-button" type="button" disabled={busy || config.enabled} onClick={() => void autoConfigure()}>{config.public_endpoint ? "Перестроить тракт" : "Построить тракт"}</button></div>
			{config.public_endpoint && <div className="pool-audit"><div><span>Публичный FreeTURN UDP <strong>{config.public_endpoint}</strong></span><span>Локальный AWG UDP <strong>{config.backend_address}</strong></span><span>Передача профиля <strong>{config.subscription_base_url ? "HTTPS-ссылка" : "Файл или QR"}</strong></span></div><small>После запуска публичный UDP-порт должен быть разрешён в firewall или проброшен на роутере. Локальный AWG-порт наружу открывать не нужно.</small></div>}
		</div>
		<div className="access-panel">
			<div className="form-grid two">
				<label className="form-field form-field-wide"><span>Основная ссылка VK для FreeTURN</span><input type="url" value={invitation} onChange={event => setInvitation(event.target.value)} placeholder={config.invitation_configured ? "Ссылка уже сохранена; оставьте пустым, чтобы не менять" : "https://vk.com/call/join/..."} disabled={busy}/><small>{config.invitation_configured ? `Сохранено ссылок: ${config.invitation_count || 1}. Основная ссылка скрыта.` : "Используется upstream FreeTURN для построения соединения."}</small></label>
				<label className="form-field form-field-wide"><span>Дополнительные ссылки VK</span><textarea value={additionalInvitations} onChange={event => setAdditionalInvitations(event.target.value)} placeholder="По одной независимой ссылке на строку" rows={3} disabled={busy}/><small>Каждая ссылка создаёт ещё 10 TURN-сессий. Оставьте пустым, чтобы сохранить действующий список.</small></label>
			</div>
			<details className="advanced-settings"><summary>Ручная настройка сети</summary><div className="form-grid two">
				<label className="checkbox-card form-field-wide"><input type="checkbox" checked={config.ordinary_enabled} onChange={event => update("ordinary_enabled", event.target.checked)} disabled={busy}/><span><strong>VLESS, Trojan и Hysteria2</strong><small>Добавлять эти серверы в персональную подписку.</small></span></label>
				<label className="form-field"><span>Публичный адрес FreeTURN</span><input value={config.public_endpoint ?? ""} onChange={event => update("public_endpoint", event.target.value)} placeholder="vpn.example.ru:4443" disabled={busy}/><small>Публичный IP или доменное имя и UDP-порт.</small></label>
				<label className="form-field"><span>Адрес подписок</span><input type="url" value={config.subscription_base_url ?? ""} onChange={event => update("subscription_base_url", event.target.value)} placeholder="Необязательно" disabled={busy}/><small>Нужен только для обновляемой HTTPS-ссылки. Файл и QR работают без домена.</small></label>
				<label className="form-field"><span>Слушать FreeTURN UDP</span><input value={config.listen_address} onChange={event => update("listen_address", event.target.value)} placeholder="0.0.0.0:4443" disabled={busy}/></label>
				<label className="form-field"><span>Локальный AWG UDP</span><input value={config.backend_address} onChange={event => update("backend_address", event.target.value)} placeholder="127.0.0.1:18443" disabled={busy}/><small>Внутренний userspace AWG. Этот адрес не является VLESS и не должен быть доступен извне.</small></label>
				<label className="form-field"><span>VLESS Reality</span><input value={config.vless_listen_address} onChange={event => update("vless_listen_address", event.target.value)} placeholder="0.0.0.0:24443" disabled={busy}/></label>
				<label className="form-field"><span>Trojan TLS</span><input value={config.trojan_listen_address} onChange={event => update("trojan_listen_address", event.target.value)} placeholder="0.0.0.0:24444" disabled={busy}/></label>
				<label className="form-field"><span>Hysteria2 UDP</span><input value={config.hysteria2_listen_address} onChange={event => update("hysteria2_listen_address", event.target.value)} placeholder="0.0.0.0:24445" disabled={busy}/></label>
				<label className="form-field"><span>Fake SNI</span><input value={config.fake_sni} onChange={event => update("fake_sni", event.target.value)} placeholder="m.vk.ru" disabled={busy}/></label>
			</div></details>
			<div className="action-bar"><button className="secondary-button" type="button" disabled={busy || !changed} onClick={() => void save()}>Сохранить настройки</button><button className={config.enabled ? "danger-button" : "primary-button"} type="button" disabled={busy || changed || (!config.enabled && !ready)} onClick={() => void toggle()}>{config.enabled ? "Остановить сервер" : "Запустить сервер"}</button></div>
		</div>
		<div className="access-panel">
			<div className="access-panel-heading"><div><strong>Новый клиент</strong><small>Нулевой срок или лимит означает отсутствие ограничения.</small></div></div>
			<div className="form-grid two">
				<label className="form-field"><span>Название</span><input value={name} onChange={event => setName(event.target.value)} placeholder="Необязательно" disabled={busy}/></label>
				<label className="form-field"><span>Действует до</span><input type="datetime-local" value={expires} onChange={event => setExpires(event.target.value)} disabled={busy}/></label>
				<label className="form-field"><span>Лимит, ГБ</span><input type="number" min={0} step="0.1" value={limitGB} onChange={event => setLimitGB(event.target.value)} placeholder="Без лимита" disabled={busy}/></label>
			</div>
			<div className="action-bar"><button className="primary-button" type="button" disabled={busy || !config.public_endpoint || !config.invitation_configured || (Boolean(limitGB) && (!Number.isFinite(Number(limitGB)) || Number(limitGB) < 0))} onClick={() => void create()}>Создать подписку</button></div>
		</div>
		<div className="call-client-list">
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
	const [downloadStatus, setDownloadStatus] = useState<"idle" | "started" | "error">("idle");
	const [profileQR, setProfileQR] = useState("");
	const [subscriptionURL, setSubscriptionURL] = useState("");
	const [subscriptionLinkError, setSubscriptionLinkError] = useState(false);
	const payload = (extra: { reset_traffic?: boolean; rotate_token?: boolean } = {}) => ({ name, enabled, expires_at: expires ? Math.floor(new Date(expires).getTime() / 1000) : 0, traffic_limit_bytes: limitGB ? Math.round(Number(limitGB) * 1024 ** 3) : 0, ...extra });
	const reloadAfter = async (operation: () => Promise<unknown>, message: string, title: string) => { if (await run(operation, message, { title })) await onReload(); };
	const loadSubscriptionURL = async () => {
		if (subscriptionURL) return subscriptionURL;
		const value = await actions.callServerSubscription(client.id);
		const address = new URL(value.subscription_url || value.subscription_path, window.location.origin).toString();
		setSubscriptionURL(address);
		return address;
	};
	const copy = async () => {
		try {
			await copyText(await loadSubscriptionURL());
			setCopyStatus("copied");
		} catch { setCopyStatus("error"); }
		window.setTimeout(() => setCopyStatus("idle"), 1800);
	};
	const revealSubscriptionURL = async () => {
		try {
			await loadSubscriptionURL();
			setSubscriptionLinkError(false);
		} catch {
			setSubscriptionLinkError(true);
		}
	};
	const download = async () => {
		try {
			const value = await actions.callServerProfile(client.id);
			const url = URL.createObjectURL(new Blob([value.profile], { type: "text/plain;charset=utf-8" }));
			const anchor = document.createElement("a");
			anchor.href = url;
			anchor.download = `${safeFilename(client.name)}.txt`;
			anchor.hidden = true;
			document.body.appendChild(anchor);
			anchor.click();
			window.setTimeout(() => { anchor.remove(); URL.revokeObjectURL(url); }, 1000);
			setDownloadStatus("started");
			window.setTimeout(() => setDownloadStatus("idle"), 1800);
		} catch {
			setDownloadStatus("error");
			window.setTimeout(() => setDownloadStatus("idle"), 3000);
		}
	};
	const showQR = async () => {
		setProfileQR(await QRCode.toDataURL(await loadSubscriptionURL(), { width: 320, margin: 2, errorCorrectionLevel: "M" }));
	};
	return <details className="call-client-card">
		<summary><span className={`node-status ${client.available ? "alive" : ""}`}/><span><strong>{client.name}</strong><small>{formatBytes(client.traffic_used_bytes)}{client.traffic_limit_bytes ? ` из ${formatBytes(client.traffic_limit_bytes)}` : ""}</small></span><em>{client.available ? "Доступен" : "Отключён"}</em></summary>
		<div className="call-client-body">
			<div className="form-grid two"><label className="form-field"><span>Название</span><input value={name} onChange={event => setName(event.target.value)} placeholder="Необязательно" disabled={busy}/></label><label className="choice-row"><input type="checkbox" checked={enabled} onChange={event => setEnabled(event.target.checked)} disabled={busy}/><span><strong>Клиент включён</strong></span></label><label className="form-field"><span>Действует до</span><input type="datetime-local" value={expires} onChange={event => setExpires(event.target.value)} disabled={busy}/></label><label className="form-field"><span>Лимит, ГБ</span><input type="number" min={0} step="0.1" value={limitGB} onChange={event => setLimitGB(event.target.value)} disabled={busy}/></label></div>
			<div className="pool-audit"><div><span>Получено <strong>{formatBytes(client.traffic_rx_bytes)}</strong></span><span>Отправлено <strong>{formatBytes(client.traffic_tx_bytes)}</strong></span><span>Последняя связь <strong>{client.last_seen_at ? new Date(client.last_seen_at * 1000).toLocaleString("ru-RU") : "—"}</strong></span></div></div>
			{(profileQR || subscriptionURL) && <div className="call-subscription-summary">
				<span>Остаток трафика <strong>{remainingTraffic(client)}</strong></span>
				<span>Подписка действует <strong>{expiryLabel(client.expires_at)}</strong></span>
			</div>}
			{profileQR && <div className="call-profile-qr">
				{/* eslint-disable-next-line @next/next/no-img-element */}
				<img src={profileQR} alt={`QR профиля ${client.name}`}/>
				<p>Отсканируйте QR в Android OrcheRoute. Не публикуйте его: внутри находятся персональные ключи.</p><button type="button" onClick={() => setProfileQR("")}>Закрыть QR</button>
			</div>}
			{subscriptionURL && <div className="call-subscription-link"><span>Прямая ссылка</span><a href={subscriptionURL} target="_blank" rel="noreferrer">{subscriptionURL}</a></div>}
			{subscriptionLinkError && <p className="inline-feedback error">Не удалось получить прямую ссылку подписки.</p>}
			<div className="call-client-actions"><button type="button" onClick={() => void revealSubscriptionURL()} disabled={busy}>{subscriptionURL ? "Ссылка показана" : "Показать ссылку"}</button><button type="button" onClick={() => void copy()} disabled={busy}>{copyStatus === "copied" ? "Скопировано" : copyStatus === "error" ? "Ошибка копирования" : "Копировать подписку"}</button><button type="button" onClick={() => void download()} disabled={busy || !client.available}>{downloadStatus === "started" ? "Скачивание начато" : downloadStatus === "error" ? "Ошибка скачивания" : "Скачать профиль"}</button><button type="button" onClick={() => void showQR()} disabled={busy || !client.available}>Показать QR</button><button type="button" onClick={() => void reloadAfter(() => actions.updateCallServerClient(client.id, payload()), "Клиент обновлён.", "Обновляем клиента")} disabled={busy}>Сохранить</button><button type="button" onClick={() => void reloadAfter(() => actions.updateCallServerClient(client.id, payload({ reset_traffic: true })), "Счётчик трафика сброшен.", "Сбрасываем трафик")} disabled={busy}>Сбросить трафик</button><button type="button" onClick={() => void reloadAfter(() => actions.updateCallServerClient(client.id, payload({ rotate_token: true })), "Ссылка подписки заменена.", "Меняем ссылку")} disabled={busy}>Сменить ссылку</button><button className="danger" type="button" onClick={() => confirm(`Удалить ${client.name}?`) && void reloadAfter(() => actions.deleteCallServerClient(client.id), "Клиент удалён.", "Удаляем клиента")} disabled={busy}>Удалить</button></div>
		</div>
	</details>;
}

const localDateTime = (timestamp: number) => { const date = new Date(timestamp * 1000); date.setMinutes(date.getMinutes() - date.getTimezoneOffset()); return date.toISOString().slice(0, 16); };
const formatBytes = (value: number) => value >= 1024 ** 3 ? `${(value / 1024 ** 3).toFixed(2)} ГБ` : value >= 1024 ** 2 ? `${(value / 1024 ** 2).toFixed(1)} МБ` : `${Math.round(value / 1024)} КБ`;
const remainingTraffic = (client: CallServerClient) => {
	if (!client.traffic_limit_bytes) return "без ограничения";
	const remaining = client.traffic_limit_bytes - client.traffic_used_bytes;
	return remaining > 0 ? `${formatBytes(remaining)} из ${formatBytes(client.traffic_limit_bytes)}` : "лимит исчерпан";
};
const expiryLabel = (expiresAt: number | undefined) => {
	if (!expiresAt) return "бессрочно";
	const days = Math.ceil((expiresAt * 1000 - Date.now()) / 86400000);
	const date = new Date(expiresAt * 1000).toLocaleDateString("ru-RU");
	if (days < 0) return `истекла ${date}`;
	if (days === 0) return `до конца дня (${date})`;
	return `до ${date} · ${days} ${daysWord(days)}`;
};
const daysWord = (days: number) => {
	const mod10 = days % 10, mod100 = days % 100;
	if (mod10 === 1 && mod100 !== 11) return "день";
	if ([2, 3, 4].includes(mod10) && ![12, 13, 14].includes(mod100)) return "дня";
	return "дней";
};
const safeFilename = (value: string) => value.replace(/[^a-zA-Z0-9а-яА-Я._-]+/g, "-").replace(/^-+|-+$/g, "") || "orcheroute-client";
const callServerError = (value: string) => ({
	call_server_no_active_clients: "Нет ни одного активного клиента.",
	call_server_not_configured: "Сначала сохраните публичный адрес и ссылку VK для FreeTURN.",
	call_server_xray_stats_unavailable: "Встроенный Xray запущен без поддержки учёта трафика.",
}[value] ?? value);
