// This editor writes build.function's native declaration. The three stages
// visualize that declaration; execution belongs to the host model effect path.
import { useHost, useReadQuery, useRecordInventory } from "@platform/app";
import { type Api } from "@platform/kernel";
import { Button, Card, Checkbox, Input, NodeCanvas, PageHeader, Panel, RecordList, Select, Textarea, t, useWorkspace,
  type CanvasNode, type NodeCatalog } from "@platform/ui";
import { useEffect, useState } from "react";
import { installedObjects, type WorkflowObject } from "./workflow-model";

type FunctionDraft = { id: string; revision: number; name: string; title: string; description: string; object: string; fields: string[];
  instructions: string; output: Api.Field[]; model?: string; maxInputBytes: number; maxOutputBytes: number; maxTokens: number;
  roles: string[]; version?: number; published?: string };
type Source = WorkflowObject & { fields: { name: string; title: string; type: string; read?: string[] }[] };
type Stage = "settings" | "source" | "model" | "output";
const empty = (): FunctionDraft => ({ id: "", revision: 0, name: "", title: "", description: "", object: "", fields: [], instructions: "",
  output: [{ name: "summary", type: "string", required: true, description: "" }], maxInputBytes: 4096, maxOutputBytes: 1024, maxTokens: 256, roles: ["builder", "user"] });
const loaded = (record: FunctionDraft): FunctionDraft => ({ ...record, fields: record.fields ?? [], output: record.output ?? [], roles: record.roles ?? [] });
const scalarTypes = ["text", "longtext", "integer", "decimal", "boolean", "choice"];
const toggle = (values: string[], name: string, enabled: boolean) => enabled ? [...new Set([...values, name])] : values.filter((value) => value !== name);

export function Functions() {
  const { source, role } = useHost();
  const { open } = useWorkspace();
  if (role("build") !== "builder") return <PageHeader title={t("AI functions")} description={t("Only a builder can edit AI functions.")} />;
  return <div className="grid gap-3">
    <PageHeader title={t("AI functions")} description={t("Turn readable record fields into typed suggestions for human review.")}
      actions={<Button onClick={() => open({ view: "function", params: { id: "new" } })}>{t("New AI function")}</Button>} />
    <RecordList source={source} type="build.function" fields={["title", "name", "object", "version"]} onOpen={(record) => open({ view: "function", params: { id: record.id } })} />
  </div>;
}

