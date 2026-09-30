// A composed page (ADR-0035): sections laid out in order, each holding one
// widget bound to what this tenant has. A table says which record is selected;
// a detail and the actions read it. Every widget renders through the owner that
// already has it — the kit's record list and record page, the action catalog,
// the aggregate chart — so a code page and a composed page look and behave the
// same, and nothing here interprets data of its own.
import {
  Button, Card, Chart, Markdown, Panel, PropertyList, RecordHistory, RecordList, RecordLookup, RecordPage, Select, Tasks, cn, t, type ChartSpec, type Encoding, type EntityRecord, type RecordView,
} from "@platform/ui";
import { useEffect, useState, type ReactNode } from "react";
import { NewActions, RecordActions, prefixOf } from "./actions";
import { GeneratedForm, RecordDetail, findDefinition, newId, useHost, type Definition } from "./index";
import { ComputeCall } from "./capability";

type Page = NonNullable<Definition["page"]>;
type Section = NonNullable<Page["sections"]>[number];

/** The page's second variable (16b): the conditions each filter set, by the
 *  object they narrow. A table, chart or metric over that object reads them. */
type Narrowed = Record<string, Record<string, unknown>>;

/** What a section is bound to, and what the page has selected and narrowed to. */
type Bound = {
  page: Page; section: Section; selected?: EntityRecord; onSelect: (record?: EntityRecord) => void; live: boolean;
  narrowed: Narrowed; onNarrow: (object: string, field: string, value: unknown) => void;
};

/** Composing: the section in hand, and choosing another by clicking it. */
type Composing = { chosen?: number; onChoose?: (at: number) => void; at?: number };

const objectOf = (page: Page, section: Section) => section.object?.name || page.object.name;

/** The filters' conditions over an object, as the host's domain (ADR-0019). */
const domainOf = (narrowed: Narrowed, object: string): unknown[] =>
  Object.entries(narrowed[object] ?? {}).filter(([, v]) => v !== undefined && v !== "").map(([field, v]) => [field, "=", v]);

/** The reference that ties a section's object to the page's selected record:
 *  the declared relation when the section names one (ADR-0040 21b), else the
 *  first reference to the page's object. */
const relatedField = (fields: { name: string; type: string; ref?: string; inverse?: string }[] | undefined, page: Page, section: Section) =>
  fields?.find((f) => f.type === "reference" && f.ref === page.object.name && (!section.relation || f.inverse === section.relation));

/** The records of an object, as a list; selecting one fills the rest of the page. */
function TableWidget({ page, section, onSelect, selected, narrowed }: Bound) {
  const { source, definitions } = useHost();
  const type = objectOf(page, section);
  const isMaster = type === page.object.name;
  const info = source.entity(type);
  // A named query (ADR-0040 21c): its declared conditions, run for the selected
  // record through its reference; the list is still the member's own read.
  const query = section.query?.name ? findDefinition(definitions, section.query)?.query : undefined;
  const refField = !isMaster
    ? (query?.by ? info?.fields.find((f) => f.name === query.by) : relatedField(info?.fields, page, section))
    : undefined;

  if (refField && !selected) {
    return (
      <div className="flex h-40 items-center justify-center rounded-md border border-dashed border-border p-4 text-center">
        <p className="text-sm text-muted">
          {t("Select a record to see related {records}.", { records: info?.plural?.toLowerCase() ?? type })}
        </p>
      </div>
    );
  }

  const relationDomain = refField && selected ? [[refField.name, "=", selected.id]] : [];
  const queryDomain = (query?.domain as unknown[] | undefined) ?? [];
  const domain = [...queryDomain, ...domainOf(narrowed, type), ...relationDomain];

  return (
    <RecordList source={source} type={type} fields={section.fields} height={320} domain={domain}
      onOpen={isMaster ? (record) => onSelect(record.id === selected?.id ? undefined : record) : undefined} />
  );
}

