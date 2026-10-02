import {
  flexRender, getCoreRowModel, getFilteredRowModel, getSortedRowModel, useReactTable,
  type ColumnDef, type SortingState,
} from "@tanstack/react-table";
import { useVirtualizer } from "@tanstack/react-virtual";
import { ArrowDown, ArrowUp, Search } from "lucide-react";
import { useCallback, useId, useRef, useState, type ReactNode } from "react";
import { cn } from "../lib/cn";
import type { FieldType } from "../fields/types";
import { Input } from "../primitives/input";
import { t } from "../i18n";

declare module "@tanstack/react-table" {
  // eslint-disable-next-line @typescript-eslint/no-unused-vars
  interface ColumnMeta<TData, TValue> {
    /** Numbers and codes align right with tabular figures. */
    align?: "left" | "right";
    width?: number;
    /** Freeze the identity/key column while the rest scroll sideways. */
    pin?: "left";
    /** Set by columnsFor: the column's field type, which also edits cells inline. */
    field?: FieldType<any, TData>;
  }
}

export type DataTableProps<T> = {
  data: T[];
  columns: ColumnDef<T, any>[];
  getRowId: (row: T) => string;
  /** Viewport height; rows outside it are not rendered, so 100k rows stay fast. */
  height?: number | string;
  rowHeight?: number;
  onRowClick?: (row:T,modifiers?:{shift:boolean;toggle:boolean})=>void;
  selectedIds?:readonly string[];
  selectedId?: string;
  searchable?: boolean;
  toolbar?: ReactNode;
  empty?: ReactNode;
  /** Shows a loading state when rows are being fetched. */
  loading?: boolean;
  loadingText?: ReactNode;
  /** Makes cells of editable field types (columnsFor) editable in place: double-click or Enter. */
  onCellEdit?: (row: T, column: string, value: unknown) => void;
  /** Drag header edges to resize. Default on; set false to lock layout. */
  resizable?: boolean;
  /** Right-click a data row (browser menu is suppressed when provided). */
  onRowContextMenu?: (row: T) => void;
};

type Session<T> = { rowId: string; columnId: string; row: T; original: unknown; draft: unknown; coord: string };

const focusSel = (root: HTMLElement | null, sel: string, n = 3) => {
  const el = root?.querySelector<HTMLElement>(sel);
  if (el) el.focus();
  else if (n) requestAnimationFrame(() => focusSel(root, sel, n - 1));
};

