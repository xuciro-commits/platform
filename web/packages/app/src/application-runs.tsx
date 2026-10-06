import type { Api } from "@platform/kernel";
import { Button, DataTable, PageHeader, Panel, Tag, t, useWorkspace, type ColumnDef } from "@platform/ui";
import { useState } from "react";
import { FlowInstanceView } from "./flows";
import { RunView } from "./agents";
import { ChainGraph, useHost, useReadQuery, useRecordInventory } from "./index";

type Run = Api.ApplicationRun;
const kindName = (kind: string) => t(({ flow: "Workflow", agent: "Agent", compute: "Calculation" } as Record<string, string>)[kind] ?? kind);
const stateName = (state: string) => t(({ running: "Working", waiting: "Waiting", done: "Done", stopped: "Stopped", stuck: "Stuck", completed: "Completed", pending: "Pending", failed: "Failed", canceled: "Canceled", cancelled: "Canceled" } as Record<string,string>)[state] ?? state);

/** One application projection, preserving original run readers and controls. */
export function ApplicationRuns({ owner, name }: { owner: string; name: string }) {
 const { role } = useHost(), { open } = useWorkspace();
 const [offset,setOffset]=useState(0),[chosen,setChosen]=useState<string>();
 const query=useReadQuery<Api.ApplicationRunPage>(`/v1/applications/${encodeURIComponent(owner)}/${encodeURIComponent(name)}/runs?offset=${offset}&limit=50`);
 const builder=role("build")==="builder" && !!query.data;
 const applications=useRecordInventory<{id:string;name:string}>("build.app",1000,builder && owner==="build");
 const selected=query.data?.runs.find(run=>`${run.kind}/${run.id}`===chosen);
 const processes=useRecordInventory<{id:string;name:string}>("build.process",1000,builder && selected?.resource?.app==="build" && selected.kind==="flow");
 const computations=useRecordInventory<{id:string;name:string}>("build.code",1000,builder && selected?.resource?.app==="build" && selected.kind==="compute");
 const application= `${owner}:${name}`;
 const columns:ColumnDef<Run>[]=[
  {accessorKey:"kind",header:t("Kind"),cell:c=>kindName(c.row.original.kind)},
  {accessorKey:"title",header:t("Run"),cell:c=><Button variant="link" onClick={()=>setChosen(`${c.row.original.kind}/${c.row.original.id}`)}>{c.row.original.title||c.row.original.id}</Button>},
  {accessorKey:"resourceName",header:t("Resource")},
  {accessorKey:"state",header:t("State"),cell:c=><Tag label={stateName(c.row.original.state)} tone={["failed","stuck","stopped"].includes(c.row.original.state)?"danger":"neutral"}/>},
  {accessorKey:"version",header:t("Startup version"),cell:c=><span className="font-mono text-xs">{c.row.original.version||t("Not recorded")}</span>},
  {accessorKey:"node",header:t("Failed node")},
  {accessorKey:"association",header:t("Relationship"),cell:c=>t(({resource:"Referenced resource",flow:"Started by a related workflow",subject:"Record type used by the application"} as Record<string,string>)[c.row.original.association]??c.row.original.association)},
 ];
 const source=selected?.kind==="flow"?processes.data?.records.find(record=>`build.${record.name}`===selected.resource?.name):selected?.kind==="compute"?computations.data?.records.find(record=>record.name===selected.resource?.name):undefined;
 const draft=applications.data?.records.find(record=>record.name===name);
 return <div className="grid min-w-0 gap-3">
  <PageHeader title={query.data?.title ?? name} description={t("Runs related to this application's referenced resources. Shared use does not establish which application started a run.")} />
  {query.isError?<Panel role="alert">{t("Application runs are unavailable to you.")}</Panel>:<>
   <DataTable data={query.data?.runs??[]} columns={columns} getRowId={run=>`${run.kind}/${run.id}`} selectedId={chosen} onRowClick={run=>setChosen(`${run.kind}/${run.id}`)} searchable={false} loading={query.isPending} empty={t("No related runs you can read.")} height={320}/>
   <div className="flex items-center gap-2 text-xs"><Button size="sm" disabled={offset===0} onClick={()=>setOffset(Math.max(0,offset-50))}>{t("Previous page")}</Button>
    <span>{offset+1}–{offset+(query.data?.runs.length??0)} / {query.data?.total??0}</span>
    <Button size="sm" disabled={offset+50>=(query.data?.total??0)} onClick={()=>setOffset(offset+50)}>{t("Next page")}</Button></div>
  </>}
  {selected&&<Panel className="grid min-w-0 gap-3">
   <div className="flex flex-wrap gap-2"><Button onClick={()=>open({view:selected.kind==="flow"?"flow":selected.kind==="agent"?"run":"operation-run",params:{id:selected.id,application}}, {window:"beside"})}>{t("Open run")}</Button>
    {source&&<Button onClick={()=>open({view:selected.kind==="flow"?"workflow":"code",params:{id:source.id,...(draft?{application:draft.id}:{})}})}>{t("Open current source editor")}</Button>}
    {selected.kind==="agent"&&<Button onClick={()=>open({view:"agents",params:{surface:"tenant"}})}>{t("Current agent registry")}</Button>}
   </div>
   <p className="text-xs text-muted">{t("The editor opens the current saved source. The startup binding below belongs to this run.")}</p>
   <RunBinding run={selected}/>
   {selected.error&&<p role="alert">{selected.node?`${t("Failed node")}: ${selected.node} · `:""}{selected.error}</p>}
   {!!selected.shared.length&&<p role="status" className="text-xs text-muted">{t("Also referenced by: {applications}",{applications:selected.shared.join(", ")})}</p>}
   {selected.kind==="flow"?<FlowInstanceView id={selected.id}/>:selected.kind==="agent"?<RunView id={selected.id} application={application}/>:<OperationRunView id={selected.id} application={application}/>}
  </Panel>}
 </div>;
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
export function OperationRunView({id,application}:{id:string;application?:string}) {
 const {can,decide}=useHost(),{open}=useWorkspace();
 const query=useReadQuery<Api.OperationResult>(`/v1/capabilities/calls/compute/${encodeURIComponent(id)}`),answer=query.data;
 if(query.isError)return <Panel role="alert">{t("This calculation run is unavailable to you.")}</Panel>;
 if(!answer)return <p role="status">{t("Loading…")}</p>;
 return <div className="grid min-w-0 gap-3">
  {application&&<Button variant="ghost" onClick={()=>open({view:"application-runs",params:{app:(application.split(":")[0]??""),name:application.split(":").slice(1).join(":")}})}>{t("Back to application runs")}</Button>}
  <p>{stateName(answer.state)}</p>
  <RunBinding run={{version:answer.version?String(answer.version):answer.ownerVersion?`code:${answer.ownerVersion}`:undefined,release:answer.release,dependencies:answer.dependencies,module:answer.module}}/>
  {answer.definition&&<p className="break-all font-mono text-xs">{t("Startup definition")}: {answer.definition}</p>}
  {answer.error&&<Panel role="alert">{answer.error}</Panel>}
  {answer.state==="failed"&&can("platform.effect.retry")&&<Button onClick={()=>void decide("platform.effect.retry",{type:"platform.effect",id},{})}>{t("Retry")}</Button>}
  {answer.output!==undefined&&<pre className="max-h-80 overflow-auto text-xs">{JSON.stringify(answer.output,null,2)}</pre>}
  <ChainGraph of={`platform.effect/${id}`}/>
 </div>;
}
