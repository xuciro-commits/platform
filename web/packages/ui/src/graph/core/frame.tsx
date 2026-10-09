// One React Flow frame for both canvas families. The family owns meaning; this
// frame owns the viewport: the border, the resize handle, expanding to the whole
// window, the background, the zoom controls and fitting the drawing to it.
import { Background, Controls, useNodesInitialized, useReactFlow, useStore } from "@xyflow/react";
import { Maximize2, Minimize2 } from "lucide-react";
import { useEffect, useRef, useState, type ReactNode } from "react";
import { t } from "../../i18n";

export const fitting = { padding: 0.12, maxZoom: 1 };

export function CanvasFrame({ label, height, children, role = "figure" }: { label?: string; height: number | string; children: ReactNode; role?: "figure" | "region" }) {
  const [expanded, setExpanded] = useState(false);
  useEffect(() => {
    if (!expanded) return;
    const close = (event: KeyboardEvent) => { if (event.key === "Escape") setExpanded(false); };
    window.addEventListener("keydown", close); return () => window.removeEventListener("keydown", close);
  }, [expanded]);
  return <div className={expanded ? "fixed inset-4 z-50 rounded-md border border-border bg-background shadow-2xl" : "relative rounded-md border border-border bg-background"}
    style={expanded ? { overflow: "hidden" } : { height, minHeight: 200, resize: "vertical", overflow: "hidden" }}
    role={role} aria-label={label}>
    {children}
    <button type="button" className="absolute right-2 top-2 z-20 rounded border border-border bg-surface p-1 text-muted shadow-sm hover:bg-row-hover"
      aria-label={t(expanded ? "Restore canvas" : "Expand canvas")} title={t(expanded ? "Restore canvas" : "Expand canvas")}
      onClick={() => setExpanded(!expanded)}>{expanded ? <Minimize2 className="size-3.5" /> : <Maximize2 className="size-3.5" />}</button>
    {!expanded && <span aria-hidden="true" className="pointer-events-none absolute bottom-0.5 right-0.5 z-10 text-xs leading-none text-muted">◢</span>}
  </div>;
}

export function CanvasFurniture() {
  return <>
    <Background gap={16} size={1} color="var(--border)" />
    <Controls showInteractive={false} position="bottom-right" />
  </>;
}

/** Fit the drawing once its boxes are measurable, and again when `signature` says
 * the drawing changed. Call inside ReactFlow. Dragging a node does not refit.
 *
 * `once` is the relation family's rule: a view is fitted when it first arrives and
 * not again while the reader works on it. A view change renders once with the
 * previous nodes, so the fit waits until the new positions and measurements have
 * reached the canvas, and a quick change of view cancels the fit still pending. */
export function CanvasRefit({ signature, ready = true, once = false }: { signature: string; ready?: boolean; once?: boolean }) {
  const { fitView } = useReactFlow();
  const measured = useNodesInitialized();
  const width = useStore((s) => s.width), height = useStore((s) => s.height);
  const fitted = useRef<Set<string>>(new Set());
  const previous = useRef<{ signature: string; width: number; height: number } | undefined>(undefined);
  const usable = ready && measured && width > 0 && height > 0;
  useEffect(() => {
    if (!usable) return;
    if (once) {
      if (fitted.current.has(signature)) return;
      let cancelled = false;
      const timer = setTimeout(() => { void fitView(fitting).then((done) => { if (done && !cancelled) fitted.current.add(signature); }); }, 60);
      return () => { cancelled = true; clearTimeout(timer); };
    }
    const last = previous.current;
    if (last?.signature === signature && last.width === width && last.height === height) return;
    previous.current = { signature, width, height }; void fitView(fitting);
  }, [usable, once, signature, width, height, fitView]);
  return null;
}
