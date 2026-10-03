import {useEffect,useRef,useState} from "react";
import {ApprovalInbox,NotificationList,Panel,t,useViewVisible,type ApprovalInboxRow,type RecordSource} from "@platform/ui";
import {pageUIManifest,type Api} from "@platform/kernel";
import {useHost,useOpenRecord,useReadQuery} from "../index";
import {approvalDecisionReason,approvalTasks,createWorkViews,originalNotifications} from "../work/service";

const message=(error:unknown)=>error instanceof Error?error.message:String(error);
/** Original member work, sharing the workspace's read cache and decision outbox. */
export function WorkViewsRenderer({kind,label,live,enabled=true,readSource}:{kind:"approval-inbox"|"notification-feed";label:string;live:boolean;enabled?:boolean;readSource?:RecordSource}) {
 const host=useHost(),visible=useViewVisible(),open=useOpenRecord(),limits=pageUIManifest.runtime.workViews,identity=JSON.stringify([host.source.scope,kind,visible]),lifecycle=useRef({identity,epoch:0,mounted:true});
 if(lifecycle.current.identity!==identity)lifecycle.current={...lifecycle.current,identity,epoch:lifecycle.current.epoch+1};
 const originalEpoch=lifecycle.current.epoch,active=()=>lifecycle.current.mounted&&lifecycle.current.epoch===originalEpoch&&lifecycle.current.identity===identity&&visible&&(!readSource||readSource.scope===host.source.scope);
 // Work request confirmation follows each actual inbox refresh. Page record
 // caches cannot stand in for a current task/request after a feed refresh.
 const service=createWorkViews(host,active),inbox=useReadQuery<Api.InboxTask[]>("/v1/inbox",3000,visible&&kind==="approval-inbox"),notices=useReadQuery<Api.Notification[]>("/v1/notifications",3000,visible&&kind==="notification-feed");
 const [approvals,setApprovals]=useState<{key:string;rows:ApprovalInboxRow[];total:number;error?:string}>(),[busy,setBusy]=useState<string[]>([]),[errors,setErrors]=useState<Record<string,string>>({}),busyIDs=useRef(new Set<string>());
 useEffect(()=>{lifecycle.current.mounted=true;return()=>{lifecycle.current.mounted=false;lifecycle.current.epoch++;};},[]);
 useEffect(()=>{busyIDs.current=new Set();setBusy([]);setErrors({});},[identity]);
 const sourceReady=!readSource||readSource.scope===host.source.scope,key=JSON.stringify([identity,host.source.revision,readSource?.revision,sourceReady,inbox.dataUpdatedAt,inbox.isFetching,inbox.isError]);
 useEffect(()=>{
  if(!visible||!sourceReady||kind!=="approval-inbox"||!inbox.data||inbox.isFetching||inbox.isError)return;let current=true;
  try {const tasks=approvalTasks(inbox.data,host.me.principalId),window=tasks.slice(0,limits.maxApprovalsWindow);setApprovals({key,rows:window.map(task=>({task,loading:true})),total:tasks.length});
   for(const task of window)void service.approval(task).then(request=>{const reason=approvalDecisionReason(request,host.me.principalId);if(current&&active())setApprovals(prior=>prior?.key===key?{...prior,rows:prior.rows.map(row=>row.task.id===task.id?{task,request,canDecide:!reason,decisionReason:reason?t(reason):undefined}:row)}:prior);},error=>{if(current&&active())setApprovals(prior=>prior?.key===key?{...prior,rows:prior.rows.map(row=>row.task.id===task.id?{task,error:t(message(error))}:row)}:prior);});
  }catch(error){setApprovals({key,rows:[],total:0,error:message(error)});}
  return()=>{current=false;};
 },[key,visible,kind]);
 const mutate=async(id:string,action:()=>Promise<boolean>)=>{
  if(!active()||!live||!enabled||busyIDs.current.has(id))return;busyIDs.current.add(id);setBusy([...busyIDs.current]);setErrors(prior=>{const next={...prior};delete next[id];return next;});
  try{if(!await action()&&active())setErrors(prior=>({...prior,[id]:t("The work decision was not confirmed.")}));}
  catch(error){if(active())setErrors(prior=>({...prior,[id]:t(message(error))}));}
  finally{if(active()){busyIDs.current.delete(id);setBusy([...busyIDs.current]);}}
 };
 const openRelated=(ref?:string)=>{if(active()&&live&&enabled&&ref&&host.source.entity(ref.split("/")[0]!))open(ref);};
 if(!visible)return null;
 if(kind==="approval-inbox"){
  if(inbox.isError)return <Panel role="alert">{t("The original approval inbox could not be read.")}</Panel>;
  if(!sourceReady||inbox.isFetching||!inbox.data||approvals?.key!==key)return <p role="status">{t("Loading original approvals…")}</p>;
  if(approvals.error)return <Panel role="alert">{t(approvals.error)}</Panel>;
  return <ApprovalInbox title={label} rows={approvals.rows} total={approvals.total} enabled={enabled&&live} busyIDs={busy} errors={errors}
   onApprove={host.can("work.approval.approve")?(request,task)=>mutate(request.id,()=>service.decideApproval(task,request,"approve")):undefined}
   onReject={host.can("work.approval.reject")?(request,task)=>mutate(request.id,()=>service.decideApproval(task,request,"reject")):undefined}
   onOpenRequest={live?(request)=>openRelated(`work.approval/${request.id}`):undefined} onOpenRelated={live?(request)=>openRelated(request.target):undefined}/>;
 }
 if(notices.isError)return <Panel role="alert">{t("The original notifications could not be read.")}</Panel>;
 if(notices.isFetching||!notices.data)return <p role="status">{t("Loading original notifications…")}</p>;
 let items:Api.Notification[];try{items=originalNotifications(notices.data,host.me.principalId);}catch(error){return <Panel role="alert">{t(message(error))}</Panel>;}
 return <NotificationList label={label} items={items} total={items.length} loadedCount={items.length} limit={limits.maxNotificationsWindow} enabled={enabled&&live} busyIDs={busy} errors={errors}
  onRead={host.can("platform.notification.read")?item=>mutate(item.id,()=>service.readNotification(item)):undefined}
  onOpen={live?item=>{if(enabled&&!item.read&&host.can("platform.notification.read"))void mutate(item.id,()=>service.readNotification(item));openRelated(item.ref);}:undefined}/>;
}
