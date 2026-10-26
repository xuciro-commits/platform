import { useSyncExternalStore } from "react";

/** Appearance = the colour scheme: "light or dark", pure CSS on the document element
 * (themes/tokens.css) — switching costs one attribute write, nothing re-mounts. Appearance
 * belongs to the shared UI token owner; application definitions never carry CSS, storage
 * keys or changes to the document element. */
export type Scheme = "system" | "light" | "dark";

const storageKey = "platform.appearance";
const listeners = new Set<() => void>();
const media = () => typeof matchMedia === "function" ? matchMedia("(prefers-color-scheme: dark)") : undefined;
const root = () => typeof document === "undefined" ? undefined : document.documentElement;

const read = (): { scheme: Scheme } => {
  const element = root();
  const scheme = element?.dataset.theme === "dark" ? "dark" : element?.dataset.theme === "light" ? "light" : "system";
  return { scheme };
};
const apply = (scheme: Scheme) => {
  const element = root();
  if (!element) return;
  if (scheme === "system") delete element.dataset.theme; else element.dataset.theme = scheme;
  try { localStorage.setItem(storageKey, JSON.stringify({ scheme })); } catch { /* private mode: the choice lives for the session */ }
  listeners.forEach((fn) => fn());
};
// Restore the reader's choice before the first paint of anything that reads it.
try {
  const stored = typeof localStorage === "undefined" ? null : localStorage.getItem(storageKey);
  if (stored) { const { scheme } = JSON.parse(stored) as { scheme?: Scheme }; const element = root(); if (element && scheme && scheme !== "system") element.dataset.theme = scheme; }
} catch { /* ignore a corrupt preference */ }

let cached = read();
const snapshot = () => { const next = read(); if (next.scheme !== cached.scheme) cached = next; return cached; };
const subscribe = (fn: () => void) => { listeners.add(fn); const query = media(); query?.addEventListener("change", fn); return () => { listeners.delete(fn); query?.removeEventListener("change", fn); }; };
const resolved = (scheme: Scheme): "light" | "dark" => scheme === "system" ? (media()?.matches ? "dark" : "light") : scheme;

export function useTheme() {
  const { scheme } = useSyncExternalStore(subscribe, snapshot, () => cached);
  const theme = useSyncExternalStore(subscribe, () => resolved(scheme), () => "light" as const);
  return {
    /** The resolved colour scheme, for libraries that need to be told (dockview). */
    theme, scheme,
    setScheme: (next: Scheme) => apply(next),
    /** Light ↔ dark, for a single header button. */
    toggle: () => apply(theme === "light" ? "dark" : "light"),
  };
}
