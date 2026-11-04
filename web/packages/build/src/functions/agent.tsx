import { useEffect, useState } from "react";
import { ContextHelp, Form, StructuredEditor } from "@platform/ui";
import type {Api} from "@platform/kernel";
import { useDefinitionDraft } from "../session/definition-draft";
import { useApplicationWorkspace } from "../projects/application-scope";
import { ResourceList } from "../editor/ResourceList";
import { DraftStatus, ResourceControls, WorkbenchMessage } from "../editor/workbench";
import { useAuthoringModel } from "../ontology/authoring-model";
import { useHost, ModelPicker, ModelOptionsEditor, useReadQuery, useRecordInventory, RunView, newId } from "@platform/app";
import { Button, Checkbox, Input, PageHeader, Panel, Select, Tag, Textarea, Toggles, Workbench, t } from "@platform/ui";
import { Plus, Trash2 } from "lucide-react";

type Case = { name: string; goal: string; ref?: string; expect?: string; asks?: boolean; takes?: string[]; avoids?: string[] };
type Draft = { id: string; revision: number; name: string; title: string; description: string; instructions: string; model?:string; modelOptions?:Api.ModelOptions; tools: string[]; steps?: number; tokens?: number; actions?: number; cost?: number;
  checkpoints?: string[]; handoffRole?: string; cases?: Case[]; state?: string; version?: number; published?: string };
type QueryRecord = { id: string; name: string; title: string; published?: string; archived?: boolean };
const empty = (): Draft => ({ id: "", revision: 0, name: "", title: "", description: "", instructions: "", tools: [], cases: [] });
const fieldClass = "grid gap-1 text-xs";

export function Agents() {
  const { source , holds} = useHost(), { open } = useApplicationWorkspace();
  if (!holds("build", "builder")) return <PageHeader title={t("Agents")} description={t("Only a builder can edit agents.")} />;
  return <div className="grid gap-3">
    <PageHeader title={t("Agents")} help={t("An agent works on the ontology with the actions and queries you give it, within a budget. What it cannot take back waits for a person; every step is kept.")}
      actions={<Button variant="primary" onClick={() => open({ view: "agent", params: { id: "new" } })}>{t("New agent")}</Button>} />
    <ResourceList source={source} type="build.agent" fields={["title", "name", "state", "version"]} onOpen={(record) => open({ view: "agent", params: { id: record.id } })} />
  </div>;
}

