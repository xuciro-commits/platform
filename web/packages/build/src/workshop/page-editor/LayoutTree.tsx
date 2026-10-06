import {WidgetGlyph,LayoutGlyph} from "./WidgetGlyph";
import { type LayoutKind } from "../page-layout";
import {type Api} from "@platform/kernel";
import { Button, Card, CommandMenu, cn, t, useCanvasGesture, type ContextCommand } from "@platform/ui";
import { useId, useState, type ReactNode } from "react";
import { widgetContract } from "@platform/app";
import { ChevronDown, ChevronRight, Columns2, Rows3, Settings2 } from "lucide-react";

type Label = { id?: string; widget: string; title?: string };

export function LayoutTree({document,sections,chosen,container,title,widgetTitles,onChoose,onContainer,onGroup,onAddOverlay,commandsForNode}: {
  document:Api.PageDocument;sections:Label[];chosen:number;container?:string;title:string;
  widgetTitles:Record<string,()=>string>;
  onChoose:(index:number)=>void;onContainer:(id:string)=>void;
  onGroup:(kind:LayoutKind)=>void;onAddOverlay:()=>void;commandsForNode:(id:string)=>ContextCommand[];
}) {
  const focusScope=useId();
  const [expanded, setExpanded] = useState<Record<string, boolean>>({});
  const canvas=useCanvasGesture();
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
      const commands=commandsForNode(id);
      return <li key={id} className="min-w-0"><CommandMenu focusKey={`${focusScope}:${section.id}`} label={t("Commands for {widget}",{widget:section.title||widgetTitles[section.widget]?.()||section.widget})} commands={commands}>
        <div data-canvas-tree={id} className={cn("flex items-center gap-0.5 rounded border px-1 py-0.5", i === chosen ? "border-primary bg-row-selected" : "border-transparent")}>
          <Button variant="ghost" size="sm" className="min-w-0 w-0 flex-1 justify-start" title={section.title||widgetTitles[section.widget]?.()||section.widget} aria-pressed={i === chosen} onClick={() => onChoose(i)} style={{touchAction:'none',cursor:'grab'}} onPointerDown={event=>canvas?.start({kind:'move',id,label:section.title||widgetTitles[section.widget]?.()||section.widget},event)}>
            <WidgetGlyph widget={section.widget}/><span className="min-w-0 truncate">{section.title || widgetTitles[section.widget]?.() || section.widget}</span>
          </Button>
        </div>
      </CommandMenu>{node.children?.length?<ul className="ml-3 grid gap-1 border-l border-border pl-2">{node.children.map(child=>renderNode(child,next))}</ul>:null}</li>;
    }
    const kindTitle=t(node.kind === "tabs" ? "Tabs" : node.kind === "columns" ? "Columns" : node.kind === "flow" ? "Flow layout" : node.kind === "toolbar" ? "Toolbar" : node.kind === "loop" ? "Loop" : "Rows");
    const parent=Object.values(document.nodes).find(n=>n.children?.includes(id)),owner=sections.find(s=>s.id===parent?.section),contract=owner?widgetContract(owner.widget):undefined,slot=contract&&"slots" in contract?contract.slots.find(s=>s.id===node.slot):undefined;
    const layoutTitle=node.slot?t("Slot: {name}",{name:t(slot?.title??node.slot)}):node.title||Object.values(document.overlays??{}).find(o=>o.root===id)?.title;
    return <li key={id} className="min-w-0">
      <CommandMenu focusKey={`${focusScope}:${id}`} label={t("Commands for {layout}",{layout:layoutTitle||kindTitle})} commands={commandsForNode(id)}><div data-canvas-tree={id} className={cn("flex items-center rounded", container === id && "outline outline-primary bg-row-selected")}
       >
        <Button size="sm" variant="ghost" aria-label={t("Expand or collapse layout group")} aria-expanded={expanded[id] !== false} onClick={() => setExpanded((old) => ({ ...old, [id]: old[id] === false }))}>{expanded[id] === false ? <ChevronRight className="size-3" /> : <ChevronDown className="size-3" />}</Button>
        <Button size="sm" variant="ghost" aria-pressed={container === id && chosen === -2} className={cn("min-w-0 w-0 flex-1 justify-start", container === id && chosen === -2 && "bg-row-selected text-primary")} onClick={() => onContainer(id)} style={{touchAction:'none',cursor:'grab'}} onPointerDown={event=>canvas?.start({kind:'move',id,label:layoutTitle||kindTitle},event)}>
          <LayoutGlyph kind={node.kind}/><span className="truncate">{kindTitle}{layoutTitle&&` · ${layoutTitle}`}</span><span className="ml-auto text-[10px] text-muted">{node.children?.length ?? 0}</span>
        </Button>
      </div></CommandMenu>
      {expanded[id] !== false && <ul className="ml-3 grid min-w-0 gap-0.5 border-l border-border pl-2">{node.children?.map((child) => renderNode(child, next))}</ul>}
    </li>;
  };
  return <div className="grid content-start gap-3 p-3">
    <Button variant="ghost" size="sm" aria-pressed={chosen === -1} aria-label={t("Page settings")} onClick={() => onChoose(-1)}
      className={cn("justify-start border", chosen === -1 ? "border-primary bg-row-selected" : "border-border")}>
      <Settings2 className="size-3" /><span className="truncate">{t("Page settings")}</span><span aria-hidden className="ml-auto truncate text-[10px] text-muted">{title}</span>
    </Button>
    <div className="grid min-w-0 gap-2 border-t border-border pt-3">
      <div className="text-xs font-semibold text-muted">{t("Layout")}</div>
      <ul className="grid min-w-0 gap-1">{renderNode(document.root)}</ul>
      <Card role="region" aria-label={t("Unused widgets")} className="grid gap-2 p-2"><strong className="text-xs text-muted">{t("Unused widgets")}</strong><p className="text-xs text-muted">{t("Stored widgets keep their settings and do not run until placed.")}</p><ul className="grid gap-1">{document.unusedWidgets?.map(entry=>renderNode(entry.node,new Set()))}</ul></Card>
      <div className="grid gap-2">
        <strong className="text-xs text-muted">{t("Overlays")}</strong>
        {Object.entries(document.overlays ?? {}).map(([id, overlay]) => <div key={id} className="grid gap-1"><span className="truncate text-xs">{overlay.title} · {t(overlay.kind === "drawer" ? "Drawer" : "Modal")}</span><ul>{renderNode(overlay.root)}</ul></div>)}
        <Button size="sm" disabled={Object.keys(document.overlays ?? {}).length >= 16} onClick={onAddOverlay}>{t("Add overlay")}</Button>
      </div>
      <div className="flex flex-wrap gap-1">
        <Button size="sm" disabled={chosen < 0} aria-label={t("Group selection in rows")} onClick={() => onGroup("rows")}><Rows3 className="size-3" />{t("Rows")}</Button>
        <Button size="sm" disabled={chosen < 0} aria-label={t("Group selection in columns")} onClick={() => onGroup("columns")}><Columns2 className="size-3" />{t("Columns")}</Button>
        <Button size="sm" disabled={chosen < 0} onClick={() => onGroup("flow")}>{t("Flow layout")}</Button>
        <Button size="sm" disabled={chosen < 0} onClick={() => onGroup("toolbar")}>{t("Toolbar")}</Button>
        <Button size="sm" disabled={chosen < 0} onClick={() => onGroup("loop")}>{t("Loop")}</Button>
        <Button size="sm" disabled={chosen < 0} aria-label={t("Group selection in tabs")} onClick={() => onGroup("tabs")}>{t("Tabs")}</Button>
      </div>
      {sections.length === 0 && <p className="text-xs text-muted">{t("Add what people should see.")}</p>}
    </div>
  </div>;
}
