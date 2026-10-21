// The platform package contributes the applications a tenant is governed and
// operated with (M1): Control Panel (people, access,
// packages, settings, audit), Runs (automation and workflow runs), Data
// Connection (connectors and webhooks), Agents, AI assistance, Knowledge, and
// the Host Console for host administrators. One package, several entries in the
// portal; the host still decides which roles each member holds.
import "./i18n";
import { defineApp, Records, useHost, type AppUI, type Host } from "@platform/app";
import { Button, Card, PageHeader, useWorkspace, navigationLinks, type NavSection, type View, t } from "@platform/ui";
import { ArrowLeftRight, BarChart3, BookA, BookOpen, Blocks, Bot, BrainCircuit, Building2, Cable, CircleUser, Database, FlaskConical, Grid3x3, History, KeyRound, Languages, MessageSquare, Network, PlugZap, Route, Server, SlidersHorizontal, Upload, Users, Workflow } from "lucide-react";
import { Members, MemberDetail } from "./people";
import { Permissions } from "./permissions";
import { MyAccount } from "./account";
import { Organisation } from "./organisation";
import { Enterprise } from "./enterprise";
import { Apps, Matrix, Protocols } from "./apps";
import { Words } from "./words";
import { Automation, Deliveries, SystemHealth, Integrations, Endpoints, OutboundEffects, AppSettingsView, Audit } from "./operations";
import { AIProviders, AIPlayground, AIUsage } from "./ai";
import { Flows, Agents, AgentRuns, AgentMemories, Evaluations } from "./processes";
import { Glossary, Knowledge } from "./knowledge";
import { HostMigrations, HostOverview, HostPromotions, HostTenant } from "./host";

const admin = (host: Host) => host.role("platform") === "admin";
const auditor = (host: Host) => host.role("platform") === "auditor";

// --- Control Panel: who may do what, which packages run, and the audit of it all.
function controlPanelNavigation(host: Host): NavSection[] {
  const item = (label: string, icon: React.ReactNode, view: string) => ({ label, icon, route: { view } });
  const sections: NavSection[] = [
    { label: t("Overview and identity"), items: [
      item(t("Control Panel"), <SlidersHorizontal />, "control-panel"),
      item(t("My account"), <CircleUser />, "account"),
      ...(admin(host) ? [item(t("Organisation"), <Building2 />, "organisation")] : []),
      ...(admin(host) || auditor(host) ? [item(t("Members"), <Users />, "members")] : []),
    ] },
    { label: t("Access and packages"), items: [
      ...(admin(host) || auditor(host) ? [item(t("Roles and permissions"), <KeyRound />, "permissions")] : []),
      ...(admin(host) || auditor(host) || host.can("build.app.create") ? [item(t("Packages"), <Blocks />, "apps")] : []),
      ...(admin(host) ? [item(t("Capability matrix"), <Grid3x3 />, "matrix"), item(t("Protocols"), <Cable />, "protocols")] : []),
    ] },
    { label: t("Settings and audit"), items: [
      ...(admin(host) ? [item(t("App settings"), <SlidersHorizontal />, "app-settings"), item(t("Languages and words"), <Languages />, "words")] : []),
      ...(admin(host) || auditor(host) ? [item(t("Audit trail"), <History />, "audit")] : []),
    ] },
  ];
  return sections.filter((section) => section.items.length > 0);
}

function ControlPanelOverview() {
  const host = useHost();
  const { open } = useWorkspace();
  const sections = controlPanelNavigation(host)
    .map((section) => ({ ...section, items: navigationLinks(section.items).filter((item) => item.route.view !== "control-panel") }))
    .filter((section) => section.items.length > 0);
  return <>
    <PageHeader title={t("Control Panel")} help={t("Members, organisation, installed packages, settings and the audit trail of {tenant}.", { tenant: host.me.tenantId })} />
    <div className="grid max-w-5xl gap-4 md:grid-cols-2">
      {sections.map((section) => <Card key={section.label} className="grid content-start gap-3 p-4">
        <h2 className="text-sm font-semibold">{section.label}</h2>
        <div className="grid gap-1">{section.items.map((item) => <Button key={item.route.view} variant="row" onClick={() => open(item.route)}>{item.icon}{item.label}</Button>)}</div>
      </Card>)}
    </div>
  </>;
}

