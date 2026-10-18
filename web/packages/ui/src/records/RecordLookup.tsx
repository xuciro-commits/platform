import { useEffect, useId, useRef, useState } from "react";
import { Button } from "../primitives/button";
import { Input, Select } from "../primitives/input";
import { t } from "../i18n";
import { useWorkspace } from "../shell/Workspace";
import type { Api } from "@platform/kernel";
import type { EntityRecord, InterfaceRecordData, InterfaceRecordIdentity, RecordSource, RecordList } from "./Records";

const pageSize = 25;

/** Searchable, paged reference selection over the caller's scoped record read. */
type LookupWindow<T> = Omit<NonNullable<Parameters<typeof RecordList>[0]["window"]>, "page"> & { page?: { records: T[]; total: number } };
function Lookup<T>({ id, scope, target, revision, read, watch, value, valueLabel, initialSearch, onChange, window, label, keyOf, selectedRecord, ariaLabel, disabled }: {
  id?: string; scope: unknown; target: string; revision?: number; read: (search: string, offset: number) => Promise<{ records: T[]; total: number }>;
  watch?: (search: string, offset: number, changed: () => void) => () => void;
  value?: string; valueLabel?: string; initialSearch?: string; onChange: (record?: T) => void; window?: LookupWindow<T>;
  label: (record: T) => string; keyOf: (record: T) => string; selectedRecord?: T; ariaLabel?: string; disabled?: boolean;
}) {
  const unique=useId();
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const [offset, setOffset] = useState(0);
  const [page, setPage] = useState<{records:T[];total:number}>();
  const [error, setError] = useState(false);
  const ports = useRef({read, watch}); ports.current = {read, watch};
  useEffect(() => { setPage(undefined); setError(false); setOpen(false); }, [scope, target]);
  useEffect(() => {
    if (!open || window) return;
    let active = true, epoch = 0;
    const load = () => {
      const request = ++epoch;
      void ports.current.read(query, offset).then(
        (result) => { if (active && epoch === request) { setPage(result); setError(false); } },
        () => { if (active && epoch === request) { setPage(undefined); setError(true); } },
      );
    };
    const stop = ports.current.watch?.(query,offset,load), timer = setTimeout(load,150);
    return () => { active = false; stop?.(); clearTimeout(timer); };
  }, [scope,target,revision,open,query,offset,window]);
  const listId = `${id ?? unique}-options`,currentPage=window?window.page:page,currentError=window?!!window.error:error;
  return <div className="relative" data-platform-escape-boundary={open||undefined} onKeyDown={e=>{if(e.key==="Escape"&&open){e.preventDefault();e.stopPropagation();e.currentTarget.querySelector<HTMLInputElement>("[role=combobox]")?.focus();setOpen(false);}}} onBlur={(e) => { if (!e.currentTarget.contains(e.relatedTarget)) setOpen(false); }}>
    <Input id={id} role="combobox" aria-label={ariaLabel} aria-autocomplete="list" aria-expanded={open} aria-controls={listId} disabled={disabled}
      readOnly={!!window?.searchLocked}
      placeholder={t("Search records")} value={open ? window?.inputSearch??window?.query.search??query : selectedRecord?label(selectedRecord):valueLabel ?? value ?? ""}
      onFocus={(e) => { setQuery(window?window.inputSearch??window.query.search??"":initialSearch??value??""); setOffset(0); setPage(undefined); setOpen(true); e.currentTarget.select(); }}
      onClick={() => {if(!disabled&&!open){setQuery(window?window.inputSearch??window.query.search??"":initialSearch??value??"");setOpen(true);}}}
      onChange={(e) => { if(disabled||window?.searchLocked)return;setOpen(true);setQuery(e.target.value); setOffset(0); setPage(undefined);if(window)window.onChange({search:e.target.value,offset:0}); }}
      onKeyDown={(e) => {
        if (e.key === "ArrowDown") { e.preventDefault(); document.getElementById(listId)?.querySelector<HTMLButtonElement>("[role=option]")?.focus(); }
        if (e.key === "Enter" && open && !disabled && !currentError && currentPage?.records.length) {
          e.preventDefault(); onChange(currentPage.records[0]!); setOpen(false);
        }
      }} />
    {open && !disabled && <div className="absolute z-30 mt-1 max-h-56 w-full min-w-0 overflow-auto rounded-md border border-border bg-surface p-1 shadow-lg">
      <div id={listId} role="listbox">{currentError ? <p role="alert" className="px-2 py-1 text-xs text-[var(--tone-danger)]">{t("Records could not be loaded.")}</p>
        : !currentPage ? <p className="px-2 py-1 text-xs text-muted">{t("Loading…")}</p>
          : currentPage.records.length === 0 ? <p className="px-2 py-1 text-xs text-muted">{t("No matching records.")}</p>
            : currentPage.records.map((record) => <button key={keyOf(record)} type="button" role="option" aria-selected={keyOf(record) === value}
                className="block w-full rounded px-2 py-1 text-left text-sm hover:bg-row-hover focus-visible:bg-row-selected focus-visible:outline-none"
                onClick={() => { onChange(record); setOpen(false); }}>{label(record)}</button>)}</div>
      {!window && page && page.total > pageSize && <div className="flex items-center justify-between gap-1 border-t border-border pt-1 text-xs text-muted">
        <Button size="sm" variant="ghost" disabled={offset === 0} onClick={() => { setOffset(Math.max(0, offset - pageSize)); setPage(undefined); }}>{t("Previous page")}</Button>
        <span>{offset + 1}–{Math.min(offset + pageSize, page.total)} / {page.total}</span>
        <Button size="sm" variant="ghost" disabled={offset + pageSize >= page.total} onClick={() => { setOffset(offset + pageSize); setPage(undefined); }}>{t("Next page")}</Button>
      </div>}
      {value && <Button size="sm" variant="ghost" onClick={() => { onChange(undefined); setQuery(""); setOpen(false); }}>{t("Clear selection")}</Button>}
    </div>}
  </div>;
}

