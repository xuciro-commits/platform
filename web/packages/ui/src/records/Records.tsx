// The application model's pages (ADR-0046): an entity type described by the
// host (`GET /v1/entities`) becomes a kit entity, a list page with server-side
// search, sort and paging, a record page (fields, related records, history) and
// generated forms. Components take a RecordSource, so the kit knows no client.
import { History as HistoryIcon, Printer } from "lucide-react";
import { Barcode } from "./barcode";
import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { useViewVisible } from "../shell/ViewVisibility";
import {StructuredEditor} from "../components/StructuredEditor";
import {PropertyList} from "../components/EntityCard";
import {Tag} from "../components/StatusTag";
import {columnsFor} from "../fields/entity";
import {RecordComments} from "./RecordComments";
import {RecordUploader} from "./RecordUploader";
import { DataTable } from "../components/DataTable";
import {ContentTabs} from "../layout/ContentTabs";
import {pageUIManifest} from "@platform/kernel";
import { Button } from "../primitives/button";
import { FlowSteps, type FlowStepEdge, type FlowStepNode } from "../graph/flow/FlowSteps";
import { t } from "../i18n";
import { humanizeKernelError } from "../lib/errors";
import type { Api } from "@platform/kernel";

import {displayOf,entityFrom,listed,StatusBar,type AttachedFile,type EntityInfo,type EntityRecord,type FieldInfo,type Money,type RecordChange,type RecordComment,type RecordSource,type RecordView} from "./record-presentation";
export {entityFrom,groupable,measurable,setCurrency,StatusBar,type AttachedFile,type EntityInfo,type EntityRecord,type FieldInfo,type InterfaceRecordData,type InterfaceRecordIdentity,type InterfaceRecordPageData,type Lifecycle,type Money,type Options,type RecordChange,type RecordComment,type RecordPageData,type RecordQuery,type RecordSource,type RecordView,type State} from "./record-presentation";
export {RecordList,type ListState} from "./RecordWorkspace";
export type RecordHistoryProps={info:EntityInfo;history?:readonly RecordChange[];heading?:boolean;label?:string;limit?:number;total?:number;loadedCount?:number;recordID?:string;recordRevision?:number};
/** A record's original journal order: who changed what. Its
 *  page shows it, and so does a composed page's timeline (ADR-0048 16b). */
export function RecordHistory({info,history=[],heading=true,label,limit,total,loadedCount=history.length,recordID,recordRevision}:RecordHistoryProps) {
  if(!Array.isArray(history)||limit!==undefined&&(!Number.isSafeInteger(limit)||limit<1||limit>pageUIManifest.runtime.workViews.maxHistoryWindow)||!Number.isSafeInteger(loadedCount)||loadedCount<history.length||total!==undefined&&(!Number.isSafeInteger(total)||total<loadedCount)||recordID!==undefined&&(typeof recordID!=="string"||!recordID)||recordRevision!==undefined&&(!Number.isSafeInteger(recordRevision)||recordRevision<0)||history.some((change:RecordChange)=>!change||typeof change.change!=="string"||!change.change||typeof change.schema!=="string"||!change.schema||typeof change.by!=="string"||typeof change.at!=="string"||!change.at||!Array.isArray(change.fields)||change.fields.some((field:Api.FieldChange)=>!field||typeof field.field!=="string"||!field.field)))return <p role="alert">{t("The original record history is unavailable or incompatible.")}</p>;
  const visible:readonly RecordChange[]=limit===undefined?history:history.slice(0,limit),range=limit!==undefined||total!==undefined||loadedCount!==history.length;
  const occurrences=new Map<string,number>();
  return (
    <section aria-label={label??t("History")} className="grid min-w-0 gap-2">
      {heading && <h2 className="flex items-center gap-1 text-sm font-semibold"><HistoryIcon className="size-3.5" />{label??t("History")}</h2>}
      {(recordID!==undefined||recordRevision!==undefined)&&<p className="break-all font-mono text-xs text-muted">{recordID}{recordRevision!==undefined?` · ${t("Revision {revision}",{revision:recordRevision})}`:""}</p>}
      {range&&<p role="status" className="text-xs text-muted">{total===undefined?t("Showing {shown} of {loaded} loaded changes; the complete total is unavailable.",{shown:visible.length,loaded:loadedCount}):t("Showing {shown} of {total} changes.",{shown:visible.length,total})}</p>}
      {visible.length===0&&<p className="text-sm text-muted">{t("No changes yet.")}</p>}
      <ol className="grid min-w-0 gap-2">
        {visible.map(h => {const occurrence=occurrences.get(h.change)??0;occurrences.set(h.change,occurrence+1);return (
          <li key={JSON.stringify([h.change,occurrence])} className="min-w-0 rounded-md border border-border bg-surface p-2 text-xs">
            <div className="flex min-w-0 flex-wrap gap-2"><span className="break-all font-mono">{h.change}</span><span className="break-all font-mono">{h.schema}</span><span className="break-words text-muted">{h.by} · <time dateTime={h.at}>{h.at}</time></span></div>
            {h.fields.length > 0 && (
              <ul className="mt-1 grid gap-0.5">
                {h.fields.map((f) => {
                  const declared = info.fields.find((x) => x.name === f.field);
                  if(!declared&&!(f.field==="archived"&&[f.before,f.after].every(value=>value===undefined||typeof value==="boolean")))return null;
                  return <li key={f.field} className="min-w-0 break-words"><span className="text-muted">{declared?.title ?? t("Archived")}</span>{" "}
                    {declared?.type === "lines" ? <LinesChange field={declared} before={f.before} after={f.after} />
                      : <>{f.before !== undefined && <><s className="text-muted">{shown(f.before)}</s> → </>}{shown(f.after)}</>}</li>;
                })}
              </ul>
            )}
          </li>
        );})}
      </ol>
    </section>
  );
}

