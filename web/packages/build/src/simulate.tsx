import { useHost, useRecordInventory } from "@platform/app";
import { Button, Card, Checkbox, FlowView, Input, PageHeader, Select, Textarea, t } from "@platform/ui";
import { apiErrorMessage, type Api } from "@platform/kernel";
import { installedObjects, type WorkflowDraft, type WorkflowObject } from "./workflow-model";
import { useEffect, useState } from "react";

type ObjectDraft = WorkflowObject;
type FunctionDraft = { id: string; name: string; title: string; object: string };
type Step = { type: string; id: string; action: string; payload: string; expect: "accepted" | "refused"; as?: string; advanceSeconds?: number; flow?: string; step?: string; answer?: string; function?: Api.FunctionFixture };
type EvaluationCase = { name: string; input: string; expected: string };
type EvaluationPolicy = { minQuality: number; maxCostUsd: number; maxLatencyMillis: number; cases: { name: string; input: Record<string, unknown>; expected: Record<string, unknown> }[] };
type TestPlan = { id: string; revision: number; title: string; object?: string; process?: string; function?: string; model?: string; as?: string; at: string; steps: Step[]; evaluation?: EvaluationPolicy[] };

/** The host owns the test runtime. This editor only assembles its fixed inputs. */
export function CandidateTest({ processId = "", functionId = "", objectId = "", embedded = false, onStepSelect }: {
  processId?: string; functionId?: string; objectId?: string; embedded?: boolean; onStepSelect?: (step: string) => void;
}) {
  const { client, role, decide } = useHost();
  const objects = useRecordInventory<ObjectDraft>("build.object");
  const plans = useRecordInventory<TestPlan>("build.testplan");
  const processes = useRecordInventory<WorkflowDraft>("build.process");
  const functions = useRecordInventory<FunctionDraft>("build.function");
  const [kind, setKind] = useState<"object" | "flow" | "function">(functionId ? "function" : processId ? "flow" : "object");
  const [model, setModel] = useState("fixture/probe");
  const [planID, setPlanID] = useState("");
  const [revision, setRevision] = useState(0);
  const [title, setTitle] = useState("");
  const [dirty, setDirty] = useState(false);
  const [saved, setSaved] = useState(false);
  const [id, setID] = useState(functionId || processId || objectId);
  const [member, setMember] = useState("");
  const [at, setAt] = useState("2026-01-01T09:00:00Z");
  const [steps, setSteps] = useState<Step[]>([]);
  const [evaluationEnabled, setEvaluationEnabled] = useState(false);
  const [minQuality, setMinQuality] = useState(1);
  const [maxCostUsd, setMaxCostUsd] = useState(0.1);
  const [maxLatencyMillis, setMaxLatencyMillis] = useState(30000);
  const [cases, setCases] = useState<EvaluationCase[]>([{ name: "synthetic", input: "{}", expected: "{}" }]);
  const [result, setResult] = useState<Api.CandidateSimulation>();
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const process = kind === "flow" ? processes.data?.records.find((record) => record.id === id) : undefined;
  const fn = kind === "function" ? functions.data?.records.find((record) => record.id === id) : undefined;
  const object = kind === "object" ? objects.data?.records.find((record) => record.id === id)
    : installedObjects(objects.data?.records ?? []).find((record) => `build.${record.name}` === (fn?.object ?? process?.object));
  const askSteps = process?.steps.filter((step) => step.ask !== undefined) ?? [];
  const functionSteps = (fn: FunctionDraft): Step[] => [
    { type: fn.object, id: "TEST-1", action: `${fn.object}.create`, payload: "{}", expect: "accepted" },
    { type: "build.function-call", id: "CALL-1", action: "build.function-call.start", payload: JSON.stringify({ name: fn.name, source: "TEST-1" }), expect: "accepted",
      function: { output: "{}", inputTokens: 0, outputTokens: 0, expectState: "ready" } },
  ];
  useEffect(() => { if (fn && steps.length === 0) setSteps(functionSteps(fn)); }, [fn, steps.length]);
  const workflowSteps = (workflow: WorkflowDraft): Step[] => {
    const ask = workflow.steps.find((step) => step.ask !== undefined);
    return [{ type: workflow.object, id: "TEST-1", action: `${workflow.object}.create`, payload: "{}", expect: "accepted", advanceSeconds: 2 },
      ...(ask ? [{ type: workflow.object, id: "TEST-1", action: "", payload: "{}", flow: `build.${workflow.name}`, step: ask.name, answer: ask.answers?.[0] ?? "", expect: "accepted" as const, advanceSeconds: 2 }] : [])];
  };
  useEffect(() => { if (process && steps.length === 0) setSteps(workflowSteps(process)); }, [process, steps.length]);
  useEffect(() => {
    if (objectId && kind === "object" && object && steps.length === 0) setSteps([
      { type: `build.${object.name}`, id: "TEST-1", action: `build.${object.name}.create`, payload: "{}", expect: "accepted" },
    ]);
  }, [objectId, kind, object, steps.length]);
  const actions = [{ name: "create", title: t("Create records") }, { name: "edit", title: t("Edit records") },
    { name: "archive", title: t("Archive records") }, ...(object?.actions ?? [])];
  const clear = () => { setResult(undefined); setError(""); setSaved(false); setDirty(true); };
  const update = (index: number, patch: Partial<Step>) => {
    clear();
    setSteps((old) => old.map((step, i) => i === index ? { ...step, ...patch } : step));
  };
  const load = (plan: TestPlan) => {
    clear(); setPlanID(plan.id); setRevision(plan.revision);
    setTitle(plan.title); setKind(plan.function ? "function" : plan.process ? "flow" : "object"); setID(plan.function || plan.process || plan.object || ""); setModel(plan.model ?? "fixture/probe"); setMember(plan.as ?? ""); setAt(plan.at);
    setSteps(plan.steps); setDirty(false);
    const policy = plan.evaluation?.[0];
    setEvaluationEnabled(Boolean(policy));
    setMinQuality(policy?.minQuality ?? 1); setMaxCostUsd(policy?.maxCostUsd ?? 0.1); setMaxLatencyMillis(policy?.maxLatencyMillis ?? 30000);
    setCases(policy?.cases.map((item) => ({ name: item.name, input: JSON.stringify(item.input, null, 2), expected: JSON.stringify(item.expected, null, 2) })) ?? [{ name: "synthetic", input: "{}", expected: "{}" }]);
  };
  const reload = async () => {
    setBusy(true);
    try {
      const refreshed = await plans.refetch();
      const plan = refreshed.data?.records.find((record) => record.id === planID);
      if (plan) load(plan);
      else setError(t("The saved test plans could not be loaded."));
    } catch { setError(t("The saved test plans could not be loaded.")); }
    finally { setBusy(false); }
  };
  const inputs = (): Api.CandidateSimulationRequest => ({
    ...(kind === "function" ? { functionId: id, model } : kind === "flow" ? { processId: id, model } : { objectId: id }), as: member, at: new Date(at).toISOString(), steps: steps.map((step) => ({
      ...step, payload: JSON.parse(step.payload),
    })),
  });
  const save = async () => {
    setError(""); setSaved(false);
    try { inputs(); } catch { setError(t("Use a valid fixed time and JSON inputs for every step.")); return; }
    let evaluation: EvaluationPolicy[] | null = null;
    if (kind === "function" && evaluationEnabled) {
      try {
        const parsed = cases.map((item) => ({ name: item.name.trim(), input: JSON.parse(item.input), expected: JSON.parse(item.expected) }));
        if (!Number.isFinite(minQuality) || minQuality <= 0 || minQuality > 1 || !Number.isFinite(maxCostUsd) || maxCostUsd <= 0 ||
          !Number.isInteger(maxLatencyMillis) || maxLatencyMillis < 1 || parsed.length < 1 || parsed.length > 5 ||
          parsed.some((item) => !item.name || !item.input || Array.isArray(item.input) || typeof item.input !== "object" ||
            !item.expected || Array.isArray(item.expected) || typeof item.expected !== "object")) throw new Error("invalid evaluation");
        evaluation = [{ minQuality, maxCostUsd, maxLatencyMillis, cases: parsed }];
      } catch { setError(t("Use bounded thresholds and JSON objects for every evaluation case.")); return; }
    }
    const target = planID || crypto.randomUUID();
    setBusy(true);
    try {
      if (await decide(`build.testplan.${planID ? "edit" : "create"}`, { type: "build.testplan", id: target },
        { title, object: kind === "object" ? id : "", process: kind === "flow" ? id : "", function: kind === "function" ? id : "", model: kind !== "object" ? model : "", as: member, at: new Date(at).toISOString(), steps, evaluation },
        { expectedRevision: planID ? revision : undefined, quiet: true, onRefused: setError })) {
        const refreshed = await plans.refetch();
        const record = refreshed.data?.records.find((plan) => plan.id === target);
        if (record) { setPlanID(target); setRevision(record.revision); setDirty(false); setSaved(true); }
        else setError(t("Reload the saved plans before editing again."));
      }
    } catch { setError(t("The test plan could not be saved.")); }
    finally { setBusy(false); }
  };
  const run = async () => {
    if (!object) return;
    setResult(undefined); setError("");
    let plan: Api.CandidateSimulationRequest;
    try { plan = inputs(); }
    catch { setError(t("Use a valid fixed time and JSON inputs for every step.")); return; }
    setBusy(true);
    try {
      const response = await client.call<Api.CandidateSimulation>("POST", "/v1/simulate/candidate", plan);
      if (response.ok) setResult(response.body);
      else setError(apiErrorMessage(response.body) ?? t("The candidate test could not be run."));
    } catch {
      setError(t("The candidate test could not be run."));
    } finally {
      setBusy(false);
    }
  };
  if (role("build") !== "builder") return <PageHeader title={t("Test a candidate")} description={t("Only a builder can test saved definitions.")} />;
  return <div className="grid gap-3">
    {!embedded && <PageHeader title={t("Test a candidate")} description={t("Try saved definitions with fixed sample actions and human answers. Every run starts empty; production records and effects are never used.")} />}
    <Card className="p-3">
      <fieldset disabled={busy} className="grid gap-3">
        <div className="grid gap-3 sm:grid-cols-2">
          <label className="grid gap-1 text-xs">{t("Saved test plan")}
            <Select value={planID} onChange={(event) => {
              const plan = plans.data?.records.find((record) => record.id === event.target.value);
              clear(); setPlanID(plan?.id ?? ""); setRevision(plan?.revision ?? 0);
              if (plan) load(plan);
            }}>
              <option value="">{t("New test plan")}</option>
              {(plans.data?.records ?? []).filter((plan) => !embedded || !processId || plan.process === processId).map((plan) => <option key={plan.id} value={plan.id}>{plan.title}</option>)}
            </Select>
          </label>
          <label className="grid gap-1 text-xs">{t("Test plan name")}
            <Input value={title} onChange={(event) => { setTitle(event.target.value); clear(); }} />
          </label>
        </div>
        {plans.isError && <p role="alert" className="text-sm text-danger">{t("The saved test plans could not be loaded.")}</p>}
        {!embedded && <label className="grid gap-1 text-xs">{t("Candidate kind")}
          <Select value={kind} onChange={(e) => { setKind(e.target.value as typeof kind); setID(""); setSteps([]); clear(); }}>
            <option value="object">{t("Objects")}</option><option value="flow">{t("Workflows")}</option><option value="function">{t("AI functions")}</option>
          </Select>
        </label>}
        {kind === "function" ? <>
          <label className="grid gap-1 text-xs">{t("Saved function draft")}
            <Select value={id} onChange={(e) => { setID(e.target.value); clear(); const chosen = functions.data?.records.find((f) => f.id === e.target.value); setSteps(chosen ? functionSteps(chosen) : []); }}>
              <option value="">{t("Choose a saved draft")}</option>{functions.data?.records.map((f) => <option key={f.id} value={f.id}>{f.title || f.name}</option>)}
            </Select>
          </label>

        </> : kind === "flow" ? <label className="grid gap-1 text-xs">{t("Saved workflow draft")}
          <Select value={id} disabled={embedded} onChange={(e) => { setID(e.target.value); clear(); const chosen = processes.data?.records.find((p) => p.id === e.target.value); setSteps(chosen ? workflowSteps(chosen) : []); }}>
            <option value="">{t("Choose a saved draft")}</option>{processes.data?.records.map((p) => <option key={p.id} value={p.id}>{p.title || p.name}</option>)}
          </Select>
        </label> : <label className="grid gap-1 text-xs">{t("Saved object draft")}
          <Select value={id} onChange={(event) => {
            setID(event.target.value); clear();
            const draft = objects.data?.records.find((record) => record.id === event.target.value);
            setSteps(draft ? [{ type: `build.${draft.name}`, id: "TEST-1", action: `build.${draft.name}.create`, payload: "{}", expect: "accepted" },
              { type: `build.${draft.name}`, id: "TEST-1", action: `build.${draft.name}.${draft.actions?.[0]?.name ?? "edit"}`, payload: "{}", expect: "accepted" }] : []);
          }}>
            <option value="">{t("Choose a saved draft")}</option>
            {(objects.data?.records ?? []).map((draft) => <option key={draft.id} value={draft.id}>{draft.title || draft.name}</option>)}
          </Select>
        </label>
        }
        {kind !== "object" && <>
          <label className="grid gap-1 text-xs">{t("Model identifier")}<Input value={model} onChange={(e) => { setModel(e.target.value); clear(); }} /></label>
          <p className="text-xs text-muted">{t("Fixed model answers test permissions and typed results. They do not measure real model quality.")}</p>
        </>}
        {kind === "function" && <div className="grid gap-3 rounded border border-border p-3">
          <Checkbox checked={evaluationEnabled} onChange={(enabled) => { setEvaluationEnabled(enabled); clear(); }}>{t("Require a measured release evaluation")}</Checkbox>
          {evaluationEnabled && <>
            <p className="text-xs text-muted">{t("These synthetic cases run three times each against the enabled model. Reported USD cost and per-call latency must stay within the limits.")}</p>
            <div className="grid gap-2 sm:grid-cols-3">
              <label className="grid gap-1 text-xs">{t("Minimum exact-match quality")}<Input type="number" min={0.01} max={1} step={0.01} value={minQuality} onChange={(e) => { setMinQuality(Number(e.target.value)); clear(); }} /></label>
              <label className="grid gap-1 text-xs">{t("Maximum total USD cost")}<Input type="number" min={0.000001} step={0.01} value={maxCostUsd} onChange={(e) => { setMaxCostUsd(Number(e.target.value)); clear(); }} /></label>
              <label className="grid gap-1 text-xs">{t("Maximum latency per call (ms)")}<Input type="number" min={1} max={300000} value={maxLatencyMillis} onChange={(e) => { setMaxLatencyMillis(Number(e.target.value)); clear(); }} /></label>
            </div>
            {cases.map((item, index) => <Card key={index} className="grid gap-2 p-2">
              <div className="flex items-center justify-between"><span className="text-xs font-medium">{t("Evaluation case {n}", { n: index + 1 })}</span><Button disabled={cases.length === 1} onClick={() => { setCases((old) => old.filter((_, i) => i !== index)); clear(); }}>{t("Remove")}</Button></div>
              <label className="grid gap-1 text-xs">{t("Case name")}<Input value={item.name} onChange={(e) => { setCases((old) => old.map((c, i) => i === index ? { ...c, name: e.target.value } : c)); clear(); }} /></label>
              <label className="grid gap-1 text-xs">{t("Synthetic input (JSON object)")}<Textarea rows={3} value={item.input} onChange={(e) => { setCases((old) => old.map((c, i) => i === index ? { ...c, input: e.target.value } : c)); clear(); }} className="font-mono" /></label>
              <label className="grid gap-1 text-xs">{t("Expected typed answer (JSON object)")}<Textarea rows={3} value={item.expected} onChange={(e) => { setCases((old) => old.map((c, i) => i === index ? { ...c, expected: e.target.value } : c)); clear(); }} className="font-mono" /></label>
            </Card>)}
            <Button disabled={cases.length >= 5} onClick={() => { setCases((old) => [...old, { name: `synthetic${old.length + 1}`, input: "{}", expected: "{}" }]); clear(); }}>{t("Add an evaluation case")}</Button>
          </>}
        </div>}
        {(objects.isError || processes.isError || functions.isError) && <p role="alert" className="text-sm text-danger">{t("The candidate test could not be run.")}</p>}
        <div className="grid gap-3 sm:grid-cols-2">
          <label className="grid gap-1 text-xs">{t("Member ID (empty: you)")}
            <Input value={member} onChange={(event) => { setMember(event.target.value); clear(); }} />
          </label>
          <label className="grid gap-1 text-xs">{t("Fixed test time")}
            <Input value={at} onChange={(event) => { setAt(event.target.value); clear(); }} />
          </label>
        </div>
        {steps.map((step, index) => <Card key={index} className="grid gap-2 p-3" role="group" aria-label={t("Test step {n}", { n: index + 1 })}>
          <div className="flex items-center justify-between gap-2"><span className="text-sm font-medium">{t("Test step {n}", { n: index + 1 })}</span>
            <Button disabled={steps.length === 1} onClick={() => { clear(); setSteps((old) => old.filter((_, i) => i !== index)); }}>{t("Remove")}</Button>
          </div>
          <div className="grid gap-2 sm:grid-cols-2">
            <label className="grid gap-1 text-xs">{t("Test record ID")}<Input value={step.id} onChange={(event) => update(index, { id: event.target.value })} /></label>
            {kind === "flow" && <label className="grid gap-1 text-xs">{t("Test step kind")}<Select value={step.answer !== undefined ? "answer" : step.action ? "action" : "clock"} onChange={(e) => update(index, e.target.value === "answer"
              ? { action: "", flow: `build.${process?.name}`, step: askSteps[0]?.name, answer: askSteps[0]?.answers?.[0] ?? "", payload: "{}" }
              : { action: e.target.value === "clock" ? "" : `${object ? `build.${object.name}` : step.type}.create`, advanceSeconds: e.target.value === "clock" ? 2 : step.advanceSeconds, flow: undefined, step: undefined, answer: undefined })}>
              <option value="action">{t("Object action")}</option><option value="answer">{t("Human answer")}</option><option value="clock">{t("Advance clock")}</option></Select></label>}
            {step.answer !== undefined ? <>
              <label className="grid gap-1 text-xs">{t("Human task step")}<Select value={step.step ?? ""} onChange={(e) => update(index, { step: e.target.value, answer: askSteps.find((s) => s.name === e.target.value)?.answers?.[0] ?? "" })}>
                {askSteps.map((ask) => <option key={ask.name} value={ask.name}>{ask.title || ask.name}</option>)}</Select></label>
              <label className="grid gap-1 text-xs">{t("Human answer")}<Select value={step.answer} onChange={(e) => update(index, { answer: e.target.value })}>
                {askSteps.find((ask) => ask.name === step.step)?.answers?.map((answer) => <option key={answer} value={answer}>{answer}</option>)}</Select></label>
            </> : step.action ? <label className="grid gap-1 text-xs">{t("Action")}<Select value={step.action} onChange={(event) => update(index, { action: event.target.value, type: event.target.value === "build.function-call.start" ? "build.function-call" : `build.${object?.name}`, function: event.target.value === "build.function-call.start" ? { output: "{}", inputTokens: 0, outputTokens: 0, expectState: "ready" } : undefined })}>
              {kind === "function" && <option value="build.function-call.start">{t("Call AI function")}</option>}
              {step.action !== "build.function-call.start" && !actions.some((action) => `build.${object?.name}.${action.name}` === step.action) && <option value={step.action}>{step.action}</option>}
              {actions.map((action) => <option key={action.name} value={`build.${object?.name}.${action.name}`}>{action.title}</option>)}
            </Select></label> : null}
          </div>
          {(kind === "flow" || kind === "function") && <div className="grid gap-2 sm:grid-cols-2">
            <label className="grid gap-1 text-xs">{t("Step member ID (empty: plan member)")}<Input value={step.as ?? ""} onChange={(e) => update(index, { as: e.target.value })} /></label>
            <label className="grid gap-1 text-xs">{t("Advance clock (seconds)")}<Input type="number" min={0} max={86400} value={step.advanceSeconds ?? 0} onChange={(e) => update(index, { advanceSeconds: Number(e.target.value) })} /></label>
          </div>}
          <label className="grid gap-1 text-xs">{t("Expected outcome")}
            <Select value={step.expect} onChange={(event) => update(index, { expect: event.target.value as Step["expect"] })}>
              <option value="accepted">{t("Accepted")}</option><option value="refused">{t("Refused")}</option>
            </Select>
          </label>
          <label className="grid gap-1 text-xs">{t("Test inputs (JSON)")}
            <Textarea rows={3} value={step.payload} onChange={(event) => update(index, { payload: event.target.value })} className="font-mono" />
          </label>
          {(kind === "flow" || step.action === "build.function-call.start") && <Checkbox checked={!!step.function} onChange={(enabled) => update(index, { function: enabled ? { output: "{}", inputTokens: 0, outputTokens: 0, expectState: "ready" } : undefined })}>{t("Supply a fixed model answer")}</Checkbox>}
          {step.function && <fieldset className="grid gap-2 rounded border border-border p-2">
            <legend className="px-1 text-xs">{t("Fixed model answer")}</legend>
            <label className="grid gap-1 text-xs">{t("Provider answer")}<Textarea rows={3} value={step.function.output} onChange={(e) => update(index, { function: { ...step.function!, output: e.target.value } })} className="font-mono" /></label>
            <div className="grid gap-2 sm:grid-cols-2">
              <label className="grid gap-1 text-xs">{t("Input tokens")}<Input type="number" min={0} max={1048576} value={step.function.inputTokens} onChange={(e) => update(index, { function: { ...step.function!, inputTokens: Number(e.target.value) } })} /></label>
              <label className="grid gap-1 text-xs">{t("Output tokens")}<Input type="number" min={0} max={1048576} value={step.function.outputTokens} onChange={(e) => update(index, { function: { ...step.function!, outputTokens: Number(e.target.value) } })} /></label>
            </div>
            <label className="grid gap-1 text-xs">{t("Expected function state")}<Select value={step.function.expectState} onChange={(e) => update(index, { function: { ...step.function!, expectState: e.target.value as Api.FunctionFixture["expectState"], expectOutput: e.target.value === "rejected" ? undefined : step.function?.expectOutput } })}>
              <option value="ready">{t("Ready")}</option><option value="rejected">{t("Rejected")}</option>
            </Select></label>
            {step.function.expectState === "ready" && <label className="grid gap-1 text-xs">{t("Expected typed answer (JSON, optional)")}<Textarea rows={3} value={step.function.expectOutput ?? ""} onChange={(e) => update(index, { function: { ...step.function!, expectOutput: e.target.value } })} className="font-mono" /></label>}
          </fieldset>}
        </Card>)}
        <div className="flex flex-wrap gap-2">
          <Button disabled={!object || steps.length >= 20} onClick={() => { clear(); setSteps((old) => [...old, { type: `build.${object?.name}`, id: "TEST-1", action: `build.${object?.name}.${object?.actions?.[0]?.name ?? "edit"}`, payload: "{}", expect: "accepted" }]); }}>{t("Add a test step")}</Button>
          <Button disabled={!object || !steps.length || !title.trim()} onClick={save}>{t("Save test plan")}</Button>
          <Button disabled={!planID} onClick={reload}>{t("Reload saved plan")}</Button>
          <Button onClick={() => { clear(); setPlanID(""); setRevision(0); setTitle(""); }}>{t("New test plan")}</Button>
          <Button variant="primary" disabled={!object || !steps.length} onClick={run}>{busy ? t("Running test…") : t("Run isolated test")}</Button>
        </div>
      </fieldset>
    </Card>
    {saved && <p role="status" className="text-sm">{t("Test plan saved. Reload it to repeat these fixed inputs.")}</p>}
    {planID && dirty && <p className="text-sm text-warning">{t("This plan has unsaved changes.")}</p>}
    {error && <Card role="alert" className="p-3 text-sm text-danger">{error}</Card>}
    {result && <Card className="grid gap-3 p-3" aria-live="polite">
      {result.passed !== undefined && <p role="status" className="text-sm font-medium">{result.passed ? t("All expected outcomes matched.") : t("Some expected outcomes did not match.")}</p>}
      <p className="break-all text-xs">{t("Tested candidate")}: <code>{result.candidateId}</code></p>
      {result.fixture && <>
        <p className="text-sm">{t("Fixed model answers test permissions and typed results. They do not measure real model quality.")}</p>
        <p className="break-all text-xs">{t("Fixed test identity")}: <code>{result.testId}</code></p>
        <p className="text-xs">{t("Fixture model identifier")}: {result.model || t("No model bound")}</p>
      </>}
      {result.recovered && <p role="status" className="text-sm">{t("Test state recovered exactly. Sample records stayed isolated.")}</p>}
      {result.steps.map((step, index) => <div key={index} className="grid gap-1 border-t border-border pt-2">
        <p className="text-sm font-medium">{t("Test step {n}", { n: index + 1 })} · {step.accepted ? t("Accepted") : t("Refused")}</p>
        {step.matched !== undefined && <p className="text-xs">{step.matched ? t("Expected outcome matched") : t("Expected outcome did not match")}</p>}
        {step.functionMatched !== undefined && <p className="text-xs">{step.functionMatched ? t("Function expectation matched") : t("Function expectation did not match")}</p>}
        {step.functions?.map((call) => <Card key={call.id} className="min-w-0 grid gap-1 p-2 text-xs">
          <p>{call.function} · {t("Version")} {call.version} · {call.state === "ready" ? t("Ready") : call.state === "rejected" ? t("Rejected") : t("Pending")}</p>
          <p className="break-all">{call.source} · {call.model}</p>
          {call.output && <pre className="whitespace-pre-wrap break-words">{call.output}</pre>}
          {call.reason && <p className="text-danger">{call.reason}</p>}
          {call.withheld && <p>{t("Some sources cannot be read")}</p>}
        </Card>)}
        {step.refusal && <p className="text-sm text-danger">{step.refusal}</p>}
        {step.flows?.map((flow) => <Card key={flow.id} className="min-w-0 overflow-x-auto p-2">
          <FlowView instance={{ ...flow, title: process?.title ?? flow.flow, key: flow.id, undo: null }} onStepSelect={onStepSelect} />
        </Card>)}
        {step.tasks?.map((task) => <p key={task.id} className="text-xs">{t("Human task")}: {task.title}</p>)}
        {step.changes.map((change) => <div key={`${change.type}/${change.id}`} className="min-w-0 text-xs">
          <p className="font-mono">{change.type}/{change.id}</p>
          <pre className="overflow-x-auto rounded bg-muted/10 p-2">{JSON.stringify(change.record, null, 2)}</pre>
        </div>)}
      </div>)}
    </Card>}
  </div>;
}
