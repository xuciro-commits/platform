// Agents a builder declares over the ontology (ADR-0077): instructions, the
// published actions and queries they may use, a budget, checkpoints and the
// cases they must pass. Publishing installs one as build.<name>; the host runs
// it like any agent - every step journaled, drafts confirmed by a person.
import { useApplicationWorkspace } from "../projects/application-scope";
import { DraftStatus } from "../shared/workbench";
import { installedObjects, type WorkflowObject } from "../automate/workflow-model";
import { useHost, useReadQuery, useRecordInventory } from "@platform/app";
import { Button, Checkbox, Input, PageHeader, Panel, RecordList, Select, Textarea, Toggles, t, useUnsavedChanges } from "@platform/ui";
import { Plus, Trash2 } from "lucide-react";
import { useEffect, useRef, useState } from "react";

type Case = { name: string; goal: string; ref?: string; expect?: string; asks?: boolean; takes?: string[]; avoids?: string[] };
type Draft = { id: string; revision: number; name: string; title: string; description: string; instructions: string; tools: string[]; steps?: number; tokens?: number; actions?: number; cost?: number;
  checkpoints?: string[]; handoffRole?: string; cases?: Case[]; state?: string; version?: number; published?: string };
type QueryRecord = { id: string; name: string; title: string; published?: string };
const empty = (): Draft => ({ id: "", revision: 0, name: "", title: "", description: "", instructions: "", tools: [], cases: [] });
const fieldClass = "grid gap-1 text-xs";

export function Agents() {
  const { source, role } = useHost(), { open } = useApplicationWorkspace();
  if (role("build") !== "builder") return <PageHeader title={t("Agents")} description={t("Only a builder can edit agents.")} />;
  return <div className="grid gap-3">
    <PageHeader title={t("Agents")} description={t("An agent works on the ontology with the actions and queries you give it, within a budget. What it cannot take back waits for a person; every step is kept.")}
      actions={<Button onClick={() => open({ view: "agent", params: { id: "new" } })}>{t("New agent")}</Button>} />
    <RecordList source={source} type="build.agent" fields={["title", "name", "state", "version"]} onOpen={(record) => open({ view: "agent", params: { id: record.id } })} />
  </div>;
}