const shown = (v: unknown) => (v === undefined || v === null || v === "" ? "—" : typeof v === "object" ? JSON.stringify(v) : String(v));

/** Read and watch one record; callers choose whether to show its previous value while loading. */
export function useRecordView(source: RecordSource, type: string, id?: string, reload = 0) {
  const identity = JSON.stringify([source.scope, type, id]);
  const key = JSON.stringify([identity, source.revision, reload]);
  const from = useRef(source);
  from.current = source;
  const sourceIdentity = source.scope ?? source;
  const [result, setResult] = useState<{ identity: string; key: string; view?: RecordView; error?: string; loading: boolean }>();
  const request = useRef(0);
  const refresh = useCallback(() => {
    if (!id) return;
    const epoch = ++request.current, source = from.current, scope = source.scope;
    setResult(previous => ({ identity, key, view: previous?.identity === identity ? previous.view : undefined, loading: true }));
    source.get(type, id).then(view => {
      if (view.record.id !== id) throw new Error("Record identity mismatch");
      if (request.current === epoch && source.scope === scope) setResult({ identity, key, view, loading: false });
    }).catch(error => {
      if (request.current === epoch && source.scope === scope) setResult({ identity, key, error: error instanceof Error ? error.message : String(error), loading: false });
    });
  }, [sourceIdentity, type, id, identity, key]);
  const visible = useViewVisible();
  useEffect(() => {
    if (!visible || !id) return;
    refresh();
    const stop = from.current.watchRecord?.(type, id, refresh);
    return () => { request.current++; stop?.(); };
  }, [refresh, visible, type, id]);
  const current = result?.identity === identity ? result : undefined;
  return { view: current?.view, error: current?.key === key ? current.error : undefined, loading: !!id && (current?.key !== key || current.loading), refresh };
}

