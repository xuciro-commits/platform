import { sourceFieldInput } from "@platform/app";
import { StructuredEditor } from "@platform/ui";
import { useActionTrial } from "@platform/app";
import { declarationValue, replaceDeclaration } from "../editor/declaration";
import { useEffect, useRef, useState } from "react";
import type { Api } from "@platform/kernel";
import { ObservationRead, SemanticObjectSelect, SemanticPropertySelect, useHost, useProvidedCapabilities, useReadQuery, useRecordInventory, useTransition } from "@platform/app";
import { Button, Checkbox, Input, PageHeader, Panel, Select, Textarea, Toggles, t, useInputDrafts, useUnsavedChanges } from "@platform/ui";
import { ExpressionEditor, literalExpression, type ExpressionChoice } from "../editor/ExpressionEditor";
import { ResourceControls } from "../editor/workbench";
import { ResourceList } from "../editor/ResourceList";
import { useApplicationWorkspace } from "../projects/application-scope";

type ObservationDraft = Omit<Api.ObservationDefinition, "connection" | "object"> & {
  id: string; revision: number; created?: Api.Stamp; changed?: Api.Stamp; connection: string; object: string; state: string; published?: string; definitionVersion?: number;
};
const empty = (): ObservationDraft => ({ id: "", revision: 0, name: "", title: "", description: "", connection: "", object: "", deviceField: "", signal: "", unit: "", path: "/observations", windowSeconds: 300, expectedRetentionSeconds: 86400, expectedGranularitySeconds: 1, roles: ["user", "builder"], state: "draft", scheduled: false, requireComplete: true });

export function ObservationSources() {
  const { source, can } = useHost(), { open } = useApplicationWorkspace();
  return <div className="grid gap-3"><PageHeader title={t("Observation sources")} help={t("Declare external time-series reads and the business decision that may consume their summary.")} actions={<><Button variant="ghost" onClick={() => open({ view: "observations" })}>{t("Read observations")}</Button>{can("build.observation.create") && <Button variant="primary" onClick={() => open({ view: "observation-source", params: { id: "new" } })}>{t("New observation source")}</Button>}</>} />
    <ResourceList source={source} type="build.observation" fields={["title", "name", "object", "signal", "state", "definitionVersion"]} onOpen={record => open({ view: "observation-source", params: { id: record.id } })} />
  </div>;
}

