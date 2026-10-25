import {useState} from "react";
import {Button,t, formatStamp} from "@platform/ui";
import type {ActionDeclaration,LocalFormDraft} from "@platform/kernel";
import {useHost} from "../index";
import {actionDraftKey,type actionDraftContext} from "./local-draft";
import {actionInputSignature} from "./refusal";

export function LocalDraftControls({context,action,target,expectedRevision,values,fixed={},disabled,saved}:{context?:ReturnType<typeof actionDraftContext>;action:ActionDeclaration;target:{type:string;id:string};expectedRevision?:number;values:Record<string,unknown>;fixed?:Record<string,unknown>;disabled?:boolean;saved?:LocalFormDraft}){
 const {client}=useHost(),[error,setError]=useState(""),[last,setLast]=useState(()=>saved?{key:saved.key,at:saved.at,signature:actionInputSignature(saved.values)}:undefined);
 if(!context)return null;
 const key=actionDraftKey(context,action,target,fixed),same=last?.key===key&&last.signature===actionInputSignature(values);
 return <div className="grid gap-1 border-t border-border pt-3"><div className="flex flex-wrap items-center gap-2">
  <Button type="button" size="sm" disabled={disabled||!target.id} onClick={()=>{
   setError("");try{const at=new Date().toISOString();client.prepared!.saveForm({key,...context,schema:action.schema,schemaVersion:action.schemaVersion,target:{...target},expectedRevision,values,at});setLast({key,at,signature:actionInputSignature(values)});}catch(problem){setError(t(problem instanceof Error?problem.message:String(problem)));}
  }}>{t("Save local draft")}</Button>
  {last?.key===key&&<span role="status" className="text-xs text-muted">{same?t("Local draft saved {time}.",{time:formatStamp(last.at)}):t("Changes since the last local save.")}</span>}
 </div><p className="text-xs text-muted">{t("A local draft keeps inputs on this device. Submit separately for the host's decision.")}</p>{error&&<p role="alert" className="text-sm text-danger">{error}</p>}</div>;
}
