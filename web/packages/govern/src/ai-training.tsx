import { useState } from "react";
import { useHost, useReadQuery as useRead } from "@platform/app";
import { Button, Checkbox, DataTable, Disclosure, Form, Input, Panel, Select, Tag, Textarea, t, type ColumnDef } from "@platform/ui";
import type { Api } from "@platform/kernel";

export function AITraining() {
 const query=useRead<Api.AITrainingReport>("/v1/ai-training");
 const {decide,client}=useHost();
 const [id,setID]=useState(""),[fields,setFields]=useState(""),[days,setDays]=useState("90"),[quality,setQuality]=useState(""),[allowed,setAllowed]=useState(false),[provider,setProvider]=useState(false),[enabled,setEnabled]=useState(false),[format,setFormat]=useState("sft"),[evidence,setEvidence]=useState(""),[selected,setSelected]=useState(""),[error,setError]=useState("");
 const candidates=query.data?.candidates??[];
 const candidate=candidates.find(item=>item.id===selected);
 const qualified=candidates.filter(item=>!item.problems?.length&& (format!=="dpo" || Object.keys(item.rejected??{}).length>0));
 const columns:ColumnDef<Api.AITrainingCandidate,any>[]=[
 {accessorKey:"action",header:t("Action")},
 {accessorKey:"outcome",header:t("Outcome"),cell:({row:{original:c}})=>t(c.outcome)},
 {id:"quality",header:t("Training qualification"),cell:({row:{original:c}})=><Tag label={c.problems?.length?t(c.problems[0]!):t("Qualified training sample")} tone={c.problems?.length?"warning":"success"}/>},
 {id:"review",header:t("Review"),cell:({row:{original:c}})=><Button size="sm" onClick={()=>{setSelected(c.id);setEvidence(c.qualityEvidence??"")}}>{t("Review")}</Button>}
 ];
 return <div className="grid gap-4">
  <Panel title={t("Training candidates")} description={t("Accepted business decisions provide candidate evidence. A valid schema or simulation is not proof of quality. No transcripts or private reasoning are copied.")}>
   <p className="mb-3 text-sm text-muted">{t("The candidate collection retains the newest 1,024 entries within 256 KiB of the original accepted-result budget. Oversized content is reduced to metadata or omitted; original evidence and revocations remain in the accepted history.")}</p>
   {query.isError?<p role="alert">{t("Training data could not be loaded.")}</p>:<DataTable data={candidates} columns={columns} getRowId={c=>c.id} height={240} empty={t("No training candidates yet")}/>}
  </Panel>
  {candidate&&<Panel title={t("Independent quality review")}>
   <div className="mb-4 grid gap-3">
    <dl className="grid grid-cols-[auto_minmax(0,1fr)] gap-x-4 gap-y-1 text-sm">
     <dt className="text-muted">{t("Action")}</dt><dd className="break-words">{candidate.action}</dd>
     <dt className="text-muted">{t("Source evidence")}</dt><dd className="break-all">{candidate.evidence}</dd>
     <dt className="text-muted">{t("Policy ID")}</dt><dd>{candidate.policy??t("No training policy")} · {candidate.policyVersion??0}</dd>
    </dl>
    {!!candidate.problems?.length&&<ul className="list-disc space-y-1 pl-5 text-sm text-muted">{candidate.problems.map(problem=><li key={problem}>{t(problem)}</li>)}</ul>}
    <div className="grid gap-3 md:grid-cols-2">
     <div><h3 className="mb-1 text-sm font-medium">{t("Accepted fields")}</h3><pre className="max-h-64 overflow-auto whitespace-pre-wrap break-words rounded-md bg-background p-3 text-xs">{JSON.stringify(candidate.chosen??{},null,2)}</pre></div>
     <div><h3 className="mb-1 text-sm font-medium">{t("Rejected fields")}</h3><pre className="max-h-64 overflow-auto whitespace-pre-wrap break-words rounded-md bg-background p-3 text-xs">{JSON.stringify(candidate.rejected??{},null,2)}</pre></div>
    </div>
    <Disclosure summary={t("Source revisions")}><pre className="max-h-48 overflow-auto whitespace-pre-wrap break-words text-xs">{JSON.stringify(candidate.expectedSources??{},null,2)}</pre></Disclosure>
   </div>
   <Form className="grid gap-2" onSubmit={async()=>{if(await decide("ai.training-candidate.review",{type:"ai.training-candidate",id:selected},{approved:true,qualityEvidence:evidence})){setSelected("");void query.refetch()}}}>
    <label className="grid gap-1 text-sm">{t("Quality evidence")}<Textarea value={evidence} onChange={e=>setEvidence(e.target.value)} placeholder={t("Record the independent quality check and its result")}/></label>
    <div className="flex flex-wrap gap-2"><Button type="submit" disabled={!evidence.trim()}>{t("Approve training qualification")}</Button><Button variant="danger" onClick={async()=>{if(await decide("ai.training-candidate.revoke",{type:"ai.training-candidate",id:selected},{})){setSelected("");void query.refetch()}}}>{t("Revoke training use")}</Button><Button variant="ghost" onClick={()=>setSelected("")}>{t("Close review")}</Button></div>
   </Form>
  </Panel>}
  <Panel title={t("Training data policy")} description={t("Only future accepted evidence uses this policy. Choose allowed scalar fields, data-use permission, provider permission and retention explicitly.")}>
   <Form className="grid gap-2" onSubmit={async()=>{if(await decide("ai.training-policy.set",{type:"ai.training-policy",id},{enabled,fields:fields.split(",").map(x=>x.trim()).filter(Boolean),trainingAllowed:allowed,providerAllowed:provider,qualityRule:quality,retainDays:Number(days)})){void query.refetch()}}}>
    <Input aria-label={t("Policy ID")} value={id} onChange={e=>setID(e.target.value)} placeholder={t("Policy ID")}/>
    <Input aria-label={t("Allowed fields")} value={fields} onChange={e=>setFields(e.target.value)} placeholder={t("Comma-separated allowed fields")}/>
    <Input aria-label={t("Quality rule")} value={quality} onChange={e=>setQuality(e.target.value)} placeholder={t("Quality rule")}/>
    <Input aria-label={t("Retention days")} type="number" min={1} max={3650} value={days} onChange={e=>setDays(e.target.value)}/>
    <Checkbox checked={enabled} onChange={setEnabled}>{t("Enable this policy")}</Checkbox>
    <Checkbox checked={allowed} onChange={setAllowed}>{t("Data is permitted for training")}</Checkbox>
    <Checkbox checked={provider} onChange={setProvider}>{t("Provider terms permit training use")}</Checkbox>
    <Button type="submit" disabled={!id}>{t("Save training policy")}</Button>
   </Form>
   {(query.data?.policies??[]).map(p=><Button key={p.id} variant="link" onClick={()=>{setID(p.id);setFields(p.fields.join(","));setQuality(p.qualityRule??"");setDays(String(p.retainDays));setAllowed(p.trainingAllowed);setProvider(p.providerAllowed);setEnabled(p.enabled)}}>{p.id} · {p.revision}</Button>)}
  </Panel>
  <Panel title={t("Export qualified samples")} description={t("Export rechecks current field access, sources, data permission, quality and revocation. Revocation blocks future downloads; previously downloaded files cannot be recalled.")}>
   <p className="mb-3 text-sm text-muted">{t("Eligible samples")}: {qualified.length}</p>
   <div className="flex gap-2"><Select aria-label={t("Training format")} value={format} onChange={e=>setFormat(e.target.value)}><option value="sft">SFT</option><option value="dpo">DPO</option></Select><Button disabled={query.isPending||qualified.length===0} onClick={async()=>{
    setError("");const answer=await client.call<Api.AITrainingExport>("GET",`/v1/ai/training/export?format=${format}`);
    if(!answer.ok||!answer.body){setError(t("Training export failed."));return}
    for(const [name,body]of [[`${answer.body.id}.jsonl`,answer.body.jsonl],[`${answer.body.id}.manifest.json`,JSON.stringify(answer.body.manifest,null,2)]]){
     const url=URL.createObjectURL(new Blob([body!],{type:"application/json"}));const link=document.createElement("a");link.href=url;link.download=name!;link.click();URL.revokeObjectURL(url);
    }
   }}>{t("Download training dataset")}</Button></div>{error&&<p role="alert">{error}</p>}
  </Panel>
 </div>;
}
