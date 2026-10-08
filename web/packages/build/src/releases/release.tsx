// Builder release workbench: saved candidates come from committed bytes.
// Evaluation and activation retain the host's existing gates.
import { useHost, useReadQuery, useRecordInventory } from "@platform/app";
import { Button, Card, Checkbox, Disclosure, PageHeader, Select, StatusTag, t, useWorkspace } from "@platform/ui";
import { apiErrorMessage, type Api } from "@platform/kernel";
import { useApplicationScope } from "../projects/application-scope";
import { useState } from "react";

// One selected saved draft in a joint candidate (ADR-0048 D1). The selection is
// what the builder checked; the server recomputes the candidate from it.
type JointChoice = { kind: ReleaseKind; id: string };
const jointKey = (choice: JointChoice) => `${choice.kind}/${choice.id}`;
const merged = (current: JointChoice[], incoming: JointChoice[]): JointChoice[] => {
  const seen = new Set(current.map(jointKey));
  const out = [...current];
  for (const choice of incoming) {
    if (!choice.id || seen.has(jointKey(choice))) continue;
    seen.add(jointKey(choice));
    out.push(choice);
  }
  return out;
};

export type ReleaseKind = "object" | "page" | "app" | "flow" | "link-type" | "property-type" | "query" | "function" | "compute";
/** A route carries the already-known candidate inputs as "kind:id,kind:id"
 * (ADR-0048 D6: the import's saved draft objects are the application's
 * dependencies, so its review opens with them selected). */
export function releaseDraftsParam(value: string | undefined, kinds: readonly string[]): JointChoice[] {
  if (!value) return [];
  const out: JointChoice[] = [];
  for (const item of value.split(",")) {
    const [kind, id] = item.split(":") as [ReleaseKind, string];
    if (kinds.includes(kind) && id && !out.some((choice) => jointKey(choice) === `${kind}/${id}`)) out.push({ kind, id });
  }
  return out;
}
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
export const releaseKinds: ReleaseKind[] = kinds.map((item) => item.kind);

