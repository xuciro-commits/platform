import { InputDraftProvider, InputProblems, useInputDrafts } from "../fields/draft";
import { useEffect, useId, useLayoutEffect, useRef, useState, type ReactNode } from "react";
import { ChevronRight, PanelBottomClose, PanelBottomOpen, PanelLeftClose, PanelLeftOpen, PanelRightClose, PanelRightOpen, Redo2, Undo2, X } from "lucide-react";
import { t } from "../i18n";
import { cn } from "../lib/cn";
import { Button } from "../primitives/button";
import { useViewTitle } from "../shell/Workspace";
import { ViewVisibilityContext, useViewVisible } from "../shell/ViewVisibility";
import { ContentTabs } from "./ContentTabs";
import { PlainPanels } from "../primitives/card";

export type WorkbenchCrumb = { label: ReactNode; onClick?: () => void };
export type WorkbenchTab = { id: string; title: ReactNode; badge?: number | string; content: ReactNode };
export type WorkbenchPanel = { label: string; heading?: ReactNode; open?: boolean; onOpenChange?: (open: boolean) => void; scope?: string; locate?: (path?: string) => void; tabs?: WorkbenchTab[]; value?: string; onChange?: (id: string) => void; content?: ReactNode; min?: number; max?: number };
export type WorkbenchHistory = { canUndo: boolean; canRedo: boolean; undo: () => void; redo: () => void };
export type WorkbenchSaving = "idle" | "dirty" | "saving" | "saved" | "error";

type Side = "left" | "right" | "dock";
type Layout = { left: number; right: number; dock: number; leftOpen: boolean; rightOpen: boolean; dockOpen: boolean; dockTab?: string };
const defaults: Layout = { left: 264, right: 320, dock: 200, leftOpen: true, rightOpen: true, dockOpen: false };
const read = (key: string): Layout => {
  try {
    const raw = JSON.parse(localStorage.getItem(`workbench:${key}`) ?? "null");
    if (!raw || typeof raw !== "object") return defaults;
    return { ...defaults,
      ...Object.fromEntries((["left", "right", "dock"] as const).filter(side => typeof raw[side] === "number" && Number.isFinite(raw[side]) && raw[side] > 0).map(side => [side, raw[side]])),
      ...Object.fromEntries((["leftOpen", "rightOpen", "dockOpen"] as const).filter(key => typeof raw[key] === "boolean").map(key => [key, raw[key]])),
      dockTab: typeof raw.dockTab === "string" ? raw.dockTab : undefined,
    };
  } catch { return defaults; }
};

