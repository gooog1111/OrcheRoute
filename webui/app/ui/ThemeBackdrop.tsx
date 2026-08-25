"use client";

import { MatrixRain } from "./MatrixRain";
import type { ThemeID } from "./theme";

export function ThemeBackdrop({ theme }: { theme: ThemeID }) {
  if (theme === "matrix") return <MatrixRain />;
  if (theme === "hello-kitty") {
    return (
      <div className="theme-backdrop hello-kitty-backdrop" aria-hidden="true">
        <span>🎀</span><span>♡</span><span>✦</span><span>🎀</span><span>♡</span>
        <div className="kitty-face"><i /><b>ᴗ</b><i /></div>
      </div>
    );
  }
  if (theme === "liquid-glass") {
    return <div className="theme-backdrop liquid-backdrop" aria-hidden="true"><span /><span /><span /></div>;
  }
  if (theme === "windows-95") {
    return (
      <div className="theme-backdrop windows-backdrop" aria-hidden="true">
        <div className="retro-window"><strong>OrcheRoute.exe</strong><p>Welcome to the Internet!</p><span>✨ UNDER CONSTRUCTION ✨</span></div>
        <div className="retro-stars">✦　·　✧　·　✦</div>
      </div>
    );
  }
  return <div className={`theme-backdrop ${theme}-backdrop`} aria-hidden="true" />;
}
