// The reader's own arrangement of a drawing (ADR-0092): every canvas can be
// dragged, and where the owner keeps no positions of their own the reader's
// arrangement stays on this device until they tidy it again. A scene opts in by
// naming a stable storeKey — the drawing's identity, not the run's — so the
// preference follows the picture it belongs to. The index bounds the store:
// readings drift, keys expire, storage never grows without limit.
import type { CanvasPosition } from "./types";

const LIMIT = 60;
const INDEX = "canvas:index";

/** The reader's stored arrangement for one drawing, if this device has one. */
export function readCanvasPositions(key: string): Record<string, CanvasPosition> | undefined {
  try {
    const raw = localStorage.getItem(`canvas:${key}`);
    const parsed = raw ? JSON.parse(raw) as unknown : undefined;
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) return undefined;
    return parsed as Record<string, CanvasPosition>;
  } catch { return undefined; } // storage unavailable: this page only
}

/** Keep the reader's arrangement for one drawing, expiring the oldest keys. */
export function writeCanvasPositions(key: string, positions: Record<string, CanvasPosition>): void {
  try {
    localStorage.setItem(`canvas:${key}`, JSON.stringify(positions));
    const raw = localStorage.getItem(INDEX);
    const index = (raw ? JSON.parse(raw) as unknown : []) as string[];
    const next = [key, ...(Array.isArray(index) ? index : []).filter((known) => known !== key)];
    for (const stale of next.slice(LIMIT)) localStorage.removeItem(`canvas:${stale}`);
    localStorage.setItem(INDEX, JSON.stringify(next.slice(0, LIMIT)));
  } catch { /* storage unavailable: the page keeps the arrangement in memory */ }
}