export function Workbench({ storageKey, crumbs = [], title, status, saving, history, actions, left, right, dock, children, className, onKeyDown, mainLabel }: {
  storageKey: string; crumbs?: WorkbenchCrumb[]; title?: ReactNode; status?: ReactNode; saving?: WorkbenchSaving; history?: WorkbenchHistory; actions?: ReactNode;
  left?: WorkbenchPanel; right?: WorkbenchPanel;
  dock?: WorkbenchPanel;
  children: ReactNode; className?: string; mainLabel?: string;
  onKeyDown?: (event: React.KeyboardEvent<HTMLDivElement>) => void;
}) {
  const inputs = useInputDrafts(), visible = useViewVisible(), prefix = useId();
  if (inputs?.dirty && saving !== "saving" && saving !== "error") saving = "dirty";
  useViewTitle(typeof title === "string" ? title : undefined);
  const root = useRef<HTMLDivElement>(null), main = useRef<HTMLDivElement>(null);
  const [layout, setLayout] = useState<Layout>(() => read(storageKey));
  const [dimensions, setDimensions] = useState({ width: 0, height: 0 });
  const compact = dimensions.width > 0 && dimensions.width < 960;
  const [focusedPane, setFocusedPane] = useState<"left" | "right">();
  useLayoutEffect(() => {
    const measure = () => {
      if (!root.current?.clientWidth) return;
      const width = root.current.clientWidth, height = main.current?.clientHeight ?? 0;
      setDimensions(old => old.width === width && (!height || old.height === height) ? old : { width, height: height || old.height });
    };
    measure();
    if (typeof ResizeObserver === "undefined") return;
    const observer = new ResizeObserver(measure);
    if (root.current) observer.observe(root.current);
    if (main.current) observer.observe(main.current);
    return () => observer.disconnect();
  }, []);
  useEffect(() => { if (!compact) setFocusedPane(undefined); }, [compact]);
  const previousDock = useRef(dock?.value);
  useEffect(() => {
    if (previousDock.current === dock?.value) return;
    previousDock.current = dock?.value;
    if (dock?.value) {
      setLayout(old => ({ ...old, dockTab: dock.value, dockOpen: dock.open ?? true }));
      setFocusedPane(undefined);
    }
  }, [dock?.value, dock?.open]);
  useEffect(() => { if (dock?.open) setFocusedPane(undefined); }, [dock?.open]);
  useEffect(() => { try { localStorage.setItem(`workbench:${storageKey}`, JSON.stringify(layout)); } catch {   } }, [layout, storageKey]);
  const patch = (next: Partial<Layout>) => setLayout(old => ({ ...old, ...next }));
  const showLeft = !!left && (compact ? focusedPane === "left" : left.open ?? layout.leftOpen);
  const showRight = !!right && (compact ? focusedPane === "right" : right.open ?? layout.rightOpen);
  const showMain = !compact || !showLeft && !showRight;
  const showDock = !!dock && (dock.open ?? layout.dockOpen);
  const reveal = (side: "left" | "right") => {
    (side === "left" ? left : right)?.onOpenChange?.(true);
    patch(side === "left" ? { leftOpen: true } : { rightOpen: true });
    if (compact) { setDockOpen(false); setFocusedPane(side); }
  };
  const togglePane = (side: "left" | "right") => {
    const showing = side === "left" ? showLeft : showRight;
    (side === "left" ? left : right)?.onOpenChange?.(!showing);
    if (compact) { if (!showing) setDockOpen(false); setFocusedPane(showing ? undefined : side); }
    else patch(side === "left" ? { leftOpen: !showing } : { rightOpen: !showing });
    if (showing) document.getElementById(`${prefix}-toggle-${side}`)?.focus();
    else requestAnimationFrame(() => { if (root.current) document.getElementById(`${prefix}-${side}`)?.focus(); });
  };
  const setDockOpen = (open: boolean) => {
    dock?.onOpenChange?.(open);
    patch({ dockOpen: open });
    if (open) setFocusedPane(undefined);
    else if (document.getElementById(`${prefix}-dock`)?.contains(document.activeElement)) document.getElementById(`${prefix}-toggle-dock`)?.focus();
  };
  const chooseDock = (id: string) => { dock?.onChange?.(id); patch({ dockTab: id }); setDockOpen(true); };
  const previousInspector = useRef({ scope: right?.scope, value: right?.value, open: right?.open });
  useEffect(() => {
    const old = previousInspector.current;
    previousInspector.current = { scope: right?.scope, value: right?.value, open: right?.open };
    if (!right) return;
    if (right.open === false) { setFocusedPane(pane => pane === "right" ? undefined : pane); return; }
    if (right.open === true && old.open !== right.open || old.scope !== right.scope || old.value !== right.value) {
      setLayout(layout => ({ ...layout, rightOpen: true }));
      if (compact) { dock?.onOpenChange?.(false); setLayout(layout => ({ ...layout, dockOpen: false })); setFocusedPane("right"); }
    }
  }, [right?.scope, right?.value, right?.open, compact]);
  const previousStructureOpen = useRef(left?.open);
  useEffect(() => {
    const wasOpen = previousStructureOpen.current;
    previousStructureOpen.current = left?.open;
    if (left?.open === false) {
      setFocusedPane(pane => pane === "left" ? undefined : pane);
      if (document.getElementById(`${prefix}-left`)?.contains(document.activeElement)) document.getElementById(`${prefix}-toggle-left`)?.focus();
    } else if (left?.open === true && (wasOpen !== true || compact)) {
      setLayout(layout => ({ ...layout, leftOpen: true }));
      if (compact) { setDockOpen(false); setFocusedPane("left"); }
      const frame = requestAnimationFrame(() => document.getElementById(`${prefix}-left`)?.focus());
      return () => cancelAnimationFrame(frame);
    }
  }, [left?.open, compact]);
  const previousFocusedPane = useRef(focusedPane);
  useEffect(() => {
    const previous = previousFocusedPane.current;
    previousFocusedPane.current = focusedPane;
    if (compact && previous === "left" && focusedPane !== "left") left?.onOpenChange?.(false);
  }, [compact, focusedPane]);
  const panel = (side: Side) => side === "left" ? left : side === "right" ? right : dock;
  const limits = (side: Side) => ({ min: panel(side)?.min ?? (side === "dock" ? 120 : 200), max: panel(side)?.max ?? (side === "dock" ? 520 : 560) });
  const baseSize = (side: Side) => Math.max(limits(side).min, Math.min(limits(side).max, layout[side]));
  const sizes = { left: baseSize("left"), right: baseSize("right"), dock: baseSize("dock") };
  const paneSpace = dimensions.width - 320 - (showLeft ? 6 : 0) - (showRight ? 6 : 0);
  const minimum = (showLeft ? limits("left").min : 0) + (showRight ? limits("right").min : 0);
  const wanted = (showLeft ? sizes.left - limits("left").min : 0) + (showRight ? sizes.right - limits("right").min : 0);
  if (!compact && dimensions.width && wanted > paneSpace - minimum && wanted > 0) {
    const scale = Math.max(0, paneSpace - minimum) / wanted;
    if (showLeft) sizes.left = limits("left").min + (sizes.left - limits("left").min) * scale;
    if (showRight) sizes.right = limits("right").min + (sizes.right - limits("right").min) * scale;
  }
  const bounds = (side: Side) => {
    const { min, max } = limits(side);
    const available = side === "dock" ? dimensions.height - 160 : dimensions.width - 320 - (side === "left" && showRight ? sizes.right + 6 : side === "right" && showLeft ? sizes.left + 6 : 0) - 6;
    return { min, max: Math.max(min, Math.min(max, (side === "dock" ? dimensions.height : dimensions.width) ? available : max)) };
  };
  sizes.dock = Math.min(sizes.dock, bounds("dock").max);
  const resize = (side: Side, size: number) => {
    const { min, max } = bounds(side);
    patch(side === "dock" ? { dock: Math.max(min, Math.min(max, size)) } : { left: sizes.left, right: sizes.right, [side]: Math.max(min, Math.min(max, size)) });
  };
  const [drag, setDrag] = useState<{ side: Side; at: number; size: number }>();
  const splitter = (side: Side) => <div role="separator" tabIndex={0} aria-orientation={side === "dock" ? "horizontal" : "vertical"}
    aria-label={t(side === "left" ? "Resize structure panel" : side === "right" ? "Resize inspector panel" : "Resize dock")}
    aria-controls={`${prefix}-${side}`} aria-valuemin={bounds(side).min} aria-valuemax={bounds(side).max} aria-valuenow={sizes[side]}
    className={cn("shrink-0 touch-none rounded hover:bg-primary/20 focus-visible:bg-primary/20 focus-visible:outline-ring", side === "dock" ? "h-1.5 w-full cursor-row-resize" : "w-1.5 cursor-col-resize")}
    onDoubleClick={() => resize(side, defaults[side])}
    onPointerDown={event => { if (event.button !== 0) return; event.preventDefault(); event.currentTarget.setPointerCapture(event.pointerId); setDrag({ side, at: side === "dock" ? event.clientY : event.clientX, size: sizes[side] }); }}
    onPointerMove={event => { if (drag?.side !== side) return; const delta = side === "dock" ? drag.at - event.clientY : (event.clientX - drag.at) * (side === "left" ? 1 : -1); resize(side, drag.size + delta); }}
    onPointerUp={event => { if (event.currentTarget.hasPointerCapture(event.pointerId)) event.currentTarget.releasePointerCapture(event.pointerId); setDrag(undefined); }}
    onPointerCancel={() => setDrag(undefined)} onLostPointerCapture={() => setDrag(undefined)}
    onKeyDown={event => {
      const keys = side === "dock" ? ["ArrowUp", "ArrowDown"] : ["ArrowLeft", "ArrowRight"];
      if (event.key === "Home" || event.key === "End") { event.preventDefault(); resize(side, event.key === "Home" ? bounds(side).min : bounds(side).max); return; }
      if (!keys.includes(event.key)) return;
      event.preventDefault();
      const grow = event.key === "ArrowRight" && side === "left" || event.key === "ArrowLeft" && side === "right" || event.key === "ArrowUp";
      resize(side, sizes[side] + (grow ? 16 : -16));
    }} />;
  const columns = compact ? "minmax(0,1fr)" : `${showLeft ? `${sizes.left}px 6px ` : ""}minmax(0,1fr)${showRight ? ` 6px ${sizes.right}px` : ""}`;
  const savingLabel = saving === "saving" ? t("Saving…") : saving === "saved" ? t("Saved") : saving === "dirty" ? t("Unsaved changes") : saving === "error" ? t("Not saved") : "";
  const paneHeading = (side: "left" | "right", panel: WorkbenchPanel) => (compact || panel.heading) && <div className="flex shrink-0 items-center gap-2 border-b border-border px-2 py-1">
    <h2 className="min-w-0 flex-1 truncate text-sm font-semibold">{panel.heading ?? panel.label}</h2>
    <Button variant="ghost" size="sm" aria-label={t(side === "left" ? "Close structure panel" : "Close inspector panel")} onClick={() => togglePane(side)}><X /></Button>
  </div>;
  return <div ref={root} className={cn("flex h-full min-h-0 min-w-0 flex-1 flex-col overflow-hidden rounded-md border border-border bg-surface", className)} tabIndex={-1}
    onKeyDown={event => {
      if (event.defaultPrevented) return;
      const command = event.metaKey || event.ctrlKey;
      if (command && event.key === "\\") { event.preventDefault(); if (event.altKey && right) togglePane("right"); else if (left) togglePane("left"); return; }
      if (command && event.key.toLowerCase() === "j" && dock) { event.preventDefault(); setDockOpen(!showDock || !showMain); return; }
      if (event.key === "Escape" && compact && focusedPane) { event.preventDefault(); togglePane(focusedPane); return; }
      onKeyDown?.(event);
    }}>
    <InputProblems /><header className="flex shrink-0 flex-wrap items-center gap-1 border-b border-border px-2 py-1">
      {left && <Button id={`${prefix}-toggle-left`} variant="ghost" size="sm" aria-label={t("Toggle structure panel")} aria-controls={`${prefix}-left`} aria-pressed={showLeft} aria-expanded={showLeft} title="⌘\\" onClick={() => togglePane("left")}>{showLeft ? <PanelLeftClose /> : <PanelLeftOpen />}</Button>}
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
      <fieldset disabled={inputs?.busy} className="ml-auto flex flex-wrap items-center gap-1">{actions}</fieldset>
      {dock && <Button id={`${prefix}-toggle-dock`} variant="ghost" size="sm" aria-label={t("Toggle dock")} aria-controls={`${prefix}-dock`} aria-pressed={showDock && showMain} aria-expanded={showDock && showMain} title="⌘J" onClick={() => setDockOpen(!showDock || !showMain)}>{showDock && showMain ? <PanelBottomClose /> : <PanelBottomOpen />}</Button>}
      {right && <Button id={`${prefix}-toggle-right`} variant="ghost" size="sm" aria-label={t("Toggle inspector panel")} aria-controls={`${prefix}-right`} aria-pressed={showRight} aria-expanded={showRight} title="⌥⌘\\" onClick={() => togglePane("right")}>{showRight ? <PanelRightClose /> : <PanelRightOpen />}</Button>}
    </header>
    <fieldset disabled={inputs?.busy || saving === "saving"} className="grid min-h-0 min-w-0 flex-1 border-0 p-0" style={{ gridTemplateColumns: columns }}>
      {left && <>
        <aside id={`${prefix}-left`} role="region" aria-label={left.label} tabIndex={-1} hidden={!showLeft} style={{ display: showLeft ? undefined : "none" }} className="flex min-h-0 min-w-0 flex-col overflow-hidden border-r border-border">
          <ViewVisibilityContext.Provider value={visible && showLeft}>
            {paneHeading("left", left)}
            <InputDraftProvider scope={left.scope ?? "structure"} reveal={() => reveal("left")}><PanelBody panel={left} /></InputDraftProvider>
          </ViewVisibilityContext.Provider>
        </aside>
        {!compact && showLeft && splitter("left")}
      </>}
      <div ref={main} role="region" aria-label={mainLabel ?? t("Editor")} hidden={!showMain} style={{ display: showMain ? undefined : "none" }} className="flex min-h-0 min-w-0 flex-col overflow-hidden">
        <ViewVisibilityContext.Provider value={visible && showMain}>
          <div className="flex min-h-0 min-w-0 flex-1 flex-col overflow-hidden">{children}</div>
          {dock && <div id={`${prefix}-dock`} className="flex shrink-0 flex-col border-t border-border" style={{ height: showDock ? sizes.dock + 36 : undefined }}>
            {showDock && splitter("dock")}
            <ContentTabs compact label={dock.label} items={tabItems(dock.tabs ?? [])} value={dock.value ?? layout.dockTab} open={showDock} onChange={chooseDock}
              actions={showDock && <Button variant="ghost" size="sm" aria-label={t("Close dock")} onClick={() => setDockOpen(false)}><X /></Button>} />
          </div>}
        </ViewVisibilityContext.Provider>
      </div>
      {right && <>
        {!compact && showRight && splitter("right")}
        <aside id={`${prefix}-right`} role="region" aria-label={right.label} tabIndex={-1} hidden={!showRight} style={{ display: showRight ? undefined : "none" }} className="flex min-h-0 min-w-0 flex-col overflow-hidden border-l border-border">
          <ViewVisibilityContext.Provider value={visible && showRight}>
            {paneHeading("right", right)}
            <InputDraftProvider scope={right.scope ?? "inspector"} reveal={path => { reveal("right"); right.locate?.(path); }}><PanelBody panel={right} /></InputDraftProvider>
          </ViewVisibilityContext.Provider>
        </aside>
      </>}
    </fieldset>
  </div>;
}

