import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";
import { dirname } from "node:path";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";

const source = (path) => readFile(new URL(path, import.meta.url), "utf8");

test("server clock uses server timestamp/timezone with monotonic ticks and cleans up timers", async () => {
  const clock = await source("../app/ui/ServerClock.tsx");
  assert.match(clock, /value\?\.unix/);
  assert.match(clock, /timeZone: value.timezone/);
  assert.match(clock, /unix \* 1000 \+ performance.now\(\) - anchor/);
  assert.match(clock, /window.clearInterval\(timer\)/);
  assert.match(clock, /window.cancelAnimationFrame\(frame\)/);
  assert.doesNotMatch(clock, /Date.now\(\)/);
  assert.match(clock, /Не синхронизировано/);
});

test("time display and synchronization belong to Linux and update settings", async () => {
  const dashboard = await source("../app/ui/Dashboard.tsx");
  const settings = await source("../app/ui/SettingsModal.tsx");
  const api = await source("../app/lib/api.ts");
  assert.match(dashboard, /platform.kind !== "android" && <ServerClock/);
  assert.match(settings, /!embedded && \([\s\S]*?<strong>Время сервера<\/strong>/);
  assert.match(settings, /actions.syncSystemTime\(\)/);
  assert.match(settings, /await getServerSystemTime\(\)/);
  assert.match(settings, /window.setInterval\(refreshTime, 3000\)/);
  assert.match(api, /"\/v1\/system\/time\/sync", \{ method: "POST", body: "\{\}"/);
});

test("clock renders the server's date and timezone, not the browser's time", async () => {
  const require = createRequire(import.meta.url);
  const ts = require("typescript");
  const Module = require("node:module");
  const filename = fileURLToPath(new URL("../app/ui/ServerClock.tsx", import.meta.url));
  const compiled = ts.transpileModule(await source("../app/ui/ServerClock.tsx"), {
    compilerOptions: { jsx: ts.JsxEmit.ReactJSX, module: ts.ModuleKind.CommonJS },
  });
  const component = new Module(filename);
  component.filename = filename;
  component.paths = Module._nodeModulePaths(dirname(filename));
  component._compile(compiled.outputText, filename);
  const value = { unix: Date.UTC(2026, 10, 6, 12) / 1000, timezone: "Europe/Moscow", status: "unsynchronized" };
  const markup = renderToStaticMarkup(createElement(component.exports.ServerClock, { value }));
  assert.match(markup, /2026-11-06T12:00:00.000Z/);
  assert.match(markup, /15:00:00/);
  assert.match(markup, /Не синхронизировано/);
  assert.match(markup, /Europe\/Moscow/);
  assert.equal(renderToStaticMarkup(createElement(component.exports.ServerClock, {})), "");
});
