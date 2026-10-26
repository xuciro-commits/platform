import { useRunControl } from "./use-run-control";
import type { Api } from "@platform/kernel";
import { Button, ContextHelp, DataTable, Disclosure, PageHeader, Panel, PropertyList, Input, Select, Tag, t, useWorkspace, type ColumnDef, formatStamp} from "@platform/ui";
import { useState, type ReactNode } from "react";
import { ChainGraph, useHost, useOpenRecord, useReadQuery, useRecordInventory } from "../index";
import {refusalCategoryName} from "../actions/refusal";

type Run = Api.ApplicationRun;
const kindName = (kind: string) => t(({ flow: "Workflow", agent: "Agent", compute: "Calculation", function: "AI function", effect: "External effect", job: "Scheduled job", dispatch: "Dispatch" } as Record<string, string>)[kind] ?? kind);
const stateName = (state: string) => t(({ running: "Working", waiting: "Waiting", done: "Done", stopped: "Stopped", stuck: "Stuck", completed: "Completed", pending: "Pending", failed: "Failed", blocked: "Blocked", canceled: "Canceled", cancelled: "Canceled", ready: "Ready", rejected: "Rejected", delivered: "Delivered", retrying: "Retrying", held: "Held", discarded: "Discarded", scheduled: "Scheduled", accepted: "Accepted by target", unknown: "Outcome unknown", compensating: "Compensating", compensated: "Compensated" } as Record<string,string>)[state] ?? state);
const placementName = (placement: string) => t(({assist:"Read-only assistance",propose:"Business proposal",act:"Authorized actions",autonomous:"Autonomous within granted scope"} as Record<string,string>)[placement] ?? placement);

