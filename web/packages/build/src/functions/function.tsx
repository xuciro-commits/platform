import { ContextHelp, StructuredEditor } from "@platform/ui";
import { useDefinitionDraft } from "../session/definition-draft";
import { useApplicationWorkspace } from "../projects/application-scope";
import { ResourceList } from "../editor/ResourceList";
import { DraftStatus, ResourceControls, WorkbenchMessage, savingState } from "../editor/workbench";
import { CandidateTest } from "../releases/simulate";
// This editor writes build.function's native declaration. The three stages
// visualize that declaration; execution belongs to the host model effect path.
import { useHost, ModelPicker, ModelOptionsEditor, useRecordInventory, useReadQuery } from "@platform/app";
import { type Api } from "@platform/kernel";
import { Button, Card, Checkbox, Input, FlowCanvas, PageHeader, Panel, Select, Textarea, Workbench, t,
  type FlowNode, type FlowCatalog } from "@platform/ui";
import { useState } from "react";
import { type WorkflowObject } from "../automate/workflow-model";

type FunctionDraft = { proposal?:Api.ProposalPolicy; conversation?:boolean; id: string; revision: number; name: string; title: string; description: string; object: string; fields: string[];
  instructions: string; output: Api.Field[]; model?: string; modelOptions?:Api.ModelOptions; maxInputBytes: number; maxOutputBytes: number; maxTokens: number;
  roles: string[]; version?: number; published?: string };
type Source = WorkflowObject & { archived?: boolean; fields: { name: string; title: string; type: string; aiInput?:boolean;read?: string[] }[] };
type Stage = "settings" | "source" | "model" | "output";
const empty = (): FunctionDraft => ({ id: "", revision: 0, name: "", title: "", description: "", object: "", fields: [], instructions: "",
  output: [{ name: "summary", type: "string", required: true, description: "" }], maxInputBytes: 4096, maxOutputBytes: 1024, maxTokens: 256, roles: ["builder", "user"] });
const loaded = (record: FunctionDraft): FunctionDraft => ({ ...record, fields: record.fields ?? [], output: record.output ?? [], roles: record.roles ?? [] });
const scalarTypes = ["text", "longtext", "integer", "decimal", "boolean", "choice"];
const toggle = (values: string[], name: string, enabled: boolean) => enabled ? [...new Set([...values, name])] : values.filter((value) => value !== name);

export function Functions() {
  const { source , holds} = useHost();
  const { open } = useApplicationWorkspace();
  if (!holds("build", "builder")) return <PageHeader title={t("AI functions")} description={t("Only a builder can edit AI functions.")} />;
  return <div className="grid gap-3">
    <PageHeader title={t("AI functions")} help={t("Turn readable record fields into typed suggestions for human review.")}
      actions={<Button onClick={() => open({ view: "function", params: { id: "new" } })}>{t("New AI function")}</Button>} />
    <ResourceList source={source} type="build.function" fields={["title", "name", "object", "version"]} onOpen={(record) => open({ view: "function", params: { id: record.id } })} />
  </div>;
}