export function RecordLookup({ id, source, type, value, onChange, window, labelField, selectedRecord, ariaLabel, disabled, stereotypes = [] }: {
  id?: string; source: RecordSource; type: string; value?: string; onChange: (id?: string) => void; stereotypes?: string[];
  window?:NonNullable<Parameters<typeof RecordList>[0]["window"]>;labelField?:string;selectedRecord?:EntityRecord;ariaLabel?:string;disabled?:boolean;
}) {
  if (type === "enterprise.element") return <EnterpriseElementLookup id={id} source={source} value={value} onChange={onChange} stereotypes={stereotypes} ariaLabel={ariaLabel} disabled={disabled} />;
  const title = labelField ?? source.entity(type)?.display;
  return <Lookup id={id} scope={source.scope ?? source} target={type} revision={source.revision} value={value} onChange={record => onChange(record?.id)}
    read={(search,offset) => source.list(type,{search,offset,limit:pageSize})}
    watch={source.watchList ? (search,offset,changed) => source.watchList!(type,{search,offset,limit:pageSize},changed) : undefined}
    keyOf={record => record.id} label={record => title && title !== "id" && record[title] ? `${record.id} · ${String(record[title])}` : record.id}
    window={window} selectedRecord={selectedRecord} ariaLabel={ariaLabel} disabled={disabled}/>;
}

/** Interface selection preserves the original type/id pair; opening or
 * acting on it goes through that type's ordinary authorized record path. */
