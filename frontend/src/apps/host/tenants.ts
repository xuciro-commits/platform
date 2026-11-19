import type { Api } from "../../host";
import { t } from "../../i18n";
import { listPage } from "../../pages/ListEntry";

export default listPage<Api.HostOverview, Api.HostTenantView>({
  title: "Tenants",
  about: "A tenant is one workspace on this host, with its own members, apps, data and release.",
  read: "/v1/host/overview",
  rows: (answer) => answer.tenants,
  id: (tenant) => tenant.id,
  columns: [
    { key: "id", header: "Tenant", kind: "code", text: (tenant) => tenant.id },
    { key: "health", header: "Health", width: 160, text: (tenant) => (tenant.quarantined ? t("Quarantined") : tenant.fault || t("Healthy")), dot: (tenant) => (tenant.quarantined || tenant.fault ? "error" : "success") },
    { key: "lifecycle", header: "Lifecycle", width: 120, text: (tenant) => t(tenant.lifecycle ?? "active"), value: (tenant) => tenant.lifecycle ?? "active" },
    { key: "members", header: "Members", width: 100, kind: "number", text: (tenant) => String(tenant.members), value: (tenant) => tenant.members },
    { key: "apps", header: "Apps", width: 90, kind: "number", text: (tenant) => String(tenant.apps.length), value: (tenant) => tenant.apps.length },
    { key: "failed", header: "Failed work", width: 120, kind: "number", text: (tenant) => String(tenant.failedWork), value: (tenant) => tenant.failedWork },
    { key: "release", header: "Release", width: 140, kind: "code", text: (tenant) => tenant.activeRelease ?? "" },
    { key: "started", header: "Started", width: 140, kind: "time", text: (tenant) => tenant.started ?? "" },
  ],
  groups: [{ key: "lifecycle", label: "Lifecycle", of: (tenant) => t(tenant.lifecycle ?? "active") }],
});
