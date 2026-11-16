import { useEffect, useMemo, useState } from "react";
import { Button } from "../primitives/button";
import { Select } from "../primitives/input";
import { Typeahead, type SearchableItem, type SearchSource } from "@astryxdesign/core/Typeahead";
import { t } from "../i18n";
import { useViewVisible } from "../shell/ViewVisibility";
import type { Api } from "@platform/api";
import type { EntityRecord, InterfaceRecordData, InterfaceRecordIdentity, RecordSource, RecordList } from "./Records";

const pageSize = 25;

type LookupWindow<T> = Omit<NonNullable<Parameters<typeof RecordList>[0]["window"]>, "page"> & { page?: { records: T[]; total: number } };
type LookupItem<T> = SearchableItem<{record?: T}>;

function Lookup<T>({ id, scope, target, revision, read, value, valueLabel, initialSearch, onChange, window, label, keyOf, selectedRecord, ariaLabel, disabled }: {
  id?: string; scope: unknown; target: string; revision?: number; read: (search: string, offset: number) => Promise<{ records: T[]; total: number }>;
  watch?: (search: string, offset: number, changed: () => void) => () => void;
  value?: string; valueLabel?: string; initialSearch?: string; onChange: (record?: T) => void; window?: LookupWindow<T>;
  label: (record: T) => string; keyOf: (record: T) => string; selectedRecord?: T; ariaLabel?: string; disabled?: boolean;
}) {
  const itemOf = (record: T): LookupItem<T> => ({ id: keyOf(record), label: label(record), auxiliaryData: { record } });
  const current: LookupItem<T> | null = selectedRecord ? itemOf(selectedRecord) : value ? { id: value, label: valueLabel ?? value, auxiliaryData: {} } : null;
  const source = useMemo<SearchSource<LookupItem<T>>>(() => ({
    search: async (query) => {
      if (window) {
        window.onChange({ search: query, offset: 0 });
        return (window.page?.records ?? []).map(itemOf);
      }
      const records: T[] = [];
      for (let offset = 0; ;) {
        const page = await read(query, offset);
        records.push(...page.records);
        if (!page.records.length || records.length >= page.total) break;
        offset += page.records.length;
      }
      return records.map(itemOf);
    },
    bootstrap: async () => {
      if (window) return (window.page?.records ?? []).map(itemOf);
      return (await read(initialSearch ?? value ?? "", 0)).records.map(itemOf);
    },
  }), [scope, target, revision, window, read, initialSearch, value, keyOf, label]);
  return <Typeahead key={window ? `${window.query.search ?? ""}:${window.page?.records?.map(keyOf).join("|") ?? ""}` : undefined} id={id} label={ariaLabel ?? t("Search records")} isLabelHidden placeholder={t("Search records")} searchSource={source} value={current} onChange={(item) => onChange(item?.auxiliaryData?.record)} isDisabled={disabled} hasEntriesOnFocus width="100%" size="sm" />;
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
