import type { Api } from "@platform/kernel";

export type LineageExportFormat = "csv" | "json";
export type LineageExportTarget = "subgraph" | "impact";

type Filters = {
  search: string;
  kind: string;
  status: string;
  hideUnconnected: boolean;
  isolated: boolean;
  fieldMode: boolean;
};

type SubgraphInput = {
  nodes: Api.LineageNode[];
  edges: Api.LineageEdge[];
  fields?: Api.LineageFieldNode[];
  fieldEdges?: Api.LineageFieldEdge[];
  filters: Filters;
};

function nodeRecord(node: Api.LineageNode) {
  return {
    id: node.id,
    kind: node.kind,
    rid: node.rid,
    project: node.project,
    name: node.name,
    title: node.title,
    state: node.state,
    status: node.status,
    staleReasons: node.staleReasons,
    declared: node.declared,
    effective: node.effective,
    guard: node.guard,
    markingStatus: node.markingStatus,
    definitionVersion: node.definitionVersion,
    executionVersion: node.executionVersion,
    datasetVersion: node.datasetVersion,
    rows: node.canReadData ? node.rows : undefined,
    at: node.at,
    errorCode: node.errorCode,
    connected: node.connected,
    distance: node.distance,
    boundary: node.boundary,
  };
}

function edgeRecord(edge: Api.LineageEdge) {
  return {
    id: edge.id,
    from: edge.from,
    to: edge.to,
    role: edge.role,
    occurrence: edge.occurrence,
    consumedVersion: edge.consumedVersion,
    currentVersion: edge.currentVersion,
    stale: edge.stale,
    crossesProject: edge.crossesProject,
    unresolved: edge.unresolved,
  };
}

function fieldRecord(field: Api.LineageFieldNode) {
  return {
    id: field.id,
    node: field.node,
    kind: field.kind,
    project: field.project,
    name: field.name,
    title: field.title,
    certainty: field.certainty,
  };
}

function fieldEdgeRecord(edge: Api.LineageFieldEdge) {
  return {
    id: edge.id,
    from: edge.from,
    to: edge.to,
    operation: edge.operation,
    step: edge.step,
    occurrence: edge.occurrence,
    certainty: edge.certainty,
  };
}

export function lineageSubgraphDocument(graph: Api.Lineage, input: SubgraphInput) {
  const nodes = input.nodes.map(nodeRecord);
  const nodeIDs = new Set(nodes.map(node => node.id));
  const edges = input.edges.filter(edge => nodeIDs.has(edge.from) && nodeIDs.has(edge.to)).map(edgeRecord);
  const fields = (input.fields ?? []).map(fieldRecord);
  const fieldIDs = new Set(fields.map(field => field.id));
  const fieldEdges = (input.fieldEdges ?? []).filter(edge => fieldIDs.has(edge.from) && fieldIDs.has(edge.to)).map(fieldEdgeRecord);
  return {
    format: "platform.lineage-subgraph",
    version: 1,
    exportedAt: new Date().toISOString(),
    source: {
      scope: graph.scope,
      basis: graph.basis,
      generatedAt: graph.generatedAt,
      coverage: graph.coverage,
      truncated: graph.truncated,
    },
    filters: input.filters,
    counts: { nodes: nodes.length, edges: edges.length, fields: fields.length, fieldEdges: fieldEdges.length },
    nodes,
    edges,
    fields,
    fieldEdges,
  };
}

export function lineageImpactDocument(graph: Api.Lineage, impact: Api.LineageImpact) {
  const nodes = new Map(graph.nodes.map(node => [node.id, node]));
  const items = impact.items.map(item => {
    const node = nodes.get(item.id);
    return {
      id: item.id,
      kind: item.kind,
      title: item.title,
      project: item.project,
      status: item.status,
      distance: item.distance,
      rid: node?.rid,
      state: node?.state,
      staleReasons: node?.staleReasons,
      declared: node?.declared,
      effective: node?.effective,
      guard: node?.guard,
      markingStatus: node?.markingStatus,
      definitionVersion: node?.definitionVersion,
      executionVersion: node?.executionVersion,
      datasetVersion: node?.datasetVersion,
      rows: node?.canReadData ? node.rows : undefined,
      at: node?.at,
      errorCode: node?.errorCode,
      boundary: node?.boundary,
    };
  });
  return {
    format: "platform.lineage-impact",
    version: 1,
    exportedAt: new Date().toISOString(),
    source: {
      scope: graph.scope,
      basis: graph.basis,
      generatedAt: graph.generatedAt,
      coverage: graph.coverage,
      truncated: graph.truncated,
    },
    impact: {
      upstream: impact.upstream,
      downstream: impact.downstream,
      byKind: impact.byKind,
      byProject: impact.byProject,
      stale: impact.stale,
      failed: impact.failed,
      fields: impact.fields,
      atLeast: impact.atLeast,
      items,
    },
  };
}

