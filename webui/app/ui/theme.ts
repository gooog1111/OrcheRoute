export const themeIDs = ["matrix", "hello-kitty", "liquid-glass", "windows-95", "rick-morty", "dark", "light"] as const;

export type ThemeID = (typeof themeIDs)[number];

export const themes: { id: ThemeID; name: string; description: string; glyph: string }[] = [
  { id: "matrix", name: "Матрица", description: "Цифровой дождь и неоновый терминал", glyph: "01" },
  { id: "hello-kitty", name: "Hello Kitty", description: "Розовая, мягкая и немного кавайная", glyph: "🎀" },
  { id: "liquid-glass", name: "Liquid Glass", description: "Прозрачные панели и живые блики", glyph: "◉" },
  { id: "windows-95", name: "Windows 95", description: "Серые окна, пиксели и дух старого Web", glyph: "95" },
  { id: "rick-morty", name: "Rick and Morty", description: "Порталы, лаборатория и межпространственный неон", glyph: "R&M" },
  { id: "dark", name: "Тёмная", description: "Спокойная тема без фоновой анимации", glyph: "◐" },
  { id: "light", name: "Светлая", description: "Чистый светлый интерфейс", glyph: "☀" },
];

export const defaultTheme: ThemeID = "matrix";
export const themeStorageKey = "orcheroute.ui.theme";

export function isThemeID(value: string | null | undefined): value is ThemeID {
  return themeIDs.includes(value as ThemeID);
}