/** Dense, virtualized, sortable, filterable table for any entity list. */
export function DataTable<T>({
  data, columns, getRowId, height = 480, rowHeight = 28, onRowClick, selectedId, searchable = true, toolbar, empty = t("No rows"),
  selectedIds,loading = false, loadingText, onCellEdit, resizable = true, onRowContextMenu,
}: DataTableProps<T>) {
  const editorPrefix=useId();
  const [editing, setEditing] = useState<Session<T> | null>(null);
  const session = useRef<Session<T> | null>(null);
  const [sizing, setSizing] = useState<Record<string, number>>({});
  const [sorting, setSorting] = useState<SortingState>([]);
  const [globalFilter, setGlobalFilter] = useState("");
  const scroller = useRef<HTMLDivElement>(null);
  const browse = typeof onRowClick === "function";
  const grid = !browse && typeof onCellEdit === "function";
  const rh = Number.isFinite(rowHeight) && rowHeight > 0 ? rowHeight : 28;

  // Like a spreadsheet: opening an editor selects the value, so typing replaces it.
  const selectOnOpen = useCallback((el: HTMLDivElement | null) => {
    const input = el?.querySelector<HTMLInputElement | HTMLTextAreaElement>(
      'input:not([type="checkbox"]):not([type="file"]):not([type="hidden"]), textarea',
    );
    if (!input) return;
    input.focus();
    try { input.select(); } catch { /* non-text */ }
  }, []);

  const close = (save: boolean, refocus = false) => {
    const s = session.current;
    if (!s) return;
    session.current = null;
    setEditing(null);
    if (save && !Object.is(s.original, s.draft)) onCellEdit?.(s.row, s.columnId, s.draft);
    if (refocus) focusSel(scroller.current, `[data-nav="${s.coord}"]`);
  };

  const table = useReactTable({
    data, columns, getRowId, state: { sorting, globalFilter },
    onSortingChange: setSorting, onGlobalFilterChange: setGlobalFilter,
    getCoreRowModel: getCoreRowModel(), getSortedRowModel: getSortedRowModel(), getFilteredRowModel: getFilteredRowModel(),
  });
  const rows = table.getRowModel().rows;
  const leaf = table.getVisibleLeafColumns();
  const virtualizer = useVirtualizer({
    count: rows.length, estimateSize: () => rh, overscan: 12,
    getScrollElement: () => scroller.current, getItemKey: (i) => rows[i]?.id ?? i,
    initialRect: { width: 0, height: typeof height === "number" ? height : 480 },
  });

  const widths = leaf.map((c) => sizing[c.id] ?? c.columnDef.meta?.width ?? c.columnDef.meta?.field?.width);
  const template = widths.map((w) => (w ? `${w}px` : "minmax(120px, 1fr)")).join(" ");
  // Wide entities scroll sideways instead of squeezing their columns.
  const minWidth = Math.max(320, widths.reduce<number>((n, w) => n + (w ?? 120), 0));
  let pinAcc = 0;
  const pins = leaf.map((c) => {
    if (c.columnDef.meta?.pin !== "left") return undefined;
    const left = pinAcc;
    pinAcc += sizing[c.id] ?? c.columnDef.meta?.width ?? c.columnDef.meta?.field?.width ?? 120;
    return left;
  });

  const go = (r: number, c: number) => {
    const rr = Math.max(0, Math.min(rows.length - 1, r)), cc = Math.max(0, Math.min(leaf.length - 1, c));
    virtualizer.scrollToIndex(rr);
    focusSel(scroller.current, `[data-nav="${rr}:${cc}"]`);
  };

  const hop = (r: number, c: number, dir: number) => {
    let nr = r, nc = c + dir;
    if (nc < 0 && nr > 0) { nr -= 1; nc = leaf.length - 1; }
    else if (nc >= leaf.length && nr < rows.length - 1) { nr += 1; nc = 0; }
    go(nr, nc);
  };

  const page = () => Math.max(1, Math.floor((scroller.current?.clientHeight ?? 480) / rh) - 2);
  const filterTo = (v: string) => { setGlobalFilter(v); if (scroller.current) scroller.current.scrollTop = 0; };

  return (
    <div className="flex min-w-0 flex-col gap-2">
      {(searchable || toolbar) && (
        <div className="flex items-center gap-2">
          {searchable && (
            <div className="relative w-64">
              <Search className="pointer-events-none absolute left-2 top-1/2 size-3.5 -translate-y-1/2 text-muted" />
              <Input aria-label={t("Filter rows")} placeholder={t("Filter")} value={globalFilter}
                onChange={(e) => filterTo(e.target.value)}
                onKeyDown={(e) => { if (e.key === "Escape" && globalFilter) { e.stopPropagation(); filterTo(""); } }}
                className="pl-7" />
            </div>
          )}
          <span className="text-xs text-muted tabular-nums" aria-live="polite">{t("{n} rows", { n: rows.length.toLocaleString() })}</span>
          <div className="ml-auto flex items-center gap-2">{toolbar}</div>
        </div>
      )}
      <div ref={scroller} role="table" aria-rowcount={rows.length} aria-colcount={leaf.length} aria-busy={loading || undefined}
        className="overflow-auto rounded-md border border-border bg-surface" style={{ height, scrollbarGutter: "stable" }}>
        <div role="rowgroup" className="sticky top-0 z-10 border-b border-border bg-surface" style={{ minWidth }}>
          {table.getHeaderGroups().map((group) => (
            <div role="row" key={group.id} className="grid" style={{ gridTemplateColumns: template }}>
              {group.headers.map((header, ci) => {
                const sorted = header.column.getIsSorted();
                const meta = header.column.columnDef.meta;
                const pin = pins[ci];
                return (
                  <div role="columnheader" key={header.id}
                    aria-sort={sorted === "asc" ? "ascending" : sorted === "desc" ? "descending" : "none"}
                    className={cn("relative flex h-7 items-center gap-1 truncate px-2 text-xs font-medium uppercase tracking-wide text-muted",
                      (meta?.align ?? meta?.field?.align) === "right" && "justify-end text-right", pin != null && "z-[2] bg-surface")}
                    style={pin != null ? { position: "sticky", left: pin } : undefined}>
                    {header.column.getCanSort() ? (
                      <button type="button" className="flex min-w-0 items-center gap-1 hover:text-foreground"
                        onClick={header.column.getToggleSortingHandler()}>
                        <span className="truncate">{flexRender(header.column.columnDef.header, header.getContext())}</span>
                        {sorted === "asc" ? <ArrowUp className="size-3 shrink-0" /> : sorted === "desc" ? <ArrowDown className="size-3 shrink-0" /> : null}
                      </button>
                    ) : <span className="truncate">{flexRender(header.column.columnDef.header, header.getContext())}</span>}
                    {resizable && (
                      <span aria-hidden className="absolute inset-y-0 -right-px z-10 w-1.5 cursor-col-resize hover:bg-ring"
                        onDoubleClick={() => setSizing(({ [header.column.id]: _, ...rest }) => rest)}
                        onPointerDown={(e) => {
                          if (e.button) return;
                          e.preventDefault(); e.stopPropagation();
                          const id = header.column.id, x0 = e.clientX, w0 = sizing[id] ?? meta?.width ?? meta?.field?.width ?? 120;
                          const ac = new AbortController();
                          window.addEventListener("pointermove", (ev) => setSizing((s) => ({ ...s, [id]: Math.max(48, w0 + ev.clientX - x0) })), { signal: ac.signal });
                          window.addEventListener("pointerup", () => ac.abort(), { signal: ac.signal, once: true });
                        }} />
                    )}
                  </div>
                );
              })}
            </div>
          ))}
        </div>
        {loading && rows.length === 0 ? (
          <div role="status" className="p-6 text-center text-sm text-muted">{loadingText ?? t("Loading…")}</div>
        ) : rows.length === 0 ? (
          <div className="p-6 text-center text-sm text-muted">{empty}</div>
        ) : (
          <div role="rowgroup" className="relative" style={{ height: virtualizer.getTotalSize(), minWidth }}>
            {virtualizer.getVirtualItems().map((item) => {
              const row = rows[item.index]!;
              const selected = selectedIds?.includes(row.id)??(selectedId != null && row.id === selectedId);
              return (
                <div role="row" key={row.id} data-row={item.index} aria-rowindex={item.index + 1} aria-selected={selected} tabIndex={browse ? 0 : undefined}
                  onClick={browse ? (e) => { if (!window.getSelection()?.toString()) onRowClick?.(row.original,{shift:e.shiftKey,toggle:e.ctrlKey||e.metaKey}); } : undefined}
                  onContextMenu={onRowContextMenu ? (e) => { e.preventDefault(); onRowContextMenu(row.original); } : undefined}
                  onKeyDown={browse ? (e) => {
                    if (e.target !== e.currentTarget || e.nativeEvent.isComposing) return;
                    if (e.key === "Enter" || e.key === " ") { e.preventDefault(); onRowClick?.(row.original,{shift:e.shiftKey,toggle:e.ctrlKey||e.metaKey||e.key===" "}); }
                    else if (e.key === "ArrowDown" || e.key === "ArrowUp") {
                      e.preventDefault();
                      const r = Math.max(0, Math.min(rows.length - 1, item.index + (e.key === "ArrowDown" ? 1 : -1)));
                      virtualizer.scrollToIndex(r);
                      focusSel(scroller.current, `[data-row="${r}"]`);
                    }
                  } : undefined}
                  className={cn("group absolute inset-x-0 top-0 grid items-center border-b border-border/60 text-sm hover:bg-row-hover",
                    browse && "cursor-pointer outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-ring", selected && "bg-row-selected")}
                  style={{ gridTemplateColumns: template, height: rh, transform: `translateY(${item.start}px)` }}>
                  {row.getVisibleCells().map((cell, ci) => {
                    const field = cell.column.columnDef.meta?.field;
                    const editable = grid && !!field?.editor && !field.readOnly;
                    const isEditing = editing?.rowId === row.id && editing.columnId === cell.column.id;
                    const pin = pins[ci];
                    const raw = cell.getValue();
                    const coord = `${item.index}:${ci}`;
                    const begin = (draft: unknown = raw) => {
                      const next = { rowId: row.id, columnId: cell.column.id, row: row.original, original: raw, draft, coord };
                      session.current = next;
                      setEditing(next);
                    };
                    return (
                      <div role="cell" key={cell.id} data-nav={coord} tabIndex={grid ? 0 : undefined}
                        title={!isEditing && (typeof raw === "string" || typeof raw === "number") ? String(raw) : undefined}
                        onClick={grid ? (e) => { e.stopPropagation(); e.currentTarget.focus(); } : undefined}
                        onDoubleClick={editable ? (e) => { e.stopPropagation(); begin(); } : undefined}
                        onKeyDown={grid && !isEditing ? (e) => {
                          if (e.nativeEvent.isComposing) return;
                          if ((e.key === "Enter" || e.key === "F2") && editable) { e.preventDefault(); begin(); return; }
                          if (editable && e.key.length === 1 && !e.ctrlKey && !e.metaKey && !e.altKey) { e.preventDefault(); begin(e.key); return; }
                          if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === "c") {
                            if (!window.getSelection()?.toString() && (typeof raw === "string" || typeof raw === "number" || typeof raw === "boolean")) {
                              void navigator.clipboard?.writeText(String(raw));
                            }
                            return;
                          }
                          if (e.key === "Tab") { e.preventDefault(); hop(item.index, ci, e.shiftKey ? -1 : 1); return; }
                          const step: Record<string, [number, number]> = {
                            ArrowLeft: [0, -1], ArrowRight: [0, 1], ArrowUp: [-1, 0], ArrowDown: [1, 0],
                            Home: [0, -ci], End: [0, leaf.length - 1 - ci], PageUp: [-page(), 0], PageDown: [page(), 0],
                          };
                          const d = step[e.key];
                          if (d) { e.preventDefault(); go(item.index + d[0], ci + d[1]); }
                        } : undefined}
                        className={cn("truncate px-2", (cell.column.columnDef.meta?.align ?? field?.align) === "right" && "text-right tabular-nums",
                          grid && "outline-none focus:z-10 focus:ring-1 focus:ring-inset focus:ring-ring", isEditing && "overflow-visible",
                          pin != null && "z-[1] bg-surface group-hover:bg-row-hover", pin != null && selected && "bg-row-selected")}
                        style={pin != null ? { position: "sticky", left: pin } : undefined}>
                        {isEditing && editing && field?.editor ? (
                          <div ref={selectOnOpen} onClick={(e) => e.stopPropagation()}
                            className="relative z-20 min-w-full rounded-md bg-surface p-0.5 shadow-lg ring-1 ring-border"
                            onKeyDown={(e) => {
                              if (e.nativeEvent.isComposing) return;
                              if (e.key === "Tab") { e.preventDefault(); e.stopPropagation(); close(true); hop(item.index, ci, e.shiftKey ? -1 : 1); }
                              else if (e.key === "Enter") { e.preventDefault(); e.stopPropagation(); close(true, true); }
                              else if (e.key === "Escape") { e.preventDefault(); e.stopPropagation(); close(false, true); }
                            }}
                            onBlur={(e) => { if (!e.currentTarget.contains(e.relatedTarget as Node | null)) close(true); }}>
                            <label className="sr-only" htmlFor={`${editorPrefix}-${row.id}-${cell.column.id}`}>{t("Edit {field} for {record}",{field:field.label,record:row.id})}</label>
                            {field.editor({
                              id:`${editorPrefix}-${row.id}-${cell.column.id}`,
                              value: editing.draft,
                              onChange: (draft) => { const s = session.current; if (!s) return; const next = { ...s, draft }; session.current = next; setEditing(next); },
                              autoFocus: true,
                            })}
                          </div>
                        ) : flexRender(cell.column.columnDef.cell, cell.getContext())}
                      </div>
                    );
                  })}
                </div>
              );
            })}
          </div>
        )}
      </div>
    </div>
  );
}
