import { renderingLimits } from "../gen/presentation-limits";
import type { DetailPresentationOptions, RecordLinkOptions, StatusTrackerOptions } from "../presentation";
import { Printer, Star } from "lucide-react";
import { Barcode } from "./barcode";
import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import { useViewVisible } from "../shell/ViewVisibility";
import { StructuredEditor } from "../components/StructuredEditor";
import { DataTable } from "../components/DataTable";
import { ContentTabs } from "../layout/ContentTabs";
import { useActionAvailability } from "./ActionAvailability";
import { FormatPill, HeaderCommands, ObjectHeader, toneOf, type HeaderPill } from "./ObjectView";
import type { ContextCommand } from "../components/CommandMenu";
import { PropertyList } from "../components/EntityCard";
import { FieldProvenance } from "./FieldProvenance";
import { Tag } from "../components/StatusTag";
import { columnsFor, valueOf, type Entity } from "../fields/entity";
import { Button } from "../primitives/button";
import { RecordReadContext } from "./RecordChip";
import { t } from "../i18n";
import { humanizeKernelError } from "../lib/errors";
import type { Api } from "@platform/kernel";
import {StatusBar, displayOf, entityFrom, listed} from "./RecordEntity";
import {RecordHistory} from "./RecordHistory";
import {ApprovalGraph, Approvals, Comments, Files, Processes, Tasks} from "./RecordPanels";
import { useWorkspace } from "../shell/WorkspaceContext";
import type {AttachedFile, EntityRecord, RecordSource, RecordView} from "./Records";

export function useRecordView(source: RecordSource, type: string, id?: string, reload = 0) {
  const identity = JSON.stringify([source.scope, type, id]);
  const key = JSON.stringify([identity, source.revision, reload]);
  const from = useRef(source);
  from.current = source;
  const sourceIdentity = source.scope ?? source;
  const [result, setResult] = useState<{ source: typeof sourceIdentity; identity: string; key: string; view?: RecordView; error?: string; loading: boolean }>();
  const request = useRef(0);
  const refresh = useCallback(() => {
    if (!id) return;
    const epoch = ++request.current, source = from.current, scope = source.scope;
    setResult(previous => ({ source: sourceIdentity, identity, key, view: previous?.source === sourceIdentity && previous.identity === identity ? previous.view : undefined, loading: true }));
    source.get(type, id).then(view => {
      if (view.record.id !== id) throw new Error("Record identity mismatch");
      if (request.current === epoch && source.scope === scope) setResult({ source: sourceIdentity, identity, key, view, loading: false });
    }).catch(error => {
      if (request.current === epoch && source.scope === scope) setResult({ source: sourceIdentity, identity, key, error: error instanceof Error ? error.message : String(error), loading: false });
    });
  }, [sourceIdentity, type, id, identity, key]);
  const visible = useViewVisible();
  useEffect(() => {
    if (!visible || !id) return;
    refresh();
    const stop = from.current.watchRecord?.(type, id, refresh);
    return () => { request.current++; stop?.(); };
  }, [refresh, visible, type, id]);
  const current = result?.source === sourceIdentity && result.identity === identity ? result : undefined;
  return { view: current?.view, error: current?.key === key ? current.error : undefined, loading: !!id && (current?.key !== key || current.loading), refresh };
}

