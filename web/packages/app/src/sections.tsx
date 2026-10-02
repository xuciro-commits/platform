import {isDecimal,scalarAssignable,type ScalarValue} from "./runtime/decimal";
// A composed page (ADR-0035): sections laid out in order, each holding one
// widget bound to what this tenant has. A table says which record is selected;
// a detail and the actions read it. Every widget renders through the owner that
// already has it — the kit's record list and record page, the action catalog,
// the aggregate chart — so a code page and a composed page look and behave the
// same, and nothing here interprets data of its own.
import {
  Button, Card, ContentTabs, Dialog, FlowLayout, Sheet, Input, Markdown, Panel, PropertyList, RecordHistory, RecordList, RecordLookup, RecordPage, Select, Tasks, cn, t, useViewVisible, type ChartSpec, type EntityRecord, type RecordSource, type RecordView,
} from "@platform/ui";
import { Component, lazy, Suspense, useEffect, useId, useMemo, useRef, useState, type CSSProperties, type ReactNode } from "react";
import { NewActions, RecordActions, prefixOf } from "./actions";
import { GeneratedForm, findDefinition, newId, useHost, useInvokeCapability, type Definition } from "./index";
import { ComputeCall } from "./capability";
import type { Api } from "@platform/kernel";
import { createWidgetRegistry, supportsPageUIProfile } from "./widgets/registry";
import { useApplicationContext, useApplicationVariables } from "./runtime/ApplicationRuntime";
import { usePageQueries } from "./runtime/PageQueries";
import { planKey, variablePlan } from "./runtime/query-plans";
import { compileChartSpec } from "./widgets/chart-spec";
import { inputSlot, usePageInputs, usePageNavigation } from "./runtime/PageNavigation";
import { NestedLoopRuntime } from "./runtime/NestedLoopRuntime";
import { LoopRuntime, type LoopContext } from "./runtime/LoopRuntime";
import type { PageSessionStore } from "./runtime/Session";
import { recordSlot, filterOwner, filtersForOwner, filterSessionBindings, selectionSlot, overlaySessionScopes, resourceVariables } from "./runtime/resources";
import { evaluateVariables, type VariableResult } from "./runtime/variables";
import { pageVariableContract, usePageVariables, usePageSession } from "./runtime/PageRuntime";

const ChartRenderer=lazy(()=>import("./widgets/Chart").then(module=>({default:module.ChartRenderer})));
const TableRenderer=lazy(()=>import("./widgets/Table").then(module=>({default:module.TableRenderer})));
const RecordTimelineRenderer=lazy(()=>import("./widgets/RecordTimeline").then(module=>({default:module.RecordTimelineRenderer})));
const KanbanRenderer=lazy(()=>import("./widgets/Kanban").then(module=>({default:module.KanbanRenderer})));
const PivotRenderer=lazy(()=>import("./widgets/Pivot").then(module=>({default:module.PivotRenderer})));
const ButtonRenderer=lazy(()=>import("./widgets/Button").then(module=>({default:module.ButtonRenderer})));

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
  window?: NonNullable<Parameters<typeof RecordList>[0]["window"]>;
 collection?:VariableResult; aggregateScope?:string;
  onClick?: () => void; numeric?:boolean; valueError?:string; value?: string; onValue?: (value: string) => void; enabled?: boolean; readSource?: RecordSource;
  sharedFilter?:Record<string,unknown>; narrowed: Narrowed; onNarrow: (object: string, field: string, value: unknown) => void;
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
function TableAdapter({ page, section, onSelect, selected, master, narrowed, sharedFilter, session, window }: Bound) {
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

  const status = section.collectionVariable ? !window ? "missing-window" : undefined
    : (section.relation || section.parentSelection) && !refField ? "invalid-reference" : refField && !master ? "missing-parent" : undefined;
  const relationDomain = refField && master ? [[refField.name, "=", master.id]] : [];
  const domain = [...((query?.domain as unknown[] | undefined) ?? []), ...domainOf(narrowed, type), ...domainOf({[type]:sharedFilter??{}},type), ...relationDomain];
  return <TableRenderer key={section.collectionVariable ? type : refField ? `${type}/${refField.name}/${master?.id}` : type}
    source={section.collectionVariable ? source : session?.querySource(section.id ?? `section:${page.sections?.indexOf(section)}`) ?? source}
    object={type} fields={section.fields} domain={section.collectionVariable ? undefined : domain} window={window}
    selected={selected} onSelect={onSelect} status={status} plural={info?.plural?.toLowerCase()}/>;
}

