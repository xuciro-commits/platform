import { useSyncExternalStore } from "react";

export type Scheme = "system" | "light" | "dark";

const storageKey = "platform.appearance";
const listeners = new Set<() => void>();
const media = () => typeof matchMedia === "function" ? matchMedia("(prefers-color-scheme: dark)") : undefined;
const root = () => typeof document === "undefined" ? undefined : document.documentElement;
const resolved = (scheme: Scheme): "light" | "dark" => scheme === "system" ? (media()?.matches ? "dark" : "light") : scheme;

const stored = (): Scheme => {
  try {
    const value = typeof localStorage === "undefined" ? null : localStorage.getItem(storageKey);
    const scheme = value ? (JSON.parse(value) as { scheme?: Scheme }).scheme : undefined;
    return scheme === "dark" || scheme === "system" ? scheme : "light";
  } catch { return "light"; }
};

let state: { scheme: Scheme; theme: "light" | "dark" } = { scheme: stored(), theme: "light" };
const paint = () => {
  const theme = resolved(state.scheme);
  if (theme !== state.theme) state = { ...state, theme };
  const element = root();
  if (element) element.dataset.theme = theme;
};
paint();
media()?.addEventListener("change", () => { if (state.scheme === "system") { paint(); listeners.forEach(fn => fn()); } });

const apply = (scheme: Scheme) => {
  state = { scheme, theme: resolved(scheme) };
  paint();
  try { localStorage.setItem(storageKey, JSON.stringify({ scheme })); } catch {   }
  listeners.forEach((fn) => fn());
};
const subscribe = (fn: () => void) => { listeners.add(fn); return () => { listeners.delete(fn); }; };

export function useTheme() {
  const { scheme, theme } = useSyncExternalStore(subscribe, () => state, () => state);
  return {
    theme, scheme,
    setScheme: (next: Scheme) => apply(next),
    toggle: () => apply(theme === "light" ? "dark" : "light"),
  };
}
