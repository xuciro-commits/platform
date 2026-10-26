// The self-hosting console consumes the original source, change and activation
// reads. All decisions and uncertain answers stay in the kernel outbox.
import {useCallback,useEffect,useRef,useState,type ReactNode} from "react";
import type {Api,Entry,EdgeClient} from "@platform/kernel";
import {useQuery} from "@tanstack/react-query";
import {useHost,useReadQuery,type Host} from "@platform/app";
import {t as translate,Panel,Button,Form,Input,Textarea,Checkbox,Disclosure,DataTable} from "@platform/ui";

const Empty=({children}:{children:ReactNode})=><p className="text-muted">{children}</p>;
const Loading=({what}:{what:string})=><p className="text-muted">{translate("Loading…")} {what}</p>;
const Note=({children}:{children:ReactNode})=><p className="text-sm text-muted">{children}</p>;
const Problem=({children}:{children:ReactNode})=><p role="alert" className="text-sm text-danger">{children}</p>;
type Outcome={ok:boolean;pending?:boolean;key:string;code?:string;message?:string};
const unanswered=(entry:Entry)=>["SUBMISSION_STATE_PENDING","SUBMISSION_STATE_SENDING","SUBMISSION_STATE_UNKNOWN"].includes(entry.state);
const outcomeOf=(entry:Entry):Outcome=>({ok:entry.state==="SUBMISSION_STATE_CONFIRMED",pending:unanswered(entry),key:entry.submission.idempotencyKey!,code:unanswered(entry)?undefined:entry.outcome,message:entry.reason});
const newId=(prefix:string)=>`${prefix}-${crypto.randomUUID()}`;
async function decide(host:Host,action:Api.Action,id:string,values:Record<string,unknown>):Promise<Outcome>{
 let saved:Entry|undefined,message:string|undefined;
 const ok=await host.decide(action.schema,{type:action.target,id},values,{quiet:true,onOutcome:entry=>{saved=entry;},onRefused:reason=>{message=reason;}});
 return saved?outcomeOf(saved):{ok,key:"",message};
}
async function resumeRequest(client:EdgeClient,key:string):Promise<Outcome>{
 await client.send(key);
 const entry=client.authorities.outbox.find(entry=>entry.submission.idempotencyKey===key);
 if(!entry)throw Error("The original request is unavailable.");
 return outcomeOf(entry);
}

type FileRow={path:string;content?:string;revision:number;digest:string;size:number;deleted?:boolean;editable?:boolean};
type ChangeFile={path:string;content?:string;digest:string;deleted?:boolean;before?:{digest:string;content:string}};
type ChangeRow={id:string;title:string;state:string;origin?:string;release?:number;rationale?:string;files?:ChangeFile[];diagnostics?:string;created?:{by:string};base?:number;sourceDigest?:string;assets?:Api.AssetRef[];prompt?:string;agent?:string;run?:string;approvedBy?:string;approvedAt?:string};
type ReleaseRow={id:string;version:number;state:string;change:string;kind?:string;diagnostics?:string};
type ActivationRow={id:string;release:number;from?:number;result:string;window?:number;at:string;evidence?:string;failure?:string};
type Act={run:(schema:string,id:string,values?:Record<string,unknown>)=>Promise<Outcome>;resume:(key:string)=>Promise<Outcome>;busy:boolean;report:ReactNode};
const assetKey=(ref:Api.AssetRef)=>`${ref.app}/${ref.kind}/${ref.name}`;
const fileRead=(path:string)=>`/v1/system-file?${new URLSearchParams({path})}`;
function useText(){return translate;}
function useLiveRead<T>(path:string){
 const load=useReadQuery<T>(path),reload=useCallback(()=>{void load.refetch();},[load.refetch]);
 return {data:load.data,loading:load.isLoading,error:load.error?.message,reload};
}

