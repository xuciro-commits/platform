// The application model's pages (ADR-0016): an entity type described by the
// host (`GET /v1/entities`) becomes a kit entity, a list page with server-side
// search, sort and paging, a record page (fields, related records, history) and
// generated forms. Components take a RecordSource, so the kit knows no client.
import { ChevronLeft, ChevronRight, History as HistoryIcon } from "lucide-react";
import { useEffect, useMemo, useState, type ReactNode } from "react";
import { z } from "zod";
import { DataTable } from "../components/DataTable";
import { PropertyList } from "../components/EntityCard";
import { Tag } from "../components/StatusTag";
import { columnsFor, defineEntity, type Entity } from "../fields/entity";
import { checkbox, date, datetime, longText, multiSelect, number, singleSelect, text, type FieldType } from "../fields/types";
import { Button } from "../primitives/button";
import { Input, Select } from "../primitives/input";
import { Chart } from "../charts/Chart";
import { Pivot } from "../charts/Pivot";
import type { AggregateData, AggregateQuery, ChartSpec, Mark } from "../charts/spec";
import { t } from "../i18n";
import type { Api } from "@platform/kernel";

// What the host describes is generated from its Go types (ADR-0023 D7); the kit
// only refines what it holds in general: any entity's record.
export type FieldInfo = Api.FieldInfo;
export type State = Api.State;
export type Lifecycle = Api.LifecycleInfo;
export type EntityInfo = Api.EntityInfo;
export type Stamp = Api.Stamp;
export type EntityRecord = { id: string; revision: number; created: Stamp; changed: Stamp; archived?: boolean } & Record<string, unknown>;
export type RecordQuery = { domain?: unknown[]; search?: string; sort?: string[]; offset?: number; limit?: number; archived?: boolean };
export type RecordPageData = Omit<Api.RecordPage, "records"> & { records: EntityRecord[] };
export type RecordChange = Api.RecordChange;
/** A file attached to a record (ADR-0028). */
export type AttachedFile = EntityRecord & { name: string; size: number; contentType: string; by: string };

/** A comment on a record (ADR-0028 D6). */
export type RecordComment = EntityRecord & { text: string; by: string; mentions?: string[] };

export type RecordView = Omit<Api.RecordView, "record" | "related" | "processes" | "approvals" | "files" | "comments"> & {
  /** The approval requests to move the record, newest first (F-38). */
  approvals: Api.ApprovalRequest[];
  files: AttachedFile[];
  comments: RecordComment[];
  record: EntityRecord; related: (Omit<Api.Related, "records"> & { records: EntityRecord[] })[];
  /** The flow instances about the record (ADR-0026 D4). */
  processes: (EntityRecord & { title: string; state: string; tokens?: { step: string }[] })[];
};
export type Money = { amount: number; currency: string };

/** Where records come from: the host's reads, wired by the app; with aggregates, lists can group, pivot and chart (ADR-0019). */
export type RecordSource = {
  entity: (type: string) => EntityInfo | undefined;
  list: (type: string, query: RecordQuery) => Promise<RecordPageData>;
  get: (type: string, id: string) => Promise<RecordView>;
  aggregate?: (type: string, query: AggregateQuery) => Promise<AggregateData>;
  /** Moves each time the host's data changed (F-32): lists, pages, charts and pivots read again. */
  revision?: number;
};

// The tenant's currency (ADR-0024): the default of amounts people enter; the workspace sets it from /v1/me.
let tenantCurrency = "EUR";
export const setCurrency = (currency: string) => { if (currency) tenantCurrency = currency; };

const money = (o: { label: string; required?: boolean; readOnly?: boolean }): FieldType<Money> => ({
  type: "money", align: "right", ...o, compare: (a, b) => (a?.amount ?? 0) - (b?.amount ?? 0), operators: [],
  text: (v) => (v ? `${(v.amount / 100).toFixed(2)} ${v.currency}` : ""),
  schema: z.object({ amount: z.number().int(), currency: z.string().length(3) }),
  display: (v) => (v && v.currency ? <span className="tabular-nums">{(v.amount / 100).toLocaleString(undefined, { style: "currency", currency: v.currency })}</span>
    : <span className="text-muted">—</span>),
  editor: ({ id, value, onChange }) => (
    <span className="flex gap-1">
      <Input id={id} type="number" step={0.01} value={value ? value.amount / 100 : ""} className="min-w-28 flex-1"
        onChange={(e) => onChange(e.target.value === "" ? undefined : { amount: Math.round(Number(e.target.value) * 100), currency: value?.currency || tenantCurrency })} />
      <Input aria-label={t("Currency")} value={value?.currency || tenantCurrency} maxLength={3} className="w-16 uppercase"
        onChange={(e) => onChange({ amount: value?.amount ?? 0, currency: e.target.value.toUpperCase() })} />
    </span>
  ),
});

