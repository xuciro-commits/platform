import type {ScalarValue} from "./decimal";
import { createContext, useContext, useEffect, useMemo, useState, useSyncExternalStore, type ReactNode } from "react";
import { Button, Panel, Select, t, useWorkspace, type Route } from "@platform/ui";
import { pageUIManifest, type Api } from "@platform/kernel";
import { useHost } from "../index";
import { ApplicationSessionHub, type ApplicationSession } from "./ApplicationSessions";
import { useApplicationQueries } from "./ApplicationQueries";
import type { RecordReference } from "./Session";
import { compileVariables, evaluateVariables, type VariableResult } from "./variables";

const Hub = createContext<ApplicationSessionHub | undefined>(undefined);
type Context = { definition: Api.Definition; session: ApplicationSession; instance: string; identity: string; preview: boolean };
const ApplicationContext = createContext<Context | undefined>(undefined);
export const useApplicationContext = () => useContext(ApplicationContext);
const empty = Object.freeze({}) as Record<string, ScalarValue>;
const emptySnapshot = () => empty, emptySubscribe = () => () => {};
const idOf = (d: Api.Definition) => `${d.ref.app}:${d.ref.name}`;

export function ApplicationSessionsProvider({ children }: { children: ReactNode }) {
  const { source } = useHost();
  const hub = useMemo(() => new ApplicationSessionHub(), [source.scope]);
  useEffect(()=>()=>hub.clear(),[hub]);
  return <Hub.Provider value={hub}>{children}</Hub.Provider>;
}

/** Application identity is explicit for reused pages. The shell retains only
 * the opaque instance ID; this provider owns attachment and teardown. */
export function ApplicationPage({ pageRef, route, preview = false, children }: { pageRef: Api.AssetRef; route: Route; preview?: boolean; children: ReactNode }) {
  const { definitions, source } = useHost(), workspace = useWorkspace(), hub = useContext(Hub);
  const [localPreview] = useState(() => `preview:${crypto.randomUUID()}`);
  const holders = definitions.filter((d) => d.application && d.ref.app === pageRef.app && d.application.pages.includes(pageRef.name));
  const id = route.params?.application ?? (holders.length === 1 ? idOf(holders[0]!) : "");
  const definition = holders.find((d) => idOf(d) === id);
  const instance = route.params?.instance ?? (preview ? localPreview : "main");
  const identity = JSON.stringify([source.scope, definition?.ref, definition?.version, definition?.application, instance, preview]);
  const session = useMemo(() => definition && hub && /^[A-Za-z0-9._:-]{1,80}$/.test(instance) ? hub.get(identity, definition.application?.variables ?? {},{source,queries:definition.application?.queries??{}}) : undefined, [hub, identity]);
  const [owner] = useState(() => Symbol());
  const routeKey = JSON.stringify(route);
  useEffect(() => {
    if (!session) return;
    session.attach(owner, () => workspace.close(JSON.parse(routeKey)));
    return () => session.detach(owner);
  }, [session, owner, routeKey]);
  const context = useMemo(() => definition && session ? { definition, session, instance, identity, preview } : undefined, [definition, session, instance, identity, preview]);
  const navigate = (application: string, instance: string) => { const params:Record<string,string> = { ...route.params, application, instance }; delete params.call; workspace.open({ ...route, params }); };
  return <ApplicationContext.Provider value={context}><div className="min-w-0 max-w-full">
    {holders.length > 0 && <div className="mb-3 flex min-w-0 max-w-full flex-wrap items-center gap-2">
      <Select aria-label={t("Page application")} value={id} onChange={(event) => navigate(event.target.value, instance)}><option value="">{t("Choose an application")}</option>{holders.map((d) => <option key={idOf(d)} value={idOf(d)}>{d.application?.title}</option>)}</Select>
      <Button disabled={!context} onClick={() => navigate(id, crypto.randomUUID())}>{t("New application instance")}</Button>
      <Button disabled={!context} onClick={() => session?.close()}>{t("Close application instance")}</Button>
      <span className="min-w-0 max-w-full break-all text-xs text-muted">{t("Instance")}: {instance === "main" ? "main" : instance.slice(-8)}</span>
    </div>}
    {definition && !session && <Panel role="alert">{t("The application instance is unavailable.")}</Panel>}
    {children}
  </div></ApplicationContext.Provider>;
}