export function ReleaseReview({ initialKind = "object", initialID = "", initialDrafts = [], embedded = false }: { initialKind?: ReleaseKind; initialID?: string; initialDrafts?: JointChoice[]; embedded?: boolean } = {}) {
  const { client, role } = useHost();
  const builder = role("build") === "builder";
  const mayRelease = builder || role("build") === "publisher";
  const scope = useApplicationScope();
  const { open } = useWorkspace();
  const [kind, setKind] = useState<ReleaseKind>(initialKind);
  const [id, setId] = useState(initialID);
  const [review, setReview] = useState<Api.ReleasePreview>();
  const [assets, setAssets] = useState<Api.ReleaseAsset[]>([]);
  const [candidateKey, setCandidateKey] = useState("");
  const [savedID, setSavedID] = useState("");
  const [offset, setOffset] = useState(0);
  const [savedReview, setSavedReview] = useState(false);
  const [runningMatches, setRunningMatches] = useState<boolean>();
  const [runningDiagnostic, setRunningDiagnostic] = useState("");
  const [canActivate, setCanActivate] = useState(false);
  const [activationDiagnostic, setActivationDiagnostic] = useState("");
  const [upgradePlan, setUpgradePlan] = useState<Api.ReleaseUpgradePlan>();
  const [origin, setOrigin] = useState<{ from: string; at?: string }>();
  const [confirmedUpgrade, setConfirmedUpgrade] = useState("");
  const [planID, setPlanID] = useState("");
  const [reportID, setReportID] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  const [joint, setJoint] = useState<JointChoice[]>(initialDrafts);
  const [jointNote, setJointNote] = useState("");
  const inventory = useReadQuery<Api.ReleasePage>(`/v1/releases/candidates?offset=${offset}&limit=20`, undefined, mayRelease);
  const hostAdmin = useReadQuery<{ subject: string; tenants: string[] }>("/v1/host/me", undefined, mayRelease).isSuccess; // the host console answers only host administrators
  const activeID = inventory.data?.activeId ?? "";
  // What another environment promoted here and nobody activated yet: the
  // release holder's inbox, ahead of the full saved inventory.
  const promoted = (inventory.data?.candidates ?? []).filter((candidate) => candidate.from && candidate.id !== activeID);
  const selected = kinds.find((item) => item.kind === kind)!;
  const query = useRecordInventory<Record>(selected.type, 1000, builder);
  const functions = useRecordInventory<Record>("build.function", 1000, builder);
  const plans = useRecordInventory<EvaluationPlan>("build.testplan", 1000, builder);
  const applications = useRecordInventory<Record>("build.app", 1000, builder);
  const reports = useRecordInventory<EvaluationReport>("build.evaluation", 1000, mayRelease);
  const records = query.data?.records ?? [];
  const functionNames = (review?.included ?? []).filter((ref) => ref.app === "build" && ref.kind === "function").map((ref) => ref.name);
  const eligiblePlans = (plans.data?.records ?? []).filter((plan) => plan.evaluation?.length && functions.data?.records.some((fn) => fn.id === plan.function && functionNames.includes(fn.name)));
  const report = reports.data?.records.find((item) => item.id === reportID);
  // The application a release review was opened from is the natural root of a
  // joint selection (ADR-0048 D1): its pages come before its own draft.
  const scopedApplication = scope ? applications.data?.records.find((item) => item.id === scope) : undefined;
  const passedFunctions = (reports.data?.records ?? []).filter((item) => item.candidate === savedID && item.state === "passed").map((item) => item.function);
  const evaluated = functionNames.every((name) => passedFunctions.includes(name));
  if (!mayRelease) {
    return <PageHeader title={t("Release review")} description={t("A builder or publisher role is required to review releases.")} />;
  }
  const loadSaved = async (candidateID: string) => {
    if (!candidateID) return;
    setBusy(true); setError(""); setReview(undefined); setPlanID(""); setReportID("");
    try {
      const result = await client.call<Api.SavedReleaseReview>("GET", `/v1/releases/candidates/${encodeURIComponent(candidateID)}`);
      if (!result.ok) setError(apiErrorMessage(result.body) ?? t("Saved releases could not be loaded."));
      else {
        setReview(result.body.preview); setSavedID(candidateID); setSavedReview(true);
        setAssets(result.body.assets ?? []);
        setRunningMatches(result.body.runningMatches); setRunningDiagnostic(result.body.runningDiagnostic ?? "");
        setCanActivate(result.body.canActivate); setActivationDiagnostic(result.body.activationDiagnostic ?? "");
        setUpgradePlan(result.body.upgradePlan);
        setOrigin(result.body.from ? { from: result.body.from, at: result.body.promotedAt } : undefined);
        setConfirmedUpgrade(current => current === result.body.upgradePlan?.id ? current : "");
        await reports.refetch();
      }
    } catch { setError(t("Saved releases could not be loaded.")); }
    finally { setBusy(false); }
  };
  const inspect = async (drafts?: JointChoice[]) => {
    setBusy(true);
    setError("");
    setReview(undefined);
    setSavedID("");
    setSavedReview(false); setRunningMatches(undefined); setRunningDiagnostic(""); setOrigin(undefined);
    setPlanID(""); setReportID("");
    try {
      const result = await client.call<Api.ReleasePreview>("POST", "/v1/releases/preview",
        drafts?.length ? { drafts } satisfies Api.ReleasePreviewRequest : { kind, id } satisfies Api.ReleasePreviewRequest);
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
  // The graph is the server's knowledge, not the builder's: ask which saved
  // record drafts the chosen draft needs, then let the builder check them.
  const addReferenced = async (from: JointChoice) => {
    if (!from.id) return;
    setBusy(true);
    setJointNote("");
    setError("");
    try {
      const result = await client.call<Api.ReleaseDraftClosure>("POST", "/v1/releases/drafts/referenced", from satisfies Api.ReleaseDraftsRequest);
      if (!result.ok) setJointNote(apiErrorMessage(result.body) ?? t("Draft dependencies could not be read."));
      else {
        const found = result.body.drafts.map((draft) => ({ kind: draft.kind as ReleaseKind, id: draft.id }));
        setJoint((current) => merged(current, [from, ...found]));
        setJointNote(found.length ? t("Added {count} dependent drafts.", { count: found.length }) : t("Nothing to add: every dependency is installed or already selected."));
      }
    } catch { setJointNote(t("Draft dependencies could not be read.")); }
    finally { setBusy(false); }
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
      const selection = review.drafts ?? [];
      const result = await client.call<Api.ReleaseSaved>("POST", "/v1/releases/candidates",
        (selection.length
          ? { drafts: selection, candidateId: review.candidateId, key: candidateKey }
          : { kind, id, candidateId: review.candidateId, key: candidateKey }) satisfies Api.ReleaseSaveRequest);
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
        { candidateId: savedID, key: crypto.randomUUID(), ...(confirmedUpgrade && confirmedUpgrade === upgradePlan?.id ? { upgradeId: confirmedUpgrade } : {}) } satisfies Api.ReleaseActivateRequest);
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
    {!embedded && <PageHeader title={t("Release review")} description={t(builder ? "Compare a saved draft, then save its exact candidate bytes. Saving does not activate it for operators." : "Review sealed candidate definitions and activate a release. Definition editing belongs to builders.")}
      actions={builder && scope && <Button onClick={() => open({ view: "project", params: { id: scope } })}>{t("Back to project")}</Button>} />}
    <Card className="grid gap-3 p-3" aria-label={t("Saved releases")}>
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h2 className="text-sm font-semibold">{t("Saved releases")}</h2>
        <Button size="sm" disabled={busy} onClick={async () => { await inventory.refetch(); if (savedReview && savedID) await loadSaved(savedID); }}>{t("Refresh releases")}</Button>
      </div>
      <p className="break-all text-xs text-muted">{activeID ? <>{t("Current active release")}: <code>{activeID}</code></> : t("No active release yet.")}</p>
      {inventory.isError ? <p role="alert" className="text-sm text-danger">{t("Saved releases could not be loaded.")}</p> : <label className="grid gap-1 text-xs">{t("Saved candidate")}
        <Select aria-label={t("Saved candidate")} disabled={busy || inventory.isLoading} value={savedReview ? savedID : ""} onChange={(event) => void loadSaved(event.target.value)}>
          <option value="">{t("Choose a saved candidate")}</option>
          {inventory.data?.candidates.map((candidate) => <option key={candidate.id} value={candidate.id}>{candidate.title} · {candidate.assets} {t("assets")} · {candidate.id.slice(-8)}{candidate.id === activeID ? ` · ${t("Active")}` : ""}</option>)}
          {savedReview && !inventory.data?.candidates.some((candidate) => candidate.id === savedID) && <option value={savedID}>{savedID}</option>}
        </Select>
      </label>}
      {promoted.length > 0 && <div className="grid gap-2 rounded border border-border p-3" role="region" aria-label={t("Promoted into this environment")}>
        <div>
          <h3 className="text-sm font-semibold">{t("Promoted into this environment")} · {promoted.length}</h3>
          <p className="text-xs text-muted">{t("Candidates another environment promoted here and that still wait for review. Open one to check its upgrade plan and activate it.")}</p>
        </div>
        <ul className="grid gap-1">
          {promoted.map((candidate) => <li key={candidate.id} className="flex flex-wrap items-center justify-between gap-2 text-xs">
            <span className="break-all">{candidate.title} · {t("from {tenant}", { tenant: candidate.from! })}{candidate.promotedAt ? ` · ${new Date(candidate.promotedAt).toLocaleString()}` : ""} · <code>{candidate.id.slice(-8)}</code></span>
            <Button size="sm" disabled={busy} onClick={() => void loadSaved(candidate.id)}>{t("Review and activate")}</Button>
          </li>)}
        </ul>
      </div>}
      {inventory.data && <div className="flex flex-wrap items-center gap-2 text-xs text-muted">
        <span>{t("Showing {shown} of {total} candidates", { shown: inventory.data.candidates.length, total: inventory.data.total })}</span>
        <Button size="sm" disabled={busy || offset === 0} onClick={() => setOffset(Math.max(0, offset - 20))}>{t("Previous")}</Button>
        <Button size="sm" disabled={busy || offset + 20 >= inventory.data.total} onClick={() => setOffset(offset + 20)}>{t("Next")}</Button>
        {activeID && <Button size="sm" disabled={busy} onClick={() => void loadSaved(activeID)}>{t("Review active release")}</Button>}
      </div>}
    </Card>
    {builder && <><Card className="grid gap-3 p-3">
      {!embedded && <label className="grid gap-1 text-xs">{t("Definition kind")}
        <Select value={kind} onChange={(event) => { setKind(event.target.value as ReleaseKind); setId(""); setReview(undefined); setSavedID(""); setSavedReview(false); setPlanID(""); setReportID(""); setError(""); setJoint([]); setJointNote(""); }}>
          {kinds.map((item) => <option key={item.kind} value={item.kind}>{t(item.label)}</option>)}
        </Select>
      </label>}
      {!embedded && <label className="grid gap-1 text-xs">{t("Saved draft")}
        <Select value={id} onChange={(event) => { setId(event.target.value); setReview(undefined); setSavedID(""); setSavedReview(false); setPlanID(""); setReportID(""); setError(""); setJoint([]); setJointNote(""); }}>
          <option value="">{t("Choose a saved draft")}</option>
          {records.map((record) => <option key={record.id} value={record.id}>{record.title || record.name} · {record.state}</option>)}
        </Select>
      </label>}
      {query.isError && <p role="alert" className="text-sm text-danger">{t("Release review could not be loaded.")}</p>}
      <Button disabled={!id || busy} onClick={() => void inspect()}>{busy ? t("Checking…") : t("Check draft and dependencies")}</Button>
    </Card>
    <Card className="grid gap-3 p-3" aria-label={t("Deliver drafts together")}>
      <div>
        <h2 className="text-sm font-semibold">{t("Deliver drafts together")}</h2>
        <p className="text-xs text-muted">{t("An object, the page over it and the application holding the page can be delivered as one candidate; nothing has to be published first.")}</p>
      </div>
      <div className="flex flex-wrap gap-2">
        <Button size="sm" disabled={!id || busy} onClick={() => { setJoint((current) => merged(current, [{ kind, id }])); setJointNote(""); }}>{t("Add selected draft")}</Button>
        <Button size="sm" disabled={!id || busy} onClick={() => void addReferenced({ kind, id })}>{t("Add the drafts it depends on")}</Button>
        {scopedApplication && <Button size="sm" disabled={busy} onClick={() => void addReferenced({ kind: "app", id: scopedApplication.id })}>{t("Add the drafts of {title}", { title: scopedApplication.title || scopedApplication.name })}</Button>}
        <Button size="sm" disabled={!joint.length || busy} onClick={() => { setJoint([]); setJointNote(""); }}>{t("Clear selection")}</Button>
      </div>
      {joint.length ? <ul className="grid gap-1" aria-label={t("Joint selection")}>
        {joint.map((choice) => <li className="flex items-center justify-between gap-2 text-xs" key={jointKey(choice)}>
          <span>{t(kinds.find((item) => item.kind === choice.kind)?.label ?? choice.kind)} · <code>{choice.id}</code></span>
          <Button size="sm" onClick={() => setJoint((current) => current.filter((item) => jointKey(item) !== jointKey(choice)))}>{t("Remove")}</Button>
        </li>)}
      </ul> : <p className="text-xs text-muted">{t("No drafts selected yet.")}</p>}
      {jointNote && <p className="text-xs text-muted" role="status">{jointNote}</p>}
      <Button disabled={!joint.length || busy} onClick={() => void inspect(joint)}>{busy ? t("Checking…") : t("Check joint candidate")}</Button>
    </Card>
    </>}
    {error && <Card className="p-3 text-sm text-danger" role="alert">{error}</Card>}
    {review && <Card className="grid gap-3 p-3" aria-live="polite">
      <div className="flex flex-wrap items-center gap-2"><span className="text-sm font-semibold">{savedReview ? t("Saved candidate review") : review.diagnostic ? t("Candidate rejected") : (review.drafts?.length ?? 0) > 1 ? t("Joint candidate ready for review") : t("Candidate ready for review")}</span>
        {savedReview && <StatusTag status={activeID === savedID ? "active" : "saved"} registry={{ active: { label: t("Active"), tone: "success" }, saved: { label: t("Saved"), tone: "info" } }} />}
        {savedReview && origin && <StatusTag status="promoted" registry={{ promoted: { label: t("Promoted from {tenant}", { tenant: origin.from }), tone: "info" } }} />}
      </div>
      {savedReview && origin && activeID !== savedID && <p className="text-xs text-muted" role="status">{t("This candidate was promoted from {tenant}{when}. It is not active here: review the differences and the storage upgrade plan below, then activate it.", { tenant: origin.from, when: origin.at ? ` · ${new Date(origin.at).toLocaleString()}` : "" })}</p>}
      {review.currentId && <p className="break-all text-xs">{t("Installed candidate")}: <code>{review.currentId}</code></p>}
      {review.candidateId && <p className="break-all text-xs">{savedReview ? t("Saved candidate") : t("Draft candidate")}: <code>{review.candidateId}</code></p>}
      {savedReview && hostAdmin && <div className="flex flex-wrap items-center gap-2"><Button size="sm" onClick={() => open({ view: "host-promotions", params: { from: client.connection.tenant, candidate: savedID } })}>{t("Promote to another environment…")}</Button><span className="text-xs text-muted">{t("Development → test → production: the sealed bytes of this candidate move into another tenant through the host console.")}</span></div>}
      {savedReview && <Disclosure className="rounded border border-border p-3" summary={<span className="text-sm font-semibold">{t("Sealed definitions")} · {assets.length}</span>}>
        <p className="my-2 text-xs text-muted">{t("These definitions come from this immutable candidate, including its original versions and dependencies.")}</p>
        {assets.map(asset => <Disclosure key={`${asset.ref.app}/${asset.ref.kind}/${asset.ref.name}`} className="my-2" summary={<span className="font-mono text-xs">{asset.ref.app}/{asset.ref.kind}/{asset.ref.name}</span>}><pre className="mt-2 max-h-72 overflow-auto whitespace-pre-wrap break-all text-xs">{JSON.stringify(asset, null, 2)}</pre></Disclosure>)}
      </Disclosure>}
      {savedReview && <p className="text-sm" role="status">{runningDiagnostic ? t("Current definitions could not be compared with this candidate.") : runningMatches ? t("Running definitions match this saved release.") : t("Running definitions differ from this saved release.")}</p>}
      {savedReview && !runningMatches && canActivate && <p className="text-sm">{t("Activation installs the candidate's included objects, pages, applications, workflows, AI functions and code functions together.")}</p>}
      {savedReview && activationDiagnostic && <p role="alert" className="text-sm text-warning">{activationDiagnostic}</p>}
      {savedReview && upgradePlan && <Card role="group" className="grid gap-2 p-3" aria-label={t("Storage upgrade plan")}>
        <h3 className="text-sm font-semibold">{t("Storage upgrade plan")}</h3>
        <p className="text-xs text-muted">{t("Add optional fields without changing existing values or record history. Required fields, type changes and removals are refused.")}</p>
        <ul className="grid gap-1 text-xs">{upgradePlan.additions.map(addition => <li key={addition.type}>{addition.type} · {addition.field} · {t(addition.kind)} · {t("{count} existing records", { count: addition.records })}</li>)}</ul>
        <Checkbox checked={confirmedUpgrade === upgradePlan.id} onChange={checked => setConfirmedUpgrade(checked ? upgradePlan.id : "")}>{t("Confirm this optional field upgrade plan")}</Checkbox>
      </Card>}
      {savedReview && runningDiagnostic && <p className="break-words text-xs text-warning">{runningDiagnostic}</p>}
      {review.diagnostic && <p role="alert" className="text-sm text-danger">{review.diagnostic}</p>}
      {!review.diagnostic && !(savedReview && runningDiagnostic) && <div className="grid gap-3 sm:grid-cols-3">
        {changed("Added", review.added)}
        {changed("Changed", review.changed)}
        {changed("Removed", review.removed)}
      </div>}
      {(review.drafts?.length ?? 0) > 0 && <div>
        <h3 className="text-xs font-semibold">{t("Delivered together")} · {review.drafts!.length}</h3>
        <ul className="mt-1 grid gap-1 text-xs" aria-label={t("Draft provenance")}>
          {review.drafts!.map((draft) => <li key={`${draft.kind}/${draft.id}`}>{t(kinds.find((item) => item.kind === draft.kind)?.label ?? draft.kind)} · <code>{draft.id}</code></li>)}
        </ul>
        <p className="mt-1 text-xs text-muted">{t("The saved candidate keeps these drafts as the assets it includes.")}</p>
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
        {builder && savedID && functionNames.length > 0 && <div className="grid gap-2 rounded border border-border p-3">
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
        {!builder && functionNames.length > 0 && !evaluated && <p role="status" className="text-sm text-warning">{t("Required function evaluations are missing. Ask a builder to complete them before activation.")}</p>}
        {savedID && <Button variant="default" disabled={busy || activeID === savedID || !evaluated || savedReview && !canActivate && !(upgradePlan && confirmedUpgrade === upgradePlan.id)} onClick={activate}>{t("Activate release")}</Button>}
        {activeID === savedID && savedID && <p className="break-all text-sm" role="status">{savedReview && !runningMatches ? t("Active release differs from running definitions.") : t("Release active for operators.")} <code>{activeID}</code></p>}
      </div>}
    </Card>}
  </div>;
}
