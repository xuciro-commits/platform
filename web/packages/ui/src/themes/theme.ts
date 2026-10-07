import { useSyncExternalStore } from "react";

/** Appearance = a colour scheme × a skin. The scheme answers "light or dark"; the skin answers
 * "which material": `default` is the flat SaaS look, `industrial` is the heavy-panel HMI look
 * (`themes/industrial.css`). Both are pure CSS on the document element — switching costs one
 * attribute write, nothing re-mounts. Appearance belongs to the shared UI token owner;
 * application definitions never carry CSS, storage keys or changes to the document element. */
export type Scheme = "system" | "light" | "dark";
export type Skin = "default" | "industrial";
export const skins: Skin[] = ["default", "industrial"];
export const schemes: Scheme[] = ["system", "light", "dark"];

const storageKey = "platform.appearance";
const listeners = new Set<() => void>();
const media = () => typeof matchMedia === "function" ? matchMedia("(prefers-color-scheme: dark)") : undefined;
const root = () => typeof document === "undefined" ? undefined : document.documentElement;

const read = (): { scheme: Scheme; skin: Skin } => {
  const element = root();
  const scheme = element?.dataset.theme === "dark" ? "dark" : element?.dataset.theme === "light" ? "light" : "system";
  const skin: Skin = element?.dataset.skin === "industrial" ? "industrial" : "default";
  return { scheme, skin };
};
const apply = (scheme: Scheme, skin: Skin) => {
  const element = root();
  if (!element) return;
  if (scheme === "system") delete element.dataset.theme; else element.dataset.theme = scheme;
  if (skin === "default") delete element.dataset.skin; else element.dataset.skin = skin;
  try { localStorage.setItem(storageKey, JSON.stringify({ scheme, skin })); } catch { /* private mode: the choice lives for the session */ }
  listeners.forEach((fn) => fn());
};
// Restore the reader's choice before the first paint of anything that reads it.
try {
  const stored = typeof localStorage === "undefined" ? null : localStorage.getItem(storageKey);
  if (stored) { const { scheme, skin } = JSON.parse(stored) as { scheme?: Scheme; skin?: Skin }; const element = root(); if (element) { if (scheme && scheme !== "system") element.dataset.theme = scheme; if (skin && skin !== "default") element.dataset.skin = skin; } }
} catch { /* ignore a corrupt preference */ }

let cached = read();
const snapshot = () => { const next = read(); if (next.scheme !== cached.scheme || next.skin !== cached.skin) cached = next; return cached; };
const subscribe = (fn: () => void) => { listeners.add(fn); const query = media(); query?.addEventListener("change", fn); return () => { listeners.delete(fn); query?.removeEventListener("change", fn); }; };
const resolved = (scheme: Scheme): "light" | "dark" => scheme === "system" ? (media()?.matches ? "dark" : "light") : scheme;

export function useTheme() {
  const { scheme, skin } = useSyncExternalStore(subscribe, snapshot, () => cached);
  const theme = useSyncExternalStore(subscribe, () => resolved(scheme), () => "light" as const);
  return {
    /** The resolved colour scheme, for libraries that need to be told (dockview). */
    theme, scheme, skin,
    setScheme: (next: Scheme) => apply(next, skin),
    setSkin: (next: Skin) => apply(scheme, next),
    /** Light ↔ dark, for a single header button. */
    toggle: () => apply(theme === "light" ? "dark" : "light", skin),
  };
}
