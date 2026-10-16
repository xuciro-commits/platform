import { useRunControl } from "./use-run-control";
import type { Api } from "@platform/kernel";
import { Card, Button, DataTable, Disclosure, PageHeader, Panel, Input, Select, Tag, t, useWorkspace, type ColumnDef } from "@platform/ui";
import { useState } from "react";
import { FlowInstanceView } from "./flows";
import { RunView } from "./agents";
import { ChainGraph, useHost, useOpenRecord, useReadQuery, useRecordInventory } from "../index";
import {refusalCategoryName} from "../actions/refusal";
import { DispatchProgress } from "../widgets/DispatchProgress";

type Run = Api.ApplicationRun;
const kindName = (kind: string) => t(({ flow: "Workflow", agent: "Agent", compute: "Calculation", function: "AI function", effect: "External effect", job: "Scheduled job", dispatch: "Device dispatch" } as Record<string, string>)[kind] ?? kind);
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
  <PageHeader title={query.data?.title ?? name} description={t("Runs related to this application's referenced resources. Shared use does not establish which application started a run.")} />
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
   <div className="flex flex-wrap gap-2">{["flow","agent","compute"].includes(selected.kind)&&<Button onClick={()=>open({view:selected.kind==="flow"?"flow-instance":selected.kind==="agent"?"run":"operation-run",params:{id:selected.id,...context,...(selected.kind==="compute"&&selected.subject?{subject:selected.subject}:{})}}, {window:"beside"})}>{t("Open run")}</Button>}
    {selected.subject&&<Button onClick={()=>selected.subject!.startsWith("flow.instance/")?open({view:"flow-instance",params:{id:selected.subject!.slice("flow.instance/".length),...context}},{window:"beside"}):openRecord(selected.subject!)}>{t("Open related record")}</Button>}
    {selected.kind === "dispatch" && <Button onClick={() => openRecord({ type: "build.dispatch", id: selected.id }, {window:"tab"})}>{t("Open dispatch record")}</Button>}
    {selected.kind === "function" && <Button onClick={() => openRecord({type:"build.function-call",id:selected.id},{window:"tab"})}>{t("Open function call")}</Button>}
    {selected.kind === "effect" && can("platform.effect.resolve") && <Button onClick={()=>open({view:"integrations"},{window:"beside"})}>{t("Open original delivery controls")}</Button>}
    {selected.kind === "job" && can("platform.work.retry") && <Button onClick={()=>open({view:"automation-operations"},{window:"beside"})}>{t("Open original scheduler controls")}</Button>}
    {source&&<Button onClick={()=>open({view:({flow:"flow",compute:"code",function:"function",agent:"agent"} as Record<string,string>)[selected.kind]!,params:{id:source.id,...(draft?{application:draft.id}:{})}},{window:"beside"})}>{t("Open current source editor")}</Button>}
    {selected.kind==="agent"&&!source&&<Button onClick={()=>open({view:"agents",params:{surface:"tenant"}},{window:"beside"})}>{t("Current agent registry")}</Button>}
   </div>
   <p className="text-xs text-muted">{t("The editor opens the current saved source. The startup binding below belongs to this run.")}</p>
   <RunBinding run={selected}/>
   {selected.subject&&<p className="break-all font-mono text-xs">{t("Related record")}: {selected.subject}</p>}
   {!!selected.applications?.length&&<ul className="grid gap-1 text-xs">{selected.applications.map(binding=><li key={`${binding.ref.app}:${binding.ref.name}`} className="break-all font-mono">{binding.ref.app} · {binding.ref.name} @ {binding.sourceVersion}</li>)}</ul>}
   {selected.error&&<p role="alert">{selected.node?`${t("Failed node")}: ${selected.node} · `:""}{selected.error}</p>}
   {!!selected.shared.length&&<p role="status" className="text-xs text-muted">{t("Also referenced by: {applications}",{applications:selected.shared.join(", ")})}</p>}
   {selected.kind==="flow"?<FlowInstanceView key={selected.id} id={selected.id} {...context}/>:selected.kind==="agent"?<RunView key={selected.id} id={selected.id} {...context}/>:selected.kind==="compute"?<OperationRunView key={selected.id} id={selected.id} subject={selected.subject} {...context}/>:selected.kind==="function"?<ChainGraph of={`build.function-call/${selected.id}`}/>:selected.kind==="effect"?<ChainGraph of={`platform.effect/${selected.id}`}/>:selected.kind==="dispatch"?<DispatchProgress type="build.dispatch" id={selected.id}/>:<p className="text-xs text-muted">{t("Shared scheduler work remains with its original owner.")}</p>}
  </Panel>}
 </div>;
}

