// The CRM's UI (ADR-0018): accounts and opportunities, composed from the
// platform's lists and record pages. An account's page lists its opportunities
// (a reference); an opportunity's page lists the stays linked to it from
// whichever app provides the lodging protocol, and what the provider told about
// them (ADR-0011) — the platform's, not the CRM's (#129).
import "./i18n";
import { Records, defineApp } from "@platform/app";
import { t } from "@platform/ui";
import { Building2, Handshake } from "lucide-react";

const opportunities = { entity: "crm.opportunity" };
const count = { aggregate: "count", type: "quantitative" } as const;

export default defineApp({
  id: "crm",
  title: "CRM",
  icon: <Handshake />,
  home: { view: "accounts" },
  // The pipeline within what the member may see: a sales rep's own opportunities, a manager's all (ADR-0019).
  dashboards: [{ id: "pipeline", title: t("Pipeline"), description: t("Opportunities you may see, by stage, owner and month."), charts: [
    { title: t("Open opportunities"), data: { ...opportunities, domain: [["stage", "=", "open"]] }, mark: "kpi", encoding: { y: count } },
    { title: t("Group rooms confirmed"), data: { ...opportunities, domain: [["block", "=", "confirmed"]] }, mark: "kpi", encoding: { y: { field: "rooms", aggregate: "sum", type: "quantitative" } } },
    { title: t("By stage"), data: opportunities, mark: { type: "arc", donut: true }, encoding: { theta: count, color: { field: "stage", type: "nominal" } } },
    { title: t("By owner and stage"), data: opportunities, mark: { type: "bar", stack: true },
      encoding: { x: { field: "owner", type: "nominal" }, y: count, color: { field: "stage", type: "nominal" } } },
    { title: t("Opened per month"), data: opportunities, mark: "line", encoding: { x: { field: "created", timeUnit: "month", type: "temporal" }, y: count } },
  ] }],
  views: [
    { id: "accounts", title: () => t("Accounts"), render: () => <Records type="crm.account" description={t("Customers, companies or people, each with their opportunities and the stays booked for them.")} /> },
    { id: "opportunities", title: () => t("Opportunities"), render: () => <Records type="crm.opportunity" /> },
  ],
  nav: () => [{ label: "CRM", items: [
    { label: t("Accounts"), icon: <Building2 />, route: { view: "accounts" } },
    { label: t("Opportunities"), icon: <Handshake />, route: { view: "opportunities" } },
  ] }],
});