/** One record: its fields, the records that refer to it, and its history from the journal. */
export function RecordPage({ source, type, id, actions, onOpen, reload = 0, can, onTransition, files, comments, tasks, fields, work, detailOnly = false,detailPresentation,recordTabs,recordLinks,linksOnly=false,statusTracker,statusOnly=false }: {
  detailPresentation?:Api.PageDetailPresentation;recordTabs?:readonly string[];recordLinks?:readonly Api.PageRecordLink[];linksOnly?:boolean;statusTracker?:Api.PageStatusTracker;statusOnly?:boolean;
  /** App API composes declared record-specific work without another read path. */
  work?: (view: RecordView) => ReactNode;
  /** Answering the open tasks about the record from its page; without it they are listed only. */
  tasks?: { answer: (task: Api.InboxTask, answer?: string) => Promise<void>; actions?: (task: Api.InboxTask) => ReactNode };
  source: RecordSource; type: string; id: string; actions?: (r: EntityRecord) => ReactNode;
  /** Presentation subset. No field omitted by source.entity can be restored here. */
  fields?: string[];
  /** A composed detail widget shows only this record's header and fields. */
  detailOnly?: boolean;
  /** Uploading a file to the record and downloading one (ADR-0038); without it the files are listed only. */
  files?: { upload: (file: File) => Promise<void>; download: (f: AttachedFile) => void };
  /** Commenting and following (ADR-0038); without it comments are listed only. */
  comments?: { add: (text: string) => Promise<boolean>; follow: (on: boolean) => Promise<void> };
  onOpen?: (type: string, r: EntityRecord) => void; reload?: number;
  /** The caller's catalog, and how to take a lifecycle transition (a decision on this record). */
  can?: (schema: string) => boolean; onTransition?: (schema: string, r: EntityRecord) => void;
}) {
  const tabIdentity=JSON.stringify([source.scope,type,id,recordTabs]);
  const [tabState,setTab]=useState<{identity:string;value:string}>();
  const info = source.entity(type);
  const { view, error, refresh: load } = useRecordView(source, type, id, reload);
  const entity = useMemo(() => (info ? entityFrom(info) : undefined), [info]);
  if (error) {
    return (
      <div role="alert" className="grid max-w-xl gap-2 rounded-md border border-[var(--tone-danger)] bg-surface p-4 text-sm">
        <p className="font-medium text-[var(--tone-danger)]">{t("Could not load record.")}</p>
        <p className="font-mono text-xs text-muted">{humanizeKernelError(error)}</p>
        <div>
          <Button size="sm" onClick={load}>{t("Try again")}</Button>
        </div>
      </div>
    );
  }
  if (!info || !entity || !view) return <p className="text-sm text-muted">{t("Loading…")}</p>;
  const r = view.record;
  if(statusOnly){
   const l=info.lifecycle,value=statusTracker,field=info.fields.find(f=>f.name===value?.field);
   if(!l||!value||value.field!==l.field||!field||!value.stages.length||value.stages.some(name=>!l.states.some(s=>s.name===name))||typeof r[value.field]!=="string")return <p role="alert" className="text-sm text-muted">{t("Lifecycle status is unavailable.")}</p>;
   const states=value.stages.map(name=>l.states.find(s=>s.name===name)!);
   return <div className="grid min-w-0 gap-2"><StatusBar lifecycle={{...l,states}} state={r[value.field] as string} tracker/>{!value.stages.includes(r[value.field] as string)&&<p className="text-xs text-muted">{t("Current state is outside the displayed stages.")}</p>}</div>;
  }

  const header=(<header className="flex flex-wrap items-center gap-2">
        <h1 className="min-w-0 break-words text-lg font-semibold">{displayOf(info, r)}</h1>
        <span className="min-w-0 break-words font-mono text-xs text-muted">{info.title} · {r.id} {t("· rev")} {r.revision}</span>
        {r.archived && <Tag label="archived" />}
		{view.effective===false&&<Tag label={t("Expired")} />}
        {!!info.versionPins?.length&&(r.versionPins as Api.RecordVersionPin[]|undefined)?.map(pin=><Tag key={pin.reference} label={t("Pinned version: {version}",{version:String(pin.version)})} />)}
        {info.versioning?.validUntil&&Boolean(r[info.versioning.validUntil])&&<Tag label={t("Valid until: {date}",{date:String(r[info.versioning.validUntil])})} />}
        {!detailOnly && <span className="ml-auto flex flex-wrap gap-1">{actions?.(r)}</span>}
      </header>);
  const barcode=detailPresentation?.barcode?detailPresentation.barcode==="id"?r.id:String(r[detailPresentation.barcode]??""):"";
  // Extension records (ADR-0020 A3): a type that extends this one pairs one record per base record; its
  // fields show as the base record's own, as far as the reader may read them (the host already filters).
  const extensions=(view.related??[]).flatMap(rel=>{const relInfo=source.entity(rel.type);const row=rel.records[0];if(!relInfo||!row||rel.field!=="base"||(relInfo as {extends?:string}).extends!==type)return [];const relEntity=entityFrom(relInfo);return [{rel,relInfo,relEntity,row}];});
  const properties=(<section className="rounded-md border border-border bg-surface p-3" data-print-label={detailPresentation?.print||undefined}>
        {barcode&&<div className="mb-3 flex justify-center overflow-hidden"><Barcode text={barcode}/></div>}
        {detailPresentation?.print&&<div className="mb-3 flex justify-end print:hidden"><Button size="sm" onClick={()=>window.print()}><Printer/>{t("Print")}</Button></div>}
        <PropertyList columns={detailPresentation?.columns as 1|2|3|4|undefined} items={[...[...new Set(fields??info.fields.map(f=>f.name))].flatMap(name=>{const f=info.fields.find(f=>f.name===name);return !f||detailPresentation?.hideNull&&(r[name]===undefined||r[name]===null||r[name]==="")?[]:[[f.title,entity.fields[name]!.display(r[name] as never,r)] as [string,ReactNode]];}),
          ...(!detailOnly ? [[t("Created"), `${r.created.by ?? ""} · ${r.created.at ? new Date(r.created.at).toLocaleString() : ""}`],
            [t("Changed"), `${r.changed.by ?? ""} · ${r.changed.at ? new Date(r.changed.at).toLocaleString() : ""}`]] as [string, ReactNode][] : [])]} />
        {extensions.map(({rel,relInfo,relEntity,row})=><div key={rel.type} className="mt-3 border-t border-border pt-3">
          <h3 className="mb-1 text-xs font-semibold text-muted">{relInfo.title}</h3>
          <PropertyList columns={detailPresentation?.columns as 1|2|3|4|undefined} items={relInfo.fields.filter(f=>f.name!=="base"&&!(detailPresentation?.hideNull&&(row[f.name]===undefined||row[f.name]===null||row[f.name]==="")))
            .map(f=>[f.title,relEntity.fields[f.name]!.display(row[f.name] as never,row)] as [string,ReactNode])}/>
        </div>)}
        {!detailOnly && <div className="mt-3 border-t border-border pt-3"><StructuredEditor key={type+":"+id} scope={type+":"+id} value={Object.fromEntries(Object.entries(r).filter(([name])=>["id","revision","created","changed","archived"].includes(name)||info.fields.some(field=>field.name===name)))} readOnly><span className="text-xs text-muted">{t("View the readable record as JSON or YAML.")}</span></StructuredEditor></div>}
      </section>);
  const groups=detailOnly&&!recordTabs&&!linksOnly?[]:recordLinks===undefined?[...view.related,...(view.linked??[])].map(rel=>({rel,title:undefined as string|undefined})):recordLinks.flatMap(group=>{const target=source.entity(group.object.name),field=target?.fields.find(f=>f.name===group.field&&f.type==="reference"&&f.ref===type&&!!f.inverse),rel=view.related.find(r=>r.type===group.object.name&&r.field===group.field);return field&&target?.app===group.object.app&&rel?[{rel,title:group.title}]:[];});
  const related=detailOnly&&!recordTabs&&!linksOnly?[]:groups.filter(({rel})=>!extensions.some(x=>x.rel===rel)).flatMap(({rel,title}) => {
    const relInfo=source.entity(rel.type),relEntity=relInfo&&entityFrom(relInfo);if(!relEntity)return [];
    return [<section key={`${rel.type}.${rel.field}`}>
      <h2 className="mb-1 text-sm font-semibold">{title||relInfo.plural} <span className="font-normal text-muted">({rel.total}{rel.field === "link" ? t(", linked") : rel.relation ? <>{" · "}{rel.relation}</> : <>{t(", by")} {rel.field}</>})</span></h2>
      <DataTable data={rel.records} columns={[{id:"id",header:"ID",accessorKey:"id",meta:{width:130}},...columnsFor(relEntity,listed(relEntity))] as never} getRowId={(x:EntityRecord)=>x.id} height={Math.min(40+rel.records.length*28,260)} searchable={false} onRowClick={onOpen&&((x:EntityRecord)=>onOpen(rel.type,x))} empty={t("None")}/>
    </section>];
  });
  if(linksOnly)return <div className="grid min-w-0 gap-3">{related.length?related:<p className="text-sm text-muted">{t("No related records.")}</p>}</div>;
  if(recordTabs){
    const tabs=recordTabs.filter(tab=>(pageUIManifest.runtime.recordView.tabs as readonly string[]).includes(tab));
    const titles:Record<string,string>={overview:t("Overview"),properties:t("Properties"),links:t("Links"),history:t("History")};
    const overview=<div className="grid gap-3">{info.lifecycle&&<StatusBar lifecycle={info.lifecycle} state={String(r[info.lifecycle.field]??"")}/>}<PropertyList columns={2} items={info.fields.filter(f=>fields?.includes(f.name)&&["integer","decimal"].includes(f.type)).slice(0,4).map(f=>[f.title,entity.fields[f.name]!.display(r[f.name],r)])}/><PropertyList items={[...view.related,...(view.linked??[])].flatMap(rel=>{const target=source.entity(rel.type);return target?[[target.plural,String(rel.total)] as [string,ReactNode]]:[];})}/></div>;
    const panels:Record<string,ReactNode>={overview,properties,links:related.length?related:<p className="text-sm text-muted">{t("No related records.")}</p>,history:<RecordHistory info={info} history={view.history}/>};
    return <div className="grid min-w-0 gap-3">{header}<ContentTabs key={tabIdentity} label={t("Record view")} value={tabState?.identity===tabIdentity?tabState.value:tabs[0]} onChange={value=>setTab({identity:tabIdentity,value})} items={tabs.map(id=>({id,title:titles[id]!,content:panels[id]}))}/></div>;
  }
  return (
    <div className="grid max-w-5xl grid-cols-[minmax(0,1fr)] gap-4">
      {header}
      {!detailOnly && info.lifecycle && <StatusBar lifecycle={info.lifecycle} state={String(r[info.lifecycle.field] ?? "")} can={can}
        onTransition={onTransition && ((schema) => onTransition(schema, r))} />}
      {!detailOnly && (view.tasks.length > 0 || view.approvals.length > 0 || view.processes.length > 0 || work) && <section aria-label={t("Work on this record")} className="grid grid-cols-[minmax(0,1fr)] gap-3 rounded-md border border-border bg-surface p-3">
        <h2 className="text-sm font-semibold">{t("Work on this record")}</h2>
        {view.tasks.length > 0 && <Tasks list={view.tasks} tasks={tasks} />}
        {view.approvals.length > 0 && <Approvals source={source} approvals={view.approvals} onOpen={onOpen} />}
        {view.processes.length > 0 && <Processes source={source} processes={view.processes} onOpen={onOpen} />}
        {work?.(view)}
      </section>}
      {!detailOnly && info.type === "work.approval" && <ApprovalGraph approval={r as unknown as Api.ApprovalRequest} />}
      {properties}
      {!detailOnly && (view.files.length > 0 || files) && <Files key={JSON.stringify(["files",source.scope,type,id])} attached={view.files} files={files} />}
      {!detailOnly && (view.comments.length > 0 || comments) && <Comments key={JSON.stringify(["comments",source.scope,type,id])} list={view.comments} following={view.following} comments={comments} />}
      {!detailOnly && related}
      {!detailOnly && (view.activity?.length ?? 0) > 0 && (
        <section>
          <h2 className="mb-1 text-sm font-semibold">{t("Activity")}</h2>
          <ol className="grid gap-1 text-sm">
            {view.activity.map((n, i) => (
              <li key={i} className="flex flex-wrap items-baseline gap-2">
                <span className="text-xs text-muted">{new Date(n.at).toLocaleString()}</span>
                <Tag label={n.by} tone="info" />
                {n.title && <span className="font-medium">{n.title}</span>}
                <span className="text-xs text-muted">{n.text}</span>
              </li>
            ))}
          </ol>
        </section>
      )}
      {!detailOnly && <RecordHistory info={info} history={view.history} />}
    </div>
  );
}

