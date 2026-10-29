import {pageUIManifest} from "../pageui/manifest";
import {scalarAssignable,type ScalarValue} from "./decimal";
import { useEffect, useMemo, useState, useSyncExternalStore } from "react";
import type { RecordSource } from "@platform/ui";
import { PageSessionStore, type SelectionPlan } from "./Session";
import {type Api} from "@platform/kernel";
import { compileVariables, evaluateVariables, type VariableResult } from "./variables";
import {initializeRules} from "./rule-engine";

export const pageVariableContract = pageUIManifest.runtime;
export const pageVariableValues = (variables: Record<string,Api.PageVariable>, state:Record<string,unknown> = {}) => evaluateVariables(variables,state,pageVariableContract);
export const pageVariableDiagnostics = (variables: Record<string, Api.PageVariable>) => compileVariables(variables, pageVariableContract).issues;

export function usePageVariables(variables: Record<string, Api.PageVariable>, state: Record<string, unknown>, session: PageSessionStore, resources: Record<string, VariableResult>) {
  const needed=Object.values(variables).some(variable=>variable.mode==="derived"),[runtime,setRuntime]=useState<{ready:boolean;error?:string}>({ready:false});
  useEffect(()=>{if(needed)void initializeRules().then(()=>setRuntime({ready:true}),error=>setRuntime({ready:false,error:error instanceof Error?error.message:String(error)}));},[needed]);
  const values = useMemo(() => {
   const evaluated=evaluateVariables(variables,state,pageVariableContract,resources,undefined,undefined,session.property);
   if(needed&&!runtime.ready)for(const [id,variable]of Object.entries(variables))if(variable.mode==="derived")evaluated[id]=runtime.error?{status:"error",code:runtime.error}:{status:"pending"};
   return evaluated;
  }, [variables, state, resources,runtime]);
  const setMany = (changes: Record<string, ScalarValue>) => {
    if (Object.entries(changes).some(([id, value]) => {
      const variable = variables[id];
      return variable?.scope !== "page" || variable?.mode !== "state" || !scalarAssignable(variable.type,value,pageVariableContract.maxStringBytes,pageVariableContract.decimal.maxBytes);
    })) return;
    session.setScalars(changes);
  };
  return { values, set: (id: string, value: ScalarValue) => setMany({ [id]: value }), setMany };
}

export function usePageSession(source: RecordSource, plan: SelectionPlan) {
  const [session] = useState(() => new PageSessionStore(source, plan));
  const snapshot = useSyncExternalStore(session.subscribe, session.snapshot, session.snapshot);
  useEffect(() => { session.activate(); return () => session.dispose(); }, [session]);
  useEffect(() => { session.updateSource(source); }, [session, source, source.scope, source.revision]);
  return { session, snapshot };
}
