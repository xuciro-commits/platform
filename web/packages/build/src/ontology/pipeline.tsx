import { useApplicationWorkspace } from "../projects/application-scope";
import { useEffect, useRef, useState } from "react";
import { useHost, useReadQuery } from "@platform/app";
import { Button, Checkbox, Input, PageHeader, Panel, RecordList, Select, Tag, t, useUnsavedChanges } from "@platform/ui";
import { PERIODS, periodLabel } from "../automate/workflow-model";
import { cell } from "./dataset";

// Pipelines (ADR-0071): a dataset in, declared steps in order, expectations on
// the result, and a dataset or an object out. The host runs a published
// pipeline when its input gains a version, on a period, or on request; rows
// that fail an expectation are quarantined on the run, never written.
type Measure = { fn: string; column?: string; to: string };
type Step = { kind: string; columns?: string[]; from?: string; to?: string; column?: string; type?: string; op?: string; value?: string; formula?: string; dataset?: string; match?: string; as?: string; outer?: boolean; desc?: boolean; measures?: Measure[] };
type Expectation = { column: string; rule: string; value?: string };
type Run = { merged?: number; at: string; inputVersion: number; rows: number; written: number; quarantined: number; failed: number; error?: string; outputVersion?: number;
  quarantine?: { reason: string; row: Record<string, unknown> }[]; failures?: { id: string; outcome: string }[] };
// ADR-0073: rows land in the enterprise model as elements owned on the source system's behalf.
type EnterpriseTarget = { source: string; stereotype: string; id: string; name: string; shortName?: string; kind?: string; parent?: string; root?: string; in?: string; relation?: string; at?: string; atKind?: string; from?: string; until?: string; properties?: string[] };
const STEREOTYPES = ["ActualOrganization", "ActualPost", "ActualPerson", "ActualLocation", "ActualResource", "ActualProject"];
const AT_KINDS = ["ResponsibleFor", "FillsPost", "ActualOrganizationRole", "IsCapableToPerform"];
const EMPTY_ENTERPRISE: EnterpriseTarget = { source: "", stereotype: "ActualOrganization", id: "", name: "" };
type Draft = { id: string; revision: number; name: string; title: string; input: string; steps: Step[]; expectations: Expectation[]; outputDataset?: string; outputObject?: string; outputEnterprise?: EnterpriseTarget; key?: string; every?: string; state: string; requested?: boolean; last?: Run };
type DatasetRow = { id: string; name: string; title: string; schema?: { name: string; type: string }[] };
const empty = (): Draft => ({ id: "", revision: 0, name: "", title: "", input: "", steps: [], expectations: [], state: "draft" });
const KINDS = ["select", "rename", "cast", "filter", "compute", "lookup", "join", "dedupe", "aggregate", "sort"];
const OPS = ["=", "!=", "<", "<=", ">", ">=", "contains", "empty", "notempty"];
const TYPES = ["string", "number", "boolean", "date"];
const FNS = ["sum", "min", "max", "count", "avg"];
const RULES = ["notnull", "unique", "in", "matches", "range"];
const fieldClass = "grid min-w-0 gap-1 text-xs";
const kindHelp = (kind: string): string => ({
  select: t("Keep only these columns"), rename: t("Rename one column"), cast: t("Convert a column to a type"), filter: t("Keep rows where the condition holds"),
  compute: t("A new column from arithmetic over number columns"), lookup: t("Take columns from a matching row of another dataset"), join: t("Combine with another dataset's rows on a key"),
  dedupe: t("First row per key wins"), aggregate: t("Group by columns and measure"), sort: t("Order rows by a column") } as Record<string, string>)[kind] ?? "";

export function Pipelines() {
  const { source, role } = useHost(), { open } = useApplicationWorkspace();
  if (role("build") !== "builder") return <PageHeader title={t("Pipelines")} description={t("Only a builder can edit pipelines.")} />;
  return <div className="grid gap-3">
    <PageHeader title={t("Pipelines")} description={t("Transform a dataset into another dataset or into records of an object, step by step, with expectations that quarantine bad rows instead of writing them.")}
      actions={<Button onClick={() => open({ view: "pipeline", params: { id: "new" } })}>{t("New pipeline")}</Button>} />
    <RecordList source={source} type="build.pipeline" fields={["title", "name", "input", "outputDataset", "outputObject", "state"]} onOpen={(record) => open({ view: "pipeline", params: { id: record.id } })} />
  </div>;
}