/** A standalone projection of the same authorized record view and related windows. */
export function RecordLinks({source,type,id,groups,onOpen}:{source:RecordSource;type:string;id:string;groups:readonly Api.PageRecordLink[];onOpen?:(type:string,r:EntityRecord)=>void}){
 return <RecordPage source={source} type={type} id={id} recordLinks={groups} onOpen={onOpen} linksOnly/>;
}

/** The caller declares lifecycle presentation; this component never emits transitions. */
export function RecordStatus({source,type,id,config}:{source:RecordSource;type:string;id:string;config?:Api.PageStatusTracker}){
 return <RecordPage source={source} type={type} id={id} statusTracker={config} statusOnly/>;
}

type Row = Record<string, unknown>;

/** The cell of a line as people read it: money with its currency, the rest as shown elsewhere. */
const cellOf = (f: FieldInfo, v: unknown) => f.type === "money" && v && typeof v === "object"
  ? `${((v as Money).amount / 100).toFixed(2)} ${(v as Money).currency}` : shown(v);

/**
 * A changed lines field row by row (F-26, F-37): lines added, removed, and
 * changed in some cells. Lines are matched by the first column whose values
 * are unique on both sides (a booking, an account), else by position.
 */
function LinesChange({ field, before, after }: { field: FieldInfo; before: unknown; after: unknown }) {
  const columns = field.fields ?? [];
  const was = Array.isArray(before) ? (before as Row[]) : [], is = Array.isArray(after) ? (after as Row[]) : [];
  const unique = (rows: Row[], name: string) => rows.every((r) => ["string", "number"].includes(typeof r[name]) && r[name] !== "")
    && new Set(rows.map((r) => r[name])).size === rows.length;
  const key = columns.find((c) => c.type !== "money" && unique(was, c.name) && unique(is, c.name))?.name;
  const id = (r: Row, i: number) => String(key ? r[key] : i);
  const text = (r: Row) => columns.filter((c) => r[c.name] !== undefined && r[c.name] !== "" && r[c.name] !== null).map((c) => `${c.title} ${cellOf(c, r[c.name])}`).join(" · ");
  const old = new Map(was.map((r, i) => [id(r, i), r])), now = new Map(is.map((r, i) => [id(r, i), r]));
  const lines: ReactNode[] = [];
  is.forEach((r, i) => {
    const k = id(r, i), prior = old.get(k);
    if (!prior) { lines.push(<li key={`+${k}`} className="text-[var(--tone-success)]">+ {text(r)}</li>); return; }
    const changed = columns.filter((c) => JSON.stringify(prior[c.name]) !== JSON.stringify(r[c.name]));
    if (changed.length) lines.push(<li key={`~${k}`}>{key ? `${r[key]}: ` : `${i + 1}: `}{changed.map((c, j) =>
      <span key={c.name}>{j > 0 && " · "}{c.title} <s className="text-muted">{cellOf(c, prior[c.name])}</s> → {cellOf(c, r[c.name])}</span>)}</li>);
  });
  was.forEach((r, i) => { const k = id(r, i); if (!now.has(k)) lines.push(<li key={`-${k}`} className="text-muted"><s>− {text(r)}</s></li>); });
  return lines.length ? <ul className="ml-3 grid gap-0.5">{lines}</ul> : <span className="text-muted">{t("unchanged")}</span>;
}

