import { useEffect, useId, useRef, useState, type CSSProperties, type ReactNode } from "react";
import { ChevronRight, PanelBottomClose, PanelBottomOpen, PanelLeftClose, PanelLeftOpen, PanelRightClose, PanelRightOpen, Redo2, Undo2 } from "lucide-react";
import { t } from "../i18n";
import { cn } from "../lib/cn";
import { Button } from "../primitives/button";
import { useViewTitle } from "../shell/Workspace";

/** The one editor container (ADR-0053 §4): title bar with breadcrumbs,
 * status, history and publish actions; a structure panel on the left; the
 * main view; an inspector on the right; a dock of problems/variables/runs
 * underneath. Panel sizes and visibility are remembered per `storageKey`.
 * Document state, commands and selection belong to the caller.
 */
export type WorkbenchCrumb = { label: ReactNode; onClick?: () => void };
export type WorkbenchTab = { id: string; title: ReactNode; badge?: number | string; content: ReactNode };
export type WorkbenchPanel = { label: string; tabs?: WorkbenchTab[]; value?: string; onChange?: (id: string) => void; content?: ReactNode; min?: number; max?: number };
export type WorkbenchHistory = { canUndo: boolean; canRedo: boolean; undo: () => void; redo: () => void };
export type WorkbenchSaving = "idle" | "dirty" | "saving" | "saved" | "error";

type Layout = { left: number; right: number; dock: number; leftOpen: boolean; rightOpen: boolean; dockOpen: boolean; dockTab?: string };
const defaults: Layout = { left: 264, right: 320, dock: 200, leftOpen: true, rightOpen: true, dockOpen: false };
const read = (key: string): Layout => { try { const raw = localStorage.getItem(`workbench:${key}`); return raw ? { ...defaults, ...JSON.parse(raw) } : defaults; } catch { return defaults; } };

