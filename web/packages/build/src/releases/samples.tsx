import { sourceFieldInput } from "@platform/app";
import { StructuredEditor } from "@platform/ui";
import { useActionTrial } from "@platform/app";
import { useEffect, useRef, useState } from "react";
import { useHost, useRecordInventory } from "@platform/app";
import { SystemDefinitionReferences } from "../editor/SystemDefinitions";
import { Button, Card, ContextHelp, Input, InputDraftProvider, InputProblems, PageHeader, Select, Textarea, t, useInputDrafts, useUnsavedChanges } from "@platform/ui";
import { apiErrorMessage, type Api } from "@platform/kernel";
import type { Project } from "../projects/project";
import { useApplicationScope, useApplicationWorkspace } from "../projects/application-scope";
import { joinProjectResource } from "../projects/membership";
import { useAuthoringCapabilities, useAuthoringModel } from "../ontology/authoring-model";
import { TestPayload } from "./test-inputs";

export type SampleSetRecord = { id: string; revision: number; name: string; title: string; samples: Api.SampleRecords[]; published?: string; definitionVersion?: number };
type SampleProps = { application: Project; selected: string; samples: string; onChange: (name: string, samples: string) => void; onInvalidChange?: (invalid: boolean) => void; useSavedValues?: boolean; document?: boolean; onSaved?: (record: SampleSetRecord) => void };
const objectValue = (value: unknown): value is Record<string, unknown> => value !== null && typeof value === "object" && !Array.isArray(value);
const failureMessage = (error: unknown) => apiErrorMessage(error) ?? (error instanceof Error ? error.message : t("The application sample could not be saved."));

export function ApplicationSamples(props: SampleProps) {
  return <InputDraftProvider isolated scope="application-samples"><ApplicationSampleForm {...props} /></InputDraftProvider>;
}

