import "./i18n";
import { defineApp, Records, useHost, type AppUI, type Host } from "@platform/app";
import { Button, Card, ContentTabs, PageHeader, useWorkspace, navigationLinks, type NavSection, type View, t } from "@platform/ui";
import type { ReactNode } from "react";
import { Markings } from "./markings";
import { Activity, ArrowLeftRight, BarChart3, BookA, BookOpen, Blocks, Bot, BrainCircuit, Building2, Cable, Settings, ShieldCheck, Database, FlaskConical, History, KeyRound, Network, Route, Server, SlidersHorizontal, Store, Upload, Users } from "lucide-react";
import { Members, MemberDetail } from "./people";
import { Permissions } from "./permissions";
import { MyAccount } from "./account";
import { Organisation } from "./organisation";
import { Enterprise } from "./enterprise";
import { Apps, Matrix, Protocols } from "./apps";
import { PackageHelp } from "./package-sources";
import { PackagePreparation } from "./package-preparation";
import { Words } from "./words";
import { Automation, Deliveries, SystemHealth, Integrations, Endpoints, OutboundEffects, AppSettingsView, Audit } from "./operations";
import { AIConfiguration } from "./ai-configuration";
import { AIProviders, AIPlayground, AIUsage } from "./ai";
import { Credentials } from "./credentials";
import { Flows, Agents, AgentRuns, AgentMemories, Evaluations } from "./processes";
import { Glossary, Knowledge } from "./knowledge";
import { HostMigrations, HostOverview, HostPromotions, HostTenant } from "./host";

import {SystemConsole} from "./system";

const admin = (host: Host) => host.holds("platform", "admin");
const auditor = (host: Host) => host.holds("platform", "auditor");

type Area = "permissions" | "ai" | "settings";
function controlPanelAreas(host: Host): Record<Area, { view: string; title: string }[]> {
  const shown = (entries: [string, string, boolean][]) => entries.filter(([, , on]) => on).map(([view, title]) => ({ view, title }));
  return {
    permissions: shown([["permissions", "Roles and permissions", admin(host) || auditor(host)], ["matrix", "Capability matrix", admin(host)], ["protocols", "Protocols", admin(host)]]),
    ai: shown([["ai-playground", "AI playground", !!host.role("ai")], ["ai-usage", "AI usage", !!host.role("ai")], ["ai-configuration", "AI configuration", host.holds("ai", "admin")], ["ai-providers", "Providers and models", host.holds("ai", "admin")]]),
    settings: shown([["words", "Languages and words", admin(host)], ["app-settings", "App settings", admin(host)], ["credentials", "Credentials", admin(host)], ["integrations", "Connectors", admin(host)], ["self-hosting", "System history", !!host.role("system")]]),
  };
}

function TabbedArea({ label, tabs, current, children }: { label: string; tabs: { view: string; title: string }[]; current: string; children: ReactNode }) {
  const { open } = useWorkspace();
  if (tabs.length < 2) return <>{children}</>;
  return <div className="grid gap-3">
    <ContentTabs label={t(label)} items={tabs.map(tab => ({ id: tab.view, title: t(tab.title) }))} value={current} onChange={view => open({ view })} />
    {children}
  </div>;
}

function AreaTabs({ area, current, children }: { area: Area; current: string; children: ReactNode }) {
  const host = useHost();
  return <TabbedArea label={area === "ai" ? "AI" : area === "settings" ? "Settings" : "Roles and permissions"} tabs={controlPanelAreas(host)[area]} current={current}>{children}</TabbedArea>;
}

function monitoringRuns(host: Host) {
  return [
    ...(host.role("flow") ? [{ view: "flows", title: "Workflow runs" }] : []),
    ...(admin(host) ? [{ view: "automation-operations", title: "Work queue" }, { view: "delivery-attempts", title: "Delivery attempts" }, { view: "effects", title: "Outbound effects" }] : []),
  ];
}

function RunsTabs({ current, children }: { current: string; children: ReactNode }) {
  const host = useHost();
  return <TabbedArea label="Runs" tabs={monitoringRuns(host)} current={current}>{children}</TabbedArea>;
}

