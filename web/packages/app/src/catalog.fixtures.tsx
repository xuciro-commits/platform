// Owner-local Catalog data. Every read resolves here; mutations are refused.
import { EdgeClient, type ActionDeclaration, type Api } from "@platform/kernel";
import { Panel, t, type EntityInfo, type EntityRecord, type RecordSource, type RecordView } from "@platform/ui";
import { PreviewWorkspace } from "@platform/ui/catalog/examples";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { useMemo, useState, type ReactNode } from "react";
import { HostContext, type Definition, type Host } from "./index";

const stamp = { by: "catalog-fixture", at: "2026-09-30T09:00:00Z" };
export const sampleObject: EntityInfo = {
  type: "catalog.sample", app: "catalog", title: "Sample record", plural: "Sample records", display: "title", standard: ["catalog.sample.create", "catalog.sample.edit"],
  fields: [{ name: "title", title: "Title", type: "text", required: true, search: true },
    { name: "state", title: "State", type: "choice", choices: ["draft", "ready"], choiceTitles: ["Draft", "Ready"] },
    { name: "quantity", title: "Quantity", type: "integer" }],
};
export const sampleRecords: EntityRecord[] = [
  { id: "SAMPLE-001", title: "Incoming order", state: "draft", quantity: 12, revision: 1, created: stamp, changed: stamp },
  { id: "SAMPLE-002", title: "Replenishment request", state: "ready", quantity: 8, revision: 1, created: stamp, changed: stamp },
];
const view = (record: EntityRecord): RecordView => ({ record, history: [], related: [], linked: [], activity: [], processes: [], approvals: [], tasks: [], files: [], comments: [], following: false });
export const sampleSource: RecordSource = {
  entity: (type) => type === sampleObject.type ? sampleObject : undefined,
  list: async (_type, query) => {
    let records = sampleRecords.filter((record) => !query.search || String(record.title).toLowerCase().includes(query.search.toLowerCase()));
    for (const term of query.domain ?? []) {
      if (Array.isArray(term) && term[1] === "=") records = records.filter((record) => record[String(term[0])] === term[2]);
    }
    return { total: records.length, records: records.slice(query.offset ?? 0, (query.offset ?? 0) + (query.limit ?? 100)) };
  },
  get: async (_type, id) => {
    const record = sampleRecords.find((record) => record.id === id);
    if (!record) throw new Error("Unknown Catalog fixture record.");
    return view(record);
  },
  aggregate: async () => ({ columns: [{ name: "state", title: "State", kind: "group", type: "nominal" }, { name: "count", title: "Count", kind: "measure", type: "quantitative" }], rows: [{ state: "draft", count: 1 }, { state: "ready", count: 1 }] }),
};
export const sampleActions: ActionDeclaration[] = [{ schema: "catalog.sample.review", target: sampleObject.type, capability: "records", title: "Review", description: "Review this sample record.",
  payload: [{ name: "note", type: "string", description: "Review note", required: true }] },
  ...["create", "edit"].map((verb) => ({ schema: `catalog.sample.${verb}`, target: sampleObject.type, capability: "records", title: verb === "create" ? t("Create") : t("Edit"), description: "Local Catalog form / 本地资产表单", new: verb === "create",
    payload: [{ name: "title", type: "string", description: t("Title"), required: verb === "create" },
      { name: "state", type: "string", description: t("State"), choices: ["draft", "ready"] }, { name: "quantity", type: "integer", description: t("Quantity") }] })),
];
export const samplePage: Definition & { page: NonNullable<Definition["page"]> } = {
  ref: { app: "catalog", kind: "page", name: "samples" }, source: "catalog-fixture", version: "fixture-v1", contractVersion: 1, requires: [],
  page: { name: "samples", title: "Sample record workspace", layout: "list-detail", object: { app: "catalog", kind: "object", name: sampleObject.type },
    listFields: ["title", "state", "quantity"], detailFields: ["title", "state", "quantity"], actions: [{ app: "catalog", kind: "action", name: sampleActions[0]!.schema }] },
};
export const sampleDefinitions: Definition[] = [
  { ref: { app: "catalog", kind: "object", name: sampleObject.type }, source: "catalog-fixture", version: "fixture-v1", contractVersion: 1, requires: [], entity: sampleObject },
  { ref: { app: "catalog", kind: "action", name: sampleActions[0]!.schema }, source: "catalog-fixture", version: "fixture-v1", contractVersion: 1, requires: [], action: sampleActions[0] }, samplePage,
];
const sampleRun: Api.AgentRunRecord = {
  id: "RUN-SAMPLE", revision: 1, created: stamp, changed: stamp, agent: "catalog.advisor", title: "Sample review", goal: "Review the sample order.",
  onBehalf: "catalog-fixture", ref: "catalog.sample/SAMPLE-001", state: "done", stepsUsed: 1, tokensUsed: 0, actionsUsed: 0,
  result: "The record is ready for human review. No model was called.", steps: [{ at: stamp.at, tool: "catalog.sample.read", outcome: "Sample quantity: 12", rationale: "Read the local example record." }],
};

