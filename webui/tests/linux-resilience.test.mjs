import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";

test("qualification offers independent target opening filter and inverse without treating 403 as success", async () => {
  const ui = await readFile(new URL("../app/ui/SettingsModal.tsx", import.meta.url), "utf8");
  assert.match(ui, /opening_filter_mode: openingMode/);
  assert.match(ui, /opening_filter_url: openingURL.trim\(\)/);
  assert.match(ui, /Допускать, если открывается/);
  assert.match(ui, /Не допускать, если открывается/);
  assert.match(ui, /403 не подходит/);
});

test("each issued subscription sends its own tri-state LAN permission", async () => {
  const ui = await readFile(new URL("../app/ui/CallServerPanel.tsx", import.meta.url), "utf8");
  assert.match(ui, /allow_lan: lanMode === "inherit" \? null : lanMode === "allow"/);
  assert.match(ui, /Доступ этой подписки к LAN сервера/);
});