const controlPanelViews: View[] = [
  { id: "control-panel", title: () => t("Control Panel"), render: () => <ControlPanelOverview /> },
  { id: "account", title: () => t("My account"), render: () => <MyAccount /> },
  { id: "organisation", title: () => t("Organisation"), render: () => <Organisation /> },
  { id: "members", title: () => t("Members"), render: () => <Members /> },
  { id: "member", title: (p) => p.id ?? t("Member"), render: (p) => <MemberDetail id={p.id ?? ""} /> },
  { id: "permissions", title: () => t("Roles and permissions"), render: () => <Permissions /> },
  { id: "apps", title: () => t("Packages"), render: () => <Apps /> },
  { id: "matrix", title: () => t("Capability matrix"), render: () => <Matrix /> },
  { id: "protocols", title: () => t("Protocols"), render: () => <Protocols /> },
  { id: "app-settings", title: () => t("App settings"), render: () => <AppSettingsView /> },
  { id: "words", title: () => t("Languages and words"), render: () => <Words /> },
  { id: "audit", title: () => t("Audit"), render: () => <Audit /> },
];

export const controlPanel = defineApp({
  id: "platform",
  serves: ["platform"],
  surface: "tenant",
  category: "govern",
  description: t("Members, organisation, installed packages, settings and the audit trail."),
  for: () => true, // every member has an account here; the rest of the panel follows their roles
  title: t("Control Panel"),
  icon: <SlidersHorizontal />,
  home: { view: "control-panel" },
  views: controlPanelViews,
  nav: controlPanelNavigation,
});

// --- Runs: what the host is doing on the tenant's behalf.
function RunsHome() {
  const host = useHost();
  return host.role("flow") ? <Flows /> : <Automation />;
}

export const runs = defineApp({
  id: "runs", serves: ["platform", "flow"], category: "operate", title: t("Runs"), icon: <Route />,
  description: t("Workflow runs, queued and failed work, and every delivery the host attempted."),
  for: (host) => admin(host) || !!host.role("flow"),
  home: { view: "runs-home" },
  views: [
    { id: "runs-home", title: () => t("Runs"), render: () => <RunsHome /> },
    { id: "flows", title: () => t("Workflow runs"), render: () => <Flows /> },
    { id: "automation-operations", title: () => t("Work queue"), render: () => <Automation /> },
    { id: "delivery-attempts", title: () => t("Delivery attempts"), render: () => <Deliveries /> },
    { id: "system-health", title: () => t("Health"), render: () => <SystemHealth /> },
  ],
  nav: (host) => [{ label: t("Runs"), items: [
    ...(host.role("flow") ? [{ label: t("Workflow runs"), icon: <Route />, route: { view: "flows" } }] : []),
    ...(admin(host) ? [
      { label: t("Work queue"), icon: <Workflow />, route: { view: "automation-operations" } },
      { label: t("Delivery attempts"), icon: <History />, route: { view: "delivery-attempts" } },
      { label: t("Health"), icon: <BarChart3 />, route: { view: "system-health" } },
    ] : []),
  ] }],
});

// --- Data Connection: how facts enter and leave the tenant.
export const dataConnection = defineApp({
  id: "data-connection", serves: ["platform"], category: "ontology", title: t("Data Connection"), icon: <Database />,
  description: t("Connectors that bring facts in, webhooks that carry events out, and their health."),
  for: (host) => admin(host) || ["builder", "integrator"].includes(host.role("build") ?? ""),
  home: { view: "integrations" },
  views: [
    { id: "integrations", title: () => t("Connectors"), render: () => <Integrations /> },
    { id: "endpoints", title: () => t("Endpoints"), render: () => <Endpoints /> },
    { id: "effects", title: () => t("Outbound effects"), render: () => <OutboundEffects /> },
  ],
  nav: (host) => [
    { label: t("Connections"), items: admin(host) ? [
      { label: t("Connectors"), icon: <PlugZap />, route: { view: "integrations" } },
      { label: t("Endpoints"), icon: <Cable />, route: { view: "endpoints" } },
    ] : [] },
    { label: t("Monitoring"), items: [
      { label: t("Observations"), icon: <BarChart3 />, route: { view: "observations" } },
      { label: t("Lineage"), icon: <Network />, route: { view: "lineage" } },
      ...(admin(host) ? [{ label: t("Outbound effects"), icon: <Upload />, route: { view: "effects" } }] : []),
    ] },
  ].filter(section => section.items.length > 0),
});

