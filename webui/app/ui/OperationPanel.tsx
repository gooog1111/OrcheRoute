"use client";

export type OperationView = {
  state: "idle" | "pending" | "running" | "success" | "error";
  title: string;
  detail: string;
  step: number;
  steps: string[];
  progress?: { current: number; total: number };
};

type OperationAction = { label: string; onClick: () => void; disabled?: boolean };

const progressValue = (value: number, total: number) => {
  if (total < 1024 * 1024) return String(value);
  return `${(value / 1024 / 1024).toFixed(1)} МБ`;
};

export function OperationPanel({ operation, onDismiss, action }: { operation: OperationView | null; onDismiss?: () => void; action?: OperationAction }) {
  if (!operation) return null;
  const statusLabel = operation.state === "running" ? "Выполняется" : operation.state === "success" ? "Готово" : operation.state === "error" ? "Ошибка" : operation.state === "pending" ? "Ожидает применения" : "Состояние";
  return <aside className={`operation-panel ${operation.state}`} role={operation.state === "error" ? "alert" : "status"} aria-live="polite">
    <div className="operation-panel-head">
      <span className="operation-indicator" />
      <div><small>{statusLabel}</small><strong>{operation.title}</strong></div>
      {operation.state !== "running" && onDismiss && <button type="button" onClick={onDismiss} aria-label="Закрыть статус">×</button>}
    </div>
    <p>{operation.detail}</p>
    {operation.progress && operation.progress.total > 0 && <div className="operation-progress-wrap">
      <div className="operation-progress"><span style={{ width: `${Math.min(100, operation.progress.current / operation.progress.total * 100)}%` }} /></div>
      <small>{progressValue(operation.progress.current, operation.progress.total)} / {progressValue(operation.progress.total, operation.progress.total)}</small>
    </div>}
    {operation.steps.length > 0 && <ol>{operation.steps.map((label, index) => <li key={label} className={index < operation.step || operation.state === "success" ? "done" : index === operation.step && operation.state === "running" ? "active" : index === operation.step && operation.state === "error" ? "failed" : ""}><span>{index < operation.step || operation.state === "success" ? "✓" : index === operation.step && operation.state === "error" ? "!" : index + 1}</span>{label}</li>)}</ol>}
    {action && operation.state !== "running" && <div className="operation-actions"><button className="primary-button" type="button" disabled={action.disabled} onClick={action.onClick}>{action.label}</button></div>}
  </aside>;
}
