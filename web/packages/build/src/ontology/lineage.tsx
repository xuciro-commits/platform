import { useApplicationWorkspace } from "../projects/application-scope";
import { assetKey, DependencyGraph, useDependencies, useReadQuery } from "@platform/app";
import { Button, DataTable, Panel, t, type Route } from "@platform/ui";

type Conn = { id: string; name: string; title: string; kind: string; state: string };
type Src = { id: string; title: string; name: string; connection?: string; profile?: string; url?: string; entity?: string; object?: string; dataset?: string; key?: string; mapping?: { from: string; to: string }[]; state: string };
type Ds = { id: string; title: string; name: string; producer?: string; version?: number; schema?: { name: string }[] };
type Pipe = { id: string; title: string; name: string; input: string; outputDataset?: string; outputObject?: string; key?: string; state: string; steps?: { kind: string; from?: string; to?: string; dataset?: string; columns?: string[]; measures?: { to: string }[] }[] };
type Wb = { id: string; title: string; name: string; connection: string; object: string; on: string; path?: string; state: string; sent?: number; failed?: number; result?: { from: string; to: string }[] };

export function useIntegrationEvidence() {
  const q = <T,>(type: string) => useReadQuery<{ records: T[] }>(`/v1/records/${type}?limit=200`, 15000).data?.records ?? [];
  return { connections: q<Conn>("build.connection"), sources: q<Src>("build.source"), datasets: q<Ds>("build.dataset"), pipelines: q<Pipe>("build.pipeline"), writebacks: q<Wb>("build.writeback") };
}

function integrationPresentation(evidence: ReturnType<typeof useIntegrationEvidence>) {
  const entries = [
    ...evidence.connections.map(record => ({ kind: "connection", view: "connection", record })),
    ...evidence.sources.map(record => ({ kind: "source", view: "data-source", record })),
    ...evidence.datasets.map(record => ({ kind: "dataset", view: "dataset", record })),
    ...evidence.pipelines.map(record => ({ kind: "pipeline", view: "pipeline", record })),
    ...evidence.writebacks.map(record => ({ kind: "writeback", view: "writeback", record })),
  ];
  return {
    labels: Object.fromEntries(entries.map(entry => [`build/${entry.kind}/${entry.record.name}`, entry.record.title])),
    route: (ref: { app: string; kind: string; name: string }): Route => {
      const entry = ref.app === "build" ? entries.find(entry => entry.kind === ref.kind && entry.record.name === ref.name) : undefined;
      return entry ? { view: entry.view, params: { id: entry.record.id } } : { view: "definition", params: { ...ref } };
    },
  };
}

function producedBy(p: Pipe, column: string): string {
  for (const s of [...(p.steps ?? [])].reverse()) {
    if ((s.kind === "rename" || s.kind === "compute") && s.to === column) return s.kind === "rename" ? `← ${s.from}` : `= ${t("formula")}`;
    if (s.kind === "aggregate" && s.measures?.some((m) => m.to === column)) return `= ${t("aggregate")}`;
  }
  return "";
}

export function ObjectLineage({ object, fields }: { object: string; fields: { name: string; title: string }[] }) {
  const evidence = useIntegrationEvidence(), { open } = useApplicationWorkspace();
  const presentation = integrationPresentation(evidence);
  const ref = { app: object.split(".")[0] ?? "", kind: "object", name: object };
  const dependencies = useDependencies(ref);
  const used = new Set((dependencies.data?.usedBy ?? []).map(assetKey));
  const direct = evidence.sources.filter(source => used.has(`build/source/${source.name}`));
  const pipes = evidence.pipelines.filter(pipeline => used.has(`build/pipeline/${pipeline.name}`));
  const openAsset = (asset: typeof ref) => open(presentation.route(asset));
  const feeders = (field: string) => [
    ...direct.filter(source => source.mapping?.some(mapping => mapping.to === field)).map(source => `${t("Source")} ${source.title} (${source.mapping!.find(mapping => mapping.to === field)!.from})`),
    ...pipes.map(pipeline => `${t("Pipeline")} ${pipeline.title} ${producedBy(pipeline, field)}`.trim()),
  ];
  return <div className="grid gap-3">
    <Panel title={t("Declared dependencies")} help={t("Published dependencies and reverse impact come from the shared asset description.")}>
      <DependencyGraph asset={ref} labels={presentation.labels} onOpen={openAsset} />
      <Button size="sm" variant="ghost" onClick={() => open({ view: "lineage", params: { ref: assetKey(ref), node: `object:${object}` } })}>{t("Open in Data Lineage")}</Button>
    </Panel>
    <Panel title={t("Field by field")} help={t("Current source mappings for the published dependencies. Open the original integration to inspect its configuration and recorded outcome.")}>
      <DataTable data={fields} getRowId={field => field.name} searchable={false} height={Math.min(360, 40 + fields.length * 28)} columns={[
        { id: "title", header: t("Field"), accessorFn: field => field.title },
        { id: "name", header: t("Name"), accessorFn: field => field.name },
        { id: "origin", header: t("Comes from"), accessorFn: field => feeders(field.name).join(" · ") || t("entered here") },
      ]} />
    </Panel>
    <Panel title={t("Runtime evidence")} help={t("Recorded integration outcomes remain separate from declared dependencies.")}>
      <Button size="sm" onClick={() => open({ view: "integration-health" })}>{t("Integration health")}</Button>
    </Panel>
  </div>;
}

export function DatasetLineage({ dataset }: { dataset: string }) {
  const presentation = integrationPresentation(useIntegrationEvidence());
  const { open } = useApplicationWorkspace();
  const record = useReadQuery<{ record?: Ds }>(`/v1/records/build.dataset/${encodeURIComponent(dataset)}`).data?.record;
  if (!record) return null;
  const ref = { app: "build", kind: "dataset", name: record.name };
  return <Panel title={t("Declared dependencies")} className="grid gap-2 text-xs" help={t("Published dependencies and reverse impact come from the shared asset description.")}>
    <DependencyGraph asset={ref} labels={presentation.labels} onOpen={asset => open(presentation.route(asset))} height={320} />
    <p>{t("Dataset version")}: {record.version ?? "—"}</p>
    <Button size="sm" variant="ghost" onClick={() => open({ view: "lineage", params: { ref: assetKey(ref), node: `dataset:${dataset}` } })}>{t("Open in Data Lineage")}</Button>
  </Panel>;
}
