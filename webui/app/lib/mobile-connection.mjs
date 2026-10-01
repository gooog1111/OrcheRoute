/** Saved intent controls the stop button, but is not proof of a live tunnel.
 * @param {boolean} requested
 * @param {string | undefined} state
 */
export function mobileConnectionView(requested, state) {
  if (!requested) return { title: "OrcheRoute выключен", label: "Готов к запуску" };
  if (state === "connected") return { title: "OrcheRoute включён", label: "Подключено" };
  if (state === "error") return { title: "Соединение потеряно", label: "Выбранный сервер недоступен" };
  if (!state || state === "disabled") return { title: "VPN не подключён", label: "Соединение отсутствует" };
  if (state === "permission_required") return { title: "VPN не подключён", label: "Требуется разрешение Android" };
  if (state === "stopping") return { title: "Выключаем OrcheRoute", label: "Остановка" };
  return { title: "Подключаем OrcheRoute", label: "Подключение" };
}
