import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
const source = name => readFile(new URL(`../../android/app/src/main/java/online/gooog1111/orcheroute/${name}.java`, import.meta.url), "utf8");
test("Activity destruction detaches and destroys main WebView and shuts down updater", async () => {
  const activity = await source("MainActivity");
  const destroy = activity.match(/protected void onDestroy\(\) \{[\s\S]*?super\.onDestroy\(\);/)[0];
  for (const statement of ['appUpdater.close()', 'old.stopLoading()', 'old.removeJavascriptInterface("OrcheRouteAndroid")', 'root.removeView(old)', 'old.destroy()', 'webView = null']) assert.ok(destroy.includes(statement), statement);
  assert.ok(destroy.indexOf('root.removeView(old)') < destroy.indexOf('old.destroy()'));
  assert.doesNotMatch(destroy, /OrcheRouteVpnService\.stop|onDisabled/);
  assert.match(activity, /webView != null && !isDestroyed\(\)/);
});
test("destroyed updater rejects tasks and cannot launch an installer", async () => {
  const updater = await source("AppUpdater");
  assert.match(updater, /synchronized void close\(\)[\s\S]*closed = true;[\s\S]*worker.shutdownNow\(\)/);
  assert.equal((updater.match(/if \(closed \|\| active\) return false/g) ?? []).length, 3);
  assert.match(updater, /if \(closed \|\| activity.isFinishing\(\) \|\| activity.isDestroyed\(\)\) return/);
  assert.match(updater, /closed \|\| Thread.currentThread\(\).isInterrupted\(\)/);
});