export function FunctionEditor({ id }: { id: string }) {
  const { decide, role } = useHost();
  const { open } = useWorkspace();
  const query = useReadQuery<{ record?: FunctionDraft }>(`/v1/records/build.function/${encodeURIComponent(id)}`);
  const inventory = useRecordInventory<Source>("build.object");
  const objects = installedObjects(inventory.data?.records ?? []);
  const [draft, setDraft] = useState<FunctionDraft>(empty);
  const [chosen, setChosen] = useState<Stage>("settings");
  const [dirty, setDirty] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => { if (query.data?.record && !dirty) setDraft(loaded(query.data.record)); }, [query.data, dirty]);
  const source = objects.find((object) => `build.${object.name}` === draft.object);
  const fields = source?.fields?.filter((field) => scalarTypes.includes(field.type)) ?? [];
  const roles = [...new Set(["builder", ...(source?.access?.length ? source.access.filter((access) => access.read !== "none").map((access) => access.role) : ["user"])])];
  const change = (patch: Partial<FunctionDraft>) => { setDraft((old) => ({ ...old, ...patch })); setDirty(true); setError(""); };
  const updateOutput = (index: number, patch: Partial<Api.Field>) => change({ output: draft.output.map((field, i) => i === index ? { ...field, ...patch } : field) });
  // Hints never replace the owner's authoritative publication validation.
  const issues: string[] = [];
  if (!/^[a-z][a-z0-9]*$/.test(draft.name) || !draft.title.trim() || !draft.description.trim()) issues.push(t("Give the function a lower-case name, title and description."));
  if (!source || !draft.fields.length || draft.fields.some((name) => !fields.some((field) => field.name === name))) issues.push(t("Choose direct scalar fields from a published object."));
  if (!draft.roles.length || draft.roles.some((role) => !roles.includes(role))) issues.push(t("Choose at least one callable role of the source object."));
  if (!draft.instructions.trim()) issues.push(t("Write instructions for the model stage."));
  if (!draft.output.length || draft.output.some((field) => !/^[a-zA-Z][a-zA-Z0-9_]*$/.test(field.name) || !field.description?.trim()) || new Set(draft.output.map((field) => field.name)).size !== draft.output.length)
    issues.push(t("Give every output a unique field name, scalar type and description."));
  const perform = async (action: () => Promise<unknown>) => {
    setBusy(true); setError("");
    try { await action(); } catch { setError(t("The function could not be saved or loaded. Your draft is still here.")); }
    finally { setBusy(false); }
  };
  const save = async (): Promise<number | undefined> => {
    const target = draft.id || crypto.randomUUID();
    const { name, title, description, object, fields, instructions, output, model, maxInputBytes, maxOutputBytes, maxTokens, roles } = draft;
    if (!await decide(`build.function.${draft.id ? "edit" : "create"}`, { type: "build.function", id: target },
      { name, title, description, object, fields, instructions, output, model: model ?? "", maxInputBytes, maxOutputBytes, maxTokens, roles },
      { expectedRevision: draft.id ? draft.revision : undefined, quiet: true, onRefused: setError })) return;
    const revision = draft.id ? draft.revision + 1 : 1;
    if (!draft.id) open({ view: "function", params: { id: target } });
    else {
      const refreshed = await query.refetch();
      if (!refreshed.data?.record || refreshed.isError) { setError(t("Reload the saved function before editing again.")); return; }
      setDraft(loaded(refreshed.data.record)); setDirty(false);
    }
    return revision;
  };
  const publish = async () => {
    if (issues.length) { setError(issues.join(" ")); return; }
    const revision = dirty ? await save() : draft.revision;
    if (revision === undefined) return;
    if (await decide("build.function.publish", { type: "build.function", id: draft.id }, {}, { expectedRevision: revision, quiet: true, onRefused: setError })) {
      const refreshed = await query.refetch();
      if (refreshed.data?.record && !refreshed.isError) { setDraft(loaded(refreshed.data.record)); setDirty(false); }
      else setError(t("Reload the saved function before editing again."));
    }
  };
  const reload = async () => {
    const refreshed = await query.refetch();
    if (refreshed.data?.record && !refreshed.isError) { setDraft(loaded(refreshed.data.record)); setDirty(false); }
    else setError(t("The function could not be saved or loaded. Your draft is still here."));
  };
  if (role("build") !== "builder") return <PageHeader title={t("AI functions")} description={t("Only a builder can edit AI functions.")} />;
  if (id !== "new" && !draft.id) return <PageHeader title={t("AI functions")} description={query.isError ? t("The function could not be loaded.") : t("Loading…")} />;
  const stages: { id: Stage; title: string }[] = [{ id: "settings", title: t("Function settings") }, { id: "source", title: t("Record inputs") }, { id: "model", title: t("Model inference") }, { id: "output", title: t("Strict output") }];
  return <div className="grid min-w-0 gap-3">
    <PageHeader title={draft.title || t("New AI function")} description={t("Save the declaration, test fixed cases, then publish its next function version.")}
      actions={<div className="flex flex-wrap gap-2">
        <Button onClick={() => open({ view: "function" })}>{t("AI functions")}</Button>
        <Button disabled={busy || !draft.id} onClick={() => void perform(reload)}>{t("Reload saved function")}</Button>
        <Button disabled={busy || (!dirty && !!draft.id)} onClick={() => void perform(save)}>{t("Save function")}</Button>
        <Button disabled={busy || !draft.id || dirty} onClick={() => open({ view: "candidate-test", params: { functionId: draft.id } })}>{t("Test function")}</Button>
        <Button variant="primary" disabled={busy || !draft.id} onClick={() => void perform(publish)}>{t("Publish function")}</Button>
      </div>} />
    {draft.version ? <Panel role="status" className="text-xs">{t("Installed function version {version}. Accepted calls keep their saved inputs and definition.", { version: draft.version })}</Panel> : null}
    {dirty && <Panel role="status" className="text-xs">{t("Not saved yet. Publishing saves first.")}</Panel>}
    {error && <Panel role="alert" className="text-sm text-danger">{error}</Panel>}
    {dirty && issues.length > 0 && <Panel aria-live="polite" className="text-xs text-muted">{issues.join(" ")}</Panel>}
    {inventory.isError && <Panel role="alert">{t("The function sources could not be loaded.")}</Panel>}
    <fieldset disabled={busy} className="grid min-w-0 gap-3 xl:grid-cols-[12rem_minmax(0,1fr)_22rem]">
      <Panel role="region" aria-label={t("Function stages")} className="grid content-start gap-2 p-3">
        {stages.map((stage) => <Button key={stage.id} aria-pressed={chosen === stage.id} variant={chosen === stage.id ? "primary" : "ghost"} onClick={() => setChosen(stage.id)}>{stage.title}</Button>)}
        <p className="text-xs text-muted">{t("Select a stage or use these buttons to edit its properties. Source permissions are checked on every call.")}</p>
      </Panel>
      <FunctionMap draft={draft} source={source?.title} chosen={chosen} onChoose={setChosen} />
      <Panel role="region" aria-label={t("Function properties")} className="grid min-w-0 content-start gap-3 p-3">
        {chosen === "settings" && <>
          <label className="grid gap-1 text-xs">{t("Function name")}<Input disabled={!!draft.published} value={draft.name} onChange={(e) => change({ name: e.target.value })} /></label>
          <label className="grid gap-1 text-xs">{t("Function title")}<Input value={draft.title} onChange={(e) => change({ title: e.target.value })} /></label>
          <label className="grid gap-1 text-xs">{t("Function description")}<Textarea rows={3} value={draft.description} onChange={(e) => change({ description: e.target.value })} /></label>
          <fieldset className="grid gap-2"><legend className="mb-2 text-xs">{t("Callable by")}</legend>
            {[...new Set([...roles, ...draft.roles])].map((role) => <Checkbox key={role} checked={draft.roles.includes(role)} onChange={(enabled) => change({ roles: toggle(draft.roles, role, enabled) })}>{role}</Checkbox>)}
          </fieldset>
        </>}
        {chosen === "source" && <>
          <label className="grid gap-1 text-xs">{t("Source object")}<Select disabled={!!draft.published} value={draft.object} onChange={(e) => change({ object: e.target.value, fields: [] })}>
            <option value="">{t("Choose a published object")}</option>{objects.map((object) => <option key={object.id} value={`build.${object.name}`}>{object.title}</option>)}
          </Select></label>
          <fieldset className="grid gap-2"><legend className="mb-2 text-xs">{t("Input fields")}</legend>
            {fields.map((field) => <Checkbox key={field.name} checked={draft.fields.includes(field.name)} onChange={(enabled) => change({ fields: toggle(draft.fields, field.name, enabled) })}>{field.title} · {field.type}</Checkbox>)}
          </fieldset>
          <label className="grid gap-1 text-xs">{t("Maximum input bytes")}<Input type="number" min={1} max={32768} value={draft.maxInputBytes} onChange={(e) => change({ maxInputBytes: Number(e.target.value) })} /></label>
        </>}
        {chosen === "model" && <>
          <label className="grid gap-1 text-xs">{t("Model identifier (empty: application default)")}<Input value={draft.model ?? ""} onChange={(e) => change({ model: e.target.value })} placeholder="provider/model" /></label>
          <label className="grid gap-1 text-xs">{t("Model instructions")}<Textarea rows={8} value={draft.instructions} onChange={(e) => change({ instructions: e.target.value })} /></label>
          <label className="grid gap-1 text-xs">{t("Maximum tokens")}<Input type="number" min={1} max={4096} value={draft.maxTokens} onChange={(e) => change({ maxTokens: Number(e.target.value) })} /></label>
        </>}
        {chosen === "output" && <>
          <label className="grid gap-1 text-xs">{t("Maximum output bytes")}<Input type="number" min={1} max={32768} value={draft.maxOutputBytes} onChange={(e) => change({ maxOutputBytes: Number(e.target.value) })} /></label>
          {draft.output.map((field, index) => <Card key={index} role="group" aria-label={t("Output field {n}", { n: index + 1 })} className="grid gap-2 p-2">
            <label className="grid gap-1 text-xs">{t("Output field name")}<Input value={field.name} onChange={(e) => updateOutput(index, { name: e.target.value })} /></label>
            <label className="grid gap-1 text-xs">{t("Output type")}<Select value={field.type} onChange={(e) => updateOutput(index, { type: e.target.value, choices: undefined })}>
              <option value="string">{t("Text")}</option><option value="boolean">{t("Boolean")}</option><option value="integer">{t("Integer")}</option><option value="decimal">{t("Decimal")}</option>
            </Select></label>
            <label className="grid gap-1 text-xs">{t("Output description")}<Textarea rows={2} value={field.description ?? ""} onChange={(e) => updateOutput(index, { description: e.target.value })} /></label>
            <Checkbox checked={!!field.required} onChange={(required) => updateOutput(index, { required })}>{t("Required")}</Checkbox>
            {field.type === "string" && <label className="grid gap-1 text-xs">{t("Allowed text values (one per line, optional)")}<Textarea rows={2} value={field.choices?.join("\n") ?? ""} onChange={(e) => updateOutput(index, { choices: e.target.value ? e.target.value.split("\n") : undefined })} /></label>}
            <Button disabled={draft.output.length === 1} onClick={() => change({ output: draft.output.filter((_, i) => i !== index) })}>{t("Remove output")}</Button>
          </Card>)}
          <Button disabled={draft.output.length >= 16} onClick={() => change({ output: [...draft.output, { name: "", type: "string", required: true, description: "" }] })}>{t("Add output field")}</Button>
        </>}
      </Panel>
    </fieldset>
  </div>;
}

