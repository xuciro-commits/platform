import type { Api } from "@platform/api";
import { useApplicationWorkspace } from "../projects/application-scope";
import { DependencyGraph, OpenLineage, useReadQuery } from "@platform/app";
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

function useObjectFieldLineage(object: string) {
  const directory = useReadQuery<Api.ResourceDirectory>("/v1/resources");
  const resource = directory.data?.resources.find(item => item.type === "build.object" && item.ref.kind === "object" && item.ref.name === object && !item.archived);
  const path = resource ? `/v1/lineage?${new URLSearchParams({ mode: "focus", focus: resource.id, direction: "both", depth: "1", fields: "on" }).toString()}` : "/v1/lineage";
  const graph = useReadQuery<Api.Lineage>(path, undefined, !!resource?.id);
  return { resource, nodes: graph.data?.fieldNodes ?? [], edges: graph.data?.fieldEdges ?? [], loading: graph.isPending, error: graph.isError };
}

export function ObjectLineage({ object, fields }: { object: string; fields: { name: string; title: string }[] }) {
  const evidence = useIntegrationEvidence(), { open } = useApplicationWorkspace();
  const presentation = integrationPresentation(evidence);
  const ref = { app: object.split(".")[0] ?? "", kind: "object", name: object };
  const lineage = useObjectFieldLineage(object);
  const openAsset = (asset: typeof ref) => open(presentation.route(asset));
  const title = (id: string) => {
    const node = lineage.nodes.find(item => item.id === id);
    return node ? `${node.kind} ${node.title || node.name}` : id;
  };
  const feeders = (field: string) => {
    const id = `object:${object}#${field}`;
    return lineage.edges.filter(edge => edge.to === id).map(edge => title(edge.from));
  };
  const usedBy = (field: string) => {
    const id = `object:${object}#${field}`;
    return lineage.edges.filter(edge => edge.from === id).map(edge => title(edge.to));
  };
  return <div className="grid gap-3">
    <Panel title={t("Declared dependencies")} help={t("Published dependencies and reverse impact come from the shared asset description.")}>
      <DependencyGraph asset={ref} labels={presentation.labels} onOpen={openAsset} />
      {lineage.resource?.draft && <OpenLineage type="build.object" record={lineage.resource.draft} />}
    </Panel>
    <Panel title={t("Field by field")} help={t("Current source mappings for the published dependencies. Open the original integration to inspect its configuration and recorded outcome.")}>
      {lineage.error && <p role="alert" className="text-xs text-danger">{t("Field lineage could not be loaded.")}</p>}
      <DataTable storageKey="ontology:field-lineage" data={fields} getRowId={field => field.name} searchable={false} loading={lineage.loading} height={Math.min(360, 40 + fields.length * 28)} columns={[
        { id: "title", header: t("Field"), accessorFn: field => field.title },
        { id: "name", header: t("Name"), accessorFn: field => field.name },
        { id: "origin", header: t("Comes from"), accessorFn: field => feeders(field.name).join(" · ") || t("unverified") },
        { id: "used", header: t("Used here"), accessorFn: field => usedBy(field.name).join(" · ") || "—" },
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
    <OpenLineage type="build.dataset" record={dataset} />
  </Panel>;
}
