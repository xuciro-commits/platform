// One React Flow frame for read-only operating graphs and editable semantic
// graphs. The graph adapter owns meaning; this frame owns viewport behavior.
import { Background, Controls, useNodesInitialized, useReactFlow, useStore } from "@xyflow/react";
import { Maximize2, Minimize2 } from "lucide-react";
import { useEffect, useState, type ReactNode } from "react";
import { t } from "../i18n";

export const fitting = { padding: 0.12, maxZoom: 1 };

export function CanvasFrame({ label, height, children, role = "figure" }: { label?: string; height: number; children: ReactNode; role?: "figure" | "region" }) {
  const [expanded, setExpanded] = useState(false);
  return <div className={expanded ? "fixed inset-4 z-50 rounded-md border border-border bg-background shadow-2xl" : "relative rounded-md border border-border bg-background"}
    style={expanded ? { overflow: "hidden" } : { height, minHeight: 200, resize: "vertical", overflow: "hidden" }}
    role={role} aria-label={label} title={label}>
    {children}
    <button type="button" className="absolute right-2 top-2 z-20 rounded border border-border bg-surface p-1 text-muted shadow-sm hover:bg-row-hover"
      aria-label={t(expanded ? "Restore canvas" : "Expand canvas")} title={t(expanded ? "Restore canvas" : "Expand canvas")}
      onClick={() => setExpanded(!expanded)}>{expanded ? <Minimize2 className="size-3.5" /> : <Maximize2 className="size-3.5" />}</button>
    {!expanded && <span aria-hidden="true" className="pointer-events-none absolute bottom-0.5 right-0.5 z-10 text-xs leading-none text-muted">◢</span>}
  </div>;
}

/** Call inside ReactFlow after nodes have been measured. Dragging a node does not refit. */
export function CanvasRefit({ signature }: { signature: string }) {
  const { fitView } = useReactFlow();
  const measured = useNodesInitialized();
  const width = useStore((s) => s.width), height = useStore((s) => s.height);
  useEffect(() => { if (measured && width > 0 && height > 0) void fitView(fitting); }, [fitView, measured, width, height, signature]);
  return null;
}

export function CanvasFurniture() {
  return <>
    <Background gap={16} size={1} color="var(--border)" />
    <Controls showInteractive={false} position="bottom-right" />
  </>;
}