export function AgentEditor({ id }: { id: string }) {
  const { decide, role } = useHost(), { open, close } = useApplicationWorkspace();
  const query = useReadQuery<{ record?: Draft }>(`/v1/records/build.agent/${encodeURIComponent(id)}`);
  const inventory = useRecordInventory<WorkflowObject>("build.object"), queries = useRecordInventory<QueryRecord>("build.query");
  const objects = installedObjects(inventory.data?.records ?? []);
  const tools = [
    ...objects.flatMap((o) => (o.actions ?? []).map((a) => ({ value: `build.${o.name}.${a.name}`, label: `${o.title} · ${a.title}` }))),
    ...(queries.data?.records ?? []).filter((q) => q.published).map((q) => ({ value: `query:${q.name}`, label: t("Query: {name}", { name: q.title }) })),
  ];
  const roles = [...new Set(["builder", ...objects.flatMap((o) => (o.access ?? []).map((a) => a.role))])];
  const [draft, setDraft] = useState<Draft>(empty), [dirty, setDirty] = useState(false), [busy, setBusy] = useState(false), [error, setError] = useState("");
  const loaded = useRef("");
  const load = (record: Draft) => { setDraft({ ...empty(), ...record, tools: record.tools ?? [], cases: record.cases ?? [] }); loaded.current = `${record.id}:${record.revision}`; };
  const { markSaved, discardChanges } = useUnsavedChanges(dirty, () => { if (query.data?.record) load(query.data.record); else setDraft(empty()); setDirty(false); setError(""); });
  useEffect(() => { const r = query.data?.record; if (r && !dirty && !busy && loaded.current !== `${r.id}:${r.revision}`) load(r); }, [query.data, dirty, busy]);
  const change = (patch: Partial<Draft>) => { setDraft((d) => ({ ...d, ...patch })); setDirty(true); setError(""); };
  const issues: string[] = [];
  if (!/^[a-z][a-z0-9]*$/.test(draft.name) || !draft.title.trim() || !draft.description.trim()) issues.push(t("Give the agent a lower-case name, a title and a description."));
  if (!draft.instructions.trim()) issues.push(t("Write its instructions."));
  if (!draft.tools.length) issues.push(t("Give it at least one action or query."));
  const perform = async (action: () => Promise<unknown>) => { setBusy(true); setError(""); try { await action(); } catch { setError(t("The agent could not be saved. Your draft is still here.")); } finally { setBusy(false); } };
  const payload = () => {
    const { name, title, description, instructions, tools, steps, tokens, actions, cost, checkpoints, handoffRole, cases } = draft;
    return { name, title, description, instructions, tools, steps: steps || 0, tokens: tokens || 0, actions: actions || 0, cost: cost || 0, checkpoints: checkpoints ?? [], handoffRole: handoffRole ?? "", cases: cases ?? [] };
  };
  const save = async (): Promise<{ id: string; revision: number } | undefined> => {
    const target = draft.id || crypto.randomUUID();
    if (!await decide(`build.agent.${draft.id ? "edit" : "create"}`, { type: "build.agent", id: target }, payload(), { expectedRevision: draft.id ? draft.revision : 0, quiet: true, onRefused: setError })) return;
    const revision = draft.id ? draft.revision + 1 : 1;
    markSaved(); setDirty(false);
    if (!draft.id) { open({ view: "agent", params: { id: target } }); close({ view: "agent", params: { id } }); }
    else { const r = await query.refetch(); if (r.data?.record) load(r.data.record); }
    return { id: target, revision };
  };
  const publish = async () => {
    if (issues.length) { setError(issues.join(" ")); return; }
    const saved = dirty || !draft.id ? await save() : { id: draft.id, revision: draft.revision };
    if (!saved) return;
    if (await decide("build.agent.publish", { type: "build.agent", id: saved.id }, {}, { expectedRevision: saved.revision, quiet: true, onRefused: setError })) {
      const r = await query.refetch(); if (r.data?.record) load(r.data.record);
    }
  };
  if (role("build") !== "builder") return <PageHeader title={t("Agents")} description={t("Only a builder can edit agents.")} />;
  if (id !== "new" && !draft.id) return <PageHeader title={t("Agents")} description={query.isError ? t("The agent could not be loaded.") : t("Loading…")} />;
  const cases = draft.cases ?? [];
  const setCase = (i: number, patch: Partial<Case>) => change({ cases: cases.map((c, at) => at === i ? { ...c, ...patch } : c) });
  return <div className="grid min-w-0 gap-3">
    <PageHeader title={draft.title || t("New agent")} description={t("Save the declaration, then publish it: people ask it from any record it may read, flows give it steps, and its cases run against the model before it meets people.")}
      actions={<div className="flex flex-wrap gap-2">
        <Button variant="ghost" onClick={() => open({ view: "agent" })}>{t("Agents")}</Button>
        <DraftStatus state={draft.version ? "published" : "draft"} problems={dirty ? issues.length : 0} />
        <Button disabled={busy || !dirty} onClick={discardChanges}>{t("Discard")}</Button>
        <Button disabled={busy || (!dirty && !!draft.id)} onClick={() => void perform(save)}>{t("Save agent")}</Button>
        <Button variant="primary" disabled={busy || issues.length > 0} onClick={() => void perform(publish)}>{t("Publish")}</Button>
      </div>} />
    {draft.version ? <Panel role="status" className="text-xs">{t("Installed as build.{name}, version {version}. A changed definition stops existing runs.", { name: draft.name, version: draft.version })}</Panel> : null}
    {error && <Panel role="alert" className="text-sm text-danger">{error}</Panel>}
    <fieldset disabled={busy} className="grid min-w-0 gap-3 lg:grid-cols-2">
      <Panel title={t("Agent")} className="grid content-start gap-3">
        <label className={fieldClass}>{t("Agent name")}<Input disabled={!!draft.published} value={draft.name} placeholder="expediter" onChange={(e) => change({ name: e.target.value })} /></label>
        <label className={fieldClass}>{t("Agent title")}<Input value={draft.title} onChange={(e) => change({ title: e.target.value })} /></label>
        <label className={fieldClass}>{t("What it does for others")}<Textarea rows={2} value={draft.description} onChange={(e) => change({ description: e.target.value })} /></label>
        <label className={fieldClass}>{t("Instructions")}<Textarea rows={8} value={draft.instructions} placeholder={t("The job, the order of work, what to check first, when to ask a person.")} onChange={(e) => change({ instructions: e.target.value })} /></label>
        <label className={fieldClass}>{t("Hands over to")}<Select value={draft.handoffRole ?? ""} onChange={(e) => change({ handoffRole: e.target.value || undefined })}>
          <option value="">{t("The builder")}</option>{roles.filter((r) => r !== "builder").map((r) => <option key={r} value={r}>{r}</option>)}</Select>
          <span className="text-[11px] text-muted">{t("Who takes the goal when a run stops, and who its questions go to when it runs for no one.")}</span></label>
      </Panel>
      <Panel title={t("Tools and budget")} className="grid content-start gap-3">
        <fieldset className={fieldClass}><legend className="mb-1">{t("Actions and queries it may use")}</legend>
          {tools.length ? <Toggles options={tools} value={draft.tools} onChange={(next) => change({ tools: next, checkpoints: (draft.checkpoints ?? []).filter((c) => next.includes(c)) })} /> : <p className="text-muted">{t("Publish an object with actions, or a query, first.")}</p>}
          <span className="text-[11px] text-muted">{t("It also always has: context, search, knowledge, remember, ask and finish.")}</span></fieldset>
        {draft.tools.length > 0 && <fieldset className={fieldClass}><legend className="mb-1">{t("Checkpoints")}</legend>
          <Toggles options={tools.filter((x) => draft.tools.includes(x.value))} value={draft.checkpoints ?? []} onChange={(checkpoints) => change({ checkpoints })} />
          <span className="text-[11px] text-muted">{t("Tools that always wait for a person to confirm, even when the agent runs on its own: the irreversible ones.")}</span></fieldset>}
        <div className="grid grid-cols-2 gap-2 sm:grid-cols-4">
          <label className={fieldClass}>{t("Model turns")}<Input type="number" min={1} value={draft.steps ?? ""} placeholder="10" onChange={(e) => change({ steps: Number(e.target.value) || undefined })} /></label>
          <label className={fieldClass}>{t("Tokens")}<Input type="number" min={1} value={draft.tokens ?? ""} placeholder="40000" onChange={(e) => change({ tokens: Number(e.target.value) || undefined })} /></label>
          <label className={fieldClass}>{t("Actions")}<Input type="number" min={1} value={draft.actions ?? ""} placeholder="3" onChange={(e) => change({ actions: Number(e.target.value) || undefined })} /></label>
          <label className={fieldClass}>{t("USD per run")}<Input type="number" min={0} step={0.01} value={draft.cost ?? ""} placeholder="0" onChange={(e) => change({ cost: Number(e.target.value) || undefined })} /></label>
        </div>
      </Panel>
      <Panel title={t("Evaluation cases")} className="grid content-start gap-2 lg:col-span-2">
        <p className="text-xs text-muted">{t("Each case is a goal about a record and what a dry run must come to. Run the declared suite in Agents to evaluate a model; publishing does not run it automatically.")}</p>
        {cases.map((c, i) => <div key={i} className="grid gap-2 rounded-md border border-border p-2 sm:grid-cols-[1fr_2fr_1fr_1fr_auto]">
          <label className={fieldClass}>{t("Case")}<Input value={c.name} onChange={(e) => setCase(i, { name: e.target.value })} /></label>
          <label className={fieldClass}>{t("Goal")}<Input value={c.goal} onChange={(e) => setCase(i, { goal: e.target.value })} /></label>
          <label className={fieldClass}>{t("About record")}<Input value={c.ref ?? ""} placeholder="build.order/o-1" onChange={(e) => setCase(i, { ref: e.target.value || undefined })} /></label>
          <label className={fieldClass}>{t("Result must say")}<Input value={c.expect ?? ""} onChange={(e) => setCase(i, { expect: e.target.value || undefined })} /></label>
          <Button size="sm" variant="ghost" className="self-end" onClick={() => change({ cases: cases.filter((_, at) => at !== i) })}><Trash2 className="size-3" />{t("Remove")}</Button>
          <Checkbox checked={!!c.asks} onChange={(asks) => setCase(i, { asks: asks || undefined })}>{t("Must ask a person")}</Checkbox>
          <label className={`${fieldClass} sm:col-span-2`}>{t("Must take")}<Toggles options={tools.filter((x) => draft.tools.includes(x.value))} value={c.takes ?? []} onChange={(takes) => setCase(i, { takes })} /></label>
          <label className={`${fieldClass} sm:col-span-2`}>{t("Must not take")}<Toggles options={tools.filter((x) => draft.tools.includes(x.value))} value={c.avoids ?? []} onChange={(avoids) => setCase(i, { avoids })} /></label>
        </div>)}
        <Button size="sm" className="justify-self-start" onClick={() => change({ cases: [...cases, { name: t("Case {n}", { n: cases.length + 1 }), goal: "" }] })}><Plus className="size-3" />{t("Add a case")}</Button>
      </Panel>
    </fieldset>
  </div>;
}
