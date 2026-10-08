import { useApplicationWorkspace } from "../projects/application-scope";
import { useReadQuery } from "@platform/app";
import { Button, DataTable, Panel, Tag, t } from "@platform/ui";

// Lineage (ADR-0072 §3): where an object's data comes from and where its
// decisions go, read from the integration definitions themselves - no second
// model. Sources feed datasets or objects; pipelines read datasets and write
// datasets or objects; writebacks send an object's actions to a connection.
type Conn = { id: string; title: string; kind: string; state: string };
type Src = { id: string; title: string; name: string; connection?: string; profile?: string; url?: string; entity?: string; object?: string; dataset?: string; key?: string; mapping?: { from: string; to: string }[]; state: string };
type Ds = { id: string; title: string; name: string; producer?: string; version?: number; schema?: { name: string }[] };
type Pipe = { id: string; title: string; name: string; input: string; outputDataset?: string; outputObject?: string; key?: string; state: string; steps?: { kind: string; from?: string; to?: string; dataset?: string; columns?: string[]; measures?: { to: string }[] }[] };
type Wb = { id: string; title: string; name: string; connection: string; object: string; on: string; path?: string; state: string; sent?: number; failed?: number; result?: { from: string; to: string }[] };

export function useLineage() {
  const q = <T,>(type: string) => useReadQuery<{ records: T[] }>(`/v1/records/${type}?limit=200`, 15000).data?.records ?? [];
  return { connections: q<Conn>("build.connection"), sources: q<Src>("build.source"), datasets: q<Ds>("build.dataset"), pipelines: q<Pipe>("build.pipeline"), writebacks: q<Wb>("build.writeback") };
}

/** The upstream chain of a dataset: the source or pipeline that loads it, back to the connection. */
function upstream(dataset: string, l: ReturnType<typeof useLineage>, seen = new Set<string>()): { label: string; route?: { view: string; params?: Record<string, string> }; state?: string }[] {
  if (seen.has(dataset)) return [];
  seen.add(dataset);
  const ds = l.datasets.find((d) => d.id === dataset);
  if (!ds) return [];
  const out: ReturnType<typeof upstream> = [{ label: `${t("Dataset")} ${ds.title}${ds.version ? ` v${ds.version}` : ""}`, route: { view: "dataset", params: { id: ds.id } } }];
  for (const s of l.sources.filter((s) => s.dataset === dataset)) {
    const c = l.connections.find((c) => c.id === s.connection);
    out.push({ label: `${t("Source")} ${s.title} · ${s.profile || "json"} ${s.entity || s.url || ""}`, route: { view: "data-source", params: { id: s.id } }, state: s.state });
    if (c) out.push({ label: `${t("Connection")} ${c.title} · ${c.kind}`, route: { view: "connection", params: { id: c.id } }, state: c.state });
  }
  for (const p of l.pipelines.filter((p) => p.outputDataset === dataset)) {
    out.push({ label: `${t("Pipeline")} ${p.title}`, route: { view: "pipeline", params: { id: p.id } }, state: p.state });
    out.push(...upstream(p.input, l, seen));
    for (const step of p.steps ?? []) if (step.dataset) out.push(...upstream(step.dataset, l, seen));
  }
  return out;
}

/** Which step of a pipeline produces a column, if any renames or computes it. */
function producedBy(p: Pipe, column: string): string {
  for (const s of [...(p.steps ?? [])].reverse()) {
    if ((s.kind === "rename" || s.kind === "compute") && s.to === column) return s.kind === "rename" ? `← ${s.from}` : `= ${t("formula")}`;
    if (s.kind === "aggregate" && s.measures?.some((m) => m.to === column)) return `= ${t("aggregate")}`;
  }
  return "";
}