export function Workbench({ storageKey, crumbs = [], title, status, saving, history, actions, left, right, dock, children, className, onKeyDown, mainLabel }: {
  storageKey: string; crumbs?: WorkbenchCrumb[]; title?: ReactNode; status?: ReactNode; saving?: WorkbenchSaving; history?: WorkbenchHistory; actions?: ReactNode;
  left?: WorkbenchPanel; right?: WorkbenchPanel; dock?: WorkbenchPanel; children: ReactNode; className?: string; mainLabel?: string;
  onKeyDown?: (event: React.KeyboardEvent<HTMLDivElement>) => void;
}) {
  useViewTitle(typeof title === "string" ? title : undefined);
  const [layout, setLayout] = useState<Layout>(() => read(storageKey));
  const previousDock = useRef(dock?.value);
  useEffect(() => {
    if (previousDock.current === dock?.value) return;
    previousDock.current = dock?.value;
    if (dock?.value) setLayout((old) => ({ ...old, dockOpen: true, dockTab: dock.value }));
  }, [dock?.value]);
  const [drag, setDrag] = useState<{ side: "left" | "right" | "dock"; at: number; size: number }>();
  useEffect(() => { try { localStorage.setItem(`workbench:${storageKey}`, JSON.stringify(layout)); } catch { /* private mode */ } }, [layout, storageKey]);
  const patch = (next: Partial<Layout>) => setLayout((old) => ({ ...old, ...next }));
  const clamp = (side: "left" | "right" | "dock", size: number) => {
    const panel = side === "left" ? left : side === "right" ? right : dock;
    return Math.max(panel?.min ?? (side === "dock" ? 120 : 200), Math.min(panel?.max ?? (side === "dock" ? 520 : 560), size));
  };
  const splitter = (side: "left" | "right" | "dock") => <div role="separator" tabIndex={0} aria-orientation={side === "dock" ? "horizontal" : "vertical"}
    aria-label={t(side === "left" ? "Resize structure panel" : side === "right" ? "Resize inspector panel" : "Resize dock")}
    className={cn("touch-none rounded hover:bg-primary/20 focus-visible:bg-primary/20 focus-visible:outline-ring", side === "dock" ? "h-1.5 w-full cursor-row-resize" : "w-1.5 cursor-col-resize")}
    onPointerDown={(event) => { if (event.button !== 0) return; event.preventDefault(); event.currentTarget.setPointerCapture(event.pointerId); setDrag({ side, at: side === "dock" ? event.clientY : event.clientX, size: layout[side] }); }}
    onPointerMove={(event) => { if (drag?.side !== side) return; const delta = side === "dock" ? drag.at - event.clientY : (event.clientX - drag.at) * (side === "left" ? 1 : -1); patch({ [side]: clamp(side, drag.size + delta) } as Partial<Layout>); }}
    onPointerUp={(event) => { if (event.currentTarget.hasPointerCapture(event.pointerId)) event.currentTarget.releasePointerCapture(event.pointerId); setDrag(undefined); }}
    onPointerCancel={() => setDrag(undefined)} onLostPointerCapture={() => setDrag(undefined)}
    onKeyDown={(event) => { const keys = side === "dock" ? ["ArrowUp", "ArrowDown"] : ["ArrowLeft", "ArrowRight"]; if (!keys.includes(event.key)) return; event.preventDefault();
      const grow = event.key === "ArrowRight" && side === "left" || event.key === "ArrowLeft" && side === "right" || event.key === "ArrowUp"; patch({ [side]: clamp(side, layout[side] + (grow ? 16 : -16)) } as Partial<Layout>); }} />;
  const showLeft = !!left && layout.leftOpen, showRight = !!right && layout.rightOpen, showDock = !!dock && layout.dockOpen;
  const columns = `${showLeft ? `${layout.left}px 6px ` : ""}minmax(0,1fr)${showRight ? ` 6px ${layout.right}px` : ""}`;
  const savingLabel = saving === "saving" ? t("Saving…") : saving === "saved" ? t("Saved") : saving === "dirty" ? t("Unsaved changes") : saving === "error" ? t("Not saved") : "";
  return <div className={cn("flex min-h-0 flex-1 flex-col overflow-hidden rounded-md border border-border bg-surface lg:h-[calc(100dvh-6.5rem)]", className)} tabIndex={-1}
    onKeyDown={(event) => {
      const command = event.metaKey || event.ctrlKey;
      if (command && event.key === "\\") { event.preventDefault(); if (event.altKey) patch({ rightOpen: !layout.rightOpen }); else patch({ leftOpen: !layout.leftOpen }); return; }
      if (command && event.key.toLowerCase() === "j" && dock) { event.preventDefault(); patch({ dockOpen: !layout.dockOpen }); return; }
      onKeyDown?.(event);
    }}>
    <header className="flex min-h-10 flex-wrap items-center gap-1 border-b border-border px-2 py-1">
      {left && <Button variant="ghost" size="sm" aria-label={t("Toggle structure panel")} aria-pressed={layout.leftOpen} title="⌘\\" onClick={() => patch({ leftOpen: !layout.leftOpen })}>{layout.leftOpen ? <PanelLeftClose /> : <PanelLeftOpen />}</Button>}
      <nav aria-label={t("Breadcrumbs")} className="flex min-w-0 items-center gap-1 text-sm">
        {crumbs.map((crumb, index) => <span key={index} className="flex min-w-0 items-center gap-1">
          {index > 0 && <ChevronRight className="size-3 shrink-0 text-muted" />}
          {crumb.onClick ? <button type="button" className="truncate rounded px-1 text-muted hover:bg-row-hover hover:text-foreground" onClick={crumb.onClick}>{crumb.label}</button> : <span className="truncate px-1 text-muted">{crumb.label}</span>}
        </span>)}
        {title && <span className="flex min-w-0 items-center gap-1">{crumbs.length > 0 && <ChevronRight className="size-3 shrink-0 text-muted" />}<h1 className="truncate px-1 font-semibold">{title}</h1></span>}
      </nav>
      <div className="flex items-center gap-1">{status}</div>
      {history && <div className="ml-2 flex items-center">
        <Button variant="ghost" size="sm" aria-label={t("Undo")} title={t("Undo") + " ⌘Z"} disabled={!history.canUndo} onClick={history.undo}><Undo2 /></Button>
        <Button variant="ghost" size="sm" aria-label={t("Redo")} title={t("Redo") + " ⇧⌘Z"} disabled={!history.canRedo} onClick={history.redo}><Redo2 /></Button>
      </div>}
      {savingLabel && <span role="status" className={cn("ml-2 text-xs", saving === "error" ? "text-danger" : "text-muted")}>{savingLabel}</span>}
      <div className="ml-auto flex flex-wrap items-center gap-1">{actions}</div>
      {dock && <Button variant="ghost" size="sm" aria-label={t("Toggle dock")} aria-pressed={layout.dockOpen} title="⌘J" onClick={() => patch({ dockOpen: !layout.dockOpen })}>{layout.dockOpen ? <PanelBottomClose /> : <PanelBottomOpen />}</Button>}
      {right && <Button variant="ghost" size="sm" aria-label={t("Toggle inspector panel")} aria-pressed={layout.rightOpen} title="⌥⌘\\" onClick={() => patch({ rightOpen: !layout.rightOpen })}>{layout.rightOpen ? <PanelRightClose /> : <PanelRightOpen />}</Button>}
    </header>
    <div className="grid min-h-0 flex-1 lg:grid-cols-[var(--workbench-columns)]" style={{ "--workbench-columns": columns } as CSSProperties}>
      {showLeft && <><aside role="region" aria-label={left.label} className="flex min-h-0 min-w-0 flex-col overflow-hidden border-r border-border"><PanelBody panel={left} /></aside>{splitter("left")}</>}
      <div role="region" aria-label={mainLabel ?? t("Editor")} className="flex min-h-0 min-w-0 flex-col overflow-hidden">
        <div className="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden">{children}</div>
        {dock && <DockBar dock={dock} open={layout.dockOpen} height={layout.dock} tab={dock.value ?? layout.dockTab} splitter={showDock ? splitter("dock") : null}
          onOpen={(id) => { dock.onChange?.(id); patch({ dockOpen: !(layout.dockOpen && (dock.value ?? layout.dockTab) === id), dockTab: id }); }} />}
      </div>
      {showRight && <>{splitter("right")}<aside role="region" aria-label={right.label} className="flex min-h-0 min-w-0 flex-col overflow-hidden border-l border-border"><PanelBody panel={right} /></aside></>}
    </div>
  </div>;
}