function useLoad<T>(load:()=>Promise<T>,key:readonly unknown[]){
 const {client}=useHost(),query=useQuery<T>({queryKey:[client.connection.token,client.connection.tenant,"self-hosting",...key],queryFn:load,retry:false});
 const reload=useCallback(()=>{void query.refetch();},[query.refetch]);
 return {data:query.data,error:query.error?.message,loading:query.isLoading,reload};
}
function useAct(reload:()=>void):Act{
 const host=useHost(),t=useText(),lock=useRef(false),[busy,setBusy]=useState(false),[said,setSaid]=useState<Outcome>();
 const execute=useCallback(async(run:()=>Promise<Outcome>):Promise<Outcome>=>{
  if(lock.current)return {ok:false,pending:true,key:"",message:t("The request is already sending.")};
  lock.current=true;setBusy(true);setSaid(undefined);
  try{const outcome=await run();setSaid(outcome);if(outcome.ok)reload();return outcome;}
  catch(error){const outcome={ok:false,key:"",message:error instanceof Error?error.message:String(error)};setSaid(outcome);return outcome;}
  finally{lock.current=false;setBusy(false);}
 },[reload,host.client]);
 const run=useCallback((schema:string,id:string,values:Record<string,unknown>={})=>execute(async()=>{
  const action=host.catalog.find(action=>action.schema===schema);if(!action)throw Error(`You may not ${schema} here.`);
  const pending=host.client.authorities.outbox.find(entry=>unanswered(entry)&&entry.submission.schema?.name===schema&&entry.submission.target?.id===id);
  return pending?outcomeOf(pending):decide(host,action,id,values);
 }),[execute,host.catalog,host.client]);
 const resume=useCallback((key:string)=>execute(()=>resumeRequest(host.client,key)),[execute,host.client]);
 const text=said?.pending?t("The request is saved, but no final receipt has arrived. Check the original request below."):said?.ok?t("Accepted."):[said?.code,said?.message].filter(Boolean).join(": ");
 return {run,resume,busy,report:said?(said.ok||said.pending?<Note>{text}</Note>:<Problem>{text||t("The host refused this decision.")}</Problem>):null};
}