const size = (n: number) => (n < 1024 ? `${n} B` : n < 1 << 20 ? `${Math.round(n / 1024)} KB` : `${(n / (1 << 20)).toFixed(1)} MB`);

/** A record's files: download each, add one. */
function Files({ attached, files }: { attached: AttachedFile[]; files?: { upload: (file: File) => Promise<void>; download: (f: AttachedFile) => void } }) {
  const [file,setFile]=useState<File>(),[busy,setBusy]=useState(false),[error,setError]=useState<string>();const epoch=useRef(0),picked=useRef(file);picked.current=file;useEffect(()=>()=>{epoch.current++;},[]);
  const upload=async()=>{const submitted=picked.current,started=epoch.current;if(!submitted||!files||busy)return;setBusy(true);setError(undefined);try{await files.upload(submitted);if(epoch.current===started&&picked.current===submitted){picked.current=undefined;setFile(undefined);}}catch{if(epoch.current===started)setError(t("The attachment could not be confirmed. Your selected file is retained."));}finally{if(epoch.current===started)setBusy(false);}};
  return (
    <section>
      <h2 className="mb-1 text-sm font-semibold">{t("Files")}</h2>
      {files&&<RecordUploader label={t("Add file")} file={file} busy={busy} error={error} onFile={next=>{picked.current=next;setFile(next);setError(undefined);}} onUpload={upload} onClear={()=>{picked.current=undefined;setFile(undefined);setError(undefined);}}/>}
      {attached.length === 0 ? <p className="text-xs text-muted">{t("No files")}</p> :
        <ul className="grid gap-1">
          {attached.map((f) => (
            <li key={f.id} className="flex items-center gap-2 rounded-md border border-border bg-surface px-3 py-1.5 text-sm">
              <button type="button" className="font-medium hover:underline" disabled={!files} onClick={() => files?.download(f)}>{f.name}</button>
              <span className="text-xs text-muted">{f.contentType} · {size(f.size)} · {f.by}</span>
            </li>
          ))}
        </ul>}
    </section>
  );
}