export function PipelineEditor({ id }: { id: string }) {
  const { decide, role, entities } = useHost(), { open, close } = useApplicationWorkspace();
  const query = useReadQuery<{ record?: Draft }>(`/v1/records/build.pipeline/${encodeURIComponent(id)}`, 5000);
  const datasets = useReadQuery<{ records: DatasetRow[] }>("/v1/records/build.dataset?limit=200").data?.records ?? [];
  const [draft, setDraft] = useState<Draft>(empty), [dirty, setDirty] = useState(false), [busy, setBusy] = useState(false), [error, setError] = useState("");
  const loaded = useRef(""), baseRevision = useRef(0), lock = useRef(false);
  const load = (record: Draft) => { setDraft({ ...empty(), ...record, steps: record.steps ?? [], expectations: record.expectations ?? [] }); baseRevision.current = record.revision; loaded.current = `${record.id}:${record.revision}`; };
  const { markSaved, discardChanges } = useUnsavedChanges(dirty, () => { if (query.data?.record) load(query.data.record); else setDraft(empty()); setDirty(false); setError(""); });
  useEffect(() => { const record = query.data?.record; if (record && !dirty && !busy && loaded.current !== `${record.id}:${record.revision}`) load(record); }, [query.data, dirty, busy]);
  const patch = (change: Partial<Draft>) => { if (lock.current) return; setDraft((d) => ({ ...d, ...change })); setDirty(true); setError(""); };
  const perform = async (action: () => Promise<unknown>) => { if (lock.current) return; lock.current = true; setBusy(true); setError(""); try { await action(); } catch { setError(t("The pipeline could not be saved or loaded. Your draft is still here.")); } finally { lock.current = false; setBusy(false); } };
  const save = async (): Promise<{ id: string; revision: number } | undefined> => {
    const target = draft.id || crypto.randomUUID(), expected = baseRevision.current;
    const { name, title, input, steps, expectations, outputDataset, outputObject, outputEnterprise, key, every } = draft;
    const payload = { name, title, input, steps, expectations, outputDataset: outputDataset ?? "", outputObject: outputObject ?? "", outputEnterprise: outputEnterprise ?? null, key: key ?? "", every: every ?? "" };
    if (!await decide(`build.pipeline.${draft.id ? "edit" : "create"}`, { type: "build.pipeline", id: target }, payload, { expectedRevision: draft.id ? expected : 0, quiet: true, onRefused: setError })) return;
    baseRevision.current = expected + 1; loaded.current = `${target}:${expected + 1}`;
    setDraft((d) => ({ ...d, id: target, revision: expected + 1 })); markSaved(); setDirty(false);
    if (!draft.id) { open({ view: "pipeline", params: { id: target } }); close({ view: "pipeline", params: { id } }); }
    else await query.refetch();
    return { id: target, revision: expected + 1 };
  };
  const transition = async (name: "publish" | "run" | "pause") => {
    const saved = dirty ? await save() : { id: draft.id, revision: baseRevision.current };
    if (!saved) return;
    if (await decide(`build.pipeline.${name}`, { type: "build.pipeline", id: saved.id }, {}, { expectedRevision: saved.revision, quiet: true, onRefused: setError })) { const result = await query.refetch(); if (result.data?.record) load(result.data.record); }
  };
  if (role("build") !== "builder") return <PageHeader title={t("Pipelines")} description={t("Only a builder can edit pipelines.")} />;
  if (id !== "new" && !draft.id) return <PageHeader title={t("Pipelines")} description={query.isError ? t("The pipeline could not be loaded.") : t("Loading…")} />;
  const last = draft.last, input = datasets.find((d) => d.id === draft.input);
  const columns = input?.schema?.map((f) => f.name) ?? [];
  const setSteps = (steps: Step[]) => patch({ steps });
  const setStep = (i: number, change: Partial<Step>) => setSteps(draft.steps.map((s, j) => j === i ? { ...s, ...change } : s));
  const move = (i: number, by: number) => { const steps = [...draft.steps]; const [s] = steps.splice(i, 1); if (s) steps.splice(i + by, 0, s); setSteps(steps); };
  const setExpectations = (expectations: Expectation[]) => patch({ expectations });
  const out = draft.outputEnterprise ? "enterprise" : draft.outputObject ? "object" : "dataset";
  const ent = draft.outputEnterprise ?? EMPTY_ENTERPRISE;
  const patchEnterprise = (change: Partial<EnterpriseTarget>) => patch({ outputEnterprise: { ...ent, ...change } });
  const column = (label: string, field: "id" | "name" | "shortName" | "kind" | "parent" | "at" | "from" | "until", placeholder: string, help?: string) =>
    <label className={fieldClass}>{label}<Input value={ent[field] ?? ""} placeholder={placeholder} list="pipeline-columns" onChange={(e) => patchEnterprise({ [field]: e.target.value })} />{help && <span className="text-[11px] text-muted">{help}</span>}</label>;
  const target = entities.find((entity) => entity.type === draft.outputObject);
  return <div className="grid min-w-0 grid-cols-1 gap-3">
    <PageHeader title={draft.title || t("New pipeline")} description={t("Input → steps → expectations → output. Publish lets the host run it whenever the input gains a version; Run now asks for one run within seconds.")}
      actions={<div className="flex min-w-0 flex-wrap gap-2">
        <Button onClick={() => open({ view: "pipeline" })}>{t("Pipelines")}</Button>
        <Button disabled={busy || !dirty} onClick={discardChanges}>{t("Discard")}</Button>
        <Button disabled={busy || !dirty && !!draft.id} onClick={() => void perform(save)}>{t("Save pipeline")}</Button>
        <Button variant="primary" disabled={busy} onClick={() => void perform(() => transition("publish"))}>{t("Publish")}</Button>
        {draft.state === "published" && <Button disabled={busy || dirty || !!draft.requested} onClick={() => void perform(() => transition("run"))}>{draft.requested ? t("Run requested…") : t("Run now")}</Button>}
        {draft.state === "published" && <Button disabled={busy || dirty} onClick={() => void perform(() => transition("pause"))}>{t("Pause")}</Button>}
      </div>} />
    {error && <Panel role="alert" className="text-danger">{error}</Panel>}
    <fieldset disabled={busy} className="grid min-w-0 grid-cols-1 gap-3 lg:grid-cols-2">
      <Panel title={t("Input")} className="grid min-w-0 content-start gap-3">
        <label className={fieldClass}>{t("Pipeline name")}<Input disabled={draft.state === "published"} value={draft.name} placeholder="cleanmaterials" onChange={(e) => patch({ name: e.target.value })} /></label>
        <label className={fieldClass}>{t("Pipeline title")}<Input value={draft.title} onChange={(e) => patch({ title: e.target.value })} /></label>
        <label className={fieldClass}>{t("Input dataset")}<Select value={draft.input} onChange={(e) => patch({ input: e.target.value })}><option value="">{t("Choose a dataset")}</option>{datasets.map((d) => <option key={d.id} value={d.id}>{d.title}</option>)}</Select>
          {columns.length > 0 && <span className="text-[11px] text-muted">{t("Columns")}: <span className="font-mono">{columns.join(", ")}</span></span>}</label>
        <label className={fieldClass}>{t("Run every")}<Select value={draft.every ?? ""} onChange={(e) => patch({ every: e.target.value })}><option value="">{t("When the input gains a version, or when asked")}</option>{PERIODS.map((period) => <option key={period} value={period}>{periodLabel(period)}</option>)}</Select></label>
      </Panel>
      <Panel title={t("Output")} className="grid min-w-0 content-start gap-3">
        <label className={fieldClass}>{t("Write to")}<Select value={out} onChange={(e) => e.target.value === "object" ? patch({ outputDataset: "", outputObject: entities[0]?.type ?? "", outputEnterprise: undefined }) : e.target.value === "enterprise" ? patch({ outputDataset: "", outputObject: "", key: "", outputEnterprise: EMPTY_ENTERPRISE }) : patch({ outputObject: "", key: "", outputEnterprise: undefined })}>
          <option value="dataset">{t("A dataset (next version)")}</option><option value="object">{t("An object (one record per row)")}</option><option value="enterprise">{t("The enterprise model (one element per row)")}</option></Select></label>
        <datalist id="pipeline-columns">{columns.map((c) => <option key={c} value={c} />)}</datalist>
        {out === "enterprise" ? <>
          <p className="text-[11px] text-muted">{t("Each run syncs the system's slice of the enterprise model: new ids start today, ids no longer sent close today, history stays. Name a column, or write =value for a constant. The publisher needs the Enterprise admin role.")}</p>
          <label className={fieldClass}>{t("Source system")}<Input value={ent.source} placeholder="sap-om" onChange={(e) => patchEnterprise({ source: e.target.value })} /><span className="text-[11px] text-muted">{t("Element ids become source:id; the system owns them.")}</span></label>
          <label className={fieldClass}>{t("Stereotype")}<Select value={ent.stereotype} onChange={(e) => patchEnterprise({ stereotype: e.target.value })}>{STEREOTYPES.map((st) => <option key={st} value={st}>{st}</option>)}</Select></label>
          {column(t("Id column"), "id", "objid")}
          {column(t("Name column"), "name", "stext")}
          {column(t("Short name column"), "shortName", "short")}
          {column(t("Kind column"), "kind", "=department")}
          {column(t("Parent id column"), "parent", "parent", t("The parent's id in the same system; top units sit under the root."))}
          <label className={fieldClass}>{t("Root element")}<Input value={ent.root ?? ""} placeholder="org-root" onChange={(e) => patchEnterprise({ root: e.target.value })} /></label>
          <label className={fieldClass}>{t("Placed in kind")}<Input value={ent.in ?? ""} placeholder="management" onChange={(e) => patchEnterprise({ in: e.target.value })} /><span className="text-[11px] text-muted">{t("A relationship kind of the enterprise model, e.g. management or legal.")}</span></label>
          <label className={fieldClass}>{t("Relation word")}<Input value={ent.relation ?? ""} placeholder="part of" onChange={(e) => patchEnterprise({ relation: e.target.value })} /></label>
          {column(t("At column"), "at", "plant", t("Places the element at another: the owner of a resource, the post a person fills."))}
          <label className={fieldClass}>{t("At relationship")}<Select value={ent.atKind ?? ""} onChange={(e) => patchEnterprise({ atKind: e.target.value })}><option value="">ResponsibleFor</option>{AT_KINDS.map((k) => <option key={k} value={k}>{k}</option>)}</Select></label>
          {column(t("From column"), "from", "begda")}
          {column(t("Until column"), "until", "endda")}
          <label className={fieldClass}>{t("Attribute columns")}<Input value={(ent.properties ?? []).join(", ")} placeholder="costcenter, bukrs" onChange={(e) => patchEnterprise({ properties: e.target.value.split(",").map((c) => c.trim()).filter(Boolean) })} /><span className="text-[11px] text-muted">{t("Kept on the element under the system's name.")}</span></label>
        </> : out === "dataset" ? <label className={fieldClass}>{t("Output dataset")}<Select value={draft.outputDataset ?? ""} onChange={(e) => patch({ outputDataset: e.target.value })}><option value="">{t("Choose a dataset")}</option>{datasets.filter((d) => d.id !== draft.input).map((d) => <option key={d.id} value={d.id}>{d.title}</option>)}</Select></label> : <>
          <label className={fieldClass}>{t("Output object")}<Select value={draft.outputObject ?? ""} onChange={(e) => patch({ outputObject: e.target.value })}>{entities.map((entity) => <option key={entity.type} value={entity.type}>{entity.title}</option>)}</Select>
            <span className="text-[11px] text-muted">{t("Columns are written to the object's fields of the same name; unknown columns are dropped.")}{target ? ` ${t("Fields")}: ${target.fields.filter((f) => !f.readOnly).map((f) => f.name).join(", ")}` : ""}</span></label>
          <label className={fieldClass}>{t("Record id column")}<Input value={draft.key ?? ""} placeholder="matnr" onChange={(e) => patch({ key: e.target.value })} /><span className="text-[11px] text-muted">{t("The same id edits the existing record.")}</span></label>
        </>}
      </Panel>
      <Panel title={t("Steps")} className="grid min-w-0 content-start gap-2 lg:col-span-2">
        {draft.steps.length === 0 && <p className="text-xs text-muted">{t("No steps: the input rows pass through as they are.")}</p>}
        {draft.steps.map((s, i) => <div key={i} className="grid gap-2 rounded border border-border p-2">
          <div className="flex flex-wrap items-center gap-2">
            <span className="w-6 text-xs text-muted">{i + 1}.</span>
            <Select value={s.kind} onChange={(e) => setSteps(draft.steps.map((x, j) => j === i ? { kind: e.target.value } : x))}>{KINDS.map((k) => <option key={k} value={k}>{t(k)}</option>)}</Select>
            <span className="text-[11px] text-muted">{kindHelp(s.kind)}</span>
            <span className="ml-auto flex gap-1"><Button size="sm" variant="ghost" disabled={i === 0} onClick={() => move(i, -1)}>↑</Button><Button size="sm" variant="ghost" disabled={i === draft.steps.length - 1} onClick={() => move(i, 1)}>↓</Button><Button size="sm" variant="ghost" onClick={() => setSteps(draft.steps.filter((_, j) => j !== i))}>{t("Remove")}</Button></span>
          </div>
          <StepFields step={s} datasets={datasets.filter((d) => d.id !== draft.input)} onChange={(change) => setStep(i, change)} />
        </div>)}
        <div><Button size="sm" onClick={() => setSteps([...draft.steps, { kind: "filter" }])}>{t("Add step")}</Button></div>
      </Panel>
      <Panel title={t("Expectations")} className="grid min-w-0 content-start gap-2 lg:col-span-2">
        <p className="text-xs text-muted">{t("Checked on the final rows. A row that fails is quarantined on the run and not written; the rest still are.")}</p>
        {draft.expectations.map((x, i) => <div key={i} className="grid grid-cols-[1fr_auto_1fr_auto] items-center gap-2">
          <Input value={x.column} placeholder={t("column")} onChange={(e) => setExpectations(draft.expectations.map((y, j) => j === i ? { ...y, column: e.target.value } : y))} />
          <Select value={x.rule} onChange={(e) => setExpectations(draft.expectations.map((y, j) => j === i ? { ...y, rule: e.target.value } : y))}>{RULES.map((r) => <option key={r} value={r}>{t(r)}</option>)}</Select>
          <Input disabled={x.rule === "notnull" || x.rule === "unique"} value={x.value ?? ""} placeholder={x.rule === "in" ? "A, B, C" : x.rule === "matches" ? "^[A-Z]{3}-\\d+$" : x.rule === "range" ? "0..100" : ""} onChange={(e) => setExpectations(draft.expectations.map((y, j) => j === i ? { ...y, value: e.target.value } : y))} />
          <Button size="sm" variant="ghost" onClick={() => setExpectations(draft.expectations.filter((_, j) => j !== i))}>{t("Remove")}</Button>
        </div>)}
        <div><Button size="sm" onClick={() => setExpectations([...draft.expectations, { column: columns[0] ?? "", rule: "notnull" }])}>{t("Add expectation")}</Button></div>
      </Panel>
      <Panel title={t("Last run")} className="grid min-w-0 content-start gap-2 lg:col-span-2">
        {!last ? <p className="text-xs text-muted">{t("Not run yet.")}</p> : <>
          <p className="flex flex-wrap items-center gap-2 text-xs"><Tag label={last.error ? t("Failed") : last.quarantined || last.failed ? t("Partly written") : t("Written")} tone={last.error ? "danger" : last.quarantined || last.failed ? "warning" : "success"} />
            <span>{new Date(last.at).toLocaleString()}</span><span>{t("input v{v}", { v: last.inputVersion })}</span>
            <span>{t("{rows} rows · {written} written · {quarantined} quarantined · {failed} refused", { rows: last.rows, written: last.written, quarantined: last.quarantined, failed: last.failed })}{last.merged ? ` · ${t("{n} merged by the matching rule", { n: last.merged })}` : ""}</span>
            {last.outputVersion ? <span>{t("output v{v}", { v: last.outputVersion })}</span> : null}</p>
          {last.error && <p className="text-xs text-danger">{last.error}</p>}
          {last.quarantine?.length ? <ul className="grid gap-1 text-xs">{last.quarantine.map((q, i) => <li key={i} className="font-mono break-all"><span className="text-warning">{q.reason}</span> · {Object.entries(q.row).map(([k, v]) => `${k}=${cell(v)}`).join(" ")}</li>)}</ul> : null}
          {last.failures?.length ? <ul className="grid gap-1 text-xs">{last.failures.map((f, i) => <li key={i} className="font-mono break-all">{f.id || "—"} · {f.outcome}</li>)}</ul> : null}
        </>}
      </Panel>
    </fieldset>
  </div>;
}

