import type { RecordHome } from "../recordRoutes";

const views = new Map(Object.entries({
  "/home/overview": ["home", "overview", "discover", "catalog-home"],
  "/home/inbox": ["inbox", "notifications", "requests"],
  "/home/proposals": ["changes", "proposal"],
  "/home/projects": ["project"],
  "/data/connections": ["connection", "integrations"],
  "/data/sources": ["data-source"],
  "/data/listeners": ["listener"],
  "/data/writebacks": ["writeback"],
  "/data/observation-sources": ["observation-source", "observations"],
  "/data/datasets": ["dataset"],
  "/data/pipelines": ["pipeline"],
  "/data/schedules": ["schedule", "schedules"],
  "/data/matching-rules": ["match"],
  "/data/media-sets": ["media-set"],
  "/data/lineage": ["lineage"],
  "/ontology/object-types": ["object-type"],
  "/ontology/link-types": ["link-type"],
  "/ontology/action-types": ["action-type"],
  "/ontology/interfaces": ["interfaces"],
  "/ontology/shared-properties": ["property-type"],
  "/ontology/value-types": ["value-type"],
  "/ontology/object-type-groups": ["object-type-group"],
  "/ontology/contracts": ["contracts"],
  "/ontology/implementations": ["implementations"],
  "/ontology/objects": ["explorer", "records", "catalog-records", "object-set", "saved"],
  "/logic/functions": ["functions", "code-functions", "code", "ai-functions", "function"],
  "/logic/automations": ["automation", "logic-flows", "flow"],
  "/apps/modules": ["module", "portal", "catalog-portal"],
  "/apps/object-views": ["object-views", "page", "page-preview"],
  "/ai/delegations": ["delegations", "agents", "agent"],
  "/ai/evaluations": ["evaluations", "evaluation-set"],
  "/delivery/products": ["products"],
  "/delivery/versions": ["versions", "release-review"],
  "/delivery/test-plans": ["test-plan"],
  "/delivery/sample-sets": ["sample-set"],
  "/delivery/marketplace": ["apps", "studio-templates"],
  "/delivery/installations": ["host-promotions"],
  "/operations/runs": ["runs", "monitoring-runs", "application-runs", "agent-runs", "automation-operations"],
  "/operations/logs": ["monitoring-logs"],
  "/operations/traces": ["monitoring-traces"],
  "/operations/metrics": ["monitoring-metrics"],
  "/operations/alerts": ["monitoring-alerts"],
  "/operations/audit": ["audit"],
  "/operations/health": ["system-health", "integration-health", "monitoring-overview"],
  "/admin/organization": ["organisation", "enterprise"],
  "/admin/members": ["member", "members"],
  "/admin/roles": ["permissions", "matrix"],
  "/admin/markings": ["marking", "markings"],
  "/admin/models": ["ai-configuration", "ai-providers"],
  "/admin/vocabulary": ["words"],
  "/admin/credentials": ["credentials"],
  "/admin/settings": ["app-settings", "control-panel"],
  "/host/tenants": ["host-overview", "host-tenant", "host-migrations"],
}).flatMap(([path, aliases]) => aliases.map((alias) => [alias, path] as const)));

const tabs = new Map([
  ["fields", "/details"],
  ["activity", "/activity"],
  ["related", "/links"],
  ["files", "/files"],
]);

type LegacyTarget = { path: string } | { rid: string; tab: string } | "unmatched" | null;

export function legacyTarget(hash: string, recordHref: RecordHome): LegacyTarget {
  if (!hash.startsWith("#/")) return null;
  const query = hash.indexOf("?");
  const path = hash.slice(2, query === -1 ? undefined : query);
  try {
    const [view, key, detail, ...extra] = path.split("/").map(decodeURIComponent);
    if (!view || extra.length) return "unmatched";
    if (view === "objects" && key && detail) return { path: recordHref(key, detail) };
    if (key?.startsWith("ri.")) return { rid: key, tab: tabs.get(detail) ?? "" };
    const target = views.get(view);
    if (!target || key !== undefined) return "unmatched";
    const id = query === -1 ? null : new URLSearchParams(hash.slice(query + 1)).get("id");
    return { path: id ? `${target}/${encodeURIComponent(id)}` : target };
  } catch {
    return "unmatched";
  }
}