/** A record's comments, oldest first, and a box to add one; following tells of its changes. */
function Comments({ list, following, comments }: { list: RecordComment[]; following: boolean; comments?: { add: (text: string) => Promise<boolean>; follow: (on: boolean) => Promise<void> } }) {
  const [text,setText]=useState(""),[busy,setBusy]=useState(false),[followBusy,setFollowBusy]=useState(false),[error,setError]=useState<string>();const epoch=useRef(0),draft=useRef(text);draft.current=text;useEffect(()=>()=>{epoch.current++;},[]);
  const add=async()=>{const submitted=draft.current,started=epoch.current;if(!comments||busy||!submitted.trim())return;setBusy(true);setError(undefined);try{const confirmed=await comments.add(submitted);if(epoch.current!==started)return;if(confirmed&&draft.current===submitted){draft.current="";setText("");}else if(!confirmed)setError(t("The comment could not be confirmed. Your draft is retained."));}catch{if(epoch.current===started)setError(t("The comment could not be confirmed. Your draft is retained."));}finally{if(epoch.current===started)setBusy(false);}};
  const follow=async(on:boolean)=>{const started=epoch.current;if(!comments||followBusy)return;setFollowBusy(true);setError(undefined);try{await comments.follow(on);}catch{if(epoch.current===started)setError(t("Following this record could not be confirmed."));}finally{if(epoch.current===started)setFollowBusy(false);}};
  return <RecordComments list={list} text={text} following={following} busy={busy} followBusy={followBusy} error={error} onText={comments?next=>{draft.current=next;setText(next);}:undefined} onAdd={comments?add:undefined} onFollow={comments?follow:undefined}/>;
}