function PanelBody({ panel }: { panel: WorkbenchPanel }) {
  const prefix = useId();
  const [own, setOwn] = useState(panel.tabs?.[0]?.id);
  const active = panel.value ?? own;
  const choose = (id: string) => { setOwn(id); panel.onChange?.(id); };
  if (!panel.tabs?.length) return <div className="min-h-0 flex-1 overflow-auto">{panel.content}</div>;
  const current = panel.tabs.find((tab) => tab.id === active) ?? panel.tabs[0]!;
  return <>
    <div role="tablist" aria-label={panel.label} className="flex shrink-0 gap-0.5 overflow-x-auto border-b border-border px-1 pt-1">
      {panel.tabs.map((tab) => <button key={tab.id} type="button" role="tab" id={`${prefix}-${tab.id}`} aria-selected={tab.id === current.id}
        className={cn("flex shrink-0 items-center gap-1 whitespace-nowrap rounded-t border-b-2 px-2 py-1 text-xs", tab.id === current.id ? "border-primary font-semibold" : "border-transparent text-muted hover:text-foreground")}
        onClick={() => choose(tab.id)}>{tab.title}{tab.badge !== undefined && tab.badge !== 0 && <span className="rounded-full bg-row-selected px-1.5 text-[10px]">{tab.badge}</span>}</button>)}
    </div>
    <div role="tabpanel" aria-labelledby={`${prefix}-${current.id}`} className="min-h-0 flex-1 overflow-auto">{current.content}</div>
  </>;
}