export type Options = Record<string, { value: string; label: string }[]>;

/** A kit entity from the host's description; `options` gives the choices of reference fields, a line's by "<lines>.<column>". */
export function entityFrom(info: EntityInfo, options: Options = {}): Entity<EntityRecord> {
  return defineEntity<EntityRecord>({ name: info.type, fields: fieldsOf(info, info.fields, options), primary: info.display === "id" ? "id" : info.display });
}

function fieldsOf(info: EntityInfo, infos: FieldInfo[], options: Options): Record<string, FieldType<any, EntityRecord>> {
  const fields: Record<string, FieldType<any, EntityRecord>> = {};
  for (const f of infos) {
    const common = { label: f.title, help: f.help, required: f.required, readOnly: f.readOnly };
    fields[f.name] = (() => {
      switch (f.type) {
        case "longtext": return longText(common);
        case "integer": return number(common);
        case "decimal": return number({ ...common, decimals: 2 });
        case "money": return money(common);
        case "date": return date(common);
        case "datetime": return datetime(common);
        case "boolean": return checkbox(common);
        case "choice": return info.lifecycle?.field === f.name ? lifecycleField(common, info.lifecycle)
          : singleSelect({ ...common, options: (f.choices ?? []).map((c, i) => ({ value: c, label: f.choiceTitles?.[i] ?? c })) });
        case "reference": return options[f.name] ? singleSelect({ ...common, options: options[f.name]! }) : { ...text(common), readOnly: true };
        case "references": case "tags": return multiSelect({ ...common, readOnly: f.type === "references" || f.readOnly, options: options[f.name] ?? [] });
        case "lines": return f.fields?.length
          ? lines(common, fieldsOf(info, f.fields, Object.fromEntries(Object.entries(options).flatMap(([k, v]) => k.startsWith(f.name + ".") ? [[k.slice(f.name.length + 1), v]] : []))))
          : { ...text(common), readOnly: true, display: (v: unknown) => <span className="text-muted">{Array.isArray(v) ? `${v.length} lines` : "—"}</span> } as FieldType<any>;
        default: return text(common);
      }
    })();
  }
  return fields;
}

// Lists show a record per row: its lines are for its page.
const listed = (entity: Entity<EntityRecord>) => Object.keys(entity.fields).filter((k) => entity.fields[k]!.type !== "lines");

type Line = Record<string, unknown>;

// Child lines (Odoo's one2many, ADR-0024): a table on record pages, and rows
// to add, edit and remove in forms; the app's rules check them.
const lines = (common: { label: string; help?: string; required?: boolean; readOnly?: boolean }, columns: Record<string, FieldType<any>>): FieldType<Line[]> => {
  const shown = Object.entries(columns);
  return {
    type: "lines", ...common, schema: z.array(z.record(z.string(), z.unknown())), compare: (a, b) => (a?.length ?? 0) - (b?.length ?? 0), operators: [],
    text: (v) => (v ? String(v.length) : ""),
    display: (v) => !v?.length ? <span className="text-muted">—</span> : (
      <table className="w-full text-sm">
        <thead><tr>{shown.map(([k, c]) => <th key={k} className={`px-1 text-xs font-medium text-muted ${c.align === "right" ? "text-right" : "text-left"}`}>{c.label}</th>)}</tr></thead>
        <tbody>{v.map((line, i) => <tr key={i} className="border-t border-border">
          {shown.map(([k, c]) => <td key={k} className={`px-1 py-0.5 ${c.align === "right" ? "text-right" : ""}`}>{c.display(line[k] as never, line)}</td>)}</tr>)}</tbody>
      </table>
    ),
    editor: ({ id, value, onChange }) => {
      const rows = value ?? [];
      const set = (i: number, k: string, v: unknown) => onChange(rows.map((r, j) => (j === i ? { ...r, [k]: v } : r)));
      return (
        <div id={id} className="grid gap-1 overflow-x-auto">
          <table className="w-full text-sm">
            <thead><tr>{shown.map(([k, c]) => <th key={k} className="px-1 text-left text-xs font-medium text-muted">{c.label}{c.required ? " *" : ""}</th>)}<th /></tr></thead>
            <tbody>{rows.map((line, i) => (
              <tr key={i}>
                {shown.map(([k, c]) => <td key={k} className="min-w-28 px-1 py-0.5">{c.editor?.({ value: line[k] as never, onChange: (v) => set(i, k, v) }) ?? c.display(line[k] as never, line)}</td>)}
                <td><Button size="sm" variant="ghost" aria-label={t("Remove line")} onClick={() => onChange(rows.filter((_, j) => j !== i))}>×</Button></td>
              </tr>
            ))}</tbody>
          </table>
          <div><Button size="sm" onClick={() => onChange([...rows, {}])}>{t("Add line")}</Button></div>
        </div>
      );
    },
  };
};