function csvCell(value: unknown): string {
  if (value === undefined || value === null) return "";
  const text = Array.isArray(value) ? value.join(" | ") : typeof value === "object" ? JSON.stringify(value) : String(value);
  return /[",\r\n]/.test(text) ? `"${text.replaceAll('"', '""')}"` : text;
}

function csv(rows: unknown[][]): string {
  return `\uFEFF${rows.map(row => row.map(csvCell).join(",")).join("\r\n")}\r\n`;
}

export function lineageSubgraphCSV(exported: ReturnType<typeof lineageSubgraphDocument>): string {
  const titles = new Map(exported.nodes.map(node => [node.id, node.title || node.name]));
  const rows: unknown[][] = [[
    "recordType", "id", "resourceId", "kind", "name", "title", "project", "state", "status",
    "from", "to", "fromTitle", "toTitle", "role", "operation", "certainty", "declared", "effective",
    "guard", "markingStatus", "definitionVersion", "executionVersion", "datasetVersion",
    "consumedVersion", "currentVersion", "step", "occurrence", "distance", "stale", "unresolved",
    "crossesProject", "staleReasons", "errorCode", "boundaryUpstream", "boundaryDownstream", "boundaryReason",
  ]];
  for (const node of exported.nodes) {
    rows.push([
      "node", node.id, node.id, node.kind, node.name, node.title, node.project, node.state, node.status,
      "", "", "", "", "", "", "", node.declared, node.effective, node.guard, node.markingStatus,
      node.definitionVersion, node.executionVersion, node.datasetVersion, "", "", "", "", node.distance,
      undefined, undefined, undefined, node.staleReasons, node.errorCode,
      node.boundary?.upstream, node.boundary?.downstream, node.boundary?.reason,
    ]);
  }
  for (const edge of exported.edges) {
    rows.push([
      "edge", edge.id, "", "", "", "", "", "", "", edge.from, edge.to, titles.get(edge.from), titles.get(edge.to),
      edge.role, "", "", "", "", "", "", "", "", "", edge.consumedVersion, edge.currentVersion, "", edge.occurrence,
      "", edge.stale, edge.unresolved, edge.crossesProject, "", "", "", "", "",
    ]);
  }
  for (const field of exported.fields) {
    rows.push([
      "field", field.id, field.node, field.kind, field.name, field.title, field.project, "", "",
      "", "", "", "", "", "", field.certainty, "", "", "", "", "", "", "", "", "", "", "", "", "", "", "", "", "", "", "", "",
    ]);
  }
  for (const edge of exported.fieldEdges) {
    rows.push([
      "field-edge", edge.id, "", "", "", "", "", "", "", edge.from, edge.to,
      "", "", "", edge.operation, edge.certainty, "", "", "", "", "", "", "", "", "", edge.step, edge.occurrence,
      "", "", "", "", "", "", "", "", "",
    ]);
  }
  return csv(rows);
}

export function lineageImpactCSV(exported: ReturnType<typeof lineageImpactDocument>): string {
  const rows: unknown[][] = [[
    "id", "kind", "title", "project", "status", "distance", "rid", "state", "staleReasons",
    "declared", "effective", "guard", "markingStatus", "definitionVersion", "executionVersion",
    "datasetVersion", "rows", "errorCode", "boundaryUpstream", "boundaryDownstream", "boundaryReason",
  ]];
  for (const item of exported.impact.items) {
    rows.push([
      item.id, item.kind, item.title, item.project, item.status, item.distance, item.rid, item.state, item.staleReasons,
      item.declared, item.effective, item.guard, item.markingStatus, item.definitionVersion, item.executionVersion,
      item.datasetVersion, item.rows, item.errorCode,
      item.boundary?.upstream, item.boundary?.downstream, item.boundary?.reason,
    ]);
  }
  return csv(rows);
}

export function lineageExportFileName(target: LineageExportTarget, format: LineageExportFormat, exportedAt: string): string {
  const stamp = exportedAt.replace(/\D/g, "").slice(0, 14);
  return `lineage-${target}-${stamp}.${format}`;
}
