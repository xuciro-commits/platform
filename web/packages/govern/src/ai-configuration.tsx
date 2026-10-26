import { useState } from "react";
import { modelOptionLabel, ModelOptionsEditor, ModelPicker, useHost, useReadQuery as useRead, type ModelPurpose } from "@platform/app";
import { Button, Dialog, Input, PageHeader, Panel, PropertyList, Select, Tag, useWorkspace, t } from "@platform/ui";
import type { Api } from "@platform/kernel";
import {modelReferenceTitle} from "./shared";
import { ReadResult } from "./operations";
import { AIProviders, AIUsage } from "./ai";

const purposes: { purpose: ModelPurpose; setting: string; title: string; description: string }[] = [
  { purpose: "generation", setting: "ai/app-model", title: "Application generation", description: "The default for functions that generate text or structured results. Each function may choose a different model." },
  { purpose: "agent", setting: "agent/model", title: "Agent tasks", description: "The default for new agent runs. Running tasks keep their accepted model and options." },
  { purpose: "embedding", setting: "knowledge/embedding-model", title: "Knowledge search", description: "Keyword search works without an AI service. A vector model adds semantic search and requires an index built with that model." },
];

export function AIConfiguration({initialSection="defaults",fromPackage,fromVersion}:{initialSection?:string;fromPackage?:string;fromVersion?:string}) {
  const [section, setSection] = useState(initialSection);
  const {open}=useWorkspace();
  const {can}=useHost();
  if(!can("ai.provider.add")&&!can("platform.setting.set"))return <PageHeader title={t("AI configuration")} description={t("Ask an administrator to connect services, enable models or change purpose defaults.")} actions={<Button onClick={()=>open({view:"ai-playground"})}>{t("Try available models")}</Button>} />;
  return <>
    <PageHeader title={t("AI configuration")} actions={fromPackage?<Button onClick={()=>open({view:"package-preparation",params:{id:fromPackage,version:fromVersion??""}})}>{t("Return to package preparation")}</Button>:undefined} help={t("Connect a service, read its models, enable the models people may use, then choose one default for each purpose. Several services and models can be enabled.")} />
    <nav aria-label={t("AI configuration")} className="mb-5 flex flex-wrap gap-2">
      {[["services", "Connect services"], ["models", "Available models"], ["defaults", "Default purposes"], ["usage", "Usage and limits"]].map(([id, title]) => <Button key={id} aria-current={section === id ? "page" : undefined} variant={section === id ? "primary" : undefined} onClick={() => setSection(id!)}>{t(title!)}</Button>)}
    </nav>
    {section === "services" && <AIProviders />}
    {section === "models" && <AvailableModels />}
    {section === "defaults" && <div className="grid max-w-4xl gap-6">{purposes.map(item => <PurposeDefault key={item.purpose} {...item} />)}</div>}
    {section === "usage" && <><Button className="mb-3" onClick={()=>open({view:"app-settings"})}>{t("Organization default limits")}</Button><AIUsage /></>}
  </>;
}

