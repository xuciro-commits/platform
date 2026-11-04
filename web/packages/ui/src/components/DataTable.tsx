import {
  flexRender, getCoreRowModel, getFilteredRowModel, getSortedRowModel, useReactTable,
  type ColumnDef,
} from "@tanstack/react-table";
import { useVirtualizer } from "@tanstack/react-virtual";
import { ArrowDown, ArrowUp, Search, Calendar, CalendarClock, CircleDot, CheckSquare, Square, Columns3, Link2, Tags, Hash, Banknote, AlignLeft, Braces, Rows3, Star } from "lucide-react";
import { useCallback,useEffect, useId, useLayoutEffect, useRef, useState, type ComponentProps, type ReactNode } from "react";
import { cn } from "../lib/cn";
import type { FieldType } from "../fields/types";
import { Input } from "../primitives/input";
import { Button } from "../primitives/button";
import { EmptyState } from "./EmptyState";
import { ActionMenu } from "./ActionMenu";
import { readTablePreferences } from "./table-preferences";
import { t } from "../i18n";

declare module "@tanstack/react-table" {
  interface ColumnMeta<TData, TValue> {
    align?: "left" | "right";
    width?: number;
    pin?: "left";
    field?: FieldType<any, TData>;
    querySort?: { direction?: "asc" | "desc"; onChange: (next: "asc" | "desc") => void };
  }
}

export type DataTableProps<T> = {
  storageKey?: string;
  data: T[];
  columns: ColumnDef<T, any>[];
  getRowId: (row: T) => string;
  height?: number | string;
  rowHeight?: number;
  onRowClick?: (row:T,modifiers?:{shift:boolean;toggle:boolean})=>void;
  selectedIds?:readonly string[];
  selectedId?: string;
  searchable?: boolean;
  toolbar?: ReactNode;
  empty?: ReactNode | Omit<ComponentProps<typeof EmptyState>, "className">;
  loading?: boolean;
  loadingText?: ReactNode;
  onCellEdit?: (row: T, column: string, value: unknown) => void;
	cellAccess?: (row:T,column:string)=>{allow:boolean;reason:string}|undefined;
  resizable?: boolean;
  virtualColumns?: boolean;
  totalRows?: number;
  manualSorting?: boolean;
  columnChooser?: boolean;
  onRowContextMenu?: (row: T) => void;
  onPaste?: (matrix: string[][], start: { row: number; col: number }) => void;
};

type Session<T> = { rowId: string; columnId: string; row: T; original: unknown; draft: unknown; coord: string };

const focusSel = (root: HTMLElement | null, sel: string, n = 3) => {
  const el = root?.querySelector<HTMLElement>(sel);
  if (el) el.focus();
  else if (n) requestAnimationFrame(() => focusSel(root, sel, n - 1));
};

const headerTypeIcons: Record<string, typeof Calendar> = {
  date: Calendar, datetime: CalendarClock, choice: CircleDot, boolean: CheckSquare,
  reference: Link2, references: Link2, tags: Tags, integer: Hash, decimal: Hash,
  money: Banknote, longtext: AlignLeft, json: Braces, lines: Rows3, rating: Star,
};
function headerTypeIcon(type?: string) {
  const Icon = type ? headerTypeIcons[type] : undefined;
  return Icon ? <Icon className="size-3 shrink-0 text-muted" aria-hidden /> : null;
}