export function SystemConsole(){
 const host=useHost(),t=useText(),changes=useLiveRead<ChangeRow[]>("/v1/system-changes"),releases=useLiveRead<ReleaseRow[]>("/v1/system-releases"),activations=useLiveRead<ActivationRow[]>("/v1/system-activations");
 const reload=useCallback(()=>{void host.refresh?.();},[host.refresh]);
 const act=useAct(reload),may=(schema:string)=>host.catalog.some(action=>action.schema===schema);
 useEffect(()=>host.client.enableLiveReads(),[host.client]);
 return <>
  <h1>{t("Self-hosting")}</h1><Note>{t("Propose a change, review its frozen files, approve it, then activate the resulting release. Saving a file alone does not change the running system.")}</Note>
  <Disclosure summary={t("Help: self-hosting")}><ol>
   <li>{t("Open source by path, or enter a new path. Save edits before including files in a proposal. Frozen kernel files are read-only.")}</li>
   <li>{t("Select saved source files and build drafts, then propose one change with a title and a reason. AI proposals need approval from a human publisher.")}</li>
   <li>{t("Definition-only releases activate immediately without compiling or restarting. Source releases wait for the updater to build; mixed releases install their definitions after a successful code swap.")}</li>
   <li>{t("Only source activations have a cancel window. Acceptance means the request was recorded; the activation result confirms whether the new version is running.")}</li>
   <li>{t("If a receipt is missing, check the original request below. It keeps the original key and input. The architect run link shows actual AI work; a proposal source label does not prove a model ran.")}</li>
  </ol></Disclosure>
  {act.report}<PendingRequests act={act}/>
  {may("agent.run.start")?<AskForChange act={act}/>:null}
  <Changes rows={changes.data??[]} releases={releases.data??[]} activations={activations.data??[]} loading={changes.loading} error={changes.error||releases.error||activations.error} act={act} may={may} reload={()=>{changes.reload();releases.reload();activations.reload();}}/>
  {may("system.file.save")||may("system.change.propose")?<Files act={act} may={may}/>:null}
 </>;
}
function PendingRequests({act}:{act:Act}){
 const host=useHost(),t=useText(),entries=host.client.authorities.outbox.filter(entry=>unanswered(entry)&&((entry.submission.schema?.name??"").startsWith("system.")||entry.submission.schema?.name==="agent.run.start"));
 if(!entries.length)return null;
 return <Panel title={t("Unconfirmed requests")}><Note>{t("The host may already have saved these requests. Retry uses their original keys and inputs.")}</Note><ul>{entries.map(entry=><li key={entry.submission.idempotencyKey}><code>{entry.submission.schema?.name} · {entry.submission.target?.id}</code> <Button disabled={act.busy} onClick={()=>void act.resume(entry.submission.idempotencyKey!)}>{t("Check the original request")}</Button></li>)}</ul></Panel>;
}
function AskForChange({act}:{act:Act}){
 const host=useHost(),t=useText(),[goal,setGoal]=useState(""),[id,setId]=useState(()=>newId("run")),[last,setLast]=useState("");
 const entry=host.client.authorities.outbox.find(entry=>entry.submission.schema?.name==="agent.run.start"&&entry.submission.target?.id===id),waiting=!!entry&&unanswered(entry);
 useEffect(()=>{if(entry?.state==="SUBMISSION_STATE_CONFIRMED"){setLast(id);setGoal("");setId(newId("run"));}},[entry?.state,id]);
 return <Panel title={t("Ask for a change")}><Note>{t("Describe one change. The architect reads source, saves a proposal, and waits for a human publisher. It cannot approve or activate releases.")}</Note>
  <Note>{t("Before the first run, enable a tool-capable model in AI and choose Model for agents in Settings.")} <a href="#/ai-providers">{t("AI configuration")}</a> · <a href="#/app-settings">{t("Agent model setting")}</a></Note>
  <Form className="grid gap-3" onSubmit={()=>{void act.run("agent.run.start",id,{agent:"system.architect",goal,act:true});}}>
   <label className="grid gap-1"><span>{t("The change")}</span><Textarea rows={4} value={goal} disabled={act.busy||waiting} onChange={event=>setGoal(event.target.value)}/></label>
   <div className="flex flex-wrap gap-2"><Button type="submit" disabled={act.busy||waiting||!goal.trim()}>{t(act.busy?"Sending…":"Run the architect")}</Button>{last?<a href={`#/record?${new URLSearchParams({type:"agent.run",id:last})}`}>{t("View the architect run")}</a>:null}</div>
  </Form>
 </Panel>;
}
function Changes({rows,releases,activations,loading,error,act,may,reload}:{rows:ChangeRow[];releases:ReleaseRow[];activations:ActivationRow[];loading:boolean;error?:string;act:Act;may:(schema:string)=>boolean;reload:()=>void}){
 const t=useText(),[open,setOpen]=useState<string>(),[window,setWindow]=useState(30),[now,setNow]=useState(Date.now());
 const requested=activations.some(item=>item.result==="requested"),selected=rows.find(row=>row.id===open);
 useEffect(()=>{if(!requested)return;const timer=setInterval(()=>setNow(Date.now()),1000);return()=>clearInterval(timer);},[requested]);
 return <Panel title={t("System changes")} actions={<Button onClick={reload}>{t("Refresh")}</Button>}>
  <label className="grid gap-1"><span>{t("Cancel window (seconds)")}</span><Input type="number" min={0} max={600} value={window} disabled={act.busy} onChange={event=>setWindow(Number(event.target.value))}/></label>
  <p className="muted small">{t("The cancel window applies to source releases. Definition-only releases activate immediately.")}</p>
  {loading&&!rows.length?<Loading what={t("System changes")}/>:error?<Problem>{error}</Problem>:!rows.length?<Empty>{t("No change has been proposed yet.")}</Empty>:<DataTable data={rows} getRowId={row=>row.id} columns={[
 {id:"title",header:t("Title"),cell:({row:{original:row}})=><>{row.title}<div className="text-muted text-xs">{row.rationale}</div></>},
 {id:"source",header:t("Source"),accessorFn:row=>row.origin??"ui"},
 {id:"state",header:t("State"),accessorFn:row=>releases.find(item=>item.version===row.release)?.state??row.state},
 {accessorKey:"release",header:t("Release")},
 {id:"actions",header:"",cell:({row:{original:row}})=>{
   const release=releases.find(item=>item.version===row.release),activation=activations.find(item=>item.release===row.release&&item.result==="requested"),until=activation?new Date(activation.at).getTime()+(activation.window??0)*1000:0;
 return <><div className="flex flex-wrap gap-2">
    <Button aria-expanded={open===row.id} onClick={()=>setOpen(open===row.id?undefined:row.id)}>{t("Review")}</Button>
    {row.state==="draft"&&may("system.change.approve")?<Button disabled={act.busy} onClick={()=>void act.run("system.change.approve",row.id)}>{t("Approve")}</Button>:null}
    {release?.state==="published"&&!requested&&may("system.release.activate")?<Button disabled={act.busy||(release.kind!=="definitions"&&(!Number.isInteger(window)||window<0||window>600))} onClick={()=>void act.run("system.release.activate",release.id,release.kind==="definitions"?{}:{window})}>{t("Activate")}</Button>:null}
    {activation&&release&&now<until&&may("system.release.cancel")?<Button disabled={act.busy} onClick={()=>void act.run("system.release.cancel",release.id,{activationId:activation.id,reason:t("Cancelled from the self-hosting console")})}>{t("Cancel activation")}</Button>:null}
   </div>{activation?<p className="muted small">{now<until?<>{t("The cancel window ends at")} {new Date(until).toLocaleTimeString()}</>:t("The updater is completing this activation.")}</p>:null}</>;}}
 ]}/>}
  {selected?<ChangeReview change={selected}/>:null}
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
function Files({act,may}:{act:Act;may:(schema:string)=>boolean}){
 const host=useHost(),t=useText(),[search,setSearch]=useState(""),[query,setQuery]=useState(""),[offset,setOffset]=useState(0),[path,setPath]=useState(""),[opened,setOpened]=useState(""),[selected,setSelected]=useState<string[]>([]),[preparing,setPreparing]=useState(false),limit=50;
 const selectionLocked=act.busy||preparing||host.client.authorities.outbox.some(entry=>unanswered(entry)&&entry.submission.schema?.name==="system.change.propose");
 const files=useLiveRead<FileRow[]>(`/v1/system-files?${new URLSearchParams({search:query,offset:String(offset),limit:String(limit)})}`);
 const choose=(file:string,include:boolean)=>{if(selectionLocked)return;setSelected(current=>include?[...new Set([...current,file])]:current.filter(path=>path!==file));};
 return <>
  <Panel title={t("System files")}>
   <Form className="search row" onSubmit={()=>{setQuery(search);setOffset(0);}}><Input aria-label={t("Find files")} value={search} onChange={event=>setSearch(event.target.value)}/><Button type="submit">{t("Search")}</Button></Form>
   <Form className="flex flex-wrap gap-2" onSubmit={()=>{setOpened(path.trim());}}><label className="grid gap-1"><span>{t("Path")}</span><Input value={path} onChange={event=>setPath(event.target.value)} spellCheck={false}/></label><Button type="submit" disabled={!path.trim()||act.busy}>{t("Open or create file")}</Button></Form>
   {files.loading&&!files.data?<Loading what={t("System files")}/>:files.error?<Problem>{files.error}</Problem>:!files.data?.length?<Empty>{t("No source file matches this page. Enter a path to create a new file.")}</Empty>:<DataTable data={files.data} getRowId={file=>file.path} columns={[
 {id:"include",header:t("Include"),cell:({row:{original:file}})=><Checkbox ariaLabel={`${t("Include")} ${file.path}`} disabled={selectionLocked||file.editable===false||!may("system.change.propose")} checked={selected.includes(file.path)} onChange={checked=>choose(file.path,checked)}/>},
 {accessorKey:"path",header:t("Path")},{accessorKey:"revision",header:t("Revision")},
 {id:"open",header:"",cell:({row:{original:file}})=><Button onClick={()=>{setPath(file.path);setOpened(file.path);}}>{t("Open")}</Button>}
 ]}/>}
   <div className="row pager"><Button disabled={offset===0} onClick={()=>setOffset(current=>Math.max(0,current-limit))}>{t("Previous")}</Button><span className="muted small">{offset+1}–{offset+(files.data?.length??0)}</span><Button disabled={(files.data?.length??0)<limit} onClick={()=>setOffset(current=>current+limit)}>{t("Next")}</Button></div>
  </Panel>
  {opened?<SourceFile key={opened} path={opened} act={act} maySave={may("system.file.save")} include={()=>choose(opened,true)}/>:null}
  {may("system.change.propose")?<ProposeFiles paths={selected} remove={path=>choose(path,false)} act={act} reading={preparing} setReading={setPreparing}/>:null}
 </>;
}
function SourceFile({path,act,maySave,include}:{path:string;act:Act;maySave:boolean;include:()=>void}){
 const host=useHost(),t=useText(),completed=host.client.authorities.outbox.findLast(entry=>entry.submission.schema?.name==="system.file.save"&&entry.submission.target?.id===path&&entry.state==="SUBMISSION_STATE_CONFIRMED")?.submission.idempotencyKey;
 const file=useLoad(async()=>{
  try{return await host.client.get<FileRow>(fileRead(path),true);}
  catch(error){if(String(error).includes("HTTP 404"))return {path,content:"",revision:0,digest:"",size:0,editable:true};throw error;}
 },["system-file",path,completed]);
 return file.error?<Panel title={path}><Problem>{file.error}</Problem><Button onClick={file.reload}>{t("Refresh")}</Button></Panel>:!file.data?<Loading what={path}/>:<FileEditor key={`${path}:${file.data.revision}:${file.data.digest}:${!!file.data.deleted}`} file={file.data} act={act} maySave={maySave} include={include} reload={file.reload} loading={file.loading}/>;
}
function FileEditor({file,act,maySave,include,reload,loading}:{file:FileRow;act:Act;maySave:boolean;include:()=>void;reload:()=>void;loading:boolean}){
 const host=useHost(),t=useText(),[content,setContent]=useState(file.content??""),[deleted,setDeleted]=useState(!!file.deleted),pending=host.client.authorities.outbox.some(entry=>unanswered(entry)&&entry.submission.schema?.name==="system.file.save"&&entry.submission.target?.id===file.path),locked=act.busy||pending||loading||!maySave||file.editable===false,dirty=content!==(file.content??"")||deleted!==!!file.deleted;
 return <Panel title={file.path} actions={<span className="muted small">{t(file.revision?"Revision":file.digest?"Running source":"New file")} {file.revision||""}</span>}>
  {file.editable===false?<Note>{t("Frozen kernel file: reading is allowed, self-hosting edits are not.")}</Note>:null}
  <Form className="form source-editor" onSubmit={()=>{void act.run("system.file.save",file.path,{path:file.path,content,revision:file.revision,deleted}).then(outcome=>{if(outcome.ok)include();});}}>
   <label className="grid gap-1"><span>{t("Content")}</span><Textarea rows={16} value={content} readOnly={locked||deleted} onChange={event=>setContent(event.target.value)} spellCheck={false}/></label>
   {maySave&&file.editable!==false?<Checkbox checked={deleted} disabled={act.busy||pending||loading} onChange={setDeleted}>{t("Delete this file in the proposed version")}</Checkbox>:null}
   <div className="flex flex-wrap gap-2">{maySave&&file.editable!==false?<Button type="submit" disabled={locked||(!dirty&&file.revision!==0)}>{t(act.busy?"Sending…":"Save file")}</Button>:null}<Button type="button" disabled={act.busy||pending||loading} onClick={()=>{setContent(file.content??"");setDeleted(!!file.deleted);reload();}}>{t("Reload file and discard edits")}</Button></div>
  </Form>
 </Panel>;
}
function ProposeFiles({paths,remove,act,reading,setReading}:{paths:string[];remove:(path:string)=>void;act:Act;reading:boolean;setReading:(reading:boolean)=>void}){
 const host=useHost(),t=useText(),[id,setId]=useState(()=>newId("chg")),[title,setTitle]=useState(""),[rationale,setRationale]=useState(""),[assets,setAssets]=useState<Api.AssetRef[]>([]),entry=host.client.authorities.outbox.find(entry=>entry.submission.schema?.name==="system.change.propose"&&entry.submission.target?.id===id),pending=!!entry&&unanswered(entry);
 useEffect(()=>{if(entry?.state==="SUBMISSION_STATE_CONFIRMED"){setTitle("");setRationale("");setId(newId("chg"));}},[entry?.state,id]);
 const[error,setError]=useState("");
 const drafts=useLoad(()=>host.client.get<Api.ReleaseDraftChanges>("/v1/releases/drafts/changes"),["system-drafts",host.source.scope]);
 const propose=async()=>{
  setReading(true);setError("");
  try{const files=await Promise.all(paths.map(path=>host.client.get<FileRow>(fileRead(path),true)));await act.run("system.change.propose",id,{title,rationale,origin:"ui",assets,files:files.map(file=>({path:file.path,content:file.content??"",revision:file.revision,digest:file.digest,deleted:!!file.deleted}))});}
  catch(error){setError(error instanceof Error?error.message:String(error));}finally{setReading(false);}
 };
 return <Panel title={t("Prepare a proposal")}><Note>{t("Select saved source files and build drafts. One proposal freezes both. A proposal with only definitions activates without compiling the host.")}</Note>
  <ul>{paths.map(path=><li key={path}><code>{path}</code> <Button disabled={act.busy||reading||pending} onClick={()=>remove(path)}>{t("Remove")}</Button></li>)}</ul>
  <h3>{t("Build drafts")}</h3>
  {drafts.loading&&!drafts.data?<Loading what={t("Build drafts")}/>:drafts.error?<><Problem>{drafts.error}</Problem><Button onClick={drafts.reload}>{t("Refresh")}</Button></>:!drafts.data?.changes.length?<Empty>{t("No build draft is saved yet. Create or edit a definition in Records, then refresh this list.")}</Empty>:<ul>{drafts.data.changes.map(draft=><li key={assetKey(draft.ref)}><Checkbox disabled={act.busy||reading||pending||!!draft.diagnostic} checked={assets.some(ref=>assetKey(ref)===assetKey(draft.ref))} onChange={checked=>setAssets(current=>checked?[...current,draft.ref]:current.filter(ref=>assetKey(ref)!==assetKey(draft.ref)))}>{draft.title||draft.ref.name} <code>{draft.ref.kind} · {draft.ref.name}</code></Checkbox> <a href={`#/record?${new URLSearchParams({type:draft.type,id:draft.id})}`}>{t("Open")}</a>{draft.diagnostic?<Problem>{draft.diagnostic}</Problem>:null}</li>)}</ul>}
  <Button disabled={reading||pending} onClick={drafts.reload}>{t("Refresh build drafts")}</Button>
  <Form className="grid gap-3" onSubmit={()=>{void propose();}}><label className="grid gap-1"><span>{t("Title")}</span><Input disabled={act.busy||reading||pending} value={title} onChange={event=>setTitle(event.target.value)}/></label><label className="grid gap-1"><span>{t("Rationale")}</span><Textarea disabled={act.busy||reading||pending} rows={3} value={rationale} onChange={event=>setRationale(event.target.value)}/></label>{error?<Problem>{error}</Problem>:null}<Button type="submit" disabled={act.busy||reading||pending||(!paths.length&&!assets.length)||!title.trim()||!rationale.trim()}>{t(act.busy||reading?"Sending…":"Propose selected changes")}</Button></Form>
 </Panel>;
}
