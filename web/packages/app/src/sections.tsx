// A composed page (ADR-0035): sections laid out in order, each holding one
// widget bound to what this tenant has. A table says which record is selected;
// a detail and the actions read it. Every widget renders through the owner that
// already has it — the kit's record list and record page, the action catalog,
// the aggregate chart — so a code page and a composed page look and behave the
// same, and nothing here interprets data of its own.
import {
  Button, Card, Chart, Markdown, Panel, PropertyList, RecordHistory, RecordList, RecordLookup, RecordPage, Select, Tasks, cn, t, type ChartSpec, type Encoding, type EntityRecord, type RecordView,
} from "@platform/ui";
import { useEffect, useId, useState, type ReactNode } from "react";
import { NewActions, RecordActions, prefixOf } from "./actions";
import { GeneratedForm, findDefinition, newId, useHost, useInvokeCapability, type Definition } from "./index";
import { ComputeCall } from "./capability";

type Page = NonNullable<Definition["page"]>;
type Section = NonNullable<Page["sections"]>[number];

/** The page's second variable (16b): the conditions each filter set, by the
 *  object they narrow. A table, chart or metric over that object reads them. */
type Narrowed = Record<string, Record<string, unknown>>;

/** What a section is bound to, and what the page has selected and narrowed to. */
type Bound = {
  page: Page; section: Section; selected?: EntityRecord; onSelect: (record?: EntityRecord) => void; live: boolean;
  master?: EntityRecord;
  narrowed: Narrowed; onNarrow: (object: string, field: string, value: unknown) => void;
};

/** Composing: the section in hand, and choosing another by clicking it. */
type Composing = { chosen?: number; onChoose?: (at: number) => void; at?: number };

const objectOf = (page: Page, section: Section) => section.object?.name || page.object.name;
const parentTypeOf = (page: Page, section: Section) => section.parentSelection
  ? page.selections?.find((selection) => selection.name === section.parentSelection)?.object.name ?? "" : page.object.name;
// Named and unnamed selections use one typed slot model.
const selectionKey = (type: string, name?: string) => `${name ? `selection:${name}` : "object"}/${type}`;

/** The filters' conditions over an object, as the host's domain (ADR-0019). */
const domainOf = (narrowed: Narrowed, object: string): unknown[] =>
  Object.entries(narrowed[object] ?? {}).filter(([, v]) => v !== undefined && v !== "").map(([field, v]) => [field, "=", v]);

/** The reference that ties a section's object to the page's selected record:
 *  the declared relation when the section names one (ADR-0040 21b), else the
 *  first reference to the page's object. */
const relatedField = (fields: { name: string; title: string; type: string; ref?: string; inverse?: string; readOnly?: boolean }[] | undefined, page: Page, section: Section) =>
  fields?.find((f) => f.type === "reference" && f.ref === parentTypeOf(page, section) && (!section.relation || f.inverse === section.relation));

/** The records of an object, as a list; selecting one fills the rest of the page. */
function TableWidget({ page, section, onSelect, selected, master, narrowed }: Bound) {
  const { source, definitions } = useHost();
  const type = objectOf(page, section);
  const isMaster = type === parentTypeOf(page, section) && !section.parentSelection && !section.relation;
  const info = source.entity(type);
  // A named query (ADR-0040 21c): its declared conditions, run for the selected
  // record through its reference; the list is still the member's own read.
  const query = section.query?.name ? findDefinition(definitions, section.query)?.query : undefined;
  const refField = !isMaster
    ? (query?.by ? info?.fields.find((f) => f.name === query.by) : relatedField(info?.fields, page, section))
    : undefined;

  if ((section.relation || section.parentSelection) && !refField) return <p role="alert" className="text-sm text-danger">
    {t("This section's parent reference is unavailable.")}</p>;

  if (refField && !master) {
    return (
      <div className="flex h-40 items-center justify-center rounded-md border border-dashed border-border p-4 text-center">
        <p className="text-sm text-muted">
          {t("Select a record to see related {records}.", { records: info?.plural?.toLowerCase() ?? type })}
        </p>
      </div>
    );
  }

  const relationDomain = refField && master ? [[refField.name, "=", master.id]] : [];
  const queryDomain = (query?.domain as unknown[] | undefined) ?? [];
  const domain = [...queryDomain, ...domainOf(narrowed, type), ...relationDomain];

  return (
    <RecordList key={refField ? `${type}/${refField.name}/${master?.id}` : type}
      source={source} type={type} fields={section.fields} height={320} domain={domain}
      onOpen={(record) => onSelect(record.id === selected?.id ? undefined : record)} />
  );
}

