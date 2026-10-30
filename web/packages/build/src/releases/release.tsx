import { useHost, useReadQuery, useRecordInventory } from "@platform/app";
import { Button, Card, Checkbox, ContextHelp, Disclosure, PageHeader, Panel, PropertyList, Select, StatusTag, t, useUnsavedChanges, useViewCall, notify} from "@platform/ui";
import { apiErrorMessage, type Api } from "@platform/kernel";
import { useApplicationScope, useApplicationWorkspace } from "../projects/application-scope";
import { matchesResourceKind, refKey, resourceKinds, resourceRef } from "../projects/resources";
import { branchRefusal } from "../branches/decisions";
import { useCurrentBranch } from "../branches/current";
import { diagnose } from "./diagnostics";
import { useEffect, useRef, useState } from "react";

export { releaseCommandBytes, releaseDraftsSelection, releaseDraftsValue } from "./selection";
export type { JointChoice, ReleaseKind } from "./selection";
import { applicationDraftSelection, jointKey, merged, ownerDraft, releaseCommandBytes, releaseSelectionProblem, type JointChoice, type ReleaseKind } from "./selection";
type ReleaseRequest = { kind: "save"; body: Api.ReleaseSaveRequest }
  | { kind: "evaluate"; body: Api.ReleaseEvaluationRequest }
  | { kind: "activate"; body: Api.ReleaseActivateRequest };

type Record = { id: string; title: string; name: string; state: string; evaluation?: unknown[]; roots?: Api.AssetRef[] };
type EvaluationPlan = { id: string; name?: string; title: string; function?: string; evaluation?: unknown[] };
type EvaluationReport = { id: string; candidate: string; function: string; state: string; quality: number; costUsd: number; costComplete: boolean; peakLatencyMillis: number; attempts: unknown[] };
const kinds: { kind: ReleaseKind; type: string; label: string }[] = [
  ...resourceKinds.filter((item) => item.kind !== "action" && item.kind !== "automation").map((item) => ({ kind: item.kind as ReleaseKind, type: item.type, label: item.plural })),
  { kind: "app", type: "build.app", label: "Applications" },
];
export const releaseKinds: ReleaseKind[] = kinds.map((item) => item.kind);

function ReleaseDiagnostic({ diagnostic, application, builder, locked, onAdd }: { diagnostic: string; application?: string; builder: boolean; locked: boolean; onAdd: (choice: JointChoice) => void }) {
  const { open } = useApplicationWorkspace();
  const diagnosis = diagnose(diagnostic, { application });
  return <div role="group" aria-label={t("Release problem")} className="grid min-w-0 gap-2">
    <p role="alert" className="break-words text-sm text-danger">{diagnosis.text}</p>
    {diagnosis.text !== diagnostic && <Disclosure summary={t("Technical details")}><p className="break-words text-xs text-muted">{diagnostic}</p></Disclosure>}
    {diagnosis.fix && <div><Button size="sm" disabled={locked} onClick={() => open({ view: diagnosis.fix!.view, params: diagnosis.fix!.params })}>{diagnosis.fix.label}</Button></div>}
    {builder && diagnosis.resources?.map(resource => <DiagnosticResource key={`${resource.kind}/${resource.name}`} {...resource} application={application} locked={locked} onAdd={onAdd} />)}
  </div>;
}

function DiagnosticResource({ kind, name, application, locked, onAdd }: { kind: "agent" | "query"; name: string; application?: string; locked: boolean; onAdd: (choice: JointChoice) => void }) {
  const { open } = useApplicationWorkspace();
  const inventory = useRecordInventory<Record>(`build.${kind}`);
  const records = inventory.data?.records.filter(record => record.name === name) ?? [];
  const record = records.length === 1 ? records[0] : undefined;
  return <div className="flex flex-wrap gap-2">
    <Button size="sm" disabled={locked || inventory.isLoading} onClick={() => open({ view: kind, params: { ...(record ? { id: record.id } : {}), ...(application ? { application } : {}) } })}>
      {record ? kind === "query" ? t("Open query") : t("Open agent") : kind === "query" ? t("Find query") : t("Find agent")}
    </Button>
    {kind === "query" && record && <Button size="sm" disabled={locked} onClick={() => onAdd({ kind, id: record.id })}>{t("Include query in candidate")}</Button>}
    {inventory.isError && <p role="status" className="text-xs text-warning">{t("The draft could not be located. Open its list to retry.")}</p>}
  </div>;
}