export function RecordPage({ source, type, id, actions, menu, onOpen, reload = 0, can, onTransition, files, comments, tasks, fields, work, detailOnly = false,detailPresentation,recordTabs,recordLinks,linksOnly=false,statusTracker,statusOnly=false }: {
  detailPresentation?:DetailPresentationOptions;recordTabs?:readonly string[];recordLinks?:readonly RecordLinkOptions[];linksOnly?:boolean;statusTracker?:StatusTrackerOptions;statusOnly?:boolean;
  work?: (view: RecordView) => ReactNode;
  tasks?: { answer: (task: Api.InboxTask, answer?: string) => Promise<void>; actions?: (task: Api.InboxTask) => ReactNode };
  source: RecordSource; type: string; id: string; actions?: (r: EntityRecord) => ReactNode; menu?: (r: EntityRecord) => ContextCommand[];
  fields?: string[];
  detailOnly?: boolean;
  files?: { upload: (file: File) => Promise<void>; download: (f: AttachedFile) => void };
  comments?: { add: (text: string) => Promise<boolean>; follow: (on: boolean) => Promise<void> };
  onOpen?: (type: string, r: EntityRecord) => void; reload?: number;
  can?: (schema: string) => boolean; onTransition?: (schema: string, r: EntityRecord) => void;
}) {
  const tabIdentity=JSON.stringify([source.scope,type,id,recordTabs]);
  const [tabState,setTab]=useState<{identity:string;value:string}>();
  const info = source.entity(type);
  const { favorites, toggleFavorite } = useWorkspace();
  const pinned = favorites.includes(`${type}:${id}`);
  const { view, error, loading, refresh: load } = useRecordView(source, type, id, reload);
  const entity = useMemo(() => (info ? entityFrom(info,{},source,onOpen ? (type,id) => onOpen(type,{id} as EntityRecord) : undefined) : undefined), [info,source,onOpen]);
  const checkLifecycle = info?.lifecycle, checkState = view && checkLifecycle ? String(view.record[checkLifecycle.field] ?? "") : "";
  const availability = useActionAvailability(source, view && onTransition && checkLifecycle && !detailOnly && !recordTabs ? { type, id, revision: view.record.revision } : undefined,
    checkLifecycle ? checkLifecycle.transitions.filter(x => x.from.includes(checkState) && (can?.(x.schema) ?? false)).map(x => x.schema) : []);
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
  const currentRecord=(content:ReactNode)=><RecordReadContext.Provider value={{scope:source.scope??source,type,record:r}}>{content}</RecordReadContext.Provider>;
  if(statusOnly){
   const l=info.lifecycle,value=statusTracker,field=info.fields.find(f=>f.name===value?.field);
   if(!l||!value||value.field!==l.field||!field||!value.stages.length||value.stages.some(name=>!l.states.some(s=>s.name===name))||typeof r[value.field]!=="string")return <p role="alert" className="text-sm text-muted">{t("Lifecycle status is unavailable.")}</p>;
   const states=value.stages.map(name=>l.states.find(s=>s.name===name)!);
   return <div className="grid min-w-0 gap-2"><StatusBar lifecycle={{...l,states}} state={r[value.field] as string} tracker/>{!value.stages.includes(r[value.field] as string)&&<p className="text-xs text-muted">{t("Current state is outside the displayed stages.")}</p>}</div>;
  }

  const formats=view.formats??{};
  const lifecycle=info.lifecycle,stateName=lifecycle?String(r[lifecycle.field]??""):"";
  const pills:HeaderPill[]=[];
  if(lifecycle&&stateName){const state=lifecycle.states.find(s=>s.name===stateName),format=formats[lifecycle.field];pills.push({key:"lifecycle",label:format?.label||state?.title||stateName,tone:toneOf(format?.tone??state?.tone),icon:format?.icon,title:info.fields.find(f=>f.name===lifecycle.field)?.title});}
  for(const f of info.fields){const format=formats[f.name];if(format&&f.name!==lifecycle?.field)pills.push({key:f.name,label:format.label||entity.fields[f.name]?.text(valueOf(entity,f.name,r) as never)||"—",tone:toneOf(format.tone),icon:format.icon,title:f.title});}
  const full=!detailOnly&&!recordTabs;
  const transitions=full&&lifecycle&&onTransition?lifecycle.transitions.filter(x=>x.from.includes(stateName)&&!loading&&(can?.(x.schema)??false)):[];
  const commands:ContextCommand[]=transitions.map(x=>{const verdict=availability[x.schema],blocked=verdict?.allowed===false;return {id:x.schema,label:x.title,disabled:blocked,reason:blocked?verdict.reason||t("This action is not available for this record."):undefined,run:()=>onTransition!(x.schema,r)};});
  const header=<ObjectHeader sticky={full} icon={info.icon} color={info.color} title={displayOf(info, r)} pills={pills.slice(0,3)}
    subtitle={<>{info.title} · {r.id} {t("· rev")} {r.revision}</>}
    tags={<>
      {r.archived && <Tag label="archived" />}
      {view.effective===false&&<Tag label={t("Expired")} />}
      {!!info.versionPins?.length&&(r.versionPins as Api.RecordVersionPin[]|undefined)?.map(pin=><Tag key={pin.reference} label={t("Pinned version: {version}",{version:String(pin.version)})} />)}
      {info.versioning?.validUntil&&Boolean(r[info.versioning.validUntil])&&<Tag label={t("Valid until: {date}",{date:String(r[info.versioning.validUntil])})} />}
    </>}
    status={loading&&<span role="status" className="text-xs text-muted">{t("Refreshing record…")}</span>}
    actions={!detailOnly?<><fieldset disabled={loading} className="m-0 flex min-w-0 flex-wrap items-center gap-1 border-0 p-0"><HeaderCommands commands={commands} menu={menu?.(r)??[]} disabled={loading}>{actions?.(r)}</HeaderCommands></fieldset><Button size="icon" aria-label={pinned ? t("Remove from favorites") : t("Add to favorites")} aria-pressed={pinned} onClick={() => toggleFavorite(`${type}:${id}`)}><Star fill={pinned ? "currentColor" : "none"} /></Button></> :undefined} />;
  const shownValue=(name:string,row:EntityRecord=r,target:Entity<EntityRecord>=entity)=>{
    const format=row===r?formats[name]:undefined;
    if(format)return <FormatPill format={format} text={target.fields[name]?.text(valueOf(target,name,row) as never)??""}/>;
    return target.fields[name]!.display(row[name] as never,row);
  };
  const barcode=detailPresentation?.barcode?detailPresentation.barcode==="id"?r.id:String(r[detailPresentation.barcode]??""):"";
  const extensions=(view.related??[]).flatMap(rel=>{const relInfo=source.entity(rel.type);const row=rel.records[0];if(!relInfo||!row||rel.field!=="base"||(relInfo as {extends?:string}).extends!==type)return [];const relEntity=entityFrom(relInfo,{},source,onOpen ? (type,id) => onOpen(type,{id} as EntityRecord) : undefined);return [{rel,relInfo,relEntity,row}];});
  const sourced=new Set(source.provenance?(info.datasources??[]).flatMap(binding=>binding.columns.map(column=>column.attribute)):[]);
  const properties=(<section className="rounded-md border border-border bg-surface p-3" data-print-label={detailPresentation?.print||undefined}>
        {barcode&&<div className="mb-3 flex justify-center overflow-hidden"><Barcode text={barcode}/></div>}
        {detailPresentation?.print&&<div className="mb-3 flex justify-end print:hidden"><Button size="sm" onClick={()=>window.print()}><Printer/>{t("Print")}</Button></div>}
        <PropertyList columns={detailPresentation?.columns as 1|2|3|4|undefined} items={[...[...new Set(fields??info.fields.map(f=>f.name))].flatMap(name=>{const f=info.fields.find(f=>f.name===name);return !f||detailPresentation?.hideNull&&!formats[name]&&(r[name]===undefined||r[name]===null||r[name]==="")?[]:[[f.title,sourced.has(name)?<span className="inline-flex min-w-0 flex-wrap items-center gap-1">{shownValue(name)}<FieldProvenance source={source} type={type} id={id} revision={r.revision} field={name} label={f.title}/></span>:shownValue(name)] as [string,ReactNode]];}),
          ...(!detailOnly ? [[t("Created"), `${r.created.by ?? ""} · ${r.created.at ? new Date(r.created.at).toLocaleString() : ""}`],
            [t("Changed"), `${r.changed.by ?? ""} · ${r.changed.at ? new Date(r.changed.at).toLocaleString() : ""}`]] as [string, ReactNode][] : [])]} />
        {extensions.map(({rel,relInfo,relEntity,row})=><div key={rel.type} className="mt-3 border-t border-border pt-3">
          <h3 className="mb-1 text-xs font-semibold text-muted">{relInfo.title}</h3>
          <PropertyList columns={detailPresentation?.columns as 1|2|3|4|undefined} items={relInfo.fields.filter(f=>f.name!=="base"&&!(detailPresentation?.hideNull&&(row[f.name]===undefined||row[f.name]===null||row[f.name]==="")))
            .map(f=>[f.title,shownValue(f.name,row,relEntity)] as [string,ReactNode])}/>
        </div>)}
        {!detailOnly && <div className="mt-3 border-t border-border pt-3"><StructuredEditor key={type+":"+id} scope={type+":"+id} value={Object.fromEntries(Object.entries(r).filter(([name])=>["id","revision","created","changed","archived"].includes(name)||info.fields.some(field=>field.name===name)))} readOnly><span className="text-xs text-muted">{t("View the readable record as JSON or YAML.")}</span></StructuredEditor></div>}
      </section>);
  const groups=detailOnly&&!recordTabs&&!linksOnly?[]:recordLinks===undefined?[...view.related,...(view.linked??[])].map(rel=>({rel,title:undefined as string|undefined})):recordLinks.flatMap(group=>{const target=source.entity(group.object.name),field=target?.fields.find(f=>f.name===group.field&&f.type==="reference"&&f.ref===type&&!!f.inverse),rel=view.related.find(r=>r.type===group.object.name&&r.field===group.field);return field&&target?.app===group.object.app&&rel?[{rel,title:group.title}]:[];});
  const shownGroups=detailOnly&&!recordTabs&&!linksOnly?[]:groups.filter(({rel})=>!extensions.some(x=>x.rel===rel));
  const related=shownGroups.flatMap(({rel,title}) => {
    const relInfo=source.entity(rel.type),relEntity=relInfo&&entityFrom(relInfo,{},source,onOpen ? (type,id) => onOpen(type,{id} as EntityRecord) : undefined);if(!relEntity)return [];
    return [<section key={`${rel.type}.${rel.field}`}>
      <h2 className="mb-1 text-sm font-semibold">{title||relInfo.plural} <span className="font-normal text-muted">({rel.total}{rel.field === "link" ? t(", linked") : rel.relation ? <>{" · "}{rel.relation}</> : <>{t(", by")} {rel.field}</>})</span></h2>
      <DataTable storageKey={`related:${type}:${rel.type}:${rel.field}`} totalRows={rel.total} manualSorting data={rel.records} columns={[{id:"id",header:"ID",accessorKey:"id",meta:{width:130}},...columnsFor(relEntity,listed(relEntity))] as never} getRowId={(x:EntityRecord)=>x.id} height={Math.min(40+rel.records.length*28,260)} searchable={false} onRowClick={onOpen&&((x:EntityRecord)=>onOpen(rel.type,x))} empty={t("None")}/>
    </section>];
  });
  if(linksOnly)return currentRecord(<div className="grid min-w-0 gap-3">{related.length?related:<p className="text-sm text-muted">{t("No related records.")}</p>}</div>);
  if(recordTabs){
    const tabs=recordTabs.filter(tab=>(renderingLimits.runtime.recordView.tabs as readonly string[]).includes(tab));
    const titles:Record<string,string>={overview:t("Overview"),properties:t("Properties"),links:t("Links"),history:t("History")};
    const overview=<div className="grid gap-3">{info.lifecycle&&<StatusBar lifecycle={info.lifecycle} state={String(r[info.lifecycle.field]??"")}/>}<PropertyList columns={2} items={info.fields.filter(f=>fields?.includes(f.name)&&["integer","decimal"].includes(f.type)).slice(0,4).map(f=>[f.title,entity.fields[f.name]!.display(r[f.name],r)])}/><PropertyList items={[...view.related,...(view.linked??[])].flatMap(rel=>{const target=source.entity(rel.type);return target?[[target.plural,String(rel.total)] as [string,ReactNode]]:[];})}/></div>;
    const panels:Record<string,ReactNode>={overview,properties,links:related.length?related:<p className="text-sm text-muted">{t("No related records.")}</p>,history:<><RecordDeliveries deliveries={view.deliveries} onOpen={onOpen}/><RecordHistory info={info} history={view.history}/></>};
    return currentRecord(<div className="grid min-w-0 gap-3">{header}<ContentTabs key={tabIdentity} label={t("Record view")} value={tabState?.identity===tabIdentity?tabState.value:tabs[0]} onChange={value=>setTab({identity:tabIdentity,value})} items={tabs.map(id=>({id,title:titles[id]!,content:panels[id]}))}/></div>);
  }
  if(detailOnly)return currentRecord(<div className="grid max-w-5xl grid-cols-[minmax(0,1fr)] gap-4">{header}{properties}</div>);
  const count=(title:string,n:number)=><>{title}{n>0&&<span className="rounded-full bg-primary/15 px-1.5 text-2xs text-primary">{n}</span>}</>;
  const linkCount=shownGroups.reduce((n,{rel})=>n+rel.total,0);
  const hasFiles=view.files.length>0||!!files,hasComments=view.comments.length>0||!!comments;
  const panes:{id:string;title:ReactNode;content:ReactNode}[]=[
    {id:"overview",title:t("Overview"),content:<div className="grid min-w-0 gap-4">
      {info.lifecycle&&<StatusBar lifecycle={info.lifecycle} state={stateName}/>}
      {work?.(view)}
      {info.type === "work.approval" && <ApprovalGraph approval={r as unknown as Api.ApprovalRequest} />}
      {properties}
    </div>},
    {id:"links",title:count(t("Links"),linkCount),content:related.length?<div className="grid min-w-0 gap-4">{related}</div>:<p className="text-sm text-muted">{t("No related records.")}</p>},
    ...(view.tasks.length>0?[{id:"tasks",title:count(t("Tasks"),view.tasks.length),content:<Tasks list={view.tasks} tasks={tasks}/>}]:[]),
    ...(view.approvals.length>0?[{id:"approvals",title:count(t("Approvals"),view.approvals.length),content:<Approvals source={source} approvals={view.approvals} onOpen={onOpen}/>}]:[]),
    ...(view.processes.length>0?[{id:"processes",title:count(t("Processes"),view.processes.length),content:<Processes source={source} processes={view.processes} onOpen={onOpen}/>}]:[]),
    ...(hasFiles?[{id:"files",title:count(t("Files"),view.files.length),content:<Files key={JSON.stringify(["files",source.scope,type,id])} attached={view.files} files={files} />}]:[]),
    ...(hasComments?[{id:"comments",title:count(t("Comments"),view.comments.length),content:<Comments key={JSON.stringify(["comments",source.scope,type,id])} list={view.comments} following={view.following} comments={comments} />}]:[]),
    {id:"history",title:t("History"),content:<div className="grid min-w-0 gap-4">
      <RecordDeliveries deliveries={view.deliveries} onOpen={onOpen}/>
      {(view.activity?.length ?? 0) > 0 && <ActivityNotes notes={view.activity} />}
      <RecordHistory info={info} history={view.history} />
    </div>},
  ];
  return currentRecord(
    <div className="grid max-w-5xl grid-cols-[minmax(0,1fr)] gap-3">
      {header}
      <ContentTabs compact key={tabIdentity} label={t("Record view")} value={tabState?.identity===tabIdentity?tabState.value:view.tasks.length>0?"tasks":undefined} onChange={value=>setTab({identity:tabIdentity,value})}
        items={panes.map(pane=>({...pane,content:<div className="pt-3">{pane.content}</div>}))} />
    </div>
  );
}

