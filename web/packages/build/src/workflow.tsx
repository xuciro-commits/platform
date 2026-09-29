// The owner definition is build.process. Canvas and keyboard properties edit
// its Steps/Next/Branches directly; Flow/Work owns all execution and testing.
import { useHost, useReadQuery, useRecordInventory } from "@platform/app";
import { Button, Card, Input, NodeCanvas, PageHeader, Panel, RecordList, Select, Textarea, canvasNodeHeight, layout, t, useWorkspace,
  type CanvasEdge, type CanvasNode, type NodeCatalog } from "@platform/ui";
import { useEffect, useState } from "react";

export type WorkflowStep = { name: string; title?: string; ask?: string; answers?: string[]; act?: string; next?: string; branches?: Record<string, string> };
export type WorkflowDraft = { id: string; revision: number; name: string; title: string; object: string; when: string; steps: WorkflowStep[]; version?: number; published?: string };
export type WorkflowObject = { id: string; name: string; title: string; published?: string; states?: { name: string; title: string }[];
  actions?: { name: string; title: string; inputs?: { required?: boolean }[]; approval?: unknown }[]; access?: { role: string; read: string }[] };

/** Pickers use the installed source semantics, even while its draft changes. */
export function installedObjects(records: WorkflowObject[]): WorkflowObject[] {
  return records.flatMap((record) => {
    if (!record.published) return [];
    try { const installed = JSON.parse(record.published) as WorkflowObject; return [installed]; }
    catch { return []; }
  });
}
const empty = (): WorkflowDraft => ({ id: "", revision: 0, name: "", title: "", object: "", when: "", steps: [] });
const nextName = (steps: WorkflowStep[], base: string) => {
  let name = base, n = 2;
  while (steps.some((step) => step.name === name)) name = `${base}${n++}`;
  return name;
};

export function Workflows() {
  const { source, role } = useHost();
  const { open } = useWorkspace();
  return <div className="grid gap-3">
    <PageHeader title={t("Workflows")} description={t("Start on an object state, ask people, then take governed actions.")}
      actions={role("build") === "builder" && <Button onClick={() => open({ view: "workflow", params: { id: "new" } })}>{t("New workflow")}</Button>} />
    <RecordList source={source} type="build.process" fields={["title", "name", "object", "version"]}
      onOpen={(record) => open({ view: "workflow", params: { id: record.id } })} />
  </div>;
}