export function ObjectLineage({ object, fields }: { object: string; fields: { name: string; title: string }[] }) {
  const l = useLineage(), { open } = useApplicationWorkspace();
  const direct = l.sources.filter((s) => s.object === object);
  const pipes = l.pipelines.filter((p) => p.outputObject === object);
  const wbs = l.writebacks.filter((w) => w.object === object);
  const Chain = ({ items }: { items: ReturnType<typeof upstream> }) => <ol className="grid gap-1 text-xs">{items.map((x, i) => <li key={i} className="flex items-center gap-2">
    <span className="text-muted">{i === 0 ? "" : "↑"}</span>{x.route ? <Button size="sm" variant="ghost" onClick={() => open(x.route!)}>{x.label}</Button> : <span>{x.label}</span>}{x.state && <Tag label={x.state} tone={x.state === "published" || x.state === "ready" ? "success" : "warning"} />}
  </li>)}</ol>;
  const feeders = (field: string) => [
    ...direct.filter((s) => s.mapping?.some((m) => m.to === field)).map((s) => `${t("Source")} ${s.title} (${s.mapping!.find((m) => m.to === field)!.from})`),
    ...pipes.map((p) => `${t("Pipeline")} ${p.title} ${producedBy(p, field)}`.trim()),
  ];
  if (!direct.length && !pipes.length && !wbs.length) return <Panel title={t("Data lineage")} description={t("Read from the integration definitions themselves: what is configured to feed this object's records and where its actions are configured to go. It is not a record-by-record history.")}><p className="text-xs text-muted">{t("Nothing feeds or follows this object yet. A data source or a pipeline writes its records; a writeback sends its actions to an external system.")}</p></Panel>;
  return <div className="grid gap-3">
    <Panel title={t("Comes from")} className="grid gap-3" description={t("Declared, not observed: these are the sources and pipelines configured to write this object's records. A dataset's version and a writeback's delivered count are the run evidence kept beside the declaration.")}>
      {direct.map((s) => { const c = l.connections.find((c) => c.id === s.connection); return <Chain key={s.id} items={[{ label: `${t("Source")} ${s.title} · ${s.profile || "json"} ${s.entity || s.url || ""} · ${t("id")} ${s.key}`, route: { view: "data-source", params: { id: s.id } }, state: s.state },
        ...(c ? [{ label: `${t("Connection")} ${c.title} · ${c.kind}`, route: { view: "connection", params: { id: c.id } }, state: c.state }] : [])]} />; })}
      {pipes.map((p) => <Chain key={p.id} items={[{ label: `${t("Pipeline")} ${p.title} · ${t("id")} ${p.key}`, route: { view: "pipeline", params: { id: p.id } }, state: p.state }, ...upstream(p.input, l), ...(p.steps ?? []).flatMap((s) => s.dataset ? upstream(s.dataset, l) : [])]} />)}
    </Panel>
    <Panel title={t("Field by field")}>
      <DataTable data={fields} getRowId={(f) => f.name} searchable={false} height={Math.min(360, 40 + fields.length * 28)} columns={[
        { id: "title", header: t("Field"), accessorFn: (f) => f.title },
        { id: "name", header: t("Name"), accessorFn: (f) => f.name },
        { id: "origin", header: t("Comes from"), accessorFn: (f) => feeders(f.name).join(" · ") || t("entered here") },
      ]} />
    </Panel>
    {wbs.length > 0 && <Panel title={t("Goes to")} className="grid gap-1">
      {wbs.map((w) => { const c = l.connections.find((c) => c.id === w.connection); return <p key={w.id} className="flex flex-wrap items-center gap-2 text-xs">
        <Button size="sm" variant="ghost" onClick={() => open({ view: "writeback", params: { id: w.id } })}>{t("Writeback")} {w.title}</Button><span>{t("after")} <span className="font-mono">{w.on}</span> → {c ? `${c.title} · ${c.kind}` : w.connection} {w.path ?? ""}</span>
        <Tag label={w.state} tone={w.state === "published" ? "success" : "warning"} />{w.result?.length ? <span className="text-muted">{t("answer writes")} {w.result.map((r) => r.to).join(", ")}</span> : null}{w.sent ? <span className="text-muted">{t("{n} delivered", { n: w.sent })}</span> : null}
      </p>; })}
    </Panel>}
  </div>;
}

export function DatasetLineage({ dataset }: { dataset: string }) {
  const l = useLineage(), { open } = useApplicationWorkspace();
  const up = upstream(dataset, l).slice(1), down = l.pipelines.filter((p) => p.input === dataset || p.steps?.some((s) => s.dataset === dataset));
  return <Panel title={t("Lineage")} className="grid gap-2 text-xs">
    <p className="font-medium">{t("Loaded by")}</p>
    {up.length ? <ol className="grid gap-1">{up.map((x, i) => <li key={i}>{x.route ? <Button size="sm" variant="ghost" onClick={() => open(x.route!)}>{x.label}</Button> : x.label}{x.state && <Tag label={x.state} tone={x.state === "published" || x.state === "ready" ? "success" : "warning"} />}</li>)}</ol> : <p className="text-muted">{t("Nothing yet.")}</p>}
    <p className="font-medium">{t("Read by")}</p>
    {down.length ? <ol className="grid gap-1">{down.map((p) => <li key={p.id}><Button size="sm" variant="ghost" onClick={() => open({ view: "pipeline", params: { id: p.id } })}>{t("Pipeline")} {p.title}</Button> → {p.outputObject ? `${t("Object")} ${p.outputObject}` : `${t("Dataset")} ${l.datasets.find((d) => d.id === p.outputDataset)?.title ?? p.outputDataset}`}</li>)}</ol> : <p className="text-muted">{t("Nothing yet.")}</p>}
  </Panel>;
}
