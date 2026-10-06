// The platform package contributes the applications a tenant is governed and
// operated with (ADR-0047 M1, ADR-0052 §3.3): Control Panel (people, access,
// packages, settings, audit), Runs (automation and workflow runs), Data
// Connection (connectors and webhooks), Agents, AI assistance, Knowledge, and
// the Host Console for host administrators. One package, several entries in the
// portal; the host still decides which roles each member holds.
import "./i18n";
import { defineApp, useHost, type AppUI, type Host } from "@platform/app";
import { Button, Card, PageHeader, useWorkspace, type NavSection, type View, t } from "@platform/ui";
import { ArrowLeftRight, BarChart3, BookA, BookOpen, Blocks, Bot, BrainCircuit, Cable, Database, FlaskConical, Grid3x3, History, MessageSquare, Network, PlugZap, Route, Server, SlidersHorizontal, Upload, Users, Workflow } from "lucide-react";
import { Members, MemberDetail, Organization } from "./people";
import { Apps, Matrix, Protocols } from "./apps";
import { Automation, Integrations, AppSettingsView, Audit } from "./operations";
import { AIProviders, AIPlayground, AIUsage } from "./ai";
import { Flows, Agents, Evaluations } from "./processes";
import { Glossary, Knowledge } from "./knowledge";
import { HostMigrations, HostOverview, HostPromotions, HostTenant } from "./host";

const admin = (host: Host) => host.role("platform") === "admin";
const auditor = (host: Host) => host.role("platform") === "auditor";

// --- Control Panel: who may do what, which packages run, and the audit of it all.
function controlPanelNavigation(host: Host): NavSection[] {
  const item = (label: string, icon: React.ReactNode, view: string) => ({ label, icon, route: { view } });
  const sections: NavSection[] = [
    { label: t("Overview"), items: [item(t("Control Panel"), <SlidersHorizontal />, "control-panel")] },
    { label: t("Identity and access"), items: [
      ...(admin(host) || auditor(host) ? [item(t("Members"), <Users />, "members")] : []),
      ...(admin(host) || host.role("enterprise") ? [item(t("Organisation"), <Network />, "organization")] : []),
    ] },
    { label: t("Packages and capabilities"), items: [
      ...(admin(host) || auditor(host) ? [item(t("Installed packages"), <Blocks />, "apps")] : []),
      ...(admin(host) ? [item(t("Capability matrix"), <Grid3x3 />, "matrix"), item(t("Protocols"), <Cable />, "protocols"), item(t("App settings"), <SlidersHorizontal />, "app-settings")] : []),
    ] },
    { label: t("Models"), items: [...(host.role("ai") === "admin" ? [item(t("Providers and models"), <Bot />, "ai-providers")] : [])] },
    { label: t("Audit"), items: [...(admin(host) || auditor(host) ? [item(t("Audit trail"), <History />, "audit")] : [])] },
  ];
  return sections.filter((section) => section.items.length > 0);
}

function ControlPanelOverview() {
  const host = useHost();
  const { open } = useWorkspace();
  return <>
    <PageHeader title={t("Control Panel")} description={t("Members, organisation, installed packages, settings and the audit trail of {tenant}.", { tenant: host.me.tenantId })} />
    <div className="grid max-w-5xl gap-4 md:grid-cols-2">
      {controlPanelNavigation(host).slice(1).map((section) => <Card key={section.label} className="grid content-start gap-3 p-4">
        <h2 className="text-sm font-semibold">{section.label}</h2>
        <div className="grid gap-1">{section.items.map((item) => <Button key={item.route.view} variant="row" onClick={() => open(item.route)}>{item.icon}{item.label}</Button>)}</div>
      </Card>)}
    </div>
  </>;
}

const controlPanelViews: View[] = [
  { id: "control-panel", title: () => t("Control Panel"), render: () => <ControlPanelOverview /> },
  { id: "members", title: () => t("Members"), render: () => <Members /> },
  { id: "member", title: (p) => p.id ?? t("Member"), render: (p) => <MemberDetail id={p.id ?? ""} /> },
  { id: "organization", title: () => t("Organisation"), render: () => <Organization /> },
  { id: "apps", title: () => t("Installed packages"), render: () => <Apps /> },
  { id: "matrix", title: () => t("Capability matrix"), render: () => <Matrix /> },
  { id: "protocols", title: () => t("Protocols"), render: () => <Protocols /> },
  { id: "app-settings", title: () => t("App settings"), render: () => <AppSettingsView /> },
  { id: "audit", title: () => t("Audit"), render: () => <Audit /> },
  { id: "ai-providers", title: () => t("AI providers"), render: () => <AIProviders /> },
];

export const controlPanel = defineApp({
  id: "platform",
  serves: ["platform", "enterprise", "ai"],
  surface: "tenant",
  category: "govern",
  description: t("Members, organisation, installed packages, connections, models and the audit trail."),
  for: (host) => ["platform", "enterprise"].some((owner) => !!host.role(owner)) || host.role("ai") === "admin",
  title: t("Control Panel"),
  icon: <SlidersHorizontal />,
  home: { view: "control-panel" },
  views: controlPanelViews,
  nav: controlPanelNavigation,
});