function StepFields({ step: s, datasets, onChange }: { step: Step; datasets: DatasetRow[]; onChange: (change: Partial<Step>) => void }) {
  const list = (v?: string[]) => (v ?? []).join(", ");
  const parse = (v: string) => v.split(",").map((x) => x.trim()).filter(Boolean);
  const text = (label: string, key: keyof Step, placeholder = "") => <label className={fieldClass}>{t(label)}<Input value={(s[key] as string | undefined) ?? ""} placeholder={placeholder} onChange={(e) => onChange({ [key]: e.target.value })} /></label>;
  const other = <label className={fieldClass}>{t("Other dataset")}<Select value={s.dataset ?? ""} onChange={(e) => onChange({ dataset: e.target.value })}><option value="">{t("Choose a dataset")}</option>{datasets.map((d) => <option key={d.id} value={d.id}>{d.title}</option>)}</Select></label>;
  const row = "grid min-w-0 grid-cols-1 gap-2 md:grid-cols-4";
  switch (s.kind) {
    case "select": return <div className={row}><label className={`${fieldClass} md:col-span-4`}>{t("Columns")}<Input value={list(s.columns)} placeholder="matnr, plant, qty" onChange={(e) => onChange({ columns: parse(e.target.value) })} /></label></div>;
    case "rename": return <div className={row}>{text("From", "from", "MaterialNumber")}{text("To", "to", "matnr")}</div>;
    case "cast": return <div className={row}>{text("Column", "column", "qty")}<label className={fieldClass}>{t("Type")}<Select value={s.type ?? ""} onChange={(e) => onChange({ type: e.target.value })}><option value="">{t("Choose a type")}</option>{TYPES.map((x) => <option key={x} value={x}>{t(x)}</option>)}</Select></label></div>;
    case "filter": return <div className={row}>{text("Column", "column", "plant")}<label className={fieldClass}>{t("Condition")}<Select value={s.op ?? "="} onChange={(e) => onChange({ op: e.target.value })}>{OPS.map((x) => <option key={x} value={x}>{x}</option>)}</Select></label>{s.op !== "empty" && s.op !== "notempty" && text("Value", "value", "1000")}</div>;
    case "compute": return <div className={row}>{text("New column", "to", "value")}<label className={`${fieldClass} md:col-span-3`}>{t("Formula")}<Input value={s.formula ?? ""} placeholder="qty * price" onChange={(e) => onChange({ formula: e.target.value })} /><span className="text-[11px] text-muted">{t("Arithmetic over number columns: + - * / and parentheses.")}</span></label></div>;
    case "lookup": case "join": return <div className={row}>{other}{text("Column here", "column", "plant")}{text("Column there", "match", "werks")}{text("Prefix", "as", "plant_")}
      {s.kind === "lookup" ? <label className={`${fieldClass} md:col-span-4`}>{t("Columns to take")}<Input value={list(s.columns)} placeholder="name, region" onChange={(e) => onChange({ columns: parse(e.target.value) })} /></label>
        : <Checkbox checked={!!s.outer} onChange={(outer) => onChange({ outer })}>{t("Keep rows without a match")}</Checkbox>}</div>;
    case "dedupe": return <div className={row}><label className={`${fieldClass} md:col-span-4`}>{t("Key columns")}<Input value={list(s.columns)} placeholder="matnr, plant" onChange={(e) => onChange({ columns: parse(e.target.value) })} /></label></div>;
    case "aggregate": return <div className="grid gap-2">
      <label className={fieldClass}>{t("Group by")}<Input value={list(s.columns)} placeholder="matnr, plant" onChange={(e) => onChange({ columns: parse(e.target.value) })} /></label>
      {(s.measures ?? []).map((m, i) => <div key={i} className="grid grid-cols-[auto_1fr_1fr_auto] items-center gap-2">
        <Select value={m.fn} onChange={(e) => onChange({ measures: s.measures!.map((x, j) => j === i ? { ...x, fn: e.target.value } : x) })}>{FNS.map((f) => <option key={f} value={f}>{f}</option>)}</Select>
        <Input disabled={m.fn === "count"} value={m.column ?? ""} placeholder={t("column")} onChange={(e) => onChange({ measures: s.measures!.map((x, j) => j === i ? { ...x, column: e.target.value } : x) })} />
        <Input value={m.to} placeholder={t("into column")} onChange={(e) => onChange({ measures: s.measures!.map((x, j) => j === i ? { ...x, to: e.target.value } : x) })} />
        <Button size="sm" variant="ghost" onClick={() => onChange({ measures: s.measures!.filter((_, j) => j !== i) })}>{t("Remove")}</Button>
      </div>)}
      <div><Button size="sm" onClick={() => onChange({ measures: [...(s.measures ?? []), { fn: "sum", column: "", to: "" }] })}>{t("Add measure")}</Button></div>
    </div>;
    case "sort": return <div className={row}>{text("Column", "column", "changed")}<Checkbox checked={!!s.desc} onChange={(desc) => onChange({ desc })}>{t("Descending")}</Checkbox></div>;
    default: return null;
  }
}