function tabItems(tabs: WorkbenchTab[]) {
  return tabs.map(tab => ({ ...tab, title: <>{tab.title}{tab.badge !== undefined && tab.badge !== 0 && <span className="rounded-full bg-primary/15 px-1.5 text-2xs text-primary">{tab.badge}</span>}</> }));
}

function PanelBody({ panel }: { panel: WorkbenchPanel }) {
  const [own, setOwn] = useState(panel.tabs?.[0]?.id);
  const choose = (id: string) => { setOwn(id); panel.onChange?.(id); };
  if (!panel.tabs?.length) return <PlainPanels><div className="min-h-0 flex-1 overflow-auto">{panel.content}</div></PlainPanels>;
  return <PlainPanels><ContentTabs key={panel.scope} compact label={panel.label} value={panel.value ?? own} onChange={choose}
    items={tabItems(panel.tabs.map(tab => ({ ...tab, content: <InputDraftProvider scope={tab.id} reveal={path => { choose(tab.id); panel.locate?.(path); }}>{tab.content}</InputDraftProvider> })))} /></PlainPanels>;
}

export type WorkbenchProblem = { id: string; severity?: "error" | "warning" | "info"; text: ReactNode; subject?: string; locate?: () => void };
export function ProblemList({ problems, empty }: { problems: WorkbenchProblem[]; empty?: ReactNode }) {
  if (!problems.length) return <p className="p-3 text-xs text-muted">{empty ?? t("No problems.")}</p>;
  return <ul className="divide-y divide-border text-xs">
    {problems.map((problem) => <li key={problem.id}>
      <button type="button" disabled={!problem.locate} onClick={problem.locate} className="flex w-full items-start gap-2 px-3 py-1.5 text-left hover:bg-row-hover disabled:cursor-default disabled:hover:bg-transparent">
        <span aria-hidden className={cn("mt-1 size-2 shrink-0 rounded-full", problem.severity === "warning" ? "bg-warning" : problem.severity === "info" ? "bg-primary" : "bg-danger")} />
        <span className="min-w-0 flex-1">{problem.text}</span>
        {problem.subject && <span className="shrink-0 font-mono text-2xs text-muted">{problem.subject}</span>}
      </button>
    </li>)}
  </ul>;
}

