import { createRecordActionSubmitter, newId, useHost, useRecordInventory } from "@platform/app";
import { apiErrorMessage, type Api } from "@platform/kernel";
import { Button, Card, Disclosure, Select, Textarea, t, useInputDrafts, useUnsavedChanges } from "@platform/ui";
import { useEffect, useMemo, useRef, useState } from "react";

type Migration = {
  id: string; revision: number; plan: Api.MigrationPlan; review: string;
  state: string; step: number; cursor?: string; processed: number; migrated: number;
  failures?: { type: string; record: string; step: number; reason: string }[];
};

/** A migration stays in the original release task. Decisions use the original
 * record submitter/outbox; progress is read from accepted build records. */
export function MigrationWorkbench({ from, to, assets, onChanged, onLock }: {
  from: string; to: string; assets: Api.ReleaseAsset[];
  onChanged: () => Promise<void>; onLock: (locked: boolean) => void;
}) {
  const host = useHost(), inputs = useInputDrafts();
  const inventory = useRecordInventory<Migration>("build.migration");
  const objects = assets.filter(asset => asset.ref.app === "build" && asset.ref.kind === "object");
  const [type, setType] = useState("");
  const [steps, setSteps] = useState<Api.MigrateStep[]>([]);
  const [stepsValid, setStepsValid] = useState(true);
  const [where, setWhere] = useState<Api.Expr>();
  const [review, setReview] = useState<Api.MigrationReview>();
  const [id, setID] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [uncertain, setUncertain] = useState(false);
  const live = useRef(true), lock = useRef(false);
  const plan: Api.MigrationPlan = { from, to, scope: { type, ...(where ? { where } : {}) }, steps };
  const signature = JSON.stringify(plan), context = useRef(signature); context.current = signature;
  const runs = (inventory.isError ? [] : inventory.data?.records ?? []).filter(run => run.plan.from === from && run.plan.to === to);
  const run = runs.find(run => run.id === id) ?? runs.find(run => run.state !== "cancelled");
  const pending = host.outbox.some(entry => entry.submission.target?.type === "build.migration"
    && ["SUBMISSION_STATE_PENDING", "SUBMISSION_STATE_SENDING", "SUBMISSION_STATE_UNKNOWN"].includes(entry.state));
  const blocked = busy || uncertain || pending;
  const dirty = steps.length > 0 && !run;
  const submit = useMemo(() => createRecordActionSubmitter(host, () => live.current), [host.client]);
  const { markSaved, discardChanges } = useUnsavedChanges(dirty || uncertain, () => {
    setSteps([]); setStepsValid(true); setWhere(undefined); setReview(undefined); setError(""); setUncertain(false);
  }, busy);
  useEffect(() => { live.current = true; return () => { live.current = false; }; }, [host.client]);
  useEffect(() => { onLock(blocked || dirty); return () => onLock(false); }, [blocked, dirty, onLock]);
  useEffect(() => { setReview(undefined); }, [signature]);
  const perform = async (job: () => Promise<void>) => {
    if (lock.current || inputs?.invalid) return;
    lock.current = true; setBusy(true); setError("");
    try { await job(); }
    catch (failure) { if (live.current) setError(failure instanceof Error ? failure.message : String(failure)); }
    finally { lock.current = false; if (live.current) setBusy(false); }
  };
  const inspect = () => perform(async () => {
    const captured = signature;
    const answer = await host.client.call<Api.MigrationReview>("POST", "/v1/releases/migrations/review", plan);
    if (!live.current || context.current !== captured) return;
    if (!answer.ok) throw Error(apiErrorMessage(answer.body) ?? t("The migration could not be reviewed."));
    setReview(answer.body);
  });
  const decide = (schema: string, target: string, revision: number, payload: Record<string, unknown>) => perform(async () => {
    const answer = await submit(schema, { type: "build.migration", id: target }, revision, payload);
    if (!live.current) return;
    if (!answer.accepted) {
      setUncertain(host.client.authorities.outbox.some(entry => entry.submission.target?.id === target
        && ["SUBMISSION_STATE_PENDING", "SUBMISSION_STATE_SENDING", "SUBMISSION_STATE_UNKNOWN"].includes(entry.state)));
      throw Error(answer.error ?? t("The migration decision was not confirmed. Retry the original decision."));
    }
    setUncertain(false); setID(target); markSaved();
    await inventory.refetch(); await onChanged();
  });
  const start = () => {
    if (!review) return;
    const target = id || newId("MIGRATION"); setID(target);
    return decide("build.migration.start", target, 0, { plan, review: review.id, reviewedAt: review.at });
  };
  const editSteps = (value: string) => {
    const parsed: unknown = JSON.parse(value);
    if (!Array.isArray(parsed)) { setStepsValid(false); setReview(undefined); setError(t("Migration steps must be an array.")); return; }
    setStepsValid(true); setSteps(parsed as Api.MigrateStep[]); setError("");
  };
  return <Card className="grid gap-3 p-3" role="region" aria-label={t("Data migration")}>
    <h3 className="text-sm font-semibold">{t("Data migration")}</h3>
    <p className="text-xs text-muted">{t("Declare the changes between these sealed releases. Each accepted batch saves its data and progress; the source remains read only until activation or cancellation.")}</p>
    <p className="break-all text-xs">{t("Source release")}: <code>{from}</code><br />{t("Target release")}: <code>{to}</code></p>
    {inventory.isError && <p role="alert" className="text-sm text-danger">{t("Migration progress could not be loaded. Refresh before starting or continuing.")}</p>}
    {runs.length > 0 && <label className="grid gap-1 text-xs">{t("Saved migration")}<Select aria-label={t("Saved migration")} disabled={blocked} value={run?.id ?? ""} onChange={event => setID(event.target.value)}>
      {!runs.some(item => item.state !== "cancelled") && <option value="">{t("New migration")}</option>}
      {runs.map(item => <option key={item.id} value={item.id}>{item.id} · {t(item.state)}</option>)}
    </Select></label>}
    {!run && <>
      <label className="grid gap-1 text-xs">{t("Migration object")}<Select aria-label={t("Migration object")} disabled={blocked} value={type} onChange={event => setType(event.target.value)}>
        <option value="">{t("Choose a migration object")}</option>
        {objects.map(asset => <option key={asset.ref.name} value={asset.ref.name}>{asset.ref.name}</option>)}
      </Select></label>
      <label className="grid gap-1 text-xs">{t("Migration steps")}<Textarea aria-label={t("Migration steps")} disabled={blocked} parse="json" rows={12} value={JSON.stringify(steps, null, 2)} onChange={event => editSteps(event.target.value)} /></label>
      <p className="text-xs text-muted">{t("Each step declares its kind, types, fields and batch size. Conversions use the existing versioned expressions; reference changes require an identity mapping.")}</p>
      <Disclosure summary={t("Record filter expression")}>
        <Textarea aria-label={t("Record filter expression")} disabled={blocked} parse="json" rows={5} value={where ? JSON.stringify(where, null, 2) : "null"} onChange={event => setWhere(JSON.parse(event.target.value) ?? undefined)} />
      </Disclosure>
      <div className="flex flex-wrap gap-2">
        <Button disabled={!type || !steps.length || !stepsValid || blocked || !!inputs?.invalid} onClick={() => void inspect()}>{t("Review migration")}</Button>
        <Button variant="ghost" disabled={blocked} onClick={() => discardChanges()}>{t("Discard migration draft")}</Button>
      </div>
      {review && <div className="grid gap-2 text-xs" role="region" aria-label={t("Migration review")}>
        <ul className="grid gap-1">{review.types.map(item => <li key={item.type}>{item.type} · {t("{count} existing records", { count: item.records })}</li>)}</ul>
        <p>{t("{count} migration steps", { count: review.steps })} · {new Date(review.at).toLocaleString()}</p>
        {review.issues.length > 0 && <ul className="grid gap-1 text-warning" aria-label={t("Migration issues")}>{review.issues.map((issue, index) => <li key={index}>{issue.type}/{issue.record} · {t("Step {number}", { number: issue.step })}: {issue.reason}</li>)}</ul>}
        <Button disabled={blocked || inventory.isError || inventory.isPending || !!inputs?.invalid} onClick={() => void start()}>{t("Start reviewed migration")}</Button>
      </div>}
    </>}
    {run && <div className="grid gap-2" role="region" aria-label={t("Migration progress")}>
      <p className="text-sm" role="status">{t("Migration state")}: {t(run.state)} · {t("Processed")}: {run.processed} · {t("Migrated")}: {run.migrated}</p>
      <p className="break-all text-xs">{t(run.state === "complete" ? "Completed steps" : "Next step")}: {run.state === "complete" ? run.step : Math.min(run.step + 1, run.plan.steps.length)} / {run.plan.steps.length}{run.cursor ? ` · ${run.cursor}` : ""}</p>
      <Disclosure summary={t("Accepted migration plan")}><pre className="max-h-72 overflow-auto whitespace-pre-wrap text-xs">{JSON.stringify(run.plan, null, 2)}</pre></Disclosure>
      {run.failures?.length ? <ul className="grid gap-1 text-xs text-warning" aria-label={t("Migration failures")}>{run.failures.map((failure, index) => <li key={index}>{failure.type}/{failure.record} · {t("Step {number}", { number: failure.step })}: {failure.reason}</li>)}</ul> : null}
      <div className="flex flex-wrap gap-2">
        {["pending", "running", "cancelling"].includes(run.state) && <Button disabled={blocked || inventory.isError} onClick={() => void decide("build.migration.advance", run.id, run.revision, {})}>{run.state === "cancelling" ? t("Continue cancellation") : t("Run next migration batch")}</Button>}
        {!["cancelled", "cancelling"].includes(run.state) && <Button variant="ghost" disabled={blocked || inventory.isError} onClick={() => void decide("build.migration.cancel", run.id, run.revision, {})}>{t("Cancel migration and restore source")}</Button>}
      </div>
      {run.state === "complete" && <p className="text-sm">{t("Data batches are complete. Review and activate this target release below; activation still validates values and constraints.")}</p>}
      {run.state === "blocked" && <p className="text-sm text-warning">{t("Resolve the recorded failures before activation. Cancel this migration to restore the source and review a corrected plan.")}</p>}
      {run.state === "cancelled" && <Button disabled={blocked} onClick={() => { setID(""); setType(run.plan.scope.type); setSteps(run.plan.steps); setWhere(run.plan.scope.where); setReview(undefined); }}>{t("Review a corrected migration")}</Button>}
    </div>}
    {(uncertain || pending) && <div className="grid gap-2" role="alert"><p className="text-sm">{t("The migration decision was not confirmed. Retry the original decision.")}</p><Button disabled={busy} onClick={() => void perform(async () => { await host.resend(); setUncertain(false); await inventory.refetch(); await onChanged(); })}>{t("Retry original migration decision")}</Button></div>}
    {error && <p role="alert" className="text-sm text-danger">{error}</p>}
    <Button size="sm" variant="ghost" disabled={busy} onClick={() => void perform(async () => { await inventory.refetch(); await onChanged(); })}>{t("Refresh migration progress")}</Button>
  </Card>;
}
