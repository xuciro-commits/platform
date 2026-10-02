// Builder release workbench: saved candidates come from committed bytes.
// Evaluation and activation retain the host's existing gates.
import { useHost, useReadQuery, useRecordInventory } from "@platform/app";
import { Button, Card, PageHeader, Select, StatusTag, t } from "@platform/ui";
import { apiErrorMessage, type Api } from "@platform/kernel";
import { useState } from "react";

export type ReleaseKind = "object" | "page" | "app" | "flow" | "link-type" | "property-type" | "query" | "function" | "compute";
type Record = { id: string; title: string; name: string; state: string };
type EvaluationPlan = { id: string; title: string; function?: string; evaluation?: unknown[] };
type EvaluationReport = { id: string; candidate: string; function: string; state: string; quality: number; costUsd: number; costComplete: boolean; peakLatencyMillis: number; attempts: unknown[] };
const kinds: { kind: ReleaseKind; type: string; label: string }[] = [
  { kind: "object", type: "build.object", label: "Objects" },
  { kind: "page", type: "build.page", label: "Pages" },
  { kind: "app", type: "build.app", label: "Applications" },
  { kind: "flow", type: "build.process", label: "Workflows" },
  {kind:"link-type",type:"build.linktype",label:"Relationships"},
  {kind:"property-type",type:"build.propertytype",label:"Shared properties"},
  { kind: "query", type: "build.query", label: "Queries" },
  { kind: "function", type: "build.function", label: "AI functions" },
  { kind: "compute", type: "build.code", label: "Code functions" },
];