export function WorkflowEditor({ id }: { id: string }) {
  const { decide, role } = useHost();
  const { open } = useWorkspace();
  const query = useReadQuery<{ record?: WorkflowDraft }>(`/v1/records/build.process/${encodeURIComponent(id)}`);
  const sources = useRecordInventory<WorkflowObject>("build.object");
  const objects = installedObjects(sources.data?.records ?? []).filter((object) => object.states?.length);
  const [draft, setDraft] = useState<WorkflowDraft>(empty);
  const [dirty, setDirty] = useState(false);
  const [chosen, setChosen] = useState(-1);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  useEffect(() => { if (query.data?.record && !dirty) setDraft(query.data.record); }, [query.data, dirty]);
  const object = objects.find((o) => `build.${o.name}` === draft.object);
  const actions = (object?.actions ?? []).filter((a) => !a.approval && !a.inputs?.some((input) => input.required));
  const roles = [...new Set(["builder", ...(object?.access?.length ? object.access.filter((a) => a.read === "all").map((a) => a.role) : ["user"])])];
  // Authoring hints only; the owner compiler remains the publication gate.
  const issues: string[] = [];
  const named = (name: string) => /^\p{Ll}[\p{Ll}\p{Nd}]*$/u.test(name);
  if (!object || !object.states?.some((s) => s.name === draft.when)) issues.push(t("Choose a published source and one of its states."));
  if (!draft.steps.length) issues.push(t("Add at least one human task or object action."));
  if (draft.steps.some((s) => !named(s.name)) || new Set(draft.steps.map((s) => s.name)).size !== draft.steps.length)
    issues.push(t("Each step needs a unique lower-case name."));
  if (draft.steps.some((s) => s.ask !== undefined ? !roles.includes(s.ask) : !actions.some((a) => a.name === s.act)))
    issues.push(t("Choose a readable human role or supported object action."));
  if (draft.steps.some((s) => s.answers?.some((answer) => !answer.trim()) || new Set(s.answers ?? []).size !== (s.answers?.length ?? 0)))
    issues.push(t("Human answers must be nonempty and unique."));
  if (draft.steps.some((s) => (s.next && !draft.steps.some((to) => to.name === s.next)) || Object.entries(s.branches ?? {}).some(([answer, to]) => !s.answers?.includes(answer) || !draft.steps.some((step) => step.name === to))))
    issues.push(t("Choose an existing step for every path."));
  const change = (patch: Partial<WorkflowDraft>) => { setDraft((old) => ({ ...old, ...patch })); setDirty(true); setError(""); };
  const update = (patch: Partial<WorkflowStep>) => change({ steps: draft.steps.map((step, i) => i === chosen ? { ...step, ...patch } : step) });
  const add = (kind: "ask" | "act") => {
    const step: WorkflowStep = { name: nextName(draft.steps, kind === "ask" ? "review" : "action"), title: kind === "ask" ? t("Human task") : t("Object action"),
      ...(kind === "ask" ? { ask: "user", answers: ["approve", "reject"] } : { act: actions[0]?.name ?? "" }) };
    setChosen(draft.steps.length); change({ steps: [...draft.steps, step] });
  };
  const save = async (): Promise<number | undefined> => {
    setError("");
    const target = draft.id || crypto.randomUUID();
    const payload = { name: draft.name, title: draft.title, object: draft.object, when: draft.when, steps: draft.steps };
    if (await decide(`build.process.${draft.id ? "edit" : "create"}`, { type: "build.process", id: target }, payload,
      { expectedRevision: draft.id ? draft.revision : undefined, quiet: true, onRefused: setError })) {
      const revision = draft.id ? draft.revision + 1 : 1;
      if (!draft.id) open({ view: "workflow", params: { id: target } });
      else {
        const refreshed = await query.refetch();
        if (refreshed.data?.record) setDraft(refreshed.data.record);
        setDirty(false);
      }
      return revision;
    }
  };
  const publish = async () => {
    const revision = dirty ? await save() : draft.revision;
    if (revision === undefined) return;
    if (await decide("build.process.publish", { type: "build.process", id: draft.id }, {}, { expectedRevision: revision, quiet: true, onRefused: setError })) await query.refetch();
  };
  const perform = async (action: () => Promise<unknown>) => { setBusy(true); try { await action(); } finally { setBusy(false); } };
  const reload = async () => {
    const fresh = await query.refetch();
    if (fresh.data?.record) { setDraft(fresh.data.record); setDirty(false); setError(""); }
  };
  const step = draft.steps[chosen];
  const move = (by: number) => {
    const next = chosen + by;
    if (next < 0 || next >= draft.steps.length) return;
    const steps = [...draft.steps]; [steps[chosen], steps[next]] = [steps[next]!, steps[chosen]!];
    change({ steps }); setChosen(next);
  };
  if (role("build") !== "builder") return <PageHeader title={t("Workflows")} description={t("Only a builder can edit workflows.")} />;
  if (id !== "new" && !draft.id) return <PageHeader title={t("Workflows")} description={query.isError ? t("The workflow could not be loaded.") : t("Loading…")} />;
  return <div className="flex flex-col gap-3 lg:min-h-0">
    <PageHeader title={draft.title || t("New workflow")} description={t("Save the draft, test fixed samples, then publish its next native version.")}
      actions={<div className="flex flex-wrap gap-2">
        <Button onClick={() => open({ view: "workflow" })}>{t("Workflows")}</Button>
        <Button disabled={busy || !draft.id} onClick={() => void perform(reload)}>{t("Reload saved workflow")}</Button>
        <Button disabled={busy || (!dirty && !!draft.id)} onClick={() => void perform(save)}>{t("Save workflow")}</Button>
        <Button disabled={busy || !draft.id || dirty} onClick={() => open({ view: "candidate-test", params: { processId: draft.id } })}>{t("Test workflow")}</Button>
        <Button variant="primary" disabled={busy || !draft.id} onClick={() => void perform(publish)}>{t("Publish workflow")}</Button>
      </div>} />
    {draft.version ? <Panel role="status" className="text-xs">{t("Installed workflow version {version}. Existing instances keep their starting version.", { version: draft.version })}</Panel> : null}
    {dirty && <Panel role="status" className="text-xs">{t("Not saved yet. Publishing saves first.")}</Panel>}
    {error && <Panel role="alert" className="text-sm text-danger">{error}</Panel>}
    {(dirty || draft.id) && issues.length > 0 && <Panel className="text-xs text-danger" aria-live="polite">{issues.join(" ")}</Panel>}
    {sources.isError && <Panel role="alert">{t("The workflow sources could not be loaded.")}</Panel>}
    <fieldset disabled={busy} className="grid min-w-0 gap-3 lg:grid-cols-[14rem_minmax(0,1fr)_20rem]">
      <Panel role="region" aria-label={t("Workflow steps")} className="grid content-start gap-2 p-3">
        <Button variant={chosen === -1 ? "primary" : "ghost"} onClick={() => setChosen(-1)}>{t("Workflow settings")}</Button>
        <p className="text-xs text-muted">{t("The first step starts the workflow. Connections choose what follows.")}</p>
        {draft.steps.map((s, i) => <Button key={i} variant={chosen === i ? "primary" : "ghost"} onClick={() => setChosen(i)}>{i + 1}. {s.title || s.name}</Button>)}
        <Button onClick={() => add("ask")}>{t("Add human task")}</Button>
        <Button disabled={!actions.length} onClick={() => add("act")}>{t("Add object action")}</Button>
      </Panel>
      <div className="min-w-0" role="region" aria-label={t("Workflow map")}>
        {issues.includes(t("Each step needs a unique lower-case name."))
          ? <Card className="p-3 text-sm text-danger">{t("Fix step names to draw the workflow map.")}</Card>
          : <WorkflowMap key={id} draft={draft} chosen={chosen} onChoose={setChosen} onChange={(steps) => change({ steps })} />}
      </div>
      <Panel role="region" aria-label={t("Workflow properties")} className="grid content-start gap-3 p-3">
        {chosen === -1 ? <>
          <label className="grid gap-1 text-xs">{t("Workflow name")}<Input disabled={!!draft.published} value={draft.name} onChange={(e) => change({ name: e.target.value })} /></label>
          <label className="grid gap-1 text-xs">{t("Workflow title")}<Input value={draft.title} onChange={(e) => change({ title: e.target.value })} /></label>
          <label className="grid gap-1 text-xs">{t("Source object")}<Select disabled={!!draft.published} value={draft.object} onChange={(e) => {
            const source = objects.find((o) => `build.${o.name}` === e.target.value);
            change({ object: e.target.value, when: source?.states?.[0]?.name ?? "" });
          }}><option value="">{t("Choose a published object")}</option>{objects.map((o) => <option key={o.id} value={`build.${o.name}`}>{o.title}</option>)}</Select></label>
          <label className="grid gap-1 text-xs">{t("Start state")}<Select value={draft.when} onChange={(e) => change({ when: e.target.value })}>
            <option value="">{t("Choose a state")}</option>{object?.states?.map((s) => <option key={s.name} value={s.name}>{s.title}</option>)}</Select></label>
        </> : step && <>
          <label className="grid gap-1 text-xs">{t("Step name")}<Input value={step.name} onChange={(e) => {
            const name = e.target.value, old = step.name;
            change({ steps: draft.steps.map((s, i) => ({ ...s, name: i === chosen ? name : s.name,
              next: s.next === old ? name : s.next, branches: Object.fromEntries(Object.entries(s.branches ?? {}).map(([answer, to]) => [answer, to === old ? name : to])) })) });
          }} /></label>
          <label className="grid gap-1 text-xs">{t("Step title")}<Input value={step.title ?? ""} onChange={(e) => update({ title: e.target.value })} /></label>
          <label className="grid gap-1 text-xs">{t("Step kind")}<Select value={step.ask !== undefined ? "ask" : "act"} onChange={(e) => update(e.target.value === "ask"
            ? { ask: "user", answers: ["approve", "reject"], act: undefined } : { act: actions[0]?.name ?? "", ask: undefined, answers: undefined, branches: undefined })}>
            <option value="ask">{t("Human task")}</option><option value="act">{t("Object action")}</option></Select></label>
          {step.ask !== undefined ? <>
            <label className="grid gap-1 text-xs">{t("Human role")}<Select value={step.ask} onChange={(e) => update({ ask: e.target.value })}>
              {!roles.includes(step.ask) && <option value={step.ask}>{step.ask}</option>}{roles.map((r) => <option key={r} value={r}>{r}</option>)}</Select></label>
            <label className="grid gap-1 text-xs">{t("Answers (one per line)")}<Textarea rows={3} value={(step.answers ?? []).join("\n")} onChange={(e) => {
              const answers = e.target.value.split("\n");
              update({ answers, branches: Object.fromEntries(Object.entries(step.branches ?? {}).filter(([answer]) => answers.includes(answer))) });
            }} /></label>
            {(step.answers ?? []).map((answer, i) => <label key={i} className="grid gap-1 text-xs">{t("After answer {answer}", { answer })}
              <Select value={step.branches?.[answer] ?? ""} onChange={(e) => {
                const branches = { ...step.branches }; if (e.target.value) branches[answer] = e.target.value; else delete branches[answer]; update({ branches });
              }}><option value="">{t("Use default next step")}</option>{draft.steps.filter((s) => s.name !== step.name).map((s) => <option key={s.name} value={s.name}>{s.title || s.name}</option>)}</Select>
            </label>)}
          </> : <label className="grid gap-1 text-xs">{t("Object action")}<Select value={step.act ?? ""} onChange={(e) => update({ act: e.target.value })}>
            <option value="">{t("Choose an action")}</option>{actions.map((a) => <option key={a.name} value={a.name}>{a.title}</option>)}</Select></label>}
          <label className="grid gap-1 text-xs">{t("Default next step")}<Select value={step.next ?? ""} onChange={(e) => update({ next: e.target.value })}>
            <option value="">{t("End workflow")}</option>{draft.steps.filter((s) => s.name !== step.name).map((s) => <option key={s.name} value={s.name}>{s.title || s.name}</option>)}</Select></label>
          <div className="flex flex-wrap gap-2"><Button disabled={chosen === 0} onClick={() => move(-1)}>{t("Move up")}</Button>
            <Button disabled={chosen === draft.steps.length - 1} onClick={() => move(1)}>{t("Move down")}</Button>
            <Button onClick={() => { change({ steps: draft.steps.filter((_, i) => i !== chosen).map((s) => ({ ...s, next: s.next === step.name ? "" : s.next,
              branches: Object.fromEntries(Object.entries(s.branches ?? {}).filter(([, to]) => to !== step.name)) })) }); setChosen(-1); }}>{t("Remove step")}</Button></div>
        </>}
      </Panel>
    </fieldset>
  </div>;
}

