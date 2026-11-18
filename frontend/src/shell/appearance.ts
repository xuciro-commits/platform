import { useSyncExternalStore } from "react";

export type Appearance = "system" | "light" | "dark";

const listeners = new Set<() => void>();
let current: Appearance = "system";

export function setAppearance(theme: string | undefined) {
  const next: Appearance = theme === "light" || theme === "dark" ? theme : "system";
  if (next === current) return;
  current = next;
  listeners.forEach((notify) => notify());
}

export function useAppearance() {
  return useSyncExternalStore((notify) => {
    listeners.add(notify);
    return () => listeners.delete(notify);
  }, () => current);
}
