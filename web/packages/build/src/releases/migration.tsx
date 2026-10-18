import { sourceFieldInput } from "@platform/app";
import { StructuredEditor } from "@platform/ui";
import { useActionTrial } from "@platform/app";
import { createRecordActionSubmitter, newId, useHost, useRecordInventory } from "@platform/app";
import { apiErrorMessage, type Api } from "@platform/kernel";
import { Button, Card, Checkbox, Disclosure, Input, Select, Toggles, t, useInputDrafts, useUnsavedChanges, type EntityInfo } from "@platform/ui";
import { useEffect, useMemo, useRef, useState } from "react";
import { ExpressionEditor, literalExpression, type ExpressionChoice } from "../editor/ExpressionEditor";

type Migration = {
  id: string; revision: number; plan: Api.MigrationPlan; review: string;
  state: string; step: number; cursor?: string; processed: number; migrated: number;
  failures?: { type: string; record: string; step: number; reason: string }[];
};

type PlanRecord = {
  id: string; revision: number; name: string; title: string; plan: Api.MigrationPlan;
  definitionVersion?: number; published?: string;
};

function planSignature(plan: Api.MigrationPlan): string {
  return JSON.stringify(plan, (_, value) => value && typeof value === "object" && !Array.isArray(value)
    ? Object.fromEntries(Object.entries(value).sort(([a], [b]) => a.localeCompare(b))) : value);
}

/** A migration stays in the original release task. Decisions use the original
 * record submitter/outbox; progress is read from accepted build records. */