// A status field shows its state with the lifecycle's tones.
const lifecycleField = (common: { label: string }, l: Lifecycle): FieldType<string> => ({
  ...singleSelect({ ...common, readOnly: true, options: l.states.map((s) => ({ value: s.name, label: s.title, tone: s.tone })) }),
});

/** The lifecycle's states, the current one marked, and the transitions the caller may take from it. */
export function StatusBar({ lifecycle, state, can, onTransition }: {
  lifecycle: Lifecycle; state: string; can?: (schema: string) => boolean; onTransition?: (schema: string, title: string) => void;
}) {
  const open = lifecycle.transitions.filter((t) => t.from.includes(state) && (!can || can(t.schema)));
  return (
    <div className="flex flex-wrap items-center gap-2">
      <ol className="flex overflow-hidden rounded-md border border-border text-xs">
        {lifecycle.states.map((s) => (
          <li key={s.name} className={s.name === state ? "bg-primary px-2 py-1 font-medium text-primary-foreground" : "px-2 py-1 text-muted"}>{s.title}</li>
        ))}
      </ol>
      {onTransition && open.map((t) => <Button key={t.schema} size="sm" onClick={() => onTransition(t.schema, t.title)}>{t.title}</Button>)}
    </div>
  );
}

const displayOf = (info: EntityInfo, r: EntityRecord) => String((info.display === "id" ? r.id : r[info.display]) ?? r.id);

/** A list of one entity type: server-side search, sort and paging; a row opens the record. */
/** The fields a list can group by (ADR-0019): values that repeat, and dates by bucket. */
export function groupable(info: EntityInfo): { value: string; label: string }[] {
  const out: { value: string; label: string }[] = [];
  for (const f of info.fields) {
    const repeats = ["choice", "reference", "boolean", "integer"].includes(f.type) || f.type === "text" && !f.search; // searched text is mostly names
    if (repeats) out.push({ value: f.name, label: f.title });
    if (f.type === "date" || f.type === "datetime") for (const u of ["month", "week", "day", "year"]) out.push({ value: `${f.name}:${u}`, label: `${f.title} (${u})` });
  }
  for (const u of ["month", "week", "day"]) out.push({ value: `created:${u}`, label: `${t("Created")} (${t(u)})` });
  return out;
}

/** The measures a list can show: the count, and sums and averages of numbers and money. */
export function measurable(info: EntityInfo): { value: string; label: string }[] {
  return [{ value: "count", label: t("Count") }, ...info.fields.filter((f) => ["integer", "decimal", "money"].includes(f.type))
    .flatMap((f) => [{ value: `sum:${f.name}`, label: `${f.title} (sum)` }, { value: `avg:${f.name}`, label: `${f.title} (average)` }])];
}

type ListView = "list" | "pivot" | "chart";
/** What a list shows: kept by a member as a saved view (ADR-0019 D4). */
export type ListState = {
  view?: ListView; search?: string; sort?: string; archived?: boolean; drilled?: unknown[];
  group?: string; columns?: string; measure?: string; mark?: Mark;
};

