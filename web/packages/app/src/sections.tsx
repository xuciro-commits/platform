// A composed page (ADR-0035): sections laid out in order, each holding one
// widget bound to what this tenant has. A table says which record is selected;
// a detail and the actions read it. Every widget renders through the owner that
// already has it — the kit's record list and record page, the action catalog,
// the aggregate chart — so a code page and a composed page look and behave the
// same, and nothing here interprets data of its own.
import {
  Button, Card, Chart, ContentTabs, Markdown, Panel, PropertyList, RecordHistory, RecordList, RecordLookup, RecordPage, Select, Tasks, cn, t, type ChartSpec, type Encoding, type EntityRecord, type RecordView,
} from "@platform/ui";
import { Component, useEffect, useId, useMemo, useState, type CSSProperties, type ReactNode } from "react";
import { NewActions, RecordActions, prefixOf } from "./actions";
import { GeneratedForm, findDefinition, newId, useHost, useInvokeCapability, type Definition } from "./index";
import { ComputeCall } from "./capability";
import type { Api } from "@platform/kernel";
import { createWidgetRegistry, supportsPageUIProfile } from "./widgets/registry";
import type { PageSessionStore } from "./runtime/Session";
import { recordSlot, resourceVariables } from "./runtime/resources";
import type { VariableResult } from "./runtime/variables";
import { usePageVariables, usePageSession } from "./runtime/PageRuntime";

type Page = NonNullable<Definition["page"]>;
type Section = NonNullable<Page["sections"]>[number];

/** The page's second variable (16b): the conditions each filter set, by the
 *  object they narrow. A table, chart or metric over that object reads them. */
type Narrowed = Record<string, Record<string, unknown>>;

/** What a section is bound to, and what the page has selected and narrowed to. */
type Bound = {
  page: Page; section: Section; selected?: EntityRecord; onSelect: (record?: EntityRecord) => void; live: boolean;
  master?: EntityRecord;
  session?: PageSessionStore;
  narrowed: Narrowed; onNarrow: (object: string, field: string, value: unknown) => void;
};

/** Composing: the section in hand, and choosing another by clicking it. */
type Composing = { chosen?: number; onChoose?: (at: number) => void; at?: number; nested?: boolean;
  wrapLayout?: (id: string, node: Api.PageLayoutNode, body: ReactNode) => ReactNode };

const objectOf = (page: Page, section: Section) => section.object?.name || page.object.name;
const parentTypeOf = (page: Page, section: Section) => section.parentSelection
  ? page.selections?.find((selection) => selection.name === section.parentSelection)?.object.name ?? "" : page.object.name;
// Named and unnamed selections use one typed slot model.
const selectionKey = recordSlot;

/** The filters' conditions over an object, as the host's domain (ADR-0019). */
const domainOf = (narrowed: Narrowed, object: string): unknown[] =>
  Object.entries(narrowed[object] ?? {}).filter(([, v]) => v !== undefined && v !== "").map(([field, v]) => [field, "=", v]);

/** The reference that ties a section's object to the page's selected record:
 *  the declared relation when the section names one (ADR-0040 21b), else the
 *  first reference to the page's object. */
const relatedField = (fields: { name: string; title: string; type: string; ref?: string; inverse?: string; readOnly?: boolean }[] | undefined, page: Page, section: Section) =>
  fields?.find((f) => f.type === "reference" && f.ref === parentTypeOf(page, section) && (!section.relation || f.inverse === section.relation));

