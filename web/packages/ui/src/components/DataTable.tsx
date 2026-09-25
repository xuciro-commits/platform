import {
  flexRender, getCoreRowModel, getFilteredRowModel, getSortedRowModel, useReactTable,
  type ColumnDef, type SortingState,
} from "@tanstack/react-table";
import { useVirtualizer } from "@tanstack/react-virtual";
import { ArrowDown, ArrowUp, Search } from "lucide-react";
import { useCallback, useRef, useState, type ReactNode } from "react";
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
  onRowClick?: (row: T) => void;
  selectedId?: string;
  searchable?: boolean;
  toolbar?: ReactNode;
  empty?: ReactNode;
  /** Makes cells of editable field types (columnsFor) editable in place: double-click or Enter. */
  onCellEdit?: (row: T, column: string, value: unknown) => void;
};

/** Dense, virtualized, sortable, filterable table for any entity list. */
export function DataTable<T>({
  data, columns, getRowId, height = 480, rowHeight = 28, onRowClick, selectedId, searchable = true, toolbar, empty = t("No rows"), onCellEdit,
}: DataTableProps<T>) {
  const [editing, setEditing] = useState<{ row: string; column: string; draft: unknown }>();
  // Like a spreadsheet: opening an editor selects the value, so typing replaces it.
  const selectOnOpen = useCallback((el: HTMLDivElement | null) => el?.querySelector<HTMLInputElement>("input:not([type=checkbox]):not([type=file])")?.select(), []);
  const [sorting, setSorting] = useState<SortingState>([]);
  const [globalFilter, setGlobalFilter] = useState("");
  const table = useReactTable({
    data, columns, getRowId, state: { sorting, globalFilter },
    onSortingChange: setSorting, onGlobalFilterChange: setGlobalFilter,
    getCoreRowModel: getCoreRowModel(), getSortedRowModel: getSortedRowModel(), getFilteredRowModel: getFilteredRowModel(),
  });
  const rows = table.getRowModel().rows;
  const scroller = useRef<HTMLDivElement>(null);
  const virtualizer = useVirtualizer({
    count: rows.length, estimateSize: () => rowHeight, overscan: 12,
    getScrollElement: () => scroller.current, initialRect: { width: 0, height: typeof height === "number" ? height : 480 },
  });
  const widths = table.getVisibleLeafColumns().map((c) => c.columnDef.meta?.width);
  const template = widths.map((w) => (w ? `${w}px` : "minmax(120px, 1fr)")).join(" ");
  // Wide entities scroll sideways instead of squeezing their columns.
  const minWidth = widths.reduce<number>((sum, w) => sum + (w ?? 120), 0);

  return (
    <div className="flex min-w-0 flex-col gap-2">
      {(searchable || toolbar) && (
        <div className="flex items-center gap-2">
          {searchable && (
            <div className="relative w-64">
              <Search className="pointer-events-none absolute left-2 top-1/2 size-3.5 -translate-y-1/2 text-muted" />
              <Input aria-label={t("Filter rows")} placeholder={t("Filter")} value={globalFilter}
                onChange={(e) => setGlobalFilter(e.target.value)} className="pl-7" />
            </div>
          )}
          <span className="text-xs text-muted tabular-nums">{t("{n} rows", { n: rows.length.toLocaleString() })}</span>
          <div className="ml-auto flex items-center gap-2">{toolbar}</div>
        </div>
      )}
      <div ref={scroller} role="table" aria-rowcount={rows.length}
        className="overflow-auto rounded-md border border-border bg-surface" style={{ height }}>
        <div role="rowgroup" className="sticky top-0 z-10 border-b border-border bg-surface" style={{ minWidth }}>
          {table.getHeaderGroups().map((group) => (
            <div role="row" key={group.id} className="grid" style={{ gridTemplateColumns: template }}>
              {group.headers.map((header) => {
                const sorted = header.column.getIsSorted();
                return (
                  <div role="columnheader" key={header.id}
                    aria-sort={sorted === "asc" ? "ascending" : sorted === "desc" ? "descending" : "none"}
                    className={cn("flex h-7 items-center gap-1 truncate px-2 text-xs font-medium uppercase tracking-wide text-muted",
                      header.column.columnDef.meta?.align === "right" && "justify-end")}>
                    {header.column.getCanSort() ? (
                      <button type="button" className="flex items-center gap-1 hover:text-foreground"
                        onClick={header.column.getToggleSortingHandler()}>
                        {flexRender(header.column.columnDef.header, header.getContext())}
                        {sorted === "asc" ? <ArrowUp className="size-3" /> : sorted === "desc" ? <ArrowDown className="size-3" /> : null}
                      </button>
                    ) : flexRender(header.column.columnDef.header, header.getContext())}
                  </div>
                );
              })}
            </div>
          ))}
        </div>
        {rows.length === 0 ? (
          <div className="p-6 text-center text-sm text-muted">{empty}</div>
        ) : (
          <div role="rowgroup" className="relative" style={{ height: virtualizer.getTotalSize(), minWidth }}>
            {virtualizer.getVirtualItems().map((item) => {
              const row = rows[item.index]!;
              return (
                <div role="row" key={row.id} aria-rowindex={item.index + 1} aria-selected={row.id === selectedId}
                  onClick={onRowClick ? () => onRowClick(row.original) : undefined}
                  className={cn("absolute inset-x-0 grid items-center border-b border-border/60 text-sm hover:bg-row-hover",
                    onRowClick && "cursor-pointer", row.id === selectedId && "bg-row-selected")}
                  style={{ gridTemplateColumns: template, height: rowHeight, transform: `translateY(${item.start}px)` }}>
                  {row.getVisibleCells().map((cell) => {
                    const field = cell.column.columnDef.meta?.field;
                    const editable = !!onCellEdit && !!field?.editor && !field.readOnly;
                    const isEditing = editing?.row === row.id && editing.column === cell.column.id;
                    const begin = () => setEditing({ row: row.id, column: cell.column.id, draft: cell.getValue() });
                    const commit = () => {
                      if (editing && editing.draft !== cell.getValue()) onCellEdit?.(row.original, cell.column.id, editing.draft);
                      setEditing(undefined);
                    };
                    return (
                      <div role="cell" key={cell.id} tabIndex={editable ? 0 : undefined}
                        onDoubleClick={editable ? (e) => { e.stopPropagation(); begin(); } : undefined}
                        onKeyDown={editable && !isEditing ? (e) => { if (e.key === "Enter") { e.preventDefault(); begin(); } } : undefined}
                        className={cn("truncate px-2", cell.column.columnDef.meta?.align === "right" && "text-right tabular-nums",
                          editable && "outline-none focus-visible:ring-1 focus-visible:ring-ring", isEditing && "overflow-visible")}>
                        {isEditing ? (
                          <div ref={selectOnOpen} onClick={(e) => e.stopPropagation()}
                            className="relative z-20 min-w-full rounded-md bg-surface p-0.5 shadow-lg ring-1 ring-border"
                            onKeyDown={(e) => { if (e.key === "Enter") commit(); if (e.key === "Escape") setEditing(undefined); }}
                            onBlur={(e) => { if (!e.currentTarget.contains(e.relatedTarget)) commit(); }}>
                            {field!.editor!({ value: editing.draft, onChange: (draft) => setEditing({ ...editing, draft }), autoFocus: true })}
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
