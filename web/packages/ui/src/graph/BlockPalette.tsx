import { Blocks, Search, X } from "lucide-react";
import { useEffect, useId, useRef, useState } from "react";
import { t } from "../i18n";
import type { CanvasAddContext, CanvasNode, NodeCatalog, NodeKind } from "./model";

function fits(kind: NodeKind, context: CanvasAddContext, nodes: CanvasNode[], catalog: NodeCatalog) {
  const source = context.source ?? (context.edge ? { node: context.edge.source, port: context.edge.sourcePort } : undefined);
  const target = context.target ?? (context.edge ? { node: context.edge.target, port: context.edge.targetPort } : undefined);
  const output = source && catalog.find((k) => k.id === nodes.find((n) => n.id === source.node)?.kind)?.outputs.find((p) => p.id === source.port);
  const input = target && catalog.find((k) => k.id === nodes.find((n) => n.id === target.node)?.kind)?.inputs.find((p) => p.id === target.port);
  const compatible = (out: { type: string; channel?: string }, into: { type: string; channel?: string }) => (out.type === into.type || into.channel === "data" && into.type === "json") && (out.channel ?? "control") === (into.channel ?? "control");
  return (!source || !!output && kind.inputs.some((port) => compatible(output, port))) && (!target || !!input && kind.outputs.some((port) => compatible(port, input)));
}

/** Context-sensitive discovery: an output opens only blocks that can receive it. */
export function BlockPalette({ catalog, nodes, context, onChoose, onClose }: {
  catalog: NodeCatalog; nodes: CanvasNode[]; context: CanvasAddContext; onChoose: (kind: string) => void; onClose: () => void;
}) {
  const [query, setQuery] = useState("");
  const [active, setActive] = useState(0);
  const listId = useId();
  const field = useRef<HTMLInputElement>(null);
  useEffect(() => { field.current?.focus(); }, []);
  const kinds = catalog.filter((kind) => kind.addable !== false && fits(kind, context, nodes, catalog) && [kind.title, kind.category, kind.description ?? "", kind.id].some((text) => text.toLocaleLowerCase().includes(query.toLocaleLowerCase())));
  const groups = [...new Set(kinds.map((kind) => kind.category))];
  return <div className="nodrag nopan platform-block-palette" role="dialog" aria-label={t("Add block")}
    onKeyDown={(event) => {
      if (event.key === "ArrowDown" || event.key === "ArrowUp") {
        event.preventDefault(); setActive((value) => Math.max(0, Math.min(kinds.length - 1, value + (event.key === "ArrowDown" ? 1 : -1))));
      }
    }}>
    <div className="flex items-center justify-between px-3 py-2 font-medium"><span>{t(context.edge ? "Insert block" : "Add block")}</span>
      <button type="button" onClick={onClose} aria-label={t("Close block library")} className="rounded p-1 text-muted hover:bg-row-hover"><X className="size-4" /></button>
    </div>
    <div className="relative mx-2 mb-2"><Search className="pointer-events-none absolute left-2 top-2 size-3.5 text-muted" />
      <input ref={field} className="w-full rounded border border-border bg-background py-1.5 pl-7 pr-2 text-xs outline-none focus:border-primary"
        aria-label={t("Search blocks")} placeholder={t("Search blocks")}
        role="combobox" aria-expanded aria-controls={listId} aria-activedescendant={kinds[active] ? `${listId}-${active}` : undefined}
        value={query} onChange={(event) => { setQuery(event.target.value); setActive(0); }} onKeyDown={(event) => {
          if (event.key === "Escape") { event.stopPropagation(); onClose(); }
          if (event.key === "Enter" && kinds[active]) { event.preventDefault(); onChoose(kinds[active].id); }
        }} />
    </div>
    <div id={listId} role="listbox" className="min-h-0 flex-1 overflow-auto pb-2" aria-label={t("Search blocks")}>
      {groups.map((category) => <div key={category}><div className="px-3 py-2 text-[10px] font-semibold uppercase tracking-wide text-muted">{category}</div>
        {kinds.filter((kind) => kind.category === category).map((kind) => <button key={kind.id} type="button" draggable
          id={`${listId}-${kinds.indexOf(kind)}`} role="option" aria-selected={kinds.indexOf(kind) === active}
          className="platform-block-palette-item" onClick={() => onChoose(kind.id)} title={kind.description}
          onDragStart={(event) => { event.dataTransfer.setData("application/platform-block", kind.id); event.dataTransfer.effectAllowed = "copy"; }}>
          <span className="platform-block-palette-icon">{kind.icon ?? <Blocks />}</span><span className="min-w-0 flex-1 text-left"><span className="block truncate font-medium">{kind.title}</span>
            {kind.description && <span className="mt-0.5 block text-[11px] text-muted">{kind.description}</span>}</span>
        </button>)}
      </div>)}
      {!kinds.length && <div className="px-3 py-5 text-xs text-muted">{t("No compatible blocks found.")}</div>}
    </div>
    {(context.source || context.target || context.edge) && <div className="border-t border-border px-3 py-2 text-[10px] text-muted">{t("Only blocks with compatible ports are shown.")}</div>}
  </div>;
}