const processTone = (state: string) =>
  state === "done" ? "success" : state === "stuck" || state === "compensated" || state === "canceled" ? "danger" : state === "compensating" ? "warning" : "info";

const approvalTone = (state: string) => state === "approved" ? "success" : state === "pending" ? "warning" : state === "withdrawn" ? "neutral" : "danger";

/** The approvals asked for a record: each request, its state, whom it waits for, and who rejected it and why. */
function Approvals({ source, approvals, onOpen }: { source: RecordSource; approvals: Api.ApprovalRequest[]; onOpen?: (type: string, r: EntityRecord) => void }) {
  const state = source.entity("work.approval")?.fields.find((f) => f.name === "state");
  return (
    <section aria-label={t("Approvals")}>
      <h2 className="mb-1 text-sm font-semibold">{t("Approvals")}</h2>
      {approvals[0] && <div className="mb-1.5"><ApprovalGraph approval={approvals[0]} /></div>}
      <ul className="grid gap-1">
        {approvals.map((a) => {
          const level = a.levels[a.level];
          const by = a.levels.flatMap((l) => l.approved.map((m) => l.decidedBy?.[m] ? t("{delegate} for {approver}", { delegate: l.decidedBy[m]!, approver: m }) : m));
          return (
            <li key={a.id} className="flex flex-wrap items-center gap-2 rounded-md border border-border bg-surface px-3 py-2 text-sm">
              {onOpen ? <Button size="sm" variant="ghost" onClick={() => onOpen("work.approval", a)}>{a.title}</Button> : <span className="font-medium">{a.title}</span>}
              <Tag label={state?.choiceTitles?.[state.choices?.indexOf(a.state) ?? -1] ?? a.state} tone={approvalTone(a.state)} />
              {a.state === "pending" && level && <span className="text-xs text-muted">{t("waiting for {level}: {approvers}", { level: level.title, approvers: level.approvers.join(", ") })}</span>}
              {by.length > 0 && <span className="text-xs text-muted">{t("approved by {members}", { members: by.join(", ") })}</span>}
              {a.rejectedBy && <span className="text-xs">{t("rejected by {member}", { member: a.rejectedBy })}{a.outcome ? `: ${a.outcome}` : ""}</span>}
              {a.state === "refused" && a.outcome && <span className="text-xs">{a.outcome}</span>}
            </li>
          );
        })}
      </ul>
    </section>
  );
}

/** The open tasks about a record: what is asked, by when, and its answers as buttons. */
export function Tasks({ list, tasks }: { list: Api.InboxTask[]; tasks?: { answer: (task: Api.InboxTask, answer?: string) => Promise<void>; actions?: (task: Api.InboxTask) => ReactNode } }) {
  const [busy, setBusy] = useState<string>();
  const answer = async (task: Api.InboxTask, a?: string) => { setBusy(task.id); try { await tasks?.answer(task, a); } finally { setBusy(undefined); } };
  return (
    <section aria-label={t("Waiting for you")}>
      <h2 className="mb-1 text-sm font-semibold">{t("Waiting for you")}</h2>
      <ul className="grid gap-1">
        {list.map((task) => (
          <li key={task.id} className="grid gap-1.5 rounded-md border border-[var(--tone-warning)] bg-surface px-3 py-2 text-sm">
            <span className="font-medium">{task.title}</span>
            {task.body && <span className="whitespace-pre-wrap text-xs text-muted">{task.body}</span>}
            {tasks && <span className="flex flex-wrap gap-1.5">
              {tasks.actions?.(task) ?? (task.answers?.length ? task.answers : [undefined]).map((a, i) =>
                <Button key={a ?? "done"} size="sm" variant={a === task.answers?.[0] ? "primary" : undefined} disabled={busy === task.id} onClick={() => void answer(task, a)}>{task.answerTitles?.[i] ?? a ?? t("Done")}</Button>)}
            </span>}
          </li>
        ))}
      </ul>
    </section>
  );
}

/** An approval chain drawn as a process (#122): the requester, each level with its
 * approvers and who decided, and how it ended — in BPMN shapes (ADR-0033). */