export function RecordList({ source, type, onOpen, toolbar, height = "calc(100dvh - 230px)", pageSize = 100, domain: fixed, initial = {}, onSave }: {
  source: RecordSource; type: string; onOpen?: (r: EntityRecord) => void; toolbar?: ReactNode; height?: number | string; pageSize?: number;
  /** Always applied, like an app's own view of the type. */
  domain?: unknown[];
  /** Where the list starts, such as a saved view; `onSave` offers to save where it is. */
  initial?: ListState; onSave?: (state: ListState) => void;
}) {
  const info = source.entity(type);
  const [search, setSearch] = useState(initial.search ?? "");
  const [sort, setSort] = useState(initial.sort ?? "-changed");
  const [offset, setOffset] = useState(0);
  const [archived, setArchived] = useState(initial.archived ?? false);
  const [page, setPage] = useState<RecordPageData>();
  const [error, setError] = useState<string>();
  const [view, setView] = useState<ListView>(initial.view ?? "list");
  const [drilled, setDrilled] = useState<unknown[] | undefined>(initial.drilled);
  const groups = useMemo(() => (info ? groupable(info) : []), [info]);
  const measures = useMemo(() => (info ? measurable(info) : []), [info]);
  const [group, setGroup] = useState(initial.group ?? "");
  const [columns, setColumns] = useState(initial.columns ?? "");
  const [measure, setMeasure] = useState(initial.measure ?? "count");
  const [mark, setMark] = useState<Mark>(initial.mark ?? "bar");
  const rows = group || (info?.lifecycle?.field ?? groups[0]?.value ?? "");
  const domain = useMemo(() => [...(fixed ?? []), ...(drilled ?? [])], [fixed, drilled]);
  const entity = useMemo(() => (info ? entityFrom(info) : undefined), [info]);
  useEffect(() => {
    if (!info || view !== "list") return;
    const field = sort.replace(/^-/, "");
    const known = ["id", "created", "changed"].includes(field) || info.fields.some((f) => f.name === field);
    const handle = setTimeout(() => {
      source.list(type, { domain, search, sort: known ? [sort] : ["id"], offset, limit: pageSize, archived })
        .then((p) => { setPage(p); setError(undefined); }, (e) => setError(String(e)));
    }, 150);
    return () => clearTimeout(handle);
  }, [source, type, info, search, sort, offset, archived, pageSize, domain, view]);
  if (!info || !entity) return <p className="text-sm text-muted">{t("Unknown entity type")} {type}.</p>;
  const columnsOf = [{ id: "id", header: "ID", accessorKey: "id", meta: { width: 130 }, cell: (c: any) => <span className="font-mono text-xs">{c.getValue()}</span> },
    ...columnsFor(entity, listed(entity)).map((c) => ({ ...c, enableSorting: false }))];
  const total = page?.total ?? 0;
  const aggregate = source.aggregate;
  const query = { domain, search, archived };
  const measureEncoding = measure === "count" ? { type: "quantitative" as const, aggregate: "count" as const }
    : { type: "quantitative" as const, aggregate: measure.split(":")[0] as "sum" | "avg", field: measure.split(":")[1] };
  const groupEncoding = { field: rows.split(":")[0], type: rows.includes(":") ? "temporal" as const : "nominal" as const, ...(rows.includes(":") ? { timeUnit: rows.split(":")[1] as "month" } : {}) };
  const spec: ChartSpec = {
    data: { entity: type, domain, search, archived }, mark,
    encoding: mark === "arc" ? { theta: measureEncoding, color: groupEncoding } : { x: groupEncoding, y: measureEncoding },
  };
  return (
    <div className="grid gap-2">
      <div className="flex flex-wrap items-center gap-2 text-sm">
        <Input aria-label={t("Search")} placeholder={t("Search {things}", { things: info.plural.toLowerCase() })} value={search} className="w-56"
          onChange={(e) => { setSearch(e.target.value); setOffset(0); }} />
        {view === "list" ? (
          <Select aria-label={t("Sort")} value={sort} className="w-48" onChange={(e) => { setSort(e.target.value); setOffset(0); }}>
            {[["-changed", t("Recently changed")], ["id", t("ID")], ...info.fields.filter((f) => f.type !== "references" && f.type !== "tags").flatMap((f) =>
              [[f.name, `${f.title} ↑`], [`-${f.name}`, `${f.title} ↓`]])].map(([v, l]) => <option key={v} value={v}>{l}</option>)}
          </Select>
        ) : <>
          <Select aria-label={t("Group by")} value={rows} className="w-44" onChange={(e) => setGroup(e.target.value)}>
            {groups.map((g) => <option key={g.value} value={g.value}>{g.label}</option>)}
          </Select>
          {view === "pivot" && (
            <Select aria-label={t("Columns")} value={columns} className="w-40" onChange={(e) => setColumns(e.target.value)}>
              <option value="">{t("No columns")}</option>
              {groups.filter((g) => g.value !== rows).map((g) => <option key={g.value} value={g.value}>{g.label}</option>)}
            </Select>
          )}
          <Select aria-label={t("Measure")} value={measure} className="w-40" onChange={(e) => setMeasure(e.target.value)}>
            {measures.map((m) => <option key={m.value} value={m.value}>{m.label}</option>)}
          </Select>
          {view === "chart" && (
            <Select aria-label={t("Chart")} value={mark} className="w-28" onChange={(e) => setMark(e.target.value as Mark)}>
              {([["bar", t("Bars")], ["line", t("Line")], ["area", t("Area")], ["arc", t("Pie")]] as const).map(([v, l]) => <option key={v} value={v}>{l}</option>)}
            </Select>
          )}
        </>}
        <label className="flex items-center gap-1 text-xs"><input type="checkbox" checked={archived} onChange={(e) => { setArchived(e.target.checked); setOffset(0); }} />archived</label>
        {drilled && <Button size="sm" variant="ghost" onClick={() => { setDrilled(undefined); setOffset(0); }}>{t("Clear drill-down ×")}</Button>}
        {toolbar}
        {onSave && <Button size="sm" variant="ghost" onClick={() => onSave({ view, search, sort, archived, drilled, group: rows, columns, measure, mark })}>{t("Save view…")}</Button>}
        {aggregate && (
          <span role="group" aria-label={t("View")} className="flex rounded-md border border-border">
            {(["list", "pivot", "chart"] as const).map((v) => (
              <button key={v} type="button" aria-pressed={view === v} onClick={() => setView(v)}
                className={`h-7 px-2 text-xs capitalize ${view === v ? "bg-row-selected font-medium" : "hover:bg-row-hover"}`}>{v}</button>
            ))}
          </span>
        )}
        {view === "list" && (
          <span className="ml-auto flex items-center gap-1 text-xs text-muted">
            {error ?? (total ? `${offset + 1}–${Math.min(offset + pageSize, total)} of ${total}` : "none")}
            <Button size="sm" variant="ghost" aria-label={t("Previous page")} disabled={offset === 0} onClick={() => setOffset(Math.max(0, offset - pageSize))}><ChevronLeft /></Button>
            <Button size="sm" variant="ghost" aria-label={t("Next page")} disabled={offset + pageSize >= total} onClick={() => setOffset(offset + pageSize)}><ChevronRight /></Button>
          </span>
        )}
      </div>
      {view === "list" && (
        <DataTable data={page?.records ?? []} columns={columnsOf as never} getRowId={(r: EntityRecord) => r.id} height={height} searchable={false}
          onRowClick={onOpen} empty={page ? t("No {things}", { things: info.plural.toLowerCase() }) : t("Loading…")} />
      )}
      {view === "pivot" && aggregate && rows && (
        <Pivot source={{ aggregate, revision: source.revision }} type={type} query={query} rows={rows} columns={columns || undefined} measure={measure}
          onDrill={(d) => { setDrilled([...(drilled ?? []), ...d]); setOffset(0); setView("list"); }} />
      )}
      {view === "chart" && aggregate && rows && <Chart spec={spec} source={{ aggregate, revision: source.revision }} height={360} />}
    </div>
  );
}