/** One application projection, preserving original run readers and controls. */
export function ApplicationRuns({ owner, name, project }: { owner: string; name: string; project?: string }) {
 const { role, can } = useHost(), { open } = useWorkspace(), openRecord = useOpenRecord();
 const [offset,setOffset]=useState(0),[chosen,setChosen]=useState<string>();
 const [kind,setKind]=useState(""),[state,setState]=useState("");
 const [subject,setSubject]=useState("");
 const [since,setSince]=useState(""),[until,setUntil]=useState("");
 const timeParam=(value:string)=>value && Number.isFinite(Date.parse(value))?new Date(value).toISOString():"";
 const query=useReadQuery<Api.ApplicationRunPage>(`/v1/applications/${encodeURIComponent(owner)}/${encodeURIComponent(name)}/runs?offset=${offset}&limit=50&kind=${encodeURIComponent(kind)}&state=${encodeURIComponent(state)}&subject=${encodeURIComponent(subject.trim())}&since=${encodeURIComponent(timeParam(since))}&until=${encodeURIComponent(timeParam(until))}`);
 const builder=role("build")==="builder" && !!query.data && !query.isError;
 const applications=useRecordInventory<{id:string;name:string}>("build.app",1000,builder && owner==="build");
 const selected=query.isError?undefined:query.data?.runs.find(run=>`${run.kind}/${run.id}`===chosen);
 const processes=useRecordInventory<{id:string;name:string}>("build.process",1000,builder && selected?.resource?.app==="build" && selected.kind==="flow");
 const computations=useRecordInventory<{id:string;name:string}>("build.code",1000,builder && selected?.resource?.app==="build" && selected.kind==="compute");
 const functions=useRecordInventory<{id:string;name:string}>("build.function",1000,builder && selected?.resource?.app==="build" && selected.kind==="function");
 const agents=useRecordInventory<{id:string;name:string}>("build.agent",1000,builder && selected?.resource?.app==="build" && selected.kind==="agent");
 const application= `${owner}:${name}`;
 const context={application,...(project?{project}:{})};
 const columns:ColumnDef<Run>[]=[
  {accessorKey:"kind",header:t("Kind"),cell:c=>kindName(c.row.original.kind)},
  {accessorKey:"title",header:t("Run"),cell:c=><Button variant="link" onClick={()=>setChosen(`${c.row.original.kind}/${c.row.original.id}`)}>{c.row.original.title||c.row.original.id}</Button>},
  {accessorKey:"resourceName",header:t("Resource")},
  {accessorKey:"state",header:t("State"),cell:c=><Tag label={c.row.original.kind === "dispatch" && c.row.original.state === "running" ? t("In transit") : stateName(c.row.original.state)} tone={["failed","stuck","stopped"].includes(c.row.original.state)?"danger":"neutral"}/>},
  {accessorKey:"version",header:t("Startup version"),cell:c=><span className="font-mono text-xs">{c.row.original.version||t("Not recorded")}</span>},
  {accessorKey:"node",header:t("Failed node")},
  {accessorKey:"association",header:t("Relationship"),cell:c=>t(({application:"Recorded application binding",resource:"Referenced resource",flow:"Started by a related workflow",subject:"Record type used by the application"} as Record<string,string>)[c.row.original.association]??c.row.original.association)},
 ];
 const source=selected?.kind==="flow"?processes.data?.records.find(record=>`build.${record.name}`===selected.resource?.name):selected?.kind==="compute"?computations.data?.records.find(record=>record.name===selected.resource?.name):selected?.kind==="function"?functions.data?.records.find(record=>record.name===selected.resource?.name):selected?.kind==="agent"?agents.data?.records.find(record=>record.name===selected.resource?.name):undefined;
 const draft=applications.data?.records.find(record=>record.name===name);
 return <div className="grid min-w-0 gap-3">
  <PageHeader title={query.data?.title ?? name} help={t("Runs related to this application's referenced resources. Shared use does not establish which application started a run.")} />
  <ApplicationRuntimePanel owner={owner} name={name}/>
  <div className="flex flex-wrap gap-2">
   <Select aria-label={t("Run kind")} value={kind} onChange={event=>{setKind(event.target.value);setOffset(0);setChosen(undefined);}}><option value="">{t("All run kinds")}</option>{["flow","agent","function","compute","effect","job","dispatch"].map(value=><option key={value} value={value}>{kindName(value)}</option>)}</Select>
   <Select aria-label={t("Run state")} value={state} onChange={event=>{setState(event.target.value);setOffset(0);setChosen(undefined);}}><option value="">{t("All run states")}</option>{["running","waiting","pending","ready","done","completed","failed","stuck","stopped","blocked","unknown","accepted","compensating","compensated","canceled","cancelled","rejected","delivered","retrying","held","discarded","scheduled"].map(value=><option key={value} value={value}>{value==="cancelled"?t("Canceled calculation"):stateName(value)}</option>)}</Select>
   <label className="text-xs">{t("Related record")}<Input value={subject} placeholder="type/id" onChange={event=>{setSubject(event.target.value);setOffset(0);setChosen(undefined);}}/></label>
   <label className="text-xs">{t("Started after")}<Input type="datetime-local" value={since} onChange={event=>{setSince(event.target.value);setOffset(0);setChosen(undefined);}}/></label>
   <label className="text-xs">{t("Started before")}<Input type="datetime-local" value={until} onChange={event=>{setUntil(event.target.value);setOffset(0);setChosen(undefined);}}/></label>
  </div>
  {query.isError?<Panel role="alert">{t("Application runs are unavailable to you.")} <Button size="sm" onClick={()=>void query.refetch()}>{t("Retry")}</Button></Panel>:<>
   <DataTable data={query.data?.runs??[]} columns={columns} getRowId={run=>`${run.kind}/${run.id}`} selectedId={chosen} onRowClick={run=>setChosen(`${run.kind}/${run.id}`)} searchable={false} loading={query.isPending} empty={t("No related runs you can read.")} height={320}/>
   <div className="flex items-center gap-2 text-xs"><Button size="sm" disabled={offset===0} onClick={()=>setOffset(Math.max(0,offset-50))}>{t("Previous page")}</Button>
    <span>{query.data?.runs.length?offset+1:0}–{offset+(query.data?.runs.length??0)} / {query.data?.total??0}</span>
    <Button size="sm" disabled={offset+50>=(query.data?.total??0)} onClick={()=>setOffset(offset+50)}>{t("Next page")}</Button></div>
  </>}
  {selected&&<Panel className="grid min-w-0 gap-3" role="region" aria-label={t("Selected run")}>
   <h2 className="text-sm font-semibold">{selected.title||selected.id}</h2>
   <div className="flex flex-wrap gap-2">{["flow","agent","compute"].includes(selected.kind)&&<Button onClick={()=>open({view:selected.kind==="flow"?"flow-instance":selected.kind==="agent"?"run":"operation-run",params:{id:selected.id,...context,...(selected.kind==="compute"&&selected.subject?{subject:selected.subject}:{})}}, {window:"beside"})}>{t("Open run")}</Button>}
    {selected.subject&&<Button onClick={()=>selected.subject!.startsWith("flow.instance/")?open({view:"flow-instance",params:{id:selected.subject!.slice("flow.instance/".length),...context}},{window:"beside"}):openRecord(selected.subject!)}>{t("Open related record")}</Button>}
    {selected.kind === "dispatch" && <Button onClick={() => openRecord({ type: "build.dispatch", id: selected.id }, {window:"tab"})}>{t("Open dispatch record")}</Button>}
    {selected.kind === "function" && <Button onClick={() => openRecord({type:"build.function-call",id:selected.id},{window:"tab"})}>{t("Open function call")}</Button>}
    {selected.kind === "effect" && can("platform.effect.resolve") && <Button onClick={()=>open({view:"integrations"},{window:"beside"})}>{t("Open original delivery controls")}</Button>}
    {selected.kind === "job" && can("platform.work.retry") && <Button onClick={()=>open({view:"automation-operations"},{window:"beside"})}>{t("Open original scheduler controls")}</Button>}
    {source&&<Button onClick={()=>open({view:({flow:"flow",compute:"code",function:"function",agent:"agent"} as Record<string,string>)[selected.kind]!,params:{id:source.id,...(draft?{application:draft.id}:{})}},{window:"beside"})}>{t("Open current source editor")}</Button>}
    {selected.kind==="agent"&&!source&&<Button onClick={()=>open({view:"agents",params:{surface:"tenant"}},{window:"beside"})}>{t("Current agent registry")}</Button>}
   </div>
   <Disclosure summary={t("Run details")}>
    <div className="grid gap-3 py-3">
     <ContextHelp title={t("Run details")}>{t("The editor opens the current saved source. The startup binding below belongs to this run.")}</ContextHelp>
     <RunBinding run={selected}/>
     {selected.subject&&<PropertyList items={[[t("Related record"),<code className="select-all break-all">{selected.subject}</code>]]}/>}
     {!!selected.applications?.length&&<DataTable data={selected.applications} columns={[
      {id:"app",header:t("Application"),cell:cell=>cell.row.original.ref.app+":"+cell.row.original.ref.name},
      {accessorKey:"sourceVersion",header:t("Startup version"),cell:cell=><code className="select-all break-all">{cell.row.original.sourceVersion}</code>},
     ]} getRowId={binding=>binding.ref.app+":"+binding.ref.name} height={160} searchable={false}/>}
    </div>
   </Disclosure>
   {selected.error&&<p role="alert">{selected.node?`${t("Failed node")}: ${selected.node} · `:""}{selected.error}</p>}
   {!!selected.shared.length&&<p role="status" className="text-xs text-muted">{t("Also referenced by: {applications}",{applications:selected.shared.join(", ")})}</p>}
   {selected.kind==="function"?<ChainGraph of={`build.function-call/${selected.id}`}/>:selected.kind==="effect"?<ChainGraph of={`platform.effect/${selected.id}`}/>:null}
  </Panel>}
 </div>;
}