export function ObservationSourceEditor({ id }: { id: string }) {
  const { decide, can, definitions, source, catalog } = useHost(), { open, close } = useApplicationWorkspace();
  const query = useReadQuery<{ record?: ObservationDraft }>(`/v1/records/build.observation/${encodeURIComponent(id)}`, undefined, id !== "new");
  const connections = useRecordInventory<Api.Connection>("build.connection");
  const capabilities = useProvidedCapabilities("action", "authoritative");
  const ownerApps = useReadQuery<Api.AppInfo[]>("/v1/apps");
  const published = useReadQuery<Api.ObservationInfo[]>("/v1/observations");
  const transitions = useTransition("build.observation");
  const authoringHost = useHost(), trial = useActionTrial(), declarationFields = authoringHost.source.entity("build.observation")?.fields ?? [];
  const [draft, setDraft] = useState<ObservationDraft>(empty), [edited, setEdited] = useState(false), [busy, setBusy] = useState(false), [error, setError] = useState("");
  const [createID] = useState(() => crypto.randomUUID());
  const inputDrafts = useInputDrafts(), dirty = edited || !!inputDrafts?.dirty;
  const original = useRef<ObservationDraft>(empty()), loaded = useRef(""), lock = useRef(false);
  const load = (value: ObservationDraft) => { original.current = structuredClone({ ...empty(), ...value, requireComplete: value.requireComplete ?? false }); loaded.current = `${value.id}:${value.revision}`; setDraft(original.current); };
  useEffect(() => { const record = query.data?.record; if (record && !dirty && !busy && loaded.current !== `${record.id}:${record.revision}`) load(record); }, [query.data, dirty, busy]);
  const { markSaved, discardChanges } = useUnsavedChanges(dirty, () => { setDraft(structuredClone(original.current)); setEdited(false); setError(""); }, busy);
  const patch = (change: Partial<ObservationDraft>) => { if (!lock.current) { setDraft(value => ({ ...value, ...change })); setEdited(true); setError(""); } };
  const perform = async (operation: () => Promise<unknown>) => { if (lock.current) return; lock.current = true; setBusy(true); setError(""); try { await operation(); } catch (problem) { setError(problem instanceof Error ? problem.message : t("The observation source could not be saved. Your draft is still here.")); } finally { lock.current = false; setBusy(false); } };
  const save = async () => {
    if (inputDrafts?.invalid) return;
    const snapshot = structuredClone(draft), target = snapshot.id || createID;
    const { name, title, description, connection, object, deviceField, signal, unit, path, windowSeconds, expectedRetentionSeconds, expectedGranularitySeconds, roles, decision, scheduled, requireComplete } = snapshot;
    const payload = { ...declarationValue(snapshot, declarationFields), name, title, description, connection, object, deviceField, signal, unit, path, windowSeconds, expectedRetentionSeconds, expectedGranularitySeconds, roles, scheduled: !!decision && !!scheduled, decision: decision ?? null, intervals:snapshot.intervals??null, requireComplete: !!requireComplete };
    if (!await decide(`build.observation.${snapshot.id ? "edit" : "create"}`, { type: "build.observation", id: target }, payload, { expectedRevision: snapshot.id ? snapshot.revision : 0, quiet: true, onRefused: setError })) return;
    load({ ...snapshot, id: target, revision: (snapshot.id ? snapshot.revision : 0) + 1 }); markSaved(); setEdited(false);
    if (!snapshot.id) { open({ view: "observation-source", params: { id: target } }); close({ view: "observation-source", params: { id } }); }
    else { const refreshed = await query.refetch(); if (refreshed.isSuccess && refreshed.data?.record) load(refreshed.data.record); }
    return target;
  };
  const review = async () => { const target = dirty || !draft.id ? await save() : draft.id; if (target) open({ view: "release-review", params: { kind: "observation", id: target } }); };
  const install = async () => { if (!draft.id || dirty) return; if (await decide("build.observation.publish", { type: "build.observation", id: draft.id }, {}, { expectedRevision: draft.revision, quiet: true, onRefused: setError })) { const refreshed = await query.refetch(); if (refreshed.isSuccess && refreshed.data?.record) load(refreshed.data.record); } };
  const object = definitions.find(item => item.ref.kind === "object" && item.ref.name === draft.object);
  const fields = object?.entity?.fields ?? [];
  const allowed = capabilities.data ?? [];
  const actions = catalog.filter(action => action.target === draft.object && !action.new && allowed.some(item => item.capability.name === action.schema));
  const selectedAction = catalog.find(action => action.schema === draft.decision?.action.name);
  const choices: ExpressionChoice[] = [{ value: "record.id", label: t("Record ID"), type: "text" }, ...(draft.decision?.mode === "intervals" ? [{ value: "intervals.count", label: t("Matching interval count"), type: "integer" }, { value: "intervals.since", label: t("Window starts at"), type: "datetime" }, { value: "intervals.until", label: t("Window ends at"), type: "datetime" }] : [{ value: "observation.latest", label: t("Latest observation"), type: "decimal" }, { value: "observation.lastat", label: t("Latest sample time"), type: "datetime" }]), ...fields.filter(field => ["text", "longtext", "choice", "reference", "integer", "decimal", "boolean", "date", "datetime"].includes(field.type)).map(field => ({ value: `record.${field.name}`, label: `${t("Record")}: ${field.title}`, type: field.type, ref: field.ref }))];
  const updateDecision = (change: Partial<Api.ObservationDecision>) => { if (draft.decision) patch({ decision: { ...draft.decision, ...change } }); };
  const updateIntervals=(change:Partial<Api.ObservationIntervals>)=>{if(draft.intervals)patch({intervals:{...draft.intervals,...change}});};
  const sourceRoles = ownerApps.data?.find(app => app.id === "build")?.roles ?? [];
  const installed = published.data?.find(item => item.ref.app === "build" && item.ref.name === draft.name);
  const protocol = connections.data?.records.find(connection => connection.id === draft.connection)?.protocol;
  const points = protocol?.configuration.points.filter(point => ["decimal", "integer", "number"].includes(point.type) && !!point.unit) ?? [];
  if (id !== "new" && !draft.id) return <PageHeader title={t("Observation source")} description={query.isError ? t("The observation source could not be loaded.") : t("Loading…")} />;
  const editable = can(`build.observation.${draft.id ? "edit" : "create"}`);
  return <div className="grid min-w-0 gap-4">
    <PageHeader title={draft.title || t("New observation source")} help={t("The source stores a portable declaration. Connection addresses, credentials and original points stay outside the artifact.")} actions={<>
      <Button variant="ghost" onClick={() => open({ view: "observation-source" })}>{t("Observation sources")}</Button>
      <ResourceControls type="build.observation" record={draft} dirty={dirty} busy={busy} onCancel={discardChanges} onSave={editable ? () => void perform(save) : undefined} onReview={editable ? () => void perform(review) : undefined} onInstall={editable ? () => void perform(install) : undefined} invalid={!!inputDrafts?.invalid} route={{ view: "observation-source", params: { id } }} />
      {draft.state === "published" && can("build.observation.retire") && <Button disabled={busy || dirty} onClick={() => transitions.take("build.observation.retire", { ...draft, created: draft.created ?? {}, changed: draft.changed ?? {} })}>{t("Retire source")}</Button>}
    </>} />
    {error && <Panel role="alert" className="text-danger">{error}</Panel>}
    <StructuredEditor initialMode="source" expectedRevision={draft.id ? draft.revision : 0} fieldInput={sourceFieldInput} value={declarationValue(draft, declarationFields)} fields={declarationFields} scope={`build.observation:${id}:${authoringHost.source.scope}`} readOnly={!editable} disabled={busy} onChange={next => { if (lock.current) return; setDraft(current => ({ ...empty(), ...replaceDeclaration(current, next, declarationFields) })); setEdited(true); setError(""); }} onTrial={next => trial(`build.observation.${draft.id ? "edit" : "create"}`, { type: "build.observation", id: draft.id || createID }, next, draft.id ? draft.revision : 0)}><fieldset disabled={busy || !editable} className="grid min-w-0 gap-3 lg:grid-cols-2">
      <Panel title={t("Observation source")} className="grid content-start gap-3">
        <label className="grid gap-1 text-xs">{t("Observation name")}<Input value={draft.name} disabled={!!draft.published} onChange={event => patch({ name: event.target.value })} /></label>
        <label className="grid gap-1 text-xs">{t("Observation title")}<Input value={draft.title} onChange={event => patch({ title: event.target.value })} /></label>
        <label className="grid gap-1 text-xs">{t("Description")}<Textarea value={draft.description ?? ""} onChange={event => patch({ description: event.target.value })} /></label>
        <label className="grid gap-1 text-xs">{t("Logical connection")}<Select value={draft.connection} onChange={event => {
          const connection = connections.data?.records.find(connection => connection.id === event.target.value);
          patch({ connection: event.target.value, signal: "", unit: "", path: connection?.protocol ? "/observation" : "/observations" });
        }}><option value="">{t("Choose a ready HTTP connection")}</option>{connections.data?.records.filter(connection => connection.kind === "http" && connection.state === "ready" && !connection.archived).map(connection => <option key={connection.id} value={connection.id}>{connection.title} · {connection.name}</option>)}</Select></label>
        <SemanticObjectSelect label={t("Observation object")} value={draft.object} onChange={ref => patch({ object: ref?.name ?? "", deviceField: "", decision: undefined, scheduled: false })} />
        <SemanticPropertySelect label={t("Device identity field")} object={object?.ref ?? { app: "", kind: "object", name: "" }} value={draft.deviceField} filter={field => ["text", "choice", "reference"].includes(field.type)} onChange={ref => patch({ deviceField: ref?.field ?? "" })} />
        {protocol ? <>
          <label className="grid gap-1 text-xs">{t("Protocol point")}<Select value={draft.signal} onChange={event => {
            const point = points.find(point => point.id === event.target.value);
            patch({ signal: point?.id ?? "", unit: point?.unit ?? "", path: "/observation" });
          }}><option value="">{t("Choose a numeric point with a unit")}</option>
            {draft.signal && !points.some(point => point.id === draft.signal) && <option value={draft.signal}>{t("Unavailable point: {name}", { name: draft.signal })}</option>}
            {points.map(point => <option key={point.id} value={point.id}>{protocol.configuration.devices.find(device => device.id === point.device)?.title ?? point.device} · {point.id} · {point.unit}</option>)}
          </Select></label>
          <label className="grid gap-1 text-xs">{t("Unit")}<Input value={draft.unit} readOnly /></label>
          <p className="text-xs text-muted">{t("Point identity, unit and read path come from the connection. Current values do not require historical storage.")}</p>
        </> : <>
          <label className="grid gap-1 text-xs">{t("Signal")}<Input value={draft.signal} onChange={event => patch({ signal: event.target.value })} /></label>
          <label className="grid gap-1 text-xs">{t("Unit")}<Input value={draft.unit} onChange={event => patch({ unit: event.target.value })} /></label>
          <label className="grid gap-1 text-xs">{t("Relative adapter path")}<Input value={draft.path} onChange={event => patch({ path: event.target.value })} /></label>
        </>}
        <fieldset className="grid gap-1 text-xs"><legend>{t("Source roles")}</legend><Toggles options={sourceRoles.map(role => ({ value: role, label: role }))} value={draft.roles} onChange={roles => patch({ roles })} /></fieldset>
        {[{ field: "windowSeconds", title: "Decision window (seconds)", min: 1 }, { field: "expectedRetentionSeconds", title: "Expected external retention (seconds)", min: 1 }, { field: "expectedGranularitySeconds", title: "Expected external granularity (seconds)", min: 1 }].map(item => <label key={item.field} className="grid gap-1 text-xs">{t(item.title)}<Input type="number" min={item.min} value={draft[item.field as "windowSeconds"]} onChange={event => patch({ [item.field]: Number(event.target.value) })} /></label>)}
        <Checkbox checked={!!draft.requireComplete} onChange={requireComplete => patch({ requireComplete })}>{t("Require complete, good-quality windows")}</Checkbox>
        <p className="text-xs text-muted">{t("Protocol connections always require complete coverage and good-quality samples before a window can decide an action.")}</p>
        <p className="text-xs text-muted">{t("Retention and granularity are expectations of the external store. The platform does not collect, store or downsample raw points.")}</p>
      </Panel>
      <Panel title={t("State interval analysis")} role="region" aria-label={t("State interval analysis")} className="grid content-start gap-3">
        <Checkbox checked={!!draft.intervals} onChange={checked=>patch({intervals:checked?{valueKind:"choice",when:literalExpression("boolean"),maxGapSeconds:Math.min(30,draft.windowSeconds),latePolicy:"correct",latenessSeconds:Math.min(60,draft.windowSeconds),maxIntervals:64}:undefined})}>{t("Analyze a state condition over the source window")}</Checkbox>
        <p className="text-xs text-muted">{t("The external source keeps raw samples. Missing time, bad quality and disallowed late samples remain unknown; a window boundary is not an observed event ending.")}</p>
        {draft.intervals&&<>
          <label className="grid gap-1 text-xs">{t("Sample value type")}<Select value={draft.intervals.valueKind} onChange={event=>updateIntervals({valueKind:event.target.value as Api.ObservationIntervals["valueKind"]})}><option value="choice">{t("Choice")}</option><option value="decimal">{t("Decimal")}</option><option value="boolean">{t("Boolean")}</option></Select></label>
          <label className="grid gap-1 text-xs">{t("Maximum sample gap (seconds)")}<Input type="number" min={1} max={draft.windowSeconds} value={draft.intervals.maxGapSeconds} onChange={event=>updateIntervals({maxGapSeconds:Number(event.target.value)})}/></label>
          <label className="grid gap-1 text-xs">{t("Late sample policy")}<Select value={draft.intervals.latePolicy} onChange={event=>updateIntervals({latePolicy:event.target.value as Api.ObservationIntervals["latePolicy"],...event.target.value==="reject"?{latenessSeconds:0}:{}})}><option value="reject">{t("Reject arrivals after the window")}</option><option value="correct">{t("Allow bounded late corrections")}</option></Select></label>
          {draft.intervals.latePolicy==="correct"&&<label className="grid gap-1 text-xs">{t("Allowed lateness (seconds)")}<Input type="number" min={0} max={draft.windowSeconds} value={draft.intervals.latenessSeconds} onChange={event=>updateIntervals({latenessSeconds:Number(event.target.value)})}/></label>}
          <label className="grid gap-1 text-xs">{t("Maximum interval results")}<Input type="number" min={1} max={256} value={draft.intervals.maxIntervals} onChange={event=>updateIntervals({maxIntervals:Number(event.target.value)})}/></label>
          <p className="text-xs font-medium">{t("Matching state condition")}</p>
          <ExpressionEditor value={draft.intervals.when} kind="boolean" choices={[{value:"sample.value",label:t("Sample value"),type:draft.intervals.valueKind},{value:"sample.at",label:t("Event time"),type:"datetime"},{value:"sample.receivedAt",label:t("Received at"),type:"datetime"}]} onChange={when=>updateIntervals({when})}/>
        </>}
      </Panel>
      <Panel title={t("Observation decision")} className="grid content-start gap-3">
        <Checkbox checked={!!draft.decision} onChange={checked => patch({ decision: checked ? { mode: draft.intervals ? "intervals" : "summary", when: literalExpression("boolean"), action: { app: object?.ref.app ?? "build", kind: "action", name: "" }, inputs: {} } : undefined, scheduled: checked ? draft.scheduled : false })}>{t("Evaluate a declared business action")}</Checkbox>
        {draft.decision && <>
          <label className="grid gap-1 text-xs">{t("Decision evidence")}<Select value={draft.decision.mode ?? "summary"} onChange={event => updateDecision({ mode: event.target.value as Api.ObservationDecision["mode"] })}><option value="summary">{t("External summary")}</option><option value="intervals" disabled={!draft.intervals}>{t("State intervals")}</option></Select></label>
          <Checkbox checked={!!draft.scheduled} onChange={scheduled => patch({ scheduled })}>{t("Evaluate completed windows automatically")}</Checkbox>
          <p className="text-xs text-muted">{t("Scheduled decisions run as the publishing member with current permissions.")}</p>
          <label className="grid gap-1 text-xs">{t("Original business action")}<Select value={draft.decision.action.name} onChange={event => { const selected = allowed.find(item => item.capability.name === event.target.value), action = actions.find(action => action.schema === event.target.value); if (selected && action) updateDecision({ action: selected.ref, inputs: Object.fromEntries(action.payload.filter(field => field.required).map(field => [field.name, literalExpression(({ string: "text", number: "decimal", integer: "integer", boolean: "boolean" } as Record<string, string>)[field.type] ?? "text")])) }); }}><option value="">{t("Choose an original action")}</option>{actions.map(action => <option key={action.schema} value={action.schema}>{action.title} · {action.schema}</option>)}</Select></label>
          <p className="text-xs font-semibold">{t("Match observation summary when")}</p>
          <ExpressionEditor value={draft.decision.when} choices={choices} lookup={source.entity} windows={draft.decision?.mode === "intervals" ? undefined : { observation: { unit: draft.unit } }} onChange={when => updateDecision({ when })} />
          {selectedAction?.payload.filter(field => field.type !== "json" && !field.type.endsWith("[]")).map(field => <div className="grid gap-2 border-t border-border pt-2" key={field.name}><Checkbox checked={!!draft.decision!.inputs[field.name]} onChange={checked => { const inputs = { ...draft.decision!.inputs }; if (checked) inputs[field.name] = literalExpression(({ string: "text", number: "decimal" } as Record<string, string>)[field.type] ?? field.type); else delete inputs[field.name]; updateDecision({ inputs }); }}>{field.description || field.name}{field.required ? " *" : ""}</Checkbox>{draft.decision!.inputs[field.name] && <ExpressionEditor value={draft.decision!.inputs[field.name]!} kind={draft.decision!.inputs[field.name]!.kind} choices={choices} lookup={source.entity} windows={draft.decision?.mode === "intervals" ? undefined : { observation: { unit: draft.unit } }} onChange={expression => updateDecision({ inputs: { ...draft.decision!.inputs, [field.name]: expression } })} />}</div>)}
          <p className="text-xs text-muted">{t("Rules see the external summary and original record fields. Only compact scalar inputs reach the original action; record state and permissions still decide the result.")}</p>
        </>}
      </Panel>
    </fieldset></StructuredEditor>
    {installed && <ObservationRead info={installed} />}
    {transitions.dialog}
  </div>;
}