function FunctionMap({ draft, source, chosen, onChoose }: { draft: FunctionDraft; source?: string; chosen: Stage; onChoose: (stage: Stage) => void }) {
  const catalog: NodeCatalog = [
    { id: "source", title: t("Record inputs"), category: "function", inputs: [], outputs: [{ id: "record", label: t("Authorised fields"), type: "record" }] },
    { id: "model", title: t("Model inference"), category: "function", inputs: [{ id: "record", label: t("Record inputs"), type: "record" }], outputs: [{ id: "answer", label: t("Typed answer"), type: "answer" }] },
    { id: "output", title: t("Strict output"), category: "function", inputs: [{ id: "answer", label: t("Typed answer"), type: "answer" }], outputs: [] },
  ];
  const nodes: CanvasNode[] = [
    { id: "source", kind: "source", label: source || t("Choose a published object"), detail: draft.fields.join(", ") || t("Choose input fields"), position: { x: 0, y: 0 } },
    { id: "model", kind: "model", label: draft.model || t("Application default model"), detail: t("Up to {n} output tokens", { n: draft.maxTokens }), position: { x: 0, y: 150 } },
    { id: "output", kind: "output", label: t("Strict JSON object"), detail: draft.output.map((field) => `${field.name}: ${field.type}`).join(", "), position: { x: 0, y: 300 } },
  ];
  return <Card className="min-w-0 p-3">
    <NodeCanvas label={t("Function map")} catalog={catalog} nodes={nodes} selected={chosen} onSelect={(id) => onChoose(id as Stage)} height={460}
      edges={[{ id: "input", source: "source", sourcePort: "record", target: "model", targetPort: "record" }, { id: "answer", source: "model", sourcePort: "answer", target: "output", targetPort: "answer" }]} />
    <p className="mt-2 text-xs text-muted">{t("The function returns a suggestion. Human review and business actions remain separate.")}</p>
  </Card>;
}