const shown = (v: unknown) => (v === undefined || v === null || v === "" ? "—" : typeof v === "object" ? JSON.stringify(v) : String(v));

/** One record: its fields, the records that refer to it, and its history from the journal. */
export function RecordPage({ source, type, id, actions, onOpen, reload = 0, can, onTransition, files, comments }: {
  source: RecordSource; type: string; id: string; actions?: (r: EntityRecord) => ReactNode;
  /** Uploading a file to the record and downloading one (ADR-0028); without it the files are listed only. */
  files?: { upload: (file: File) => Promise<void>; download: (f: AttachedFile) => void };
  /** Commenting and following (ADR-0028 D6); without it comments are listed only. */
  comments?: { add: (text: string) => Promise<boolean>; follow: (on: boolean) => Promise<void> };
  onOpen?: (type: string, r: EntityRecord) => void; reload?: number;
  /** The caller's catalog, and how to take a lifecycle transition (a decision on this record). */
  can?: (schema: string) => boolean; onTransition?: (schema: string, r: EntityRecord) => void;
}) {
  const info = source.entity(type);
  const [view, setView] = useState<RecordView>();
  const [error, setError] = useState<string>();
  useEffect(() => { source.get(type, id).then(setView, (e) => setError(String(e))); }, [source, type, id, reload]);
  const entity = useMemo(() => (info ? entityFrom(info) : undefined), [info]);
  if (error) return <p className="text-sm text-[var(--tone-danger)]">{error}</p>;
  if (!info || !entity || !view) return <p className="text-sm text-muted">{t("Loading…")}</p>;
  const r = view.record;
  return (
    <div className="grid max-w-5xl grid-cols-[minmax(0,1fr)] gap-4">
      <header className="flex flex-wrap items-center gap-2">
        <h1 className="text-lg font-semibold">{displayOf(info, r)}</h1>
        <span className="font-mono text-xs text-muted">{info.title} · {r.id} {t("· rev")} {r.revision}</span>
        {r.archived && <Tag label="archived" />}
        <span className="ml-auto flex gap-1">{actions?.(r)}</span>
      </header>
      {info.lifecycle && <StatusBar lifecycle={info.lifecycle} state={String(r[info.lifecycle.field] ?? "")} can={can}
        onTransition={onTransition && ((schema) => onTransition(schema, r))} />}
      <section className="rounded-md border border-border bg-surface p-3">
        <PropertyList items={[...info.fields.map((f) => [f.title, entity.fields[f.name]!.display(r[f.name] as never, r)] as [string, ReactNode]),
          [t("Created"), `${r.created.by ?? ""} · ${r.created.at ? new Date(r.created.at).toLocaleString() : ""}`],
          [t("Changed"), `${r.changed.by ?? ""} · ${r.changed.at ? new Date(r.changed.at).toLocaleString() : ""}`]]} />
      </section>
      {view.approvals.length > 0 && <Approvals source={source} approvals={view.approvals} />}
      {view.processes.length > 0 && <Processes source={source} processes={view.processes} onOpen={onOpen} />}
      {(view.files.length > 0 || files) && <Files attached={view.files} files={files} />}
      {(view.comments.length > 0 || comments) && <Comments list={view.comments} following={view.following} comments={comments} />}
      {view.related.map((rel) => {
        const relInfo = source.entity(rel.type);
        const relEntity = relInfo && entityFrom(relInfo);
        return relEntity && (
          <section key={`${rel.type}.${rel.field}`}>
            <h2 className="mb-1 text-sm font-semibold">{rel.title} <span className="font-normal text-muted">({rel.total}{t(", by")} {rel.field})</span></h2>
            <DataTable data={rel.records} columns={[{ id: "id", header: "ID", accessorKey: "id", meta: { width: 130 } }, ...columnsFor(relEntity, listed(relEntity))] as never}
              getRowId={(x: EntityRecord) => x.id} height={Math.min(40 + rel.records.length * 28, 260)} searchable={false}
              onRowClick={onOpen && ((x: EntityRecord) => onOpen(rel.type, x))} empty={t("None")} />
          </section>
        );
      })}
      <section>
        <h2 className="mb-1 flex items-center gap-1 text-sm font-semibold"><HistoryIcon className="size-3.5" />{t("History")}</h2>
        <ol className="grid gap-2">
          {view.history.map((h, i) => (
            <li key={`${h.change}:${i}`} className="rounded-md border border-border bg-surface p-2 text-xs">
              <div className="flex gap-2"><span className="font-mono">{h.schema}</span><span className="text-muted">{h.by} · {new Date(h.at).toLocaleString()}</span></div>
              {h.fields.length > 0 && (
                <ul className="mt-1 grid gap-0.5">
                  {h.fields.map((f) => <li key={f.field}><span className="text-muted">{info.fields.find((x) => x.name === f.field)?.title ?? f.field}</span> {f.before !== undefined && <><s className="text-muted">{shown(f.before)}</s> → </>}{shown(f.after)}</li>)}
                </ul>
              )}
            </li>
          ))}
        </ol>
      </section>
    </div>
  );
}