function RecordTimelineAdapter({page,section,onSelect,selected,window}:Bound) {
 const {source}=useHost(),info=source.entity(objectOf(page,section));
 const start=info?.fields.find(f=>f.name===section.timeStart),end=section.timeEnd?info?.fields.find(f=>f.name===section.timeEnd):undefined;
 const label=(name?:string)=>name==="id"||!!info?.fields.some(f=>f.name===name&&["text","longtext","choice","reference"].includes(f.type));
 const valid=start&&["date","datetime"].includes(start.type)&&(!section.timeEnd||end?.type===start.type)&&label(section.timeLabel)&&(!section.timeGroup||label(section.timeGroup));
 return <RecordTimelineRenderer window={window} fields={valid?{start:section.timeStart!,end:section.timeEnd,label:section.timeLabel!,group:section.timeGroup,kind:start.type as "date"|"datetime"}:undefined} selected={selected} onSelect={onSelect} title={section.title||t("Record timeline")}/>;
}
function KanbanAdapter({page,section,onSelect,selected,window,live}:Bound) {
 const {source,catalog}=useHost(),object=objectOf(page,section),info=source.entity(object);
 const allowed=new Set((section.actions??[]).map(a=>a.name));
 const moves=(info?.lifecycle?.transitions??[]).filter(m=>m.to.length===1&&allowed.has(m.schema)&&catalog.some(a=>a.schema===m.schema&&a.target===object)).map(m=>({schema:m.schema,title:m.title,from:m.from,to:m.to[0]!}));
 return <KanbanRenderer key={JSON.stringify([object,window?.query,source.scope])} object={object} info={info} window={window} cardLabel={section.cardLabel??""} fields={section.fields} selected={selected} onSelect={onSelect} moves={moves} live={live} title={section.title||t("Kanban board")}/>;
}

/** The record the page has selected, with the fields the builder chose. */
function DetailWidget({ page, section, selected, readSource }: Bound) {
  const host = useHost(), source = readSource ?? host.source;
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
  return compileChartSpec({object:objectOf(page,section),title:section.title,group:section.group,measure:section.measure,mark:section.mark,kpi,domain});
}