function ActivityNotes({ notes }: { notes: Api.Note[] }) {
  return (
    <section>
      <h2 className="mb-1 text-sm font-semibold">{t("Activity")}</h2>
      <ol className="grid gap-1 text-sm">
        {notes.map((n, i) => (
          <li key={i} className="flex flex-wrap items-baseline gap-2">
            <span className="text-xs text-muted">{new Date(n.at).toLocaleString()}</span>
            <Tag label={n.by} tone="info" />
            {n.title && <span className="font-medium">{n.title}</span>}
            <span className="text-xs text-muted">{n.text}</span>
          </li>
        ))}
      </ol>
    </section>
  );
}

export function RecordDeliveries({deliveries,onOpen}:{deliveries?:Api.RecordDelivery[];onOpen?:(type:string,r:EntityRecord)=>void}){
 if(!deliveries?.length)return null;
 return <section className="grid gap-2" aria-label={t("Delivery results")}><h2 className="text-sm font-semibold">{t("Delivery results")}</h2><p className="text-xs text-muted">{t("Business acceptance and delivery are separate results.")}</p><ul className="grid gap-2">{deliveries.map(delivery=><li key={delivery.id} className="flex flex-wrap items-baseline gap-2 rounded-md border border-border p-2 text-sm"><span>{delivery.event}</span><Tag label={t(delivery.state)} tone={delivery.state==="delivered"?"success":["rejected","failed"].includes(delivery.state)?"danger":"info"}/><time className="text-xs text-muted" dateTime={delivery.at}>{new Date(delivery.at).toLocaleString()}</time>{delivery.message&&<p className="w-full">{t(delivery.message)}</p>}{delivery.receipt&&onOpen&&<Button size="sm" onClick={()=>{const [type,...id]=delivery.receipt!.split("/");onOpen(type!,{id:id.join("/")} as EntityRecord);}}>{t("Open receipt")}</Button>}</li>)}</ul></section>;
}

export function RecordLinks({source,type,id,groups,onOpen}:{source:RecordSource;type:string;id:string;groups:readonly RecordLinkOptions[];onOpen?:(type:string,r:EntityRecord)=>void}){
 return <RecordPage source={source} type={type} id={id} recordLinks={groups} onOpen={onOpen} linksOnly/>;
}

export function RecordStatus({source,type,id,config}:{source:RecordSource;type:string;id:string;config?:StatusTrackerOptions}){
 return <RecordPage source={source} type={type} id={id} statusTracker={config} statusOnly/>;
}