const size = (n: number) => (n < 1024 ? `${n} B` : n < 1 << 20 ? `${Math.round(n / 1024)} KB` : `${(n / (1 << 20)).toFixed(1)} MB`);

/** A record's files: download each, add one. */
function Files({ attached, files }: { attached: AttachedFile[]; files?: { upload: (file: File) => Promise<void>; download: (f: AttachedFile) => void } }) {
  const [busy, setBusy] = useState(false);
  return (
    <section>
      <h2 className="mb-1 flex items-center gap-2 text-sm font-semibold">{t("Files")}
        {files && <label className="cursor-pointer text-xs font-normal text-[var(--tone-info)] hover:underline">
          {busy ? t("Uploading…") : t("Add file")}
          <input type="file" className="hidden" disabled={busy} onChange={async (e) => {
            const file = e.target.files?.[0];
            e.target.value = "";
            if (!file) return;
            setBusy(true);
            try { await files.upload(file); } finally { setBusy(false); }
          }} />
        </label>}
      </h2>
      {attached.length === 0 ? <p className="text-xs text-muted">{t("No files")}</p> :
        <ul className="grid gap-1">
          {attached.map((f) => (
            <li key={f.id} className="flex items-center gap-2 rounded-md border border-border bg-surface px-3 py-1.5 text-sm">
              <button type="button" className="font-medium hover:underline" disabled={!files} onClick={() => files?.download(f)}>{f.name}</button>
              <span className="text-xs text-muted">{f.contentType} · {size(f.size)} · {f.by}</span>
            </li>
          ))}
        </ul>}
    </section>
  );
}