function PurposeDefault({ purpose, title, description, ...item }: typeof purposes[number]) {
  const query = useRead<Api.ModelSelection>(`/v1/ai/model-selection?purpose=${purpose}`);
  const { decide } = useHost();
  const [draft, setDraft] = useState<string>();
  const [options, setOptions] = useState<Api.ModelOptions>();
  const [saved, setSaved] = useState(false);
  const value = draft ?? query.data?.default ?? "";
  const selected = query.data?.models.find(row => `${row.model.provider}/${row.model.model}` === (value==="@generation"?query.data?.resolvedDefault:value));
  return <Panel title={t(title)} className="grid gap-3">
    <p className="max-w-prose text-sm text-muted">{t(description)}</p>
    {purpose==="embedding" ? <KnowledgeConfiguration /> : <ModelPicker value={value} onChange={next => { setDraft(next); setSaved(false); }} purpose={purpose} inherit={false} label={t(title)} />}
    {purpose !== "embedding" && <ModelOptionsEditor value={options ?? query.data?.options ?? {}} onChange={next => { setOptions(next); setSaved(false); }} capabilities={selected?.model.capabilities} />}
    <p className="text-xs text-muted">{t("Saving configuration does not test a generation request. Try the model explicitly before using it in a business task.")}</p>
    <div className="flex flex-wrap items-center gap-2">
      {purpose!=="embedding" && <Button variant="primary" disabled={!query.data || draft === undefined || value === query.data.default} onClick={async () => { if (await decide("platform.setting.set", { type: "platform.setting", id: item.setting }, { value },{quiet:true})) { setDraft(undefined); setSaved(true); } }}>{t("Save default")}</Button>}
      {purpose !== "embedding" && <Button disabled={!options} onClick={async () => { if (await decide("platform.setting.set", { type: "platform.setting", id: `${item.setting}-options` }, { value: JSON.stringify(options) },{quiet:true})) { setOptions(undefined); setSaved(true); } }}>{t("Save purpose options")}</Button>}
      {purpose!=="embedding"&&Object.keys(query.data?.options??{}).length>0&&<Button onClick={async()=>{if(await decide("platform.setting.set",{type:"platform.setting",id:`${item.setting}-options`},{value:""},{quiet:true})){setOptions(undefined);setSaved(true)}}}>{t("Reset purpose options")}</Button>}
      {saved && <span role="status" className="text-xs text-muted">{t("Configuration saved")}</span>}
    </div>
    {selected?.effective && purpose !== "embedding" && <PropertyList items={[[t("Effective generation budget"), selected.effective.options.maxTokens || t("Provider default")], [t("Effective thinking mode"), selected.effective.options.thinking ? modelOptionLabel(selected.effective.options.thinking) : t("Provider default")], [t("Effective thinking level"), selected.effective.options.effort ? modelOptionLabel(selected.effective.options.effort) : t("Provider default")]]} />}
  </Panel>;
}

function KnowledgeConfiguration() {
  const query=useRead<Api.KnowledgeIndexStatus>("/v1/ai/knowledge-index");
  const {decide}=useHost();
  const [model,setModel]=useState("");
  const [confirm,setConfirm]=useState(false);
  const status=query.data;
  return <div className="grid gap-3">
    <p className="text-sm">{t("Current search")}: {status?.active&&!status.needsRebuild?`${t("Keywords and vectors")} · ${status.active}`:t("Keywords only")}</p>
    <ModelPicker value={model} onChange={setModel} purpose="embedding" inherit={false} label={t("Prepare vector model")} />
    <div className="flex flex-wrap gap-2"><Button disabled={!model} onClick={()=>setConfirm(true)}>{t("Build this model's index")}</Button>{status?.active && <Button onClick={()=>void decide("platform.setting.set",{type:"platform.setting",id:"knowledge/embedding-model"},{value:""})}>{t("Use keyword search only")}</Button>}</div>
    {status?.needsRebuild&&<p role="alert" className="text-sm text-[var(--tone-danger)]">{status.problem}</p>}
    {query.error && <p role="alert">{t("Index status could not be loaded.")} <Button size="sm" onClick={()=>void query.refetch()}>{t("Retry")}</Button></p>}
    {status?.preparing && <div className="grid gap-2"><p role="status" className="break-words text-sm">{status.preparing} · {t("Index progress")}: {status.completed}/{status.total}{status.dimensions?` · ${status.dimensions} ${t("dimensions")}`:""}</p>{status.problem && <p role="alert" className="text-sm text-[var(--tone-danger)]">{status.problem}</p>}<div className="flex flex-wrap gap-2"><Button variant="primary" disabled={!status.ready} onClick={()=>void decide("platform.setting.set",{type:"platform.setting",id:"knowledge/embedding-model"},{value:status.preparing})}>{t("Activate prepared index")}</Button><Button onClick={()=>void decide("platform.setting.set",{type:"platform.setting",id:"knowledge/embedding-next-model"},{value:""})}>{t("Stop preparing")}</Button></div></div>}
    <Dialog open={confirm} onOpenChange={setConfirm} title={t("Prepare vector index")}><p className="text-sm">{t("The knowledge passages will be sent to this model to build an index. Provider charges may apply. Current search stays available until you activate the prepared index.")}</p><p className="my-3 break-words text-sm">{model}</p><div className="flex justify-end gap-2"><Button onClick={()=>setConfirm(false)}>{t("Cancel")}</Button><Button variant="primary" onClick={async()=>{if(await decide("platform.setting.set",{type:"platform.setting",id:"knowledge/embedding-next-model"},{value:model}))setConfirm(false)}}>{t("Confirm and build index")}</Button></div></Dialog>
  </div>;
}

