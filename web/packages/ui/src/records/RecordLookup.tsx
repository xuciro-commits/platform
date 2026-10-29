import { useEffect, useId, useRef, useState } from "react";
import { Button } from "../primitives/button";
import { Input, Select } from "../primitives/input";
import { t } from "../i18n";
import { useViewVisible } from "../shell/ViewVisibility";
import type { Api } from "@platform/kernel";
import type { EntityRecord, InterfaceRecordData, InterfaceRecordIdentity, RecordSource, RecordList } from "./Records";

const pageSize = 25;

type LookupWindow<T> = Omit<NonNullable<Parameters<typeof RecordList>[0]["window"]>, "page"> & { page?: { records: T[]; total: number } };
function Lookup<T>({ id, scope, target, revision, read, watch, value, valueLabel, initialSearch, onChange, window, label, keyOf, selectedRecord, ariaLabel, disabled }: {
  id?: string; scope: unknown; target: string; revision?: number; read: (search: string, offset: number) => Promise<{ records: T[]; total: number }>;
  watch?: (search: string, offset: number, changed: () => void) => () => void;
  value?: string; valueLabel?: string; initialSearch?: string; onChange: (record?: T) => void; window?: LookupWindow<T>;
  label: (record: T) => string; keyOf: (record: T) => string; selectedRecord?: T; ariaLabel?: string; disabled?: boolean;
}) {
  const unique=useId();
  const [opened, setOpened] = useState<{scope:unknown;target:string}>();
  const open=opened?.scope===scope&&opened?.target===target;
  const setOpen=(open:boolean)=>setOpened(open?{scope,target}:undefined);
  const [query, setQuery] = useState("");
  const [offset, setOffset] = useState(0);
  const [page, setPage] = useState<{records:T[];total:number}>();
  const [error, setError] = useState<string>();
  const ports = useRef({read, watch}); ports.current = {read, watch};
  useEffect(() => { setPage(undefined); setError(undefined); setOpen(false); }, [scope, target]);
  useEffect(() => {
    if (!open || window) return;
    let active = true, epoch = 0;
    const load = () => {
      const request = ++epoch;
      void ports.current.read(query, offset).then(
        (result) => { if (active && epoch === request) { setPage(result); setError(undefined); } },
        (problem) => { if (active && epoch === request) { setPage(undefined); setError(problem instanceof Error?problem.message:t("Records could not be loaded.")); } },
      );
    };
    const stop = ports.current.watch?.(query,offset,load), timer = setTimeout(load,150);
    return () => { active = false; stop?.(); clearTimeout(timer); };
  }, [scope,target,revision,open,query,offset,window]);
  const listId = `${id ?? unique}-options`,currentPage=window?window.page:page,currentError=window?window.error:error;
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
      <div id={listId} role="listbox">{currentError ? <p role="alert" className="px-2 py-1 text-xs text-[var(--tone-danger)]">{t(currentError)}</p>
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

export function EnterpriseElementLookup({id,source,value,onChange,stereotypes=[],ariaLabel,disabled}:{id?:string;source:RecordSource;value?:string;onChange:(id?:string)=>void;stereotypes?:string[];ariaLabel?:string;disabled?:boolean}) {
 const scope=source.scope??source,revision=source.revision,families=stereotypes.join(","),visible=useViewVisible();
 const [resolved,setResolved]=useState<{scope:typeof scope;revision?:number;families:string;value?:string;items:Api.EnterpriseQueryElement[];kept?:Api.EnterpriseQueryElement;error?:boolean}>();
 const current=resolved?.scope===scope&&resolved.revision===revision&&resolved.families===families&&resolved.value===value?resolved:undefined;
 const items=current?.items??[],kept=current?.kept,error=current?.error;
 useEffect(() => {
   if(!visible)return;
   let active = true, epoch = 0;
   const load = async () => {
     const request = ++epoch;
     setResolved(undefined);
     try {
       const [options, resolved] = await Promise.all([
         source.enterpriseQuery?.(stereotypes, "") ?? Promise.reject(Error("unavailable")),
         value ? source.enterpriseResolve?.([value]) ?? Promise.resolve({elements: []}) : Promise.resolve({elements: []}),
       ]);
       if (active && epoch === request) setResolved({scope,revision,families,value,items:options.elements,kept:resolved.elements.find(element=>element.id===value)});
     } catch { if (active && epoch === request) setResolved({scope,revision,families,value,items:[],error:true}); }
   };
   void load();
   const stop = source.watchEnterprise?.(() => void load());
   return () => { active = false; stop?.(); };
 }, [scope, revision, families, value, source.enterpriseQuery, source.enterpriseResolve, source.watchEnterprise,visible]);
 return <div className="grid min-w-0 gap-1"><Select id={id} aria-label={ariaLabel} disabled={disabled} value={value??""} onChange={event=>onChange(event.target.value||undefined)} className="h-7 w-full min-w-0 rounded-md border border-border bg-surface px-2 text-xs"><option value="">—</option>{items.map(item=><option key={item.id} value={item.id}>{item.name}{item.kind?" · "+item.kind:""}</option>)}{value&&!items.some(item=>item.id===value)&&<option value={value}>{kept?kept.name+(kept.closed?" · "+t("Inactive"):""):value}</option>}</Select>{error&&<p role="alert" className="text-xs text-danger">{t("Enterprise elements could not be loaded.")}</p>}</div>;
}

export function EnterpriseReference({source,value,onOpen}:{source:RecordSource;value?:string;onOpen?:(type:string,id:string)=>void}) {
 const identity=source.scope??source,visible=useViewVisible();
 const [resolved,setResolved]=useState<{source:typeof identity;value?:string;revision?:number;element?:Api.EnterpriseQueryElement}>();
 const element=resolved?.source===identity&&resolved.value===value&&resolved.revision===source.revision?resolved.element:undefined;
 useEffect(() => {
   if(!visible)return;
   let active = true, epoch = 0;
   const load = async () => {
     const request = ++epoch;
     setResolved(undefined);
     if (!value) return;
     try {
       const answer = await source.enterpriseResolve?.([value]);
       if (active && epoch === request) setResolved({source:identity,value,revision:source.revision,element:answer?.elements.find(element=>element.id===value)});
     } catch {   }
   };
   void load();
   const stop = source.watchEnterprise?.(() => void load());
   return () => { active = false; stop?.(); };
 }, [identity, source.revision, source.enterpriseResolve, source.watchEnterprise, value,visible]);
 return value?<Button variant="link" size="sm" disabled={!onOpen} className="max-w-full whitespace-normal text-left" onClick={event=>{event.stopPropagation();onOpen?.("enterprise.element",value);}}>{element?.name??value}{element?.closed?" · "+t("Inactive"):""}</Button>:<span className="text-muted">—</span>;
}
