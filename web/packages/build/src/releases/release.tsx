// Builder release workbench: saved candidates come from committed bytes.
// Evaluation and activation retain the host's existing gates.
import { useHost, useReadQuery, useRecordInventory } from "@platform/app";
import { Button, Card, Checkbox, Disclosure, PageHeader, Select, StatusTag, t, useUnsavedChanges, useWorkspace } from "@platform/ui";
import { apiErrorMessage, type Api } from "@platform/kernel";
import { useApplicationScope } from "../projects/application-scope";
import { resourceKinds, type ResourceKind } from "../projects/resources";
import { diagnose } from "./diagnostics";
import { useRef, useState } from "react";
import { MigrationWorkbench } from "./migration";

// One selected saved draft in a joint candidate (ADR-0048 D1). The selection is
// what the builder checked; the server recomputes the candidate from it.
export type JointChoice = { kind: ReleaseKind; id: string };
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

export type ReleaseKind = Exclude<ResourceKind, "action" | "automation"> | "app";
/** Decode incoming candidate selections without silently discarding a bad
 * joint closure and delivering only its application root (ADR-0048 D6). */
export function releaseDraftsSelection(value: string | undefined, kinds: readonly string[]): { drafts: JointChoice[]; error?: string } {
  if (!value) return { drafts: [] };
  const invalid = () => ({ drafts: [], error: "The release selection is invalid or exceeds its budget. Reopen review from the application." });
  let selected: unknown;
  if (value.trimStart().startsWith("[")) {
    try { selected = JSON.parse(value); } catch { return invalid(); }
  } else {
    // Incoming-only compatibility. A record ID may itself contain colons.
    selected = value.split(",").map((item) => {
      const separator = item.indexOf(":");
      return { kind: item.slice(0, separator), id: separator < 0 ? "" : item.slice(separator + 1) };
    });
  }
  if (!Array.isArray(selected) || selected.length > 32) return invalid();
  const out: JointChoice[] = [];
  for (const item of selected) {
    if (!item || typeof item !== "object" || typeof item.kind !== "string" || typeof item.id !== "string"
      || !kinds.includes(item.kind) || !item.id) return invalid();
    if (!out.some((choice) => jointKey(choice) === `${item.kind}/${item.id}`)) out.push({ kind: item.kind as ReleaseKind, id: item.id });
  }
  return { drafts: out };
}
/** Source compatibility for consumers that only need the decoded list. */
export const releaseDraftsParam = (value: string | undefined, kinds: readonly string[]): JointChoice[] => releaseDraftsSelection(value, kinds).drafts;

/** New links carry lossless JSON, not punctuation-delimited record identities. */
export const releaseDraftsValue = (drafts: readonly JointChoice[]): string => JSON.stringify(drafts.map(({ kind, id }) => ({ kind, id })));

type ReleaseRequest = { kind: "save"; body: Api.ReleaseSaveRequest }
  | { kind: "evaluate"; body: Api.ReleaseEvaluationRequest }
  | { kind: "activate"; body: Api.ReleaseActivateRequest };

type Record = { id: string; title: string; name: string; state: string };
type EvaluationPlan = { id: string; title: string; function?: string; evaluation?: unknown[] };
type EvaluationReport = { id: string; candidate: string; function: string; state: string; quality: number; costUsd: number; costComplete: boolean; peakLatencyMillis: number; attempts: unknown[] };
const kinds: { kind: ReleaseKind; type: string; label: string }[] = [
  ...resourceKinds.filter((item) => item.kind !== "action" && item.kind !== "automation").map((item) => ({ kind: item.kind as ReleaseKind, type: item.type, label: item.plural })),
  { kind: "app", type: "build.app", label: "Applications" },
];
export const releaseKinds: ReleaseKind[] = kinds.map((item) => item.kind);