function DockBar({ dock, open, height, tab, splitter, onOpen }: { dock: WorkbenchPanel; open: boolean; height: number; tab?: string; splitter: ReactNode; onOpen: (id: string) => void }) {
  const tabs = dock.tabs ?? [];
  const current = tabs.find((item) => item.id === tab) ?? tabs[0];
  return <div className="flex shrink-0 flex-col border-t border-border">
    {open && splitter}
    <div role="tablist" aria-label={dock.label} className="flex items-center gap-0.5 px-1 py-0.5">
      {tabs.map((item) => <button key={item.id} type="button" role="tab" aria-selected={open && item.id === current?.id}
        className={cn("flex items-center gap-1 rounded px-2 py-0.5 text-xs", open && item.id === current?.id ? "bg-row-selected font-semibold" : "text-muted hover:text-foreground")}
        onClick={() => onOpen(item.id)}>{item.title}{item.badge !== undefined && item.badge !== 0 && <span className="rounded-full bg-primary/15 px-1.5 text-[10px] text-primary">{item.badge}</span>}</button>)}
    </div>
    {open && current && <div role="tabpanel" className="min-h-0 overflow-auto border-t border-border" style={{ height }}>{current.content}</div>}
  </div>;
}

/** A diagnostics list for the dock: each row can locate its subject. */
export type WorkbenchProblem = { id: string; severity?: "error" | "warning" | "info"; text: ReactNode; subject?: string; locate?: () => void };
export function ProblemList({ problems, empty }: { problems: WorkbenchProblem[]; empty?: ReactNode }) {
  if (!problems.length) return <p className="p-3 text-xs text-muted">{empty ?? t("No problems.")}</p>;
  return <ul className="divide-y divide-border text-xs">
    {problems.map((problem) => <li key={problem.id}>
      <button type="button" disabled={!problem.locate} onClick={problem.locate} className="flex w-full items-start gap-2 px-3 py-1.5 text-left hover:bg-row-hover disabled:cursor-default disabled:hover:bg-transparent">
        <span aria-hidden className={cn("mt-1 size-2 shrink-0 rounded-full", problem.severity === "warning" ? "bg-warning" : problem.severity === "info" ? "bg-primary" : "bg-danger")} />
        <span className="min-w-0 flex-1">{problem.text}</span>
        {problem.subject && <span className="shrink-0 font-mono text-[10px] text-muted">{problem.subject}</span>}
      </button>
    </li>)}
  </ul>;
}

/** A row of resource structure: icon, label, trailing meta, selectable. */
export function StructureRow({ icon, label, meta, selected, depth = 0, onClick, actions, className }: {
  icon?: ReactNode; label: ReactNode; meta?: ReactNode; selected?: boolean; depth?: number; onClick?: () => void; actions?: ReactNode; className?: string;
}) {
  return <div className={cn("group flex min-w-0 items-center gap-1 rounded pr-1", selected ? "bg-row-selected" : "hover:bg-row-hover", className)} style={{ paddingLeft: 6 + depth * 14 }}>
    <button type="button" aria-pressed={selected} onClick={onClick} className="flex min-w-0 flex-1 items-center gap-1.5 py-1 text-left text-xs">
      {icon && <span className="flex size-4 shrink-0 items-center justify-center text-muted [&>svg]:size-3.5">{icon}</span>}
      <span className="min-w-0 flex-1 truncate">{label}</span>
      {meta && <span className="shrink-0 text-[10px] text-muted">{meta}</span>}
    </button>
    {actions && <span className="flex shrink-0 items-center opacity-0 group-focus-within:opacity-100 group-hover:opacity-100">{actions}</span>}
  </div>;
}

/** Section heading inside a structure or inspector panel. */
export function PanelSection({ title, actions, children, className }: { title: ReactNode; actions?: ReactNode; children?: ReactNode; className?: string }) {
  return <section className={cn("grid min-w-0 gap-1 px-2 py-2", className)}>
    <div className="flex items-center gap-1 px-1"><h3 className="min-w-0 flex-1 truncate text-[11px] font-semibold uppercase tracking-wide text-muted">{title}</h3>{actions}</div>
    {children}
  </section>;
}
