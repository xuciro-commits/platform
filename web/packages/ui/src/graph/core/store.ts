import type { CanvasPosition } from "./types";

const LIMIT = 60;
const INDEX = "canvas:index";

export function readCanvasPositions(key: string): Record<string, CanvasPosition> | undefined {
  try {
    const raw = localStorage.getItem(`canvas:${key}`);
    const parsed = raw ? JSON.parse(raw) as unknown : undefined;
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) return undefined;
    return parsed as Record<string, CanvasPosition>;
  } catch { return undefined; }
}

export function writeCanvasPositions(key: string, positions: Record<string, CanvasPosition>): void {
  try {
    localStorage.setItem(`canvas:${key}`, JSON.stringify(positions));
    const raw = localStorage.getItem(INDEX);
    const index = (raw ? JSON.parse(raw) as unknown : []) as string[];
    const next = [key, ...(Array.isArray(index) ? index : []).filter((known) => known !== key)];
    for (const stale of next.slice(LIMIT)) localStorage.removeItem(`canvas:${stale}`);
    localStorage.setItem(INDEX, JSON.stringify(next.slice(0, LIMIT)));
  } catch {   }
}
