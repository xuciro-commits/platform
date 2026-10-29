// Frozen system records remain readable through their original owner. Only
// original unanswered requests can be checked; this console creates no work.
import {useCallback,useEffect,useRef,useState,type ReactNode} from "react";
import type {Api,Entry,EdgeClient} from "@platform/kernel";
import {useQuery} from "@tanstack/react-query";
import {useHost,useReadQuery} from "@platform/app";
import {t as translate,Panel,Button,Form,Input,Disclosure,DataTable} from "@platform/ui";

const Empty=({children}:{children:ReactNode})=><p className="text-muted">{children}</p>;
const Loading=({what}:{what:string})=><p className="text-muted">{translate("Loading…")} {what}</p>;
const Note=({children}:{children:ReactNode})=><p className="text-sm text-muted">{children}</p>;
const Problem=({children}:{children:ReactNode})=><p role="alert" className="text-sm text-danger">{children}</p>;
type Outcome={ok:boolean;pending?:boolean;key:string;code?:string;message?:string};
const unanswered=(entry:Entry)=>["SUBMISSION_STATE_PENDING","SUBMISSION_STATE_SENDING","SUBMISSION_STATE_UNKNOWN"].includes(entry.state);
const outcomeOf=(entry:Entry):Outcome=>({ok:entry.state==="SUBMISSION_STATE_CONFIRMED",pending:unanswered(entry),key:entry.submission.idempotencyKey!,code:unanswered(entry)?undefined:entry.outcome,message:entry.reason});
async function resumeRequest(client:EdgeClient,key:string):Promise<Outcome>{
 await client.send(key);
 const entry=client.authorities.outbox.find(entry=>entry.submission.idempotencyKey===key);
 if(!entry)throw Error("The original request is unavailable.");
 return outcomeOf(entry);
}

type FileRow={path:string;content?:string;revision:number;digest:string;size:number;deleted?:boolean};
type ChangeFile={path:string;content?:string;digest:string;deleted?:boolean;before?:{digest:string;content:string}};
type ChangeRow={id:string;title:string;state:string;origin?:string;release?:number;rationale?:string;files?:ChangeFile[];diagnostics?:string;created?:{by:string};base?:number;sourceDigest?:string;assets?:Api.AssetRef[];prompt?:string;agent?:string;run?:string;approvedBy?:string;approvedAt?:string};
type ReleaseRow={id:string;version:number;state:string;change:string;kind?:string;diagnostics?:string};
type ActivationRow={id:string;release:number;from?:number;result:string;window?:number;at:string;evidence?:string;failure?:string};
type Receipts={resume:(key:string)=>Promise<Outcome>;busy:boolean;report:ReactNode};
const assetKey=(ref:Api.AssetRef)=>`${ref.app}/${ref.kind}/${ref.name}`;
const fileRead=(path:string)=>`/v1/system-file?${new URLSearchParams({path})}`;
function useText(){return translate;}
function useLiveRead<T>(path:string){
 const load=useReadQuery<T>(path),reload=useCallback(()=>{void load.refetch();},[load.refetch]);
 return {data:load.data,loading:load.isLoading,error:load.error?.message,reload};
}
function useReceipts(reload:()=>void):Receipts{
 const {client}=useHost(),t=useText(),lock=useRef(false),[busy,setBusy]=useState(false),[said,setSaid]=useState<Outcome>();
 const resume=useCallback(async(key:string):Promise<Outcome>=>{
  if(lock.current)return {ok:false,pending:true,key,message:t("The request is already sending.")};
  lock.current=true;setBusy(true);setSaid(undefined);
  try{const outcome=await resumeRequest(client,key);setSaid(outcome);reload();return outcome;}
  catch(error){const outcome={ok:false,key,message:error instanceof Error?error.message:String(error)};setSaid(outcome);return outcome;}
  finally{lock.current=false;setBusy(false);}
 },[client,reload]);
 const message=said?.pending?t("The request is saved, but no final receipt has arrived. Check the original request below."):said?.ok?t("Accepted."):[said?.code,said?.message].filter(Boolean).join(": ");
 return {resume,busy,report:said?(said.ok||said.pending?<Note>{message}</Note>:<Problem>{message||t("The host refused this decision.")}</Problem>):null};
}