export function ReleaseReview({ initialKind = "object", initialID = "", initialDrafts = [], selectionError, embedded = false }: { initialKind?: ReleaseKind; initialID?: string; initialDrafts?: JointChoice[]; selectionError?: string; embedded?: boolean } = {}) {
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
  const [promotedTo, setPromotedTo] = useState<Api.PromotionTarget[]>([]);
  const wentTo = (targets?: Api.PromotionTarget[]) => (targets ?? []).map((target) => `${target.tenant} (${target.active ? t("active") : t("awaiting review")})`).join(", ");
  const [confirmedUpgrade, setConfirmedUpgrade] = useState("");
  const [planID, setPlanID] = useState("");
  const [reportID, setReportID] = useState("");
  const [error, setError] = useState(selectionError ? t(selectionError) : "");
  const [busy, setBusy] = useState(false);
  const [migrationLocked, setMigrationLocked] = useState(false);
  const lock = useRef(false);
  const [pending, setPending] = useState<ReleaseRequest>();
  const pendingRef = useRef(pending); pendingRef.current = pending;
  const rememberRequest = (request?: ReleaseRequest) => { pendingRef.current = request; setPending(request); };
  const { confirmDiscard } = useUnsavedChanges(!!pending, () => rememberRequest(), busy);
  const locked = busy || !!pending || migrationLocked;
  // One existing task view owns the captured HTTP request. No new outbox or
  // release authority: a retry sends exactly its original key and payload.
  const perform = async (job: () => Promise<void>) => {
    if (lock.current) return;
    lock.current = true; setBusy(true); setError("");
    try { await job(); }
    finally { lock.current = false; setBusy(false); }
  };
  const [joint, setJoint] = useState<JointChoice[]>(() => initialDrafts.length && initialID ? merged([{ kind: initialKind, id: initialID }], initialDrafts) : initialDrafts);
  const [jointNote, setJointNote] = useState("");
  const inventory = useReadQuery<Api.ReleasePage>(`/v1/releases/candidates?offset=${offset}&limit=20`, undefined, mayRelease);
  const hostAdmin = useReadQuery<{ subject: string; tenants: string[] }>("/v1/host/me", undefined, mayRelease).isSuccess; // the host console answers only host administrators
  const activeID = inventory.isError ? "" : inventory.data?.activeId ?? "";
  // What another environment promoted here and nobody activated yet: the
  // release holder's inbox, ahead of the full saved inventory.
  const promoted = (inventory.isError ? [] : inventory.data?.candidates ?? []).filter((candidate) => candidate.from && candidate.id !== activeID);
  const selected = kinds.find((item) => item.kind === kind)!;
  const query = useRecordInventory<Record>(selected.type, 1000, builder);
  const functions = useRecordInventory<Record>("build.function", 1000, builder);
  const plans = useRecordInventory<EvaluationPlan>("build.testplan", 1000, builder);
  const applications = useRecordInventory<Record>("build.app", 1000, builder);
  const reports = useRecordInventory<EvaluationReport>("build.evaluation", 1000, mayRelease);
  const records = query.isError ? [] : query.data?.records ?? [];
  const functionNames = (review?.included ?? []).filter((ref) => ref.app === "build" && ref.kind === "function").map((ref) => ref.name);
  const eligiblePlans = (plans.isError || functions.isError ? [] : plans.data?.records ?? []).filter((plan) => plan.evaluation?.length && functions.data?.records.some((fn) => fn.id === plan.function && functionNames.includes(fn.name)));
  const report = reports.isError ? undefined : reports.data?.records.find((item) => item.id === reportID);
  // The application a release review was opened from is the natural root of a
  // joint selection (ADR-0048 D1): its pages come before its own draft.
  const scopedApplication = scope ? applications.data?.records.find((item) => item.id === scope) : undefined;
  const passedFunctions = (reports.isError ? [] : reports.data?.records ?? []).filter((item) => item.candidate === savedID && item.state === "passed").map((item) => item.function);
  const evaluated = functionNames.every((name) => passedFunctions.includes(name));
  if (!mayRelease) {
    return <PageHeader title={t("Release review")} description={t("A builder or publisher role is required to review releases.")} />;
  }
  const clearReview = () => {
    setReview(undefined); setAssets([]); setSavedReview(false); setRunningMatches(undefined); setRunningDiagnostic("");
    setCanActivate(false); setActivationDiagnostic(""); setUpgradePlan(undefined); setOrigin(undefined); setPromotedTo([]);
    setPlanID(""); setReportID("");
  };
  const readSaved = async (candidateID: string) => {
    clearReview(); setSavedID(candidateID);
    try {
      const result = await client.call<Api.SavedReleaseReview>("GET", `/v1/releases/candidates/${encodeURIComponent(candidateID)}`);
      if (!result.ok || result.body.preview?.candidateId !== candidateID) {
        setError(apiErrorMessage(result.body) ?? t("Saved releases could not be loaded.")); return;
      }
      setReview(result.body.preview); setSavedReview(true); setAssets(result.body.assets ?? []);
      setRunningMatches(result.body.runningMatches); setRunningDiagnostic(result.body.runningDiagnostic ?? "");
      setCanActivate(result.body.canActivate); setActivationDiagnostic(result.body.activationDiagnostic ?? "");
      setUpgradePlan(result.body.upgradePlan);
      setOrigin(result.body.from ? { from: result.body.from, at: result.body.promotedAt } : undefined);
      setPromotedTo(result.body.promotedTo ?? []);
      setConfirmedUpgrade(current => current === result.body.upgradePlan?.id ? current : "");
      await reports.refetch();
    } catch { setError(t("Saved releases could not be loaded.")); }
  };
  const loadSaved = (candidateID: string) => {
    if (!candidateID || pendingRef.current) return;
    return perform(() => readSaved(candidateID));
  };
  const inspect = (drafts?: JointChoice[]) => {
    if (pendingRef.current || selectionError) return;
    const request: Api.ReleasePreviewRequest = drafts?.length ? { drafts: drafts.map(choice => ({ ...choice })) } : { kind, id };
    if (new TextEncoder().encode(JSON.stringify(request)).length > 4096) { setError(t("The release selection exceeds the original 4096-byte request budget. No drafts were dropped.")); return; }
    return perform(async () => {
      clearReview(); setSavedID(""); setConfirmedUpgrade(""); setCandidateKey("");
      try {
        const result = await client.call<Api.ReleasePreview>("POST", "/v1/releases/preview", request);
        if (!result.ok) setError(apiErrorMessage(result.body) ?? t("Release review could not be loaded."));
        else { setReview(result.body); setCandidateKey(crypto.randomUUID()); }
      } catch { setError(t("Release review could not be loaded.")); }
    });
  };
  // The original server owner computes dependencies. The view never invents
  // a closure from cached browser inventories or truncates it to fit a budget.
  const addReferenced = (from: JointChoice) => {
    if (!from.id || pendingRef.current) return;
    return perform(async () => {
      setJointNote("");
      try {
        const result = await client.call<Api.ReleaseDraftClosure>("POST", "/v1/releases/drafts/referenced", from satisfies Api.ReleaseDraftsRequest);
        if (!result.ok) setJointNote(apiErrorMessage(result.body) ?? t("Draft dependencies could not be read."));
        else {
          const found = result.body.drafts.map(draft => ({ kind: draft.kind as ReleaseKind, id: draft.id }));
          const selection = merged(joint, [from, ...found]);
          if (selection.length > 32) { setJointNote(t("A joint candidate accepts at most 32 drafts. Select a supported application closure.")); return; }
          setJoint(selection);
          setJointNote(found.length ? t("Added {count} dependent drafts.", { count: found.length }) : t("Nothing to add: every dependency is installed or already selected."));
        }
      } catch { setJointNote(t("Draft dependencies could not be read.")); }
    });
  };
  const send = (request: ReleaseRequest) => perform(async () => {
    const current = pendingRef.current;
    if (current && current !== request) return; // an unconfirmed request cannot be replaced
    if (new TextEncoder().encode(JSON.stringify(request.body)).length > 4096) { setError(t("The release selection exceeds the original 4096-byte request budget. No drafts were dropped.")); return; }
    rememberRequest(request);
    const path = request.kind === "save" ? "/v1/releases/candidates" : request.kind === "evaluate" ? "/v1/releases/evaluations" : "/v1/releases/active";
    let answer: { ok: boolean; status: number; body: { id: string } };
    try { answer = await client.call<{ id: string }>("POST", path, request.body); }
    catch { setError(t("The release request was not confirmed. Retry the original request.")); return; }
    if (!answer.ok || typeof answer.body?.id !== "string" || !answer.body.id
      || request.kind !== "evaluate" && answer.body.id !== request.body.candidateId) {
      // Candidate saving can stand even if sealing reports 409. Any unclear
      // answer retains the original request; a new key is not a safe retry.
      const message = apiErrorMessage(answer.body) ?? t("The release request was not confirmed. Retry the original request.");
      if (request.kind === "activate") await readSaved(request.body.candidateId);
      setError(message); return;
    }
    rememberRequest();
    try {
      if (request.kind === "evaluate") { setReportID(answer.body.id); await reports.refetch(); }
      else { setSavedID(answer.body.id); await inventory.refetch(); await readSaved(answer.body.id); }
    } catch { setError(t("The release request was accepted, but its refreshed review is unavailable.")); }

  });
  const evaluate = () => {
    if (pendingRef.current) return pendingRef.current.kind === "evaluate" ? send(pendingRef.current) : undefined;
    if (!savedReview || !savedID || !planID) return;
    return send({ kind: "evaluate", body: { candidateId: savedID, planId: planID, key: crypto.randomUUID() } });
  };
  const save = () => {
    if (pendingRef.current) return pendingRef.current.kind === "save" ? send(pendingRef.current) : undefined;
    if (!review?.candidateId || !candidateKey || savedReview) return;
    const selection = review.drafts ?? [];
    return send({ kind: "save", body: { ...(selection.length ? { drafts: selection.map(choice => ({ ...choice })) } : { kind, id }), candidateId: review.candidateId, key: candidateKey } });
  };
  const activate = () => {
    if (pendingRef.current) return pendingRef.current.kind === "activate" ? send(pendingRef.current) : undefined;
    if (!savedReview || !savedID || review?.candidateId !== savedID || !evaluated
      || (!canActivate && (!upgradePlan || confirmedUpgrade !== upgradePlan.id))) return;
    return send({ kind: "activate", body: { candidateId: savedID, key: crypto.randomUUID(), ...(confirmedUpgrade && confirmedUpgrade === upgradePlan?.id ? { upgradeId: confirmedUpgrade } : {}) } });
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
        <Button size="sm" disabled={locked} onClick={() => void perform(async () => { await inventory.refetch(); if (savedID) await readSaved(savedID); })}>{t("Refresh releases")}</Button>
      </div>
      <p className="break-all text-xs text-muted">{inventory.isError ? t("Active release is unavailable.") : inventory.isPending ? t("Loading…") : activeID ? <>{t("Current active release")}: <code>{activeID}</code></> : t("No active release yet.")}</p>
      {inventory.isError ? <p role="alert" className="text-sm text-danger">{t("Saved releases could not be loaded.")}</p> : <label className="grid gap-1 text-xs">{t("Saved candidate")}
        <Select aria-label={t("Saved candidate")} disabled={locked || inventory.isLoading} value={savedReview ? savedID : ""} onChange={(event) => void loadSaved(event.target.value)}>
          <option value="">{t("Choose a saved candidate")}</option>
          {inventory.data?.candidates.map((candidate) => <option key={candidate.id} value={candidate.id}>{candidate.title} · {candidate.assets} {t("assets")} · {candidate.id.slice(-8)}{candidate.id === activeID ? ` · ${t("Active")}` : ""}{candidate.promotedTo?.length ? ` · ${t("promoted to {targets}", { targets: wentTo(candidate.promotedTo) })}` : ""}</option>)}
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
            <Button size="sm" disabled={locked} onClick={() => void loadSaved(candidate.id)}>{t("Review and activate")}</Button>
          </li>)}
        </ul>
      </div>}
      {!inventory.isError && inventory.data && <div className="flex flex-wrap items-center gap-2 text-xs text-muted">
        <span>{t("Showing {shown} of {total} candidates", { shown: inventory.data.candidates.length, total: inventory.data.total })}</span>
        <Button size="sm" disabled={locked || offset === 0} onClick={() => setOffset(Math.max(0, offset - 20))}>{t("Previous")}</Button>
        <Button size="sm" disabled={locked || offset + 20 >= inventory.data.total} onClick={() => setOffset(offset + 20)}>{t("Next")}</Button>
        {activeID && <Button size="sm" disabled={locked} onClick={() => void loadSaved(activeID)}>{t("Review active release")}</Button>}
      </div>}
    </Card>
    {builder && <><Card className="grid gap-3 p-3">
      {!embedded && <label className="grid gap-1 text-xs">{t("Definition kind")}
        <Select disabled={locked} value={kind} onChange={(event) => { setKind(event.target.value as ReleaseKind); setId(""); setReview(undefined); setSavedID(""); setSavedReview(false); setPlanID(""); setReportID(""); setError(""); setJoint([]); setJointNote(""); }}>
          {kinds.map((item) => <option key={item.kind} value={item.kind}>{t(item.label)}</option>)}
        </Select>
      </label>}
      {!embedded && <label className="grid gap-1 text-xs">{t("Saved draft")}
        <Select disabled={locked} value={id} onChange={(event) => { setId(event.target.value); setReview(undefined); setSavedID(""); setSavedReview(false); setPlanID(""); setReportID(""); setError(""); setJoint([]); setJointNote(""); }}>
          <option value="">{t("Choose a saved draft")}</option>
          {records.map((record) => <option key={record.id} value={record.id}>{record.title || record.name} · {record.state}</option>)}
        </Select>
      </label>}
      {query.isError && <p role="alert" className="text-sm text-danger">{t("Release review could not be loaded.")}</p>}
      <Button disabled={!!selectionError || !id || locked} onClick={() => void inspect()}>{busy ? t("Checking…") : t("Check draft and dependencies")}</Button>
    </Card>
    <Card className="grid gap-3 p-3" aria-label={t("Deliver drafts together")}>
      <div>
        <h2 className="text-sm font-semibold">{t("Deliver drafts together")}</h2>
        <p className="text-xs text-muted">{t("An object, the page over it and the application holding the page can be delivered as one candidate; nothing has to be published first.")}</p>
      </div>
      <div className="flex flex-wrap gap-2">
        <Button size="sm" disabled={!id || locked} onClick={() => {
          const selection = merged(joint, [{ kind, id }]);
          if (selection.length > 32) { setJointNote(t("A joint candidate accepts at most 32 drafts. Select a supported application closure.")); return; }
          setJoint(selection); setJointNote("");
        }}>{t("Add selected draft")}</Button>
        <Button size="sm" disabled={!id || locked} onClick={() => void addReferenced({ kind, id })}>{t("Add the drafts it depends on")}</Button>
        {scopedApplication && <Button size="sm" disabled={locked} onClick={() => void addReferenced({ kind: "app", id: scopedApplication.id })}>{t("Add the drafts of {title}", { title: scopedApplication.title || scopedApplication.name })}</Button>}
        <Button size="sm" disabled={!joint.length || joint.length > 32 || locked} onClick={() => { setJoint([]); setJointNote(""); }}>{t("Clear selection")}</Button>
      </div>
      {joint.length ? <ul className="grid gap-1" aria-label={t("Joint selection")}>
        {joint.map((choice) => <li className="flex items-center justify-between gap-2 text-xs" key={jointKey(choice)}>
          <span>{t(kinds.find((item) => item.kind === choice.kind)?.label ?? choice.kind)} · <code>{choice.id}</code></span>
          <Button size="sm" disabled={locked} onClick={() => setJoint((current) => current.filter((item) => jointKey(item) !== jointKey(choice)))}>{t("Remove")}</Button>
        </li>)}
      </ul> : <p className="text-xs text-muted">{t("No drafts selected yet.")}</p>}
      {joint.length > 32 && <p role="alert" className="text-xs text-warning">{t("A joint candidate accepts at most 32 drafts. Select a supported application closure.")}</p>}
      {jointNote && <p className="text-xs text-muted" role="status">{jointNote}</p>}
      <Button disabled={!!selectionError || !joint.length || joint.length > 32 || locked} onClick={() => void inspect(joint)}>{busy ? t("Checking…") : t("Check joint candidate")}</Button>
    </Card>
    </>}
    {error && <Card className="p-3 text-sm text-danger" role="alert">{error}</Card>}
    {pending && <Card role="alert" className="grid gap-2 p-3 text-sm">
      <p>{t("A release request is unconfirmed. It may already have been accepted. Retry its original key and payload; leaving does not cancel accepted work.")}</p>
      <code className="break-all text-xs">{pending.kind} · {pending.body.candidateId} · {pending.body.key}</code>
      <div className="flex gap-2">
        <Button disabled={busy} onClick={() => void send(pending)}>{t("Retry original release request")}</Button>
        <Button variant="ghost" disabled={busy} onClick={() => confirmDiscard(() => rememberRequest())}>{t("Forget local request")}</Button>
      </div>
    </Card>}
    {review && <Card className="grid gap-3 p-3" aria-live="polite">
      <div className="flex flex-wrap items-center gap-2"><span className="text-sm font-semibold">{savedReview ? t("Saved candidate review") : review.diagnostic ? t("Candidate rejected") : (review.drafts?.length ?? 0) > 1 ? t("Joint candidate ready for review") : t("Candidate ready for review")}</span>
        {savedReview && <StatusTag status={activeID === savedID ? "active" : "saved"} registry={{ active: { label: t("Active"), tone: "success" }, saved: { label: t("Saved"), tone: "info" } }} />}
        {savedReview && origin && <StatusTag status="promoted" registry={{ promoted: { label: t("Promoted from {tenant}", { tenant: origin.from }), tone: "info" } }} />}
        {savedReview && promotedTo.map((target) => <StatusTag key={target.tenant} status={target.active ? "live" : "held"} registry={{ live: { label: t("Active in {tenant}", { tenant: target.tenant }), tone: "success" }, held: { label: t("Awaiting review in {tenant}", { tenant: target.tenant }), tone: "warning" } }} />)}
      </div>
      {savedReview && promotedTo.length > 0 && <ul className="grid gap-1 text-xs text-muted" aria-label={t("Promotions of this candidate")}>
        {promotedTo.map((target) => <li key={target.tenant}>{t("Promoted to {tenant} by {member} · {when} · {state}", { tenant: target.tenant, member: target.member, when: new Date(target.at).toLocaleString(), state: target.active ? t("active there") : t("not activated there yet; its release holder reviews the upgrade plan in that environment") })}</li>)}
      </ul>}
      {savedReview && origin && activeID !== savedID && <p className="text-xs text-muted" role="status">{t("This candidate was promoted from {tenant}{when}. It is not active here: review the differences and the storage upgrade plan below, then activate it.", { tenant: origin.from, when: origin.at ? ` · ${new Date(origin.at).toLocaleString()}` : "" })}</p>}
      {review.currentId && <p className="break-all text-xs">{t("Installed candidate")}: <code>{review.currentId}</code></p>}
      {review.candidateId && <p className="break-all text-xs">{savedReview ? t("Saved candidate") : t("Draft candidate")}: <code>{review.candidateId}</code></p>}
      {savedReview && hostAdmin && <div className="flex flex-wrap items-center gap-2"><Button size="sm" disabled={locked} onClick={() => open({ view: "host-promotions", params: { from: client.connection.tenant, candidate: savedID } })}>{t("Promote to another environment…")}</Button><span className="text-xs text-muted">{t("Development → test → production: the sealed bytes of this candidate move into another tenant through the host console.")}</span></div>}
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
        <ul className="grid gap-1 text-xs">{upgradePlan.additions.map(addition => <li key={`${addition.type}/${addition.field}`}>{addition.type} · {addition.field} · {t(addition.kind)} · {t("{count} existing records", { count: addition.records })}</li>)}</ul>
        <Checkbox disabled={locked} checked={confirmedUpgrade === upgradePlan.id} onChange={checked => setConfirmedUpgrade(checked ? upgradePlan.id : "")}>{t("Confirm this optional field upgrade plan")}</Checkbox>
      </Card>}
      {builder && savedReview && savedID && activeID && activeID !== savedID && <MigrationWorkbench from={activeID} to={savedID} assets={assets} onChanged={async () => { await inventory.refetch(); await readSaved(savedID); }} onLock={setMigrationLocked} />}
      {savedReview && runningDiagnostic && <p className="break-words text-xs text-warning">{runningDiagnostic}</p>}
      {review.diagnostic && (() => { const d = diagnose(review.diagnostic, { application: scope || (kind === "app" ? id : undefined) }); return <div className="grid gap-2">
        <p role="alert" className="text-sm text-danger">{d.text}</p>
        {d.text !== review.diagnostic && <p className="text-xs text-muted"><code>{review.diagnostic}</code></p>}
        {d.fix && <div><Button size="sm" onClick={() => open({ view: d.fix!.view, params: d.fix!.params })}>{d.fix.label}</Button></div>}
      </div>; })()}
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
        {!savedReview && <Button disabled={locked || Boolean(savedID)} onClick={save}>{busy ? t("Saving…") : t("Save immutable candidate")}</Button>}
        {savedID && activeID !== savedID && <p className="break-all text-sm" role="status">{t("Candidate saved; not active for operators.")} <code>{savedID}</code></p>}
        {builder && savedID && functionNames.length > 0 && <div className="grid gap-2 rounded border border-border p-3">
          <div className="text-sm font-semibold">{t("Measured function evaluation")}</div>
          <p className="text-xs text-muted">{t("A saved synthetic plan runs real model calls. A passing report for each included function is required before activation.")}</p>
          <label className="grid gap-1 text-xs">{t("Evaluation plan")}
            <Select disabled={locked} value={planID} onChange={(event) => { setPlanID(event.target.value); setReportID(""); }}>
              <option value="">{t("Choose an evaluation plan")}</option>
              {eligiblePlans.map((plan) => <option key={plan.id} value={plan.id}>{plan.title}</option>)}
            </Select>
          </label>
          <div className="flex flex-wrap gap-2">
            <Button disabled={!planID || locked} onClick={evaluate}>{busy ? t("Starting evaluation…") : t("Run measured evaluation")}</Button>
            <Button disabled={locked} onClick={() => { void reports.refetch(); }}>{t("Refresh evaluation reports")}</Button>
          </div>
          {reports.isError && <p role="alert" className="text-xs text-warning">{t("Evaluation reports are unavailable. Cached results do not grant activation.")}</p>}
          {reportID && <p className="break-all text-xs">{t("Evaluation report")}: <code>{reportID}</code></p>}
          {report && <p className="text-sm" role="status">{t("Report state")}: {t(report.state)} · {t("Quality")}: {Math.round(report.quality * 100)}% · {t("Reported USD cost")}: {report.costComplete ? report.costUsd : t("Unknown")} · {t("Peak latency (ms)")}: {report.peakLatencyMillis} · {t("Calls")}: {report.attempts.length}</p>}
          {!eligiblePlans.length && <p className="text-xs text-warning">{t("Save a function test plan with evaluation cases before running the release evaluation.")}</p>}
        </div>}
        {!builder && functionNames.length > 0 && !evaluated && <p role="status" className="text-sm text-warning">{t("Required function evaluations are missing. Ask a builder to complete them before activation.")}</p>}
        {savedID && <Button variant="default" disabled={locked || !savedReview || review.candidateId !== savedID || activeID === savedID || !evaluated || !canActivate && !(upgradePlan && confirmedUpgrade === upgradePlan.id)} onClick={activate}>{t("Activate release")}</Button>}
        {activeID === savedID && savedID && <p className="break-all text-sm" role="status">{savedReview && !runningMatches ? t("Active release differs from running definitions.") : t("Release active for operators.")} <code>{activeID}</code></p>}
      </div>}
    </Card>}
  </div>;
}