function controlPanelNavigation(host: Host): NavSection[] {
  const areas = controlPanelAreas(host);
  const item = (label: string, icon: React.ReactNode, view: string) => ({ label, icon, route: { view } });
  const area = (label: string, icon: React.ReactNode, entries: { view: string }[]) => entries.length ? [{ label, icon, route: { view: entries[0]!.view }, covers: entries.map(entry => entry.view) }] : [];
  const sections: NavSection[] = [
    { label: t("Overview and identity"), items: [
      item(t("Overview"), <SlidersHorizontal />, "control-panel"),
      ...(admin(host) ? [item(t("Organisation"), <Building2 />, "organisation")] : []),
      ...(admin(host) || auditor(host) ? [item(t("Members"), <Users />, "members")] : []),
    ] },
    { label: t("Access"), items: [
      ...area(t("Roles and permissions"), <KeyRound />, areas.permissions),
      ...(admin(host) || auditor(host) ? [item(t("Markings"), <ShieldCheck />, "markings")] : []),
    ] },
    { label: t("AIP"), items: area(t("AI"), <Bot />, areas.ai) },
    { label: t("Settings and audit"), items: [
      ...area(t("Settings"), <Settings />, areas.settings),
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
  { id: "self-hosting", title: () => t("System history"), render: () => <AreaTabs area="settings" current="self-hosting"><SystemConsole /></AreaTabs> },
  { id: "credentials", title: () => t("Credentials"), render: () => <AreaTabs area="settings" current="credentials"><Credentials /></AreaTabs> },
  { id: "markings", title: () => t("Markings"), render: () => <Markings /> },
  { id: "integrations", title: () => t("Connectors"), render: () => <AreaTabs area="settings" current="integrations"><Integrations /></AreaTabs> },
  { id: "control-panel", title: () => t("Control Panel"), render: () => <ControlPanelOverview /> },
  { id: "account", title: () => t("My account"), render: () => <MyAccount /> },
  { id: "organisation", title: () => t("Organisation"), render: () => <Organisation /> },
  { id: "members", title: () => t("Members"), render: () => <Members /> },
  { id: "member", title: (p) => p.id ?? t("Member"), render: (p) => <MemberDetail id={p.id ?? ""} /> },
  { id: "permissions", title: () => t("Roles and permissions"), render: () => <AreaTabs area="permissions" current="permissions"><Permissions /></AreaTabs> },
  { id: "matrix", title: () => t("Capability matrix"), render: () => <AreaTabs area="permissions" current="matrix"><Matrix /></AreaTabs> },
  { id: "protocols", title: () => t("Protocols"), render: () => <AreaTabs area="permissions" current="protocols"><Protocols /></AreaTabs> },
  { id: "app-settings", title: () => t("App settings"), render: () => <AreaTabs area="settings" current="app-settings"><AppSettingsView /></AreaTabs> },
  { id: "words", title: () => t("Languages and words"), render: () => <AreaTabs area="settings" current="words"><Words /></AreaTabs> },
  { id: "audit", title: () => t("Audit"), render: () => <Audit /> },
  { id: "ai-playground", title: () => t("AI playground"), render: p => <AreaTabs area="ai" current="ai-playground"><AIPlayground initialModel={p.model} /></AreaTabs> },
  { id: "ai-usage", title: () => t("AI usage"), render: () => <AreaTabs area="ai" current="ai-usage"><AIUsage /></AreaTabs> },
  { id: "ai-configuration", title: () => t("AI configuration"), render: p => <AreaTabs area="ai" current="ai-configuration"><AIConfiguration initialSection={p.section} fromPackage={p.fromPackage} fromVersion={p.fromVersion} /></AreaTabs> },
  { id: "ai-providers", title: () => t("Providers and models"), render: () => <AreaTabs area="ai" current="ai-providers"><AIProviders /></AreaTabs> },
];

export const controlPanel = defineApp({
  id: "platform",
  serves: ["platform", "ai"],
  surface: "tenant",
  category: "govern",
  description: "Members, roles, AI models, settings and the audit trail.",
  for: () => true,
  title: "Control Panel",
  icon: <SlidersHorizontal />,
  home: { view: "control-panel" },
  views: controlPanelViews,
  nav: controlPanelNavigation,
});

export const marketplace = defineApp({
  id: "marketplace", serves: ["platform"], category: "govern", title: "Marketplace", icon: <Store />,
  description: "Packaged products available to this tenant: install, upgrade and prepare their applications.",
  for: (host) => admin(host) || auditor(host) || host.can("build.app.create"),
  home: { view: "apps" },
  views: [
    { id: "apps", title: () => t("Packages"), render: () => <Apps /> },
    { id: "package-preparation", title: () => t("Prepare package applications"), render: p => <PackagePreparation key={`${p.id}@${p.version}`} id={p.id ?? ""} version={p.version ?? ""} /> },
    { id: "package-help", title: () => t("Package guide"), render: p => <PackageHelp topic={p.topic} /> },
  ],
  nav: () => [{ label: t("Marketplace"), items: [
    { label: t("Packages"), icon: <Blocks />, route: { view: "apps" } },
    { label: t("Package guide"), icon: <BookOpen />, route: { view: "package-help" } },
  ] }],
});

function RunsHome() {
  const host = useHost();
  return host.role("flow") ? <Flows /> : <Automation />;
}

export const monitoring = defineApp({
  id: "monitoring", serves: ["platform", "flow"], category: "govern", title: "Monitoring", icon: <Activity />,
  description: "Health, runs, queued and failed work, and every delivery the host attempted.",
  for: (host) => admin(host) || !!host.role("flow"),
  home: { view: "runs-home" },
  views: [
    { id: "runs-home", title: () => t("Runs"), render: () => <RunsHome /> },
    { id: "flows", title: () => t("Workflow runs"), render: () => <RunsTabs current="flows"><Flows /></RunsTabs> },
    { id: "automation-operations", title: () => t("Work queue"), render: () => <RunsTabs current="automation-operations"><Automation /></RunsTabs> },
    { id: "delivery-attempts", title: () => t("Delivery attempts"), render: () => <RunsTabs current="delivery-attempts"><Deliveries /></RunsTabs> },
    { id: "effects", title: () => t("Outbound effects"), render: () => <RunsTabs current="effects"><OutboundEffects /></RunsTabs> },
    { id: "system-health", title: () => t("Health"), render: () => <SystemHealth /> },
  ],
  nav: (host) => {
    const runs = monitoringRuns(host);
    return [{ label: t("Monitoring"), items: [
      ...(runs.length ? [{ label: t("Runs"), icon: <Route />, route: { view: runs[0]!.view }, covers: ["runs-home", ...runs.map(run => run.view)] }] : []),
      ...(admin(host) ? [{ label: t("Health"), icon: <BarChart3 />, route: { view: "system-health" } }] : []),
    ] }];
  },
});

export const dataConnection = defineApp({
  id: "data-connection", serves: ["platform"], category: "build", title: "Data Connection", icon: <Database />,
  description: "Connections, syncs and streams that bring data in, and exports, endpoints and webhooks that carry it out.",
  for: (host) => admin(host) || host.holds("build", "builder", "integrator"),
  home: (host) => host.holds("build", "builder", "integrator") ? { view: "connection" } : { view: "endpoints" },
  views: [
    { id: "endpoints", title: () => t("Webhooks"), render: () => <Endpoints /> },
  ],
  nav: (host) => [
    { label: t("Sources"), items: [] },
    { label: t("Streams"), items: [{ label: t("Observations"), icon: <BarChart3 />, route: { view: "observations" } }] },
    { label: t("Exports"), items: [] },
    { label: t("Webhooks"), items: admin(host) ? [{ label: t("Webhooks"), icon: <Cable />, route: { view: "endpoints" } }] : [] },
  ],
});

export const agents = defineApp({
  id: "agents", serves: ["agent"], category: "build", title: "Agent Studio", icon: <BrainCircuit />,
  description: "AI agents, what they are allowed to do, their runs and evaluations.",
  for: (host) => !!host.role("agent"),
  home: { view: "agents" },
  views: [
    { id: "agents", title: () => t("Agents"), render: () => <Agents /> },
    { id: "agent-runs", title: () => t("Agent runs"), render: () => <AgentRuns /> },
    { id: "agent-memories", title: () => t("Agent memories"), render: () => <AgentMemories /> },
    { id: "evaluations", title: () => t("Evaluations"), render: () => <Evaluations /> },
  ],
  nav: () => [{ label: t("Operate"), items: [
    { label: t("Agent overview"), icon: <BrainCircuit />, route: { view: "agents" } },
    { label: t("Agent runs"), icon: <Route />, route: { view: "agent-runs" } },
    { label: t("Agent memories"), icon: <BookA />, route: { view: "agent-memories" } },
    { label: t("Evaluations"), icon: <FlaskConical />, route: { view: "evaluations" } },
  ] }],
});

export const knowledge = defineApp({
  id: "knowledge", serves: ["knowledge"], surface: "work", category: "business", title: "Knowledge", icon: <BookOpen />,
  description: "Documents and the glossary of your organisation.",
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

export const hostConsole = defineApp({
  id: "host-console", serves: [], category: "govern", title: "Host Console", icon: <Server />,
  description: "Every tenant on this host: health, lifecycle, support sessions, promotions and migrations.",
  home: { view: "host-overview" },
  views: [
 { id: "host-package-help", title: () => t("Package guide"), render: p => <PackageHelp maintenance topic={p.topic ?? "foundations"} /> },
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
  id: "core", serves: ["core"], category: "business", title: "Master data", icon: <BookOpen />,
  description: "Shared master data and accounting records.",
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
  ] }],
});

export const enterprise = defineApp({
  id: "enterprise", serves: ["enterprise"], category: "business", title: "Organisation directory", icon: <Network />,
  description: "Units, posts, people and assignments. Historical enterprise content is read-only.",
  for: (host) => admin(host) || host.holds("enterprise", "admin", "viewer"),
  home: { view: "enterprise" },
  views: [{ id: "enterprise", title: () => t("Organisation directory"), render: params => <Enterprise element={params.element} section={params.section} /> }],
  nav: () => [{label:t("Organisation directory"),items:[{ label: t("Organisation directory"), icon: <Building2 />, route: {view:"enterprise"} }]}],
});

export const contributions: AppUI[] = [marketplace, monitoring, dataConnection, agents, knowledge, hostConsole, masterData, enterprise];
export default controlPanel;