export function SystemConsole(){
 const host=useHost(),t=useText(),changes=useLiveRead<ChangeRow[]>("/v1/system-changes"),releases=useLiveRead<ReleaseRow[]>("/v1/system-releases"),activations=useLiveRead<ActivationRow[]>("/v1/system-activations");
 const reload=useCallback(()=>{changes.reload();releases.reload();activations.reload();},[changes.reload,releases.reload,activations.reload]);
 const receipts=useReceipts(reload);
 useEffect(()=>host.client.enableLiveReads(),[host.client]);
 return <>
  <h1>{t("System history")}</h1>
  <Note>{t("System source editing, proposals, approvals, builds and new activations are frozen. Existing records and release evidence remain available for diagnosis.")}</Note>
  <p><a href="#/control-panel">{t("Control Panel")}</a> · <a href="#/host-overview">{t("Host Console")}</a></p>
  <Panel title={t("Operational records")} description={t("Read the original audit, usage, work and qualified training records. Configuration and task controls stay in their owner consoles.")}><div className="flex flex-wrap gap-3"><a href="#/audit">{t("Audit")}</a><a href="#/ai-usage">{t("AI usage")}</a><a href="#/automation-operations">{t("Work queue")}</a><a href="#/ai-configuration?section=training">{t("Training candidates")}</a></div></Panel>
  <Note>{t("Use the static control panel for authorized configuration and the host console for maintenance. Opening a historical record does not start a build or activation.")}</Note>
  {receipts.report}<PendingRequests receipts={receipts}/>
  <Changes rows={changes.data??[]} releases={releases.data??[]} activations={activations.data??[]} loading={changes.loading||releases.loading||activations.loading} error={changes.error||releases.error||activations.error} reload={reload}/>
  <Files/>
 </>;
}
function PendingRequests({receipts}:{receipts:Receipts}){
 const host=useHost(),t=useText(),entries=host.client.authorities.outbox.filter(entry=>unanswered(entry)&&((entry.submission.schema?.name??"").startsWith("system.")||entry.submission.schema?.name==="agent.run.start"));
 if(!entries.length)return null;
 return <Panel title={t("Unconfirmed requests")}><Note>{t("The host may already have saved these requests. Checking keeps their original keys and inputs; new system work is refused.")}</Note><ul>{entries.map(entry=><li key={entry.submission.idempotencyKey}><code>{entry.submission.schema?.name} · {entry.submission.target?.id}</code> <Button disabled={receipts.busy} onClick={()=>void receipts.resume(entry.submission.idempotencyKey!)}>{t("Check the original request")}</Button></li>)}</ul></Panel>;
}
function Changes({rows,releases,activations,loading,error,reload}:{rows:ChangeRow[];releases:ReleaseRow[];activations:ActivationRow[];loading:boolean;error?:string;reload:()=>void}){
 const t=useText(),[open,setOpen]=useState<string>(),selected=rows.find(row=>row.id===open);
 return <Panel title={t("System changes")} actions={<Button onClick={reload}>{t("Refresh")}</Button>}>
  {loading&&!rows.length?<Loading what={t("System changes")}/>:error?<Problem>{error}</Problem>:!rows.length?<Empty>{t("No system change is recorded.")}</Empty>:<DataTable data={rows} getRowId={row=>row.id} columns={[
   {id:"title",header:t("Title"),cell:({row:{original:row}})=><>{row.title}<div className="text-muted text-xs">{row.rationale}</div></>},
   {id:"source",header:t("Source"),accessorFn:row=>row.origin??"ui"},
   {id:"state",header:t("State"),accessorFn:row=>releases.find(item=>item.version===row.release)?.state??row.state},
   {accessorKey:"release",header:t("Release")},
   {id:"actions",header:"",cell:({row:{original:row}})=><Button aria-expanded={open===row.id} onClick={()=>setOpen(open===row.id?undefined:row.id)}>{t("Review")}</Button>}
  ]}/>}
  {selected?<ChangeReview change={selected}/>:null}
  {releases.length?<Disclosure summary={t("Release history")}><pre className="code">{JSON.stringify(releases,null,2)}</pre></Disclosure>:null}
  {activations.length?<Disclosure summary={t("Activation history")}><pre className="code">{JSON.stringify(activations,null,2)}</pre></Disclosure>:null}
 </Panel>;
}
function ChangeReview({change}:{change:ChangeRow}){
 const t=useText();
 return <Panel title={change.title} aria-label={`${t("Review")} ${change.title}`}>
  <h3>{change.title}</h3><p>{change.rationale}</p>
  <dl className="record">
   <div><dt>{t("Change ID")}</dt><dd><code>{change.id}</code></dd></div>
   <div><dt>{t("Source")}</dt><dd>{change.origin??"ui"}{change.agent?<> · <code>{change.agent}</code></>:null}</dd></div>
   <div><dt>{t("State")}</dt><dd>{change.state}</dd></div>
   <div><dt>{t("Base release")}</dt><dd>{change.base??0}</dd></div>
   {change.sourceDigest?<div><dt>{t("Source snapshot")}</dt><dd><code>{change.sourceDigest}</code></dd></div>:null}
   {change.approvedBy?<div><dt>{t("Approved by")}</dt><dd>{change.approvedBy}{change.approvedAt?<> · <time dateTime={change.approvedAt}>{new Date(change.approvedAt).toLocaleString()}</time></>:null}</dd></div>:null}
  </dl>
  {change.prompt?<><h4>{t("Original prompt")}</h4><pre className="code">{change.prompt}</pre></>:null}
  {change.run?<p><a href={`#/record?${new URLSearchParams({type:"agent.run",id:change.run})}`}>{t("View the architect run")}</a></p>:null}
  {change.assets?.length?<><h4>{t("Selected definitions")}</h4><ul>{change.assets.map(ref=><li key={assetKey(ref)}><code>{assetKey(ref)}</code></li>)}</ul></>:null}
  <h4>{t("Frozen files")}</h4>
  {change.files?.length?change.files.map(file=>{
   const before=file.before,status=file.deleted?"Deleted":!before?"Baseline unavailable":!before.digest?"Added":before.digest===file.digest?"Unchanged":"Changed";
   return <Disclosure key={file.path} summary={<><code>{file.path}</code> · {t(status)}</>}><div className="split">
    <div><h4>{t("Before")}</h4>{before?.digest?<><code className="muted small">{before.digest}</code><pre className="code">{before.content}</pre></>:<Note>{t(before?"This file did not exist in the source baseline.":"This older proposal did not retain its baseline content. Its previous contents cannot be shown.")}</Note>}</div>
    <div><h4>{t("After")}</h4>{file.deleted?<Note>{t("The proposed version removes this file.")}</Note>:<><code className="muted small">{file.digest}</code><pre className="code">{file.content??""}</pre></>}</div>
   </div></Disclosure>;
  }):<Note>{t("This proposal changes definitions only.")}</Note>}
  {change.diagnostics?<><h4>{t("Build diagnostics")}</h4><pre className="code">{change.diagnostics}</pre></>:null}
 </Panel>;
}
function Files(){
 const t=useText(),[search,setSearch]=useState(""),[query,setQuery]=useState(""),[offset,setOffset]=useState(0),[opened,setOpened]=useState(""),limit=50;
 const files=useLiveRead<FileRow[]>(`/v1/system-files?${new URLSearchParams({search:query,offset:String(offset),limit:String(limit)})}`);
 return <>
  <Panel title={t("System files")}>
   <Form className="search row" onSubmit={()=>{setQuery(search);setOffset(0);}}><Input aria-label={t("Find files")} value={search} onChange={event=>setSearch(event.target.value)}/><Button type="submit">{t("Search")}</Button></Form>
   {files.loading&&!files.data?<Loading what={t("System files")}/>:files.error?<><Problem>{files.error}</Problem><Button onClick={files.reload}>{t("Refresh")}</Button></>:!files.data?.length?<Empty>{t("No source file matches this page.")}</Empty>:<DataTable data={files.data} getRowId={file=>file.path} columns={[
    {accessorKey:"path",header:t("Path")},{accessorKey:"revision",header:t("Revision")},
    {id:"open",header:"",cell:({row:{original:file}})=><Button onClick={()=>setOpened(file.path)}>{t("Open")}</Button>}
   ]}/>}
   <div className="row pager"><Button disabled={offset===0} onClick={()=>setOffset(current=>Math.max(0,current-limit))}>{t("Previous")}</Button><span className="muted small">{offset+1}–{offset+(files.data?.length??0)}</span><Button disabled={(files.data?.length??0)<limit} onClick={()=>setOffset(current=>current+limit)}>{t("Next")}</Button></div>
  </Panel>
  {opened?<SourceFile key={opened} path={opened}/>:null}
 </>;
}
function SourceFile({path}:{path:string}){
 const host=useHost(),t=useText(),file=useQuery<FileRow>({queryKey:[host.client.connection.token,host.client.connection.tenant,"system-file",path],queryFn:()=>host.client.get<FileRow>(fileRead(path),true),retry:false});
 return <Panel title={path} actions={<Button onClick={()=>void file.refetch()}>{t("Refresh")}</Button>}>
  {file.error?<Problem>{file.error.message}</Problem>:!file.data?<Loading what={path}/>:<>
   <dl className="record"><div><dt>{t("Revision")}</dt><dd>{file.data.revision}</dd></div><div><dt>{t("Source snapshot")}</dt><dd><code>{file.data.digest}</code></dd></div></dl>
   {file.data.deleted?<Note>{t("This retained file is marked as deleted.")}</Note>:null}
   <pre className="code">{file.data.content??""}</pre>
  </>}
 </Panel>;
}