export function ApplicationRuntimePanel({owner,name}:{owner:string;name:string}) {
 const query=useReadQuery<Api.ApplicationRuntime>(`/v1/applications/${encodeURIComponent(owner)}/${encodeURIComponent(name)}/runtime`),runtime=query.isError?undefined:query.data;
 if(query.isError)return <Panel role="alert">{t("Application runtime is unavailable. Refresh after publishing the application.")}</Panel>;
 if(!runtime)return <p role="status">{t("Loading…")}</p>;
 const refusalColumns:ColumnDef<Api.RefusalCodeCount,any>[]=[
  {accessorKey:"category",header:t("Result category"),cell:cell=>refusalCategoryName(cell.getValue())},
  {accessorKey:"type",header:t("Object type")},
  {accessorKey:"code",header:t("Refusal code"),cell:cell=><span className="break-all font-mono">{cell.getValue()}</span>},
  {accessorKey:"count",header:t("Count"),meta:{align:"right",width:80}},
 ];
 return <Panel className="grid min-w-0 gap-3" aria-label={t("Application artifact")}>
  <div className="flex flex-wrap gap-2"><strong>{t("Application artifact")}</strong><Tag label={t(runtime.state)} tone={runtime.state==="isolated"?"danger":runtime.state==="degraded"?"warning":"success"}/><span className="text-xs text-muted">{t("Counts and diagnostics follow your authorized scope.")}</span></div>
  <dl className="grid gap-2 text-xs"><div><dt>{t("Current content version")}</dt><dd className="break-all font-mono">{runtime.artifact.version}</dd></div>{runtime.previous&&<div><dt>{t("Previous content version")}</dt><dd className="break-all font-mono">{runtime.previous}</dd></div>}<div><dt>{t("Recovery")}</dt><dd>{t(runtime.recovery.state)} · {t("Predecessors")}: {t(runtime.recovery.predecessors)} · {t("{pending} pending effects · {failed} failed runs",{pending:runtime.recovery.pendingEffects,failed:runtime.recovery.failedRuns})}</dd></div></dl>
  <p className="text-xs">{t("{accepted} accepted · {refused} refused · {constraints} constraint failures",{accepted:runtime.monitoring.accepted,refused:runtime.monitoring.refused,constraints:runtime.monitoring.constraintFailures})} · {t("{tokens} AI tokens · {pending} pending effects · {waiting} waiting workflows",{tokens:runtime.monitoring.runs.aiTokens,pending:runtime.monitoring.runs.pendingEffects,waiting:runtime.monitoring.runs.waitingFlows})}</p>
  <p className="text-xs">{t("{count} business refusals · {rate}% refusal rate",{count:runtime.monitoring.businessRefusals,rate:(runtime.monitoring.refusalRate*100).toFixed(1)})}</p>
  <Disclosure summary={t("Refusal codes by object type")}><DataTable data={runtime.monitoring.refusalCodes??[]} columns={refusalColumns} getRowId={entry=>`${entry.category}/${entry.type}/${entry.code}`} height={180} searchable={false} empty={t("No recorded refusals in this application.")}/></Disclosure>
  <p className="text-xs text-muted">{t("Reported AI cost: ${cost}; {unknown} runs lack complete cost reporting.",{cost:runtime.monitoring.runs.aiUsd.toFixed(4),unknown:runtime.monitoring.runs.unmeteredAI})}</p>
  {runtime.monitoring.memberQuota&&<p className="text-xs">{t("Your shared AI quota: {tokens} / {daily} tokens today; {calls} / {minute} calls this minute.",{tokens:runtime.monitoring.memberQuota.usedTokens,daily:runtime.monitoring.memberQuota.dailyTokens||t("No limit"),calls:runtime.monitoring.memberQuota.usedCalls,minute:runtime.monitoring.memberQuota.perMinute||t("No limit")})}</p>}
  {!!runtime.monitoring.agentBudgets?.length&&<Disclosure summary={t("Agent budgets per run")}><ul className="grid gap-1 text-xs">{runtime.monitoring.agentBudgets.map(item=><li key={item.resource.ref.name}>{item.resource.ref.name} · {t("{steps} steps · {tokens} tokens · {actions} actions · ${cost}",{steps:item.budget.Steps||t("Owner default"),tokens:item.budget.Tokens||t("Owner default"),actions:item.budget.Actions||t("Owner default"),cost:item.budget.Cost||t("No limit")})}</li>)}</ul></Disclosure>}
  {runtime.warnings.map((warning,index)=><p role="alert" className="text-sm text-warning" key={index}>{t("Declared {resource} scale exceeded: {actual} / {expected}",{resource:warning.resource||t("Runs"),actual:warning.actual,expected:warning.expected})}</p>)}
  <ul className="grid gap-1 text-xs">{runtime.data.map(data=><li key={`${data.ref.app}:${data.ref.name}`}><span className="font-mono">{data.ref.name}</span> · {data.records}{data.expected?` / ${data.expected}`:""}</li>)}</ul>
  {!!runtime.ai?.length&&<Disclosure summary={t("AI placement and evaluations")}><div className="grid gap-2 py-2">{runtime.ai.map(item=><Card key={item.resource.ref.name} className="grid gap-1 p-2 text-xs"><strong>{item.resource.ref.name} · {placementName(item.placement)}</strong><p>{item.model||t("Application default model")}</p><p>{t("{calls} calls · {input} input tokens · {output} output tokens",{calls:item.calls,input:item.input,output:item.output})}</p><p>{t("Reported AI cost: ${cost}; {unknown} runs lack complete cost reporting.",{cost:item.cost.toFixed(4),unknown:item.costUnknown})}</p>{!!item.sharedCalls&&<p>{t("Shared calls are attributed to each declaring application.")}</p>}{item.adoptions.map(action=><p key={action}>{t("Adoption action")}: {action}</p>)}{!!item.checkpoints.length&&<p>{t("Approval checkpoints")}: {item.checkpoints.join(", ")}</p>}{item.handoffRole&&<p>{t("Handover role")}: {item.handoffRole}</p>}{item.evaluations.map(report=><p key={report.id}>{t("Evaluation")}: {report.id} · {t(report.state)} · {report.model} · {report.quality}</p>)}</Card>)}</div></Disclosure>}
  <Disclosure summary={t("Artifact contents and dependencies")}><ul className="mt-2 grid gap-1 text-xs">{[...runtime.artifact.model,...runtime.artifact.rules,...runtime.artifact.pages,...runtime.artifact.flows,...runtime.artifact.ai,...runtime.artifact.integrations,...(runtime.artifact.samples??[])].map(binding=><li key={`${binding.ref.app}:${binding.ref.kind}:${binding.ref.name}`} className="break-all font-mono">{binding.ref.app} · {binding.ref.kind} · {binding.ref.name} @ {binding.sourceVersion}</li>)}</ul></Disclosure>
  {!!runtime.artifact.environment.length&&<Disclosure summary={t("Required environment bindings")}><ul className="mt-2 grid gap-1 text-xs">{runtime.artifact.environment.map(binding=><li key={`${binding.ref.app}:${binding.ref.name}`}>{binding.ref.name} · {binding.kind}{binding.name?` · ${binding.name}`:""} · {binding.credential?t("Credential required"):t("No credential required")}</li>)}</ul></Disclosure>}
  {!!runtime.artifact.roles.length&&<Disclosure summary={t("Declared roles and scopes")}><ul className="mt-2 grid gap-1 text-xs">{runtime.artifact.roles.map((role,index)=><li key={index}>{role.resource.ref.name} · {role.role} · {role.scope||t("Declared action role")}{role.readFields?.length?` · ${t("Read fields")}: ${role.readFields.join(", ")}`:""}{role.writeFields?.length?` · ${t("Write fields")}: ${role.writeFields.join(", ")}`:""}</li>)}</ul></Disclosure>}
  <RecordDiagnostic owner={owner} name={name} types={runtime.data.map(data=>data.ref.name)}/>
  {runtime.artifact.profile&&<Disclosure summary={t("Operating declarations")}><p className="text-xs text-muted">{t("Declared retention and recovery targets require environment evidence.")}</p><pre className="overflow-auto text-xs">{JSON.stringify(runtime.artifact.profile,null,2)}</pre></Disclosure>}
  {runtime.maintenance&&<Disclosure summary={t("Application maintenance")}>
    <ul className="grid gap-1 text-xs">{runtime.versions.map(version=><li key={`${version.release}:${version.version}`} className="break-all">{version.version} · {version.active?t("Current content version"):t("Previous content version")}{version.at?` · ${version.at}`:""}</li>)}</ul>
    <ul className="mt-2 grid gap-1 text-xs">{runtime.maintenance.migrations.map(migration=><li key={migration.id}>{t("Migration")} {migration.id} · {t(migration.state)}</li>)}{runtime.maintenance.retired.map((item,index)=><li key={index}>{t("Retirement")} · {item.type} · {item.name} · {t(item.state)}</li>)}{runtime.maintenance.extensions.map(item=><li key={item.resource.ref.name}>{t("Extension")} · {item.resource.ref.name} → {item.base}</li>)}{runtime.maintenance.retention.map(item=><li key={item.category}>{t("Retention declaration")} · {t(item.category)} · {item.duration}</li>)}</ul>
    {!!runtime.maintenance.retention.length&&<p className="text-xs text-muted">{t("Retention is declared; automatic deletion is not enabled by this declaration.")}</p>}
  </Disclosure>}
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
   <ol className="grid gap-2 text-xs">{query.data.decisions.map(decision=><li key={decision.change}><strong>{decision.schema}</strong> · <span className="font-mono">{decision.change}</span> · {new Date(decision.at).toLocaleString()}
    <p className="break-all font-mono">{t("Definition content")}: {decision.content?.version??t("Not recorded")}</p>
    {decision.definition&&<ul>{decision.definition.dependencies.map(binding=><li key={`${binding.ref.app}:${binding.ref.kind}:${binding.ref.name}`} className="break-all font-mono">{binding.ref.name} @ {binding.sourceVersion}</li>)}</ul>}
   </li>)}</ol>
   <p className="text-xs">{t("{count} related runs",{count:query.data.runs.length})}</p>
   <ChainGraph of={`${selection.type}/${selection.id}`}/>
  </div>}
 </Disclosure>;
}

