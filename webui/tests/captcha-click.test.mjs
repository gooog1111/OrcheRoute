import test from "node:test";
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import vm from "node:vm";

const java = await readFile(new URL("../../android/app/src/main/java/online/gooog1111/orcheroute/VkCaptchaAutoClick.java", import.meta.url), "utf8");
const script = java.match(/SCRIPT = """([\s\S]*?)""";/)[1].replaceAll("\\\\", "\\");
function fixture(host = "id.vk.ru", text = "Я не робот", checkbox = false, protocol = "https:", port = "", label = false) {
  let clicks = 0, stopped = false, tick, touch;
  const element = {
    innerText: text, disabled: false, checked: false,
    getAttribute: () => null, matches: selector => selector === "label" ? label : checkbox,
    querySelector: () => label ? { disabled: false, checked: false } : null,
    getBoundingClientRect: () => ({ width: 100, height: 30 }), click: () => clicks++,
  };
  const context = vm.createContext({
    location: { protocol, hostname: host, port, pathname: "/not_robot_captcha" },
    window: { addEventListener() {} },
    document: { querySelectorAll: selector => label && !selector.includes("label") ? [] : [element], addEventListener: (_, fn) => touch = fn },
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
test("FreeTURN local proxy CAPTCHA is supported only on the dedicated port", () => {
  assert.equal(fixture("localhost", "Я не робот", false, "http:", "8765").clicks(), 1);
  assert.equal(fixture("127.0.0.1", "Я не робот", false, "http:", "8765").clicks(), 1);
  assert.equal(fixture("localhost", "Я не робот", false, "http:", "19110").clicks(), 0);
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

test("VK visible label can activate its hidden checkbox, but unrelated labels cannot", () => {
  const f = fixture("localhost", "Я не робот", false, "http:", "8765", true);
  assert.equal(f.clicks(), 1);
  f.tick();
  assert.equal(f.clicks(), 1);
  assert.equal(fixture("localhost", "Согласен с условиями", false, "http:", "8765", true).clicks(), 0);
  assert.match(script, /querySelectorAll\('[^']*label/);
});

test("CAPTCHA uses a bounded draggable card without a fullscreen system window", async () => {
  const dialog = await readFile(new URL("../../android/app/src/main/java/online/gooog1111/orcheroute/VkCaptchaDialog.java", import.meta.url), "utf8");
  assert.match(dialog, /cardWidth = Math\.min/);
  assert.match(dialog, /cardHeight = Math\.min/);
  assert.match(dialog, /FLAG_NOT_TOUCH_MODAL/);
  assert.match(dialog, /handle\.setOnTouchListener/);
  assert.match(dialog, /MotionEvent\.ACTION_MOVE/);
  assert.match(dialog, /updateViewLayout\(overlay, window\)/);
  assert.match(dialog, /cardWidth,\s+cardHeight,\s+WindowManager\.LayoutParams\.TYPE_APPLICATION_OVERLAY/);
  assert.doesNotMatch(dialog, /WindowManager\.LayoutParams\.MATCH_PARENT/);
  assert.match(dialog, /webLayout\.topMargin = headerHeight/);
});
