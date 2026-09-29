import { useHost, useReadQuery } from "@platform/app";
import { Button, Card, Input, PageHeader, Select, Textarea, t } from "@platform/ui";
import type { Api } from "@platform/kernel";
import { useState } from "react";

type ObjectDraft = { id: string; name: string; title: string; actions?: { name: string; title: string }[] };
type Step = { id: string; action: string; payload: string };

/** The host owns the test runtime. This editor only assembles its fixed inputs. */
export function CandidateTest() {
  const { client, role } = useHost();
  const objects = useReadQuery<{ records: ObjectDraft[] }>("/v1/records/build.object?limit=1000");
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
  const clear = () => { setResult(undefined); setError(""); };
  const update = (index: number, patch: Partial<Step>) => {
    clear();
    setSteps((old) => old.map((step, i) => i === index ? { ...step, ...patch } : step));
  };
  const run = async () => {
    if (!object) return;
    clear();
    let plan: Api.CandidateSimulationRequest;
    try {
      plan = { objectId: id, as: member, at: new Date(at).toISOString(), steps: steps.map((step) => ({
        type: `build.${object.name}`, id: step.id, action: `build.${object.name}.${step.action}`, payload: JSON.parse(step.payload),
      })) };
    } catch {
      setError(t("Use a valid fixed time and JSON inputs for every step."));
      return;
    }
    setBusy(true);
    try {
      const response = await client.call<Api.CandidateSimulation>("POST", "/v1/simulate/candidate", plan);
      if (response.ok) setResult(response.body);
      else setError((response.body as { error?: string }).error ?? t("The candidate test could not be run."));
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
        <label className="grid gap-1 text-xs">{t("Saved object draft")}
          <Select value={id} onChange={(event) => {
            setID(event.target.value); clear();
            const draft = objects.data?.records.find((record) => record.id === event.target.value);
            setSteps(draft ? [{ id: "TEST-1", action: "create", payload: "{}" },
              { id: "TEST-1", action: draft.actions?.[0]?.name ?? "edit", payload: "{}" }] : []);
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
            <label className="grid gap-1 text-xs">{t("Action")}<Select value={step.action} onChange={(event) => update(index, { action: event.target.value })}>
              {actions.map((action) => <option key={action.name} value={action.name}>{action.title}</option>)}
            </Select></label>
          </div>
          <label className="grid gap-1 text-xs">{t("Test inputs (JSON)")}
            <Textarea rows={3} value={step.payload} onChange={(event) => update(index, { payload: event.target.value })} className="font-mono" />
          </label>
        </Card>)}
        <div className="flex flex-wrap gap-2">
          <Button disabled={!object || steps.length >= 20} onClick={() => { clear(); setSteps((old) => [...old, { id: "TEST-1", action: object?.actions?.[0]?.name ?? "edit", payload: "{}" }]); }}>{t("Add a test step")}</Button>
          <Button variant="primary" disabled={!object || !steps.length} onClick={run}>{busy ? t("Running test…") : t("Run isolated test")}</Button>
        </div>
      </fieldset>
    </Card>
    {error && <Card role="alert" className="p-3 text-sm text-danger">{error}</Card>}
    {result && <Card className="grid gap-3 p-3" aria-live="polite">
      <p className="break-all text-xs">{t("Tested candidate")}: <code>{result.candidateId}</code></p>
      {result.recovered && <p role="status" className="text-sm">{t("Test state recovered exactly. Nothing was saved to production.")}</p>}
      {result.steps.map((step, index) => <div key={index} className="grid gap-1 border-t border-border pt-2">
        <p className="text-sm font-medium">{t("Test step {n}", { n: index + 1 })} · {step.accepted ? t("Accepted") : t("Refused")}</p>
        {step.refusal && <p className="text-sm text-danger">{step.refusal}</p>}
        {step.changes.map((change) => <div key={`${change.type}/${change.id}`} className="min-w-0 text-xs">
          <p className="font-mono">{change.type}/{change.id}</p>
          <pre className="overflow-x-auto rounded bg-muted/10 p-2">{JSON.stringify(change.record, null, 2)}</pre>
        </div>)}
      </div>)}
    </Card>}
  </div>;
}