export function AgentEditor({ id }: { id: string }) {
  const { holds, decide, can } = useHost(), { open } = useApplicationWorkspace();
  const model = useAuthoringModel(), queries = useRecordInventory<QueryRecord>("build.query");
  const [debugGoal, setDebugGoal] = useState("");
  const [debugRun, setDebugRun] = useState<string>();
  const [debugTrace, setDebugTrace] = useState<{ at: string; step: string; what: string; detail: string }[]>([]);
  const [debugPoll, setDebugPoll] = useState<ReturnType<typeof setInterval>>();
  const runDebug = async () => {
    if (!debugGoal.trim() || !draft.id) return;
    const run = newId("RUN");
    if (await decide("agent.run.start", { type: "agent.run", id: run }, { agent: draft.id, goal: debugGoal })) {
      setDebugRun(run);
      setDebugGoal("");
      setDebugTrace([]);
      if (debugPoll) clearInterval(debugPoll);
      const poll = setInterval(async () => {
        try {
          const res = await fetch(`/v1/runs/${encodeURIComponent(run)}`);
          if (res.ok) {
            const data = await res.json();
            const trace = (data.trace ?? []).map((line: any) => ({
              at: line.at, step: line.step, what: line.what, detail: line.detail
            }));
            setDebugTrace(trace);
            if (data.state === "done" || data.state === "failed" || data.state === "cancelled") {
              clearInterval(poll);
            }
          }
        } catch { /* ignore poll errors */ }
      }, 1000);
      setDebugPoll(poll);
    }
  };
  const objects = (model.drafts.data?.records ?? []).filter(object => !object.archived);
  const objectTitles = new Map(objects.map(object => [`build.${object.name}`, object.title]));
  const tools = [
    ...model.actions.filter(action => objectTitles.has(action.target)).map(action => ({ value: action.schema, label: `${objectTitles.get(action.target)} · ${action.title}` })),
    ...(queries.data?.records ?? []).filter(query => !query.archived).map(query => ({ value: `query:${query.name}`, label: t("Query: {name}", { name: query.title }) })),
  ];
  const roles = [...new Set(["builder", ...objects.flatMap((o) => (o.access ?? []).map((a) => a.role))])];
  const editor = useDefinitionDraft<Draft>({
    id, type: "build.agent", view: "agent", empty,
    normalize: record => ({ ...record, tools: record.tools ?? [], cases: record.cases ?? [] }),
    failure: t("The agent could not be saved. Your draft is still here."),
    payload: ({ name, title, description, instructions, model, modelOptions, tools, steps, tokens, actions, cost, checkpoints, handoffRole, cases }) =>
      ({ name, title, description, instructions, model:model??"", modelOptions:modelOptions??{}, tools, steps: steps || 0, tokens: tokens || 0, actions: actions || 0, cost: cost || 0, checkpoints: checkpoints ?? [], handoffRole: handoffRole ?? "", cases: cases ?? [] }),
  });
  const { draft, query, dirty, busy, error, patch: change, perform, save, discardChanges } = editor;
  const selection=useReadQuery<Api.ModelSelection>("/v1/ai/model-selection?purpose=agent").data;
  const selectedModel=selection?.models.find(row=>`${row.model.provider}/${row.model.model}`===(draft.model||selection.default));
  const issues: string[] = [];
  if (!/^[a-z][a-z0-9]*$/.test(draft.name) || !(draft.title ?? "").trim() || !(draft.description ?? "").trim()) issues.push(t("Give the agent a lower-case name, a title and a description."));
  if (!(draft.instructions ?? "").trim()) issues.push(t("Write its instructions."));
  if (!(draft.tools ?? []).length) issues.push(t("Give it at least one action or query."));
  if (!holds("build", "builder")) return <Workbench storageKey="agent" title={t("Agent")}><WorkbenchMessage>{t("Only a builder can edit agents.")}</WorkbenchMessage></Workbench>;
  if (id !== "new" && !draft.id) return <Workbench storageKey="agent" title={t("Agent")}><WorkbenchMessage>{query.isError ? t("The agent could not be loaded.") : t("Loading…")}</WorkbenchMessage></Workbench>;
  const cases = draft.cases ?? [];
  const setCase = (i: number, patch: Partial<Case>) => change({ cases: cases.map((c, at) => at === i ? { ...c, ...patch } : c) });
  return <Workbench storageKey="agent" crumbs={[{ label: t("Agent definitions"), onClick: () => open({ view: "agent" }) }]} title={draft.title || t("New agent")}
    status={<DraftStatus state={draft.version ? "published" : "draft"} problems={dirty ? issues.length : 0} />} busy={busy}
    actions={<>
      <ResourceControls type="build.agent" record={draft} dirty={dirty} busy={busy} empty={!draft.id} onSave={() => perform(save)} onCancel={discardChanges} route={{ view: "agent", params: { id } }} />
    </>}><div className="grid min-h-0 min-w-0 flex-1 content-start gap-3 overflow-auto p-3">
    <ContextHelp title={t("Agent")}><p className="text-xs text-muted">{t("Save the declaration, then publish it: people ask it from any record it may read and flows give it steps. Evaluate its declared cases in Agents.")}</p></ContextHelp>
    {draft.version ? <Panel role="status" className="text-xs">{t("Installed as build.{name}, version {version}. A changed definition stops existing runs.", { name: draft.name, version: draft.version })}</Panel> : null}
    {error && <Panel role="alert" className="text-sm text-danger">{error}</Panel>}
    {(model.drafts.isError || queries.isError) && <Panel role="alert">{t("The capability library could not be loaded.")}</Panel>}
    <StructuredEditor {...editor.structured}><fieldset disabled={busy} className="grid min-w-0 gap-3 lg:grid-cols-2">
      <Panel title={t("Agent")} className="grid content-start gap-3">
        <label className={fieldClass}>{t("Agent name")}<Input disabled={!!draft.published} value={draft.name} placeholder="expediter" onChange={(e) => change({ name: e.target.value })} /></label>
        <label className={fieldClass}>{t("Agent title")}<Input value={draft.title} onChange={(e) => change({ title: e.target.value })} /></label>
        <label className={fieldClass}>{t("What it does for others")}<Textarea rows={2} value={draft.description} onChange={(e) => change({ description: e.target.value })} /></label>
        <ModelPicker value={draft.model??""} onChange={model=>change({model})} purpose="agent" />
        <ModelOptionsEditor value={draft.modelOptions??{}} onChange={modelOptions=>change({modelOptions})} capabilities={selectedModel?.model.capabilities} />
        <label className={fieldClass}>{t("Instructions")}<Textarea rows={8} value={draft.instructions} placeholder={t("The job, the order of work, what to check first, when to ask a person.")} onChange={(e) => change({ instructions: e.target.value })} /></label>
        <label className={fieldClass}>{t("Hands over to")}<Select value={draft.handoffRole ?? ""} onChange={(e) => change({ handoffRole: e.target.value || undefined })}>
          <option value="">{t("The builder")}</option>{roles.filter((r) => r !== "builder").map((r) => <option key={r} value={r}>{r}</option>)}</Select>
          <span className="text-xs text-muted">{t("Who takes the goal when a run stops, and who its questions go to when it runs for no one.")}</span></label>
      </Panel>
      <Panel title={t("Tools and budget")} className="grid content-start gap-3">
        <fieldset className={fieldClass}><legend className="mb-1">{t("Actions and queries it may use")}</legend>
          {tools.length ? <Toggles options={tools} value={(draft.tools ?? [])} onChange={(next) => change({ tools: next, checkpoints: (draft.checkpoints ?? []).filter((c) => next.includes(c)) })} /> : <p className="text-muted">{t("No actions or queries yet.")}</p>}
          <span className="text-xs text-muted">{t("It also always has: context, search, knowledge, remember, ask and finish.")}</span></fieldset>
        {(draft.tools ?? []).length > 0 && <fieldset className={fieldClass}><legend className="mb-1">{t("Checkpoints")}</legend>
          <Toggles options={tools.filter((x) => (draft.tools ?? []).includes(x.value))} value={draft.checkpoints ?? []} onChange={(checkpoints) => change({ checkpoints })} />
          <span className="text-xs text-muted">{t("Tools that always wait for a person to confirm, even when the agent runs on its own: the irreversible ones.")}</span></fieldset>}
        <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
          <label className={fieldClass}>{t("Model turns")}<Input draftKey="model-turns" type="number" min={1} value={draft.steps ?? ""} placeholder="10" onChange={(e) => change({ steps: Number(e.target.value) || undefined })} /></label>
          <label className={fieldClass}>{t("Tokens")}<Input draftKey="tokens" type="number" min={1} value={draft.tokens ?? ""} placeholder="40000" onChange={(e) => change({ tokens: Number(e.target.value) || undefined })} /></label>
          <label className={fieldClass}>{t("Actions")}<Input draftKey="actions" type="number" min={1} value={draft.actions ?? ""} placeholder="3" onChange={(e) => change({ actions: Number(e.target.value) || undefined })} /></label>
          <label className={fieldClass}>{t("USD per run")}<Input draftKey="usd-per-run" type="number" min={0} step={0.01} value={draft.cost ?? ""} placeholder="0" onChange={(e) => change({ cost: Number(e.target.value) || undefined })} /></label>
        </div>
      </Panel>
      <Panel title={t("Evaluation cases")} className="grid content-start gap-2 lg:col-span-2">
        <ContextHelp title={t("Evaluation cases")}><p className="text-xs text-muted">{t("Each case is a goal about a record and what a dry run must come to. Run the declared suite in Agents to evaluate a model; publishing does not run it automatically.")}</p></ContextHelp>
        {cases.map((c, i) => <div key={i} className="grid gap-2 rounded-md border border-border p-2 sm:grid-cols-[1fr_2fr_1fr_1fr_auto]">
          <label className={fieldClass}>{t("Case")}<Input value={c.name} onChange={(e) => setCase(i, { name: e.target.value })} /></label>
          <label className={fieldClass}>{t("Goal")}<Input value={c.goal} onChange={(e) => setCase(i, { goal: e.target.value })} /></label>
          <label className={fieldClass}>{t("About record")}<div className="grid grid-cols-[1fr_1fr] gap-1">
            <Select aria-label={t("Object")} value={refType(c.ref)} onChange={(e) => setCase(i, { ref: joinRef(e.target.value, refId(c.ref)) })}>
              <option value="">{t("Object")}</option>
              {refType(c.ref) && !objects.some((o) => `build.${o.name}` === refType(c.ref)) && <option value={refType(c.ref)}>{refType(c.ref)}</option>}
              {objects.map((o) => <option key={o.name} value={`build.${o.name}`}>{o.title}</option>)}
            </Select>
            <Input aria-label={t("Record id")} placeholder={t("record id")} value={refId(c.ref)} onChange={(e) => setCase(i, { ref: joinRef(refType(c.ref), e.target.value) })} />
          </div></label>
          <label className={fieldClass}>{t("Result must say")}<Input value={c.expect ?? ""} onChange={(e) => setCase(i, { expect: e.target.value || undefined })} /></label>
          <Button size="sm" variant="ghost" className="self-end" onClick={() => change({ cases: cases.filter((_, at) => at !== i) })}><Trash2 className="size-3" />{t("Remove")}</Button>
          <Checkbox checked={!!c.asks} onChange={(asks) => setCase(i, { asks: asks || undefined })}>{t("Must ask a person")}</Checkbox>
          <label className={`${fieldClass} sm:col-span-2`}>{t("Must take")}<Toggles options={tools.filter((x) => (draft.tools ?? []).includes(x.value))} value={c.takes ?? []} onChange={(takes) => setCase(i, { takes })} /></label>
          <label className={`${fieldClass} sm:col-span-2`}>{t("Must not take")}<Toggles options={tools.filter((x) => (draft.tools ?? []).includes(x.value))} value={c.avoids ?? []} onChange={(avoids) => setCase(i, { avoids })} /></label>
        </div>)}
        <Button size="sm" className="justify-self-start" onClick={() => change({ cases: [...cases, { name: t("Case {n}", { n: cases.length + 1 }), goal: "" }] })}><Plus className="size-3" />{t("Add a case")}</Button>
      </Panel>
    </fieldset></StructuredEditor>
    <Panel title={t("对话调试")} className="grid content-start gap-2 lg:col-span-2">
      <ContextHelp title={t("对话调试")}><p className="text-xs text-muted">{t("Run the saved draft against a goal, exactly as it would run installed. Use it to debug instructions and tools before publishing.")}</p></ContextHelp>
      {!draft.id ? (
        <p className="text-sm text-muted">{t("Save the agent before debugging.")}</p>
      ) : (
        <>
          <Form className="grid gap-2" onSubmit={runDebug}>
            <Textarea rows={3} placeholder={t("What should it do?")} value={debugGoal} onChange={(e) => setDebugGoal(e.target.value)} />
            <div className="flex items-center gap-2">
              <Button type="submit" variant="primary" disabled={!debugGoal.trim() || busy || !can("agent.run.start")}>{t("Run debug")}</Button>
            </div>
          </Form>
          {debugRun && <RunView key={debugRun} id={debugRun} compact />}
          {debugTrace.length > 0 && <Panel title={t("流式工具轨迹")} className="grid gap-2 max-h-64 overflow-auto">
            <ul className="grid gap-1 text-xs font-mono">
              {debugTrace.map((line, i) => <li key={i} className="flex gap-1 p-1 rounded bg-surface border-b border-border">
                <span className="w-16 text-2xs text-muted">{line.at}</span>
                <span className="w-20 truncate">{line.step}</span>
                <span className="w-24 truncate text-muted">{line.what}</span>
                <span className="truncate">{line.detail}</span>
              </li>)}
            </ul>
          </Panel>}
          {debugRun && debugTrace.length === 0 && <p className="text-xs text-muted">{t("Running… waiting for tool events.")}</p>}
        </>
      )}
    </Panel>
  </div></Workbench>;
}

const refType = (ref?: string) => ref?.includes("/") ? ref.slice(0, ref.indexOf("/")) : "";
const refId = (ref?: string) => ref?.includes("/") ? ref.slice(ref.indexOf("/") + 1) : ref ?? "";
const joinRef = (type: string, id: string) => (type || id ? `${type}/${id}` : undefined);