function ApprovalGraph({ approval: a }: { approval: Api.ApprovalRequest }) {
  const nodes: FlowStepNode[] = [{ id: "requester", label: a.requester, detail: t("asked"), notation: "event-start", tone: "success" }];
  const edges: FlowStepEdge[] = [];
  let previous = "requester";
  a.levels.forEach((l, i) => {
    const decided = l.approved.map((m) => l.decidedBy?.[m] ? t("{delegate} for {approver}", { delegate: l.decidedBy[m]!, approver: m }) : m);
    const rejectedHere = a.state === "rejected" && i === a.level;
    const here = a.state === "pending" && i === a.level;
    nodes.push({
      id: `level-${i}`, label: l.title, current: here, notation: "user-task",
      detail: rejectedHere ? t("rejected by {member}", { member: a.rejectedBy ?? "" }) : decided.length ? t("approved by {members}", { members: decided.join(", ") }) : l.approvers.join(", "),
      tone: rejectedHere ? "danger" : here ? "info" : i < a.level || a.state === "approved" || a.state === "refused" ? "success" : undefined,
    });
    edges.push({ from: previous, to: `level-${i}` });
    previous = `level-${i}`;
  });
  const ended = a.state !== "pending";
  const outcomes: Record<string, string> = { approved: t("Approved"), rejected: t("Rejected"), refused: t("Refused when run"), withdrawn: t("Withdrawn") };
  nodes.push({ id: "outcome", label: ended ? outcomes[a.state] ?? a.state : t("Outcome"), detail: a.outcome || undefined, notation: "event-end",
    tone: a.state === "approved" ? "success" : a.state === "rejected" || a.state === "refused" ? "danger" : a.state === "withdrawn" ? "neutral" : undefined });
  edges.push({ from: previous, to: "outcome", dashed: !ended });
  return <FlowSteps nodes={nodes} edges={edges} height={150} label={t("Approvals")} storeKey={`approvals:${a.id}`} />;
}

/** The processes about a record: each flow, its state, and the steps it stands at. */
function Processes({ source, processes, onOpen }: { source: RecordSource; processes: RecordView["processes"]; onOpen?: (type: string, r: EntityRecord) => void }) {
  const state = source.entity("flow.instance")?.fields.find((f) => f.name === "state");
  return (
    <section aria-label={t("Processes")}>
      <h2 className="mb-1 text-sm font-semibold">{t("Processes")}</h2>
      <ul className="grid gap-1">
        {processes.map((p) => (
          <li key={p.id} className="flex flex-wrap items-center gap-2 rounded-md border border-border bg-surface px-3 py-2 text-sm">
            <button type="button" className="font-medium hover:underline" onClick={() => onOpen?.("flow.instance", p)}>{p.title}</button>
            <Tag label={state?.choiceTitles?.[state.choices?.indexOf(p.state) ?? -1] ?? p.state} tone={processTone(p.state)} />
            {!!p.tokens?.length && <span className="text-xs text-muted">{t("at {steps}", { steps: p.tokens.map((x) => x.step).join(", ") })}</span>}
          </li>
        ))}
      </ul>
    </section>
  );
}

/** A task offered to a member; `answers` are what a flow's question offers (ADR-0007), none: done. */
export type InboxTask = Api.InboxTask;

/** A member's open tasks (ADR-0007), overdue first; each can open what it is about and offers the actions the app gives it. */
export function Inbox({ tasks, onOpen, actions, empty = t("Nothing for you") }: {
  tasks: InboxTask[]; onOpen?: (task: InboxTask) => void; actions?: (task: InboxTask) => ReactNode; empty?: string;
}) {
  if (tasks.length === 0) return <p className="text-sm text-muted">{empty}</p>;
  const now = Date.now();
  return (
    <ul className="grid max-w-3xl gap-2">
      {tasks.map((task) => {
        const late = !!task.due && Date.parse(task.due) < now;
        return (
          <li key={task.id} className="rounded-md border border-border bg-surface p-3 text-sm">
            <div className="flex flex-wrap items-center gap-2">
              <button type="button" className="text-left font-medium hover:underline" onClick={() => onOpen?.(task)}>{task.title}</button>
              {late && <Tag label={t("overdue")} tone="danger" />}
              {task.due && !late && <span className="text-xs text-muted">{t("due {when}", { when: new Date(task.due).toLocaleString() })}</span>}
              {task.assignee && <span className="text-xs text-muted">{t("taken by")} {task.assignee}</span>}
              <span className="ml-auto flex gap-1">{actions?.(task)}</span>
            </div>
            {task.body && <p className="mt-1 whitespace-pre-wrap text-xs text-muted">{task.body}</p>}
          </li>
        );
      })}
    </ul>
  );
}
