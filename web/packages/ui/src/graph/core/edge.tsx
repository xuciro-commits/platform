import { EdgeLabelRenderer } from "@xyflow/react";
import type { ReactNode } from "react";

export function canvasEdgeColor(tone: string | undefined, selected: boolean | undefined, fallback: string): string {
  return tone ? `var(--tone-${tone})` : selected ? "var(--primary)" : fallback;
}

export function CanvasEdgeLabel({ x, y, offset = 0, children }: { x: number; y: number; offset?: number; children: ReactNode }) {
  return <EdgeLabelRenderer>
    <span className="platform-canvas-edge-label" style={{ transform: `translate(-50%, -50%) translate(${x}px, ${y + offset}px)` }}>{children}</span>
  </EdgeLabelRenderer>;
}