/** The record the page has selected, with the fields the builder chose. */
function DetailWidget({ page, section, selected }: Bound) {
  const { source } = useHost();
  const type = objectOf(page, section);
  if (!selected) return <p className="text-sm text-muted">{t("Select a record to see it here.")}</p>;
  // The fields alone: what people do with it is the actions widget's (ADR-0035 D2).
  return <RecordPage key={`${type}/${selected.id}`} source={source} type={type} id={selected.id} fields={section.fields} detailOnly />;
}

/** The actions the builder chose, on what is selected (Workshop's button group). */
function ActionsWidget({ page, section, selected, live }: Bound) {
  const type = objectOf(page, section);
  const allowed = (section.actions ?? []).map((ref) => ref.name);
  if (!live) return <p className="text-sm text-muted">{t("Actions do not run while you compose.")}</p>;
  return (
    <div className="flex flex-wrap gap-2">
      <NewActions type={type} allowed={allowed} />
      {selected
        ? <RecordActions type={type} record={selected} allowed={allowed} steps />
        : <span className="self-center text-sm text-muted">{t("Select a record to act on it.")}</span>}
    </div>
  );
}

/** An aggregate of the object: grouped and measured, drawn by the kit (ADR-0019). */
function chartSpec(page: Page, section: Section, kpi: boolean, domain: unknown[]): ChartSpec {
  const [aggregate, field] = (section.measure ?? "count").split(":");
  const value: Encoding = { field, type: "quantitative", aggregate: aggregate as Encoding["aggregate"] };
  const [group, timeUnit] = (section.group ?? "").split(":");
  const by: Encoding = { field: group, type: timeUnit ? "temporal" : "nominal", timeUnit: timeUnit as Encoding["timeUnit"] };
  return {
    title: kpi ? section.title : undefined,
    data: { entity: objectOf(page, section), domain },
    mark: kpi ? "kpi" : "bar",
    encoding: kpi ? { y: value } : { x: by, y: value },
  };
}

function ChartWidget({ page, section, kpi, narrowed, master }: Bound & { kpi: boolean }) {
  const { source } = useHost();
  const aggregate = source.aggregate;
  const type = objectOf(page, section);
  const isMaster = type === parentTypeOf(page, section) && !section.parentSelection && !section.relation;
  const info = source.entity(type);
  const refField = !isMaster ? relatedField(info?.fields, page, section) : undefined;
  if ((section.relation || section.parentSelection) && !refField) return <p role="alert" className="text-sm text-danger">
    {t("This section's parent reference is unavailable.")}</p>;
  if (refField && !master) return <p className="text-sm text-muted">
    {t("Select a record to see related {records}.", { records: info?.plural?.toLowerCase() ?? type })}
  </p>;
  const relationDomain = refField && master ? [[refField.name, "=", master.id]] : [];
  const domain = [...domainOf(narrowed, type), ...relationDomain];

  return <Chart spec={chartSpec(page, section, kpi, domain)} frame={false} height={kpi ? 120 : 240}
    source={aggregate ? { aggregate, revision: source.revision } : undefined} />;
}

/** The filter (16b): a value to narrow the object's records by, for each field
 *  the builder chose. What it sets is the page's second variable. */
