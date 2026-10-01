import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import vm from "node:vm";

const java = await readFile(new URL("../../android/app/src/main/java/online/gooog1111/orcheroute/VkCaptchaAutoClick.java", import.meta.url), "utf8");
const script = java.match(/SCRIPT = """([\s\S]*?)""";/)[1].replaceAll("\\\\", "\\");
function fixture(host = "id.vk.ru", text = "Я не робот", checkbox = false) {
  let clicks = 0, stopped = false, tick, touch;
  const element = {
    innerText: text, disabled: false, checked: false,
    getAttribute: () => null, matches: () => checkbox,
    getBoundingClientRect: () => ({ width: 100, height: 30 }), click: () => clicks++,
  };
  const context = vm.createContext({
    location: { protocol: "https:", hostname: host, pathname: "/not_robot_captcha" },
    window: { addEventListener() {} },
    document: { querySelectorAll: () => [element], addEventListener: (_, fn) => touch = fn },
    getComputedStyle: () => ({ visibility: "visible", display: "block", opacity: "1" }),
    setInterval: fn => { tick = fn; return 1; }, clearInterval: () => stopped = true, setTimeout() {},
  });
  vm.runInContext(script, context);
  return { element, context, clicks: () => clicks, stopped: () => stopped, tick: () => tick?.(), touch: () => touch?.({ isTrusted: true }) };
}
test("CAPTCHA window clicks initial control only once, including nonbreaking spaces", () => {
  const f = fixture("id.vk.ru", "I'm not a\u00a0robot");
  assert.equal(f.clicks(), 1);
  f.tick(); vm.runInContext(script, f.context);
  assert.equal(f.clicks(), 1);
});
test("CAPTCHA window does not click unrelated buttons or foreign origins", () => {
  assert.equal(fixture("evil.example").clicks(), 0);
  assert.equal(fixture("id.vk.ru", "Закрыть").clicks(), 0);
});
test("CAPTCHA window supports checkbox and stops automation on real user interaction", () => {
  const f = fixture("api.vk.ru", "", true);
  assert.equal(f.clicks(), 1);
  f.touch(); assert.equal(f.stopped(), true);
});
test("CAPTCHA window never fabricates success or submits a token", () => {
  assert.doesNotMatch(script, /OrcheRouteCaptcha|success_token|complete\(/);
  assert.match(script, /count >= 3/);
  assert.match(script, /setTimeout\(stop, 20000\)/);
});
