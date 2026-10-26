import type { CanvasPosition } from "../types";
import type { LayoutRoute } from "./types";
/** Render ELK's own polyline, rather than asking React Flow to invent a different route. */
export function routedPath(route: LayoutRoute): [string, number, number] {
  const points = route.points;
  const path = points.map((p, i) => `${i ? "L" : "M"}${p.x},${p.y}`).join(" ");
  const lengths = points.slice(1).map((p, i) => Math.hypot(p.x - points[i]!.x, p.y - points[i]!.y));
  let middle = lengths.reduce((a, b) => a + b, 0) / 2;
  let label = route.label ?? points[0] ?? { x: 0, y: 0 };
  if (!route.label) for (const [i, length] of lengths.entries()) {
    if (middle <= length) { const a = points[i]!, b = points[i + 1]!, t = length ? middle / length : 0; label = { x: a.x + (b.x - a.x) * t, y: a.y + (b.y - a.y) * t }; break; }
    middle -= length;
  }
  return [path, label.x, label.y];
}
/** Any manual move invalidates routes through that drawing, including lines passing an unrelated moved obstacle. */
export function samePositions(expected: Record<string, CanvasPosition>, actual: { id: string; position: CanvasPosition }[]): boolean {
  return actual.length === Object.keys(expected).length && actual.every((node) => expected[node.id]?.x === node.position.x && expected[node.id]?.y === node.position.y);
}
