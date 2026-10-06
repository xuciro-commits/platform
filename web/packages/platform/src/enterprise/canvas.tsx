// The drawing surface: elements as draggable cards, relationships as lines,
// a link mode that connects two elements. Pure presentation; the modeler owns
// the positions and the decisions.
import { useRef, useState } from "react";
import { cn } from "@platform/ui";
import type { Element, Relationship } from "./model";

export type Positions = Record<string, [number, number]>;
const W = 140, H = 48;

export function Canvas({ elements, relationships, positions, selected, linking, readOnly = false, onMove, onSelect, onDrop, onLink, label }: {
  elements: Element[]; relationships: Relationship[]; positions: Positions; selected?: string; linking: boolean; readOnly?: boolean;
  onMove: (id: string, at: [number, number]) => void; onSelect: (id?: string) => void;
  onDrop: (stereotype: string, at: [number, number]) => void; onLink: (source: string, target: string) => void;
  label: (r: Relationship) => string;
}) {
  const svg = useRef<SVGSVGElement>(null);
  const [drag, setDrag] = useState<{ id: string; dx: number; dy: number }>();
  const [from, setFrom] = useState<string>();
  const point = (e: { clientX: number; clientY: number }): [number, number] => {
    const r = svg.current!.getBoundingClientRect();
    return [e.clientX - r.left + svg.current!.parentElement!.scrollLeft, e.clientY - r.top + svg.current!.parentElement!.scrollTop];
  };
  const extent = Object.values(positions).reduce(([w, h], [x, y]) => [Math.max(w, x + W + 80), Math.max(h, y + H + 80)], [800, 500]);
  const centre = (id: string) => { const p = positions[id] ?? [0, 0]; return [p[0] + W / 2, p[1] + H / 2] as const; };
  return <div className="relative h-full min-h-[480px] overflow-auto bg-[radial-gradient(var(--color-border)_1px,transparent_1px)] [background-size:20px_20px]"
    onDragOver={(e) => { if (!readOnly && e.dataTransfer.types.includes("application/x-uaf-stereotype")) e.preventDefault(); }}
    onDrop={(e) => { if (readOnly) return; const st = e.dataTransfer.getData("application/x-uaf-stereotype"); if (!st) return; e.preventDefault(); const [x, y] = point(e); onDrop(st, [x - W / 2, y - H / 2]); }}>
    <svg ref={svg} width={extent[0]} height={extent[1]} className="block select-none" onPointerDown={(e) => { if (e.target === svg.current) { onSelect(undefined); setFrom(undefined); } }}
      onPointerMove={(e) => { if (!drag) return; const [x, y] = point(e); onMove(drag.id, [Math.max(0, x - drag.dx), Math.max(0, y - drag.dy)]); }}
      onPointerUp={() => setDrag(undefined)}>
      <defs><marker id="uaf-arrow" viewBox="0 0 10 10" refX="10" refY="5" markerWidth="8" markerHeight="8" orient="auto-start-reverse"><path d="M0,0 L10,5 L0,10 z" className="fill-muted" /></marker></defs>
      {relationships.map((r) => {
        if (!positions[r.source] || !positions[r.target]) return null;
        const [x1, y1] = centre(r.source), [x2, y2] = centre(r.target);
        return <g key={r.id} className="text-muted">
          <line x1={x1} y1={y1} x2={x2} y2={y2} className="stroke-muted" strokeWidth={1.2} markerEnd="url(#uaf-arrow)" strokeDasharray={r.until ? "4 3" : undefined} />
          <text x={(x1 + x2) / 2} y={(y1 + y2) / 2 - 4} textAnchor="middle" className="fill-muted text-[10px]">{label(r)}</text>
        </g>;
      })}
      {elements.map((el) => {
        const p = positions[el.id]; if (!p) return null;
        const sel = selected === el.id, src = from === el.id;
        return <g key={el.id} transform={`translate(${p[0]},${p[1]})`} className={cn(readOnly ? "cursor-pointer" : "cursor-grab", drag?.id === el.id && "cursor-grabbing")}
          onPointerDown={(e) => { e.stopPropagation(); if (readOnly) { onSelect(el.id); return; } if (linking) { if (!from) setFrom(el.id); else if (from !== el.id) { onLink(from, el.id); setFrom(undefined); } return; }
            const [x, y] = point(e); setDrag({ id: el.id, dx: x - p[0], dy: y - p[1] }); onSelect(el.id); }}>
          <rect width={W} height={H} rx={6} className={cn("fill-surface stroke-border", sel && "stroke-primary", src && "stroke-[var(--tone-warning)]", el.until && "opacity-60")} strokeWidth={sel || src ? 2 : 1} />
          <text x={8} y={18} className="fill-muted text-[9px] uppercase tracking-wide">{el.kind || el.stereotype.replace(/^Actual/, "")}</text>
          <text x={8} y={36} className="fill-foreground text-[12px] font-medium"><title>{el.name}</title>{el.shortName || (el.name.length > 20 ? el.name.slice(0, 19) + "…" : el.name)}</text>
          {el.legal && <circle cx={W - 10} cy={10} r={3} className="fill-primary" />}
        </g>;
      })}
    </svg>
  </div>;
}
