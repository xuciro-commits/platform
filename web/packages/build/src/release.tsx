// A builder-only review and immutable candidate save. Saving does not install
// a definition, activate a release or run a preview sandbox.
import { useHost, useRecordInventory } from "@platform/app";
import { Button, Card, PageHeader, Select, t } from "@platform/ui";
import { apiErrorMessage, type Api } from "@platform/kernel";
import { useState } from "react";

export type ReleaseKind = "object" | "page" | "app" | "flow" | "function";
type Record = { id: string; title: string; name: string; state: string };
type EvaluationPlan = { id: string; title: string; function?: string; evaluation?: unknown[] };
type EvaluationReport = { id: string; candidate: string; function: string; state: string; quality: number; costUsd: number; costComplete: boolean; peakLatencyMillis: number; attempts: unknown[] };
const kinds: { kind: ReleaseKind; type: string; label: string }[] = [
  { kind: "object", type: "build.object", label: "Objects" },
  { kind: "page", type: "build.page", label: "Pages" },
  { kind: "app", type: "build.app", label: "Applications" },
  { kind: "flow", type: "build.process", label: "Workflows" },
  { kind: "function", type: "build.function", label: "AI functions" },
];

export function ReleaseReview({ initialKind = "object", initialID = "", embedded = false }: { initialKind?: ReleaseKind; initialID?: string; embedded?: boolean } = {}) {
  const { client, role } = useHost();
  const [kind, setKind] = useState<ReleaseKind>(initialKind);
  const [id, setId] = useState(initialID);
  const [review, setReview] = useState<Api.ReleasePreview>();
  const [candidateKey, setCandidateKey] = useState("");
  const [savedID, setSavedID] = useState("");
  const [activeID, setActiveID] = useState("");
  const [planID, setPlanID] = useState("");
  const [reportID, setReportID] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const selected = kinds.find((item) => item.kind === kind)!;
  const query = useRecordInventory<Record>(selected.type);
  const functions = useRecordInventory<Record>("build.function");
  const plans = useRecordInventory<EvaluationPlan>("build.testplan");
  const reports = useRecordInventory<EvaluationReport>("build.evaluation");
  const records = query.data?.records ?? [];
  const functionNames = (review?.included ?? []).filter((ref) => ref.app === "build" && ref.kind === "function").map((ref) => ref.name);
  const eligiblePlans = (plans.data?.records ?? []).filter((plan) => plan.evaluation?.length && functions.data?.records.some((fn) => fn.id === plan.function && functionNames.includes(fn.name)));
  const report = reports.data?.records.find((item) => item.id === reportID);
  const passedFunctions = (reports.data?.records ?? []).filter((item) => item.candidate === savedID && item.state === "passed").map((item) => item.function);
  const evaluated = functionNames.every((name) => passedFunctions.includes(name));
  if (role("build") !== "builder") {
    return <PageHeader title={t("Release review")} description={t("Only a builder can review complete release definitions.")} />;
  }
  const inspect = async () => {
    setBusy(true);
    setError("");
    setReview(undefined);
    setSavedID("");
    setActiveID("");
    setPlanID(""); setReportID("");
    try {
      const result = await client.call<Api.ReleasePreview>("POST", "/v1/releases/preview", { kind, id });
      if (!result.ok) setError(apiErrorMessage(result.body) ?? t("Release review could not be loaded."));
      else {
        setReview(result.body);
        setCandidateKey(crypto.randomUUID());
      }
    } catch {
      setError(t("Release review could not be loaded."));
    } finally {
      setBusy(false);
    }
  };
  const evaluate = async () => {
    if (!savedID || !planID) return;
    setBusy(true); setError(""); setReportID("");
    try {
      const result = await client.call<Api.ReleaseEvaluationStarted>("POST", "/v1/releases/evaluations",
        { candidateId: savedID, planId: planID, key: crypto.randomUUID() } satisfies Api.ReleaseEvaluationRequest);
      if (!result.ok) setError(apiErrorMessage(result.body) ?? t("Function evaluation could not be started."));
      else { setReportID(result.body.id); await reports.refetch(); }
    } catch { setError(t("Function evaluation could not be started.")); }
    finally { setBusy(false); }
  };
  const save = async () => {
    if (!review?.candidateId || !candidateKey) return;
    setBusy(true);
    setError("");
    try {
      const result = await client.call<Api.ReleaseSaved>("POST", "/v1/releases/candidates",
        { kind, id, candidateId: review.candidateId, key: candidateKey } satisfies Api.ReleaseSaveRequest);
      if (!result.ok) setError(apiErrorMessage(result.body) ?? t("Candidate could not be saved."));
      else setSavedID(result.body.id);
    } catch {
      setError(t("Candidate could not be saved."));
    } finally {
      setBusy(false);
    }
  };
  // Activation succeeds only when operators already run exactly these bytes.
  const activate = async () => {
    if (!savedID) return;
    setBusy(true);
    setError("");
    try {
      const result = await client.call<Api.ReleaseActive>("POST", "/v1/releases/active",
        { candidateId: savedID, key: crypto.randomUUID() } satisfies Api.ReleaseActivateRequest);
      if (!result.ok) setError(apiErrorMessage(result.body) ?? t("Release could not be activated."));
      else setActiveID(result.body.id);
    } catch {
      setError(t("Release could not be activated."));
    } finally {
      setBusy(false);
    }
  };
  const changed = (label: string, refs?: Api.AssetRef[]) => (
    <div>
      <h3 className="text-xs font-semibold">{t(label)} · {refs?.length ?? 0}</h3>
      <ul className="mt-1 grid gap-1 font-mono text-xs">
        {(refs ?? []).map((ref) => <li key={`${ref.app}/${ref.kind}/${ref.name}`}>{ref.app}/{ref.kind}/{ref.name}</li>)}
      </ul>
    </div>
  );
  return <div className="grid gap-3">
    {!embedded && <PageHeader title={t("Release review")} description={t("Compare a saved draft, then save its exact candidate bytes. Saving does not activate it for operators.")} />}
    <Card className="grid gap-3 p-3">
      {!embedded && <label className="grid gap-1 text-xs">{t("Definition kind")}
        <Select value={kind} onChange={(event) => { setKind(event.target.value as ReleaseKind); setId(""); setReview(undefined); setSavedID(""); setActiveID(""); setPlanID(""); setReportID(""); setError(""); }}>
          {kinds.map((item) => <option key={item.kind} value={item.kind}>{t(item.label)}</option>)}
        </Select>
      </label>}
      {!embedded && <label className="grid gap-1 text-xs">{t("Saved draft")}
        <Select value={id} onChange={(event) => { setId(event.target.value); setReview(undefined); setSavedID(""); setActiveID(""); setPlanID(""); setReportID(""); setError(""); }}>
          <option value="">{t("Choose a saved draft")}</option>
          {records.map((record) => <option key={record.id} value={record.id}>{record.title || record.name} · {record.state}</option>)}
        </Select>
      </label>}
      {query.isError && <p role="alert" className="text-sm text-danger">{t("Release review could not be loaded.")}</p>}
      <Button disabled={!id || busy} onClick={inspect}>{busy ? t("Checking…") : t("Check draft and dependencies")}</Button>
    </Card>
    {error && <Card className="p-3 text-sm text-danger" role="alert">{error}</Card>}
    {review && <Card className="grid gap-3 p-3" aria-live="polite">
      <div className="text-sm font-semibold">{review.diagnostic ? t("Candidate rejected") : t("Candidate ready for review")}</div>
      {review.currentId && <p className="break-all text-xs">{t("Installed candidate")}: <code>{review.currentId}</code></p>}
      {review.candidateId && <p className="break-all text-xs">{t("Draft candidate")}: <code>{review.candidateId}</code></p>}
      {review.diagnostic && <p role="alert" className="text-sm text-danger">{review.diagnostic}</p>}
      {!review.diagnostic && <div className="grid gap-3 sm:grid-cols-3">
        {changed("Added", review.added)}
        {changed("Changed", review.changed)}
        {changed("Removed", review.removed)}
      </div>}
      {!review.diagnostic && <div>
        <h3 className="text-xs font-semibold">{t("Included assets")} · {review.included.length}</h3>
        <ul className="mt-1 grid max-h-52 gap-1 overflow-y-auto rounded border border-border p-2 font-mono text-xs sm:grid-cols-2">
          {review.included.map((ref) => <li className="break-all" key={`${ref.app}/${ref.kind}/${ref.name}`}>{ref.app}/{ref.kind}/{ref.name}</li>)}
        </ul>
      </div>}
      {!review.diagnostic && review.candidateId && <div className="grid gap-2">
        <Button disabled={busy || Boolean(savedID)} onClick={save}>{busy ? t("Saving…") : t("Save immutable candidate")}</Button>
        {savedID && !activeID && <p className="break-all text-sm" role="status">{t("Candidate saved; not active for operators.")} <code>{savedID}</code></p>}
        {savedID && functionNames.length > 0 && <div className="grid gap-2 rounded border border-border p-3">
          <div className="text-sm font-semibold">{t("Measured function evaluation")}</div>
          <p className="text-xs text-muted">{t("A saved synthetic plan runs real model calls. A passing report for each included function is required before activation.")}</p>
          <label className="grid gap-1 text-xs">{t("Evaluation plan")}
            <Select value={planID} onChange={(event) => { setPlanID(event.target.value); setReportID(""); }}>
              <option value="">{t("Choose an evaluation plan")}</option>
              {eligiblePlans.map((plan) => <option key={plan.id} value={plan.id}>{plan.title}</option>)}
            </Select>
          </label>
          <div className="flex flex-wrap gap-2">
            <Button disabled={!planID || busy} onClick={evaluate}>{busy ? t("Starting evaluation…") : t("Run measured evaluation")}</Button>
            <Button disabled={busy} onClick={() => { void reports.refetch(); }}>{t("Refresh evaluation reports")}</Button>
          </div>
          {reportID && <p className="break-all text-xs">{t("Evaluation report")}: <code>{reportID}</code></p>}
          {report && <p className="text-sm" role="status">{t("Report state")}: {t(report.state)} · {t("Quality")}: {Math.round(report.quality * 100)}% · {t("Reported USD cost")}: {report.costComplete ? report.costUsd : t("Unknown")} · {t("Peak latency (ms)")}: {report.peakLatencyMillis} · {t("Calls")}: {report.attempts.length}</p>}
          {!eligiblePlans.length && <p className="text-xs text-warning">{t("Save a function test plan with evaluation cases before running the release evaluation.")}</p>}
        </div>}
        {savedID && <Button variant="default" disabled={busy || activeID === savedID || !evaluated} onClick={activate}>{t("Activate release")}</Button>}
        {activeID && <p className="break-all text-sm" role="status">{t("Release active for operators.")} <code>{activeID}</code></p>}
      </div>}
    </Card>}
  </div>;
}