class FixtureClient extends EdgeClient {
  constructor(private readonly reads: Record<string, unknown>) { super({ server: "", token: "catalog-fixture", tenant: "catalog-fixture", principal: "catalog-fixture" }, "catalog-fixture"); }
  override async get<T>(path: string): Promise<T> {
    if (Object.hasOwn(this.reads, path)) return this.reads[path] as T;
    let result: unknown;
    if (path === "/v1/runs") result = [sampleRun];
    else if (path === "/v1/agents") result = [{ id: "catalog.advisor", app: "catalog", title: "Sample advisor", tools: [], instructions: "Local fixture only.", budget: {} }];
    else if (path === "/v1/memories" || path.startsWith("/v1/knowledge")) result = [];
    else if (path.startsWith("/v1/search")) result = sampleRecords.map((record) => ({ type: sampleObject.type, id: record.id, title: record.title }));
    else if (path === "/v1/records/agent.run/RUN-SAMPLE") result = { record: sampleRun };
    else if (path.startsWith("/v1/chain/")) result = { nodes: [{ ref: "catalog.sample/SAMPLE-001", title: "Incoming order", kind: "record" }, { ref: "agent.run/RUN-SAMPLE", title: "Sample review", kind: "run", state: "done" }], edges: [{ from: "catalog.sample/SAMPLE-001", to: "agent.run/RUN-SAMPLE" }] };
    else if (path.startsWith("/v1/capabilities/")) result = { title: "Sample calculation", input: { type: "object", properties: { quantity: { type: "integer" } }, required: ["quantity"] } };
    else throw new Error(`No local fixture for ${path}`);
    return result as T;
  }
  override async call<T>(): Promise<{ ok: boolean; status: number; body: T }> { throw new Error("Catalog fixtures cannot call the host."); }
  override async exportCSV(): Promise<Blob> { return new Blob(["title,state,quantity\nIncoming order,draft,12\n"], { type: "text/csv" }); }
  override async importCSV(): Promise<never> { throw new Error("Catalog fixtures cannot import files."); }
  override async upload(): Promise<never> { throw new Error("Catalog fixtures cannot upload files."); }
  override async download(): Promise<never> { throw new Error("Catalog fixtures contain no downloadable files."); }
}

/** Trusted local fixtures for owner demos, never a tenant or runtime host. */
const noReads: Record<string, unknown> = {};
const readerRole = { catalog: "reader" };
export function CatalogFixture({ children, reads = noReads, roles = readerRole, definitions = sampleDefinitions }: { children: ReactNode; reads?: Record<string, unknown>; roles?: Record<string, string>; definitions?: Definition[] }) {
  const [message, setMessage] = useState("");
  const [queryClient] = useState(() => new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } }));
  const host = useMemo<Host>(() => ({
    client: new FixtureClient(reads), me: { tenantId: "catalog-fixture", principalId: "catalog-fixture", profile: { id: "catalog-fixture", tenant: "catalog-fixture", roles },
      apps: [{ id: "catalog", title: "Catalog fixtures", role: "reader" }], tenants: ["catalog-fixture"], language: "en", languages: ["en", "zh-CN"], currency: "CNY",
      account: { member: "catalog-fixture", effective: { language: "en", timezone: "UTC", dateFormat: "ymd", numberFormat: "1,234.56", weekStart: "monday", email: "", digest: "instant" } },
      tenant: { id: "catalog-fixture", name: "Catalog fixture", settings: {}, apps: ["catalog"], members: 1, status: "active" } },
    role: (app) => roles[app], can: (schema) => sampleActions.some((action) => action.schema === schema),
    action: (schema) => sampleActions.find((action) => action.schema === schema), catalog: sampleActions,
    decide: async (_schema, _target, _payload, options) => { const reason = t("Local example only. No action was sent to a host."); setMessage(reason); options?.onRefused?.(reason); return false; },
    outbox: [], resend: async () => {}, entities: [sampleObject], definitions, source: sampleSource, opens: new Map(),
  }), [reads, roles, definitions]);
  return <QueryClientProvider client={queryClient}><HostContext.Provider value={host}><PreviewWorkspace onOpen={() => setMessage(t("Local example only. Open the connected workspace to follow this link."))}>
    <div className="grid min-w-0 gap-3"><Panel role="status" className="text-xs text-muted">{t("Synthetic local data. No backend requests or mutations.")}</Panel>
      {children}{message && <p role="status" className="text-xs text-muted">{message}</p>}</div>
  </PreviewWorkspace></HostContext.Provider></QueryClientProvider>;
}
