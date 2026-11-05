import "./i18n";
import { appRegistration, defineApp, useHost, type AppUI, type AppView, type Host } from "@platform/app";
import { Button, Card, PageHeader, useWorkspace, navigationLinks, type NavSection, t } from "@platform/ui";
import { MarkingEditor, Markings } from "./markings";
import { Activity, Archive, ArrowLeftRight, BarChart3, BookA, BookOpen, Blocks, Bookmark, Bot, BrainCircuit, Building2, Cable, LayoutDashboard, Settings, ShieldCheck, FlaskConical, History, KeyRound, Route, Server, SlidersHorizontal, Store, Upload, Users } from "lucide-react";
import { Members, MemberDetail } from "./people";
import { Permissions } from "./permissions";
import { MyAccount } from "./account";
import { Organisation } from "./organisation";
import { Enterprise } from "./enterprise";
import { Apps, Matrix, Protocols } from "./apps";
import { PackageHelp } from "./package-sources";
import { PackagePreparation } from "./package-preparation";
import { Words } from "./words";
import { SystemHealth, Integrations, Endpoints, AppSettingsView, Audit } from "./operations";
import { MonitoringOverviewView, MonitoringRunsView, MonitoringViewsView } from "./monitoring";
import { AIConfiguration } from "./ai-configuration";
import { AIProviders, AIPlayground, AIUsage } from "./ai";
import { Credentials } from "./credentials";
import { Agents, AgentRuns, AgentMemories, Evaluations } from "./processes";
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