// --- Runs: what the host is doing on the tenant's behalf.
export const runs = defineApp({
  id: "runs", serves: ["platform", "flow"], category: "operate", title: t("Runs"), icon: <Route />,
  description: t("Workflow runs, queued and failed work, and every delivery the host attempted."),
  for: (host) => admin(host) || !!host.role("flow"),
  home: { view: "flows" },
  views: [
    { id: "flows", title: () => t("Workflow runs"), render: () => <Flows /> },
    { id: "automation", title: () => t("Automation"), render: () => <Automation /> },
  ],
  nav: (host) => [{ label: t("Runs"), items: [
    ...(host.role("flow") ? [{ label: t("Workflow runs"), icon: <Route />, route: { view: "flows" } }] : []),
    ...(admin(host) ? [{ label: t("Automation and deliveries"), icon: <Workflow />, route: { view: "automation" } }] : []),
  ] }],
});

// --- Data Connection: how facts enter and leave the tenant.
export const dataConnection = defineApp({
  id: "data-connection", serves: ["platform"], category: "ontology", title: t("Data Connection"), icon: <Database />,
  description: t("Connectors that bring facts in, webhooks that carry events out, and their health."),
  for: admin,
  home: { view: "integrations" },
  views: [{ id: "integrations", title: () => t("Integrations"), render: () => <Integrations /> }],
  nav: () => [{ label: t("Data Connection"), items: [
    { label: t("Connectors and webhooks"), icon: <PlugZap />, route: { view: "integrations" } },
    { label: t("Lineage"), icon: <Network />, route: { view: "lineage" } },
  ] }],
});

// --- Agents: AI agents, their runs and evaluations.
export const agents = defineApp({
  id: "agents", serves: ["agent"], category: "build", title: t("Agents"), icon: <BrainCircuit />,
  description: t("AI agents, what they are allowed to do, their runs and evaluations."),
  for: (host) => !!host.role("agent"),
  home: { view: "agents" },
  views: [
    { id: "agents", title: () => t("Agents"), render: () => <Agents /> },
    { id: "evaluations", title: () => t("Evaluations"), render: () => <Evaluations /> },
  ],
  nav: () => [{ label: t("Agents"), items: [
    { label: t("Agents"), icon: <BrainCircuit />, route: { view: "agents" } },
    { label: t("Evaluations"), icon: <FlaskConical />, route: { view: "evaluations" } },
  ] }],
});

// --- AI assistance: try models and functions; see usage.
export const ai = defineApp({
  id: "ai", serves: ["ai"], surface: "work", category: "build", title: t("AI assistance"), icon: <MessageSquare />,
  description: t("Try models and functions, and see usage."),
  for: (host) => !!host.role("ai"),
  home: { view: "ai-playground" },
  views: [
    { id: "ai-playground", title: () => t("AI playground"), render: () => <AIPlayground /> },
    { id: "ai-usage", title: () => t("AI usage"), render: () => <AIUsage /> },
  ],
  nav: () => [{ label: t("AI assistance"), items: [
    { label: t("Playground"), icon: <MessageSquare />, route: { view: "ai-playground" } },
    { label: t("Usage"), icon: <BarChart3 />, route: { view: "ai-usage" } },
  ] }],
});

// --- Knowledge: content is business data, not a definition or a governance setting.
export const knowledge = defineApp({
  id: "knowledge", serves: ["knowledge"], surface: "work", category: "business", title: t("Knowledge"), icon: <BookOpen />,
  description: t("Documents and the glossary of your organisation."),
  for: (host) => !!host.role("knowledge"),
  home: { view: "knowledge" },
  views: [
    { id: "knowledge", title: () => t("Knowledge"), render: () => <Knowledge /> },
    { id: "glossary", title: () => t("Glossary"), render: () => <Glossary /> },
  ],
  nav: () => [{ label: t("Knowledge"), items: [
    { label: t("Documents"), icon: <BookOpen />, route: { view: "knowledge" } },
    { label: t("Glossary"), icon: <BookA />, route: { view: "glossary" } },
  ] }],
});

// --- Host Console: the host administrator's view over every tenant. The
// workspace shows it only when `/v1/host/me` answers for this subject.
export const hostConsole = defineApp({
  id: "host-console", serves: [], category: "govern", title: t("Host Console"), icon: <Server />,
  description: t("Every tenant on this host: health, lifecycle, support sessions, promotions and migrations."),
  home: { view: "host-overview" },
  views: [
    { id: "host-overview", title: () => t("Host Console"), render: () => <HostOverview /> },
    { id: "host-tenant", title: (p) => p.tenant ?? t("Tenant"), render: (p) => <HostTenant key={p.tenant} tenant={p.tenant ?? ""} /> },
    { id: "host-promotions", title: () => t("Promote a release"), render: (p) => <HostPromotions tenant={p.tenant} from={p.from} candidate={p.candidate} /> },
    { id: "host-migrations", title: () => t("Migrate records"), render: (p) => <HostMigrations tenant={p.tenant} /> },
  ],
  nav: () => [{ label: t("Host Console"), items: [
    { label: t("Tenants"), icon: <Server />, route: { view: "host-overview" } },
    { label: t("Promote a release"), icon: <Upload />, route: { view: "host-promotions" } },
    { label: t("Migrate records"), icon: <ArrowLeftRight />, route: { view: "host-migrations" } },
  ] }],
});

export const contributions: AppUI[] = [runs, dataConnection, agents, ai, knowledge, hostConsole];
export default controlPanel;
