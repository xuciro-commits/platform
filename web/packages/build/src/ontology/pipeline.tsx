import { DraftStatus, ResourceControls } from "../editor/workbench";
import { PropertyList, StructuredEditor , PanelGroup , Notice } from "@platform/ui";
import {formulaText} from "../editor/rule-text";
import { useDefinitionDraft } from "../session/definition-draft";
import { useApplicationWorkspace } from "../projects/application-scope";
import { ResourceList } from "../editor/ResourceList";
import { ArrowDown, ArrowUp } from "lucide-react";
import { useState } from "react";
import { useHost, useReadQuery } from "@platform/app";
import { apiErrorMessage, type Api } from "@platform/kernel";
import { MarkingTag } from "./marking";
import { Button, Checkbox, DataTable, FlowCanvas, Input, PageHeader, Panel, Select, Tag, t, type ColumnDef, type FlowCatalog, type FlowEdge, type FlowNode } from "@platform/ui";
import { PERIODS, periodLabel } from "../automate/workflow-model";
import { cell } from "./dataset";
import { useAuthoringModel } from "./authoring-model";
import { DeclaredByObject, declaringObject } from "./object-datasources";

type Measure = { fn: string; column?: string; to: string };
type Step = { kind: string; columns?: string[]; from?: string; to?: string; column?: string; type?: string; op?: string; value?: string; formula?: string;formulaExpr?:import("@platform/kernel").Api.Expr; dataset?: string; match?: string; as?: string; outer?: boolean; desc?: boolean; measures?: Measure[] };
type Expectation = { column: string; rule: string; value?: string };
type Run = { merged?: number; at: string; inputVersion: number; rows: number; written: number; quarantined: number; failed: number; error?: string; outputVersion?: number;
  quarantine?: { reason: string; row: Record<string, unknown> }[]; failures?: { id: string; outcome: string }[] };
type EnterpriseTarget = { source: string; stereotype: string; id: string; name: string; shortName?: string; kind?: string; parent?: string; root?: string; in?: string; relation?: string; at?: string; atKind?: string; from?: string; until?: string; properties?: string[] };
const STEREOTYPES = ["ActualOrganization", "ActualPost", "ActualPerson", "ActualLocation", "ActualResource", "ActualProject"];
const AT_KINDS = ["ResponsibleFor", "FillsPost", "ActualOrganizationRole", "IsCapableToPerform"];
const EMPTY_ENTERPRISE: EnterpriseTarget = { source: "", stereotype: "ActualOrganization", id: "", name: "" };
type Draft = { id: string; revision: number; name: string; title: string; input: string; steps: Step[]; expectations: Expectation[]; outputDataset?: string; outputObject?: string; outputEnterprise?: EnterpriseTarget; key?: string; every?: string; marking?: string; state: string; requested?: boolean; last?: Run };
type DatasetRow = { id: string; name: string; title: string; schema?: { name: string; type: string }[] };
const empty = (): Draft => ({ id: "", revision: 0, name: "", title: "", input: "", steps: [], expectations: [], state: "draft" });
const KINDS = ["select", "rename", "cast", "filter", "compute", "lookup", "join", "dedupe", "aggregate", "sort"];
const OPS = ["=", "!=", "<", "<=", ">", ">=", "contains", "empty", "notempty"];
const TYPES = ["string", "number", "boolean", "date"];
const FNS = ["sum", "min", "max", "count", "avg"];
const RULES = ["notnull", "unique", "in", "matches", "range"];
const fieldClass = "grid min-w-0 gap-1 text-xs";
const rows = (label: string, limit = 1) => ({ id: "rows", label, type: "rows", limit, channel: "data" as const });
const stepCatalog = (): FlowCatalog => [
  ...KINDS.map((kind) => ({ id: kind, title: t(kind), description: kindHelp(kind), class: "transform" as const,
    inputs: [rows(t("Rows in"))], outputs: [rows(t("Rows out"))] })),
  { id: "dataset", title: t("Dataset"), class: "document" as const, addable: false, notation: "event-start", inputs: [], outputs: [rows(t("Rows out"))] },
  { id: "output", title: t("Output"), class: "document" as const, addable: false, notation: "event-end", inputs: [rows(t("Rows in"))], outputs: [] },
];

