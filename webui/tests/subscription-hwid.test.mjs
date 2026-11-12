import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";
import { dirname } from "node:path";
import { createElement } from "react";
import { renderToStaticMarkup } from "react-dom/server";

const require = createRequire(import.meta.url);
const ts = require("typescript");
const Module = require("node:module");
const filename = fileURLToPath(new URL("../app/ui/SubscriptionHWID.tsx", import.meta.url));
const source = await readFile(filename, "utf8");
const compiled = ts.transpileModule(source, { compilerOptions: { jsx: ts.JsxEmit.ReactJSX, module: ts.ModuleKind.CommonJS } });
const component = new Module(filename);
component.filename = filename;
component.paths = Module._nodeModulePaths(dirname(filename));
component.require = (id) => id === "../lib/api" ? { actions: {}, getSubscriptionHWIDs: () => Promise.resolve({}) } : id === "../lib/clipboard" ? {} : require(id);
component._compile(compiled.outputText, filename);
const Choice = component.exports.HWIDChoice;
const settings = { current: "current-device-hwid", saved: [{ id: "phone", name: "Мой телефон", value: "owned-phone-hwid" }] };

test("HWID selector renders disabled-by-default, current, saved and custom choices", () => {
  const html = renderToStaticMarkup(createElement(Choice, { mode: "custom", value: "owned-phone-hwid", settings, onChange() {} }));
  assert.match(html, /Не отправлять/);
  assert.match(html, /Текущее устройство/);
  assert.match(html, /Мой телефон/);
  assert.match(html, /Свой HWID/);
  assert.match(html, /value="saved:phone" selected=""/);
});

test("HWID choices keep selected identifiers without generating a new one", () => {
  const calls = [];
  const tree = Choice({ mode: "none", value: "", settings, onChange: (...args) => calls.push(args) });
  const find = (node) => {
    if (!node || typeof node !== "object") return null;
    if (node.type === "select") return node;
    for (const child of [node.props?.children].flat(Infinity)) { const found = find(child); if (found) return found; }
    return null;
  };
  const select = find(tree);
  for (const value of ["saved:phone", "current", "custom", "none"]) select.props.onChange({ target: { value } });
  assert.deepEqual(calls, [["custom", "owned-phone-hwid"], ["current", ""], ["custom", ""], ["none", ""]]);
  assert.doesNotMatch(source, /randomUUID|Math.random/);
});

test("saved HWIDs and choices share the subscription settings on both platforms", async () => {
  const modal = await readFile(new URL("../app/ui/SettingsModal.tsx", import.meta.url), "utf8");
  const java = await readFile(new URL("../../android/app/src/main/java/online/gooog1111/orcheroute/MobileRuntime.java", import.meta.url), "utf8");
  assert.match(modal, /<HWIDSettingsPanel disabled=\{changesLocked\}/);
  assert.match(modal, /hwid_mode: payload.parser === "standard" \? hwidMode : "none"/);
  assert.match(java, /Mobilecore.fetchSubscriptionWithHWID/);
  assert.match(java, /\/v1\/subscription-hwid/);
  assert.doesNotMatch(java, /Mobilecore.fetchSubscription\(/);
});

test("HWID controls use themed fields and the device catalog is collapsed below subscriptions", async () => {
  for (const mode of ["none", "current", "custom"]) {
    const html = renderToStaticMarkup(createElement(Choice, { mode, value: "x".repeat(256), settings, onChange() {} }));
    assert.match(html, /class="form-field"/);
    assert.match(html, /class="input-wrap"><select/);
    if (mode !== "none") assert.match(html, /class="input-wrap"><input/);
  }
  const panel = renderToStaticMarkup(createElement(component.exports.HWIDSettingsPanel, { disabled: false }));
  assert.match(panel, /^<details class="editor-card hwid-catalog">/);
  assert.match(panel, /<summary><strong>HWID устройств/);
  const modal = await readFile(new URL("../app/ui/SettingsModal.tsx", import.meta.url), "utf8");
  assert.ok(modal.indexOf("<HWIDSettingsPanel") > modal.indexOf('className="subscription-list editable"'));
  const css = await readFile(new URL("../app/globals.css", import.meta.url), "utf8");
  assert.match(css, /\.hwid-fields \{[^}]*grid-template-columns: minmax\(0, 1fr\)[^}]*min-inline-size: 0/);
});