export function useApplicationVariables(variables: Record<string, Api.PageVariable>) {
  const context = useApplicationContext();
  const state = useSyncExternalStore(context?.session.subscribe ?? emptySubscribe, context?.session.snapshot ?? emptySnapshot, emptySnapshot);
  const declarations = context?.definition.application?.variables ?? {};
  const recordResources:Record<string,VariableResult>={};
  for(const [id,v] of Object.entries(declarations)){if(v.type!=="record"||v.mode!=="resource")continue;const read=context?.session.reads?.snapshot().records[id];recordResources[id]=read?.status==="value"?{status:"value",value:{kind:"record",reference:read.value}}:read?.status==="pending"?{status:"pending"}:read?.status==="error"?{status:"error",code:"Resource read failed"}:{status:"empty"};}
  for(const [id,v] of Object.entries(declarations)){if(v.type!=="filter"||v.mode!=="resource")continue;const fields=context?.session.reads?.snapshot().filters[id]??{};recordResources[id]={status:Object.keys(fields).length?"value":"empty",value:{kind:"filter",object:v.source?.object?.name??"",fields}};}
  const inputs=useMemo(()=>evaluateVariables(declarations,state,pageUIManifest.runtime,recordResources,undefined,undefined,context?.session.reads?.property),[declarations,state]);
  const queries=useApplicationQueries(context?.session,inputs);
  const values = useMemo(() => evaluateVariables(declarations,state,pageUIManifest.runtime,{...queries.resources,...recordResources},undefined,undefined,context?.session.reads?.property),[declarations,state,JSON.stringify(queries.resources)]);
  const invalid = useMemo(() => compileVariables(declarations, pageUIManifest.runtime).issues.length > 0, [declarations]);
  const resources: Record<string, VariableResult> = {};
  let error: string | undefined;
  for (const [id, variable] of Object.entries(variables)) {
    if (variable.mode !== "shared") continue;
    const source = variable.source?.variable ?? "", declaration = declarations[source];
    if (!context) error = "Choose an application to use its shared variables.";
    else if (!declaration || declaration.type !== variable.type || variable.writable && declaration.mode !== "state" && !["record","filter"].includes(variable.type) || variable.type==="object-set"&&(declaration.mode!=="resource"||!declaration.source?.query||["app","kind","name"].some((key)=>variable.source?.object?.[key as keyof Api.AssetRef]!==context.definition.application?.queries?.[declaration.source!.query!]?.object[key as keyof Api.AssetRef])) || ["record","filter"].includes(variable.type)&&(declaration.mode!=="resource"||declaration.source?.kind!==variable.type||!variable.source?.object||["app","kind","name"].some((key)=>variable.source?.object?.[key as keyof Api.AssetRef]!==declaration.source?.object?.[key as keyof Api.AssetRef])) || invalid) error = "The application does not satisfy this page's shared bindings.";
    resources[id] = error ? { status: "error", code: error } : values[source] ?? { status: "empty" };
  }
  const set = (id: string, value: ScalarValue) => {
    const variable = variables[id];
    if (variable?.mode === "shared" && variable.writable && variable.source?.variable) context?.session.set(variable.source.variable, value);
  };
  const windows=Object.fromEntries(Object.entries(variables).filter(([,v])=>v.mode==="shared"&&v.type==="object-set").map(([id,v])=>[id,queries.windows[v.source?.variable??""]]));
  const signatures=Object.fromEntries(Object.entries(variables).filter(([,v])=>v.mode==="shared"&&v.type==="object-set").map(([id,v])=>[id,queries.signatures[v.source?.variable??""]??""]));
  const select=(id:string,reference:RecordReference|undefined,owner:symbol,onlyOwner=false)=>{const v=variables[id];if(v?.type==="record"&&v.mode==="shared"&&v.writable&&v.source?.variable&&!error)context?.session.select(v.source.variable,reference,owner,onlyOwner);};
  const filter=(id:string,field:string,value:unknown)=>{const v=variables[id];if(v?.type==="filter"&&v.mode==="shared"&&v.writable&&v.source?.variable&&!error)context?.session.filter(v.source.variable,field,value);};
  return { resources, windows, signatures, select, filter, retry:(id:string)=>queries.retry(variables[id]?.source?.variable??""), set, error, identity: context?.identity };
}