// --- Agents: AI agents, their runs and evaluations.
export const agents = defineApp({
  id: "agents", serves: ["agent"], category: "build", title: t("Agents"), icon: <BrainCircuit />,
  description: t("AI agents, what they are allowed to do, their runs and evaluations."),
  for: (host) => !!host.role("agent"),
  home: { view: "agents" },
  views: [
    { id: "agents", title: () => t("Agents"), render: () => <Agents /> },
    { id: "agent-runs", title: () => t("Agent runs"), render: () => <AgentRuns /> },
    { id: "agent-memories", title: () => t("Agent memories"), render: () => <AgentMemories /> },
    { id: "evaluations", title: () => t("Evaluations"), render: () => <Evaluations /> },
  ],
  nav: host => [{ label: t("Agents"), items: [
    { label: t("Agents"), icon: <BrainCircuit />, route: { view: "agents" } },
    { label: t("Agent runs"), icon: <Route />, route: { view: "agent-runs" } },
    { label: t("Agent memories"), icon: <BookA />, route: { view: "agent-memories" } },
    { label: t("Evaluations"), icon: <FlaskConical />, route: { view: "evaluations" } },
    ...(host.can("agent.run.start") ? [{ label: t("Assistant"), icon: <MessageSquare />, route: { view: "assistant" } }] : []),
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
    { id: "ai-providers", title: () => t("Providers and models"), render: () => <AIProviders /> },
  ],
  nav: (host) => [{ label: t("AI assistance"), items: [
    { label: t("Playground"), icon: <MessageSquare />, route: { view: "ai-playground" } },
    { label: t("Usage"), icon: <BarChart3 />, route: { view: "ai-usage" } },
    ...(host.role("ai") === "admin" ? [{ label: t("Providers and models"), icon: <Bot />, route: { view: "ai-providers" } }] : []),
  ] }],
});

// --- Knowledge: content is business data, not a definition or a governance setting.
export const knowledge = defineApp({
  id: "knowledge", serves: ["knowledge"], surface: "work", category: "ontology", title: t("Knowledge"), icon: <BookOpen />,
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

function MasterDataView({ type }: { type?: string }) {
  const host = useHost();
  const chosen = type ?? host.entities.find(entity => entity.app === "core")?.type ?? "core.account";
  return <Records key={chosen} type={chosen} />;
}

const masterData = defineApp({
  id: "core", serves: ["core"], category: "ontology", title: t("Master data"), icon: <BookOpen />,
  description: t("Shared master data and accounting records."),
  for: (host) => !!host.role("core"), home: { view: "master-data" },
  views: [{ id: "master-data", title: () => t("Master data"), render: (p) => <MasterDataView type={p.type} /> }],
  nav: (host) => [{ label: t("Master data"), items: [
    ...[
      { label: t("People and places"), types: ["core.person", "core.partner", "core.site", "core.location"] },
      { label: t("Materials and units"), types: ["core.material", "core.uom", "core.currency"] },
      { label: t("Accounting"), types: ["core.account", "core.period", "core.journal"] },
    ].map(group => ({ label: group.label, icon: <BookOpen />, items: group.types.flatMap(type => {
      const entity = host.entities.find(entity => entity.app === "core" && entity.type === type);
      return entity ? [{ label: entity.plural, icon: <BookOpen />, route: { view: "master-data", params: { type } } }] : [];
    }) })).filter(group => group.items.length > 0),
    { label: t("Object Explorer"), icon: <Database />, route: { view: "explorer" } },
  ] }],
});

export const enterprise = defineApp({
  id: "enterprise", serves: ["enterprise"], category: "ontology", title: t("Enterprise"), icon: <Network />,
  description: t("The organisation's people, places, capabilities and relationships."),
  for: (host) => admin(host) || ["admin", "viewer"].includes(host.role("enterprise") ?? ""),
  home: { view: "enterprise" },
  views: [{ id: "enterprise", title: () => t("Enterprise"), render: params => <Enterprise element={params.element} section={params.section} /> }],
  nav: () => [{ label: t("Enterprise"), items: [
    { label: t("Enterprise"), icon: <Network />, route: { view: "enterprise" } },
    { label: t("Organisation management"), icon: <Building2 />, route: { view: "enterprise", params: { section: "organization" } } },
    { label: t("Business usage"), icon: <Blocks />, route: { view: "enterprise", params: { section: "usage" } } },
    { label: t("Tenant links"), icon: <ArrowLeftRight />, route: { view: "enterprise", params: { section: "tenant-links" } } },
  ] }],
});

export const contributions: AppUI[] = [runs, dataConnection, agents, ai, knowledge, hostConsole, masterData, enterprise];
export default controlPanel;
