// The self-hosting console consumes the original source, change and activation
// reads. All decisions and uncertain answers stay in the kernel outbox.
import {useCallback,useEffect,useRef,useState,type ReactNode} from "react";
import type {Api} from "@platform/kernel";
import * as api from "./api";
import {useHost,useLoad} from "./host";
import {t as translate} from "./i18n";
import {Empty,Loading,Note,Panel,Problem} from "./ui";

type FileRow={path:string;content?:string;revision:number;digest:string;size:number;deleted?:boolean;editable?:boolean};
type ChangeFile={path:string;content?:string;digest:string;deleted?:boolean;before?:{digest:string;content:string}};
type ChangeRow={id:string;title:string;state:string;origin?:string;release?:number;rationale?:string;files?:ChangeFile[];diagnostics?:string;created?:{by:string};base?:number;sourceDigest?:string;assets?:Api.AssetRef[];prompt?:string;agent?:string;run?:string;approvedBy?:string;approvedAt?:string};
type ReleaseRow={id:string;version:number;state:string;change:string;kind?:string;diagnostics?:string};
type ActivationRow={id:string;release:number;from?:number;result:string;window?:number;at:string;evidence?:string;failure?:string};
type Act={run:(schema:string,id:string,values?:Record<string,unknown>)=>Promise<api.Outcome>;resume:(key:string)=>Promise<api.Outcome>;busy:boolean;report:ReactNode};
const assetKey=(ref:Api.AssetRef)=>`${ref.app}/${ref.kind}/${ref.name}`;
const fileRead=(path:string)=>`/v1/system-file?${new URLSearchParams({path})}`;
function useText(){const{me}=useHost();return(text:string)=>translate(text,me.language);}
function useLiveRead<T>(path:string){
 const{client,revision}=useHost(),load=useLoad(()=>client.get<T>(path),[client,path,revision]);
 useEffect(()=>client.subscribeRead(path,load.reload),[client,path,load.reload]);
 return load;
}
function useAct(reload:()=>void):Act{
 const host=useHost(),t=useText(),lock=useRef(false),[busy,setBusy]=useState(false),[said,setSaid]=useState<api.Outcome>();
 const execute=useCallback(async(run:()=>Promise<api.Outcome>):Promise<api.Outcome>=>{
  if(lock.current)return {ok:false,pending:true,key:"",message:t("The request is already sending.")};
  lock.current=true;setBusy(true);setSaid(undefined);
  try{const outcome=await run();setSaid(outcome);if(outcome.ok)reload();return outcome;}
  catch(error){const outcome={ok:false,key:"",message:error instanceof Error?error.message:String(error)};setSaid(outcome);return outcome;}
  finally{lock.current=false;setBusy(false);}
 },[reload,host.client]);
 const run=useCallback((schema:string,id:string,values:Record<string,unknown>={})=>execute(async()=>{
  const action=host.actions.find(action=>action.schema===schema);if(!action)throw Error(`You may not ${schema} here.`);
  const pending=host.client.authorities.outbox.find(entry=>api.unanswered(entry)&&entry.submission.schema?.name===schema&&entry.submission.target?.id===id);
  return pending?api.outcomeOf(pending):api.decide(host.client,action,id,values);
 }),[execute,host.actions,host.client]);
 const resume=useCallback((key:string)=>execute(()=>api.resume(host.client,key)),[execute,host.client]);
 const text=said?.pending?t("The request is saved, but no final receipt has arrived. Check the original request below."):said?.ok?t("Accepted."):[said?.code,said?.message].filter(Boolean).join(": ");
 return {run,resume,busy,report:said?(said.ok||said.pending?<Note>{text}</Note>:<Problem>{text||t("The host refused this decision.")}</Problem>):null};
}

