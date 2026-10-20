import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";

const service = await readFile(new URL("../../android/app/src/main/java/online/gooog1111/orcheroute/OrcheRouteVpnService.java", import.meta.url), "utf8");

test("notification restart uses an explicit immutable foreground-service reload intent", () => {
  assert.match(service, /"Перезапустить",\s+restartIntent\(\)/);
  const restart = service.match(/private PendingIntent restartIntent\(\)[\s\S]*?\n    }/)[0];
  assert.match(restart, /new Intent\(this, OrcheRouteVpnService.class\).setAction\(ACTION_RELOAD\)/);
  assert.match(restart, /FLAG_IMMUTABLE/);
  assert.match(restart, /getForegroundService\(this, 2, restart, flags\)/);
  assert.doesNotMatch(restart, /onDisabled|onProxyHealth|selectNode|setDesiredEnabled/);
  assert.match(service, /"Выключить",\s+stopIntent\(\)/);
});

test("passive FreeTURN outage reporting does not stop or switch the manually selected VPN", () => {
  const report = service.match(/private void reportFreeTURNState\([\s\S]*?\n    }/)[0];
  assert.match(report, /shouldReportUnavailable/);
  assert.match(report, /onTransportError/);
  assert.match(report, /onTransportRestored/);
  assert.doesNotMatch(report, /stopTunnel|stopFreeTURN|reload\(|onDisabled|selectNode/);
  const traffic = service.match(/private void updateTrafficNotification\(\)[\s\S]*?\n    }/)[0];
  assert.match(traffic, /reportFreeTURNState/);
  assert.ok(traffic.indexOf("reportFreeTURNState") < traffic.indexOf("if (freeTurnRecoveryTriggered.get()) return"));
});
