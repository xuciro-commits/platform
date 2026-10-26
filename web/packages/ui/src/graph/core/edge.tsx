import { EdgeLabelRenderer } from "@xyflow/react";
import type { ReactNode } from "react";

/** One colour rule for every line on every canvas: the owner's tone wins, then
 * being chosen, then the family's own default. */
export function canvasEdgeColor(tone: string | undefined, selected: boolean | undefined, fallback: string): string {
  return tone ? `var(--tone-${tone})` : selected ? "var(--primary)" : fallback;
}

/** A line's name, drawn over the drawing rather than into the SVG, so it stays
 * readable at any zoom and never rotates with the path. */
export function CanvasEdgeLabel({ x, y, offset = 0, children }: { x: number; y: number; offset?: number; children: ReactNode }) {
  return <EdgeLabelRenderer>
    <span className="platform-canvas-edge-label" style={{ transform: `translate(-50%, -50%) translate(${x}px, ${y + offset}px)` }}>{children}</span>
  </EdgeLabelRenderer>;
}