export function ApplicationRuntimePanel({owner,name}:{owner:string;name:string}) {
 const query=useReadQuery<Api.ApplicationRuntime>("/v1/applications/"+encodeURIComponent(owner)+"/"+encodeURIComponent(name)+"/runtime"),runtime=query.isError?undefined:query.data;
 if(query.isError)return <Panel role="alert">{t("Application runtime is unavailable. Refresh after publishing the application.")}</Panel>;
 if(!runtime)return <p role="status">{t("Loading…")}</p>;
 const monitoring=runtime.monitoring,runs=monitoring.runs;
 const refusalColumns:ColumnDef<Api.RefusalCodeCount,any>[]=[
  {accessorKey:"category",header:t("Result category"),cell:cell=>refusalCategoryName(cell.getValue())},
  {accessorKey:"type",header:t("Object type")},
  {accessorKey:"code",header:t("Refusal code"),cell:cell=><span className="break-all font-mono">{cell.getValue()}</span>},
  {accessorKey:"count",header:t("Count"),meta:{align:"right",width:80}},
 ];
 const bindings=[...runtime.artifact.model,...runtime.artifact.rules,...runtime.artifact.pages,...runtime.artifact.flows,...runtime.artifact.ai,...runtime.artifact.integrations,...(runtime.artifact.samples??[])];
 return <Panel className="grid min-w-0 gap-4" aria-label={t("Application artifact")}>
  <div className="flex flex-wrap items-center gap-2">
   <h2 className="text-sm font-semibold">{t("Application artifact")}</h2>
   <Tag label={t(runtime.state)} tone={runtime.state==="isolated"?"danger":runtime.state==="degraded"?"warning":"success"}/>
   <ContextHelp title={t("Application artifact")}>{t("Counts and diagnostics follow your authorized scope.")}</ContextHelp>
  </div>
  <PropertyList columns={4} items={[
   [t("Accepted"),monitoring.accepted],
   [t("Refused"),monitoring.refused],
   [t("Pending effects"),runs.pendingEffects],
   [t("Waiting workflows"),runs.waitingFlows],
  ]}/>
  {(monitoring.constraintFailures>0||runtime.recovery.failedRuns>0)&&<div role="alert">
   <PropertyList columns={2} items={[
    [t("Constraint failures"),monitoring.constraintFailures],
    [t("Failed runs"),runtime.recovery.failedRuns],
   ]}/>
  </div>}
  {runtime.warnings.map((warning,index)=><p role="alert" className="text-sm text-warning" key={index}>{t("Declared {resource} scale exceeded: {actual} / {expected}",{resource:warning.resource||t("Runs"),actual:warning.actual,expected:warning.expected})}</p>)}
  <div className="grid min-w-0 gap-2 border-t border-border pt-3">
   <Disclosure summary={t("Versions and recovery")}>
    <div className="grid gap-3 py-3">
     <PropertyList items={[
      [t("Current content version"),<code className="select-all break-all">{runtime.artifact.version}</code>],
      ...(runtime.previous?[[t("Previous content version"),<code className="select-all break-all">{runtime.previous}</code>] as [string,ReactNode]]:[]),
     ]}/>
     <PropertyList columns={2} items={[
      [t("Recovery"),t(runtime.recovery.state)],
      [t("Predecessors"),t(runtime.recovery.predecessors)],
      [t("Pending effects"),runtime.recovery.pendingEffects],
      [t("Failed runs"),runtime.recovery.failedRuns],
     ]}/>
    </div>
   </Disclosure>
   <Disclosure summary={t("Refusal codes by object type")}>
    <div className="grid gap-3 py-3">
     <PropertyList columns={3} items={[
      [t("Business refusals"),monitoring.businessRefusals],
      [t("Refusal rate"),(monitoring.refusalRate*100).toFixed(1)+"%"],
      [t("Constraint failures"),monitoring.constraintFailures],
     ]}/>
     <DataTable data={monitoring.refusalCodes??[]} columns={refusalColumns} getRowId={entry=>entry.category+"/"+entry.type+"/"+entry.code} height={180} searchable={false} empty={t("No recorded refusals in this application.")}/>
    </div>
   </Disclosure>
   <Disclosure summary={t("AI usage and budgets")}>
    <div className="grid gap-4 py-3">
     <PropertyList columns={3} items={[
      [t("AI tokens"),runs.aiTokens],
      [t("Reported AI cost"),"$"+runs.aiUsd.toFixed(4)],
      [t("Runs without complete cost reporting"),runs.unmeteredAI],
     ]}/>
     {monitoring.memberQuota&&<div className="grid gap-2">
      <h3 className="text-sm font-medium">{t("Your shared AI quota")}</h3>
      <PropertyList columns={2} items={[
       [t("Tokens today"),monitoring.memberQuota.usedTokens+" / "+(monitoring.memberQuota.dailyTokens||t("No limit"))],
       [t("Calls this minute"),monitoring.memberQuota.usedCalls+" / "+(monitoring.memberQuota.perMinute||t("No limit"))],
      ]}/>
     </div>}
     {!!monitoring.agentBudgets?.length&&<div className="grid gap-2">
      <h3 className="text-sm font-medium">{t("Agent budgets per run")}</h3>
      <DataTable data={monitoring.agentBudgets} columns={[
       {id:"resource",header:t("Resource"),cell:cell=><code className="break-all">{cell.row.original.resource.ref.name}</code>},
       {id:"steps",header:t("Steps"),cell:cell=>cell.row.original.budget.Steps||t("Owner default")},
       {id:"tokens",header:t("Tokens"),cell:cell=>cell.row.original.budget.Tokens||t("Owner default")},
       {id:"actions",header:t("Actions"),cell:cell=>cell.row.original.budget.Actions||t("Owner default")},
       {id:"cost",header:t("Cost"),cell:cell=>cell.row.original.budget.Cost||t("No limit")},
      ]} getRowId={item=>item.resource.ref.name} height={180} searchable={false}/>
     </div>}
    </div>
   </Disclosure>
   <Disclosure summary={t("Application data")}>
    <div className="py-3"><DataTable data={runtime.data} columns={[
     {id:"resource",header:t("Object type"),cell:cell=><code className="break-all">{cell.row.original.ref.name}</code>},
     {accessorKey:"records",header:t("Records"),meta:{align:"right"}},
     {accessorKey:"expected",header:t("Declared records"),cell:cell=>cell.row.original.expected||"—",meta:{align:"right"}},
    ]} getRowId={item=>item.ref.app+":"+item.ref.name} height={200} searchable={false}/></div>
   </Disclosure>
   {!!runtime.ai?.length&&<Disclosure summary={t("AI placement and evaluations")}>
    <div className="grid gap-4 py-3">{runtime.ai.map(item=><Panel variant="plain" key={item.resource.ref.name} className="grid min-w-0 gap-3 border-b border-border pb-4 last:border-b-0 last:pb-0">
     <h3 className="break-all text-sm font-medium">{item.resource.ref.name}</h3>
     <PropertyList columns={3} items={[
      [t("Placement"),placementName(item.placement)],
      [t("Model"),item.model||t("Application default model")],
      [t("Calls"),item.calls],
      [t("Input tokens"),item.input],
      [t("Output tokens"),item.output],
      [t("Reported AI cost"),"$"+item.cost.toFixed(4)],
      [t("Runs without complete cost reporting"),item.costUnknown],
     ]}/>
     {!!item.sharedCalls&&<ContextHelp title={t("Shared calls")}>{t("Shared calls are attributed to each declaring application.")}</ContextHelp>}
     {(!!item.adoptions?.length||!!item.checkpoints?.length||!!item.handoffRole)&&<PropertyList items={[
      ...(item.adoptions?.length?[[t("Adoption action"),<ul className="grid gap-1">{item.adoptions.map(action=><li className="break-all font-mono text-xs" key={action}>{action}</li>)}</ul>] as [string,ReactNode]]:[]),
      ...(item.checkpoints?.length?[[t("Approval checkpoints"),item.checkpoints.join(", ")] as [string,ReactNode]]:[]),
      ...(item.handoffRole?[[t("Handover role"),item.handoffRole] as [string,ReactNode]]:[]),
     ]}/>}
     {!!item.evaluations?.length&&<DataTable data={item.evaluations} columns={[
      {accessorKey:"id",header:t("Evaluation"),cell:cell=><code className="select-all break-all">{cell.row.original.id}</code>},
      {accessorKey:"state",header:t("State"),cell:cell=>t(cell.row.original.state)},
      {accessorKey:"model",header:t("Model")},
      {accessorKey:"quality",header:t("Quality")},
     ]} getRowId={report=>report.id} height={180} searchable={false}/>}
    </Panel>)}</div>
   </Disclosure>}
   <Disclosure summary={t("Artifact contents and dependencies")}>
    <div className="py-3"><DataTable data={bindings} columns={[
     {id:"app",header:t("Application"),cell:cell=>cell.row.original.ref.app},
     {id:"kind",header:t("Kind"),cell:cell=>cell.row.original.ref.kind},
     {id:"name",header:t("Resource"),cell:cell=><code className="break-all">{cell.row.original.ref.name}</code>},
     {accessorKey:"sourceVersion",header:t("Version"),cell:cell=><code className="select-all break-all">{cell.row.original.sourceVersion}</code>},
    ]} getRowId={binding=>binding.ref.app+":"+binding.ref.kind+":"+binding.ref.name} height={240} searchable={false}/></div>
   </Disclosure>
   {!!runtime.artifact.environment.length&&<Disclosure summary={t("Required environment bindings")}>
    <div className="py-3"><DataTable data={runtime.artifact.environment} columns={[
     {id:"resource",header:t("Resource"),cell:cell=>cell.row.original.ref.name},
     {accessorKey:"kind",header:t("Kind")},
     {accessorKey:"name",header:t("Name")},
     {id:"credential",header:t("Credentials"),cell:cell=>cell.row.original.credential?t("Credential required"):t("No credential required")},
    ]} getRowId={binding=>binding.ref.app+":"+binding.ref.name} height={180} searchable={false}/></div>
   </Disclosure>}
   {!!runtime.artifact.roles.length&&<Disclosure summary={t("Declared roles and scopes")}>
    <div className="py-3"><DataTable data={runtime.artifact.roles.map((role,index)=>({...role,rowId:String(index)}))} columns={[
     {id:"resource",header:t("Resource"),cell:cell=>cell.row.original.resource.ref.name},
     {accessorKey:"role",header:t("Role")},
     {accessorKey:"scope",header:t("Scope"),cell:cell=>cell.row.original.scope||t("Declared action role")},
     {id:"readFields",header:t("Read fields"),cell:cell=><span className="break-all">{cell.row.original.readFields?.join(", ")||"—"}</span>},
     {id:"writeFields",header:t("Write fields"),cell:cell=><span className="break-all">{cell.row.original.writeFields?.join(", ")||"—"}</span>},
    ]} getRowId={role=>role.rowId} height={200} searchable={false}/></div>
   </Disclosure>}
   <RecordDiagnostic owner={owner} name={name} types={runtime.data.map(data=>data.ref.name)}/>
   {runtime.artifact.profile&&<Disclosure summary={t("Operating declarations")}>
    <div className="grid gap-3 py-3">
     <ContextHelp title={t("Operating declarations")}>{t("Declared retention and recovery targets require environment evidence.")}</ContextHelp>
     <pre className="overflow-auto text-xs">{JSON.stringify(runtime.artifact.profile,null,2)}</pre>
    </div>
   </Disclosure>}
   {runtime.maintenance&&<Disclosure summary={t("Application maintenance")}>
    <div className="grid gap-4 py-3">
     <DataTable data={runtime.versions} columns={[
      {accessorKey:"version",header:t("Version"),cell:cell=><code className="select-all break-all">{cell.row.original.version}</code>},
      {id:"active",header:t("State"),cell:cell=>cell.row.original.active?t("Current content version"):t("Previous content version")},
      {accessorKey:"at",header:t("Time")},
     ]} getRowId={version=>version.release+":"+version.version} height={180} searchable={false}/>
     {!!runtime.maintenance.extensions.length&&<div className="grid gap-2"><h3 className="text-sm font-medium">{t("Extensions")}</h3><PropertyList items={runtime.maintenance.extensions.map(item=>[item.resource.ref.name,item.base])}/></div>}
     {!!runtime.maintenance.retention.length&&<div className="grid gap-2">
      <div className="flex items-center gap-2"><h3 className="text-sm font-medium">{t("Retention declaration")}</h3><ContextHelp title={t("Retention declaration")}>{t("Retention is declared; automatic deletion is not enabled by this declaration.")}</ContextHelp></div>
      <PropertyList items={runtime.maintenance.retention.map(item=>[t(item.category),item.duration])}/>
     </div>}
    </div>
   </Disclosure>}
  </div>
 </Panel>;
}

