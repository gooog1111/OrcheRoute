import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { createRequire } from "node:module";
const require = createRequire(import.meta.url);
const ts = require("typescript");
const Module = require("node:module");
const compiled = ts.transpileModule(await readFile(new URL("../app/lib/updateStatusSync.ts", import.meta.url), "utf8"), {
  compilerOptions: { module: ts.ModuleKind.CommonJS },
});
const module = new Module("update-status-sync");
module._compile(compiled.outputText, "update-status-sync.js");
const { createUpdateStatusSync } = module.exports;
const deferred = () => { let resolve; let reject; const promise = new Promise((ok, fail) => { resolve = ok; reject = fail; }); return { promise, resolve, reject }; };

test("check immediately shows progress and refreshes the version without reloading the page", async () => {
  const post = deferred(), old = deferred();
  const seen = []; let reads = 0;
  const sync = createUpdateStatusSync(() => ++reads === 1 ? old.promise : Promise.resolve("0.8.6"), (status) => seen.push(status));
  const stale = sync.refresh(); await Promise.resolve();
  const checking = sync.command(() => post.promise, "checking");
  assert.deepEqual(seen, ["checking"]);
  await sync.refresh(); assert.equal(reads, 1);
  post.resolve(); await checking;
  old.resolve("0.8.5"); await stale;
  assert.deepEqual(seen, ["checking", "0.8.6"]);
});

test("slow polling is serialized and disposal rejects late results", async () => {
  const result = deferred(); let reads = 0; const seen = [];
  const sync = createUpdateStatusSync(() => { reads++; return result.promise; }, (status) => seen.push(status));
  const first = sync.refresh(), second = sync.refresh();
  await Promise.resolve(); assert.equal(reads, 1); assert.equal(first, second);
  sync.dispose(); result.resolve("old"); await first;
  assert.deepEqual(seen, []);
});

test("failed action is reported to the UI and does not block the next check", async () => {
  const seen = [];
  const sync = createUpdateStatusSync(async () => "0.8.6", (status) => seen.push(status));
  await assert.rejects(sync.command(async () => { throw new Error("offline"); }, "checking"), /offline/);
  await sync.refresh();
  assert.deepEqual(seen, ["checking"]);
  await sync.command(async () => {}, "checking");
  assert.equal(seen.at(-1), "0.8.6");
});

test("closing settings while a command is pending does not update unmounted UI", async () => {
  const post = deferred(), seen = [];
  const sync = createUpdateStatusSync(async () => "0.8.6", (value) => seen.push(value));
  const command = sync.command(() => post.promise, "checking");
  sync.dispose(); post.resolve(); await command;
  assert.deepEqual(seen, ["checking"]);
});

test("update buttons and foreground events use the same ordered status reader", async () => {
  const source = await readFile(new URL("../app/ui/SettingsModal.tsx", import.meta.url), "utf8");
  assert.match(source, /createUpdateStatusSync\(getServerAppUpdateStatus, setAppUpdate\)/);
  assert.match(source, /runServerUpdate\(\(\) => actions.checkAppUpdate/);
  assert.match(source, /runServerUpdate\(\(\) => actions.installAppUpdate/);
  assert.match(source, /runServerUpdate\(\(\) => actions.setAppUpdateChannel/);
  assert.match(source, /window.addEventListener\("focus", refresh\)/);
  assert.match(source, /document.addEventListener\("visibilitychange", visible\)/);
  assert.match(source, /sync\?\.dispose\(\)/);
});