/** The record the page has selected, with the fields the builder chose. */
function DetailWidget({ page, section, selected }: Bound) {
  const type = objectOf(page, section);
  const isMaster = type === page.object.name;
  if (!selected) return <p className="text-sm text-muted">{t("Select a record to see it here.")}</p>;
  if (!isMaster && selected.type && selected.type !== type) {
    return <p className="text-sm text-muted">{t("Select a {object} to see it here.", { object: type })}</p>;
  }
  // The fields alone: what people do with it is the actions widget's (ADR-0035 D2).
  return <RecordDetail type={type} id={selected.id} fields={section.fields} allowed={[]} />;
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

function ChartWidget({ page, section, kpi, narrowed, selected }: Bound & { kpi: boolean }) {
  const { source } = useHost();
  const aggregate = source.aggregate;
  const type = objectOf(page, section);
  const isMaster = type === page.object.name;
  const info = source.entity(type);
  const refField = !isMaster ? relatedField(info?.fields, page, section) : undefined;
  const relationDomain = refField && selected ? [[refField.name, "=", selected.id]] : [];
  const domain = [...domainOf(narrowed, type), ...relationDomain];

  return <Chart spec={chartSpec(page, section, kpi, domain)} frame={false} height={kpi ? 120 : 240}
    source={aggregate ? { aggregate, revision: source.revision } : undefined} />;
}

/** The filter (16b): a value to narrow the object's records by, for each field
 *  the builder chose. What it sets is the page's second variable. */
function FilterWidget({ page, section, narrowed, onNarrow }: Bound) {
  const { source } = useHost();
  const type = objectOf(page, section);
  const info = source.entity(type);
  const set = narrowed[type] ?? {};
  return (
    <div role="search" aria-label={section.title || t("Filter")} className="flex flex-wrap items-end gap-3">
      {(section.fields ?? []).map((name) => {
        const f = info?.fields.find((x) => x.name === name);
        if (!f) return null; // not a field this member reads
        const id = `filter-${type}-${name}`;
        const value = set[name];
        return (
          <label key={name} htmlFor={id} className="grid gap-1 text-xs text-muted">{f.title}
            {f.type === "reference" && f.ref
              ? <RecordLookup id={id} source={source} type={f.ref} value={value as string | undefined} onChange={(v) => onNarrow(type, name, v)} />
              : <Select id={id} className="w-40" value={value === undefined ? "" : String(value)}
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
function FormWidget({ page, section, live }: Bound) {
  const { decide } = useHost();
  const type = objectOf(page, section);
  const [round, setRound] = useState(0); // a fresh, empty form after each record
  return (
    <div className="grid gap-2">
      {!live && <p className="text-xs text-muted">{t("The form does not submit while you compose.")}</p>}
      <GeneratedForm key={round} type={type} fields={section.fields} submitLabel={t("Create")} onCancel={() => setRound((r) => r + 1)}
        onSubmit={async (values) => {
          if (!live) return;
          if (await decide(`${type}.create`, { type, id: newId(prefixOf(type)) }, values, { expectedRevision: 0 })) setRound((r) => r + 1);
        }} />
    </div>
  );
}

/** The selected record as its page reads it: history, tasks waiting on it. */
function useRecordView(type: string, id?: string) {
  const { source } = useHost();
  const [view, setView] = useState<RecordView>();
  useEffect(() => {
    setView(undefined);
    if (id) source.get(type, id).then(setView, () => setView(undefined));
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
      case "form": return <FormWidget {...bound} />;
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
 * selected. `live` false is the builder's canvas — the same widgets over the
 * same records, with nothing that writes.
 */
export function ComposedPage({ page, live = true, notice, chosen, onChoose }: {
  page: Page; live?: boolean; notice?: ReactNode;
} & Composing) {
  const [selected, setSelected] = useState<EntityRecord>();
  const [narrowed, setNarrowed] = useState<Narrowed>({});
  const onNarrow = (object: string, field: string, value: unknown) => {
    setNarrowed((n) => ({ ...n, [object]: { ...n[object], [field]: value } }));
    setSelected(undefined); // what was selected may no longer be among them
  };
  return (
    <div className="grid gap-3">
      {notice}
      <div className="grid gap-3 md:grid-cols-2">
        {(page.sections ?? []).map((section, i) => (
          <SectionView key={i} page={page} section={section} selected={selected} onSelect={setSelected} live={live} narrowed={narrowed} onNarrow={onNarrow}
            chosen={chosen} onChoose={onChoose} at={i} />
        ))}
      </div>
      {(page.sections ?? []).length === 0 && <Panel role="status" className="text-sm text-muted">{t("Nothing is on this page yet.")}</Panel>}
    </div>
  );
}

/** Whether a page is composed of sections (ADR-0035) rather than the list-detail shorthand. */
export const isComposed = (page: Page) => page.layout === "composed";
