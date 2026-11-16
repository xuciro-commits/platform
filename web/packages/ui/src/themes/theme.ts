import { useSyncExternalStore } from "react";

export type Scheme = "system" | "light" | "dark";
export const themeNames = ["neutral", "stone", "matcha", "butter", "chocolate", "gothic", "y2k"] as const;
export type ThemeName = typeof themeNames[number];
export const themeLabels: Record<ThemeName, string> = {
  neutral: "Neutral",
  stone: "Stone",
  matcha: "Matcha",
  butter: "Butter",
  chocolate: "Chocolate",
  gothic: "Gothic",
  y2k: "Y2K",
};

const storageKey = "platform.appearance";
const listeners = new Set<() => void>();
const media = () => typeof matchMedia === "function" ? matchMedia("(prefers-color-scheme: dark)") : undefined;
const root = () => typeof document === "undefined" ? undefined : document.documentElement;
const resolved = (scheme: Scheme): "light" | "dark" => scheme === "system" ? (media()?.matches ? "dark" : "light") : scheme;
const isThemeName = (value: unknown): value is ThemeName => typeof value === "string" && (themeNames as readonly string[]).includes(value);

const stored = (): { scheme: Scheme; themeName: ThemeName } => {
  try {
    const value = typeof localStorage === "undefined" ? null : localStorage.getItem(storageKey);
    const parsed = value ? (JSON.parse(value) as { scheme?: Scheme; theme?: string }) : {};
    const scheme = parsed.scheme;
    return { scheme: scheme === "dark" || scheme === "system" ? scheme : "light", themeName: isThemeName(parsed.theme) ? parsed.theme : "neutral" };
  } catch { return { scheme: "light", themeName: "neutral" }; }
};

let state: { scheme: Scheme; theme: "light" | "dark"; themeName: ThemeName } = { ...stored(), theme: "light" };
const paint = () => {
  const theme = resolved(state.scheme);
  if (theme !== state.theme) state = { ...state, theme };
  const element = root();
  if (element) element.dataset.theme = theme;
};
paint();
media()?.addEventListener("change", () => { if (state.scheme === "system") { paint(); listeners.forEach(fn => fn()); } });

const apply = (next: Partial<Pick<typeof state, "scheme" | "themeName">>) => {
  const scheme = next.scheme ?? state.scheme;
  const themeName = next.themeName ?? state.themeName;
  state = { scheme, themeName, theme: resolved(scheme) };
  paint();
  try { localStorage.setItem(storageKey, JSON.stringify({ scheme, theme: themeName })); } catch {   }
  listeners.forEach((fn) => fn());
};
const subscribe = (fn: () => void) => { listeners.add(fn); return () => { listeners.delete(fn); }; };

export function useTheme() {
  const { scheme, theme, themeName } = useSyncExternalStore(subscribe, () => state, () => state);
  return {
    theme, scheme, themeName,
    setScheme: (next: Scheme) => apply({ scheme: next }),
    setThemeName: (next: ThemeName) => apply({ themeName: next }),
    toggle: () => apply({ scheme: theme === "light" ? "dark" : "light" }),
  };
}
