import { useEffect, useId, useState } from "react";
import { Button } from "../primitives/button";
import { Input } from "../primitives/input";
import { t } from "../i18n";
import type { EntityRecord, RecordPageData, RecordSource, RecordList } from "./Records";

const pageSize = 25;

/** Searchable, paged reference selection over the caller's scoped record read. */
export function RecordLookup({ id, source, type, value, onChange, window, labelField, selectedRecord, ariaLabel, disabled }: {
  id?: string; source: RecordSource; type: string; value?: string; onChange: (id?: string) => void;
  window?:NonNullable<Parameters<typeof RecordList>[0]["window"]>;labelField?:string;selectedRecord?:EntityRecord;ariaLabel?:string;disabled?:boolean;
}) {
  const unique=useId();
  const info = source.entity(type);
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [offset, setOffset] = useState(0);
  const [page, setPage] = useState<RecordPageData>();
  const [error, setError] = useState(false);
  useEffect(() => {
    if (!open || window) return;
    let active = true;
    const timer = setTimeout(() => {
      void source.list(type, { search: query, offset, limit: pageSize }).then(
        (result) => { if (active) { setPage(result); setError(false); } },
        () => { if (active) { setPage(undefined); setError(true); } },
      );
    }, 150);
    return () => { active = false; clearTimeout(timer); };
  }, [source, type, open, query, offset,window]);
  const listId = `${id ?? unique}-options`,currentPage=window?window.page:page,currentError=window?!!window.error:error;
  const title=labelField??info?.display;
  const label = (record: EntityRecord) => title && title !== "id" && record[title]
    ? `${record.id} · ${String(record[title])}` : record.id;
  return <div className="relative" data-platform-escape-boundary={open||undefined} onKeyDown={e=>{if(e.key==="Escape"&&open){e.preventDefault();e.stopPropagation();e.currentTarget.querySelector<HTMLInputElement>("[role=combobox]")?.focus();setOpen(false);}}} onBlur={(e) => { if (!e.currentTarget.contains(e.relatedTarget)) setOpen(false); }}>
    <Input id={id} role="combobox" aria-label={ariaLabel} aria-autocomplete="list" aria-expanded={open} aria-controls={listId} disabled={disabled}
      readOnly={!!window?.searchLocked}
      placeholder={t("Search records")} value={open ? window?.inputSearch??window?.query.search??query : selectedRecord?label(selectedRecord):value ?? ""}
      onFocus={(e) => { setQuery(window?window.inputSearch??window.query.search??"":value??""); setOffset(0); setPage(undefined); setOpen(true); e.currentTarget.select(); }}
      onChange={(e) => { if(disabled||window?.searchLocked)return;setQuery(e.target.value); setOffset(0); setPage(undefined);if(window)window.onChange({search:e.target.value,offset:0}); }}
      onKeyDown={(e) => {
        if (e.key === "ArrowDown") { e.preventDefault(); document.getElementById(listId)?.querySelector<HTMLButtonElement>("[role=option]")?.focus(); }
        if (e.key === "Enter" && open && !disabled && !currentError && currentPage?.records.length) {
          e.preventDefault(); onChange(currentPage.records[0]!.id); setOpen(false);
        }
      }} />
    {open && !disabled && <div className="absolute z-30 mt-1 max-h-56 w-full min-w-0 overflow-auto rounded-md border border-border bg-surface p-1 shadow-lg">
      <div id={listId} role="listbox">{currentError ? <p role="alert" className="px-2 py-1 text-xs text-[var(--tone-danger)]">{t("Records could not be loaded.")}</p>
        : !currentPage ? <p className="px-2 py-1 text-xs text-muted">{t("Loading…")}</p>
          : currentPage.records.length === 0 ? <p className="px-2 py-1 text-xs text-muted">{t("No matching records.")}</p>
            : currentPage.records.map((record) => <button key={record.id} type="button" role="option" aria-selected={record.id === value}
                className="block w-full rounded px-2 py-1 text-left text-sm hover:bg-row-hover focus-visible:bg-row-selected focus-visible:outline-none"
                onClick={() => { onChange(record.id); setOpen(false); }}>{label(record)}</button>)}</div>
      {!window && page && page.total > pageSize && <div className="flex items-center justify-between gap-1 border-t border-border pt-1 text-xs text-muted">
        <Button size="sm" variant="ghost" disabled={offset === 0} onClick={() => { setOffset(Math.max(0, offset - pageSize)); setPage(undefined); }}>{t("Previous page")}</Button>
        <span>{offset + 1}–{Math.min(offset + pageSize, page.total)} / {page.total}</span>
        <Button size="sm" variant="ghost" disabled={offset + pageSize >= page.total} onClick={() => { setOffset(offset + pageSize); setPage(undefined); }}>{t("Next page")}</Button>
      </div>}
      {value && <Button size="sm" variant="ghost" onClick={() => { onChange(undefined); setQuery(""); setOpen(false); }}>{t("Clear selection")}</Button>}
    </div>}
  </div>;
}
