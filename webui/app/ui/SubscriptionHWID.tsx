"use client";

import { useEffect, useState } from "react";
import { actions, getSubscriptionHWIDs, type SubscriptionHWIDs, type SavedHWID } from "../lib/api";
import { copyText } from "../lib/clipboard";

export function HWIDSettingsPanel({ disabled }: { disabled: boolean }) {
  const [settings, setSettings] = useState<SubscriptionHWIDs | null>(null);
  const [saved, setSaved] = useState<SavedHWID[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => {
    let active = true;
    getSubscriptionHWIDs().then((value) => { if (active) { setSettings(value); setSaved(value.saved); } })
      .catch(() => { if (active) setError("Не удалось загрузить HWID"); });
    return () => { active = false; };
  }, []);
  const changed = JSON.stringify(saved) !== JSON.stringify(settings?.saved ?? []);
  const save = async () => {
    setBusy(true); setError("");
    try {
      const value = await actions.saveSubscriptionHWIDs(saved);
      setSettings(value); setSaved(value.saved);
    } catch { setError("Проверьте названия и HWID. Повторяющиеся идентификаторы не допускаются."); }
    finally { setBusy(false); }
  };
  return <section className="editor-card">
    <strong>HWID</strong>
    <fieldset disabled={disabled || busy || !settings} className="hwid-fields">
      <label>Текущее устройство<input aria-label="HWID текущего устройства" readOnly value={settings?.current ?? ""} /></label>
      <button type="button" className="secondary-button" onClick={() => void copyText(settings?.current ?? "")}>Скопировать HWID</button>
      <strong>Сохранённые HWID</strong>
      {saved.map((item, index) => <div className="hwid-record" key={item.id || `new-${index}`}>
        <input aria-label="Название HWID" placeholder="Название" maxLength={64} value={item.name}
          onChange={(event) => setSaved((items) => items.map((current, position) => position === index ? { ...current, name: event.target.value } : current))} />
        <input aria-label="Сохранённый HWID" placeholder="HWID" maxLength={256} spellCheck={false} value={item.value}
          onChange={(event) => setSaved((items) => items.map((current, position) => position === index ? { ...current, value: event.target.value } : current))} />
        <button type="button" className="secondary-button" onClick={() => setSaved((items) => items.filter((_, position) => position !== index))}>Удалить</button>
      </div>)}
      <div className="import-actions">
        <button type="button" className="secondary-button" disabled={saved.length >= 64}
          onClick={() => setSaved((items) => [...items, { id: "", name: "", value: "" }])}>Добавить HWID</button>
        <button type="button" className="primary-button" disabled={!changed || saved.some((item) => !item.name.trim() || !item.value.trim())} onClick={() => void save()}>Сохранить</button>
      </div>
    </fieldset>
    {error && <div className="inline-feedback error" role="alert">{error}</div>}
  </section>;
}

type SelectionProps = {
  mode: "none" | "current" | "custom"; value: string;
  onChange: (mode: "none" | "current" | "custom", value: string) => void;
};

export function SubscriptionHWID(props: SelectionProps) {
  const [settings, setSettings] = useState<SubscriptionHWIDs | null>(null);
  useEffect(() => {
    let active = true;
    getSubscriptionHWIDs().then((result) => { if (active) setSettings(result); }).catch(() => {});
    return () => { active = false; };
  }, []);
  return <HWIDChoice {...props} settings={settings} />;
}

export function HWIDChoice({ mode, value, onChange, settings }: SelectionProps & { settings: SubscriptionHWIDs | null }) {
  const existing = mode === "custom" ? settings?.saved.find((item) => item.value === value) : undefined;
  const selected = existing ? `saved:${existing.id}` : mode;
  return <div className="hwid-fields">
    <label>HWID подписки
      <select value={selected} onChange={(event) => {
        const choice = event.target.value;
        if (choice.startsWith("saved:")) {
          const item = settings?.saved.find((candidate) => `saved:${candidate.id}` === choice);
          if (item) onChange("custom", item.value);
        } else onChange(choice as "none" | "current" | "custom", "");
      }}>
        <option value="none">Не отправлять</option>
        <option value="current" disabled={!settings?.current}>Текущее устройство</option>
        {settings?.saved.map((item) => <option key={item.id} value={`saved:${item.id}`}>{item.name}</option>)}
        <option value="custom">Свой HWID</option>
      </select>
    </label>
    {mode === "current" && <input aria-label="Выбранный HWID устройства" readOnly value={settings?.current ?? ""} />}
    {mode === "custom" && <input aria-label="HWID подписки" maxLength={256} spellCheck={false} value={value} placeholder="HWID"
      onChange={(event) => onChange("custom", event.target.value)} />}
  </div>;
}
