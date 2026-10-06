import { Button, Disclosure, Input, t, useCanvasGesture } from "@platform/ui";
import { widgetContract } from "@platform/app";
import { useState } from "react";
import { WidgetGlyph } from "./WidgetGlyph";

/** The widget library (ADR-0053 §5.3): every registered widget by category;
 * drag one onto the canvas or click to add it to the selected container. */
export function WidgetLibrary({ widgets, widgetTitles, onAdd }: { widgets: readonly string[]; widgetTitles: Record<string, () => string>; onAdd: (widget: string) => void }) {
  const [search, setSearch] = useState("");
  const canvas = useCanvasGesture();
  return <div className="grid content-start gap-2 p-2">
    <Input type="search" aria-label={t("Search widgets")} placeholder={t("Search widgets")} value={search} onChange={(event) => setSearch(event.target.value)} />
    <p className="px-1 text-[11px] text-muted">{t("Drag a widget onto the canvas, or click to add it to the selected layout.")}</p>
    {[...new Set(widgets.map((id) => widgetContract(id)?.category ?? "Content"))].map((category) => {
      const matches = widgets.filter((widget) => (widgetContract(widget)?.category ?? "Content") === category && `${widgetTitles[widget]?.() ?? widget} ${category}`.toLowerCase().includes(search.toLowerCase()));
      return matches.length ? <Disclosure key={`${category}:${!!search}`} defaultOpen={!!search || category === "Content"} className="grid gap-1" summary={<span className="py-1 text-[11px] font-semibold text-muted">{t(category)} · {matches.length}</span>}>
        <div className="grid grid-cols-2 gap-1">{matches.map((widget) => <Button key={widget} size="sm" variant="ghost" className="justify-start truncate border border-border" title={widgetTitles[widget]?.() || widget}
          onClick={() => onAdd(widget)} style={{ touchAction: "none", cursor: "grab" }} onPointerDown={(event) => canvas?.start({ kind: "new", type: widget, label: widgetTitles[widget]?.() || widget }, event)}>
          <span className="text-primary"><WidgetGlyph widget={widget} /></span><span className="truncate">{widgetTitles[widget]?.() || widget}</span></Button>)}</div>
      </Disclosure> : null;
    })}
  </div>;
}