export function ReleaseReview({ initialKind = "object", initialID = "", initialDrafts = [], selectionError, embedded = false, candidateID, applicationName }: { initialKind?: ReleaseKind; initialID?: string; initialDrafts?: JointChoice[]; selectionError?: string; embedded?: boolean; candidateID?: string; applicationName?: string } = {}) {
  const { client, me, refresh , holds} = useHost();
  const systemScope = me.tenantId === "system";
  const builder = holds("build", "builder");
  const mayRelease = builder || holds("build", "publisher");
  const scope = useApplicationScope(), { id: branchId } = useCurrentBranch();
  const { open } = useApplicationWorkspace();
  const [kind, setKind] = useState<ReleaseKind>(initialKind);
  const [id, setId] = useState(initialID);
  const [review, setReview] = useState<Api.ReleasePreview>();
  const [assets, setAssets] = useState<Api.ReleaseAsset[]>([]);
	const [savedCandidateAI, setCandidateAI] = useState<Api.ApplicationAI[]>([]);
  const [candidateKey, setCandidateKey] = useState("");
  const [savedID, setSavedID] = useState("");
  const [offset, setOffset] = useState(0);
  const [savedReview, setSavedReview] = useState(false);
  const [runningMatches, setRunningMatches] = useState<boolean>();
  const [runningDiagnostic, setRunningDiagnostic] = useState("");
  const [canActivate, setCanActivate] = useState(false);
  const [activationDiagnostic, setActivationDiagnostic] = useState("");
  const [upgradePlan, setUpgradePlan] = useState<Api.ReleaseUpgradePlan>();
  const [extensions, setExtensions] = useState<Api.ReleaseExtensionReview[]>([]);
  const [origin, setOrigin] = useState<{ from: string; at?: string }>();
  const [promotedTo, setPromotedTo] = useState<Api.PromotionTarget[]>([]);
  const wentTo = (targets?: Api.PromotionTarget[]) => (targets ?? []).map((target) => `${target.tenant} (${target.active ? t("active") : t("awaiting review")})`).join(", ");
  const [confirmedUpgrade, setConfirmedUpgrade] = useState("");
  const [planID, setPlanID] = useState("");
  const [reportID, setReportID] = useState("");
  const reviewedEvaluation = useRef("");
  const [error, setError] = useState(selectionError ? t(selectionError) : "");
  const [busy, setBusy] = useState(false);
  const lock = useRef(false);
  const [pending, setPending] = useState<ReleaseRequest>();
  const pendingRef = useRef(pending); pendingRef.current = pending;
  const rememberRequest = (request?: ReleaseRequest) => { pendingRef.current = request; setPending(request); };
  const { confirmDiscard } = useUnsavedChanges(!!pending, () => rememberRequest(), busy);
  const locked = busy || !!pending;
  const perform = async (job: () => Promise<void>) => {
    if (lock.current) return;
    lock.current = true; setBusy(true); setError("");
    try { await job(); }
    finally { lock.current = false; setBusy(false); }
  };
  const call = useViewCall();
  const invalidSelection = !!selectionError || !!call?.expired;
  const transferred = (call?.input as { drafts?: JointChoice[] } | undefined)?.drafts;
  const [joint, setJoint] = useState<JointChoice[]>(() => {
    if (transferred?.length) return transferred;
    return initialID && (initialKind === "app" || initialDrafts.length) ? merged([{ kind: initialKind, id: initialID }], initialDrafts) : initialDrafts;
  });
  const [jointNote, setJointNote] = useState("");
  const applications = useRecordInventory<Record & Api.Application>("build.app", 1000, builder);
  const scopedApplication = scope ? applications.data?.records.find(item => item.id === scope) : undefined;
  const held = new Set([...(scopedApplication?.resources ?? []).map(refKey), ...(scopedApplication?.pages ?? []).map(name => refKey({ app: "build", kind: "page", name }))]);
  const filterName = applicationName ?? scopedApplication?.name;
  const scopeUnresolved = !!scope && !scopedApplication && !applicationName;
  const inventory = useReadQuery<Api.ReleasePage>(`/v1/releases/candidates?offset=${offset}&limit=20${filterName ? `&application=${encodeURIComponent(filterName)}` : ""}`, undefined, mayRelease && (!scope || !!scopedApplication || !!applicationName));
  const hostAdmin = useReadQuery<{ subject: string; tenants: string[] }>("/v1/host/me", undefined, mayRelease).isSuccess;
  const activeID = inventory.isError ? "" : inventory.data?.activeId ?? "";
  const promoted = (inventory.isError ? [] : inventory.data?.candidates ?? []).filter((candidate) => candidate.from && candidate.id !== activeID);
  const selected = kinds.find((item) => item.kind === kind)!;
  const query = useRecordInventory<Record>(selected.type, 1000, builder);
  const functions = useRecordInventory<Record>("build.function", 1000, builder);
  const plans = useRecordInventory<EvaluationPlan>("build.testplan", 1000, builder);
  const reports = useRecordInventory<EvaluationReport>("build.evaluation", 1000, mayRelease);
  const resourceKind = resourceKinds.find(item => item.kind === kind);
  const records = (query.isError ? [] : query.data?.records ?? []).filter(record => {
    if (resourceKind && kind !== "flow" && !matchesResourceKind(resourceKind, record)) return false;
    if (!scope) return true;
    if (kind === "app") return record.id === scope;
    return !!scopedApplication && held.has(refKey(resourceKind ? resourceRef(resourceKind, record.name) : { app: "build", kind, name: record.name }));
  });
  const functionNames = (review?.included ?? []).filter((ref) => ref.app === "build" && ref.kind === "function").map((ref) => ref.name);
  const localPlans = (plans.isError || functions.isError ? [] : plans.data?.records ?? []).filter((plan) => plan.evaluation?.length && functions.data?.records.some((fn) => fn.id === plan.function && functionNames.includes(fn.name)));
  const eligiblePlans = [...localPlans, ...assets.filter(asset => asset.ref.app === "build" && asset.ref.kind === "evaluation-set" && !localPlans.some(plan => plan.name === asset.ref.name)).map(asset => ({id:asset.ref.name,title:asset.ref.name}))];
  const report = reports.isError ? undefined : reports.data?.records.find((item) => item.id === reportID);
  const candidateQuery=useReadQuery<Api.SavedReleaseReview>(`/v1/releases/candidates/${encodeURIComponent(savedID)}`,undefined,!!savedID&&mayRelease);
  const candidateAI=candidateQuery.isError?[]:candidateQuery.data?.ai??savedCandidateAI;
  const passedFunctions=candidateAI.filter(item=>item.resource.ref.kind==="function"&&item.evaluations.some(report=>report.candidate===savedID&&report.state==="passed")).map(item=>item.resource.ref.name);
  const currentReportState=candidateQuery.isError?"Evaluation reports are unavailable.":candidateAI.flatMap(item=>item.evaluations).find(item=>item.id===report?.id)?.state??report?.state;
  const agentNames = (review?.included ?? []).filter(ref => ref.app === "build" && ref.kind === "agent").map(ref => ref.name);
  const evaluated = functionNames.every((name) => passedFunctions.includes(name)) && agentNames.every(name => candidateAI.some(item => item.resource.ref.kind === "agent" && item.resource.ref.name === name && item.evaluations.some(report => report.candidate === savedID && report.state === "passed")));
  useEffect(() => {
    const key = `${savedID}/${report?.id}`;
    if (!mayRelease || !savedID || report?.state !== "passed" || locked || reviewedEvaluation.current === key) return;
    reviewedEvaluation.current = key;
    void perform(() => readSaved(savedID, true));
  }, [mayRelease, savedID, report?.id, report?.state, locked]);
  const clearReview = () => {
    setReview(undefined); setAssets([]); setCandidateAI([]); setSavedReview(false); setRunningMatches(undefined); setRunningDiagnostic("");
    setCanActivate(false); setActivationDiagnostic(""); setUpgradePlan(undefined); setExtensions([]); setOrigin(undefined); setPromotedTo([]);
    setPlanID(""); setReportID("");
  };
  const readSaved = async (candidateID: string, keepEvaluation = false) => {
    if (!keepEvaluation) clearReview();
    setSavedID(candidateID);
    try {
      const result = await client.call<Api.SavedReleaseReview>("GET", `/v1/releases/candidates/${encodeURIComponent(candidateID)}`);
      if (!result.ok || result.body.preview?.candidateId !== candidateID) {
        setError(apiErrorMessage(result.body) ?? t("Saved releases could not be loaded.")); return;
      }
      setReview(result.body.preview); setSavedReview(true); setAssets(result.body.assets ?? []); setCandidateAI(result.body.ai ?? []);
      setRunningMatches(result.body.runningMatches); setRunningDiagnostic(result.body.runningDiagnostic ?? "");
      setCanActivate(result.body.canActivate); setActivationDiagnostic(result.body.activationDiagnostic ?? "");
      setUpgradePlan(result.body.upgradePlan);
      setExtensions(result.body.extensions ?? []);
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
  const openedCandidate = useRef("");
  useEffect(() => {
    if (!mayRelease || locked || !candidateID || openedCandidate.current === candidateID) return;
    openedCandidate.current = candidateID;
    void loadSaved(candidateID);
  }, [mayRelease, locked, candidateID]);
  if (!mayRelease) {
    return <PageHeader title={t("Release review")} description={t("A builder or publisher role is required to review releases.")} />;
  }
  const readReferenced = async (from: JointChoice): Promise<JointChoice[]> => {
    const result = await client.call<Api.ReleaseDraftClosure>("POST", "/v1/releases/drafts/referenced", { ...ownerDraft(from), branch: branchId } satisfies Api.ReleaseDraftsRequest);
    if (!result.ok) throw new Error(apiErrorMessage(result.body) ?? t("Draft dependencies could not be read."));
    return result.body.drafts.map(draft => ({ kind: draft.kind as ReleaseKind, id: draft.id }));
  };
  const inspect = (drafts?: JointChoice[]) => {
    if (!builder || pendingRef.current || invalidSelection) return;
    const problem = drafts && releaseSelectionProblem(drafts);
    if (problem) { setError(t(problem)); return; }
    return perform(async () => {
      clearReview(); setSavedID(""); setConfirmedUpgrade(""); setCandidateKey("");
      try {
        const roots = drafts?.length ? drafts : [{ kind, id }];
        const selection = await applicationDraftSelection(roots, readReferenced);
        const problem = releaseSelectionProblem(selection);
        if (problem) { setError(t(problem)); return; }
        setJoint(selection);
        const request: Api.ReleasePreviewRequest = { drafts: selection.map(ownerDraft), branch: branchId };
        const result = await client.call<Api.ReleasePreview>("POST", "/v1/releases/preview", request);
        if (!result.ok) setError(apiErrorMessage(result.body) ?? t("Release review could not be loaded."));
        else { setReview(result.body); setCandidateKey(crypto.randomUUID()); }
      } catch (error) { setError(error instanceof Error ? error.message : t("Release review could not be loaded.")); }
    });
  };
  const addReferenced = (from: JointChoice) => {
    if (!builder || invalidSelection || !from.id || pendingRef.current) return;
    return perform(async () => {
      setJointNote("");
      try {
        const found = await readReferenced(from);
        const selection = merged(joint, [from, ...found]);
        const problem = releaseSelectionProblem(selection);
        if (problem) { setJointNote(t(problem)); return; }
        setJoint(selection);
        setJointNote(found.length ? t("Added {count} dependent drafts.", { count: found.length }) : t("Nothing to add: every dependency is installed or already selected."));
      } catch (error) { setJointNote(error instanceof Error ? error.message : t("Draft dependencies could not be read.")); }
    });
  };
  const send = (request: ReleaseRequest) => perform(async () => {
    const current = pendingRef.current;
    if (current && current !== request) return;
    if (new TextEncoder().encode(JSON.stringify(request.body)).length > releaseCommandBytes) { setError(t("The release selection exceeds the original 65536-byte request budget. No drafts were dropped.")); return; }
    rememberRequest(request);
    const path = request.kind === "save" ? "/v1/releases/candidates" : request.kind === "evaluate" ? "/v1/releases/evaluations" : "/v1/releases/active";
    let answer: { ok: boolean; status: number; body: { id: string } };
    try { answer = await client.call<{ id: string }>("POST", path, request.body); }
    catch { setError(t("The release request was not confirmed. Retry the original request.")); return; }
    if (!answer.ok || typeof answer.body?.id !== "string" || !answer.body.id
      || request.kind !== "evaluate" && answer.body.id !== request.body.candidateId) {
      const message = apiErrorMessage(answer.body) ?? t("The release request was not confirmed. Retry the original request.");
      if (request.kind === "activate") await readSaved(request.body.candidateId);
      setError(message); return;
    }
    rememberRequest();
    notify.success(t(request.kind === "save" ? "Candidate saved. Activate it to publish the application." : request.kind === "activate" ? "Release activated." : "Evaluation started."));
    try {
      if (request.kind === "evaluate") { setReportID(answer.body.id); await reports.refetch(); }
      else {
        if (request.kind === "activate") await refresh?.();
        setSavedID(answer.body.id); await inventory.refetch(); await readSaved(answer.body.id);
      }
    } catch { setError(t("The release request was accepted, but its refreshed review is unavailable.")); }

  });
  const evaluate = () => {
    if (pendingRef.current) return pendingRef.current.kind === "evaluate" ? send(pendingRef.current) : undefined;
    if (!savedReview || !savedID || !planID) return;
    return send({ kind: "evaluate", body: { candidateId: savedID, planId: planID, key: crypto.randomUUID() } });
  };
  const evaluateAgent = (name: string) => {
    if (pendingRef.current) return;
    if (!savedReview || !savedID) return;
    return send({ kind: "evaluate", body: { candidateId: savedID, agent: `build.${name}`, key: crypto.randomUUID() } });
  };
  const save = () => {
    if (pendingRef.current) return pendingRef.current.kind === "save" ? send(pendingRef.current) : undefined;
    if (!review?.candidateId || !candidateKey || savedReview) return;
    const selection = review.drafts ?? [];
    return send({ kind: "save", body: { ...(selection.length ? { drafts: selection.map(choice => ({ ...choice })) } : ownerDraft({ kind, id })), candidateId: review.candidateId, key: candidateKey } });
  };
  const activate = () => {
    if (pendingRef.current) return pendingRef.current.kind === "activate" ? send(pendingRef.current) : undefined;
    if (!savedReview || !savedID || review?.candidateId !== savedID
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
    {systemScope && <Panel role="status" className="grid gap-2 text-sm">
      <p>{t("This is the system self-hosting workspace. To publish a business application, switch to a business workspace using the account menu. System definitions require an approved Platform change.")}</p>
      <div><Button onClick={() => open({ view: "self-hosting" })}>{t("Open self-hosting")}</Button></div>
    </Panel>}
    {!embedded && <PageHeader title={t("Release review")} help={t(builder ? "Compare a saved draft, then save its exact candidate bytes. Saving does not activate it for operators." : "Review sealed candidate definitions and activate a release. Definition editing belongs to builders.")}
      actions={builder && scope && <Button onClick={() => open({ view: "project", params: { id: scope } })}>{t("Back to project")}</Button>} />}
    {scopeUnresolved && !builder && <Panel role="status" className="p-3 text-sm text-muted">{t("This handoff points at a builder draft, which a publisher does not read. Reopen release review from a saved candidate or a named application.")}</Panel>}
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
        <div className="flex items-center justify-between gap-2">
          <h3 className="text-sm font-semibold">{t("Promoted into this environment")} · {promoted.length}</h3>
          <ContextHelp title={t("Promoted into this environment")}>{t("Candidates another environment promoted here and that still wait for review. Open one to check its upgrade plan and activate it.")}</ContextHelp>
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
      <Button disabled={invalidSelection || !id || locked} onClick={() => void inspect()}>{busy ? t("Checking…") : t("Check draft and dependencies")}</Button>
    </Card>
    <Card className="grid gap-3 p-3" aria-label={t("Deliver drafts together")}>
      <div className="flex items-center justify-between gap-2">
        <h2 className="text-sm font-semibold">{t("Deliver drafts together")}</h2>
        <ContextHelp title={t("Deliver drafts together")}>{t("Checking an application includes its saved dependency drafts. Review the complete selection and resolve any connection or definition problems before saving; nothing has to be published separately.")}</ContextHelp>
      </div>
      <div className="flex flex-wrap gap-2">
        <Button size="sm" disabled={!id || locked} onClick={() => {
          const selection = merged(joint, [{ kind, id }]);
          if (selection.length > 128) { setJointNote(t("A joint candidate accepts at most 128 drafts. Select a supported application closure.")); return; }
          setJoint(selection); setJointNote("");
        }}>{t("Add selected draft")}</Button>
        <Button size="sm" disabled={!id || locked} onClick={() => void addReferenced({ kind, id })}>{t("Add the drafts it depends on")}</Button>
        {scopedApplication && <Button size="sm" disabled={locked} onClick={() => void addReferenced({ kind: "app", id: scopedApplication.id })}>{t("Add the drafts of {title}", { title: scopedApplication.title || scopedApplication.name })}</Button>}
        <Button size="sm" disabled={!joint.length || joint.length > 128 || locked} onClick={() => { setJoint([]); setJointNote(""); }}>{t("Clear selection")}</Button>
      </div>
      {joint.length ? <ul className="grid gap-1" aria-label={t("Joint selection")}>
        {joint.map((choice) => <li className="flex items-center justify-between gap-2 text-xs" key={jointKey(choice)}>
          <span>{t(kinds.find((item) => item.kind === choice.kind)?.label ?? choice.kind)} · <code>{choice.id}</code></span>
          <Button size="sm" disabled={locked} onClick={() => setJoint((current) => current.filter((item) => jointKey(item) !== jointKey(choice)))}>{t("Remove")}</Button>
        </li>)}
      </ul> : <p className="text-xs text-muted">{t("No drafts selected yet.")}</p>}
      {joint.length > 128 && <p role="alert" className="text-xs text-warning">{t("A joint candidate accepts at most 128 drafts. Select a supported application closure.")}</p>}
      {jointNote && <p className="text-xs text-muted" role="status">{jointNote}</p>}
      <Button disabled={invalidSelection || !joint.length || joint.length > 128 || locked} onClick={() => void inspect(joint)}>{busy ? t("Checking…") : t("Check joint candidate")}</Button>
    </Card>
    </>}
    {call?.expired && !error && <Panel role="status" className="p-3 text-sm text-muted">{t("The original handoff expired when the view was reopened. Reopen release review from the application to review its current selection.")}</Panel>}
    {error && <Card className="p-3"><ReleaseDiagnostic diagnostic={error} application={scope || (kind === "app" ? id : undefined)} builder={builder} locked={locked} onAdd={choice => { setJoint(current => merged(current, [choice])); setError(""); clearReview(); }} /></Card>}
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
      {(review.currentId || review.candidateId) && <Disclosure summary={t("Candidate identity")}><PropertyList items={[
        ...(review.currentId ? [[t("Installed candidate"), <code>{review.currentId}</code>] as [string, React.ReactNode]] : []),
        ...(review.candidateId ? [[savedReview ? t("Saved candidate") : t("Draft candidate"), <code>{review.candidateId}</code>] as [string, React.ReactNode]] : []),
      ]} /></Disclosure>}
      {savedReview && hostAdmin && <div className="flex flex-wrap items-center gap-2"><Button size="sm" disabled={locked} onClick={() => open({ view: "host-promotions", params: { from: client.connection.tenant, candidate: savedID } })}>{t("Promote to another environment…")}</Button><ContextHelp title={t("Promote to another environment…")}>{t("Development → test → production: the sealed bytes of this candidate move into another tenant through the host console.")}</ContextHelp></div>}
      {savedReview && <Disclosure className="rounded border border-border p-3" summary={<span className="text-sm font-semibold">{t("Sealed definitions")} · {assets.length}</span>}>
        <p className="my-2 text-xs text-muted">{t("These definitions come from this immutable candidate, including its original versions and dependencies.")}</p>
        {assets.map(asset => <Disclosure key={`${asset.ref.app}/${asset.ref.kind}/${asset.ref.name}`} className="my-2" summary={<span className="font-mono text-xs">{asset.ref.app}/{asset.ref.kind}/{asset.ref.name}</span>}><pre className="mt-2 max-h-72 overflow-auto whitespace-pre-wrap break-all text-xs">{JSON.stringify(asset, null, 2)}</pre></Disclosure>)}
      </Disclosure>}
      {savedReview && <p className="text-sm" role="status">{runningDiagnostic ? t("Current definitions could not be compared with this candidate.") : runningMatches ? t("Running definitions match this saved release.") : t("Running definitions differ from this saved release.")}</p>}
      {savedReview && !runningMatches && canActivate && <p className="text-sm">{t("Activation installs the candidate's included objects, pages, applications, workflows, AI functions and code functions together.")}</p>}
      {savedReview && activationDiagnostic && <p role="alert" className="text-sm text-warning">{activationDiagnostic}</p>}
      {savedReview && extensions.length > 0 && <Panel variant="plain" role="region" aria-label={t("Customer extensions")} className="grid gap-3 border-t border-border pt-3">
        <h3 className="text-sm font-medium">{t("Customer extensions")}</h3>
        {extensions.map(extension => <div key={extension.type} className="grid gap-1 text-xs">
          <PropertyList columns={2} items={[[t("Object type"), extension.type], [t("Extension of"), extension.base.ref.name], [t("Source version"), extension.base.sourceVersion || t("Original version not recorded")], [t("Target version"), extension.targetVersion], [t("Retained fields"), extension.fields.join(", ")]]} />
          {extension.issue && <p role="alert" className="text-warning">{extension.issue}</p>}
        </div>)}
      </Panel>}
      {savedReview && upgradePlan && <Panel variant="plain" role="group" className="grid gap-3 border-t border-border pt-3" aria-label={t("Storage upgrade plan")}>
        <div className="flex items-center justify-between gap-2"><h3 className="text-sm font-semibold">{t("Storage upgrade plan")}</h3><ContextHelp title={t("Storage upgrade plan")}>{t("Add optional fields without changing existing values or record history. Required fields, type changes and removals are refused.")}</ContextHelp></div>
        <ul className="grid gap-1 text-xs">{upgradePlan.additions.map(addition => <li key={`${addition.type}/${addition.field}`}>{addition.type} · {addition.field} · {t(addition.kind)} · {t("{count} existing records", { count: addition.records })}</li>)}</ul>
        <Checkbox disabled={locked} checked={confirmedUpgrade === upgradePlan.id} onChange={checked => setConfirmedUpgrade(checked ? upgradePlan.id : "")}>{t("Confirm this optional field upgrade plan")}</Checkbox>
      </Panel>}
      {savedReview && runningDiagnostic && <p className="break-words text-xs text-warning">{runningDiagnostic}</p>}
      {review.diagnostic && <ReleaseDiagnostic diagnostic={review.diagnostic} application={scope || (kind === "app" ? id : undefined)} builder={builder} locked={locked} onAdd={choice => { setJoint(current => merged(current, [choice])); setError(""); clearReview(); }} />}
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
        {!savedReview && <Button disabled={locked || Boolean(savedID) || !!branchId} title={branchId ? branchRefusal("publish") : undefined} onClick={save}>{busy ? t("Saving…") : t("Save immutable candidate")}</Button>}
        {savedID && activeID !== savedID && <p className="break-all text-sm" role="status">{t("Candidate saved; not active for operators.")} <code>{savedID}</code></p>}
        {builder && savedID && functionNames.length > 0 && <div className="grid gap-2 rounded border border-border p-3">
          <div className="flex items-center justify-between gap-2"><h3 className="text-sm font-semibold">{t("Measured function evaluation")}</h3><ContextHelp title={t("Measured function evaluation")}>{t("Measured evaluations call configured models. Missing or failed reports do not prevent application delivery.")}</ContextHelp></div>
          <label className="grid gap-1 text-xs">{t("Evaluation plan")}
            <Select disabled={locked} value={planID} onChange={(event) => { setPlanID(event.target.value); setReportID(""); }}>
              <option value="">{t("Choose an evaluation plan")}</option>
              {eligiblePlans.map((plan) => <option key={plan.id} value={plan.id}>{plan.title}</option>)}
            </Select>
          </label>
          <div className="flex flex-wrap gap-2">
            <Button disabled={!planID || locked} onClick={evaluate}>{busy ? t("Starting evaluation…") : t("Run measured evaluation")}</Button>
            <Button disabled={locked} onClick={() => { void perform(() => readSaved(savedID, true)); }}>{t("Refresh evaluation reports")}</Button>
          </div>
          {(reports.isError||candidateQuery.isError) && <p role="alert" className="text-xs text-warning">{t("Evaluation reports could not be read. No quality result is claimed.")}</p>}
          {reportID && <p className="break-all text-xs">{t("Evaluation report")}: <code>{reportID}</code></p>}
          {report && <div role="status"><PropertyList columns={3} items={[[t("Report state"),t(currentReportState??report.state)],[t("Quality"),Math.round(report.quality*100)+"%"],[t("Reported USD cost"),report.costComplete?report.costUsd:t("Unknown")],[t("Peak latency (ms)"),report.peakLatencyMillis],[t("Calls"),report.attempts.length]]}/></div>}
          {!eligiblePlans.length && <p className="text-xs text-warning">{t("Save a function test plan with evaluation cases before running the release evaluation.")}</p>}
        </div>}
        {builder && savedID && agentNames.length > 0 && <Panel title={t("Agent candidate evaluations")} role="region" aria-label={t("Agent candidate evaluations")} className="grid gap-2">
          <p className="text-xs text-muted">{t("Run each agent's declared cases three times with the current agent model to measure that configuration. Reports do not determine application delivery.")}</p>
          {agentNames.map(name => <div key={name} className="grid gap-1 text-xs"><div className="flex flex-wrap items-center gap-2"><strong>{name}</strong><Button size="sm" disabled={locked} onClick={() => void evaluateAgent(name)}>{t("Run declared cases")}</Button></div>
            {candidateAI.filter(item => item.resource.ref.kind === "agent" && item.resource.ref.name === name).flatMap(item => item.evaluations.filter(report => report.candidate === savedID).map(report => <PropertyList key={report.id} columns={2} items={[[t("Report"),<code>{report.id}</code>],[t("Report state"),t(report.state)],[t("Model"),report.model],[t("Quality"),Math.round(report.quality*100)+"%"]]}/>))}
          </div>)}
          <Button disabled={locked} onClick={() => void perform(() => readSaved(savedID))}>{t("Refresh evaluation reports")}</Button>
          {reportID && <p className="break-all text-xs">{t("Evaluation report")}: {reportID}</p>}
        </Panel>}
        {(functionNames.length > 0 || agentNames.length > 0) && !evaluated && <p role="status" className="text-sm text-warning">{t("This candidate is not fully evaluated. Configure models and run evaluations when needed; no quality result is claimed.")}</p>}
        {savedID && !systemScope && <Button variant="default" disabled={locked || !savedReview || review.candidateId !== savedID || activeID === savedID || !canActivate && !(upgradePlan && confirmedUpgrade === upgradePlan.id)} onClick={activate}>{t("Activate release")}</Button>}
        {activeID === savedID && savedID && <p className="break-all text-sm" role="status">{savedReview && !runningMatches ? t("Active release differs from running definitions.") : t("Release active for operators.")} <code>{activeID}</code></p>}
        {activeID === savedID && savedID && !runningMatches && (function () {
          const application = applications.data?.records.find((item) => joint.some((choice) => choice.kind === "app" && choice.id === item.id));
          return application ? <Button size="sm" onClick={() => open({ view: "application-runs", params: { app: "build", name: application.name, application: `build:${application.name}` } })}>{t("Verify as an operator")}</Button>
            : <p className="text-xs text-muted">{t("Verify the release from the business application in the operations zone.")}</p>;
        })()}
      </div>}
    </Card>}
  </div>;
}