function AvailableModels() {
  const query = useRead<Api.ModelSelection>("/v1/ai/model-selection?purpose=generation");
  const { decide,client } = useHost();
  const {open:go}=useWorkspace();
  const [editing, setEditing] = useState<Api.Model>();
  const [configurationSaved,setConfigurationSaved]=useState(false);
  const [removing, setRemoving] = useState<Api.ModelChoice>();
  const [options, setOptions] = useState<Api.ModelOptions>({});
  const [facts,setFacts]=useState<Api.ModelCapabilities>();
  const [factsError,setFactsError]=useState("");
  const [daily, setDaily] = useState(0);
  const [access, setAccess] = useState("users");
  const save = async () => {
    if (!editing) return;
    if (await decide("ai.model.enable", { type: "ai.model", id: `${editing.provider}/${editing.model}` }, { access, dailyTokens: daily, defaults: options, capabilities:facts, expectedRevision: editing.revision ?? 0 },{quiet:true})) {setEditing(undefined);setConfigurationSaved(true);}
  };
  return <>
    {configurationSaved&&<p role="status" className="mb-3 text-sm">{t("Configuration saved")}</p>}
    <ReadResult query={query}>{selection => <div className="grid gap-4">
      {selection.models.length === 0 && <p>{t("No models are enabled. Open Connect services, choose a provider and enable a model.")}</p>}
      {selection.models.map(choice => {
        const model = choice.model;
        return <div key={`${model.provider}/${model.model}`} className="grid gap-2 border-b border-border pb-4 sm:grid-cols-[minmax(0,1fr)_auto]">
          <div className="min-w-0"><p className="break-words font-medium">{model.model}</p><p className="break-words text-sm text-muted">{model.provider} · {t(model.access === "everyone" ? "Everyone in this tenant" : "AI members only")}</p><p className="mt-1 text-xs text-muted">{t("Used by")}: {choice.references?.map(modelReferenceTitle).join(", ") || t("No purpose default")}</p></div>
          <div className="flex flex-wrap items-start gap-2"><Button onClick={() => { setEditing(model); setFacts(undefined);setFactsError(""); setOptions(model.defaults ?? {}); setDaily(model.dailyTokens ?? 0); setAccess(model.access); }}>{t("Details and defaults")}</Button><Button onClick={() => go({ view: "ai-playground", params: { model: `${model.provider}/${model.model}` } })}>{t("Try model")}</Button><Button variant="danger" onClick={() => setRemoving(choice)}>{t("Disable")}</Button></div>
        </div>;
      })}
    </div>}</ReadResult>
    <Dialog open={!!editing} onOpenChange={open => { if (!open) setEditing(undefined); }} title={editing?.model ?? t("Model details")}>
      {editing && <div className="grid gap-4 text-sm">
        <ModelFacts capabilities={facts??editing.capabilities} />
        <Button onClick={async()=>{const result=await client.call<Api.CatalogModel[] & {error?:{detail?:string}}>("GET",`/v1/ai/providers/${encodeURIComponent(editing.provider)}/models?refresh=true`);const found=result.ok?result.body.find(model=>model.id===editing.model):undefined;if(found?.capabilities){setFacts(found.capabilities);setFactsError("")}else{setFactsError(result.body.error?.detail??t("This model was not returned by the provider. Current configuration is preserved."))}}}>{t("Read latest model facts")}</Button>
        {facts&&<p role="status" className="text-xs text-muted">{t("New facts are shown for review. Save configuration to adopt them.")}</p>}
        {factsError&&<p role="alert" className="text-sm text-[var(--tone-danger)]">{factsError}</p>}
        <label className="grid gap-1">{t("Who can call this model")}<Select value={access} onChange={event => setAccess(event.target.value)}><option value="users">{t("AI members only")}</option><option value="everyone">{t("Everyone in this tenant")}</option></Select></label>
        <label className="grid gap-1">{t("Model daily token limit")}<Input type="number" min={0} value={daily} onChange={event => setDaily(Number(event.target.value))} /></label>
        <p className="text-xs text-muted">{t("Zero means no model-wide daily cap. Individual and application limits still apply.")}</p>
        <ModelOptionsEditor value={options} onChange={setOptions} capabilities={facts??editing.capabilities} />
        <p className="text-xs text-muted">{t("Defaults affect new tasks. Existing evaluations must match the effective configuration before publication.")}</p>
        <div className="flex justify-end gap-2"><Button onClick={() => setEditing(undefined)}>{t("Cancel")}</Button><Button variant="primary" onClick={() => void save()}>{t("Save configuration")}</Button></div>
      </div>}
    </Dialog>
    <Dialog open={!!removing} onOpenChange={open => { if (!open) setRemoving(undefined); }} title={t("Disable model")}>
      <p className="text-sm">{t("Tasks using this model will stop making model calls. References are preserved so you can enable it again or choose a replacement.")}</p>
      <p className="my-3 break-words text-sm">{t("Purpose references")}: {removing?.references?.map(modelReferenceTitle).join(", ") || t("None")}</p>
      <p className="text-xs text-muted">{t("Resource overrides and running tasks may also use this model.")}</p>
      <div className="mt-4 flex justify-end gap-2"><Button onClick={() => setRemoving(undefined)}>{t("Cancel")}</Button><Button variant="danger" onClick={async () => { if (removing && await decide("ai.model.disable", { type: "ai.model", id: `${removing.model.provider}/${removing.model.model}` }, {})) setRemoving(undefined); }}>{t("Disable model")}</Button></div>
    </Dialog>
  </>;
}

function ModelFacts({ capabilities: facts }: { capabilities?: Api.ModelCapabilities }) {
  const unknown = t("Not reported by provider");
  return <div className="grid gap-2">
    <Tag label={t("Provider facts · read only")} />
    <PropertyList items={[[t("Vendor thinking modes"), facts?.declaredThinkingModes?.map(modelOptionLabel).join(", ")||unknown], [t("Configurable thinking modes"), facts?.thinkingModes?.map(modelOptionLabel).join(", ")||t("Not verified")], [t("Model tool calling"), facts?.tools===undefined?unknown:t(facts.tools?"Supported":"Not supported")], [t("Input limit (tokens)"), facts?.inputTokens ?? unknown], [t("Generation limit (tokens)"), facts?.outputTokens ?? unknown], [t("Shared context window (tokens)"), facts?.contextTokens ?? unknown], [t("Native structured output"), facts?.structured === undefined ? unknown : t(facts.structured ? "Supported" : "Not supported")], [t("Capability source"), facts?.source ?? unknown], [t("Read at"), facts?.observedAt ?? unknown]]} />
    <p className="text-xs text-muted">{t("Platform functions validate structured results even when native schema support is unknown. Thinking controls appear only for implemented adapter options.")}</p>
  </div>;
}