function ApplicationSampleForm({ application, selected, samples: suppliedSamples, onChange, onInvalidChange, useSavedValues = false, document = false, onSaved }: SampleProps) {
  const host = useHost(), inventory = useRecordInventory<SampleSetRecord>("build.sample-set"), inputs = useInputDrafts();
  useEffect(() => {
    onInvalidChange?.(!!inputs?.invalid);
    return () => onInvalidChange?.(false);
  }, [inputs?.invalid, onInvalidChange]);
  const trial = useActionTrial(), declarationFields = host.source.entity("build.sample-set")?.fields ?? [];
  let parsedSamples: unknown; try { parsedSamples = JSON.parse(suppliedSamples); } catch { /* The existing sample fields retain incomplete JSON. */ }
  const record = inventory.data?.records.find(record => record.name === selected);
  const base = useRef(record), selection = useRef(selected), lock = useRef(false);
  const samples = useSavedValues && selected
    ? JSON.stringify((base.current?.name === selected ? base.current : record)?.samples ?? [], null, 2) : suppliedSamples;
  const [baseline, setBaseline] = useState(record ? JSON.stringify(record.samples, null, 2) : "[]");
  const [name, setName] = useState(record?.name ?? ""), [title, setTitle] = useState(record?.title ?? "");
  const [createId, setCreateId] = useState(() => crypto.randomUUID());
  const [edited, setEdited] = useState(false), [busy, setBusy] = useState(false), [error, setError] = useState("");
  const [membershipError, setMembershipError] = useState(""), [joining, setJoining] = useState(false), [saved, setSaved] = useState(false);
  const dirty = edited || !!inputs?.dirty || !!base.current && samples !== baseline;
  const change = () => { setEdited(true); setSaved(false); setError(""); };
  const load = (next?: SampleSetRecord) => {
    base.current = next; selection.current = next?.name ?? "";
    const rows = JSON.stringify(next?.samples ?? [], null, 2);
    setName(next?.name ?? ""); setTitle(next?.title ?? ""); setBaseline(rows);
    setEdited(false); setSaved(false); setError(""); setMembershipError(""); setJoining(false);
    onChange(next?.name ?? "", rows);
  };
  const { markSaved, discardChanges, confirmOwnDiscard } = useUnsavedChanges(dirty, () => {
    const current = base.current;
    setName(current?.name ?? ""); setTitle(current?.title ?? "");
    if (!useSavedValues || current?.name !== selected) onChange(current?.name ?? "", baseline);
    setEdited(false); setSaved(false); setError("");
  }, busy);
  useEffect(() => {
    if (busy) return;
    if (selection.current !== selected) {
      selection.current = selected;
      if (base.current?.name !== selected) {
        base.current = record; setName(record?.name ?? ""); setTitle(record?.title ?? "");
        setBaseline(JSON.stringify(record?.samples ?? [], null, 2)); setEdited(false); setSaved(false);
        setError(""); setMembershipError(""); setJoining(false); inputs?.clear();
      }
    }
    if (!record || edited || inputs?.dirty || base.current && (samples !== baseline || record.revision <= base.current.revision)) return;
    const refreshSamples = !!base.current;
    base.current = record; setName(record.name); setTitle(record.title);
    const rows = JSON.stringify(record.samples, null, 2); setBaseline(rows);
    if (!useSavedValues && refreshSamples && rows !== samples) onChange(record.name, rows);
  }, [selected, record?.id, record?.revision, busy, edited, inputs?.dirty, samples, baseline, useSavedValues]);

  const save = async () => {
    if (lock.current || inputs?.invalid) return;
    lock.current = true; setBusy(true); setError(""); setSaved(false);
    try {
      let current = base.current;
      if (!current || dirty) {
        let rows: Api.SampleRecords[];
        try { rows = JSON.parse(samples); if (!Array.isArray(rows)) throw Error(); }
        catch { setError(t("Use a JSON array of synthetic model records.")); return; }
        const id = current?.id ?? createId, revision = current?.revision ?? 0;
        if (!await host.decide(`build.sample-set.${current ? "edit" : "create"}`, { type: "build.sample-set", id }, { name, title, samples: rows }, {
          expectedRevision: revision, quiet: true, onRefused: setError,
        })) return;
        current = { ...current, id, revision: revision + 1, name, title, samples: rows };
        base.current = current; selection.current = name; setBaseline(samples); setEdited(false);
        inputs?.clear(); markSaved();
        if (!useSavedValues || selected !== name) onChange(name, samples);
      }
      setJoining(true); setMembershipError("");
      try {
        await joinProjectResource(host, application.id, { app: "build", kind: "sample-set", name: current.name });
      } catch (error) { setMembershipError(failureMessage(error)); return; }
      setJoining(false); setSaved(true); markSaved();
      void inventory.refetch(); onSaved?.(current);
    } catch (error) { setError(failureMessage(error)); }
    finally { lock.current = false; setBusy(false); }
  };
  const missing = !!selected && !record && !base.current;
  return <Card className="grid gap-2 p-3" role="region" aria-label={t("Application samples")}>
    <div className="flex items-center justify-between gap-2"><h3 className="text-sm font-semibold">{t("Application samples")}</h3><ContextHelp title={t("Application samples")}>{t("Prepare synthetic records and references. Trial edits remain temporary until you explicitly save the sample.")}</ContextHelp></div>
    <StructuredEditor expectedRevision={base.current?.revision ?? 0} fieldInput={sourceFieldInput} value={{ name, title, samples: parsedSamples ?? [] }} fields={declarationFields} scope={`build.sample-set:${base.current?.id ?? createId}:${host.source.scope}`} disabled={busy || missing} onChange={next => { setName(next.name as string); setTitle(next.title as string); change(); onChange(next.name as string, JSON.stringify(next.samples ?? [], null, 2)); }} onTrial={next => trial(`build.sample-set.${base.current ? "edit" : "create"}`, { type: "build.sample-set", id: base.current?.id ?? createId }, next, base.current?.revision ?? 0)}>    <fieldset disabled={busy || missing} className="grid gap-2">
      {!document && <label className="grid gap-1 text-xs">{t("Sample set")}<Select value={selected} onChange={event => {
        const next = inventory.data?.records.find(record => record.name === event.target.value);
        confirmOwnDiscard(() => { inputs?.clear(); setCreateId(crypto.randomUUID()); load(next); markSaved(); });
      }}>
        <option value="">{t("New sample set")}</option>
        {inventory.data?.records.map(record => <option key={record.id} value={record.name}>{record.title}{application.resources?.some(ref => ref.app === "build" && ref.kind === "sample-set" && ref.name === record.name) ? "" : " · " + t("Add to application")}</option>)}
      </Select></label>}
      <div className="grid gap-2 sm:grid-cols-2">
        <label className="grid gap-1 text-xs">{t("Sample set name")}<Input value={name} disabled={!!base.current} onChange={event => { setName(event.target.value); change(); }} /></label>
        <label className="grid gap-1 text-xs">{t("Sample set title")}<Input value={title} onChange={event => { setTitle(event.target.value); change(); }} /></label>
      </div>
      <SyntheticRecords application={application} samples={samples} onChange={rows => { change(); onChange(selected, rows); }} />
      <div className="flex gap-2">
        <Button disabled={!name.trim() || !title.trim()} onClick={() => void save()}>{t(joining && !dirty ? "Retry adding sample to application" : "Save application sample")}</Button>
        {dirty && <Button variant="ghost" onClick={discardChanges}>{t("Discard")}</Button>}
      </div>
    </fieldset></StructuredEditor>
    {base.current?.definitionVersion ? <p className="text-xs">{t("Published sample version")}: {base.current.definitionVersion}</p> : null}
    {saved && <p role="status" className="text-xs">{t("Application sample saved. Release review retains its version.")}</p>}
    {(error || membershipError || inventory.isError || missing) && <p role="alert" className="text-sm text-danger">{error || membershipError || t("The application sample could not be loaded.")}</p>}
    <InputProblems />
  </Card>;
}