function controlPanelNavigation(host: Host): NavSection[] {
  const areas = controlPanelAreas(host);
  const item = (label: string, icon: React.ReactNode, view: string) => ({ label, icon, route: { view } });
  const area = (label: string, icon: React.ReactNode, entries: { view: string }[]) => entries.length ? [{ label, icon, route: { view: entries[0]!.view }, covers: entries.map(entry => entry.view) }] : [];
  const sections: NavSection[] = [
    { label: t("Overview and identity"), items: [
      item(t("Overview"), <SlidersHorizontal />, "control-panel"),
      ...(admin(host) ? [{...item(t("Organisation"), <Building2 />, "organisation"),covers:["organisation","enterprise"]}] : []),
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

function controlPanelViewAllowed(view: string, host: Host): boolean {
  if (view === "account" || view === "control-panel") return true;
  if (view === "members" || view === "permissions" || view === "markings" || view === "audit") return admin(host) || auditor(host);
  if (view === "self-hosting") return !!host.role("system");
  if (view === "ai-playground" || view === "ai-usage") return !!host.role("ai") || host.holds("ai", "admin");
  if (view === "ai-configuration" || view === "ai-providers") return host.holds("ai", "admin");
  return admin(host);
}

const controlPanelViewList: AppView[] = [
  { id: "self-hosting", title: () => t("System history"), render: () => <SystemConsole /> },
  { id: "credentials", title: () => t("Credentials"), render: () => <Credentials /> },
  { id: "markings", title: () => t("Markings"), render: () => <Markings /> },
  { id: "marking", title: () => t("Marking"), render: params => <MarkingEditor key={params.id} id={params.id ?? "new"} /> },
  { id: "integrations", title: () => t("Connectors"), render: () => <Integrations /> },
  { id: "control-panel", title: () => t("Control Panel"), render: () => <ControlPanelOverview /> },
  { id: "account", title: () => t("My account"), render: () => <MyAccount /> },
  { id: "organisation", title: () => t("Organisation"), render: () => <Organisation /> },
 { id:"enterprise",title:()=>t("Organization directory"),render:params=><Enterprise element={params.element} section={params.section}/> },
  { id: "members", title: () => t("Members"), render: () => <Members /> },
  { id: "member", title: (p) => p.id ?? t("Member"), render: (p) => <MemberDetail id={p.id ?? ""} /> },
  { id: "permissions", title: () => t("Roles and permissions"), render: () => <Permissions /> },
  { id: "matrix", title: () => t("Capability matrix"), render: () => <Matrix /> },
  { id: "protocols", title: () => t("Protocols"), render: () => <Protocols /> },
  { id: "app-settings", title: () => t("App settings"), render: () => <AppSettingsView /> },
  { id: "words", title: () => t("Languages and words"), render: () => <Words /> },
  { id: "audit", title: () => t("Audit"), render: () => <Audit /> },
  { id: "ai-playground", title: () => t("AI playground"), render: p => <AIPlayground initialModel={p.model} /> },
  { id: "ai-usage", title: () => t("AI usage"), render: () => <AIUsage /> },
  { id: "ai-configuration", title: () => t("AI configuration"), render: p => <AIConfiguration initialSection={p.section} fromPackage={p.fromPackage} fromVersion={p.fromVersion} /> },
  { id: "ai-providers", title: () => t("Providers and models"), render: () => <AIProviders /> },
];
const controlPanelViews: AppView[] = controlPanelViewList.map(view => ({ ...view, for: (host: Host) => controlPanelViewAllowed(view.id, host) }));

export const controlPanel = defineApp({
  ...appRegistration("platform"),
  description: "Members, roles, AI models, settings and the audit trail.",
  portal: host => controlPanelNavigation(host).some(section => navigationLinks(section.items).some(item => item.route.view !== "control-panel")),
  icon: <SlidersHorizontal />,
  views: controlPanelViews,
  nav: controlPanelNavigation,
});

export const marketplace = defineApp({
  ...appRegistration("marketplace"), icon: <Store />,
  description: "Packaged products available to this tenant: install, upgrade and prepare their applications.",
  views: [
    { id: "apps", title: () => t("Marketplace"), render: p => <Apps tab={p.tab}/> },
    { id: "package-preparation", title: () => t("Prepare package applications"), render: p => <PackagePreparation key={`${p.id}@${p.version}`} id={p.id ?? ""} version={p.version ?? ""} /> },
    { id: "package-help", title: () => t("Package guide"), render: p => <PackageHelp topic={p.topic} /> },
  ],
  nav: () => [{ label: t("Marketplace"), items: [
    { label: t("Catalog"), icon: <Store />, route: { view: "apps",params:{tab:"catalog"} } },
    { label: t("Installed"), icon: <Blocks />, route: { view: "apps",params:{tab:"installed"} } },
    { label: t("Retired"), icon: <Archive />, route: { view: "apps",params:{tab:"retired"} } },
    { label: t("Updates"), icon: <Upload />, route: { view: "apps",params:{tab:"updates"} } },
    { label: t("Package guide"), icon: <BookOpen />, route: { view: "package-help" } },
  ] }],
});

const monitoringBuilder = (host: Host) => host.holds("build", "builder");
const monitoringRuns = (host: Host) => admin(host) || !!host.role("flow") || monitoringBuilder(host);

export function createMonitoring(host: Host, integration?: Pick<AppUI, "for" | "views" | "nav">): AppUI {
  const runs = monitoringRuns(host);
  const integrating = !!integration && (!integration.for || integration.for(host));
  return defineApp({
    ...appRegistration("monitoring"), icon: <Activity />,
    description: "Overview, every run the host knows about, and its own health.",
    views: [
      ...(integrating ? integration.views : []),
      ...(runs ? [
        { id: "monitoring-overview", title: () => t("Overview"), render: () => <MonitoringOverviewView /> },
        { id: "monitoring-runs", title: () => t("Runs"), render: (p: Record<string, string>) => <MonitoringRunsView type={p.type} status={p.status} /> },
        { id: "monitoring-views", title: () => t("Monitoring views"), render: () => <MonitoringViewsView /> },
      ] : []),
      ...(admin(host) ? [
        { id: "system-health", title: () => t("Health"), render: () => <SystemHealth /> },
      ] : []),
    ],
    nav: (host) => {
      return [{ label: t("Monitoring"), items: [
        ...(monitoringRuns(host) ? [
          { label: t("Overview"), icon: <LayoutDashboard />, route: { view: "monitoring-overview" } },
          { label: t("Runs"), icon: <Route />, route: { view: "monitoring-runs" } },
          { label: t("Monitoring views"), icon: <Bookmark />, route: { view: "monitoring-views" } },
        ] : []),
        ...(admin(host) ? [{ label: t("Health"), icon: <BarChart3 />, route: { view: "system-health" } }] : []),
        ...(integrating ? integration.nav(host).flatMap(section => section.items) : []),
      ] }];
    },
  });
}

export const dataConnectionOperations = {
  views: [{ id: "endpoints", title: () => t("Webhooks"), render: () => <Endpoints /> }],
  streamLinks: () => [{ label: t("Observations"), icon: <BarChart3 />, route: { view: "observations" } }],
  webhookLinks: (host: Host) => admin(host) ? [{ label: t("Webhooks"), icon: <Cable />, route: { view: "endpoints" } }] : [],
};

export const agentOperations: Pick<AppUI, "for" | "views" | "nav"> = {
  for: (host) => !!host.role("agent"),
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
};

export const hostConsole = defineApp({
  ...appRegistration("host-console"), icon: <Server />,
  description: "Every tenant on this host: health, lifecycle, support sessions, promotions and migrations.",
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

export const contributions: AppUI[] = [marketplace, hostConsole];
export default controlPanel;