function RecordDiagnostic({owner,name,types}:{owner:string;name:string;types:string[]}) {
 const [typ,setType]=useState(""),[id,setID]=useState(""),[selection,setSelection]=useState<{type:string;id:string}>();
 const selectedType=typ||types[0]||"";
 const query=useReadQuery<Api.ApplicationDiagnostic>(`/v1/applications/${encodeURIComponent(owner)}/${encodeURIComponent(name)}/records/${encodeURIComponent(selection?.type??"")}/${encodeURIComponent(selection?.id??"")}/diagnostic`,undefined,!!selection);
 return <Disclosure summary={t("Diagnose a record")}>
  <div className="mt-2 flex flex-wrap gap-2"><Select aria-label={t("Object type")} value={selectedType} onChange={event=>setType(event.target.value)}>{types.map(value=><option key={value} value={value}>{value}</option>)}</Select><Input aria-label={t("Record ID")} value={id} onChange={event=>setID(event.target.value)}/><Button disabled={!selectedType||!id.trim()} onClick={()=>setSelection({type:selectedType,id:id.trim()})}>{t("Diagnose")}</Button></div>
  {selection&&query.isPending&&<p role="status">{t("Loading…")}</p>}
  {selection&&query.isError&&<p role="alert">{t("This record diagnostic is unavailable to you.")}</p>}
  {selection&&query.data&&!query.isError&&<div className="mt-2 grid gap-3">
   <ol className="grid gap-2 text-xs">{query.data.decisions.map(decision=><li key={decision.change}><strong>{decision.schema}</strong> · <span className="font-mono">{decision.change}</span> · {formatStamp(decision.at)}
    <p className="break-all font-mono">{t("Definition content")}: {decision.content?.version??t("Not recorded")}</p>
    {decision.definition&&<ul>{decision.definition.dependencies.map(binding=><li key={`${binding.ref.app}:${binding.ref.kind}:${binding.ref.name}`} className="break-all font-mono">{binding.ref.name} @ {binding.sourceVersion}</li>)}</ul>}
   </li>)}</ol>
   <p className="text-xs">{t("{count} related runs",{count:query.data.runs.length})}</p>
   <ChainGraph of={`${selection.type}/${selection.id}`}/>
  </div>}
 </Disclosure>;
}