/** A record's comments, oldest first, and a box to add one; following tells of its changes. */
function Comments({ list, following, comments }: { list: RecordComment[]; following: boolean; comments?: { add: (text: string) => Promise<boolean>; follow: (on: boolean) => Promise<void> } }) {
  const [text, setText] = useState("");
  return (
    <section>
      <h2 className="mb-1 flex items-center gap-2 text-sm font-semibold">{t("Comments")}
        {comments && <Button size="sm" variant="ghost" onClick={() => void comments.follow(!following)}>{following ? t("Unfollow") : t("Follow")}</Button>}
      </h2>
      <ol className="grid gap-2">
        {list.map((c) => (
          <li key={c.id} className="rounded-md border border-border bg-surface p-2 text-sm">
            <div className="text-xs text-muted">{c.by} · {c.created?.at ? new Date(c.created.at).toLocaleString() : ""}</div>
            <p className="whitespace-pre-wrap">{c.text}</p>
          </li>
        ))}
      </ol>
      {comments && <form className="mt-2 grid gap-2" onSubmit={async (e) => { e.preventDefault(); if (text.trim() && await comments.add(text)) setText(""); }}>
        <textarea aria-label={t("Comment")} className="min-h-16 rounded-md border border-border bg-surface p-2 text-sm" placeholder={t("Write a comment; @member tells them")}
          value={text} onChange={(e) => setText(e.target.value)} />
        <div><Button type="submit" size="sm" variant="primary" disabled={!text.trim()}>{t("Comment")}</Button></div>
      </form>}
    </section>
  );
}

const processTone = (state: string) =>
  state === "done" ? "success" : state === "stuck" || state === "compensated" || state === "canceled" ? "danger" : state === "compensating" ? "warning" : "info";

const approvalTone = (state: string) => state === "approved" ? "success" : state === "pending" ? "warning" : state === "withdrawn" ? "neutral" : "danger";