function FilterWidget({ page, section, narrowed, onNarrow }: Bound) {
  const { source } = useHost();
  const prefix = useId();
  const type = objectOf(page, section);
  const info = source.entity(type);
  const set = narrowed[type] ?? {};
  return (
    <div role="search" aria-label={section.title || t("Filter")} className="flex flex-wrap items-end gap-3">
      {(section.fields ?? []).map((name) => {
        const f = info?.fields.find((x) => x.name === name);
        if (!f) return null; // not a field this member reads
        const id = `${prefix}-${type}-${name}`;
        const value = set[name];
        return (
          <label key={name} htmlFor={id} className="grid gap-1 text-xs text-muted">{f.title}
            {f.type === "reference" && f.ref
              ? <RecordLookup id={id} source={source} type={f.ref} value={value as string | undefined} onChange={(v) => onNarrow(type, name, v)} />
              : <Select id={id} aria-label={f.title} className="w-40" value={value === undefined ? "" : String(value)}
                  onChange={(e) => onNarrow(type, name, e.target.value === "" ? undefined : f.type === "boolean" ? e.target.value === "true" : e.target.value)}>
                  <option value="">{t("Any")}</option>
                  {f.type === "boolean"
                    ? <><option value="true">{t("Yes")}</option><option value="false">{t("No")}</option></>
                    : (f.choices ?? []).map((c, i) => <option key={c} value={c}>{f.choiceTitles?.[i] ?? c}</option>)}
                </Select>}
          </label>
        );
      })}
      {Object.values(set).some((v) => v !== undefined && v !== "") &&
        <Button size="sm" variant="ghost" onClick={() => Object.keys(set).forEach((name) => onNarrow(type, name, undefined))}>{t("Clear")}</Button>}
    </div>
  );
}

/** The form (16b): a new record of the object, made through its own create
 *  action with the fields the builder chose; the host checks it like any other. */
function FormWidget({ page, section, live, master }: Bound) {
  const { decide, source } = useHost();
  const invoke = useInvokeCapability();
  const type = objectOf(page, section), parentType = parentTypeOf(page, section);
  const refField = section.relation ? relatedField(source.entity(type)?.fields, page, section) : undefined;
  const [round, setRound] = useState(0), [error, setError] = useState("");
  const bindings = section.inputs ?? {};
  const bindingKey = JSON.stringify([parentType, master?.id, bindings]);
  const [bound, setBound] = useState<{ key: string; values?: Record<string, unknown>; error?: string }>({ key: "" });
  useEffect(() => {
    let current = true;
    setBound({ key: bindingKey });
    Promise.all(Object.entries(bindings).map(async ([name, binding]) => {
      if (binding.source === "literal") return [name, binding.value] as const;
      if (binding.source !== "subject" || !master || !binding.path?.length) throw new Error(t("The bound record input is unavailable."));
      let typ = parentType, record = (await source.get(typ, master.id)).record;
      for (const [index, part] of binding.path.entries()) {
        const field = source.entity(typ)?.fields.find((field) => field.name === part);
        const value = record[part];
        if (!field || value === undefined) throw new Error(t("The bound record input is unavailable."));
        if (index === binding.path.length - 1) return [name, value] as const;
        if (field.type !== "reference" || !field.ref || typeof value !== "string" || !value) throw new Error(t("The bound record input is unavailable."));
        typ = field.ref; record = (await source.get(typ, value)).record;
      }
      throw new Error(t("The bound record input is unavailable."));
    })).then((values) => { if (current) setBound({ key: bindingKey, values: Object.fromEntries(values) }); }, () => {
      if (current) setBound({ key: bindingKey, error: t("The bound record input is unavailable.") });
    });
    return () => { current = false; };
  }, [bindingKey, source, source.revision]);
  if (section.relation && (!refField || refField.readOnly)) return <p role="alert" className="text-sm text-danger">
    {t("This form's parent reference is unavailable.")}</p>;
  if (refField && !master) return <p className="text-sm text-muted">{t("Select a parent record before creating a related record.")}</p>;
  const fields = (section.fields ?? source.entity(type)?.fields.map((field) => field.name) ?? [])
    .filter((name) => name !== refField?.name && !bindings[name]);
  const parentInfo = source.entity(parentType);
  const ready = bound.key === bindingKey && bound.values !== undefined;
  const supplied = ready ? Object.entries(bound.values!).map(([name, value]) => [source.entity(type)?.fields.find((field) => field.name === name)?.title ?? name, String(value)] as [string, string]) : [];
  return <div className="grid gap-2">
    {!live && <p className="text-xs text-muted">{t("The form does not submit while you compose.")}</p>}
    {refField && master && <PropertyList items={[[refField.title, String(master[parentInfo?.display ?? "id"] ?? master.id)]]} />}
    {supplied.length > 0 && <PropertyList items={supplied} />}
    {!ready && Object.keys(bindings).length > 0 && <p role={bound.error ? "alert" : "status"} className="text-xs text-muted">{bound.error ?? t("Loading bound inputs…")}</p>}
    {error && <p role="alert" className="text-sm text-danger">{error}</p>}
    <fieldset disabled={!live || !ready}>
      <GeneratedForm key={round} type={type} fields={fields} submitLabel={t("Create")} onCancel={() => { setError(""); setRound((r) => r + 1); }}
        onSubmit={async (values) => {
          if (!live) return;
          setError("");
          const payload = refField && master ? { ...values, [refField.name]: master.id } : values;
          const id = newId(prefixOf(type));
          try {
            if (Object.keys(bindings).length > 0) {
              await invoke({ ref: { app: type.split(".")[0]!, kind: "action", name: `${type}.create` }, target: id, key: crypto.randomUUID(),
                inputs: payload, bindings, record: Object.values(bindings).some((binding) => binding.source === "subject") && master ? `${parentType}/${master.id}` : undefined, expectedRevision: 0 });
              setRound((r) => r + 1);
            } else if (await decide(`${type}.create`, { type, id }, payload, { expectedRevision: 0 })) setRound((r) => r + 1);
          } catch (failure) { setError(failure instanceof Error ? failure.message : t("The related record could not be created.")); }
        }} />
    </fieldset>
  </div>;
}