export function SystemConsole(){
 const host=useHost(),t=useText(),changes=useLiveRead<ChangeRow[]>("/v1/system-changes"),releases=useLiveRead<ReleaseRow[]>("/v1/system-releases"),activations=useLiveRead<ActivationRow[]>("/v1/system-activations");
 const act=useAct(host.reload),may=(schema:string)=>host.actions.some(action=>action.schema===schema);
 useEffect(()=>host.client.enableLiveReads(),[host.client]);
 return <>
  <h1>{t("Self-hosting")}</h1><Note>{t("Propose a change, review its frozen files, approve it, then activate the resulting release. Saving a file alone does not change the running system.")}</Note>
  <details><summary>{t("Help: self-hosting")}</summary><ol>
   <li>{t("Open source by path, or enter a new path. Save edits before including files in a proposal. Frozen kernel files are read-only.")}</li>
   <li>{t("Select saved source files and build drafts, then propose one change with a title and a reason. AI proposals need approval from a human publisher.")}</li>
   <li>{t("Definition-only releases activate immediately without compiling or restarting. Source releases wait for the updater to build; mixed releases install their definitions after a successful code swap.")}</li>
   <li>{t("Only source activations have a cancel window. Acceptance means the request was recorded; the activation result confirms whether the new version is running.")}</li>
   <li>{t("If a receipt is missing, check the original request below. It keeps the original key and input. The architect run link shows actual AI work; a proposal source label does not prove a model ran.")}</li>
  </ol></details>
  {act.report}<PendingRequests act={act}/>
  {may("agent.run.start")?<AskForChange act={act}/>:null}
  <Changes rows={changes.data??[]} releases={releases.data??[]} activations={activations.data??[]} loading={changes.loading} error={changes.error||releases.error||activations.error} act={act} may={may} reload={()=>{changes.reload();releases.reload();activations.reload();}}/>
  {may("system.file.save")||may("system.change.propose")?<Files act={act} may={may}/>:null}
 </>;
}
function PendingRequests({act}:{act:Act}){
 const host=useHost(),t=useText(),entries=host.client.authorities.outbox.filter(entry=>api.unanswered(entry)&&((entry.submission.schema?.name??"").startsWith("system.")||entry.submission.schema?.name==="agent.run.start"));
 if(!entries.length)return null;
 return <Panel title={t("Unconfirmed requests")}><Note>{t("The host may already have saved these requests. Retry uses their original keys and inputs.")}</Note><ul>{entries.map(entry=><li key={entry.submission.idempotencyKey}><code>{entry.submission.schema?.name} · {entry.submission.target?.id}</code> <button disabled={act.busy} onClick={()=>void act.resume(entry.submission.idempotencyKey!)}>{t("Check the original request")}</button></li>)}</ul></Panel>;
}
function AskForChange({act}:{act:Act}){
 const host=useHost(),t=useText(),[goal,setGoal]=useState(""),[id,setId]=useState(()=>api.newId("run")),[last,setLast]=useState("");
 const entry=host.client.authorities.outbox.find(entry=>entry.submission.schema?.name==="agent.run.start"&&entry.submission.target?.id===id),waiting=!!entry&&api.unanswered(entry);
 useEffect(()=>{if(entry?.state==="SUBMISSION_STATE_CONFIRMED"){setLast(id);setGoal("");setId(api.newId("run"));}},[entry?.state,id]);
 return <Panel title={t("Ask for a change")}><Note>{t("Describe one change. The architect reads source, saves a proposal, and waits for a human publisher. It cannot approve or activate releases.")}</Note>
  <Note>{t("Before the first run, enable a tool-capable model in AI and choose Model for agents in Settings.")} <a href="#/o/ai">{t("AI configuration")}</a> · <a href="#/o/settings">{t("Agent model setting")}</a></Note>
  <form className="form" onSubmit={event=>{event.preventDefault();void act.run("agent.run.start",id,{agent:"system.architect",goal,act:true});}}>
   <label className="field"><span>{t("The change")}</span><textarea rows={4} value={goal} disabled={act.busy||waiting} onChange={event=>setGoal(event.target.value)}/></label>
   <div className="row wrap"><button disabled={act.busy||waiting||!goal.trim()}>{t(act.busy?"Sending…":"Run the architect")}</button>{last?<a href={`#/r/agent.run/${encodeURIComponent(last)}`}>{t("View the architect run")}</a>:null}</div>
  </form>
 </Panel>;
}
function Changes({rows,releases,activations,loading,error,act,may,reload}:{rows:ChangeRow[];releases:ReleaseRow[];activations:ActivationRow[];loading:boolean;error?:string;act:Act;may:(schema:string)=>boolean;reload:()=>void}){
 const t=useText(),[open,setOpen]=useState<string>(),[window,setWindow]=useState(30),[now,setNow]=useState(Date.now());
 const requested=activations.some(item=>item.result==="requested"),selected=rows.find(row=>row.id===open);
 useEffect(()=>{if(!requested)return;const timer=setInterval(()=>setNow(Date.now()),1000);return()=>clearInterval(timer);},[requested]);
 return <Panel title={t("System changes")} actions={<button onClick={reload}>{t("Refresh")}</button>}>
  <label className="field"><span>{t("Cancel window (seconds)")}</span><input type="number" min={0} max={600} value={window} disabled={act.busy} onChange={event=>setWindow(Number(event.target.value))}/></label>
  <p className="muted small">{t("The cancel window applies to source releases. Definition-only releases activate immediately.")}</p>
  {loading&&!rows.length?<Loading what={t("System changes")}/>:error?<Problem>{error}</Problem>:!rows.length?<Empty>{t("No change has been proposed yet.")}</Empty>:<div className="lines-scroll"><table className="grid"><thead><tr><th>{t("Title")}</th><th>{t("Source")}</th><th>{t("State")}</th><th>{t("Release")}</th><th/></tr></thead><tbody>{rows.map(row=>{
   const release=releases.find(item=>item.version===row.release),activation=activations.find(item=>item.release===row.release&&item.result==="requested"),until=activation?new Date(activation.at).getTime()+(activation.window??0)*1000:0;
   return <tr key={row.id}><td>{row.title}<div className="muted small">{row.rationale}</div></td><td>{row.origin??"ui"}</td><td>{release?.state??row.state}</td><td>{row.release||""}</td><td className="right"><div className="row wrap">
    <button aria-expanded={open===row.id} onClick={()=>setOpen(open===row.id?undefined:row.id)}>{t("Review")}</button>
    {row.state==="draft"&&may("system.change.approve")?<button disabled={act.busy} onClick={()=>void act.run("system.change.approve",row.id)}>{t("Approve")}</button>:null}
    {release?.state==="published"&&!requested&&may("system.release.activate")?<button disabled={act.busy||(release.kind!=="definitions"&&(!Number.isInteger(window)||window<0||window>600))} onClick={()=>void act.run("system.release.activate",release.id,release.kind==="definitions"?{}:{window})}>{t("Activate")}</button>:null}
    {activation&&release&&now<until&&may("system.release.cancel")?<button disabled={act.busy} onClick={()=>void act.run("system.release.cancel",release.id,{activationId:activation.id,reason:t("Cancelled from the self-hosting console")})}>{t("Cancel activation")}</button>:null}
   </div>{activation?<p className="muted small">{now<until?<>{t("The cancel window ends at")} {new Date(until).toLocaleTimeString()}</>:t("The updater is completing this activation.")}</p>:null}</td></tr>;
  })}</tbody></table></div>}
  {selected?<ChangeReview change={selected}/>:null}
  {activations.length?<details><summary>{t("Activation history")}</summary><pre className="code">{JSON.stringify(activations,null,2)}</pre></details>:null}
 </Panel>;
}
function ChangeReview({change}:{change:ChangeRow}){
 const t=useText();
 return <section aria-label={`${t("Review")} ${change.title}`}>
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
  {change.run?<p><a href={`#/r/agent.run/${encodeURIComponent(change.run)}`}>{t("View the architect run")}</a></p>:null}
  {change.assets?.length?<><h4>{t("Selected definitions")}</h4><ul>{change.assets.map(ref=><li key={assetKey(ref)}><code>{assetKey(ref)}</code></li>)}</ul></>:null}
  <h4>{t("Frozen files")}</h4>
  {change.files?.length?change.files.map(file=>{
   const before=file.before,status=file.deleted?"Deleted":!before?"Baseline unavailable":!before.digest?"Added":before.digest===file.digest?"Unchanged":"Changed";
   return <details key={file.path}><summary><code>{file.path}</code> · {t(status)}</summary><div className="split">
    <div><h4>{t("Before")}</h4>{before?.digest?<><code className="muted small">{before.digest}</code><pre className="code">{before.content}</pre></>:<Note>{t(before?"This file did not exist in the source baseline.":"This older proposal did not retain its baseline content. Its previous contents cannot be shown.")}</Note>}</div>
    <div><h4>{t("After")}</h4>{file.deleted?<Note>{t("The proposed version removes this file.")}</Note>:<><code className="muted small">{file.digest}</code><pre className="code">{file.content??""}</pre></>}</div>
   </div></details>;
  }):<Note>{t("This proposal changes definitions only.")}</Note>}
  {change.diagnostics?<><h4>{t("Build diagnostics")}</h4><pre className="code">{change.diagnostics}</pre></>:null}
 </section>;
}
function Files({act,may}:{act:Act;may:(schema:string)=>boolean}){
 const host=useHost(),t=useText(),[search,setSearch]=useState(""),[query,setQuery]=useState(""),[offset,setOffset]=useState(0),[path,setPath]=useState(""),[opened,setOpened]=useState(""),[selected,setSelected]=useState<string[]>([]),[preparing,setPreparing]=useState(false),limit=50;
 const selectionLocked=act.busy||preparing||host.client.authorities.outbox.some(entry=>api.unanswered(entry)&&entry.submission.schema?.name==="system.change.propose");
 const files=useLiveRead<FileRow[]>(`/v1/system-files?${new URLSearchParams({search:query,offset:String(offset),limit:String(limit)})}`);
 const choose=(file:string,include:boolean)=>{if(selectionLocked)return;setSelected(current=>include?[...new Set([...current,file])]:current.filter(path=>path!==file));};
 return <>
  <Panel title={t("System files")}>
   <form className="search row" onSubmit={event=>{event.preventDefault();setQuery(search);setOffset(0);}}><input aria-label={t("Find files")} value={search} onChange={event=>setSearch(event.target.value)}/><button>{t("Search")}</button></form>
   <form className="row wrap" onSubmit={event=>{event.preventDefault();setOpened(path.trim());}}><label className="field"><span>{t("Path")}</span><input value={path} onChange={event=>setPath(event.target.value)} spellCheck={false}/></label><button disabled={!path.trim()||act.busy}>{t("Open or create file")}</button></form>
   {files.loading&&!files.data?<Loading what={t("System files")}/>:files.error?<Problem>{files.error}</Problem>:!files.data?.length?<Empty>{t("No source file matches this page. Enter a path to create a new file.")}</Empty>:<div className="source-list"><table className="grid"><thead><tr><th>{t("Include")}</th><th>{t("Path")}</th><th>{t("Revision")}</th><th/></tr></thead><tbody>{files.data.map(file=><tr key={file.path}><td><input type="checkbox" aria-label={`${t("Include")} ${file.path}`} disabled={selectionLocked||file.editable===false||!may("system.change.propose")} checked={selected.includes(file.path)} onChange={event=>choose(file.path,event.target.checked)}/></td><td><code>{file.path}</code></td><td>{file.revision}</td><td><button onClick={()=>{setPath(file.path);setOpened(file.path);}}>{t("Open")}</button></td></tr>)}</tbody></table></div>}
   <div className="row pager"><button disabled={offset===0} onClick={()=>setOffset(current=>Math.max(0,current-limit))}>{t("Previous")}</button><span className="muted small">{offset+1}–{offset+(files.data?.length??0)}</span><button disabled={(files.data?.length??0)<limit} onClick={()=>setOffset(current=>current+limit)}>{t("Next")}</button></div>
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
 },[host.client,path,completed]);
 return file.error?<Panel title={path}><Problem>{file.error}</Problem><button onClick={file.reload}>{t("Refresh")}</button></Panel>:!file.data?<Loading what={path}/>:<FileEditor key={`${path}:${file.data.revision}:${file.data.digest}:${!!file.data.deleted}`} file={file.data} act={act} maySave={maySave} include={include} reload={file.reload} loading={file.loading}/>;
}
function FileEditor({file,act,maySave,include,reload,loading}:{file:FileRow;act:Act;maySave:boolean;include:()=>void;reload:()=>void;loading:boolean}){
 const host=useHost(),t=useText(),[content,setContent]=useState(file.content??""),[deleted,setDeleted]=useState(!!file.deleted),pending=host.client.authorities.outbox.some(entry=>api.unanswered(entry)&&entry.submission.schema?.name==="system.file.save"&&entry.submission.target?.id===file.path),locked=act.busy||pending||loading||!maySave||file.editable===false,dirty=content!==(file.content??"")||deleted!==!!file.deleted;
 return <Panel title={file.path} actions={<span className="muted small">{t(file.revision?"Revision":file.digest?"Running source":"New file")} {file.revision||""}</span>}>
  {file.editable===false?<Note>{t("Frozen kernel file: reading is allowed, self-hosting edits are not.")}</Note>:null}
  <form className="form source-editor" onSubmit={event=>{event.preventDefault();void act.run("system.file.save",file.path,{path:file.path,content,revision:file.revision,deleted}).then(outcome=>{if(outcome.ok)include();});}}>
   <label className="field"><span>{t("Content")}</span><textarea rows={16} value={content} readOnly={locked||deleted} onChange={event=>setContent(event.target.value)} spellCheck={false}/></label>
   {maySave&&file.editable!==false?<label className="field check"><input type="checkbox" checked={deleted} disabled={act.busy||pending||loading} onChange={event=>setDeleted(event.target.checked)}/><span>{t("Delete this file in the proposed version")}</span></label>:null}
   <div className="row wrap">{maySave&&file.editable!==false?<button disabled={locked||(!dirty&&file.revision!==0)}>{t(act.busy?"Sending…":"Save file")}</button>:null}<button type="button" disabled={act.busy||pending||loading} onClick={()=>{setContent(file.content??"");setDeleted(!!file.deleted);reload();}}>{t("Reload file and discard edits")}</button></div>
  </form>
 </Panel>;
}
function ProposeFiles({paths,remove,act,reading,setReading}:{paths:string[];remove:(path:string)=>void;act:Act;reading:boolean;setReading:(reading:boolean)=>void}){
 const host=useHost(),t=useText(),[id,setId]=useState(()=>api.newId("chg")),[title,setTitle]=useState(""),[rationale,setRationale]=useState(""),[assets,setAssets]=useState<Api.AssetRef[]>([]),entry=host.client.authorities.outbox.find(entry=>entry.submission.schema?.name==="system.change.propose"&&entry.submission.target?.id===id),pending=!!entry&&api.unanswered(entry);
 useEffect(()=>{if(entry?.state==="SUBMISSION_STATE_CONFIRMED"){setTitle("");setRationale("");setId(api.newId("chg"));}},[entry?.state,id]);
 const[error,setError]=useState("");
 const drafts=useLoad(()=>host.client.get<Api.ReleaseDraftChanges>("/v1/releases/drafts/changes"),[host.client,host.revision]);
 const propose=async()=>{
  setReading(true);setError("");
  try{const files=await Promise.all(paths.map(path=>host.client.get<FileRow>(fileRead(path),true)));await act.run("system.change.propose",id,{title,rationale,origin:"ui",assets,files:files.map(file=>({path:file.path,content:file.content??"",revision:file.revision,digest:file.digest,deleted:!!file.deleted}))});}
  catch(error){setError(error instanceof Error?error.message:String(error));}finally{setReading(false);}
 };
 return <Panel title={t("Prepare a proposal")}><Note>{t("Select saved source files and build drafts. One proposal freezes both. A proposal with only definitions activates without compiling the host.")}</Note>
  <ul>{paths.map(path=><li key={path}><code>{path}</code> <button disabled={act.busy||reading||pending} onClick={()=>remove(path)}>{t("Remove")}</button></li>)}</ul>
  <h3>{t("Build drafts")}</h3>
  {drafts.loading&&!drafts.data?<Loading what={t("Build drafts")}/>:drafts.error?<><Problem>{drafts.error}</Problem><button onClick={drafts.reload}>{t("Refresh")}</button></>:!drafts.data?.changes.length?<Empty>{t("No build draft is saved yet. Create or edit a definition in Records, then refresh this list.")}</Empty>:<ul>{drafts.data.changes.map(draft=><li key={assetKey(draft.ref)}><label><input type="checkbox" disabled={act.busy||reading||pending||!!draft.diagnostic} checked={assets.some(ref=>assetKey(ref)===assetKey(draft.ref))} onChange={event=>setAssets(current=>event.target.checked?[...current,draft.ref]:current.filter(ref=>assetKey(ref)!==assetKey(draft.ref)))}/> {draft.title||draft.ref.name} <code>{draft.ref.kind} · {draft.ref.name}</code></label> <a href={`#/r/${encodeURIComponent(draft.type)}/${encodeURIComponent(draft.id)}`}>{t("Open")}</a>{draft.diagnostic?<Problem>{draft.diagnostic}</Problem>:null}</li>)}</ul>}
  <button disabled={reading||pending} onClick={drafts.reload}>{t("Refresh build drafts")}</button>
  <form className="form" onSubmit={event=>{event.preventDefault();void propose();}}><label className="field"><span>{t("Title")}</span><input disabled={act.busy||reading||pending} value={title} onChange={event=>setTitle(event.target.value)}/></label><label className="field"><span>{t("Rationale")}</span><textarea disabled={act.busy||reading||pending} rows={3} value={rationale} onChange={event=>setRationale(event.target.value)}/></label>{error?<Problem>{error}</Problem>:null}<button disabled={act.busy||reading||pending||(!paths.length&&!assets.length)||!title.trim()||!rationale.trim()}>{t(act.busy||reading?"Sending…":"Propose selected changes")}</button></form>
 </Panel>;
}
