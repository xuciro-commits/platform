import { useHost, useReadQuery } from "@platform/app";
import { Button, Card, Input, PageHeader, Select, Textarea, t } from "@platform/ui";
import { apiErrorMessage, type Api } from "@platform/kernel";
import { useState } from "react";

type ObjectDraft = { id: string; name: string; title: string; actions?: { name: string; title: string }[] };
type Step = { type: string; id: string; action: string; payload: string; expect: "accepted" | "refused" };
type TestPlan = { id: string; revision: number; title: string; object: string; as?: string; at: string; steps: Step[] };

/** The host owns the test runtime. This editor only assembles its fixed inputs. */
export function CandidateTest() {
  const { client, role, decide } = useHost();
  const objects = useReadQuery<{ records: ObjectDraft[] }>("/v1/records/build.object?limit=1000");
  const plans = useReadQuery<{ records: TestPlan[] }>("/v1/records/build.testplan?limit=1000");
  const [planID, setPlanID] = useState("");
  const [revision, setRevision] = useState(0);
  const [title, setTitle] = useState("");
  const [dirty, setDirty] = useState(false);
  const [saved, setSaved] = useState(false);
  const [id, setID] = useState("");
  const [member, setMember] = useState("");
  const [at, setAt] = useState("2026-01-01T09:00:00Z");
  const [steps, setSteps] = useState<Step[]>([]);
  const [result, setResult] = useState<Api.CandidateSimulation>();
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const object = objects.data?.records.find((record) => record.id === id);
  const actions = [{ name: "create", title: t("Create records") }, { name: "edit", title: t("Edit records") },
    { name: "archive", title: t("Archive records") }, ...(object?.actions ?? [])];
  const clear = () => { setResult(undefined); setError(""); setSaved(false); setDirty(true); };
  const update = (index: number, patch: Partial<Step>) => {
    clear();
    setSteps((old) => old.map((step, i) => i === index ? { ...step, ...patch } : step));
  };
  const load = (plan: TestPlan) => {
    clear(); setPlanID(plan.id); setRevision(plan.revision);
    setTitle(plan.title); setID(plan.object); setMember(plan.as ?? ""); setAt(plan.at);
    setSteps(plan.steps); setDirty(false);
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
    objectId: id, as: member, at: new Date(at).toISOString(), steps: steps.map((step) => ({
      type: step.type, id: step.id, action: step.action, expect: step.expect, payload: JSON.parse(step.payload),
    })),
  });
  const save = async () => {
    setError(""); setSaved(false);
    try { inputs(); } catch { setError(t("Use a valid fixed time and JSON inputs for every step.")); return; }
    const target = planID || crypto.randomUUID();
    setBusy(true);
    try {
      if (await decide(`build.testplan.${planID ? "edit" : "create"}`, { type: "build.testplan", id: target },
        { title, object: id, as: member, at: new Date(at).toISOString(), steps },
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
    <PageHeader title={t("Test a candidate")} description={t("Try a saved object draft with fixed sample actions. Every run starts empty; production records and effects are never used.")} />
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
              {(plans.data?.records ?? []).map((plan) => <option key={plan.id} value={plan.id}>{plan.title}</option>)}
            </Select>
          </label>
          <label className="grid gap-1 text-xs">{t("Test plan name")}
            <Input value={title} onChange={(event) => { setTitle(event.target.value); clear(); }} />
          </label>
        </div>
        {plans.isError && <p role="alert" className="text-sm text-danger">{t("The saved test plans could not be loaded.")}</p>}
        <label className="grid gap-1 text-xs">{t("Saved object draft")}
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
        {objects.isError && <p role="alert" className="text-sm text-danger">{t("The candidate test could not be run.")}</p>}
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
            <label className="grid gap-1 text-xs">{t("Action")}<Select value={step.action} onChange={(event) => update(index, { action: event.target.value, type: `build.${object?.name}` })}>
              {!actions.some((action) => `build.${object?.name}.${action.name}` === step.action) && <option value={step.action}>{step.action}</option>}
              {actions.map((action) => <option key={action.name} value={`build.${object?.name}.${action.name}`}>{action.title}</option>)}
            </Select></label>
          </div>
          <label className="grid gap-1 text-xs">{t("Expected outcome")}
            <Select value={step.expect} onChange={(event) => update(index, { expect: event.target.value as Step["expect"] })}>
              <option value="accepted">{t("Accepted")}</option><option value="refused">{t("Refused")}</option>
            </Select>
          </label>
          <label className="grid gap-1 text-xs">{t("Test inputs (JSON)")}
            <Textarea rows={3} value={step.payload} onChange={(event) => update(index, { payload: event.target.value })} className="font-mono" />
          </label>
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
      {result.recovered && <p role="status" className="text-sm">{t("Test state recovered exactly. Sample records stayed isolated.")}</p>}
      {result.steps.map((step, index) => <div key={index} className="grid gap-1 border-t border-border pt-2">
        <p className="text-sm font-medium">{t("Test step {n}", { n: index + 1 })} · {step.accepted ? t("Accepted") : t("Refused")}</p>
        {step.matched !== undefined && <p className="text-xs">{step.matched ? t("Expected outcome matched") : t("Expected outcome did not match")}</p>}
        {step.refusal && <p className="text-sm text-danger">{step.refusal}</p>}
        {step.changes.map((change) => <div key={`${change.type}/${change.id}`} className="min-w-0 text-xs">
          <p className="font-mono">{change.type}/{change.id}</p>
          <pre className="overflow-x-auto rounded bg-muted/10 p-2">{JSON.stringify(change.record, null, 2)}</pre>
        </div>)}
      </div>)}
    </Card>}
  </div>;
}