export function ReleaseReview({ initialKind = "object", initialID = "", embedded = false }: { initialKind?: ReleaseKind; initialID?: string; embedded?: boolean } = {}) {
  const { client, role } = useHost();
  const [kind, setKind] = useState<ReleaseKind>(initialKind);
  const [id, setId] = useState(initialID);
  const [review, setReview] = useState<Api.ReleasePreview>();
  const [candidateKey, setCandidateKey] = useState("");
  const [savedID, setSavedID] = useState("");
  const [offset, setOffset] = useState(0);
  const [savedReview, setSavedReview] = useState(false);
  const [runningMatches, setRunningMatches] = useState<boolean>();
  const [runningDiagnostic, setRunningDiagnostic] = useState("");
  const [canActivate, setCanActivate] = useState(false);
  const [activationDiagnostic, setActivationDiagnostic] = useState("");
  const [planID, setPlanID] = useState("");
  const [reportID, setReportID] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const inventory = useReadQuery<Api.ReleasePage>(`/v1/releases/candidates?offset=${offset}&limit=20`);
  const activeID = inventory.data?.activeId ?? "";
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
  const loadSaved = async (candidateID: string) => {
    if (!candidateID) return;
    setBusy(true); setError(""); setReview(undefined); setPlanID(""); setReportID("");
    try {
      const result = await client.call<Api.SavedReleaseReview>("GET", `/v1/releases/candidates/${encodeURIComponent(candidateID)}`);
      if (!result.ok) setError(apiErrorMessage(result.body) ?? t("Saved releases could not be loaded."));
      else {
        setReview(result.body.preview); setSavedID(candidateID); setSavedReview(true);
        setRunningMatches(result.body.runningMatches); setRunningDiagnostic(result.body.runningDiagnostic ?? "");
        setCanActivate(result.body.canActivate); setActivationDiagnostic(result.body.activationDiagnostic ?? "");
        await reports.refetch();
      }
    } catch { setError(t("Saved releases could not be loaded.")); }
    finally { setBusy(false); }
  };
  const inspect = async () => {
    setBusy(true);
    setError("");
    setReview(undefined);
    setSavedID("");
    setSavedReview(false); setRunningMatches(undefined); setRunningDiagnostic("");
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
      else { setSavedID(result.body.id); await inventory.refetch(); await loadSaved(result.body.id); }
    } catch {
      setError(t("Candidate could not be saved."));
    } finally {
      setBusy(false);
    }
  };
  // The host stages the saved closure and commits installation with its pointer.
  const activate = async () => {
    if (!savedID) return;
    setBusy(true);
    setError("");
    try {
      const result = await client.call<Api.ReleaseActive>("POST", "/v1/releases/active",
        { candidateId: savedID, key: crypto.randomUUID() } satisfies Api.ReleaseActivateRequest);
      if (!result.ok) setError(apiErrorMessage(result.body) ?? t("Release could not be activated."));
      else { await inventory.refetch(); await loadSaved(result.body.id); }
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
    <Card className="grid gap-3 p-3" aria-label={t("Saved releases")}>
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h2 className="text-sm font-semibold">{t("Saved releases")}</h2>
        <Button size="sm" disabled={busy} onClick={async () => { await inventory.refetch(); if (savedReview && savedID) await loadSaved(savedID); }}>{t("Refresh releases")}</Button>
      </div>
      <p className="break-all text-xs text-muted">{activeID ? <>{t("Current active release")}: <code>{activeID}</code></> : t("No active release yet.")}</p>
      {inventory.isError ? <p role="alert" className="text-sm text-danger">{t("Saved releases could not be loaded.")}</p> : <label className="grid gap-1 text-xs">{t("Saved candidate")}
        <Select disabled={busy || inventory.isLoading} value={savedReview ? savedID : ""} onChange={(event) => void loadSaved(event.target.value)}>
          <option value="">{t("Choose a saved candidate")}</option>
          {inventory.data?.candidates.map((candidate) => <option key={candidate.id} value={candidate.id}>{candidate.title} · {candidate.assets} {t("assets")} · {candidate.id.slice(-8)}{candidate.id === activeID ? ` · ${t("Active")}` : ""}</option>)}
          {savedReview && !inventory.data?.candidates.some((candidate) => candidate.id === savedID) && <option value={savedID}>{savedID}</option>}
        </Select>
      </label>}
      {inventory.data && <div className="flex flex-wrap items-center gap-2 text-xs text-muted">
        <span>{t("Showing {shown} of {total} candidates", { shown: inventory.data.candidates.length, total: inventory.data.total })}</span>
        <Button size="sm" disabled={busy || offset === 0} onClick={() => setOffset(Math.max(0, offset - 20))}>{t("Previous")}</Button>
        <Button size="sm" disabled={busy || offset + 20 >= inventory.data.total} onClick={() => setOffset(offset + 20)}>{t("Next")}</Button>
        {activeID && <Button size="sm" disabled={busy} onClick={() => void loadSaved(activeID)}>{t("Review active release")}</Button>}
      </div>}
    </Card>
    <Card className="grid gap-3 p-3">
      {!embedded && <label className="grid gap-1 text-xs">{t("Definition kind")}
        <Select value={kind} onChange={(event) => { setKind(event.target.value as ReleaseKind); setId(""); setReview(undefined); setSavedID(""); setSavedReview(false); setPlanID(""); setReportID(""); setError(""); }}>
          {kinds.map((item) => <option key={item.kind} value={item.kind}>{t(item.label)}</option>)}
        </Select>
      </label>}
      {!embedded && <label className="grid gap-1 text-xs">{t("Saved draft")}
        <Select value={id} onChange={(event) => { setId(event.target.value); setReview(undefined); setSavedID(""); setSavedReview(false); setPlanID(""); setReportID(""); setError(""); }}>
          <option value="">{t("Choose a saved draft")}</option>
          {records.map((record) => <option key={record.id} value={record.id}>{record.title || record.name} · {record.state}</option>)}
        </Select>
      </label>}
      {query.isError && <p role="alert" className="text-sm text-danger">{t("Release review could not be loaded.")}</p>}
      <Button disabled={!id || busy} onClick={inspect}>{busy ? t("Checking…") : t("Check draft and dependencies")}</Button>
    </Card>
    {error && <Card className="p-3 text-sm text-danger" role="alert">{error}</Card>}
    {review && <Card className="grid gap-3 p-3" aria-live="polite">
      <div className="flex flex-wrap items-center gap-2"><span className="text-sm font-semibold">{savedReview ? t("Saved candidate review") : review.diagnostic ? t("Candidate rejected") : t("Candidate ready for review")}</span>
        {savedReview && <StatusTag status={activeID === savedID ? "active" : "saved"} registry={{ active: { label: t("Active"), tone: "success" }, saved: { label: t("Saved"), tone: "info" } }} />}
      </div>
      {review.currentId && <p className="break-all text-xs">{t("Installed candidate")}: <code>{review.currentId}</code></p>}
      {review.candidateId && <p className="break-all text-xs">{savedReview ? t("Saved candidate") : t("Draft candidate")}: <code>{review.candidateId}</code></p>}
      {savedReview && <p className="text-sm" role="status">{runningDiagnostic ? t("Current definitions could not be compared with this candidate.") : runningMatches ? t("Running definitions match this saved release.") : t("Running definitions differ from this saved release.")}</p>}
      {savedReview && !runningMatches && canActivate && <p className="text-sm">{t("Activation installs the candidate's included objects, pages, applications, workflows, AI functions and code functions together.")}</p>}
      {savedReview && activationDiagnostic && <p role="alert" className="text-sm text-warning">{activationDiagnostic}</p>}
      {savedReview && runningDiagnostic && <p className="break-words text-xs text-warning">{runningDiagnostic}</p>}
      {review.diagnostic && <p role="alert" className="text-sm text-danger">{review.diagnostic}</p>}
      {!review.diagnostic && !(savedReview && runningDiagnostic) && <div className="grid gap-3 sm:grid-cols-3">
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
        {!savedReview && <Button disabled={busy || Boolean(savedID)} onClick={save}>{busy ? t("Saving…") : t("Save immutable candidate")}</Button>}
        {savedID && activeID !== savedID && <p className="break-all text-sm" role="status">{t("Candidate saved; not active for operators.")} <code>{savedID}</code></p>}
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
        {savedID && <Button variant="default" disabled={busy || activeID === savedID || !evaluated || savedReview && !canActivate} onClick={activate}>{t("Activate release")}</Button>}
        {activeID === savedID && savedID && <p className="break-all text-sm" role="status">{savedReview && !runningMatches ? t("Active release differs from running definitions.") : t("Release active for operators.")} <code>{activeID}</code></p>}
      </div>}
    </Card>}
  </div>;
}