/** The selected record as its page reads it: history, tasks waiting on it. */
function useRecordView(type: string, id?: string) {
  const { source } = useHost();
  const [view, setView] = useState<RecordView>();
  useEffect(() => {
    let current = true;
    setView(undefined);
    if (id) source.get(type, id).then((value) => { if (current) setView(value); }, () => { if (current) setView(undefined); });
    return () => { current = false; };
  }, [source, type, id, source.revision]);
  return view;
}

/** The timeline (16b): the selected record's history from the journal. */
function TimelineWidget({ page, section, selected }: Bound) {
  const { source } = useHost();
  const type = objectOf(page, section);
  const info = source.entity(type);
  const view = useRecordView(type, selected?.id);
  if (!selected) return <p className="text-sm text-muted">{t("Select a record to see what happened to it.")}</p>;
  if (!view || !info) return <p className="text-sm text-muted">{t("Loading…")}</p>;
  return <RecordHistory info={info} history={view.history} heading={false} />;
}

/** The tasks (16b): what waits on the selected record for this member — approvals
 *  and flow steps from the work app — answered where they are. */
function TasksWidget({ page, section, selected, live }: Bound) {
  const { can, decide } = useHost();
  const type = objectOf(page, section);
  const view = useRecordView(type, selected?.id);
  if (!selected) return <p className="text-sm text-muted">{t("Select a record to see what waits on it.")}</p>;
  if (!view) return <p className="text-sm text-muted">{t("Loading…")}</p>;
  if (view.tasks.length === 0) return <p className="text-sm text-muted">{t("Nothing waits on it.")}</p>;
  const answer = live && can("work.task.complete")
    ? { answer: async (task: RecordView["tasks"][number], a?: string) => { await decide("work.task.complete", { type: "work.task", id: task.id }, a ? { answer: a } : {}); } }
    : undefined;
  return <Tasks list={view.tasks} tasks={answer} />;
}

/** The builder's published function is called by its ordinary action. The
 * saved call record remains the only answer and permission surface. */