/** The approvals asked for a record: each request, its state, whom it waits for, and who rejected it and why. */
function Approvals({ source, approvals }: { source: RecordSource; approvals: Api.ApprovalRequest[] }) {
  const state = source.entity("work.approval")?.fields.find((f) => f.name === "state");
  return (
    <section>
      <h2 className="mb-1 text-sm font-semibold">{t("Approvals")}</h2>
      <ul className="grid gap-1">
        {approvals.map((a) => {
          const level = a.levels[a.level];
          const by = a.levels.flatMap((l) => l.approved.map((m) => l.decidedBy?.[m] ? t("{delegate} for {approver}", { delegate: l.decidedBy[m]!, approver: m }) : m));
          return (
            <li key={a.id} className="flex flex-wrap items-center gap-2 rounded-md border border-border bg-surface px-3 py-2 text-sm">
              <span className="font-medium">{a.title}</span>
              <Tag label={state?.choiceTitles?.[state.choices?.indexOf(a.state) ?? -1] ?? a.state} tone={approvalTone(a.state)} />
              {a.state === "pending" && level && <span className="text-xs text-muted">{t("waiting for {level}: {approvers}", { level: level.title, approvers: level.approvers.join(", ") })}</span>}
              {by.length > 0 && <span className="text-xs text-muted">{t("approved by {members}", { members: by.join(", ") })}</span>}
              {a.rejectedBy && <span className="text-xs">{t("rejected by {member}", { member: a.rejectedBy })}{a.outcome ? `: ${a.outcome}` : ""}</span>}
              {a.state === "refused" && a.outcome && <span className="text-xs">{a.outcome}</span>}
            </li>
          );
        })}
      </ul>
    </section>
  );
}

/** The processes about a record: each flow, its state, and the steps it stands at. */
function Processes({ source, processes, onOpen }: { source: RecordSource; processes: RecordView["processes"]; onOpen?: (type: string, r: EntityRecord) => void }) {
  const state = source.entity("flow.instance")?.fields.find((f) => f.name === "state");
  return (
    <section>
      <h2 className="mb-1 text-sm font-semibold">{t("Processes")}</h2>
      <ul className="grid gap-1">
        {processes.map((p) => (
          <li key={p.id} className="flex flex-wrap items-center gap-2 rounded-md border border-border bg-surface px-3 py-2 text-sm">
            <button type="button" className="font-medium hover:underline" onClick={() => onOpen?.("flow.instance", p)}>{p.title}</button>
            <Tag label={state?.choiceTitles?.[state.choices?.indexOf(p.state) ?? -1] ?? p.state} tone={processTone(p.state)} />
            {!!p.tokens?.length && <span className="text-xs text-muted">{t("at {steps}", { steps: p.tokens.map((x) => x.step).join(", ") })}</span>}
          </li>
        ))}
      </ul>
    </section>
  );
}

/** A task offered to a member; `answers` are what a flow's question offers (ADR-0020), none: done. */
export type InboxTask = Api.InboxTask;

/** A member's open tasks (ADR-0017), overdue first; each can open what it is about and offers the actions the app gives it. */
export function Inbox({ tasks, onOpen, actions, empty = t("Nothing for you") }: {
  tasks: InboxTask[]; onOpen?: (task: InboxTask) => void; actions?: (task: InboxTask) => ReactNode; empty?: string;
}) {
  if (tasks.length === 0) return <p className="text-sm text-muted">{empty}</p>;
  const now = Date.now();
  return (
    <ul className="grid max-w-3xl gap-2">
      {tasks.map((task) => {
        const late = !!task.due && Date.parse(task.due) < now;
        return (
          <li key={task.id} className="rounded-md border border-border bg-surface p-3 text-sm">
            <div className="flex flex-wrap items-center gap-2">
              <button type="button" className="text-left font-medium hover:underline" onClick={() => onOpen?.(task)}>{task.title}</button>
              {late && <Tag label={t("overdue")} tone="danger" />}
              {task.due && !late && <span className="text-xs text-muted">{t("due {when}", { when: new Date(task.due).toLocaleString() })}</span>}
              {task.assignee && <span className="text-xs text-muted">{t("taken by")} {task.assignee}</span>}
              <span className="ml-auto flex gap-1">{actions?.(task)}</span>
            </div>
            {task.body && <p className="mt-1 whitespace-pre-wrap text-xs text-muted">{task.body}</p>}
          </li>
        );
      })}
    </ul>
  );
}