export function StructureRow({ icon, label, meta, selected, depth = 0, onClick, actions, className }: {
  icon?: ReactNode; label: ReactNode; meta?: ReactNode; selected?: boolean; depth?: number; onClick?: () => void; actions?: ReactNode; className?: string;
}) {
  return <div className={cn("group flex min-w-0 items-center gap-1 rounded pr-1", selected ? "bg-row-selected" : "hover:bg-row-hover", className)} style={{ paddingLeft: 6 + depth * 14 }}>
    <button type="button" aria-pressed={selected} onClick={onClick} className="flex min-w-0 flex-1 items-center gap-1.5 py-1 text-left text-xs">
      {icon && <span className="flex size-4 shrink-0 items-center justify-center text-muted [&>svg]:size-3.5">{icon}</span>}
      <span className="min-w-0 flex-1 truncate">{label}</span>
      {meta && <span className="shrink-0 text-2xs text-muted">{meta}</span>}
    </button>
    {actions && <span className="flex shrink-0 items-center opacity-0 group-focus-within:opacity-100 group-hover:opacity-100">{actions}</span>}
  </div>;
}

export function PanelSection({ title, actions, children, className }: { title: ReactNode; actions?: ReactNode; children?: ReactNode; className?: string }) {
  return <section className={cn("grid min-w-0 gap-1 px-2 py-2", className)}>
    <div className="flex items-center gap-1 px-1"><h3 className="min-w-0 flex-1 truncate text-xs font-semibold uppercase tracking-wide text-muted">{title}</h3>{actions}</div>
    {children}
  </section>;
}
