// The furniture every canvas shows around its drawing: the floating toolbar, the
// help card, the bar of operations on the selected element, the empty state. Both
// families drew their own copy of these; there is now one (ADR-0086 §D2).
import { CircleHelp } from "lucide-react";
import type { ReactNode } from "react";
import { t } from "../../i18n";
import { cn } from "../../lib/cn";
import type { CanvasAction } from "./types";

export function CanvasToolbar({ label, children }: { label: string; children: ReactNode }) {
  return <div className="nodrag nopan platform-canvas-toolbar" role="toolbar" aria-label={label}>{children}</div>;
}

export function CanvasTool({ label, title, icon, text, onClick, disabled, primary, pressed, expanded, hasPopup }: {
  label: string; title?: string; icon: ReactNode; text?: string; onClick: () => void;
  disabled?: boolean; primary?: boolean; pressed?: boolean; expanded?: boolean; hasPopup?: "menu" | true;
}) {
  return <button type="button" className={cn("platform-canvas-tool", primary && "platform-canvas-tool-primary", pressed && "bg-row-selected")}
    disabled={disabled} onClick={onClick} title={title ?? label} aria-label={label}
    aria-pressed={pressed === undefined ? undefined : pressed} aria-expanded={expanded} aria-haspopup={hasPopup}>{icon}{text && <span>{text}</span>}</button>;
}

export function CanvasToolDivider() {
  return <span className="mx-0.5 h-4 w-px bg-border" />;
}

/** The `?` card: what a person can do here, one line per affordance the owner enabled. */
export function CanvasHelp({ open, onClose, items }: { open: boolean; onClose: () => void; items: ReactNode[] }) {
  if (!open) return null;
  return <div role="dialog" aria-label={t("How to work on this canvas")} className="absolute right-3 top-14 z-20 w-72 rounded-md border border-border bg-surface p-3 text-xs shadow-lg">
    <p className="mb-1 font-semibold">{t("On the canvas")}</p>
    <ul className="grid gap-1 text-muted">{items.filter(Boolean).map((item, i) => <li key={i}>{item}</li>)}</ul>
    <button type="button" className="mt-2 rounded px-2 py-1 text-muted hover:bg-row-hover" onClick={onClose}>{t("Close")}</button>
  </div>;
}

export function CanvasHelpTool({ open, onToggle }: { open: boolean; onToggle: () => void }) {
  return <CanvasTool label={t("How to work on this canvas")} icon={<CircleHelp />} onClick={onToggle} expanded={open} />;
}

/** The operations of the selected element or connection, along the canvas foot.
 * Narrow panels scroll it sideways instead of stacking the words (ADR-0085 D5). */
export function CanvasActionBar({ label, children }: { label: string; children: ReactNode }) {
  return <div className="nodrag nopan platform-canvas-actions" role="toolbar" aria-label={label}>{children}</div>;
}

export function CanvasActionButton({ action }: { action: CanvasAction }) {
  return <button type="button" disabled={action.disabled} title={action.hint} onClick={action.run}
    className={cn("platform-canvas-tool", action.tone === "danger" && "text-[var(--tone-danger)]")}>{action.icon}{action.label && <span>{action.label}</span>}</button>;
}

export function CanvasEmpty({ children }: { children?: ReactNode }) {
  return <div className="pointer-events-none absolute inset-0 flex flex-col items-center justify-center gap-2 p-6 text-center text-muted">
    {children ?? <span className="text-sm">{t("Nothing shown yet.")}</span>}
  </div>;
}
