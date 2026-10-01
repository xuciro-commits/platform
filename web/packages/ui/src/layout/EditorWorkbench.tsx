import { useState, type CSSProperties, type ReactNode } from "react";
import { t } from "../i18n";
import { cn } from "../lib/cn";

/** Shared editor pane mechanics. Document commands and selection are owned by
 * the caller; resizing never writes presentation props into the document.
 */
export function EditorWorkbench({ left, children, right, leftLabel, centerLabel, rightLabel, className }: {
  left?: ReactNode; children: ReactNode; right?: ReactNode;
  leftLabel: string; centerLabel: string; rightLabel: string; className?: string;
}) {
  const [widths, setWidths] = useState({ left: 248, right: 304 });
  const [drag, setDrag] = useState<{ side: "left" | "right"; x: number; width: number }>();
  const resize = (side: "left" | "right", width: number) => setWidths((old) => ({ ...old, [side]: Math.max(208, Math.min(420, width)) }));
  const splitter = (side: "left" | "right") => <div role="separator" tabIndex={0} aria-orientation="vertical"
    aria-label={t(side === "left" ? "Resize library panel" : "Resize inspector panel")} aria-valuemin={208} aria-valuemax={420} aria-valuenow={widths[side]}
    className="hidden w-1.5 cursor-col-resize touch-none rounded hover:bg-primary/20 focus-visible:bg-primary/20 focus-visible:outline-ring lg:block"
    onPointerDown={(event) => { if (event.button !== 0) return; event.preventDefault(); event.currentTarget.setPointerCapture(event.pointerId); setDrag({ side, x: event.clientX, width: widths[side] }); }}
    onPointerMove={(event) => { if (drag?.side === side) resize(side, drag.width + (event.clientX - drag.x) * (side === "left" ? 1 : -1)); }}
    onPointerUp={(event) => { if (event.currentTarget.hasPointerCapture(event.pointerId)) event.currentTarget.releasePointerCapture(event.pointerId); setDrag(undefined); }}
    onPointerCancel={() => setDrag(undefined)} onLostPointerCapture={() => setDrag(undefined)}
    onKeyDown={(event) => { if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") return; event.preventDefault(); resize(side, widths[side] + (event.key === "ArrowRight" ? 16 : -16) * (side === "left" ? 1 : -1)); }} />;
  const columns = `${left ? `${widths.left}px 6px ` : ""}minmax(0,1fr)${right ? ` 6px ${widths.right}px` : ""}`;
  return <div className={cn("grid gap-y-2 lg:min-h-0 lg:flex-1 lg:gap-y-0 lg:grid-cols-[var(--editor-columns)]", className)}
    style={{ "--editor-columns": columns } as CSSProperties}>
    {left && <><aside role="region" aria-label={leftLabel} className="min-w-0 overflow-auto rounded-md border border-border bg-surface lg:min-h-0">{left}</aside>{splitter("left")}</>}
    <div role="region" aria-label={centerLabel} className="flex min-w-0 flex-col overflow-hidden rounded-md border border-border bg-surface lg:min-h-0">{children}</div>
    {right && <>{splitter("right")}<aside role="region" aria-label={rightLabel} className="min-w-0 overflow-auto rounded-md border border-border bg-surface lg:min-h-0">{right}</aside></>}
  </div>;
}