export function FunctionEditor({ id }: { id: string }) {
  const { holds} = useHost();
  const { open } = useApplicationWorkspace();
  const inventory = useRecordInventory<Source>("build.object");
  const objects = (inventory.data?.records ?? []).filter(object => !object.archived);
  const [chosen, setChosen] = useState<Stage>("settings"), [testing, setTesting] = useState(false);
  const editor = useDefinitionDraft<FunctionDraft>({
    id, type: "build.function", kind: "function", view: "function", empty, normalize: loaded,
    failure: t("The function could not be saved or loaded. Your draft is still here."), onDiscard: () => setChosen("settings"),
    payload: ({proposal,conversation,name,title,description,object,fields,instructions,output,model,modelOptions,maxInputBytes,maxOutputBytes,maxTokens,roles}) =>
      ({proposal:proposal??null,conversation:conversation??false,name,title,description,object,fields,instructions,output,model:model??"",modelOptions:modelOptions??{},maxInputBytes,maxOutputBytes,maxTokens,roles}),
  });
  const {draft,query,dirty,busy,error,setError,patch:change,perform,discardChanges,confirmDiscard}=editor;
  const modelSelection=useReadQuery<Api.ModelSelection>("/v1/ai/model-selection?purpose=generation").data;
  const modelChoice=modelSelection?.models.find(row=>`${row.model.provider}/${row.model.model}`===(draft.model||modelSelection.default));
  const source = objects.find((object) => `build.${object.name}` === draft.object);
  const fields = source?.fields?.filter((field) => scalarTypes.includes(field.type)&&field.aiInput!==false) ?? [];
  const roles = [...new Set(["builder", ...(source?.access?.length ? source.access.filter((access) => access.read !== "none").map((access) => access.role) : ["user"])])];
  const updateOutput = (index: number, patch: Partial<Api.Field>) => change({ output: (draft.output ?? []).map((field, i) => i === index ? { ...field, ...patch } : field) });
  // Hints never replace the owner's authoritative publication validation.
  const issues: string[] = [];
  if (!/^[a-z][a-z0-9]*$/.test(draft.name) || !(draft.title ?? "").trim() || !(draft.description ?? "").trim()) issues.push(t("Give the function a lower-case name, title and description."));
  if (!source || !(draft.fields ?? []).length || (draft.fields ?? []).some((name) => !fields.some((field) => field.name === name))) issues.push(t("Choose direct scalar fields from a saved object."));
  if (!(draft.roles ?? []).length || (draft.roles ?? []).some((role) => !roles.includes(role))) issues.push(t("Choose at least one callable role of the source object."));
  if (!(draft.instructions ?? "").trim()) issues.push(t("Write instructions for the model stage."));
  if (!(draft.output ?? []).length || (draft.output ?? []).some((field) => !/^[a-zA-Z][a-zA-Z0-9_]*$/.test(field.name) || !field.description?.trim()) || new Set((draft.output ?? []).map((field) => field.name)).size !== (draft.output ?? []).length)
    issues.push(t("Give every output a unique field name, scalar type and description."));
  const save = async (deferNavigation = false) => {
    const saved = await editor.save(deferNavigation);
    if (saved?.reloaded === false) { setError(t("Reload the saved function before editing again.")); return; }
    return saved;
  };
  const publish = async () => {
    if (issues.length) { setError(issues.join(" ")); return; }
    const saved = dirty || id === "new" ? await save() : { id: draft.id, revision: draft.revision };
    if (saved) await editor.transition("publish", saved);
  };
  const review = async () => {
    if (issues.length) { setError(issues.join(" ")); return; }
    const saved = dirty || id === "new" ? await save(true) : { id: draft.id, revision: draft.revision };
    if (saved) open({ view: "release-review", params: { kind: "function", id: saved.id } }, { ifCurrent: { view: "function", params: { id } }, replace: id === "new" });
  };
  const reload = () => editor.reload(t("The function could not be saved or loaded. Your draft is still here."));
  if (!holds("build", "builder")) return <Workbench storageKey="function" title={t("AI function")}><WorkbenchMessage>{t("Only a builder can edit AI functions.")}</WorkbenchMessage></Workbench>;
  if (id !== "new" && !draft.id) return <Workbench storageKey="function" title={t("AI function")}><WorkbenchMessage>{query.isError ? t("The function could not be loaded.") : t("Loading…")}</WorkbenchMessage></Workbench>;
  const stages: { id: Stage; title: string }[] = [{ id: "settings", title: t("Function settings") }, { id: "source", title: t("Record inputs") }, { id: "model", title: t("Model inference") }, { id: "output", title: t("Strict output") }];
  return <Workbench storageKey="function" crumbs={[{ label: t("Functions"), onClick: () => open({ view: "function" }) }, { label: t("AI functions"), onClick: () => open({ view: "function" }) }]} title={draft.title || t("New AI function")}
    status={<DraftStatus state={draft.version ? "published" : "draft"} problems={dirty ? issues.length : 0} />} saving={savingState(dirty, busy, error || undefined)}
    actions={<>
        <Button size="sm" disabled={busy || !draft.id} variant="ghost" onClick={() => confirmDiscard(() => void perform(reload))}>{t("Reload saved function")}</Button>
        <Button size="sm" disabled={busy || (!dirty && id !== "new")} onClick={() => void perform(save)}>{t("Save function")}</Button>
        <Button size="sm" variant={testing ? "primary" : "ghost"} aria-pressed={testing} disabled={busy || !draft.id || dirty} onClick={() => setTesting(!testing)}>{t("Test")}</Button>
        <ResourceControls type="build.function" record={draft} dirty={dirty} busy={busy} invalid={issues.length > 0 || !draft.id} onReview={() => void perform(review)} onInstall={() => void perform(publish)} onCancel={discardChanges} route={{ view: "function", params: { id } }} />
    </>}><div className="grid min-h-0 min-w-0 flex-1 content-start gap-3 overflow-auto p-3">
    <ContextHelp title={t("AI function")}><p className="text-xs text-muted">{t("Save the declaration, test fixed cases, then review its release candidate.")}</p></ContextHelp>
    {testing && draft.id && !dirty && <Card className="p-3"><CandidateTest functionId={draft.id} embedded /></Card>}
    {draft.version ? <Panel role="status" className="text-xs">{t("Installed function version {version}. Accepted calls keep their saved inputs and definition.", { version: draft.version })}</Panel> : null}
    {error && <Panel role="alert" className="text-sm text-danger">{error}</Panel>}
    {dirty && issues.length > 0 && <Panel aria-live="polite" className="text-xs text-muted">{issues.join(" ")}</Panel>}
    {inventory.isError && <Panel role="alert">{t("The function sources could not be loaded.")}</Panel>}
    <StructuredEditor {...editor.structured}><fieldset disabled={busy} className="grid min-w-0 gap-3 xl:grid-cols-[12rem_minmax(0,1fr)_22rem]">
      <Panel role="region" aria-label={t("Function stages")} className="grid content-start gap-2 p-3">
        {stages.map((stage) => <Button key={stage.id} aria-pressed={chosen === stage.id} variant={chosen === stage.id ? "primary" : "ghost"} onClick={() => setChosen(stage.id)}>{stage.title}</Button>)}
        <ContextHelp title={t("Function stages")}><p className="text-xs text-muted">{t("Select a stage or use these buttons to edit its properties. Source permissions are checked on every call.")}</p></ContextHelp>
      </Panel>
      <FunctionMap draft={draft} source={source?.title} chosen={chosen} onChoose={setChosen} />
      <Panel role="region" aria-label={t("Function properties")} className="grid min-w-0 content-start gap-3 p-3">
        {chosen === "settings" && <>
 <Checkbox checked={!!draft.proposal} onChange={enabled=>change({proposal:enabled?{expiresAfterSeconds:3600}:undefined,conversation:false})}>{t("Produce a business proposal")}</Checkbox>
 {draft.proposal&&<fieldset className="grid gap-2 rounded border border-border p-2"><legend className="text-xs">{t("Proposal review policy")}</legend>
 <label className="grid gap-1 text-xs">{t("Proposal expiry (seconds)")}<Input type="number" min={1} max={604800} value={draft.proposal.expiresAfterSeconds} onChange={event=>change({proposal:{...draft.proposal!,expiresAfterSeconds:Number(event.target.value)}})}/></label>
 <label className="grid gap-1 text-xs">{t("Handover role")}<Select value={draft.proposal.handoffRole??""} onChange={event=>change({proposal:{...draft.proposal!,handoffRole:event.target.value||undefined,confidenceField:undefined,minimumConfidence:0}})}><option value="">{t("Original adoption action roles")}</option>{roles.map(role=><option key={role} value={role}>{role}</option>)}</Select></label>
 {draft.proposal.handoffRole&&<><label className="grid gap-1 text-xs">{t("Confidence output")}<Select value={draft.proposal.confidenceField??""} onChange={event=>change({proposal:{...draft.proposal!,confidenceField:event.target.value||undefined,minimumConfidence:event.target.value?0.5:0}})}><option value="">{t("Always hand over")}</option>{(draft.output ?? []).filter(field=>field.required&&["decimal","integer"].includes(field.type)).map(field=><option key={field.name} value={field.name}>{field.name}</option>)}</Select></label>
 {!!draft.proposal.confidenceField&&<label className="grid gap-1 text-xs">{t("Minimum confidence")}<Input type="number" min={0} max={1} step={0.05} value={draft.proposal.minimumConfidence??0} onChange={event=>change({proposal:{...draft.proposal!,minimumConfidence:Number(event.target.value)}})}/></label>}</>}
 </fieldset>}

          <label className="grid gap-1 text-xs">{t("Function name")}<Input disabled={!!draft.published} value={draft.name} onChange={(e) => change({ name: e.target.value })} /></label>
          <label className="grid gap-1 text-xs">{t("Function title")}<Input value={draft.title} onChange={(e) => change({ title: e.target.value })} /></label>
          <label className="grid gap-1 text-xs">{t("Function description")}<Textarea rows={3} value={draft.description} onChange={(e) => change({ description: e.target.value })} /></label>
          <fieldset className="grid gap-2"><legend className="mb-2 text-xs">{t("Callable by")}</legend>
            {[...new Set([...roles, ...(draft.roles ?? [])])].map((role) => <Checkbox key={role} checked={(draft.roles ?? []).includes(role)} onChange={(enabled) => change({ roles: toggle((draft.roles ?? []), role, enabled) })}>{role}</Checkbox>)}
          </fieldset>
        </>}
        {chosen === "source" && <>
          <label className="grid gap-1 text-xs">{t("Source object")}<Select disabled={!!draft.published} value={draft.object} onChange={(e) => change({ object: e.target.value, fields: [] })}>
            <option value="">{t("Choose an object")}</option>{objects.map((object) => <option key={object.id} value={`build.${object.name}`}>{object.title}</option>)}
          </Select></label>
          <fieldset className="grid gap-2"><legend className="mb-2 text-xs">{t("Input fields")}</legend>
            {fields.map((field) => <Checkbox key={field.name} checked={(draft.fields ?? []).includes(field.name)} onChange={(enabled) => change({ fields: toggle((draft.fields ?? []), field.name, enabled) })}>{field.title} · {field.type}</Checkbox>)}
          </fieldset>
          <label className="grid gap-1 text-xs">{t("Maximum input bytes")}<Input draftKey="maximum-input-bytes" type="number" min={1} max={32768} value={draft.maxInputBytes} onChange={(e) => change({ maxInputBytes: Number(e.target.value) })} /></label>
        </>}
        {chosen === "model" && <>
          <ModelPicker value={draft.model??""} onChange={model=>change({model})} purpose="generation" />
          <ModelOptionsEditor generationLimit={4096} value={{...draft.modelOptions,maxTokens:draft.maxTokens}} onChange={({maxTokens,...modelOptions})=>change({modelOptions,maxTokens:maxTokens??draft.maxTokens})} capabilities={modelChoice?.model.capabilities} />
          <Checkbox disabled={!!draft.proposal} checked={draft.conversation??false} onChange={conversation=>change({conversation})}>{t("Accept conversation questions and retained call history")}</Checkbox>
          {draft.conversation&&<ContextHelp title={t("Conversation input")}><p className="text-xs text-muted">{t("Conversation input contains the authorized record, current question and up to eight completed calls. The full input still uses the original byte and token budgets.")}</p></ContextHelp>}
          <label className="grid gap-1 text-xs">{t("Model instructions")}<Textarea rows={8} value={draft.instructions} onChange={(e) => change({ instructions: e.target.value })} /></label>

        </>}
        {chosen === "output" && <>
          <label className="grid gap-1 text-xs">{t("Maximum output bytes")}<Input draftKey="maximum-output-bytes" type="number" min={1} max={32768} value={draft.maxOutputBytes} onChange={(e) => change({ maxOutputBytes: Number(e.target.value) })} /></label>
          {(draft.output ?? []).map((field, index) => <Card key={index} role="group" aria-label={t("Output field {n}", { n: index + 1 })} className="grid gap-2 p-2">
            <label className="grid gap-1 text-xs">{t("Output field name")}<Input value={field.name} onChange={(e) => updateOutput(index, { name: e.target.value })} /></label>
            <label className="grid gap-1 text-xs">{t("Output type")}<Select value={field.type} onChange={(e) => updateOutput(index, { type: e.target.value, choices: undefined })}>
              <option value="string">{t("Text")}</option><option value="boolean">{t("Boolean")}</option><option value="integer">{t("Integer")}</option><option value="decimal">{t("Decimal")}</option>
            </Select></label>
            <label className="grid gap-1 text-xs">{t("Output description")}<Textarea rows={2} value={field.description ?? ""} onChange={(e) => updateOutput(index, { description: e.target.value })} /></label>
            <Checkbox checked={!!field.required} onChange={(required) => updateOutput(index, { required })}>{t("Required")}</Checkbox>
            {field.type === "string" && <label className="grid gap-1 text-xs">{t("Allowed text values (one per line, optional)")}<Textarea rows={2} value={field.choices?.join("\n") ?? ""} onChange={(e) => updateOutput(index, { choices: e.target.value ? e.target.value.split("\n") : undefined })} /></label>}
            <Button disabled={(draft.output ?? []).length === 1} onClick={() => change({ output: (draft.output ?? []).filter((_, i) => i !== index) })}>{t("Remove output")}</Button>
          </Card>)}
          <Button disabled={(draft.output ?? []).length >= 16} onClick={() => change({ output: [...(draft.output ?? []), { name: "", type: "string", required: true, description: "" }] })}>{t("Add output field")}</Button>
        </>}
      </Panel>
    </fieldset></StructuredEditor>
  </div></Workbench>;
}