function RunBinding({run}:{run:Pick<Run,"release"|"version"|"dependencies"|"module">}) {
 return <PropertyList items={[
  [t("Startup version"),<code className="select-all break-all">{run.version||t("Not recorded")}</code>],
  [t("Startup release"),<code className="select-all break-all">{run.release||t("No startup activation recorded")}</code>],
  ...(run.dependencies?[[t("Dependency release"),<code className="select-all break-all">{run.dependencies}</code>] as [string,ReactNode]]:[]),
  ...(run.module?[[t("Module"),<code className="select-all break-all">{run.module}</code>] as [string,ReactNode]]:[]),
 ]}/>;

}

/** The original compute read and effect retry action, including source checks. */
export function OperationRunView({id,application,project,subject}:{id:string;application?:string;project?:string;subject?:string}) {
 const {can}=useHost(),{open}=useWorkspace();
 const control=useRunControl("platform.effect",id);
 const query=useReadQuery<Api.OperationResult>(`/v1/capabilities/calls/compute/${encodeURIComponent(id)}`),answer=query.data;
 if(query.isError)return <Panel role="alert">{t("This calculation run is unavailable to you.")}</Panel>;
 if(!answer)return <p role="status">{t("Loading…")}</p>;
 return <div className="grid min-w-0 gap-3">
  {project?<Button variant="ghost" onClick={()=>open({view:"project",params:{id:project,section:"runs"}})}>{t("Back to project")}</Button>:application&&<Button variant="ghost" onClick={()=>open({view:"application-runs",params:{app:(application.split(":")[0]??""),name:application.split(":").slice(1).join(":"),application}})}>{t("Back to application runs")}</Button>}
  {subject?.startsWith("flow.instance/")&&<Button variant="ghost" onClick={()=>open({view:"flow-instance",params:{id:subject.slice("flow.instance/".length),...(application?{application}:{}),...(project?{project}:{})}},{window:"beside"})}>{t("Open related workflow run")}</Button>}
  {control.error&&<Panel role="alert" className="text-sm text-danger">{control.error}</Panel>}
  {control.pending&&<Panel role="status" className="grid gap-2 text-sm"><p>{t("A runtime decision is unconfirmed. Retry the original operation; leaving does not cancel accepted work.")}</p><Button disabled={control.busy} onClick={()=>void control.retry()}>{t("Retry original operation")}</Button></Panel>}
  <Tag label={stateName(answer.state)} tone={answer.state==="failed"?"danger":"neutral"}/>
  <Disclosure summary={t("Run details")}><div className="grid gap-3 py-3">
   <RunBinding run={{version:answer.version?String(answer.version):answer.ownerVersion?"code:"+answer.ownerVersion:undefined,release:answer.release,dependencies:answer.dependencies,module:answer.module}}/>
   {answer.definition&&<PropertyList items={[[t("Startup definition"),<code className="select-all break-all">{answer.definition}</code>]]}/>}
  </div></Disclosure>
  {answer.error&&<Panel role="alert">{answer.error}</Panel>}
  {answer.state==="failed"&&can("platform.effect.retry")&&<Button disabled={control.busy||!!control.pending} onClick={()=>void control.control("platform.effect.retry")}>{t("Retry")}</Button>}
  {answer.output!==undefined&&<pre className="max-h-80 overflow-auto text-xs">{JSON.stringify(answer.output,null,2)}</pre>}
  <ChainGraph of={`platform.effect/${id}`}/>
 </div>;
}
