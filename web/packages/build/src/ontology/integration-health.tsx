import { integrates } from "./marking";
import { useApplicationWorkspace } from "../projects/application-scope";
import { useHost, useReadQuery } from "@platform/app";
import type { Api } from "@platform/kernel";
import { Button, DataTable, PageHeader, Panel, Tag, t, type ColumnDef } from "@platform/ui";
import { useLineage } from "./lineage";

// Integration health (ADR-0072 §4 / ADR-0069 Ⅰ-F): one page over every
// connection, source, pipeline and writeback with its last outcome, read from
// the definitions and the outbox - nothing is stored for it.
type Row = { id: string; title: string; view: string; state: string; when?: string; summary: string; tone: "success" | "warning" | "danger" | undefined };

export function IntegrationHealth() {
  const { role } = useHost(), { open } = useApplicationWorkspace(), l = useLineage();
  const sources = useReadQuery<{ records: (Api.Source & { dataset?: string })[] }>("/v1/records/build.source?limit=200", 15000).data?.records ?? [];
  const pipelines = useReadQuery<{ records: { id: string; title: string; name: string; state: string; last?: { at: string; error?: string; rows: number; written: number; quarantined: number; failed: number } }[] }>("/v1/records/build.pipeline?limit=200", 15000).data?.records ?? [];
  const connections = useReadQuery<{ records: { id: string; title: string; kind: string; state: string; last?: { at: string; ok: boolean; error?: string; detail?: string } }[] }>("/v1/records/build.connection?limit=200", 15000).data?.records ?? [];
  const effects = useReadQuery<Api.IntegrationEffect[]>("/v1/integration-effects", 10000).data ?? [];
  if (!integrates(role("build"))) return <PageHeader title={t("Integration health")} description={t("Only a builder can see integration health.")} />;
  const when = (at?: string) => at ? new Date(at).toLocaleString() : "";
  const rows: Row[] = [
    ...effects.filter((x) => x.endpoint === "core:books" && x.event.startsWith("journal/")).map((x): Row => ({ id: x.id, title: `${t("Journal entry")} · ${x.event.slice(8)}`, view: role("core") ? "master-data" : "", state: x.state, when: when(x.last), summary: x.error ? t(x.error) : t(x.state), tone: x.state === "delivered" ? "success" : x.state === "pending" || x.state === "retrying" ? "warning" : "danger" })),
    ...connections.map((c): Row => ({ id: c.id, title: `${t("Connection")} · ${c.title} · ${c.kind}`, view: "connection", state: c.state, when: when(c.last?.at),
      summary: !c.last ? t("Not checked yet.") : c.last.ok ? `${t("Reachable")} ${c.last.detail ?? ""}` : c.last.error ?? t("Failed"), tone: !c.last ? undefined : c.last.ok ? "success" : "danger" })),
    ...sources.map((s): Row => ({ id: s.id, title: `${t("Source")} · ${s.title} → ${s.dataset ? `${t("Dataset")} ${l.datasets.find((d) => d.id === s.dataset)?.title ?? s.dataset}` : s.object}`, view: "data-source", state: s.state, when: when(s.last?.at),
      summary: !s.last ? t("Not pulled yet.") : s.last.error ? s.last.error : t("{rows} rows · {applied} applied · {failed} failed", { rows: s.last.rows, applied: s.last.applied, failed: s.last.failed }), tone: !s.last ? undefined : s.last.error ? "danger" : s.last.failed ? "warning" : "success" })),
    ...pipelines.map((p): Row => ({ id: p.id, title: `${t("Pipeline")} · ${p.title}`, view: "pipeline", state: p.state, when: when(p.last?.at),
      summary: !p.last ? t("Not run yet.") : p.last.error ? p.last.error : t("{rows} rows · {written} written · {quarantined} quarantined · {failed} refused", { rows: p.last.rows, written: p.last.written, quarantined: p.last.quarantined, failed: p.last.failed }), tone: !p.last ? undefined : p.last.error ? "danger" : p.last.quarantined || p.last.failed ? "warning" : "success" })),
    ...l.writebacks.map((w): Row => { const queued = effects.filter((x) => x.event === `writeback/${w.name}` && (x.state === "pending" || x.state === "retrying")); const failing = queued.find((x) => x.state === "retrying");
      return { id: w.id, title: `${t("Writeback")} · ${w.title} → ${l.connections.find((c) => c.id === w.connection)?.title ?? w.connection}`, view: "writeback", state: w.state, when: failing?.last ? when(String(failing.last)) : undefined,
        summary: `${t("{n} delivered", { n: w.sent ?? 0 })} · ${t("{n} failed", { n: w.failed ?? 0 })}${queued.length ? ` · ${t("{n} queued", { n: queued.length })}` : ""}${failing?.error ? ` · ${t(failing.error)}` : ""}`, tone: failing ? "danger" : queued.length ? "warning" : w.sent ? "success" : undefined }; }),
  ];
  const bad = rows.filter((r) => r.tone === "danger").length, warn = rows.filter((r) => r.tone === "warning").length;
  const columns: ColumnDef<Row, unknown>[] = [
    { id: "title", header: t("Integration"), accessorFn: (r) => r.title, cell: ({ row: { original: r } }) => <Button size="sm" variant="ghost" disabled={!r.view} onClick={() => open({ view: r.view, params: r.view === "master-data" ? { type: "core.journal" } : { id: r.id } })}>{r.title}</Button> },
    { id: "state", header: t("State"), accessorFn: (r) => r.state, cell: ({ row: { original: r } }) => <Tag label={r.state} tone={r.state === "published" || r.state === "ready" || r.state === "delivered" ? "success" : r.state === "failed" || r.state === "rejected" ? "danger" : "warning"} /> },
    { id: "summary", header: t("Last outcome"), accessorFn: (r) => r.summary, cell: ({ row: { original: r } }) => r.tone ? <Tag label={r.summary} tone={r.tone} /> : <span className="text-muted">{r.summary}</span> },
    { id: "when", header: t("Last activity"), accessorFn: (r) => r.when ?? "" },
  ];
  return <div className="grid gap-3">
    <PageHeader title={t("Integration health")} description={t("Every connection, source, pipeline, writeback and journal delivery with its last outcome. Red needs a hand; amber is partial; attempts themselves are in Settings → Integrations.")}
      actions={<div className="flex gap-2"><Tag label={t("{n} failing", { n: bad })} tone={bad ? "danger" : undefined} /><Tag label={t("{n} partial", { n: warn })} tone={warn ? "warning" : undefined} /></div>} />
    <Panel>
      {rows.length === 0 ? <p className="text-xs text-muted">{t("No integrations yet. Start with a connection.")}</p> :
        <DataTable data={rows} columns={columns} getRowId={(r) => `${r.view}:${r.id}`} searchable={false} height={Math.min(480, 40 + rows.length * 28)} />}
    </Panel>
  </div>;
}