function FunctionMap({ draft, source, chosen, onChoose }: { draft: FunctionDraft; source?: string; chosen: Stage; onChoose: (stage: Stage) => void }) {
  const catalog: FlowCatalog = [
    { id: "source", title: t("Record inputs"), class: "trigger", group: t("Function"), inputs: [], outputs: [{ id: "record", label: t("Authorised fields"), type: "record" }] },
    { id: "model", title: t("Model inference"), class: "function", group: t("Function"), inputs: [{ id: "record", label: t("Record inputs"), type: "record" }], outputs: [{ id: "answer", label: t("Typed answer"), type: "answer" }] },
    { id: "output", title: t("Strict output"), class: "document", group: t("Function"), inputs: [{ id: "answer", label: t("Typed answer"), type: "answer" }], outputs: [] },
  ];
  const nodes: FlowNode[] = [
    { id: "source", kind: "source", label: source || t("Choose an object"), detail: (draft.fields ?? []).join(", ") || t("Choose input fields"), position: { x: 0, y: 0 } },
    { id: "model", kind: "model", label: draft.model || t("Application default model"), detail: t("Up to {n} output tokens", { n: draft.maxTokens }), position: { x: 0, y: 150 } },
    { id: "output", kind: "output", label: t("Strict JSON object"), detail: (draft.output ?? []).map((field) => `${field.name}: ${field.type}`).join(", "), position: { x: 0, y: 300 } },
  ];
  return <Card className="min-w-0 p-3">
    <FlowCanvas label={t("Function map")} catalog={catalog} nodes={nodes} selected={chosen} onSelect={(id) => onChoose(id as Stage)} height={460}
      storeKey={`function-map:${draft.id || "new"}`}
      edges={[{ id: "input", source: "source", sourcePort: "record", target: "model", targetPort: "record" }, { id: "answer", source: "model", sourcePort: "answer", target: "output", targetPort: "answer" }]} />
    <ContextHelp title={t("Strict output")}><p className="mt-2 text-xs text-muted">{t("The function returns a suggestion. Human review and business actions remain separate.")}</p></ContextHelp>
  </Card>;
}
