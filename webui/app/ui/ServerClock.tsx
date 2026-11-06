"use client";

import { useEffect, useState } from "react";
import type { SystemTimeStatus } from "../lib/api";

const labels: Record<SystemTimeStatus["status"], string> = {
  checking: "Проверка времени",
  synchronized: "Синхронизировано",
  unsynchronized: "Не синхронизировано",
  unavailable: "Синхронизация не проверена",
  invalid_date: "Проверьте дату сервера",
};

export function ServerClock({ value }: { value?: SystemTimeStatus }) {
  const unix = value?.unix;
  const [clock, setClock] = useState<number | null>(null);
  useEffect(() => {
    if (unix === undefined) return;
    const anchor = performance.now();
    const update = () => setClock(unix * 1000 + performance.now() - anchor);
    const frame = window.requestAnimationFrame(update);
    const timer = window.setInterval(update, 1000);
    return () => { window.cancelAnimationFrame(frame); window.clearInterval(timer); };
  }, [unix]);
  if (!value) return null;
  const current = new Date(clock ?? value.unix * 1000);
  let formatted: string;
  try {
    formatted = new Intl.DateTimeFormat("ru-RU", { dateStyle: "medium", timeStyle: "medium", timeZone: value.timezone }).format(current);
  } catch {
    formatted = current.toISOString().replace("T", " ").slice(0, 19);
  }
  return <div className="server-clock">
    <time dateTime={current.toISOString()}>Время сервера: {formatted} · {value.timezone}</time>
    <small role="status">{value.syncing ? "Синхронизация…" : value.error || labels[value.status]}</small>
  </div>;
}
