import {useEffect,useMemo,useState,type ReactNode} from "react";
import {Panel,PropertyList,t,type EntityInfo} from "@platform/ui";
import {initializeRules} from "./rule-engine";
import {formRuleValues} from "./form-rules";

export function FormRulePreview({info,values,clock}:{info:EntityInfo;values:Record<string,unknown>;clock:{me:string;tenant:string;now:string;today:string}}){
 const [runtime,setRuntime]=useState<{ready:boolean;error?:string}>({ready:false});
 useEffect(()=>{if(Object.keys(info.localRules??{}).length)void initializeRules().then(()=>setRuntime({ready:true}),error=>setRuntime({ready:false,error:error instanceof Error?error.message:String(error)}));},[info.localRules]);
 const result=useMemo<ReturnType<typeof formRuleValues>>(()=>runtime.ready?formRuleValues(info,values,clock):{},[info,values,clock,runtime]);
 const rules=Object.keys(info.localRules??{});
 if(!rules.length)return null;
 return <Panel title={t("Local calculation preview")} description={t("Calculated from the visible form inputs. Saving rechecks current facts on the host.")}>
  {runtime.error?<p role="alert" className="text-danger">{t(runtime.error)}</p>:!runtime.ready?<p role="status">{t("Rule runtime is loading.")}</p>:<PropertyList items={rules.map((name):[string,ReactNode]=>{
   const value=result[name],label=info.fields.find(field=>field.name===name)?.title??name;
   return [label,value?.error?<span role="status">{t("Local preview unavailable")}: {value.error}</span>:value?.status!=="value"?<span role="status">{t(value?.status==="withheld"?"The source is not readable":value?.status==="incomplete"?"The source collection is incomplete":"Waiting for the original inputs")}</span>:<output>{typeof value.value==="object"?JSON.stringify(value.value):String(value.value)}</output>];
  })}/>}
 </Panel>;
}

