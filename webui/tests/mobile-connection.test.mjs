import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import { mobileConnectionView } from "../app/lib/mobile-connection.mjs";

test("saved VPN intent is not displayed as an established connection after process restart", () => {
  for (const state of [undefined, "disabled", "error", "starting", "permission_required"]) {
    assert.notEqual(mobileConnectionView(true, state).title, "OrcheRoute включён");
    assert.notEqual(mobileConnectionView(true, state).label, "Подключено");
  }
  assert.equal(mobileConnectionView(true, "connected").title, "OrcheRoute включён");
  assert.equal(mobileConnectionView(false, "connected").title, "OrcheRoute выключен");
});

test("unexpected service destruction is reported before tunnel cleanup and removes foreground notification", async () => {
  const source = await readFile(new URL("../../android/app/src/main/java/online/gooog1111/orcheroute/OrcheRouteVpnService.java", import.meta.url), "utf8");
  const destroy = source.match(/public void onDestroy\(\) \{[\s\S]*?super\.onDestroy\(\);/)[0];
  assert.match(destroy, /!stopping && \(connected \|\| starting\)/);
  assert.ok(destroy.indexOf("onTransportError(") < destroy.indexOf("stopTunnel();"));
  assert.match(destroy, /stopForeground\(true\)/);
  assert.doesNotMatch(destroy, /onDisabled|setDesiredEnabled/);
});