function stepDetail(s: Step, datasets: DatasetRow[]): string {
  const other = datasets.find((d) => d.id === s.dataset)?.title ?? s.dataset ?? "";
  switch (s.kind) {
    case "select": case "dedupe": return (s.columns ?? []).join(", ");
    case "rename": return `${s.from ?? ""} → ${s.to ?? ""}`;
    case "cast": return `${s.column ?? ""} · ${s.type ?? ""}`;
    case "filter": return [s.column, s.op, s.value].filter(Boolean).join(" ");
    case "compute": return `${s.to ?? ""} = ${s.formula ?? ""}`;
    case "lookup": case "join": return [other, s.column && s.match ? `${s.column} = ${s.match}` : "", s.as ? `${s.as}*` : ""].filter(Boolean).join(" · ");
    case "aggregate": return [...(s.columns ?? []), ...(s.measures ?? []).map((m) => `${m.fn}(${m.column ?? "*"}) → ${m.to}`)].join(", ");
    case "sort": return `${s.column ?? ""}${s.desc ? " ↓" : ""}`;
    default: return "";
  }
}
const kindHelp = (kind: string): string => ({
  select: t("Keep only these columns"), rename: t("Rename one column"), cast: t("Convert a column to a type"), filter: t("Keep rows where the condition holds"),
  compute: t("A new column from arithmetic over number columns"), lookup: t("Take columns from a matching row of another dataset"), join: t("Combine with another dataset's rows on a key"),
  dedupe: t("First row per key wins"), aggregate: t("Group by columns and measure"), sort: t("Order rows by a column") } as Record<string, string>)[kind] ?? "";

export function Pipelines() {
  const { source , holds} = useHost(), { open } = useApplicationWorkspace();
  if (!holds("build", "builder", "integrator")) return <PageHeader title={t("Pipelines")} description={t("Only a builder or integrator can edit pipelines.")} />;
  return <div className="grid gap-3">
    <PageHeader title={t("Pipelines")} help={t("Transform a dataset into another dataset or into records of an object, step by step, with expectations that quarantine bad rows instead of writing them.")}
      actions={<Button variant="primary" onClick={() => open({ view: "pipeline", params: { id: "new" } })}>{t("New pipeline")}</Button>} />
    <ResourceList source={source} type="build.pipeline" fields={["title", "name", "input", "outputDataset", "outputObject", "state"]} onOpen={(record) => open({ view: "pipeline", params: { id: record.id } })} />
  </div>;
}

