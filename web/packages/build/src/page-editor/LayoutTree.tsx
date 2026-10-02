import { loopOwner, overlayOwner, variableAccessible, type LayoutKind } from "../page-layout";
import type { Api } from "@platform/kernel";
import { Button, Card, Input, Select, cn, t } from "@platform/ui";
import { useState, type DragEvent, type ReactNode } from "react";
import { widgetContract } from "@platform/app";
import { ArrowDown, ArrowUp, ChevronDown, ChevronRight, Columns2, Layers, Plus, Rows3, Settings2, Trash2 } from "lucide-react";

type Label = { id?: string; widget: string; title?: string };

export function LayoutTree({ document, sections, chosen, container, title, widgetTitles, widgets, onChoose, onContainer, onAdd, onMove, onRemove, onGroup, onRelocate, onInsert, onAddOverlay }: {
  document: Api.PageDocument; sections: Label[]; chosen: number; container?: string; title: string;
  widgetTitles: Record<string, () => string>; widgets: readonly string[];
  onChoose: (i: number) => void; onContainer: (id: string) => void; onAdd: (widget: string) => void;
  onMove: (i: number, by: -1 | 1) => void;
  onRelocate: (section: string, container: string, afterSection?: string) => void;
  onInsert: (widget: string, container: string, afterSection?: string) => void; onRemove: (i: number) => void; onGroup: (kind: LayoutKind) => void; onAddOverlay: () => void;
}) {
  const [search, setSearch] = useState("");
  const [expanded, setExpanded] = useState<Record<string, boolean>>({});
  const [hover, setHover] = useState<string>();
  const drop = (event: DragEvent, container: string, after?: string) => {
    const widget = event.dataTransfer.getData("application/platform-page-widget"), section = event.dataTransfer.getData("application/platform-page-section");
    if (!widget && !section) return;
    event.preventDefault(); event.stopPropagation(); setHover(undefined);
    if (widget) onInsert(widget, container, after); else onRelocate(section, container, after);
  };
  const over = (event: DragEvent, id: string) => {
    if (!event.dataTransfer.types.some((type) => type === "application/platform-page-widget" || type === "application/platform-page-section")) return;
    event.preventDefault(); event.stopPropagation(); setHover(id);
  };
  const indexed = new Map(sections.map((section, i) => [section.id, { section, i }]));
  const renderNode = (id: string, ancestors = new Set<string>()): ReactNode => {
    if (ancestors.has(id)) return <li key={id} role="alert">{t("This page layout is unavailable.")}</li>;
    const next = new Set(ancestors); next.add(id);
    const node = document.nodes[id];
    if (!node) return null;
    if (node.kind === "widget") {
      const item = indexed.get(node.section);
      if (!item) return null;
      const { section, i } = item;
      return <li key={id} className="min-w-0" onDragOver={(event) => over(event, id)} onDragLeave={() => setHover(undefined)} onDrop={(event) => drop(event, document.root, section.id)}>
        <div className={cn("flex items-center gap-0.5 rounded border px-1 py-0.5", i === chosen || hover === id ? "border-primary bg-row-selected" : "border-transparent")}>
          <Button variant="ghost" size="sm" className="min-w-0 w-0 flex-1 justify-start" aria-pressed={i === chosen} onClick={() => onChoose(i)} draggable onDragStart={(event) => { event.dataTransfer.setData("application/platform-page-section", section.id!); event.dataTransfer.effectAllowed = "move"; }}>
            <Layers className="size-3 shrink-0" /><span className="min-w-0 truncate">{section.title || widgetTitles[section.widget]?.() || section.widget}</span>
          </Button>
          <Button size="sm" variant="ghost" aria-label={t("Move up")} onClick={() => onMove(i, -1)}><ArrowUp className="size-3" /></Button>
          <Button size="sm" variant="ghost" aria-label={t("Move down")} onClick={() => onMove(i, 1)}><ArrowDown className="size-3" /></Button>
          <Button size="sm" variant="ghost" aria-label={t("Remove section")} onClick={() => onRemove(i)}><Trash2 className="size-3" /></Button>
        </div>
      </li>;
    }
    return <li key={id} className="min-w-0">
      <div className={cn("flex items-center rounded", hover === id && "outline outline-primary bg-row-selected")}
        onDragOver={(event) => over(event, id)} onDragLeave={() => setHover(undefined)} onDrop={(event) => drop(event, id)}>
        <Button size="sm" variant="ghost" aria-label={t("Expand or collapse layout group")} aria-expanded={expanded[id] !== false} onClick={() => setExpanded((old) => ({ ...old, [id]: old[id] === false }))}>{expanded[id] === false ? <ChevronRight className="size-3" /> : <ChevronDown className="size-3" />}</Button>
        <Button size="sm" variant="ghost" aria-pressed={container === id && chosen === -2} className={cn("min-w-0 w-0 flex-1 justify-start", container === id && chosen === -2 && "bg-row-selected text-primary")} onClick={() => onContainer(id)}>
          {node.kind === "columns" ? <Columns2 className="size-3" /> : <Rows3 className="size-3" />}{t(node.kind === "tabs" ? "Tabs" : node.kind === "columns" ? "Columns" : node.kind === "flow" ? "Flow layout" : node.kind === "toolbar" ? "Toolbar" : node.kind === "loop" ? "Loop" : "Rows")}<span className="ml-auto text-[10px] text-muted">{node.children?.length ?? 0}</span>
        </Button>
      </div>
      {expanded[id] !== false && <ul className="ml-3 grid min-w-0 gap-0.5 border-l border-border pl-2">{node.children?.map((child) => renderNode(child, next))}</ul>}
    </li>;
  };
  return <div className="grid content-start gap-3 p-3">
    <Button variant="ghost" size="sm" aria-pressed={chosen === -1} aria-label={t("Page settings")} onClick={() => onChoose(-1)}
      className={cn("justify-start border", chosen === -1 ? "border-primary bg-row-selected" : "border-border")}>
      <Settings2 className="size-3" /><span className="truncate">{t("Page settings")}</span><span aria-hidden className="ml-auto truncate text-[10px] text-muted">{title}</span>
    </Button>
    <div className="grid gap-1">
      <div className="text-xs font-semibold text-muted">{t("Add a widget")}</div>
      <Input aria-label={t("Search widgets")} placeholder={t("Search widgets")} value={search} onChange={(event) => setSearch(event.target.value)} />
      <div className="grid grid-cols-2 gap-1">{widgets.filter((widget) => `${widgetTitles[widget]?.() ?? widget} ${widgetContract(widget)?.category ?? ""}`.toLowerCase().includes(search.toLowerCase())).map((widget) => <Button key={widget} size="sm" className="justify-start truncate" onClick={() => onAdd(widget)} draggable onDragStart={(event) => { event.dataTransfer.setData("application/platform-page-widget", widget); event.dataTransfer.effectAllowed = "copy"; }}><Plus className="size-3" />{widgetTitles[widget]?.() || widget}</Button>)}</div>
    </div>
    <div className="grid min-w-0 gap-2 border-t border-border pt-3">
      <div className="text-xs font-semibold text-muted">{t("Layout")}</div>
      <ul className="grid min-w-0 gap-1">{renderNode(document.root)}</ul>
      <div className="grid gap-2">
        <strong className="text-xs text-muted">{t("Overlays")}</strong>
        {Object.entries(document.overlays ?? {}).map(([id, overlay]) => <div key={id} className="grid gap-1"><span className="truncate text-xs">{overlay.title} · {t(overlay.kind === "drawer" ? "Drawer" : "Modal")}</span><ul>{renderNode(overlay.root)}</ul></div>)}
        <Button size="sm" disabled={Object.keys(document.overlays ?? {}).length >= 16} onClick={onAddOverlay}>{t("Add overlay")}</Button>
      </div>
      <div className="flex flex-wrap gap-1">
        <Button size="sm" disabled={chosen < 0} aria-label={t("Group with next in rows")} onClick={() => onGroup("rows")}><Rows3 className="size-3" />{t("Rows")}</Button>
        <Button size="sm" disabled={chosen < 0} aria-label={t("Group with next in columns")} onClick={() => onGroup("columns")}><Columns2 className="size-3" />{t("Columns")}</Button>
        <Button size="sm" disabled={chosen < 0} onClick={() => onGroup("flow")}>{t("Flow layout")}</Button>
        <Button size="sm" disabled={chosen < 0} onClick={() => onGroup("toolbar")}>{t("Toolbar")}</Button>
        <Button size="sm" disabled={chosen < 0} onClick={() => onGroup("loop")}>{t("Loop")}</Button>
        <Button size="sm" disabled={chosen < 0} aria-label={t("Group with next in tabs")} onClick={() => onGroup("tabs")}>{t("Tabs")}</Button>
      </div>
      {sections.length === 0 && <p className="text-xs text-muted">{t("Add what people should see.")}</p>}
    </div>
  </div>;
}