function WorkflowMap({ draft, chosen, onChoose, onChange }: { draft: WorkflowDraft; chosen: number; onChoose: (i: number) => void; onChange: (steps: WorkflowStep[]) => void }) {
  const catalog: NodeCatalog = [{ id: "@start", title: t("Start state"), category: "workflow", inputs: [], outputs: [{ id: "next", label: t("Starts"), type: "flow", limit: 1 }] },
    ...draft.steps.map((s, i) => ({ id: `step:${i}`, title: s.ask !== undefined ? t("Human task") : t("Object action"), category: "workflow",
      inputs: [{ id: "enter", label: t("Arrives here"), type: "flow" }], outputs: [{ id: "next", label: t("Default"), type: "flow", limit: 1 },
        ...(s.ask !== undefined ? (s.answers ?? []).map((answer, n) => ({ id: `answer:${n}`, label: answer, type: "flow", limit: 1 })) : [])] }))];
  const edges: CanvasEdge[] = draft.steps.flatMap((s, i) => [
    ...(i === 0 ? [{ id: "@start", source: "@start", sourcePort: "next", target: s.name, targetPort: "enter" }] : []),
    ...(s.next ? [{ id: `next:${s.name}`, source: s.name, sourcePort: "next", target: s.next, targetPort: "enter" }] : []),
    ...Object.entries(s.branches ?? {}).map(([answer, to]) => ({ id: `answer:${s.name}:${answer}`, source: s.name, sourcePort: `answer:${s.answers?.indexOf(answer)}`, target: to, targetPort: "enter" })),
  ]);
  const places = layout([{ id: "@start", label: draft.when }, ...draft.steps.map((s) => ({ id: s.name, label: s.title ?? s.name }))], edges.map((e) => ({ from: e.source, to: e.target })), "right",
    { width: 160, height: Math.max(...catalog.map(canvasNodeHeight)), gapX: 40, gapY: 30 });
  const nodes: CanvasNode[] = [{ id: "@start", kind: "@start", label: draft.when || t("Choose a state"), position: places.get("@start") ?? { x: 0, y: 0 } },
    ...draft.steps.map((s, i) => ({ id: s.name, kind: `step:${i}`, label: s.title || s.name, detail: s.ask || s.act, position: places.get(s.name) ?? { x: 0, y: 0 } }))];
  return <Card className="grid gap-2 p-3">
    <p className="text-xs text-muted">{t("Connect outputs to steps, or use the properties with your keyboard. Select a line and Delete to remove its path.")}</p>
    <NodeCanvas label={t("Workflow map")} catalog={catalog} nodes={nodes} edges={edges} selected={chosen < 0 ? "@start" : draft.steps[chosen]?.name}
      onSelect={(name) => onChoose(draft.steps.findIndex((s) => s.name === name))}
      onConnect={(c) => {
        if (c.source === "@start") {
          const target = draft.steps.find((s) => s.name === c.target);
          if (target) onChange([target, ...draft.steps.filter((s) => s !== target)]);
        } else onChange(draft.steps.map((s) => s.name !== c.source ? s : c.sourceHandle === "next" ? { ...s, next: c.target }
          : { ...s, branches: { ...s.branches, [s.answers?.[Number(c.sourceHandle?.split(":")[1])] ?? ""]: c.target } }));
      }}
      onDisconnect={(gone) => onChange(draft.steps.map((s) => ({ ...s, next: gone.some((e) => e.source === s.name && e.sourcePort === "next") ? "" : s.next,
        branches: Object.fromEntries(Object.entries(s.branches ?? {}).filter(([answer]) => !gone.some((e) => e.id === `answer:${s.name}:${answer}`))) })))} />
  </Card>;
}