function SyntheticRecords({ application, samples, onChange }: Pick<SampleProps, "application" | "samples"> & { onChange: (samples: string) => void }) {
  const host = useHost(), model = useAuthoringModel(), capabilities = useAuthoringCapabilities(), inputs = useInputDrafts();
  const [advanced, setAdvanced] = useState(false);
  const keys = useRef(new Map<string, string>());
  const recordKey = (type: string, id: string) => {
    const identity = JSON.stringify([type, id]);
    if (!keys.current.has(identity)) keys.current.set(identity, crypto.randomUUID());
    return keys.current.get(identity)!;
  };
  let groups: Api.SampleRecords[] | undefined;
  try {
    const parsed: unknown = JSON.parse(samples);
    if (Array.isArray(parsed) && parsed.every(group => objectValue(group) && typeof group.type === "string" && Array.isArray(group.records)
      && Object.keys(group).every(key => key === "type" || key === "records")
      && group.records.every(row => objectValue(row) && typeof row.id === "string" && (row.state === undefined || typeof row.state === "string")))
      && new Set(parsed.map(group => group.type)).size === parsed.length
      && parsed.every(group => new Set(group.records.map((row: Record<string, unknown>) => row.id)).size === group.records.length)) groups = parsed;
  } catch { /* The full editor keeps incomplete and deliberately invalid input verbatim. */ }
  const members = model.entities.filter(entity => application.resources?.some(ref => ref.kind === "object" && ref.app === entity.app && ref.name === entity.type));
  const write = (next: Api.SampleRecords[]) => onChange(JSON.stringify(next, null, 2));
  const dropInputs = (key: string) => inputs?.clear(`${inputs.scope}/record-${key}/`);
  const recordFields = (type: string) => {
    const schema = `${type}.create`, capability = capabilities.data.find(item => item.kind === "action" && item.target === type && item.ref.name === schema);
    if (model.drafts.data?.records.some(draft => !draft.archived && `build.${draft.name}` === type)) return capability?.source === "draft" ? capability.parameters : undefined;
    return capability?.parameters ?? host.action(schema)?.payload;
  };
  const showFields = !!groups && !advanced;
  return <div className="grid gap-2">
    <Button size="sm" variant="ghost" className="justify-self-start" disabled={!groups} onClick={() => setAdvanced(!advanced)}>{t(showFields ? "Advanced sample JSON" : "Use sample fields")}</Button>
    {!showFields ? <label className="grid gap-1 text-xs">{t("Synthetic sample records (JSON)")}
      <Textarea aria-label={t("Synthetic sample records (JSON)")} rows={8} className="font-mono" value={samples} onChange={event => onChange(event.target.value)} />
    </label> : <>
      {groups!.map((group, groupIndex) => {
        const entity = model.entity(group.type), fields = recordFields(group.type), states = entity?.lifecycle?.states ?? [];
        const patchRecord = (index: number, row: unknown) => write(groups!.map((value, at) => at === groupIndex ? { ...value, records: value.records.map((value, at) => at === index ? row : value) } : value));
        const addRecord = () => {
          let number = 1; while (group.records.some(row => objectValue(row) && row.id === `TEST-${number}`)) number++;
          write(groups!.map((value, at) => at === groupIndex ? { ...value, records: [...value.records, { id: `TEST-${number}`, ...(states.length ? { state: states[0]!.name } : {}) }] } : value));
        };
        return <fieldset key={group.type} className="grid min-w-0 gap-3 rounded border border-border p-3">
          <legend className="px-1 text-sm">{entity?.title ?? group.type} · {group.type}</legend>
          <Button size="sm" variant="ghost" className="justify-self-end" onClick={() => {
            group.records.forEach(row => { if (objectValue(row)) dropInputs(recordKey(group.type, String(row.id))); });
            write(groups!.filter((_, at) => at !== groupIndex));
          }}>{t("Remove sample object type")}</Button>
          {group.records.map((value, index) => {
            const row = value as Record<string, unknown>, key = recordKey(group.type, row.id as string);
            const { id, ...properties } = row;
            if (states.length) delete properties.state;
            return <InputDraftProvider key={key} scope={`record-${key}`}><fieldset className="grid min-w-0 gap-2 rounded border border-border p-2">
              <div className="flex items-end gap-2">
                <label className="grid min-w-0 flex-1 gap-1 text-xs">{t("Synthetic record ID")}<Input value={id as string} onChange={event => {
                  keys.current.delete(JSON.stringify([group.type, id])); keys.current.set(JSON.stringify([group.type, event.target.value]), key);
                  patchRecord(index, { ...row, id: event.target.value });
                }} /></label>
                <Button size="sm" variant="ghost" onClick={() => {
                  dropInputs(key); write(groups!.map((value, at) => at === groupIndex ? { ...value, records: value.records.filter((_, at) => at !== index) } : value));
                }}>{t("Remove synthetic record")}</Button>
              </div>
              {states.length > 0 && <label className="grid gap-1 text-xs">{t("Synthetic record state")}<Select value={String(row.state ?? "")} onChange={event => patchRecord(index, { ...row, state: event.target.value })}>
                <option value="">—</option>{states.map(state => <option key={state.name} value={state.name}>{state.title}</option>)}
                {typeof row.state === "string" && row.state && !states.some(state => state.name === row.state) && <option value={row.state}>{row.state}</option>}
              </Select></label>}
              <TestPayload fields={fields} payload={JSON.stringify(properties, null, 2)} onChange={payload => {
                const next: unknown = JSON.parse(payload);
                patchRecord(index, objectValue(next) ? { id, ...(states.length && Object.hasOwn(row, "state") ? { state: row.state } : {}), ...next } : next);
              }} />
            </fieldset></InputDraftProvider>;
          })}
          <Button size="sm" className="justify-self-start" onClick={addRecord}>{t("Add synthetic record")}</Button>
        </fieldset>;
      })}
      <label className="grid gap-1 text-xs">{t("Add sample object type")}<Select aria-label={t("Add sample object type")} value="" onChange={event => {
        const type = event.target.value, entity = model.entity(type), initial = entity?.lifecycle?.states[0]?.name;
        if (type) write([...groups!, { type, records: [{ id: "TEST-1", ...(initial ? { state: initial } : {}) }] }]);
      }}>
        <option value="">{t("Select object type…")}</option>
        {members.filter(entity => !groups!.some(group => group.type === entity.type)).map(entity => <option key={entity.type} value={entity.type}>{entity.title} · {entity.type}</option>)}
      </Select></label>
      {!members.length && <p className="text-xs text-muted">{t("Add an object type to this application to prepare synthetic records.")}</p>}
      {(model.drafts.isError || capabilities.isError) && <p role="alert" className="text-xs text-danger">{t("Saved sample fields could not be loaded. Use advanced JSON or retry.")} <Button size="sm" onClick={() => { void model.drafts.refetch(); void capabilities.refetch(); }}>{t("Retry")}</Button></p>}
    </>}
  </div>;
}