export function InterfaceRecordLookup({id,source,name,value,onChange,ariaLabel,disabled,window,labelField}: {
  id?:string;source:RecordSource;name:string;value?:InterfaceRecordIdentity;onChange:(reference?:InterfaceRecordIdentity)=>void;ariaLabel?:string;disabled?:boolean;window?:LookupWindow<InterfaceRecordData>;labelField?:string;
}) {
  const keyOf = (ref:InterfaceRecordIdentity) => JSON.stringify([ref.type,ref.id]);
  const prefix = (ref:InterfaceRecordIdentity) => `${source.entity(ref.type)?.title ?? ref.type} · ${ref.id}`;
  return <Lookup<InterfaceRecordData> id={id} scope={source.scope ?? source} target={name} revision={source.revision} value={value && keyOf(value)} valueLabel={value && prefix(value)} initialSearch=""
    read={(search,offset) => source.interfaceList ? source.interfaceList(name,{search,offset,limit:pageSize}) : Promise.reject(Error("Interface record read is unavailable"))}
    watch={source.watchInterfaceList ? (search,offset,changed) => source.watchInterfaceList!(name,{search,offset,limit:pageSize},changed) : undefined}
    onChange={row => onChange(row && {type:row.type,id:row.id})} keyOf={keyOf} label={row => {
      const display=labelField??source.entity(row.type)?.display,value=display && display!=="id" && row.record[display];
      return value ? `${prefix(row)} · ${String(value)}` : prefix(row);
    }} window={window} ariaLabel={ariaLabel} disabled={disabled}/>;
}

/** The model owns validity and labels; record editing never treats it as a record table. */
export function EnterpriseElementLookup({id,source,value,onChange,stereotypes=[],ariaLabel,disabled}:{id?:string;source:RecordSource;value?:string;onChange:(id?:string)=>void;stereotypes?:string[];ariaLabel?:string;disabled?:boolean}) {
 const [items,setItems]=useState<Api.EnterpriseQueryElement[]>([]),[kept,setKept]=useState<Api.EnterpriseQueryElement>(),[error,setError]=useState(false);
 const scope=source.scope,revision=source.revision,families=stereotypes.join(",");
 useEffect(() => {
   let active = true, epoch = 0;
   setError(false); setItems([]); setKept(undefined);
   const load = async () => {
     const request = ++epoch;
     try {
       const [options, resolved] = await Promise.all([
         source.enterpriseQuery?.(stereotypes, "") ?? Promise.reject(Error("unavailable")),
         value ? source.enterpriseResolve?.([value]) ?? Promise.resolve({elements: []}) : Promise.resolve({elements: []}),
       ]);
       if (active && epoch === request) { setItems(options.elements); setKept(resolved.elements[0]); setError(false); }
     } catch { if (active && epoch === request) setError(true); }
   };
   void load();
   const stop = source.watchEnterprise?.(() => void load());
   return () => { active = false; stop?.(); };
 }, [scope, revision, families, value, source.enterpriseQuery, source.enterpriseResolve, source.watchEnterprise]);
 return <div className="grid min-w-0 gap-1"><Select id={id} aria-label={ariaLabel} disabled={disabled} value={value??""} onChange={event=>onChange(event.target.value||undefined)} className="h-7 w-full min-w-0 rounded-md border border-border bg-surface px-2 text-xs"><option value="">—</option>{items.map(item=><option key={item.id} value={item.id}>{item.name}{item.kind?" · "+item.kind:""}</option>)}{value&&!items.some(item=>item.id===value)&&<option value={value}>{kept?kept.name+(kept.closed?" · "+t("Inactive"):""):value}</option>}</Select>{error&&<p role="alert" className="text-xs text-danger">{t("Enterprise elements could not be loaded.")}</p>}</div>;
}

export function EnterpriseReference({source,value}:{source:RecordSource;value?:string}) {
 const {open}=useWorkspace();const [element,setElement]=useState<Api.EnterpriseQueryElement>();
 useEffect(() => {
   let active = true, epoch = 0;
   setElement(undefined);
   const load = async () => {
     const request = ++epoch;
     if (!value) return;
     try {
       const answer = await source.enterpriseResolve?.([value]);
       if (active && epoch === request) setElement(answer?.elements[0]);
     } catch { /* A retained reference still reads as its ID when unavailable. */ }
   };
   void load();
   const stop = source.watchEnterprise?.(() => void load());
   return () => { active = false; stop?.(); };
 }, [source.scope, source.revision, source.enterpriseResolve, source.watchEnterprise, value]);
 return value?<Button variant="link" size="sm" className="max-w-full whitespace-normal text-left" onClick={()=>open({view:"enterprise",params:{element:value,section:"organization"}})}>{element?.name??value}{element?.closed?" · "+t("Inactive"):""}</Button>:<span className="text-muted">—</span>;
}