function FunctionWidget({ page, section, selected, live }: Bound) {
  const { source, can, decide } = useHost();
  const [callID, setCallID] = useState("");
  const [reload, setReload] = useState(0);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [measured, setMeasured] = useState<EntityRecord>();
  const name = section.function?.ref.name ?? "";
  const version = Number(section.function?.sourceVersion.match(/\.function-(\d+)$/)?.[1] ?? 0);
  useEffect(() => { setCallID(""); setError(""); }, [selected?.id, name, version]);
  useEffect(() => {
    if (!live || !callID) { setMeasured(undefined); return; }
    let current = true;
    source.get("build.function-call", callID).then((view) => { if (current) setMeasured(view.record); }, () => { if (current) setMeasured(undefined); });
    return () => { current = false; };
  }, [source, live, callID, reload]);
  if (!name || !version) return <p role="alert" className="text-sm text-danger">{t("Choose a published function for this page.")}</p>;
  return <div className="grid gap-3">
    <p className="text-xs text-muted">{name} · {t("Version")} {version}</p>
    {!selected ? <p className="text-sm text-muted">{t("Select a record to request advice.")}</p> : <>
      <div className="flex flex-wrap gap-2">
        <Button disabled={!live || busy || !can("build.function-call.start")} onClick={async () => {
          setBusy(true); setError("");
          const id = newId("CALL");
          try {
            if (await decide("build.function-call.start", { type: "build.function-call", id },
              { name, version, source: selected.id }, { quiet: true, onRefused: setError })) { setCallID(id); setReload((n) => n + 1); }
          } finally { setBusy(false); }
        }}>{busy ? t("Requesting advice…") : t("Request advice")}</Button>
        <Button disabled={!live} onClick={() => setReload((n) => n + 1)}>{t("Refresh advice")}</Button>
      </div>
      {!live && <p className="text-xs text-muted">{t("Advice calls do not run while you compose.")}</p>}
      {error && <p role="alert" className="text-xs text-danger">{error}</p>}
      {live && <RecordList key={reload} source={source} type="build.function-call" fields={["function", "version", "state", "source"]}
        domain={[["source", "=", `${page.object.name}/${selected.id}`], ["function", "=", name], ["version", "=", version]]}
        onOpen={(record) => setCallID(record.id)} />}
      {live && callID && measured?.metered === true && <Card className="grid gap-2 p-3">
        <h3 className="text-sm font-semibold">{t("Measured model call")}</h3>
        <PropertyList items={[
          [t("Input tokens"), measured.tokensReported ? String(measured.inputTokens ?? 0) : t("Not reported")],
          [t("Output tokens"), measured.tokensReported ? String(measured.outputTokens ?? 0) : t("Not reported")],
          [t("Model latency"), `${measured.latencyMillis ?? 0} ms`],
          [t("Reported USD cost"), measured.costReported ? `$${Number(measured.costUsd ?? 0).toFixed(6)}` : t("Not reported")],
          [t("Requested model"), String(measured.model ?? t("Not reported"))],
          [t("Served model"), measured.servedModel ? String(measured.servedModel) : t("Not reported")],
        ]} />
      </Card>}
      {live && callID && measured?.state !== "pending" && measured?.metered === false &&
        <p className="text-xs text-muted">{t("No model call was measured for this result.")}</p>}
      {live && callID && <RecordPage source={source} type="build.function-call" id={callID} fields={["state", "output", "code", "reason"]} reload={reload} />}
    </>}
  </div>;
}

/** One section: its title, and the widget it holds. While a page is being
 *  composed, clicking it takes it in hand. */
export function SectionView(bound: Bound & Composing) {
  const { section, chosen, onChoose, at } = bound;
  const body: ReactNode = (() => {
    switch (section.widget) {
      case "table": return <TableWidget {...bound} />;
      case "detail": return <DetailWidget {...bound} />;
      case "actions": return <ActionsWidget {...bound} />;
      case "chart": return <ChartWidget {...bound} kpi={false} />;
      case "metric": return <ChartWidget {...bound} kpi />;
      case "text": return <Markdown content={section.text} className="text-sm" />;
      case "filter": return <FilterWidget {...bound} />;
      case "form": return <FormWidget key={`${objectOf(bound.page, section)}/${section.relation ?? ""}/${section.parentSelection ?? ""}/${section.relation ? bound.master?.id ?? "" : ""}`} {...bound} />;
      case "timeline": return <TimelineWidget {...bound} />;
      case "tasks": return <TasksWidget {...bound} />;
      case "function": return <FunctionWidget {...bound} />;
      case "compute": return <ComputeCall binding={section.operation} bindings={section.inputs} record={bound.selected} recordType={bound.page.object.name} live={bound.live} />;
      default: return <p role="alert" className="text-sm text-danger">{t("This widget is unavailable.")}</p>;
    }
  })();
  const inHand = onChoose !== undefined && chosen === at;
  return (
    <Card onClick={onChoose && at !== undefined ? () => onChoose(at) : undefined}
      className={cn("grid content-start gap-2 p-3", section.width === "half" ? "md:col-span-1" : "md:col-span-2",
        onChoose && "cursor-pointer", inHand && "outline outline-2 outline-primary")}>
      {section.title && section.widget !== "metric" && <h3 className="text-sm font-semibold">{section.title}</h3>}
      {body}
    </Card>
  );
}