export function LayoutProperties({ document, id, onChange, onPatch, onUngroup }: {
  document: Api.PageDocument; id: string; onChange: (kind: LayoutKind) => void; onPatch: (id: string, patch: Partial<Api.PageLayoutNode>) => void; onUngroup: () => void;
}) {
  const node = document.nodes[id];
  if (!node) return null;
  return <Card className="grid content-start gap-3 p-3">
    <div className="text-xs font-semibold text-muted">{t("Layout container")}</div>
    <label className="grid gap-1 text-xs">{t("Layout")}
      <Select value={node.kind} onChange={(event) => onChange(event.target.value as LayoutKind)}>
        <option value="rows">{t("Rows")}</option><option value="columns">{t("Columns")}</option><option value="tabs">{t("Tabs")}</option><option value="flow">{t("Flow layout")}</option><option value="toolbar">{t("Toolbar")}</option><option value="loop">{t("Loop")}</option>
      </Select>
    </label>
    <label className="grid gap-1 text-xs">{t("Container title")}<Input value={node.title ?? ""} onChange={(event) => onPatch(id, { title: event.target.value })} /></label>
    {(node.kind === "flow" || node.kind === "toolbar") && <label className="grid gap-1 text-xs">{t("Alignment")}<Select value={node.align ?? "start"} onChange={(event) => onPatch(id, { align: event.target.value })}>
      <option value="start">{t("Start")}</option><option value="center">{t("Center")}</option><option value="end">{t("End")}</option><option value="between">{t("Space between")}</option>
    </Select></label>}
    {node.kind === "loop" && node.loop && <>
      <label className="grid gap-1 text-xs">{t("Loop query window")}<Select value={node.loop.collection} onChange={(event) => onPatch(id, { loop: { ...node.loop!, collection: event.target.value } })}><option value="">{t("Choose a query window")}</option>{Object.entries(document.variables ?? {}).filter(([, value]) => (loopOwner(document,id)?value.scope==="loop-item"&&value.owner===loopOwner(document,id)&&value.source?.kind==="plan":variableAccessible(value,undefined,overlayOwner(document,id))) && value.type === "object-set" && (value.source?.kind === "query" || value.source?.kind === "plan" || value.mode==="shared")).map(([key, value]) => <option key={key} value={key}>{value.title || key}</option>)}</Select></label>
      <label className="grid gap-1 text-xs">{t("Loop item limit")}<Input type="number" min={1} max={100} value={node.loop.limit} onChange={(event) => onPatch(id, { loop: { ...node.loop!, limit: Number(event.target.value) } })} /></label>
      <p className="text-xs text-muted">{t("Record widgets bind to each item. The source query stays outside the loop.")}</p>
    </>}
    {node.kind === "tabs" && <>
      <label className="grid gap-1 text-xs">{t("Active tab variable")}<Select value={node.activeVariable ?? ""} onChange={(event) => onPatch(id, { activeVariable: event.target.value })}>
        {Object.entries(document.variables ?? {}).filter(([, value]) => value.type === "string" && value.mode === "state").map(([key, value]) => <option key={key} value={key}>{value.title || key}</option>)}
      </Select></label>
      {node.children?.map((child, i) => <label key={child} className="grid gap-1 text-xs">{t("Tab {n} title", { n: i + 1 })}<Input value={document.nodes[child]?.title ?? ""} onChange={(event) => onPatch(child, { title: event.target.value })} /></label>)}
      <p className="text-xs text-muted">{t("Tabs keep visited content mounted until this page session ends.")}</p>
    </>}
    <p className="text-xs text-muted">{t("Add widgets to this container, or nest another group inside it.")}</p>
    <Button disabled={id === document.root || Object.values(document.overlays ?? {}).some((overlay) => overlay.root === id)} onClick={onUngroup}>{t("Ungroup")}</Button>
  </Card>;
}