export function ApplicationSampleAsset({ id = "new" }: { id?: string }) {
  const application = useApplicationScope(), { open } = useApplicationWorkspace();
  const projects = useRecordInventory<Project>("build.app"), inventory = useRecordInventory<SampleSetRecord>("build.sample-set");
  const project = projects.data?.records.find(record => record.id === application), record = inventory.data?.records.find(record => record.id === id);
  if (!application) return <div className="grid max-w-xl content-start gap-4 p-4">
    <PageHeader title={t("Sample sets")} help={t("Choose an application to prepare its sample records.")} />
    <SystemDefinitionReferences type="build.sample-set" />
    <label className="grid gap-1 text-sm">{t("Application")}<Select value="" disabled={projects.isPending} onChange={event => {
      if (event.target.value) open({ view: "sample-set", params: { id, application: event.target.value } });
    }}>
      <option value="">{t("Choose an application")}</option>
      {projects.data?.records.filter(project => !project.archived).map(project => <option key={project.id} value={project.id}>{project.title || project.name}</option>)}
    </Select></label>
    {projects.isError && <p role="alert" className="text-sm text-danger">{t("The applications could not be loaded.")} <Button size="sm" onClick={() => void projects.refetch()}>{t("Retry")}</Button></p>}
    {projects.isSuccess && !projects.data.records.some(project => !project.archived) && <p className="text-sm text-muted">{t("Create an application before preparing sample records.")}</p>}
    <Button className="w-fit" variant="ghost" onClick={() => open({ view: "projects" })}>{t("All applications")}</Button>
  </div>;
  const failed = projects.isError || inventory.isError || projects.isSuccess && !project || inventory.isSuccess && id !== "new" && !record;
  if (!project || id !== "new" && !record) return <p role={failed ? "alert" : "status"} className="p-3 text-sm">{failed ? t("The application sample could not be loaded.") : t("Loading…")}</p>;
  return <SampleDocument key={`${application}:${id}`} application={project} record={record} onSaved={saved => {
    if (id !== "new") return;
    open({ view: "sample-set", params: { id: saved.id, application } }, { ifCurrent: { view: "sample-set", params: { id, application } }, replace: true });
  }} />;
}

function SampleDocument({ application, record, onSaved }: { application: Project; record?: SampleSetRecord; onSaved: (record: SampleSetRecord) => void }) {
  const [selected, setSelected] = useState(record?.name ?? ""), [samples, setSamples] = useState(JSON.stringify(record?.samples ?? [], null, 2));
  return <ApplicationSamples document={!!record} application={application} selected={selected} samples={samples} onChange={(name, rows) => { setSelected(name); setSamples(rows); }} onSaved={onSaved} />;
}