/**
 * A composed page as people use it: the sections in order, sharing what is
 * selected for each typed slot. Related lists follow their declared parent;
 * detail/actions read their own object's selection. `live` false is the builder's canvas — the same widgets over the
 * same records, with nothing that writes.
 */
export function ComposedPage({ page, live = true, notice, chosen, onChoose }: {
  page: Page; live?: boolean; notice?: ReactNode;
} & Composing) {
  const { source } = useHost();
  const [selected, setSelected] = useState<Record<string, EntityRecord | undefined>>({});
  const [narrowed, setNarrowed] = useState<Narrowed>({});
  const masterType = page.object.name;
  const slots = new Map([
    [selectionKey(masterType), masterType],
    ...(page.sections ?? []).map((section) => [selectionKey(objectOf(page, section)), objectOf(page, section)] as const),
    ...(page.selections ?? []).map((variable) => [selectionKey(variable.object.name, variable.name), variable.object.name] as const),
  ]);
  const children = new Map<string, Set<string>>();
  for (const section of page.sections ?? []) {
    const type = objectOf(page, section);
    if (section.widget !== "table" || type === masterType && !section.parentSelection || !(section.relation || section.parentSelection || relatedField(source.entity(type)?.fields, page, section))) continue;
    const parent = selectionKey(parentTypeOf(page, section), section.parentSelection), child = selectionKey(type, section.selection);
    if (!children.has(parent)) children.set(parent, new Set());
    children.get(parent)!.add(child);
  }
  const clear = (records: typeof selected, keys: string[]) => {
    const next = { ...records }, seen = new Set<string>();
    const remove = (key: string) => {
      if (seen.has(key)) return;
      seen.add(key); delete next[key];
      for (const child of children.get(key) ?? []) remove(child);
    };
    keys.forEach(remove);
    return next;
  };
  const references = JSON.stringify(Object.entries(selected).flatMap(([key, record]) => record && slots.has(key) ? [[key, slots.get(key), record.id]] : []));
  // Decisions refresh the host source. Re-read selected records so lifecycle
  // actions use the current state/revision rather than the table's old snapshot.
  useEffect(() => {
    let current = true;
    for (const [key, type, id] of JSON.parse(references) as [string, string, string][]) {
      source.get(type, id).then((view) => {
        if (current) setSelected((records) => records[key]?.id === id ? { ...records, [key]: view.record } : records);
      }, () => {
        if (current) setSelected((records) => records[key]?.id !== id ? records : clear(records, [key]));
      });
    }
    return () => { current = false; };
  }, [source, source.revision, masterType, references]);
  const onSelect = (key: string, record?: EntityRecord) => {
    // Child selections belong to the current master. Changing the master must
    // never leave a detail or an action aimed at the previous master's child.
    setSelected((records) => ({ ...clear(records, [key]), [key]: record }));
  };
  const onNarrow = (object: string, field: string, value: unknown) => {
    setNarrowed((n) => ({ ...n, [object]: { ...n[object], [field]: value } }));
    setSelected((records) => clear(records, [...slots].filter(([, type]) => type === object).map(([key]) => key)));
  };
  return (
    <div className="grid gap-3">
      {notice}
      <div className="grid gap-3 md:grid-cols-2">
        {(page.sections ?? []).map((section, i) => (
          <SectionView key={i} page={page} section={section} selected={selected[selectionKey(objectOf(page, section), section.selection)]}
            master={selected[selectionKey(parentTypeOf(page, section), section.parentSelection)]} onSelect={(record) => onSelect(selectionKey(objectOf(page, section), section.selection), record)} live={live} narrowed={narrowed} onNarrow={onNarrow}
            chosen={chosen} onChoose={onChoose} at={i} />
        ))}
      </div>
      {(page.sections ?? []).length === 0 && <Panel role="status" className="text-sm text-muted">{t("Nothing is on this page yet.")}</Panel>}
    </div>
  );
}

/** Whether a page is composed of sections (ADR-0035) rather than the list-detail shorthand. */
export const isComposed = (page: Page) => page.layout === "composed";
