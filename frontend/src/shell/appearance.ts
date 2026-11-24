import { useEffect, useState, useSyncExternalStore } from "react";

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

export function useIsDark() {
  const mode = useAppearance();
  const [systemDark, setSystemDark] = useState(() =>
    typeof window !== "undefined" && window.matchMedia("(prefers-color-scheme: dark)").matches,
  );
  useEffect(() => {
    if (typeof window === "undefined") return;
    const media = window.matchMedia("(prefers-color-scheme: dark)");
    const update = () => setSystemDark(media.matches);
    media.addEventListener("change", update);
    return () => media.removeEventListener("change", update);
  }, []);
  return mode === "dark" || (mode === "system" && systemDark);
}