export function MigrationWorkbench({ from, to, assets, onChanged, onLock }: {
  from: string; to: string; assets: Api.ReleaseAsset[];
  onChanged: () => Promise<void>; onLock: (locked: boolean) => void;
}) {
  const host = useHost(), inputs = useInputDrafts();
  const trial = useActionTrial(), declarationFields = host.source.entity("build.migration-plan")?.fields ?? [];
  const [createID] = useState(() => newId("MPLAN"));
  const [planTitle, setPlanTitle] = useState(""), [planName, setPlanName] = useState("");
  const inventory = useRecordInventory<Migration>("build.migration");
  const plans = useRecordInventory<PlanRecord>("build.migration-plan");
  const objects = assets.filter(asset => asset.ref.app === "build" && asset.ref.kind === "object");
  const [type, setType] = useState("");
  const [steps, setSteps] = useState<Api.MigrateStep[]>([]);
  const [stepsValid, setStepsValid] = useState(true);
  const [where, setWhere] = useState<Api.Expr>();
  const [review, setReview] = useState<Api.MigrationReview>();
  const [id, setID] = useState("");
  const [planID, setPlanID] = useState("");
  const [savedSignature, setSavedSignature] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [uncertain, setUncertain] = useState(false);
  const live = useRef(true), lock = useRef(false);
  const plan: Api.MigrationPlan = { from, to, scope: { type, ...(where ? { where } : {}) }, steps };
  const signature = planSignature(plan), context = useRef(signature); context.current = signature;
  const runs = (inventory.isError ? [] : inventory.data?.records ?? []).filter(run => run.plan.from === from && run.plan.to === to);
  const run = runs.find(run => run.id === id) ?? runs.find(run => run.state !== "cancelled");
  const savedPlans = (plans.data?.records ?? []).filter(item => item.plan.from === from && item.plan.to === to);
  const savedPlan = savedPlans.find(item => item.id === planID);
  const pinned: Api.ReleaseAsset | undefined = savedPlan?.published ? JSON.parse(savedPlan.published) : undefined;
  const saved = signature === savedSignature && (!planName || planName === savedPlan?.name) && (!planTitle || planTitle === savedPlan?.title);
  const published = pinned && planSignature((pinned.body as { plan: Api.MigrationPlan }).plan) === signature;
  const pending = host.outbox.some(entry => ["build.migration", "build.migration-plan"].includes(entry.submission.target?.type ?? "")
    && ["SUBMISSION_STATE_PENDING", "SUBMISSION_STATE_SENDING", "SUBMISSION_STATE_UNKNOWN"].includes(entry.state));
  const blocked = busy || uncertain || pending;
  const dirty = steps.length > 0 && !run && !saved;
  const submit = useMemo(() => createRecordActionSubmitter(host, () => live.current), [host.client]);
  const { markSaved, discardChanges } = useUnsavedChanges(dirty || uncertain, () => {
    setSteps([]); setStepsValid(true); setWhere(undefined); setReview(undefined); setError(""); setUncertain(false);
    setPlanID(""); setSavedSignature(""); setPlanName(""); setPlanTitle("");
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
    if (!review || !published || !saved) return;
    const target = id || newId("MIGRATION"); setID(target);
    return decide("build.migration.start", target, 0, { asset: { ref: pinned.ref, sourceVersion: pinned.sourceVersion }, review: review.id, reviewedAt: review.at });
  };
  const choosePlan = (item?: PlanRecord) => {
    setPlanTitle(item?.title ?? ""); setPlanName(item?.name ?? "");
    setPlanID(item?.id ?? ""); setReview(undefined); setError("");
    if (!item) { setSavedSignature(""); return; }
    setType(item.plan.scope.type); setSteps(item.plan.steps); setStepsValid(true); setWhere(item.plan.scope.where);
    setSavedSignature(planSignature(item.plan)); markSaved();
  };
  const savePlan = (publish = false) => perform(async () => {
    const target = planID || createID; setPlanID(target);
    const payload = publish ? {} : { plan, name: planName || savedPlan?.name || target.toLowerCase().replace(/[^a-z0-9]/g, ""), title: planTitle || savedPlan?.title || `${t("Migration plan")} · ${type}` };
    const schema = `build.migration-plan.${publish ? "publish" : savedPlan ? "edit" : "create"}`;
    const answer = await submit(schema, { type: "build.migration-plan", id: target }, savedPlan?.revision ?? 0, payload);
    if (!live.current) return;
    if (!answer.accepted) {
      setUncertain(host.client.authorities.outbox.some(entry => entry.submission.target?.id === target && ["SUBMISSION_STATE_PENDING", "SUBMISSION_STATE_SENDING", "SUBMISSION_STATE_UNKNOWN"].includes(entry.state)));
      throw Error(answer.error ?? t("The migration decision was not confirmed. Retry the original decision."));
    }
    setUncertain(false);
    const refreshed = await plans.refetch();
    choosePlan(refreshed.data?.records.find(item => item.id === target));
  });
  return <Card className="grid gap-3 p-3" role="region" aria-label={t("Data migration")}>
    <h3 className="text-sm font-semibold">{t("Data migration")}</h3>
    <p className="text-xs text-muted">{t("Declare the changes between these sealed releases. Each accepted batch saves its data and progress; the source remains read only until activation or cancellation.")}</p>
    <p className="break-all text-xs">{t("Source release")}: <code>{from}</code><br />{t("Target release")}: <code>{to}</code></p>
    {inventory.isError && <p role="alert" className="text-sm text-danger">{t("Migration progress could not be loaded. Refresh before starting or continuing.")}</p>}
    {runs.length > 0 && <label className="grid gap-1 text-xs">{t("Saved migration")}<Select aria-label={t("Saved migration")} disabled={blocked} value={run?.id ?? ""} onChange={event => setID(event.target.value)}>
      {!runs.some(item => item.state !== "cancelled") && <option value="">{t("New migration")}</option>}
      {runs.map(item => <option key={item.id} value={item.id}>{item.id} · {t(item.state)}</option>)}
    </Select></label>}
    {!run && <StructuredEditor expectedRevision={savedPlan?.revision ?? 0} fieldInput={sourceFieldInput} value={{ name: planName || savedPlan?.name || createID.toLowerCase().replace(/[^a-z0-9]/g, ""), title: planTitle || savedPlan?.title || `${t("Migration plan")} · ${type}`, plan }} fields={declarationFields} scope={`build.migration-plan:${planID || createID}:${host.source.scope}`} disabled={blocked} validate={value => { const candidate = value.plan as Api.MigrationPlan; return candidate?.from !== from || candidate?.to !== to ? [{ path: ["plan"], code: "release", message: t("Keep the selected source and target releases.") }] : []; }} onChange={value => { const candidate = value.plan as Api.MigrationPlan; setType(candidate.scope.type); setWhere(candidate.scope.where); setSteps(candidate.steps); setPlanName(value.name as string); setPlanTitle(value.title as string); setStepsValid(true); setError(""); }} onTrial={value => trial(`build.migration-plan.${savedPlan ? "edit" : "create"}`, { type: "build.migration-plan", id: planID || createID }, value, savedPlan?.revision ?? 0)}>
      <label className="grid gap-1 text-xs">{t("Saved migration plan")}<Select aria-label={t("Saved migration plan")} disabled={blocked} value={planID} onChange={event => choosePlan(savedPlans.find(item => item.id === event.target.value))}>
        <option value="">{t("New migration plan")}</option>
        {savedPlans.map(item => <option key={item.id} value={item.id}>{item.title} · {item.definitionVersion ?? 0}</option>)}
      </Select></label>
      <label className="grid gap-1 text-xs">{t("Migration object")}<Select aria-label={t("Migration object")} disabled={blocked} value={type} onChange={event => setType(event.target.value)}>
        <option value="">{t("Choose a migration object")}</option>
        {objects.map(asset => <option key={asset.ref.name} value={asset.ref.name}>{asset.ref.name}</option>)}
      </Select></label>
      <MigrationStepsEditor value={steps} onChange={value => { setSteps(value); setStepsValid(true); setError(""); }} type={type} entities={host.entities} assets={objects} disabled={blocked} />
      <Checkbox checked={!!where} disabled={blocked} onChange={enabled => setWhere(enabled ? literalExpression("boolean") : undefined)}>{t("Filter migration records")}</Checkbox>
      {where && <fieldset disabled={blocked} className="min-w-0"><legend className="mb-2 text-xs">{t("Record filter expression")}</legend><ExpressionEditor value={where} onChange={setWhere} choices={migrationChoices(host.entities.find(entity => entity.type === type))} /></fieldset>}
      <div className="flex flex-wrap gap-2">
        <Button variant="ghost" disabled={!type || !steps.length || !stepsValid || blocked || plans.isError || plans.isPending} onClick={() => void savePlan()}>{t("Save migration plan")}</Button>
        <Button variant="ghost" disabled={!savedPlan || !saved || blocked} onClick={() => void savePlan(true)}>{t("Publish migration plan")}</Button>
        <Button disabled={!type || !steps.length || !stepsValid || blocked || !!inputs?.invalid} onClick={() => void inspect()}>{t("Review migration")}</Button>
        <Button variant="ghost" disabled={blocked} onClick={() => discardChanges()}>{t("Discard migration draft")}</Button>
      </div>
      {review && <div className="grid gap-2 text-xs" role="region" aria-label={t("Migration review")}>
        <ul className="grid gap-1">{review.types.map(item => <li key={item.type}>{item.type} · {t("{count} existing records", { count: item.records })}</li>)}</ul>
        <p>{t("{count} migration steps", { count: review.steps })} · {new Date(review.at).toLocaleString()}</p>
        {review.issues.length > 0 && <ul className="grid gap-1 text-warning" aria-label={t("Migration issues")}>{review.issues.map((issue, index) => <li key={index}>{issue.type}/{issue.record} · {t("Step {number}", { number: issue.step })}: {issue.reason}</li>)}</ul>}
        <Button disabled={!saved || !published || blocked || inventory.isError || inventory.isPending || !!inputs?.invalid} onClick={() => void start()}>{t("Start reviewed migration")}</Button>
      </div>}
    </StructuredEditor>}
    {run && <div className="grid gap-2" role="region" aria-label={t("Migration progress")}>
      <p className="text-sm" role="status">{t("Migration state")}: {t(run.state)} · {t("Processed")}: {run.processed} · {t("Migrated")}: {run.migrated}</p>
      <p className="break-all text-xs">{t(run.state === "complete" ? "Completed steps" : "Next step")}: {run.state === "complete" ? run.step : Math.min(run.step + 1, run.plan.steps.length)} / {run.plan.steps.length}{run.cursor ? ` · ${run.cursor}` : ""}</p>
      <Disclosure summary={t("Accepted migration plan")}><pre className="max-h-72 overflow-auto whitespace-pre-wrap text-xs">{JSON.stringify(run.plan, null, 2)}</pre></Disclosure>
      {run.failures?.length ? <ul className="grid gap-1 text-xs text-warning" aria-label={t("Migration failures")}>{run.failures.map((failure, index) => <li key={index}>{failure.type}/{failure.record} · {t("Step {number}", { number: failure.step })}: {failure.reason}</li>)}</ul> : null}
      {run.plan.steps.some(step => step.kind === "retire-action") && <p className="text-xs">{t("Already rejected approval requests remain rejected if this migration is cancelled.")}</p>}
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
    <Button size="sm" variant="ghost" disabled={busy} onClick={() => void perform(async () => { await inventory.refetch(); await plans.refetch(); await onChanged(); })}>{t("Refresh migration progress")}</Button>
  </Card>;
}

const migrationKinds = ["add", "default", "rename", "split", "merge", "retype", "extract", "retire", "retire-action", "rebind"];
const migrationLabels: Record<string, string> = { add: "Add a field", default: "Fill missing values", rename: "Rename a field", split: "Split a field", merge: "Merge fields", retype: "Convert a field type", extract: "Extract records", retire: "Retire a field", "retire-action": "Retire an action", rebind: "Change a reference target" };
const migrationKind = (type: string) => ({ longtext: "text", number: "decimal", string: "text" } as Record<string, string>)[type] ?? type;
const migrationChoices = (entity?: EntityInfo): ExpressionChoice[] => [
  { value: "id", label: t("Record ID"), type: "reference", ref: entity?.type },
  ...(entity?.fields ?? []).map(field => ({ value: field.name, label: field.title || field.name, type: migrationKind(field.type), ref: field.ref })),
];

/** These controls edit the original plan. Only the release owner reviews and
 * executes transformations against the sealed source and target shapes. */
function MigrationStepsEditor({ value, onChange, type, entities, assets, disabled }: {
  value: Api.MigrateStep[]; onChange: (value: Api.MigrateStep[]) => void; type: string;
  entities: EntityInfo[]; assets: Api.ReleaseAsset[]; disabled: boolean;
}) {
  const [mappingError, setMappingError] = useState("");
  const patch = (index: number, change: Partial<Api.MigrateStep>) => onChange(value.map((step, at) => at === index ? { ...step, ...change } : step));
  const move = (index: number, direction: number) => {
    const next = [...value];
    [next[index], next[index + direction]] = [next[index + direction]!, next[index]!];
    onChange(next);
  };
  return <fieldset disabled={disabled} className="grid min-w-0 gap-3">
    <legend className="mb-2 text-xs font-medium">{t("Migration steps")}</legend>
    {!value.length && <p className="text-xs text-muted">{t("Add a step to describe how records change between these releases.")}</p>}
    {value.map((step, index) => {
      const source = entities.find(entity => entity.type === step.types[0]);
      const destination = assets.find(asset => asset.ref.name === (step.kind === "extract" ? step.target : step.types[0]));
      const fields = (destination?.body as { fields?: Api.FieldInfo[] } | undefined)?.fields ?? [];
      const choices = migrationChoices(source);
      const changeFields = (fields: Api.MigrateField[]) => patch(index, { fields });
      const changeField = (at: number, change: Partial<Api.MigrateField>) => changeFields((step.fields ?? []).map((field, fi) => fi === at ? { ...field, ...change } : field));
      return <div key={index} className="grid min-w-0 gap-3 rounded border border-border p-3" role="group" aria-label={t("Migration step {number}", { number: index + 1 })}>
        <div className="grid gap-3 sm:grid-cols-2">
          <label className="grid gap-1 text-xs">{t("Step kind")}<Select value={step.kind} onChange={event => {
            const kind = event.target.value as Api.MigrateStep["kind"];
            patch(index, { kind, onFailure: step.onFailure || "reject", fields: kind === "retire-action" ? undefined : [{ from: "", to: "" }], expr: undefined, mapping: undefined, action: undefined, redirect: undefined, target: undefined, identity: kind === "extract" ? { op: "", kind: "reference", ref: "id" } : undefined });
          }}>{migrationKinds.map(kind => <option key={kind} value={kind}>{t(migrationLabels[kind]!)}</option>)}</Select></label>
          <label className="grid gap-1 text-xs">{t("Records per batch")}<Input type="number" min={1} max={256} value={step.batch} onChange={event => patch(index, { batch: Number(event.target.value) })} /></label>
          <label className="grid gap-1 text-xs">{t("Conversion failure")}<Select value={step.onFailure ?? "reject"} onChange={event => patch(index, { onFailure: event.target.value as Api.MigrateStep["onFailure"] })}><option value="reject">{t("Reject the whole batch")}</option><option value="quarantine">{t("Keep failed rows for review")}</option></Select></label>
          {step.kind === "extract" && <label className="grid gap-1 text-xs">{t("Target object")}<Select value={step.target ?? ""} onChange={event => patch(index, { target: event.target.value })}><option value="">{t("Choose a migration object")}</option>{assets.filter(asset => !step.types.includes(asset.ref.name)).map(asset => <option key={asset.ref.name} value={asset.ref.name}>{asset.ref.name}</option>)}</Select></label>}
        </div>
        <fieldset className="grid gap-1"><legend className="text-xs">{t("Object types in this step")}</legend><Toggles value={step.types} options={assets.map(asset => ({ value: asset.ref.name, label: asset.ref.name }))} onChange={types => patch(index, { types })} /></fieldset>
        {step.kind === "retire-action" ? <div className="grid gap-3 sm:grid-cols-2">
          <label className="grid gap-1 text-xs">{t("Action to retire")}<Input value={step.action ?? ""} onChange={event => patch(index, { action: event.target.value })} /></label>
          <label className="grid gap-1 text-xs">{t("Redirect pending approvals to action")}<Input value={step.redirect ?? ""} onChange={event => patch(index, { redirect: event.target.value || undefined })} /></label>
        </div> : <>
          {step.kind === "extract" && step.identity && <div className="grid gap-1"><span className="text-xs">{t("New record identity")}</span><ExpressionEditor value={step.identity} kind={step.identity.kind} choices={choices} onChange={identity => patch(index, { identity })} /></div>}
          {(step.fields ?? []).map((field, at) => {
            const target = fields.find(info => info.name === field.to);
            return <div key={at} className="grid min-w-0 gap-2 border-t border-border pt-3">
              <div className="grid gap-3 sm:grid-cols-2">
                <label className="grid gap-1 text-xs">{t("Source field")}<Select value={field.from ?? ""} onChange={event => changeField(at, { from: event.target.value || undefined, ...(step.kind === "retire" ? { to: event.target.value } : {}) })}><option value="">{t("No source field")}</option>{(source?.fields ?? []).map(info => <option key={info.name} value={info.name}>{info.title || info.name} · {info.name}</option>)}</Select></label>
                <label className="grid gap-1 text-xs">{t("Target field")}<Select disabled={step.kind === "retire"} value={field.to} onChange={event => changeField(at, { to: event.target.value })}><option value="">{t("Choose a target field")}</option>{fields.map(info => <option key={info.name} value={info.name}>{info.title || info.name} · {info.name}</option>)}</Select></label>
              </div>
              {!["rename", "retire", "rebind"].includes(step.kind) && <>
                <Checkbox checked={!!field.expr} onChange={enabled => changeField(at, { expr: enabled ? literalExpression(migrationKind(target?.type ?? "text")) : undefined })}>{t("Set a conversion expression")}</Checkbox>
                {field.expr && <ExpressionEditor value={field.expr} kind={migrationKind(target?.type ?? field.expr.kind)} choices={choices} onChange={expr => changeField(at, { expr })} />}
              </>}
              <Button size="sm" variant="ghost" className="justify-self-start" onClick={() => changeFields((step.fields ?? []).filter((_, fi) => fi !== at))}>{t("Remove field change")}</Button>
            </div>;
          })}
          {step.expr && <div className="grid gap-1"><span className="text-xs">{t("Default conversion expression")}</span><ExpressionEditor value={step.expr} kind={step.expr.kind} choices={choices} onChange={expr => patch(index, { expr })} /><Button size="sm" variant="ghost" className="justify-self-start" onClick={() => patch(index, { expr: undefined })}>{t("Remove default expression")}</Button></div>}
          <Button size="sm" variant="ghost" className="justify-self-start" onClick={() => changeFields([...(step.fields ?? []), { to: "" }])}>{t("Add field change")}</Button>
          {step.kind === "rebind" && <div className="grid gap-2">
            <span className="text-xs">{t("Reference identity mapping")}</span>
            {Object.entries(step.mapping ?? {}).map(([from, to], at) => <div key={at} className="grid gap-2 sm:grid-cols-[1fr_1fr_auto]">
              <Input aria-label={t("Source record ID")} value={from} onChange={event => {
                const source = event.target.value;
                if (source !== from && Object.hasOwn(step.mapping ?? {}, source)) { setMappingError(t("Reference identities must be unique. The existing mapping is retained.")); return; }
                setMappingError("");
                patch(index, { mapping: Object.fromEntries(Object.entries(step.mapping ?? {}).map((entry, i) => i === at ? [source, to] : entry)) });
              }} />
              <Input aria-label={t("Target record ID")} value={to} onChange={event => patch(index, { mapping: { ...step.mapping, [from]: event.target.value } })} />
              <Button size="sm" variant="ghost" onClick={() => patch(index, { mapping: Object.fromEntries(Object.entries(step.mapping ?? {}).filter((_, i) => i !== at)) })}>{t("Remove mapping")}</Button>
            </div>)}
            <Button size="sm" variant="ghost" className="justify-self-start" disabled={Object.hasOwn(step.mapping ?? {}, "")} onClick={() => patch(index, { mapping: { ...step.mapping, "": "" } })}>{t("Add mapping")}</Button>
          </div>}
        </>}
        <div className="flex flex-wrap gap-2">
          <Button size="sm" variant="ghost" disabled={!index} onClick={() => move(index, -1)}>{t("Move step up")}</Button>
          <Button size="sm" variant="ghost" disabled={index === value.length - 1} onClick={() => move(index, 1)}>{t("Move step down")}</Button>
          <Button size="sm" variant="ghost" onClick={() => onChange(value.filter((_, at) => at !== index))}>{t("Remove step")}</Button>
        </div>
      </div>;
    })}
    {mappingError && <p role="alert" className="text-xs text-danger">{mappingError}</p>}
    <Button size="sm" variant="ghost" className="justify-self-start" disabled={!type || value.length >= 256} onClick={() => onChange([...value, { kind: "rename", types: [type], batch: 64, fields: [{ from: "", to: "" }] }])}>{t("Add migration step")}</Button>
  </fieldset>;
}