export function DataTable<T>({
  storageKey, data, columns, getRowId, height = 480, rowHeight, onRowClick, selectedId, searchable = true, toolbar, empty = t("No rows"),
  selectedIds,loading = false, loadingText, onCellEdit, cellAccess,resizable = true, virtualColumns = false, totalRows, manualSorting = false, columnChooser = true, onRowContextMenu, onPaste,
}: DataTableProps<T>) {
  const editorPrefix=useId();
  const [editing, setEditing] = useState<Session<T> | null>(null);
  const session = useRef<Session<T> | null>(null);
  const [preferences, setPreferences] = useState(() => ({ storageKey, ...readTablePreferences(storageKey) }));
  const [globalFilter, setGlobalFilter] = useState("");
  if (preferences.storageKey !== storageKey) {
    setPreferences({ storageKey, ...readTablePreferences(storageKey) });
    setGlobalFilter("");
  }
  const { sizing } = preferences;
  const setSizing = (update: (current: Record<string, number>) => Record<string, number>) => setPreferences(current => ({ ...current, sizing: update(current.sizing) }));
  useEffect(() => {
    if (!storageKey) return;
    const { storageKey: _, ...saved } = preferences;
    try { localStorage.setItem(`table:${storageKey}`, JSON.stringify(saved)); } catch {   }
  }, [preferences, storageKey]);
  const scroller = useRef<HTMLDivElement>(null);
  const resizeDrag = useRef<{ id: string; at: number; width: number } | undefined>(undefined);
  const browse = typeof onRowClick === "function";
  const grid = !browse && typeof onCellEdit === "function";
  const [tokenRowHeight,setTokenRowHeight]=useState(28);
  const explicitRowHeight=typeof rowHeight==="number"&&Number.isFinite(rowHeight)&&rowHeight>0;
  const rh = explicitRowHeight ? rowHeight : tokenRowHeight;
  useLayoutEffect(()=>{
    if(explicitRowHeight||!scroller.current)return;
    const read=()=>setTokenRowHeight(Number.parseFloat(getComputedStyle(scroller.current!).getPropertyValue("--table-row-h"))||28);
    read();
    const observer=new MutationObserver(read);
    for(let element:HTMLElement|null=scroller.current;element;element=element.parentElement)observer.observe(element,{attributes:true,attributeFilter:["data-density","style","class"]});
    return ()=>observer.disconnect();
  },[explicitRowHeight]);

  const selectOnOpen = useCallback((el: HTMLDivElement | null) => {
    const input = el?.querySelector<HTMLInputElement | HTMLTextAreaElement>(
      'input:not([type="checkbox"]):not([type="file"]):not([type="hidden"]), textarea',
    );
    if (!input) return;
    input.focus();
    try { input.select(); } catch {   }
  }, []);

  const close = (save: boolean, refocus = false) => {
    const s = session.current;
    if (!s) return;
    session.current = null;
    setEditing(null);
    if (save && !Object.is(s.original, s.draft)) onCellEdit?.(s.row, s.columnId, s.draft);
    if (refocus) focusSel(scroller.current, `[data-nav="${s.coord}"]`);
  };

  const hasQuerySort = (definitions: ColumnDef<T, any>[]): boolean => definitions.some(column => !!column.meta?.querySort || "columns" in column && hasQuerySort(column.columns ?? []));
  const querySorting = manualSorting || hasQuerySort(columns);
  const table = useReactTable({
    data, columns, getRowId, state: { sorting: querySorting ? [] : preferences.sorting, globalFilter, columnVisibility: preferences.visibility },
    manualSorting: querySorting,
    onSortingChange: update => setPreferences(current => ({ ...current, sorting: typeof update === "function" ? update(current.sorting) : update })),
    onColumnVisibilityChange: update => setPreferences(current => ({ ...current, visibility: typeof update === "function" ? update(current.visibility) : update })),
    onGlobalFilterChange: setGlobalFilter,
    getCoreRowModel: getCoreRowModel(), getSortedRowModel: getSortedRowModel(), getFilteredRowModel: getFilteredRowModel(),
  });
  const rows = table.getRowModel().rows;
  const leaf = table.getVisibleLeafColumns();
  const virtualizer = useVirtualizer({
    count: rows.length, estimateSize: () => rh, overscan: 12,
    getScrollElement: () => scroller.current, getItemKey: (i) => rows[i]?.id ?? i,
    initialRect: { width: 0, height: typeof height === "number" ? height : 480 },
  });
  useEffect(()=>virtualizer.measure(),[rh]);

  const widths = leaf.map((c) => sizing[c.id] ?? c.columnDef.meta?.width ?? c.columnDef.meta?.field?.width);
  const horizontal = virtualColumns && rows.length > 0 && table.getHeaderGroups().length === 1;
  const columnVirtualizer = useVirtualizer({
    horizontal:true, enabled:horizontal, count:leaf.length, overscan:2,
    estimateSize:i=>widths[i]??120, getScrollElement:()=>scroller.current,
    getItemKey:i=>leaf[i]?.id??i, initialRect:{width:640,height:0},
  });
  const widthKey=JSON.stringify(widths);
  useEffect(()=>{if(horizontal)columnVirtualizer.measure();},[horizontal,widthKey]);
  const mountedColumns=horizontal?new Set([...columnVirtualizer.getVirtualItems().map(i=>i.index),...leaf.flatMap((c,i)=>c.columnDef.meta?.pin==="left"?[i]:[])]):undefined;
  const template = widths.map((w) => rows.length === 0 ? "minmax(0, 1fr)" : horizontal ? `${w??120}px` : w ? `${w}px` : "minmax(120px, 1fr)").join(" ");
  const minWidth = rows.length === 0 ? 0 : widths.reduce<number>((n, w) => n + (w ?? 120), 0);
  let pinAcc = 0;
  const pins = leaf.map((c) => {
    if (rows.length === 0 || c.columnDef.meta?.pin !== "left") return undefined;
    const left = pinAcc;
    pinAcc += sizing[c.id] ?? c.columnDef.meta?.width ?? c.columnDef.meta?.field?.width ?? 120;
    return left;
  });

  const go = (r: number, c: number) => {
    const rr = Math.max(0, Math.min(rows.length - 1, r)), cc = Math.max(0, Math.min(leaf.length - 1, c));
    virtualizer.scrollToIndex(rr);
    if(horizontal&&pins[cc]==null)columnVirtualizer.scrollToIndex(cc);
    focusSel(scroller.current, `[data-nav="${rr}:${cc}"]`);
  };

  const hop = (r: number, c: number, dir: number) => {
    let nr = r, nc = c + dir;
    if (nc < 0 && nr > 0) { nr -= 1; nc = leaf.length - 1; }
    else if (nc >= leaf.length && nr < rows.length - 1) { nr += 1; nc = 0; }
    go(nr, nc);
  };
  const canHop = (r: number, c: number, backwards: boolean) => backwards ? r > 0 || c > 0 : r < rows.length - 1 || c < leaf.length - 1;
  const headerRows = table.getHeaderGroups().length;

  const page = () => Math.max(1, Math.floor((scroller.current?.clientHeight ?? 480) / rh) - 2);
  const filterTo = (v: string) => { setGlobalFilter(v); if (scroller.current) scroller.current.scrollTop = 0; };
  const hideable = columnChooser ? table.getAllLeafColumns().filter(column => column.getCanHide()) : [];
  const chooseColumns = hideable.length > 0 && table.getAllLeafColumns().length > 1;
  const emptyState = typeof empty === "object" && empty !== null && "title" in empty ? empty : { title: empty };

  return (
    <div className="flex min-w-0 flex-col gap-2">
      {(searchable || toolbar || chooseColumns || totalRows !== undefined) && (
        <div className="flex min-w-0 flex-wrap items-center gap-2">
          {searchable && (
            <div className="relative min-w-0 max-w-full basis-64">
              <Search className="pointer-events-none absolute left-2 top-1/2 size-3.5 -translate-y-1/2 text-muted" />
              <Input aria-label={t("Filter rows")} placeholder={t("Filter")} value={globalFilter}
                onChange={(e) => filterTo(e.target.value)}
                onKeyDown={(e) => { if (e.key === "Escape" && globalFilter) { e.stopPropagation(); filterTo(""); } }}
                className="pl-7" />
            </div>
          )}
          <span className="shrink-0 text-xs text-muted tabular-nums" aria-live="polite">{totalRows === undefined ? t("{n} rows", { n: rows.length.toLocaleString() }) : t("{shown} of {total} rows", { shown: rows.length.toLocaleString(), total: totalRows.toLocaleString() })}</span>
          {(toolbar || chooseColumns) && <div className="ml-auto flex min-w-0 flex-wrap items-center gap-2">
            {toolbar}
            {chooseColumns && <ActionMenu label={t("Columns")} icon={<Columns3 aria-hidden />} commands={hideable.map(column => ({
              id: column.id, label: typeof column.columnDef.header === "string" ? column.columnDef.header : column.columnDef.meta?.field?.label ?? column.id,
              icon: column.getIsVisible() ? <CheckSquare aria-hidden /> : <Square aria-hidden />,
              disabled: column.getIsVisible() && leaf.length === 1,
              run: () => column.toggleVisibility(),
            }))} />}
          </div>}
        </div>
      )}
      <div ref={scroller} role="table" aria-rowcount={(totalRows ?? rows.length) + headerRows} aria-colcount={leaf.length} aria-busy={loading || undefined}
        className="overflow-auto rounded-md border border-border bg-surface" style={{ height, scrollbarGutter: "stable" }}
        onPaste={onPaste ? (e) => {
          const text = (e.clipboardData || (window as any).clipboardData).getData("text");
          if (!text.trim()) return;
          let startRow = 0, startCol = 0;
          if (editing) {
            startRow = editing.rowId === "default" ? Math.max(0, rows.length - 1) : Math.max(0, Number(editing.rowId) || 0);
            startCol = leaf.findIndex(c => c.id === editing.columnId);
            if (startCol < 0) startCol = 0;
          }
          const matrix = text.split("\n").filter(line => line.length > 0).map(line => line.split("\t"));
          onPaste(matrix, { row: startRow, col: startCol });
        } : undefined}>
        <div role="rowgroup" className={cn("sticky top-0 z-10 border-b border-border bg-surface", rows.length === 0 && "overflow-hidden")} style={{ minWidth }}>
          {table.getHeaderGroups().map((group, index) => { let columnIndex = 0; return (
            <div role="row" aria-rowindex={index + 1} key={group.id} className="grid" style={{ gridTemplateColumns: template }}>
              {group.headers.map((header) => {
                const ci = columnIndex; columnIndex += header.colSpan;
                if(mountedColumns&&!mountedColumns.has(ci))return null;
                const meta = header.column.columnDef.meta;
                const sorted = meta?.querySort ? meta.querySort.direction : header.column.getIsSorted();
                const pin = pins[ci];
                return (
                  <div role="columnheader" key={header.id}
                    aria-colindex={ci+1}
                    aria-colspan={header.colSpan > 1 ? header.colSpan : undefined}
                    aria-sort={sorted === "asc" ? "ascending" : sorted === "desc" ? "descending" : "none"}
                    className={cn("relative flex min-h-7 min-w-0 items-center gap-1 px-2 py-1 text-xs font-medium uppercase tracking-wide text-muted",
                      (meta?.align ?? meta?.field?.align) === "right" && "justify-end text-right", pin != null && "z-[2] bg-surface")}
                    style={{gridColumn:`${ci+1} / span ${header.colSpan}`,...(pin != null ? { position: "sticky", left: pin } : {})}}>
                    {!header.isPlaceholder && (!querySorting && header.column.getCanSort() || meta?.querySort ? (
                      <button type="button" className="flex min-w-0 items-center gap-1 hover:text-foreground"
                        onClick={(event) => meta?.querySort ? meta.querySort.onChange(sorted === "asc" ? "desc" : "asc") : header.column.getToggleSortingHandler()?.(event)}>
                        {headerTypeIcon(meta?.field?.type)}<span className="truncate">{flexRender(header.column.columnDef.header, header.getContext())}</span>
                        {sorted === "asc" ? <ArrowUp className="size-3 shrink-0" /> : sorted === "desc" ? <ArrowDown className="size-3 shrink-0" /> : null}
                      </button>
                    ) : <div className="min-w-0 flex-1 truncate">{flexRender(header.column.columnDef.header, header.getContext())}</div>)}
                    {resizable && !header.isPlaceholder && !header.column.columns.length && (
                      <span role="separator" tabIndex={0} aria-orientation="vertical" aria-valuemin={48}
                        aria-valuenow={Math.round(sizing[header.column.id] ?? meta?.width ?? meta?.field?.width ?? 120)}
                        aria-label={t("Resize column {column}", { column: typeof header.column.columnDef.header === "string" ? header.column.columnDef.header : meta?.field?.label ?? header.column.id })}
                        className="absolute inset-y-0 right-0 z-10 w-1.5 cursor-col-resize touch-none hover:bg-ring focus-visible:bg-ring focus-visible:outline-none"
                        onDoubleClick={() => setSizing(({ [header.column.id]: _, ...rest }) => rest)}
                        onPointerDown={(e) => {
                          if (e.button) return;
                          e.preventDefault(); e.stopPropagation();
                          e.currentTarget.setPointerCapture(e.pointerId);
                          resizeDrag.current = { id: header.column.id, at: e.clientX, width: e.currentTarget.parentElement!.getBoundingClientRect().width };
                        }}
                        onPointerMove={event => {
                          const drag = resizeDrag.current;
                          if (drag?.id === header.column.id) setSizing(current => ({ ...current, [drag.id]: Math.max(48, drag.width + event.clientX - drag.at) }));
                        }}
                        onPointerUp={event => { if (event.currentTarget.hasPointerCapture(event.pointerId)) event.currentTarget.releasePointerCapture(event.pointerId); resizeDrag.current = undefined; }}
                        onPointerCancel={() => { resizeDrag.current = undefined; }} onLostPointerCapture={() => { resizeDrag.current = undefined; }}
                        onKeyDown={event => {
                          if (event.key === "Enter") { event.preventDefault(); event.stopPropagation(); setSizing(({ [header.column.id]: _, ...rest }) => rest); return; }
                          if (!["ArrowLeft", "ArrowRight", "Home"].includes(event.key)) return;
                          event.preventDefault(); event.stopPropagation();
                          const width = event.currentTarget.parentElement!.getBoundingClientRect().width;
                          setSizing(current => ({ ...current, [header.column.id]: event.key === "Home" ? 48 : Math.max(48, width + (event.key === "ArrowRight" ? 16 : -16)) }));
                        }} />
                    )}
                  </div>
                );
              })}
            </div>
          );})}
        </div>
        {loading && rows.length === 0 ? (
          <div role="status" className="grid gap-2.5 p-6">
            <span className="sr-only">{loadingText ?? t("Loading…")}</span>
            {[0, 1, 2, 3, 4].map((i) => <div key={i} className="platform-skeleton h-5" style={{ width: `${88 - i * 15}%`, animationDelay: `${i * 90}ms` }} />)}
          </div>
        ) : rows.length === 0 ? (
          <EmptyState {...(globalFilter ? {
            title: t("No matching rows"), hint: t("Try another search or clear the current filter."), icon: <Search aria-hidden className="size-5" />,
            action: <Button size="sm" onClick={() => filterTo("")}>{t("Clear search")}</Button>,
          } : emptyState)} className="p-6" />
        ) : (
          <div role="rowgroup" className="relative" style={{ height: virtualizer.getTotalSize(), minWidth }}>
            {virtualizer.getVirtualItems().map((item) => {
              const row = rows[item.index]!;
              const selected = selectedIds?.includes(row.id)??(selectedId != null && row.id === selectedId);
              return (
                <div role="row" key={row.id} data-row={item.index} aria-rowindex={item.index + headerRows + 1} aria-selected={selected} tabIndex={browse ? 0 : undefined}
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
                    if(mountedColumns&&!mountedColumns.has(ci)&&editing?.columnId!==cell.column.id)return null;
                    const field = cell.column.columnDef.meta?.field;
                    const access=cellAccess?.(row.original,cell.column.id);
                    const editable = grid && !!field?.editor && !field.readOnly && access?.allow!==false;
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
                        aria-colindex={ci+1}
                        aria-disabled={grid&&access?.allow===false?true:undefined}
                        title={access?.allow===false?t(access.reason):!isEditing && (typeof raw === "string" || typeof raw === "number") ? String(raw) : undefined}
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
                          if (e.key === "Tab") {
                            if (canHop(item.index, ci, e.shiftKey)) { e.preventDefault(); hop(item.index, ci, e.shiftKey ? -1 : 1); }
                            return;
                          }
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
                        style={{...(horizontal?{gridColumn:ci+1}:{}),...(pin != null ? { position: "sticky", left: pin } : {})}}>
                        {isEditing && editing && field?.editor ? (
                          <div ref={selectOnOpen} onClick={(e) => e.stopPropagation()}
                            className="relative z-20 min-w-full rounded-md bg-surface p-0.5 shadow-lg ring-1 ring-border"
                            onKeyDown={(e) => {
                              if (e.nativeEvent.isComposing) return;
                              if (e.key === "Tab") {
                                e.stopPropagation();
                                if (canHop(item.index, ci, e.shiftKey)) { e.preventDefault(); close(true); hop(item.index, ci, e.shiftKey ? -1 : 1); }
                                else { e.currentTarget.closest<HTMLElement>("[data-nav]")?.focus(); close(true); }
                              }
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
