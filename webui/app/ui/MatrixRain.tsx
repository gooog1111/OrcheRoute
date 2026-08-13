"use client";

import { useEffect, useRef } from "react";

const GLYPHS = "01アイウエオカキクケコサシスセソタチツテトナニヌネノハヒフヘホルート";

export function MatrixRain() {
  const canvasRef = useRef<HTMLCanvasElement>(null);

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const context = canvas.getContext("2d", { alpha: true });
    if (!context) return;

    const reducedMotion = window.matchMedia("(prefers-reduced-motion: reduce)");
    let frame = 0;
    let lastFrame = 0;
    let columns = 0;
    let drops: number[] = [];
    const fontSize = window.innerWidth < 640 ? 14 : 17;

    const resize = () => {
      const ratio = Math.min(window.devicePixelRatio || 1, 1.5);
      canvas.width = Math.floor(window.innerWidth * ratio);
      canvas.height = Math.floor(window.innerHeight * ratio);
      canvas.style.width = `${window.innerWidth}px`;
      canvas.style.height = `${window.innerHeight}px`;
      context.setTransform(ratio, 0, 0, ratio, 0, 0);
      columns = Math.ceil(window.innerWidth / fontSize);
      drops = Array.from({ length: columns }, (_, index) => drops[index] ?? Math.random() * (window.innerHeight / fontSize));
    };

    const paintBackdrop = () => {
      context.clearRect(0, 0, window.innerWidth, window.innerHeight);
      context.font = `500 ${fontSize}px "Cascadia Code", Consolas, monospace`;
      context.textAlign = "center";
      for (let index = 0; index < columns; index += 1) {
        for (let row = index % 7; row < window.innerHeight / fontSize; row += 7 + (index % 4)) {
          const glyph = GLYPHS[(index * 11 + row * 3) % GLYPHS.length];
          context.fillStyle = `rgba(24, 224, 158, ${0.1 + ((index + row) % 4) * 0.025})`;
          context.fillText(glyph, index * fontSize + fontSize / 2, row * fontSize);
        }
      }
    };

    const draw = (timestamp: number) => {
      frame = window.requestAnimationFrame(draw);
      if (document.hidden || timestamp - lastFrame < (reducedMotion.matches ? 180 : 54)) return;
      lastFrame = timestamp;
      context.fillStyle = "rgba(2, 8, 7, 0.115)";
      context.fillRect(0, 0, window.innerWidth, window.innerHeight);
      context.font = `500 ${fontSize}px "Cascadia Code", Consolas, monospace`;
      context.textAlign = "center";

      for (let index = 0; index < columns; index += 1) {
        const glyph = GLYPHS[Math.floor(Math.random() * GLYPHS.length)];
        const y = drops[index] * fontSize;
        context.fillStyle = Math.random() > 0.965 ? "rgba(178, 255, 232, .82)" : "rgba(24, 224, 158, .46)";
        context.fillText(glyph, index * fontSize + fontSize / 2, y);
        if (y > window.innerHeight + fontSize && Math.random() > 0.972) drops[index] = -Math.random() * 24;
        drops[index] += 0.72 + (index % 5) * 0.055;
      }
    };

    const start = () => {
      window.cancelAnimationFrame(frame);
      paintBackdrop();
      frame = window.requestAnimationFrame(draw);
    };
    const handleResize = () => {
      resize();
      start();
    };
    const visibility = () => {
      if (!document.hidden) start();
    };
    resize();
    start();
    window.addEventListener("resize", handleResize, { passive: true });
    document.addEventListener("visibilitychange", visibility);
    reducedMotion.addEventListener("change", start);
    return () => {
      window.cancelAnimationFrame(frame);
      window.removeEventListener("resize", handleResize);
      document.removeEventListener("visibilitychange", visibility);
      reducedMotion.removeEventListener("change", start);
    };
  }, []);

  return <canvas ref={canvasRef} className="matrix-rain" aria-hidden="true" />;
}