function ChartWidget({ page, section, kpi, pivot, narrowed, sharedFilter, master, window, collection, aggregateScope }: Bound & { kpi: boolean; pivot?:boolean }) {
  const { source } = useHost();
  const aggregate = source.aggregate;
  const render=(spec:ChartSpec,readScope?:string)=>pivot?<PivotRenderer object={objectOf(page,section)} query={"entity" in spec.data?{domain:spec.data.domain,search:spec.data.search,set:spec.data.set,traversal:spec.data.traversal,archived:spec.data.archived}:{}} rows={section.group??""} columns={section.columnGroup} measure={section.measure??"count"} source={aggregate?{aggregate,scope:readScope??source.scope,revision:source.revision}:undefined}/>:<ChartRenderer spec={spec} height={kpi?120:240} source={aggregate?{aggregate,scope:readScope??source.scope,revision:source.revision}:undefined}/>;
  if(section.collectionVariable){
    if(!window)return <Panel role={collection?.status==="error"?"alert":"status"}>{t(collection?.status==="error"?collection.code:"Query window is unavailable.")}</Panel>;
    if(window.error)return <Panel role="alert">{t(window.error)}</Panel>;
    const {domain,search,set,archived,traversal}=window.query;
    const spec=chartSpec(page,section,kpi,domain??[]);spec.data={entity:objectOf(page,section),domain,search,set,archived,traversal};
    return render(spec,aggregateScope);
  }
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
  const domain = [...domainOf(narrowed, type), ...domainOf({[type]:sharedFilter??{}},type), ...relationDomain];

  return render(chartSpec(page, section, kpi, domain),aggregateScope);
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
function useRecordView(type: string, id?: string, readSource?: RecordSource) {
  const host = useHost(), source = readSource ?? host.source;
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
function TimelineWidget({ page, section, selected, readSource }: Bound) {
  const { source } = useHost();
  const type = objectOf(page, section);
  const info = source.entity(type);
  const view = useRecordView(type, selected?.id, readSource);
  if (!selected) return <p className="text-sm text-muted">{t("Select a record to see what happened to it.")}</p>;
  if (!view || !info) return <p className="text-sm text-muted">{t("Loading…")}</p>;
  return <RecordHistory info={info} history={view.history} heading={false} />;
}

/** The tasks (16b): what waits on the selected record for this member — approvals
 *  and flow steps from the work app — answered where they are. */
function TasksWidget({ page, section, selected, live, readSource }: Bound) {
  const { can, decide } = useHost();
  const type = objectOf(page, section);
  const view = useRecordView(type, selected?.id, readSource);
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
  kanban:KanbanAdapter,
  "record-timeline":RecordTimelineAdapter,
  input: ({ section, value, onValue, enabled,numeric,valueError }) => <div className="grid gap-1"><Input inputMode={numeric?"decimal":undefined} maxLength={numeric?pageVariableContract.decimal.maxBytes:undefined} aria-invalid={!!valueError} aria-label={section.title || t("Text input")} value={value ?? ""} disabled={!onValue || enabled === false} onChange={(event) => onValue?.(event.target.value)} />{valueError&&<p role="alert" className="text-xs text-danger">{t(valueError)}</p>}</div>,
  button: ({ section, onClick, enabled }) => <ButtonRenderer title={section.title} onClick={onClick} enabled={enabled}/>,
  table: TableAdapter, detail: DetailWidget, actions: ActionsWidget,
  pivot: (bound) => <ChartWidget {...bound} kpi={false} pivot/>,
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
  const implementation = widgets.resolveDefinition(section.widget, section.configVersion ?? (bound.page.document ? 0 : 1));
  const Renderer=implementation?.Renderer;
  const body = Renderer ? <Suspense fallback={<p role="status">{t("Loading…")}</p>}><Renderer {...bound}/></Suspense> : <p role="alert" className="text-sm text-danger">{t("This widget is unavailable.")}</p>;
  const inHand = onChoose !== undefined && chosen === at;
  if (implementation?.contract.layoutPreferences.frame === "inline") return <div onClick={onChoose && at !== undefined ? () => onChoose(at) : undefined} className={cn("min-w-0", inHand && "outline outline-2 outline-primary rounded")}><WidgetBoundary key={`${section.id ?? at}/${section.widget}/${section.configVersion}/${JSON.stringify(section)}`}>{body}</WidgetBoundary></div>;
  return (
    <Card onClick={onChoose && at !== undefined ? () => onChoose(at) : undefined}
      className={cn("grid min-w-0 grid-cols-1 content-start gap-2 p-3", nested ? "w-full" : section.width === "half" ? "md:col-span-1" : "md:col-span-2",
        onChoose && "cursor-pointer", inHand && "outline outline-2 outline-primary")}>
      {section.title && <h3 className="text-sm font-semibold">{section.title}</h3>}
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
  editingRoot?: string;
  onVariableValues?: (values: Record<string, VariableResult>) => void;
} & Composing;
export function ComposedPage(props: ComposedPageProps) {
  const { me } = useHost();const application=useApplicationContext();
  return <PageSession key={JSON.stringify([me, props.definitionKey, props.page, application?.identity, application?.session.readScope])} {...props} />;
}

function PageSession({ page, live = true, notice, chosen, onChoose, wrapLayout, onVariableValues, editingRoot }: ComposedPageProps) {
  const { source } = useHost();
  const viewVisible = useViewVisible();
  const pageFocus = useRef<HTMLDivElement>(null), callers = useRef<Record<string, HTMLElement | null>>({});
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
    ...(page.sections ?? []).map((section) => [selectionSlot(page,section), objectOf(page, section)] as const),
    ...Object.values(page.document?.interface?.inputs ?? {}).filter((port) => port.type === "record" && port.object).map((port) => [inputSlot(port.variable), port.object!.name] as const),
    ...Object.entries(initialVariables).filter(([,v])=>v.mode==="shared"&&v.type==="record"&&v.source?.object).map(([id,v])=>[inputSlot(id),v.source!.object!.name] as const),
    ...(page.selections ?? []).map((variable) => [selectionKey(variable.object.name, variable.name), variable.object.name] as const),
  ]);
  const children = new Map<string, Set<string>>();
  const queryParents = new Map<string, string>();
  for (const section of page.sections ?? []) {
    const type = objectOf(page, section);
    if (section.collectionVariable || section.widget !== "table" || type === masterType && !section.parentSelection || !(section.relation || section.parentSelection || relatedField(source.entity(type)?.fields, page, section))) continue;
    const parent = selectionSlot(page,section,true), child = selectionSlot(page,section);
    if (!children.has(parent)) children.set(parent, new Set());
    children.get(parent)!.add(child);
    queryParents.set(section.id ?? `section:${page.sections!.indexOf(section)}`, parent);
  }
  const applicationVariable=(id:string)=>{const v=initialVariables[id];if(v?.mode==="shared"&&v.type==="object-set")return id;if(v?.source?.kind==="query"){const section=page.sections?.find((s)=>s.id===v.source?.section);const bound=initialVariables[section?.collectionVariable??""];if(bound?.mode==="shared"&&bound.type==="object-set")return section?.collectionVariable;}return undefined;};
  const querySelections = new Map<string,Set<string>>();
  for(const section of page.sections??[]){if(!["table","record-timeline","kanban"].includes(section.widget))continue;const variable=page.document?.variables?.[section.collectionVariable??""];if(variable?.source?.kind==="plan"){const key=planKey(variable.source.query??"");if(!querySelections.has(key))querySelections.set(key,new Set());querySelections.get(key)!.add(selectionSlot(page,section));}}
  for(const section of page.sections??[]){if(!["table","record-timeline","kanban"].includes(section.widget))continue;const id=section.collectionVariable;if(id&&applicationVariable(id)){const key=`application/${id}`;if(!querySelections.has(key))querySelections.set(key,new Set());querySelections.get(key)!.add(selectionSlot(page,section));}}
  for(const section of page.sections??[]){if(section.filterVariable&&section.widget==="table"){const key=section.id??`section:${page.sections!.indexOf(section)}`;if(!querySelections.has(key))querySelections.set(key,new Set());querySelections.get(key)!.add(selectionSlot(page,section));}}
  for(const [id,q] of Object.entries(page.document?.queries??{})){const v=initialVariables[q.for?.variable??""],producer=page.sections?.find(s=>s.id===v?.source?.section);if(q.query?.ref.kind!=="link-type"||v?.source?.kind!=="record"||!producer)continue;const parent=selectionSlot(page,producer);queryParents.set(planKey(id),parent);for(const child of querySelections.get(planKey(id))??[]){if(!children.has(parent))children.set(parent,new Set());(children.get(parent) as Set<string>).add(child);}}
  const { session, snapshot } = usePageSession(source, { objects: slots, children, queryParents, querySelections, ...(Number(/^platform\.page\.v2\.(\d+)$/.exec(page.document?.uiProfile??"")?.[1])>=13?filterSessionBindings(page):{}), overlayScopes:overlaySessionScopes(page) });
  const resourceKey = JSON.stringify([page.object, page.document?.variables, page.sections]);
  const resources = useMemo(() => resourceVariables(page, snapshot), [resourceKey, snapshot]);
  const incoming = usePageInputs(page, session, snapshot);
  const application = useApplicationVariables(initialVariables);
  const [recordProducers]=useState(()=>new Map<string,symbol>());
  const recordProducer=(section:string)=>{if(!recordProducers.has(section))recordProducers.set(section,Symbol(section));return recordProducers.get(section)!;};
  const applicationRecords=JSON.stringify(Object.entries(initialVariables).filter(([,v])=>v.mode==="shared"&&v.type==="record").map(([id])=>[id,application.resources[id]]));
  useEffect(()=>{for(const [id,v] of Object.entries(initialVariables)){if(v.mode!=="shared"||v.type!=="record")continue;const value=application.resources[id],ref=value?.status==="value"&&typeof value.value==="object"&&value.value.kind==="record"?value.value.reference:undefined,current=session.selected(inputSlot(id));if(current?.id!==ref?.id)session.selectReference(inputSlot(id),ref);}},[session,applicationRecords]);
  const previousSelections=useRef<Record<string,string>>({});
  useEffect(()=>{for(const section of page.sections??[]){if(!section.selectionVariable)continue;const slot=selectionSlot(page,section),state=snapshot.records[slot],id=state?.status==="value"?state.value.id:undefined;if(previousSelections.current[slot]&&state?.status==="empty"&&!id)application.select(section.selectionVariable,undefined,recordProducer(section.id??""),true);if(id)previousSelections.current[slot]=id;else if(state?.status!=="pending")delete previousSelections.current[slot];}},[snapshot.records]);

  useEffect(()=>{for(const [id,signature] of Object.entries(application.signatures)){const result=application.resources[id],window=result&&(result.status==="value"||result.status==="empty")&&typeof result.value==="object"&&result.value?.kind==="object-set"?result.value.window:undefined;session.reconcileExternalWindow(`application/${id}`,result?.status==="error"?"":signature,window?.records.map((r)=>r.id));}},[session,JSON.stringify(application.signatures),JSON.stringify(application.resources)]);
  const aliases=Object.fromEntries(Object.entries(initialVariables).flatMap(([id,v])=>{const sharedFilter=v.source?.kind==="filter"?page.sections?.find((s)=>s.id===v.source?.section)?.filterVariable:undefined;const source=sharedFilter??applicationVariable(id);return source&&source!==id?[[id,application.resources[source]??{status:"empty" as const}]]:[]}));
  const inputResources = useMemo(() => ({ ...resources, ...incoming.inputs, ...application.resources,...aliases }), [resources, JSON.stringify(incoming.inputs), JSON.stringify(application.resources),JSON.stringify(aliases)]);
  const inputValues = useMemo(() => evaluateVariables(initialVariables, snapshot.scalars, pageVariableContract, inputResources,undefined,undefined,session.property), [initialVariables,snapshot.scalars,inputResources]);
  const overlayForRoot = (root: string) => Object.entries(page.document?.overlays ?? {}).find(([, overlay]) => overlay.root === root)?.[0];
  const overlayInputs=Object.fromEntries(Object.keys(page.document?.overlays??{}).map((id)=>[id,evaluateVariables(initialVariables,snapshot.scalars,pageVariableContract,inputResources,undefined,id,session.property)]));
  const queries = usePageQueries(page,inputValues,session,snapshot,overlayInputs,editingRoot?overlayForRoot(editingRoot):undefined);
  const allResources = useMemo(() => ({ ...inputResources,...queries.resources }), [inputResources,JSON.stringify(queries.resources)]);
  const variables = usePageVariables(initialVariables, snapshot.scalars, session, allResources);
  const overlayValues = useMemo(() => Object.fromEntries(Object.keys(page.document?.overlays ?? {}).map((id) => [id, evaluateVariables(initialVariables, snapshot.scalars, pageVariableContract, allResources, undefined, id,session.property)])), [initialVariables, snapshot.scalars, allResources]);

  const navigation = usePageNavigation(page, live, variables.values, variables.set);
  useEffect(() => { onVariableValues?.({...variables.values,...Object.fromEntries(Object.entries(initialVariables).filter(([,v])=>v.scope==="overlay").map(([id,v])=>[id,overlayValues[v.owner??""]?.[id]??{status:"empty" as const}]))}); }, [onVariableValues, variables.values, overlayValues]);
  const selectionQuery=(section:Section)=>{if(!(Number(/^platform\.page\.v2\.(\d+)$/.exec(page.document?.uiProfile??"")?.[1])>=15))return undefined;const v=initialVariables[section.collectionVariable??""];return v?.source?.kind==="plan"?planKey(v.source.query??""):v?.mode==="shared"?`application/${section.collectionVariable}`:section.id;};
  const onSelect = (key: string, record?: EntityRecord, query?:string) => session.select(key, record,query);
  const indexed = new Map((page.sections ?? []).map((section, i) => [section.id, { section, i }]));
  const writeState = (id: string, value: ScalarValue, owner?: string) => {
    const variable = initialVariables[id];
    if (variable?.mode === "shared") { application.set(id,value); return; }
    if (variable?.mode !== "state" || !(variable.scope === "page" || variable.scope === "overlay" && variable.owner === owner) || !scalarAssignable(variable.type,value,pageVariableContract.maxStringBytes,pageVariableContract.decimal.maxBytes)) return;
    const entries = Object.entries(page.document?.overlays ?? {}), target = entries.find(([, overlay]) => overlay.openVariable === id);
    const changes: Record<string, ScalarValue> = { [id]: value }, reset: string[] = [];
    if (target) {
      if (value === true) callers.current[id] = document.activeElement instanceof HTMLElement ? document.activeElement : null;
      for (const [owner, overlay] of entries) {
        const closing = owner === target[0] ? value === false : value === true;
        if (value === true) changes[overlay.openVariable] = owner === target[0];
        if (closing) {
          session.endOverlay(owner);
          reset.push(...Object.entries(initialVariables).filter(([, v]) => v.scope === "overlay" && v.owner === owner && v.mode === "state").map(([key]) => key));
        }
      }
    }
    session.setScalars(changes, reset);
  };
  const setContextState = (id: string, value: ScalarValue, context?: LoopContext, overlay?: string) => initialVariables[id]?.scope === "loop-item" && context ? context.set(id, value) : writeState(id, value, overlay);
  const booleanValue = (id: string) => { const result = variables.values[id]; return result?.status === "value" && result.value === true; };
  const renderSection = (section: Section, i: number, nested: boolean, enabled = true, context?: LoopContext, overlay?: string, valueVariable?: string) => {
    const values = context?.values ?? (overlay ? overlayValues[overlay] : variables.values) ?? {}, value = values[valueVariable ?? ""];
    const shared=section.filterVariable?application.resources[section.filterVariable]:undefined;
    if(section.filterVariable&&(!shared||shared.status==="error"||shared.status==="pending"||!("value" in shared)||typeof shared.value!=="object"||shared.value.kind!=="filter"))return <Panel role="alert">{t("Shared filter is unavailable.")}</Panel>;
    const sharedFilter=shared&&"value" in shared&&typeof shared.value==="object"&&shared.value.kind==="filter"?shared.value.fields:undefined;
    const epoch = overlay ? session.overlayEpoch(overlay) : undefined;
    return (
      <SectionView key={section.id || i} page={page} section={section} session={session} readSource={context?.source} selected={section.selectionVariable?session.selected(inputSlot(section.selectionVariable)):section.recordVariable ? context ? context.record : initialVariables[section.recordVariable]?.mode==="resource"&&initialVariables[section.recordVariable]?.source?.kind==="record" ? (()=>{const producer=page.sections?.find((s)=>s.id===initialVariables[section.recordVariable!]?.source?.section);return producer?session.selected(selectionSlot(page,producer)):undefined})() : snapshot.records[inputSlot(section.recordVariable)]?.status === "value" ? session.selected(inputSlot(section.recordVariable)) : undefined : session.selected(selectionSlot(page,section))}
        master={session.selected(selectionSlot(page,section,true))} onSelect={(record) => {onSelect(selectionSlot(page,section),record,selectionQuery(section));if(section.selectionVariable)application.select(section.selectionVariable,record?{object:section.object?.name||page.object.name,id:record.id}:undefined,recordProducer(section.id??""));}} live={live} narrowed={section.widget==="filter"&&section.filterVariable?{[objectOf(page,section)]:sharedFilter??{}}:filtersForOwner(snapshot.filters,filterOwner(page,section))} sharedFilter={sharedFilter} onNarrow={(object,field,value)=>section.filterVariable&&section.widget==="filter"?application.filter(section.filterVariable,field,value):session.filter(object,field,value,filterOwner(page,section))}
        chosen={chosen} onChoose={onChoose} at={i} nested={nested} enabled={enabled}
        window={section.collectionVariable?applicationVariable(section.collectionVariable)?application.windows[applicationVariable(section.collectionVariable)!]:queries.windows[initialVariables[section.collectionVariable]?.source?.query??""]:undefined}
        collection={values[section.collectionVariable??""]} aggregateScope={JSON.stringify([source.scope,applicationVariable(section.collectionVariable??"")?[application.identity,application.readScope]:undefined,overlay,epoch])}
        numeric={initialVariables[valueVariable??""]?.type==="decimal"} valueError={value?.status==="error"?value.code:undefined} value={value?.status==="error"?value.draft:value?.status==="value"?value.draft??(isDecimal(value.value)?value.value.value:typeof value.value==="string"?value.value:undefined):undefined} onValue={valueVariable ? (value) => setContextState(valueVariable,initialVariables[valueVariable]?.type==="decimal"?{kind:"decimal",value}:value, context, overlay) : undefined}
        onClick={page.document?.events?.find((event) => event.source === section.id && event.event === "click") ? () => {
          const event = page.document!.events!.find((event) => event.source === section.id && event.event === "click")!;
          if (event.navigate || event.return) { navigation.emit(event, { values, set: (id, value) => setContextState(id, value, context, overlay), isActive: () => (!overlay || session.overlayEpoch(overlay) === epoch) && (!context || context.session.hasLoopItem(context.owner, context.key) && context.session.querySignature(context.queryKey) === context.signature) }); return; }
          if (typeof event.value === "string" || typeof event.value === "boolean" || isDecimal(event.value)) setContextState(event.target, event.value, context, overlay);
        } : undefined} />
    );
  };
  const renderNode = (id: string, ancestors: Set<string>, context?: LoopContext, overlay?: string): ReactNode => {
    if (!page.document || ancestors.has(id)) return <Panel role="alert">{t("This page layout is unavailable.")}</Panel>;
    const node = page.document.nodes[id];
    if (!node) return <Panel role="alert">{t("This page layout is unavailable.")}</Panel>;
    const values = context?.values ?? (overlay ? overlayValues[overlay] : variables.values) ?? {};
    if (node.visibleWhen) {
      const visible = values[node.visibleWhen];
      if (!visible || visible.status === "error") return <Panel role="alert">{t("This page variable could not be evaluated.")} {node.visibleWhen}</Panel>;
      if (visible.status === "pending") return <Panel role="status">{t("Loading page variable…")}</Panel>;
      if (visible.value !== true) return wrapLayout ? wrapLayout(id, node, <Panel>{t("Hidden by page variable")}: {node.visibleWhen}</Panel>) : null;
    }
    if (node.kind === "widget") {
      const item = indexed.get(node.section);
      if (!item) return null; // server filtered this widget for the reader
      if (context && !([...pageVariableContract.loop.recordWidgets, ...pageVariableContract.loop.presentationWidgets] as readonly string[]).includes(item.section.widget)) return <Panel role="alert">{t("This widget is not supported in a loop.")}</Panel>;
      const body = renderSection(item.section, item.i, true, !node.enabledWhen || (() => { const result = values[node.enabledWhen]; return result?.status === "value" && result.value === true; })(), context, overlay, node.valueVariable);
      return wrapLayout ? wrapLayout(id, node, body) : body;
    }
    if (!["rows", "columns", "tabs", "flow", "toolbar", "loop"].includes(node.kind)) return <Panel role="alert">{t("This page layout is unavailable.")}</Panel>;
    const next = new Set(ancestors); next.add(id);
    if (node.kind === "loop") {
      if(context)return <NestedLoopRuntime page={page} node={node} owner={id} parent={context} overlay={overlay}>{item=><>{node.children?.map(child=><div key={child} className="min-w-0">{renderNode(child,next,item,overlay)}</div>)}</>}</NestedLoopRuntime>;
      if (!node.loop) return <Panel role="alert">{t("Choose a loop query window.")}</Panel>;
      const collection = initialVariables[node.loop.collection]?.source, queryID=variablePlan(page,node.loop.collection);
      const shared=applicationVariable(node.loop.collection);const queryKey = shared?`application/${shared}`:queryID!==undefined?planKey(queryID):collection?.section??"";
      const body = <LoopRuntime page={page} queryKey={queryKey} expectedSignature={shared?application.signatures[shared]??"":queryID!==undefined ? queries.signatures[queryID]??"" : undefined} owner={id} loop={node.loop} label={node.title || t("Repeated records")} result={values[node.loop.collection]} session={session} snapshot={snapshot} variables={initialVariables} resources={allResources} overlay={overlay}>
        {(item) => <>{node.children?.map((child) => <div key={child} className="min-w-0">{renderNode(child, next, item, overlay)}</div>)}</>}
      </LoopRuntime>;
      return wrapLayout ? wrapLayout(id, node, body) : body;
    }
    if (node.kind === "tabs") {
      const active = values[node.activeVariable ?? ""];
      if (!active || active.status !== "value" || typeof active.value !== "string") return <Panel role="alert">{t("This page variable could not be evaluated.")} {node.activeVariable}</Panel>;
      const body = <ContentTabs label={node.title || t("Page tabs")} value={active.value} onChange={(value) => setContextState(node.activeVariable!, value, context, overlay)}
        items={(node.children ?? []).map((child, index) => ({ id: child,
          title: page.document!.nodes[child]?.title || indexed.get(page.document!.nodes[child]?.section)?.section.title || t("Tab {n}", { n: index + 1 }), content: renderNode(child, next, context, overlay) }))} />;
      return wrapLayout ? wrapLayout(id, node, body) : body;
    }
    if (node.kind === "flow" || node.kind === "toolbar") {
      const body = <FlowLayout toolbar={node.kind === "toolbar"} label={node.title || t("Toolbar")} align={node.align}>{node.children?.map((child) => <div key={child} className="min-w-0 max-w-full">{renderNode(child, next, context, overlay)}</div>)}</FlowLayout>;
      return wrapLayout ? wrapLayout(id, node, body) : body;
    }
    const body = <div key={id} className={node.kind === "columns" ? "grid min-w-0 grid-cols-1 gap-3 @md:grid-cols-[repeat(var(--page-columns),minmax(0,1fr))]" : "flex min-w-0 flex-col gap-3"}
      style={node.kind === "columns" ? { "--page-columns": Math.max(1, node.children?.length ?? 0) } as CSSProperties : undefined}>
      {node.children?.map((child) => <div key={child} className="@container min-w-0">{renderNode(child, next, context, overlay)}</div>)}
    </div>;
    return wrapLayout ? wrapLayout(id, node, body) : body;
  };
  const queryErrors=(owner?:string)=>Object.entries(page.document?.queries??{}).filter(([,plan])=>(plan.owner??"")===(owner??"")).map(([id,plan])=>{
    const result=Object.entries(initialVariables).find(([,v])=>(v.source?.kind==="plan"||v.mode==="aggregate")&&v.source?.query===id),status=result&&queries.resources[result[0]];
    return status?.status==="error"?<Panel key={id} role="alert" className="flex flex-wrap items-center gap-2">{plan.title||id}: {t(status.code)}<Button onClick={()=>queries.retry(id)}>{t("Retry query")}</Button></Panel>:null;
  });
  return (
    <div ref={pageFocus} tabIndex={-1} className="@container/page grid min-w-0 grid-cols-1 gap-3 outline-none">
      {notice}
      {queryErrors(editingRoot ? overlayForRoot(editingRoot) : undefined)}
      {Object.entries(application.resources).filter(([id,result])=>result.status==="error"&&["object-set","decimal"].includes(initialVariables[id]?.type??"")&&!application.error).map(([id,result])=><Panel key={id} role="alert" className="flex gap-2">{result.status==="error"?t(result.code):""}<Button onClick={()=>application.retry(id)}>{t("Retry query")}</Button></Panel>)}
      {Object.entries(application.resources).filter(([id,result])=>result.status==="error"&&initialVariables[id]?.type==="record"&&!application.error).map(([id,result])=><Panel key={id} role="alert">{result.status==="error"?t(result.code):""}</Panel>)}
      {application.error && <Panel role="alert">{t(application.error)}</Panel>}
      {(incoming.error || navigation.error) && <Panel role="alert">{t(incoming.error ?? navigation.error!)}</Panel>}
      {incoming.error && !onChoose ? null : page.document ? page.document.formatVersion !== 2 || !supportsPageUIProfile(page.document.uiProfile)
        ? <Panel role="alert">{t("This page needs a newer workspace version. Refresh after updating the workspace.")}</Panel>
        : renderNode(editingRoot ?? page.document.root, new Set(), undefined, editingRoot ? overlayForRoot(editingRoot) : undefined) : <div className="grid gap-3 md:grid-cols-2">
        {(page.sections ?? []).map((section, i) => (
          renderSection(section, i, false)
        ))}
      </div>}
      {!editingRoot && page.document && supportsPageUIProfile(page.document.uiProfile) && Object.entries(page.document.overlays ?? {}).map(([id, overlay]) => {
        const open = booleanValue(overlay.openVariable);
        const Frame = overlay.kind === "drawer" ? Sheet : Dialog;
        const sections: string[] = [];
        const collect = (id: string, seen = new Set<string>()) => {
          if (seen.has(id)) return; seen.add(id);
          const node = page.document!.nodes[id]; if (!node) return;
          if (node.section) sections.push(node.section);
          node.children?.forEach((child) => collect(child, seen));
        };
        collect(overlay.root);
        return <Frame key={id} open={open} suspended={!viewVisible} title={overlay.title} onOpenChange={(open) => writeState(overlay.openVariable, open)} returnFocus={callers.current[overlay.openVariable]} fallbackFocus={pageFocus.current}>
          {open && <OverlayBody session={session} sectionIDs={sections} owner={id}>{queryErrors(id)}{renderNode(overlay.root, new Set(), undefined, id)}</OverlayBody>}
        </Frame>;
      })}
      {(page.sections ?? []).length === 0 && <Panel role="status" className="text-sm text-muted">{t("Nothing is on this page yet.")}</Panel>}
    </div>
  );
}

function OverlayBody({ children, session, sectionIDs, owner }: { children: ReactNode; session: PageSessionStore; sectionIDs: string[]; owner:string }) {
  const key = JSON.stringify(sectionIDs);
  useEffect(() => () => session.endOverlay(owner), [session, key, owner]);
  return <div className="@container grid min-w-0 grid-cols-1 gap-3">{children}</div>;
}

/** Whether a page is composed of sections (ADR-0035) rather than the list-detail shorthand. */
export const isComposed = (page: Page) => page.layout === "composed";