export function PipelineEditor({ id }: { id: string }) {
  const host = useHost(), { client, holds } = host, { open } = useApplicationWorkspace();
  const { entities, drafts: objects } = useAuthoringModel();
  const datasets = useReadQuery<{ records: DatasetRow[] }>("/v1/records/build.dataset?limit=200").data?.records ?? [];
  const enterprise = useReadQuery<Api.EnterpriseModel>("/v1/enterprise").data ?? { kinds: [], elements: [] };
  const { draft, structured, query, dirty, busy, error, inputs, patch, perform, save, discardChanges, transition } = useDefinitionDraft<Draft>({
    id, type: "build.pipeline", view: "pipeline", empty, normalize: record => ({ ...record, steps: record.steps ?? [], expectations: record.expectations ?? [] }),
    failure: t("The pipeline could not be saved or loaded. Your draft is still here."),
    payload: draft => {
      const { name, title, input, steps, expectations, outputDataset, outputObject, outputEnterprise, key, every } = draft;
      return { name, title, input, steps, expectations, outputDataset: outputDataset ?? "", outputObject: outputObject ?? "", outputEnterprise: outputEnterprise ?? null, key: declaringObject(entities, "pipeline", draft.id) ? "" : key ?? "", every: every ?? "" };
    },
  });
  const [picked, setPicked] = useState<string>();
  const [preview, setPreview] = useState<{ signature: string; result: Api.PipelinePreview }>();
  const signature = JSON.stringify(draft);
  const previewResult = preview?.signature === signature ? preview.result : undefined;
  const trial = async () => {
    setPreview(undefined);
    const response = await client.call<Api.PipelinePreview>("POST", "/v1/pipelines/preview", { pipeline: draft });
    if (!response.ok) throw Error(apiErrorMessage(response.body) ?? t("The pipeline trial could not be completed. Your draft is still here."));
    setPreview({ signature, result: response.body });
  };
  if (!holds("build", "builder", "integrator")) return <PageHeader title={t("Pipelines")} description={t("Only a builder or integrator can edit pipelines.")} />;
  if (id !== "new" && !draft.id) return <PageHeader title={t("Pipelines")} description={query.isError ? t("The pipeline could not be loaded.") : t("Loading…")} />;
  const last = draft.last, input = datasets.find((d) => d.id === draft.input);
  const columns = input?.schema?.map((f) => f.name) ?? [];
  const setSteps = (steps: Step[]) => patch({ steps });
  const setStep = (i: number, change: Partial<Step>) => setSteps((draft.steps ?? []).map((s, j) => j === i ? { ...s, ...change } : s));
  const move = (i: number, by: number) => { const steps = [...(draft.steps ?? [])]; const [s] = steps.splice(i, 1); if (s) steps.splice(i + by, 0, s); setSteps(steps); };
  const setExpectations = (expectations: Expectation[]) => patch({ expectations });
  const out = draft.outputEnterprise ? "enterprise" : draft.outputObject ? "object" : "dataset";
  const ent = draft.outputEnterprise ?? EMPTY_ENTERPRISE;
  const patchEnterprise = (change: Partial<EnterpriseTarget>) => patch({ outputEnterprise: { ...ent, ...change } });
  const column = (label: string, field: "id" | "name" | "shortName" | "kind" | "parent" | "at" | "from" | "until", placeholder: string, help?: string) =>
    <label className={fieldClass}>{label}<Input value={ent[field] ?? ""} placeholder={placeholder} list="pipeline-columns" onChange={(e) => patchEnterprise({ [field]: e.target.value })} />{help && <span className="text-xs text-muted">{help}</span>}</label>;
  const target = entities.find((entity) => entity.type === draft.outputObject);
  const owner = declaringObject(entities, "pipeline", draft.id);
  const targetDraft = owner ? objects.data?.records.find(record => !record.archived && `build.${record.name}` === owner.type) : undefined;
  const others = datasets.filter((d) => d.id !== draft.input);
  const stepNodes: FlowNode[] = [
    { id: "in", kind: "dataset", label: input?.title || draft.input || t("Input dataset"), detail: t("The dataset this pipeline reads"), position: { x: 0, y: 0 } },
    ...(draft.steps ?? []).map((s, i) => ({ id: `s${i}`, kind: s.kind, label: `${i + 1}. ${t(s.kind)}`, detail: stepDetail(s, others), position: { x: 0, y: 0 } })),
    { id: "out", kind: "output", label: out === "enterprise" ? t("The enterprise model") : out === "object" ? (target?.title ?? draft.outputObject ?? t("An object")) : (datasets.find((d) => d.id === draft.outputDataset)?.title ?? t("A dataset (next version)")),
      detail: out === "object" && draft.key ? `${t("Record id column")} ${draft.key}` : undefined, position: { x: 0, y: 0 } },
  ].map((node, i) => ({ ...node, position: { x: i * 268, y: 0 } }));
  const stepEdges: FlowEdge[] = stepNodes.slice(0, -1).map((node, i) => ({ id: `${node.id}>${stepNodes[i + 1]!.id}`, source: node.id, sourcePort: "rows", target: stepNodes[i + 1]!.id, targetPort: "rows" }));
  return <div className="grid min-w-0 grid-cols-1 gap-3">
    <PageHeader crumbs={[{ label: t("Pipelines"), onClick: () => open({ view: "pipeline" }) }]} title={draft.title || t("New pipeline")} status={<><MarkingTag marking={draft.marking} /><DraftStatus state={draft.state} /></>} meta={draft.name ? <span className="font-mono">{draft.name}</span> : undefined} help={t("Declare the input, transformations, expectations and output. Publishing saves the declaration; Run now explicitly requests a run with current permissions.")}
      actions={<ResourceControls type="build.pipeline" record={draft} dirty={dirty} busy={busy} route={{ view: "pipeline", params: { id } }} onCancel={discardChanges} onSave={() => perform(save)}
        operations={[
          { id: "trial", label: t("Trial pipeline"), disabled: !!inputs?.invalid || !draft.input, run: () => void perform(trial) },
          ...(draft.state === "published" ? [{ id: "run", label: draft.requested ? t("Run requested…") : t("Run now"), disabled: dirty || !!draft.requested, run: () => void perform(() => transition("run")) },
            { id: "pause", label: t("Pause"), disabled: dirty, run: () => void perform(() => transition("pause")) }] : []),
        ]} />} />
    {error && <Notice tone="danger" message={error} />}
    {objects.isError && holds("build", "builder") && <Notice tone="danger" message={t("Object drafts could not be loaded. Installed definitions are still available.")} />}
    {previewResult && <Panel title={t("Draft trial")} role="region" aria-label={t("Pipeline trial results")} className="grid min-w-0 gap-3">
      <p className="text-xs text-muted">{t("Transforms the current draft over readable dataset versions. No pipeline is published, and no dataset versions or business records are written.")}</p>
      <p className="flex flex-wrap items-center gap-2 text-xs"><MarkingTag marking={previewResult.marking} /><span>{t("Input v{version}: {sampled} of {total} rows · {rows} output rows · {quarantined} quarantined", { version: previewResult.inputVersion, sampled: previewResult.sampledRows, total: previewResult.inputRows, rows: previewResult.rows.length, quarantined: previewResult.quarantine.length })}</span></p>
      {previewResult.truncated && <p role="status" className="text-xs text-warning">{t("Each dataset is limited to its first 100 rows. Joins, counts and aggregates describe that sample, not the complete datasets.")}</p>}
      <div className="min-w-0" role="region" aria-label={t("Trial output rows")}><PipelineRows rows={previewResult.rows} /></div>
      {previewResult.quarantine.length > 0 && <div className="grid min-w-0 gap-2" role="region" aria-label={t("Trial quarantined rows")}>
        <h3 className="text-xs font-semibold">{t("Quarantined rows")}</h3>
        <DataTable data={previewResult.quarantine} getRowId={row => String(previewResult.quarantine.indexOf(row))} searchable={false} height={Math.min(320, 40 + previewResult.quarantine.length * 28)} columns={[
          { id: "reason", header: t("Reason"), accessorFn: row => row.reason },
          ...[...new Set(previewResult.quarantine.flatMap(item => Object.keys(item.row)))].map((name): ColumnDef<Api.PipelinePreview["quarantine"][number], unknown> => ({ id: `row:${name}`, header: name, accessorFn: item => item.row[name], cell: context => cell(context.getValue()) })),
        ]} />
      </div>}
    </Panel>}
    <StructuredEditor {...structured}><fieldset disabled={busy} className="min-w-0 border-0 p-0"><PanelGroup className="grid-cols-1 lg:grid-cols-2">
      <Panel title={t("Input")} className="grid min-w-0 content-start gap-3">
        <label className={fieldClass}>{t("Pipeline name")}<Input disabled={draft.state === "published"} value={draft.name} placeholder="cleanmaterials" onChange={(e) => patch({ name: e.target.value })} /></label>
        <label className={fieldClass}>{t("Pipeline title")}<Input value={draft.title} onChange={(e) => patch({ title: e.target.value })} /></label>
        <label className={fieldClass}>{t("Input dataset")}<Select value={draft.input} onChange={(e) => patch({ input: e.target.value })}><option value="">{t("Choose a dataset")}</option>{datasets.map((d) => <option key={d.id} value={d.id}>{d.title}</option>)}</Select>
          {columns.length > 0 && <span className="text-xs text-muted">{t("Columns")}: <span className="font-mono">{columns.join(", ")}</span></span>}</label>
        <label className={fieldClass}>{t("Run every")}<Select value={draft.every ?? ""} onChange={(e) => patch({ every: e.target.value })}><option value="">{t("When the input gains a version, or when asked")}</option>{PERIODS.map((period) => <option key={period} value={period}>{periodLabel(period)}</option>)}</Select></label>
      </Panel>
      <Panel title={t("Output")} className="grid min-w-0 content-start gap-3">
        <label className={fieldClass}>{t("Write to")}<Select value={out} onChange={(e) => e.target.value === "object" ? patch({ outputDataset: "", outputObject: entities[0]?.type ?? "", outputEnterprise: undefined }) : e.target.value === "enterprise" ? patch({ outputDataset: "", outputObject: "", key: "", outputEnterprise: EMPTY_ENTERPRISE }) : patch({ outputObject: "", key: "", outputEnterprise: undefined })}>
          <option value="dataset">{t("A dataset (next version)")}</option><option value="object">{t("An object (one record per row)")}</option><option value="enterprise">{t("The enterprise model (one element per row)")}</option></Select></label>
        <datalist id="pipeline-columns">{columns.map((c) => <option key={c} value={c} />)}</datalist>
        {out === "enterprise" ? <>
          <p className="text-xs text-muted">{t("Each run syncs the system's slice of the enterprise model: new ids start today, ids no longer sent close today, history stays. Name a column, or write =value for a constant. The running member needs the Enterprise admin role.")}</p>
          <label className={fieldClass}>{t("Source system")}<Input value={ent.source} placeholder="sap-om" onChange={(e) => patchEnterprise({ source: e.target.value })} /><span className="text-xs text-muted">{t("Element ids become source:id; the system owns them.")}</span></label>
          <label className={fieldClass}>{t("Stereotype")}<Select value={ent.stereotype} onChange={(e) => patchEnterprise({ stereotype: e.target.value })}>{STEREOTYPES.map((st) => <option key={st} value={st}>{st}</option>)}</Select></label>
          {column(t("Id column"), "id", "objid")}
          {column(t("Name column"), "name", "stext")}
          {column(t("Short name column"), "shortName", "short")}
          {column(t("Kind column"), "kind", "=department")}
          {column(t("Parent id column"), "parent", "parent", t("The parent's id in the same system; top units sit under the root."))}
          <label className={fieldClass}>{t("Root element")}<Select value={ent.root ?? ""} onChange={(e) => patchEnterprise({ root: e.target.value })}>
            <option value="">{t("Choose an element of the enterprise model")}</option>
            {ent.root && !enterprise.elements.some((el) => el.id === ent.root) && <option value={ent.root}>{ent.root}</option>}
            {enterprise.elements.filter((el) => !el.until).map((el) => <option key={el.id} value={el.id}>{el.name} · {el.id}</option>)}
          </Select></label>
          <label className={fieldClass}>{t("Placed in kind")}<Select value={ent.in ?? ""} onChange={(e) => patchEnterprise({ in: e.target.value })}>
            <option value="">{t("Choose a relationship kind")}</option>
            {ent.in && !enterprise.kinds.some((k) => k.id === ent.in) && <option value={ent.in}>{ent.in}</option>}
            {enterprise.kinds.map((k) => <option key={k.id} value={k.id}>{k.name} · {k.id}</option>)}
          </Select><span className="text-xs text-muted">{t("A relationship kind of the enterprise model, e.g. management or legal.")}</span></label>
          <label className={fieldClass}>{t("Relation word")}<Input value={ent.relation ?? ""} placeholder="part of" onChange={(e) => patchEnterprise({ relation: e.target.value })} /></label>
          {column(t("At column"), "at", "plant", t("Places the element at another: the owner of a resource, the post a person fills."))}
          <label className={fieldClass}>{t("At relationship")}<Select value={ent.atKind ?? ""} onChange={(e) => patchEnterprise({ atKind: e.target.value })}><option value="">ResponsibleFor</option>{AT_KINDS.map((k) => <option key={k} value={k}>{k}</option>)}</Select></label>
          {column(t("From column"), "from", "begda")}
          {column(t("Until column"), "until", "endda")}
          <label className={fieldClass}>{t("Attribute columns")}<Input value={(ent.properties ?? []).join(", ")} placeholder="costcenter, bukrs" onChange={(e) => patchEnterprise({ properties: e.target.value.split(",").map((c) => c.trim()).filter(Boolean) })} /><span className="text-xs text-muted">{t("Kept on the element under the system's name.")}</span></label>
        </> : out === "dataset" ? <label className={fieldClass}>{t("Output dataset")}<Select value={draft.outputDataset ?? ""} onChange={(e) => patch({ outputDataset: e.target.value })}><option value="">{t("Choose a dataset")}</option>{datasets.filter((d) => d.id !== draft.input).map((d) => <option key={d.id} value={d.id}>{d.title}</option>)}</Select></label> : <>
          <label className={fieldClass}>{t("Output object")}<Select value={draft.outputObject ?? ""} onChange={(e) => patch({ outputObject: e.target.value })}>{entities.map((entity) => <option key={entity.type} value={entity.type}>{entity.title}</option>)}</Select>
            <span className="text-xs text-muted">{t("Columns are written to the object's fields of the same name; unknown columns are dropped.")}{target ? ` ${t("Fields")}: ${target.fields.filter((f) => !f.readOnly).map((f) => f.name).join(", ")}` : ""}</span></label>
          {owner ? <DeclaredByObject object={owner} onOpen={targetDraft && host.can("build.object.edit") ? () => open({ view: "object-type", params: { id: targetDraft.id, tab: "datasources" } }) : undefined} /> : <label className={fieldClass}>{t("Record id column")}<Input value={draft.key ?? ""} placeholder="matnr" onChange={(e) => patch({ key: e.target.value })} /><span className="text-xs text-muted">{t("The same id edits the existing record.")}</span></label>}
        </>}
      </Panel>
      <Panel title={t("Steps")} className="grid min-w-0 content-start gap-2 lg:col-span-2"
        help={t("Drag a step from the library onto the line, or insert one on the arrow where it belongs. Choose a step to edit its fields below.")}>
        <FlowCanvas catalog={stepCatalog()} nodes={stepNodes} edges={stepEdges} selected={picked} onSelect={setPicked}
          mode={busy ? "view" : "edit"} direction="right" height={280} label={t("Steps")} storeKey={`pipeline:${id}`}
          onAdd={(kind) => { setSteps([...(draft.steps ?? []), { kind }]); setPicked(`s${(draft.steps ?? []).length}`); }}
          onInsert={(edge, kind) => { const at = edge.source === "in" ? 0 : Number(edge.source.slice(1)) + 1; setSteps([...(draft.steps ?? []).slice(0, at), { kind }, ...(draft.steps ?? []).slice(at)]); setPicked(`s${at}`); }}
          onDelete={(gone) => { const drop = gone.map((n) => Number(n.id.slice(1))).filter((i) => !Number.isNaN(i)).sort((a, b) => b - a); if (drop.length) { setSteps((draft.steps ?? []).filter((_, j) => !drop.includes(j))); setPicked(undefined); } }} />
        {picked && picked.startsWith("s") && (draft.steps ?? [])[Number(picked.slice(1))] && <div className="grid gap-2 rounded border border-border p-2">
          <div className="flex flex-wrap items-center gap-2">
            <span className="w-6 text-xs text-muted">{Number(picked.slice(1)) + 1}.</span>
            <Select aria-label={t("Step kind")} value={(draft.steps ?? [])[Number(picked.slice(1))]!.kind} onChange={(e) => setStep(Number(picked.slice(1)), { kind: e.target.value })}>{KINDS.map((k) => <option key={k} value={k}>{t(k)}</option>)}</Select>
            <span className="text-xs text-muted">{kindHelp((draft.steps ?? [])[Number(picked.slice(1))]!.kind)}</span>
            <span className="ml-auto flex gap-1">
              <Button size="sm" variant="ghost" aria-label={t("Move up")} disabled={picked === "s0"} onClick={() => move(Number(picked.slice(1)), -1)}><ArrowUp className="size-3" /></Button>
              <Button size="sm" variant="ghost" aria-label={t("Move down")} disabled={Number(picked.slice(1)) === (draft.steps ?? []).length - 1} onClick={() => move(Number(picked.slice(1)), 1)}><ArrowDown className="size-3" /></Button>
              <Button size="sm" variant="ghost" onClick={() => { setSteps((draft.steps ?? []).filter((_, j) => j !== Number(picked.slice(1)))); setPicked(undefined); }}>{t("Remove")}</Button>
            </span>
          </div>
          <StepFields step={(draft.steps ?? [])[Number(picked.slice(1))]!} datasets={datasets.filter((d) => d.id !== draft.input)} onChange={(change) => setStep(Number(picked.slice(1)), change)} />
        </div>}
      </Panel>
      <Panel title={t("Expectations")} className="grid min-w-0 content-start gap-2 lg:col-span-2" help={t("Checked on the final rows. A row that fails is quarantined on the run and not written; the rest still are.")}>
        {(draft.expectations ?? []).map((x, i) => <div key={i} className="grid grid-cols-[1fr_auto_1fr_auto] items-center gap-2">
          <Input value={x.column} placeholder={t("column")} onChange={(e) => setExpectations((draft.expectations ?? []).map((y, j) => j === i ? { ...y, column: e.target.value } : y))} />
          <Select value={x.rule} onChange={(e) => setExpectations((draft.expectations ?? []).map((y, j) => j === i ? { ...y, rule: e.target.value } : y))}>{RULES.map((r) => <option key={r} value={r}>{t(r)}</option>)}</Select>
          <Input disabled={x.rule === "notnull" || x.rule === "unique"} value={x.value ?? ""} placeholder={x.rule === "in" ? "A, B, C" : x.rule === "matches" ? "^[A-Z]{3}-\\d+$" : x.rule === "range" ? "0..100" : ""} onChange={(e) => setExpectations((draft.expectations ?? []).map((y, j) => j === i ? { ...y, value: e.target.value } : y))} />
          <Button size="sm" variant="ghost" onClick={() => setExpectations((draft.expectations ?? []).filter((_, j) => j !== i))}>{t("Remove")}</Button>
        </div>)}
        <div><Button size="sm" onClick={() => setExpectations([...(draft.expectations ?? []), { column: columns[0] ?? "", rule: "notnull" }])}>{t("Add expectation")}</Button></div>
      </Panel>
      <Panel title={t("Last run")} className="grid min-w-0 content-start gap-2 lg:col-span-2">
        {!last ? <p className="text-xs text-muted">{t("Not run yet.")}</p> : <>
          <div className="flex flex-wrap items-center gap-2 text-xs"><Tag label={last.error ? t("Failed") : last.quarantined || last.failed ? t("Partly written") : t("Written")} tone={last.error ? "danger" : last.quarantined || last.failed ? "warning" : "success"} /><time>{new Date(last.at).toLocaleString()}</time></div>
          <PropertyList columns={3} items={[[t("Rows"), last.rows], [t("Written"), last.written], [t("Quarantined"), last.quarantined], [t("Refused"), last.failed], [t("Input version"), last.inputVersion], [t("Output version"), last.outputVersion ?? "—"], ...(last.merged ? [[t("Merged"), last.merged] as [string, React.ReactNode]] : [])]} />
          {last.error && <p className="text-xs text-danger">{last.error}</p>}
          {last.quarantine?.length ? <ul className="grid gap-1 text-xs">{last.quarantine.map((q, i) => <li key={i} className="font-mono break-all"><span className="text-warning">{q.reason}</span> · {Object.entries(q.row).map(([k, v]) => `${k}=${cell(v)}`).join(" ")}</li>)}</ul> : null}
          {last.failures?.length ? <ul className="grid gap-1 text-xs">{last.failures.map((f, i) => <li key={i} className="font-mono break-all">{f.id || "—"} · {f.outcome}</li>)}</ul> : null}
        </>}
      </Panel>
    </PanelGroup></fieldset></StructuredEditor>
  </div>;
}

function PipelineRows({ rows }: { rows: Record<string, unknown>[] }) {
  const columns = [...new Set(rows.flatMap(row => Object.keys(row)))];
  return <DataTable data={rows} getRowId={row => String(rows.indexOf(row))} searchable={false} height={Math.min(420, 40 + rows.length * 28)} columns={columns.map((name): ColumnDef<Record<string, unknown>, unknown> => ({ id: name, header: name, accessorFn: row => row[name], cell: context => cell(context.getValue()) }))} />;
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
    case "compute": return <div className={row}>{text("New column", "to", "value")}<label className={`${fieldClass} md:col-span-3`}>{t("Formula")}<Input value={s.formula ?? (s.formulaExpr?formulaText(s.formulaExpr):"")} placeholder="qty * price" onChange={(e) => onChange({ formula: e.target.value,formulaExpr:undefined })} /><span className="text-xs text-muted">{t("Arithmetic over number columns: + - * / and parentheses.")}</span></label></div>;
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
