package online.gooog1111.orcheroute;

/** Bounded assistance for the initial checkbox; never reports CAPTCHA success. */
final class VkCaptchaAutoClick {
    static final String SCRIPT = """
            (() => {
              const remote = location.protocol === 'https:' && ['id.vk.ru', 'api.vk.ru'].includes(location.hostname);
              const local = location.protocol === 'http:' && ['localhost', '127.0.0.1'].includes(location.hostname) && location.port === '8765';
              if ((!remote && !local) || !location.pathname.startsWith('/not_robot_captcha') || window.__orcheCaptchaClick) return;
              window.__orcheCaptchaClick = true;
              const clicked = new WeakSet();
              let count = 0, timer;
              const stop = () => { clearInterval(timer); };
              const tick = () => {
                if (count >= 3) { stop(); return; }
                for (const element of document.querySelectorAll('button, [role="checkbox"], input[type="checkbox"]')) {
                  const text = (element.getAttribute('aria-label') || element.innerText || '').trim().toLowerCase().replace(/\\s+/g, ' ');
                  const checkbox = element.matches('[role="checkbox"], input[type="checkbox"]');
                  if (!checkbox && !['я не робот', 'i am not a robot', "i'm not a robot", 'начать проверку'].includes(text)) continue;
                  if (clicked.has(element) || element.disabled || element.checked
                      || element.getAttribute('aria-checked') === 'true'
                      || element.getAttribute('aria-disabled') === 'true') continue;
                  const rect = element.getBoundingClientRect();
                  const style = getComputedStyle(element);
                  if (rect.width <= 0 || rect.height <= 0 || style.visibility === 'hidden'
                      || style.display === 'none' || Number(style.opacity) === 0) continue;
                  clicked.add(element); count++; element.click(); return;
                }
              };
              timer = setInterval(tick, 500);
              setTimeout(stop, 20000);
              window.addEventListener('pagehide', stop, { once: true });
              document.addEventListener('pointerdown', event => { if (event.isTrusted) stop(); }, { capture: true });
              tick();
            })();
            """;
    private VkCaptchaAutoClick() { }
}