function RunBinding({run}:{run:Pick<Run,"release"|"version"|"dependencies"|"module">}) {
 return <dl className="grid min-w-0 gap-1 text-xs">
  <div><dt className="text-muted">{t("Startup version")}</dt><dd className="break-all font-mono">{run.version||t("Not recorded")}</dd></div>
  <div><dt className="text-muted">{t("Startup release")}</dt><dd className="break-all font-mono">{run.release||t("No startup activation recorded")}</dd></div>
  {run.dependencies&&<div><dt className="text-muted">{t("Dependency release")}</dt><dd className="break-all font-mono">{run.dependencies}</dd></div>}
  {run.module&&<div><dt className="text-muted">{t("Module")}</dt><dd className="break-all font-mono">{run.module}</dd></div>}
 </dl>;
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
  <p>{stateName(answer.state)}</p>
  <RunBinding run={{version:answer.version?String(answer.version):answer.ownerVersion?`code:${answer.ownerVersion}`:undefined,release:answer.release,dependencies:answer.dependencies,module:answer.module}}/>
  {answer.definition&&<p className="break-all font-mono text-xs">{t("Startup definition")}: {answer.definition}</p>}
  {answer.error&&<Panel role="alert">{answer.error}</Panel>}
  {answer.state==="failed"&&can("platform.effect.retry")&&<Button disabled={control.busy||!!control.pending} onClick={()=>void control.control("platform.effect.retry")}>{t("Retry")}</Button>}
  {answer.output!==undefined&&<pre className="max-h-80 overflow-auto text-xs">{JSON.stringify(answer.output,null,2)}</pre>}
  <ChainGraph of={`platform.effect/${id}`}/>
 </div>;
}