/** The records of an object, as a list; selecting one fills the rest of the page. */
function TableWidget({ page, section, onSelect, selected, master, narrowed, session }: Bound) {
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
      source={session?.querySource(section.id ?? `section:${page.sections?.indexOf(section)}`) ?? source} type={type} fields={section.fields} height={320} domain={domain}
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
const widgets = createWidgetRegistry<Bound>({
  table: TableWidget, detail: DetailWidget, actions: ActionsWidget,
  chart: (bound) => <ChartWidget {...bound} kpi={false} />,
  metric: (bound) => <ChartWidget {...bound} kpi />,
  text: ({ section }) => <Markdown content={section.text} className="text-sm" />,
  filter: FilterWidget,
  form: (bound) => <FormWidget key={`${objectOf(bound.page, bound.section)}/${bound.section.relation ?? ""}/${bound.section.parentSelection ?? ""}/${bound.section.relation ? bound.master?.id ?? "" : ""}`} {...bound} />,
  timeline: TimelineWidget, tasks: TasksWidget, function: FunctionWidget,
  compute: (bound) => <ComputeCall binding={bound.section.operation} bindings={bound.section.inputs} record={bound.selected} recordType={bound.page.object.name} live={bound.live} />,
});

class WidgetBoundary extends Component<{ children: ReactNode }, { failed: boolean }> {
  state = { failed: false };
  static getDerivedStateFromError() { return { failed: true }; }
  render() { return this.state.failed ? <Panel role="alert">{t("This widget could not be displayed.")}</Panel> : this.props.children; }
}

export function SectionView(bound: Bound & Composing) {
  const { section, chosen, onChoose, at, nested } = bound;
  const Renderer = widgets.resolve(section.widget, section.configVersion ?? (bound.page.document ? 0 : 1));
  const body = Renderer ? <Renderer {...bound} /> : <p role="alert" className="text-sm text-danger">{t("This widget is unavailable.")}</p>;
  const inHand = onChoose !== undefined && chosen === at;
  return (
    <Card onClick={onChoose && at !== undefined ? () => onChoose(at) : undefined}
      className={cn("grid min-w-0 content-start gap-2 p-3", nested ? "w-full" : section.width === "half" ? "md:col-span-1" : "md:col-span-2",
        onChoose && "cursor-pointer", inHand && "outline outline-2 outline-primary")}>
      {section.title && section.widget !== "metric" && <h3 className="text-sm font-semibold">{section.title}</h3>}
      <WidgetBoundary key={`${section.id ?? at}/${section.widget}/${section.configVersion}/${JSON.stringify(section)}`}>{body}</WidgetBoundary>
    </Card>
  );
}

/**
 * A composed page as people use it: the sections in order, sharing what is
 * selected for each typed slot. Related lists follow their declared parent;
 * detail/actions read their own object's selection. `live` false is the builder's canvas — the same widgets over the
 * same records, with nothing that writes.
 */
type ComposedPageProps = {
  page: Page; live?: boolean; notice?: ReactNode; definitionKey?: string;
  onVariableValues?: (values: Record<string, VariableResult>) => void;
} & Composing;
export function ComposedPage(props: ComposedPageProps) {
  const { me } = useHost();
  return <PageSession key={JSON.stringify([me, props.definitionKey, props.page])} {...props} />;
}

function PageSession({ page, live = true, notice, chosen, onChoose, wrapLayout, onVariableValues }: ComposedPageProps) {
  const { source } = useHost();
  const initialVariables = useMemo(() => {
    const values = { ...page.document?.variables };
    for (const node of Object.values(page.document?.nodes ?? {})) {
      const id = node.activeVariable, variable = id ? values[id] : undefined;
      if (id && node.kind === "tabs" && node.children?.length && variable?.mode === "state" && variable.type === "string" && !node.children.includes(String(variable.initial))) {
        values[id] = { ...variable, initial: node.children[0] };
      }
    }
    return values;
  }, [page.document]);
  const masterType = page.object.name;
  const slots = new Map([
    [selectionKey(masterType), masterType],
    ...(page.sections ?? []).map((section) => [selectionKey(objectOf(page, section)), objectOf(page, section)] as const),
    ...(page.selections ?? []).map((variable) => [selectionKey(variable.object.name, variable.name), variable.object.name] as const),
  ]);
  const children = new Map<string, Set<string>>();
  const queryParents = new Map<string, string>();
  for (const section of page.sections ?? []) {
    const type = objectOf(page, section);
    if (section.widget !== "table" || type === masterType && !section.parentSelection || !(section.relation || section.parentSelection || relatedField(source.entity(type)?.fields, page, section))) continue;
    const parent = selectionKey(parentTypeOf(page, section), section.parentSelection), child = selectionKey(type, section.selection);
    if (!children.has(parent)) children.set(parent, new Set());
    children.get(parent)!.add(child);
    queryParents.set(section.id ?? `section:${page.sections!.indexOf(section)}`, parent);
  }
  const { session, snapshot } = usePageSession(source, { objects: slots, children, queryParents });
  const resourceKey = JSON.stringify([page.object, page.document?.variables, page.sections]);
  const resources = useMemo(() => resourceVariables(page, snapshot), [resourceKey, snapshot]);
  const variables = usePageVariables(initialVariables, snapshot.scalars, session, resources);
  useEffect(() => { onVariableValues?.(variables.values); }, [onVariableValues, variables.values]);
  const narrowed = snapshot.filters;
  const onSelect = (key: string, record?: EntityRecord) => session.select(key, record);
  const onNarrow = (object: string, field: string, value: unknown) => session.filter(object, field, value);
  const indexed = new Map((page.sections ?? []).map((section, i) => [section.id, { section, i }]));
  const renderSection = (section: Section, i: number, nested: boolean) => (
    <SectionView key={section.id || i} page={page} section={section} session={session} selected={session.selected(selectionKey(objectOf(page, section), section.selection))}
      master={session.selected(selectionKey(parentTypeOf(page, section), section.parentSelection))} onSelect={(record) => onSelect(selectionKey(objectOf(page, section), section.selection), record)} live={live} narrowed={narrowed} onNarrow={onNarrow}
      chosen={chosen} onChoose={onChoose} at={i} nested={nested} />
  );
  const renderNode = (id: string, ancestors: Set<string>): ReactNode => {
    if (!page.document || ancestors.has(id)) return <Panel role="alert">{t("This page layout is unavailable.")}</Panel>;
    const node = page.document.nodes[id];
    if (!node) return <Panel role="alert">{t("This page layout is unavailable.")}</Panel>;
    if (node.visibleWhen) {
      const visible = variables.values[node.visibleWhen];
      if (!visible || visible.status === "error") return <Panel role="alert">{t("This page variable could not be evaluated.")} {node.visibleWhen}</Panel>;
      if (visible.status === "pending") return <Panel role="status">{t("Loading page variable…")}</Panel>;
      if (visible.value !== true) return wrapLayout ? wrapLayout(id, node, <Panel>{t("Hidden by page variable")}: {node.visibleWhen}</Panel>) : null;
    }
    if (node.kind === "widget") {
      const item = indexed.get(node.section);
      if (!item) return null; // server filtered this widget for the reader
      const body = renderSection(item.section, item.i, true);
      return wrapLayout ? wrapLayout(id, node, body) : body;
    }
    if (!["rows", "columns", "tabs"].includes(node.kind)) return <Panel role="alert">{t("This page layout is unavailable.")}</Panel>;
    const next = new Set(ancestors); next.add(id);
    if (node.kind === "tabs") {
      const active = variables.values[node.activeVariable ?? ""];
      if (!active || active.status !== "value" || typeof active.value !== "string") return <Panel role="alert">{t("This page variable could not be evaluated.")} {node.activeVariable}</Panel>;
      const body = <ContentTabs label={node.title || t("Page tabs")} value={active.value} onChange={(value) => variables.set(node.activeVariable!, value)}
        items={(node.children ?? []).map((child, index) => ({ id: child,
          title: page.document!.nodes[child]?.title || indexed.get(page.document!.nodes[child]?.section)?.section.title || t("Tab {n}", { n: index + 1 }), content: renderNode(child, next) }))} />;
      return wrapLayout ? wrapLayout(id, node, body) : body;
    }
    const body = <div key={id} className={node.kind === "columns" ? "grid min-w-0 grid-cols-1 gap-3 @md:grid-cols-[repeat(var(--page-columns),minmax(0,1fr))]" : "flex min-w-0 flex-col gap-3"}
      style={node.kind === "columns" ? { "--page-columns": Math.max(1, node.children?.length ?? 0) } as CSSProperties : undefined}>
      {node.children?.map((child) => <div key={child} className="@container min-w-0">{renderNode(child, next)}</div>)}
    </div>;
    return wrapLayout ? wrapLayout(id, node, body) : body;
  };
  return (
    <div className="@container/page grid gap-3">
      {notice}
      {page.document ? page.document.formatVersion !== 2 || !supportsPageUIProfile(page.document.uiProfile)
        ? <Panel role="alert">{t("This page needs a newer workspace version. Refresh after updating the workspace.")}</Panel>
        : renderNode(page.document.root, new Set()) : <div className="grid gap-3 md:grid-cols-2">
        {(page.sections ?? []).map((section, i) => (
          renderSection(section, i, false)
        ))}
      </div>}
      {(page.sections ?? []).length === 0 && <Panel role="status" className="text-sm text-muted">{t("Nothing is on this page yet.")}</Panel>}
    </div>
  );
}

/** Whether a page is composed of sections (ADR-0035) rather than the list-detail shorthand. */
export const isComposed = (page: Page) => page.layout === "composed";
