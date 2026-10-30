import type { Api } from "@platform/kernel";
import { useReadQuery } from "@platform/app";
import { Button, ContextHelp, EmptyState, Input, InputDraftProvider, Notice, Panel, Rejection, Select, t, type EntityInfo } from "@platform/ui";
import { Plus, Trash2 } from "lucide-react";
import type { Process } from "./object-model";

type Published = { id: string; name: string; title: string; state: string; object?: string; outputObject?: string };
const strategies = [
  ["edits-win", "Edits win (default)"],
  ["latest-source-time", "Latest source time wins"],
  ["edit-only", "Edits only"],
] as const;
const strategyHelp: Record<string, string> = {
  "edits-win": "A person's edit is kept; the source fills a value only while no edit exists.",
  "latest-source-time": "The newer of the person's edit and the source time wins.",
  "edit-only": "The attribute changes only by edits; pulled values are recorded but never applied.",
};
const fieldClass = "grid min-w-0 gap-1 text-xs";

export function ObjectDataSourcesEditor({ process, parent, refused, onChange }: { process: Process; parent: string; refused?: string; onChange: (next: Process) => void }) {
  const sources = useReadQuery<{ records: Published[] }>("/v1/records/build.source?limit=500");
  const pipelines = useReadQuery<{ records: Published[] }>("/v1/records/build.pipeline?limit=500");
  const bindings = process.datasources ?? [];
  const writable = process.fields.filter(field => !field.readOnly && !field.aliasOf && !field.formula && !field.formulaExpr);
  const set = (next: Api.DataSourceBinding[]) => onChange({ ...process, datasources: next.length ? next : undefined });
  const patch = (at: number, values: Partial<Api.DataSourceBinding>) => set(bindings.map((binding, index) => index === at ? { ...binding, ...values } : binding));
  const choices = (kind: string) => (kind === "pipeline" ? pipelines : sources).data?.records.filter(record => record.state === "published") ?? [];
  const targetOf = (kind: string, id: string) => { const record = (kind === "pipeline" ? pipelines : sources).data?.records.find(entry => entry.id === id); return kind === "pipeline" ? record?.outputObject : record?.object; };
  const add = () => {
    const first = choices("source")[0];
    set([...bindings, { kind: "source", source: first?.id ?? "", key: "", columns: [] }]);
  };
  return <div className="grid max-w-4xl content-start gap-3 p-4">
    <div className="flex items-center justify-between"><h2 className="text-sm font-semibold">{t("Data sources")}</h2><Button size="sm" onClick={add}><Plus />{t("Add a data source")}</Button></div>
    <ContextHelp title={t("Data sources")}><p className="text-sm text-muted">{t("The object declares which published sources and pipelines feed it, how a row is identified and which column feeds which attribute. Each attribute chooses what wins when a person edited it and a source also has a value.")}</p></ContextHelp>
    {refused && <Rejection message={refused} />}
    {(sources.isError || pipelines.isError) && <Notice tone="danger" message={t("The published sources and pipelines could not be loaded.")} />}
    {!bindings.length && <EmptyState title={t("No data sources declared")} hint={t("Without a declaration a Source or Pipeline keeps its own key and field mapping.")} className="rounded-md border border-border p-6" />}
    {bindings.map((binding, at) => {
      const known = choices(binding.kind), target = targetOf(binding.kind, binding.source);
      return <InputDraftProvider key={at} scope={JSON.stringify({ kind: "datasource", at })}><Panel title={t("Data source {number}", { number: at + 1 })} actions={<Button size="sm" variant="ghost" aria-label={t("Remove data source")} onClick={() => set(bindings.filter((_, index) => index !== at))}><Trash2 />{t("Remove")}</Button>}>
        <div className="grid gap-3 sm:grid-cols-2">
          <label className={fieldClass}>{t("Kind")}<Select value={binding.kind} onChange={event => patch(at, { kind: event.target.value, source: choices(event.target.value)[0]?.id ?? "" })}>
            <option value="source">{t("Source")}</option><option value="pipeline">{t("Pipeline")}</option></Select></label>
          <label className={fieldClass}>{binding.kind === "pipeline" ? t("Pipeline") : t("Source")}<Select value={binding.source} onChange={event => patch(at, { source: event.target.value })}>
            {!known.some(record => record.id === binding.source) && <option value={binding.source}>{binding.source || t("Choose…")}</option>}
            {known.map(record => <option key={record.id} value={record.id}>{record.title || record.name} · {record.name}</option>)}</Select></label>
          <label className={fieldClass}>{t("Key column")}<Input value={binding.key} placeholder="id" onChange={event => patch(at, { key: event.target.value })} />
            <span className="text-muted">{t("The row column whose value identifies the record; the same key updates the same record.")}</span></label>
          <label className={fieldClass}>{t("Source time column")}<Input value={binding.sourceTime ?? ""} placeholder="changed_at" onChange={event => patch(at, { sourceTime: event.target.value || undefined })} />
            <span className="text-muted">{t("Optional. Needed to compare against edits under “Latest source time wins”.")}</span></label>
        </div>
        {target && target !== parent && <div className="mt-3"><Notice tone="warning" message={(binding.kind === "pipeline" ? t("This pipeline writes to {object}, not to this object.", { object: target }) : t("This source writes to {object}, not to this object.", { object: target }))} /></div>}
        <div className="mt-3 grid gap-2">
          <div className="grid grid-cols-[1fr_1fr_1fr_auto] items-center gap-2 text-xs font-medium text-muted"><span>{t("Column")}</span><span>{t("Attribute")}</span><span>{t("When both have a value")}</span><span className="w-7" /></div>
          {binding.columns.map((column, row) => {
            const attribute = writable.find(field => field.name === column.attribute);
            const update = (values: Partial<Api.DataSourceColumn>) => patch(at, { columns: binding.columns.map((entry, index) => index === row ? { ...entry, ...values } : entry) });
            const strategy = column.strategy || "edits-win";
            return <div key={row} role="group" aria-label={t("Column mapping {number}", { number: row + 1 })} className="grid gap-1">
              <div className="grid grid-cols-[1fr_1fr_1fr_auto] items-center gap-2">
                <Input aria-label={t("Column")} value={column.column} placeholder="sku" onChange={event => update({ column: event.target.value })} />
                <Select aria-label={t("Attribute")} value={column.attribute} onChange={event => update({ attribute: event.target.value })}>
                  <option value="">{t("Choose a field")}</option>
                  {column.attribute && !attribute && <option value={column.attribute}>{column.attribute} · {t("Unavailable field")}</option>}
                  {writable.map(field => <option key={field.name} value={field.name}>{field.title || field.name}</option>)}</Select>
                <Select aria-label={t("When both have a value")} value={strategy} onChange={event => update({ strategy: event.target.value === "edits-win" ? undefined : event.target.value })}>
                  {strategies.map(([value, label]) => <option key={value} value={value}>{t(label)}</option>)}</Select>
                <Button size="sm" variant="ghost" aria-label={t("Remove column")} onClick={() => patch(at, { columns: binding.columns.filter((_, index) => index !== row) })}><Trash2 /></Button>
              </div>
              <p className="pl-1 text-xs text-muted">{t(strategyHelp[strategy] ?? strategyHelp["edits-win"]!)}</p>
            </div>;
          })}
          <div><Button size="sm" onClick={() => patch(at, { columns: [...binding.columns, { column: "", attribute: writable.find(field => !bindings.some(entry => entry.columns.some(column => column.attribute === field.name)))?.name ?? "" }] })}><Plus />{t("Add a column")}</Button></div>
        </div>
      </Panel></InputDraftProvider>;
    })}
  </div>;
}

export const declaringObject = (entities: EntityInfo[], kind: "source" | "pipeline", id: string) =>
  id ? entities.find(entity => entity.datasources?.some(binding => binding.kind === kind && binding.source === id)) : undefined;

export function DeclaredByObject({ object, onOpen }: { object: EntityInfo; onOpen?: () => void }) {
  return <div className="grid gap-2"><Notice tone="info" title={t("Declared by {object}", { object: object.title })} message={t("The object type owns the row key and the column-to-attribute mapping of this definition. Edit them under Data sources on the object type; the legacy key and mapping stay empty here.")} />
    {onOpen && <Button size="sm" variant="ghost" className="w-fit" onClick={onOpen}>{t("Open the object's data sources")}</Button>}</div>;
}
