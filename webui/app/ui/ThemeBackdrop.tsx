"use client";

import { useEffect, useState } from "react";
import { MatrixRain } from "./MatrixRain";
import type { ThemeID } from "./theme";

export function ThemeBackdrop({ theme }: { theme: ThemeID }) {
  const [visibleEpoch, setVisibleEpoch] = useState(0);
  const [visible, setVisible] = useState(() => typeof document === "undefined" || !document.hidden);

  useEffect(() => {
    const handleVisibility = () => {
      if (document.hidden) {
        setVisible(false);
        return;
      }
      setVisibleEpoch((value) => value + 1);
      setVisible(true);
    };
    document.addEventListener("visibilitychange", handleVisibility);
    return () => document.removeEventListener("visibilitychange", handleVisibility);
  }, []);

  // Recreating the decoration after a suspended mobile tab prevents browsers
  // from trying to catch up old CSS animation frames at an excessive speed.
  if (!visible) return <div className="theme-backdrop theme-backdrop-paused" aria-hidden="true" />;
  if (theme === "matrix") return <MatrixRain key={visibleEpoch} />;
  if (theme === "hello-kitty") {
    return (
      <div key={visibleEpoch} className="theme-backdrop hello-kitty-backdrop" aria-hidden="true">
        <span>🎀</span><span>♡</span><span>✦</span><span>🎀</span><span>♡</span>
        <i className="hk-logo" />
        <i className="hk-character hk-flowers" />
        <i className="hk-character hk-standing" />
        <i className="hk-character hk-sitting" />
      </div>
    );
  }
  if (theme === "liquid-glass") {
    return <div key={visibleEpoch} className="theme-backdrop liquid-backdrop" aria-hidden="true"><span /><span /><span /></div>;
  }
  if (theme === "windows-95") {
    return (
      <div key={visibleEpoch} className="theme-backdrop windows-backdrop" aria-hidden="true">
        <div className="retro-window"><strong>OrcheRoute.exe</strong><p>Welcome to the Internet!</p><span>✨ UNDER CONSTRUCTION ✨</span></div>
        <div className="retro-marquee"><span>★ ORCHEROUTE ONLINE ★ WELCOME TO THE WEB ★</span></div>
        <div className="retro-icons"><b>🌐</b><b>💾</b><b>📁</b></div>
        <div className="retro-stars">✦　·　✧　·　✦</div>
      </div>
    );
  }
  if (theme === "rick-morty") {
    return (
      <div key={visibleEpoch} className="theme-backdrop rick-morty-backdrop" aria-hidden="true">
        <span className="rm-ooze"><i/><i/><i/><i/><i/></span>
        <span className="portal portal-one"><i /><i /><i /></span>
        <span className="portal portal-two"><i /><i /><i /></span>
        <span className="dimension-orbit"><i /><i /><i /></span>
        <span className="rm-saucer"><i className="rm-saucer-beam"/><i className="rm-saucer-dome"/><i className="rm-saucer-body"/></span>
        <span className="science-symbols">C-137　⚛　≋　∑　 portal online</span>
      </div>
    );
  }
  return <div key={visibleEpoch} className={`theme-backdrop ${theme}-backdrop`} aria-hidden="true" />;
}
