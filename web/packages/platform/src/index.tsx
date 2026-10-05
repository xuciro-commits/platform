// Tenant operations/governance and business knowledge share their original owners (ADR-0047 M1).
import "./i18n";
import { defineApp, useHost, type Host } from "@platform/app";
import { Button, Card, PageHeader, useWorkspace, type NavSection, type View, t } from "@platform/ui";
import { BarChart3, BookA, BookOpen, Blocks, Bot, BrainCircuit, Cable, FlaskConical, Grid3x3, History, MessageSquare, Network, PlugZap, Route, SlidersHorizontal, Users, Workflow } from "lucide-react";
import { Members, MemberDetail, Organization } from "./people";
import { Apps, Matrix, Protocols } from "./apps";
import { Automation, Integrations, AppSettingsView, Audit } from "./operations";
import { AIProviders, AIPlayground, AIUsage } from "./ai";
import { Flows, Agents, Evaluations } from "./processes";
import { Glossary, Knowledge } from "./knowledge";

function tenantNavigation(host: Host): NavSection[] {
  const nav = (label: string, icon: React.ReactNode, view: string) => ({ label, icon, route: { view, params: { surface: "tenant" } } });
  const admin = host.role("platform") === "admin";
  const builder = host.role("build") === "builder";
  const sections: NavSection[] = [
    { label: t("Tenant console"), items: [nav(t("Overview"), <Blocks />, "tenant-overview")] },
    { label: t("Delivery and operations"), items: [
      ...(builder ? [
        { label: t("Release review"), icon: <Blocks />, route: { view: "release-review", params: { surface: "tenant" } } },
        { label: t("Test a candidate"), icon: <FlaskConical />, route: { view: "candidate-test", params: { surface: "tenant" } } },
      ] : []),
      ...(admin ? [nav(t("Automation"), <Workflow />, "automation")] : []),
      ...(host.role("flow") ? [nav(t("Workflow runs"), <Route />, "flows")] : []),
      ...(host.role("agent") ? [nav(t("Agents"), <BrainCircuit />, "agents"), nav(t("Evaluations"), <FlaskConical />, "evaluations")] : []),
      ...(host.role("ai") ? [nav(t("Usage"), <BarChart3 />, "ai-usage")] : []),
    ] },
    { label: t("Connections and models"), items: [
      ...(admin ? [nav(t("Integrations"), <PlugZap />, "integrations"), nav(t("App settings"), <SlidersHorizontal />, "app-settings")] : []),
      ...(host.role("ai") === "admin" ? [nav(t("Providers and models"), <Bot />, "ai-providers")] : []),
      ...(host.role("ai") ? [nav(t("Playground"), <MessageSquare />, "ai-playground")] : []),
    ] },
    { label: t("Access and governance"), items: [
      ...(admin ? [nav(t("Members"), <Users />, "members")] : []),
      ...(admin || host.role("org") ? [nav(t("Organisation"), <Network />, "organization")] : []),
      ...(admin ? [
        nav(t("Installed packages"), <Blocks />, "apps"), nav(t("Capability matrix"), <Grid3x3 />, "matrix"),
        nav(t("Protocols"), <Cable />, "protocols"), nav(t("Audit"), <History />, "audit"),
      ] : []),
    ] },
  ];
  return sections.filter((section) => section.items.length > 0);
}

function TenantOverview() {
  const host = useHost();
  const { open } = useWorkspace();
  return <>
    <PageHeader title={t("Tenant console")} description={t("Operate delivered applications and manage access in {tenant}.", { tenant: host.me.tenantId })} />
    <div className="grid max-w-5xl gap-4 md:grid-cols-2">
      {tenantNavigation(host).slice(1).map((section) => <Card key={section.label} className="grid content-start gap-3 p-4">
        <h2 className="text-sm font-semibold">{section.label}</h2>
        <div className="grid gap-1">{section.items.map((item) => <Button key={item.route.view} variant="row" onClick={() => open(item.route)}>{item.icon}{item.label}</Button>)}</div>
      </Card>)}
    </div>
  </>;
}

const views: View[] = [
  { id: "tenant-overview", title: () => t("Tenant console"), render: () => <TenantOverview /> },
  { id: "agents", title: () => t("Agents"), render: () => <Agents /> },
  { id: "evaluations", title: () => t("Evaluations"), render: () => <Evaluations /> },
  { id: "flows", title: () => t("Workflow runs"), render: () => <Flows /> },
  { id: "members", title: () => t("Members"), render: () => <Members /> },
  { id: "member", title: (p) => p.id ?? t("Member"), render: (p) => <MemberDetail id={p.id ?? ""} /> },
  { id: "organization", title: () => t("Organisation"), render: () => <Organization /> },
  { id: "apps", title: () => t("Installed packages"), render: () => <Apps /> },
  { id: "matrix", title: () => t("Capability matrix"), render: () => <Matrix /> },
  { id: "protocols", title: () => t("Protocols"), render: () => <Protocols /> },
  { id: "automation", title: () => t("Automation"), render: () => <Automation /> },
  { id: "integrations", title: () => t("Integrations"), render: () => <Integrations /> },
  { id: "app-settings", title: () => t("App settings"), render: () => <AppSettingsView /> },
  { id: "audit", title: () => t("Audit"), render: () => <Audit /> },
  { id: "ai-providers", title: () => t("AI providers"), render: () => <AIProviders /> },
];

// Knowledge content is business data, not a definition or a governance setting.
export const contributions = [defineApp({
  id: "knowledge",
  serves: ["knowledge"],
  surface: "work",
  for: (host) => !!host.role("knowledge"),
  title: t("Knowledge"),
  icon: <BookOpen />,
  home: { view: "knowledge" },
  views: [
    { id: "knowledge", title: () => t("Knowledge"), render: () => <Knowledge /> },
    { id: "glossary", title: () => t("Glossary"), render: () => <Glossary /> },
  ],
  nav: () => [{ label: t("Knowledge"), items: [
    { label: t("Documents"), icon: <BookOpen />, route: { view: "knowledge" } },
    { label: t("Glossary"), icon: <BookA />, route: { view: "glossary" } },
  ] }],
}), defineApp({
  id: "ai",
  serves: ["ai"],
  surface: "work",
  for: (host) => !!host.role("ai"),
  title: t("AI assistance"),
  icon: <MessageSquare />,
  home: { view: "ai-playground" },
  views: [
    { id: "ai-playground", title: () => t("AI playground"), render: () => <AIPlayground /> },
    { id: "ai-usage", title: () => t("AI usage"), render: () => <AIUsage /> },
  ],
  nav: () => [{ label: t("AI assistance"), items: [
    { label: t("Playground"), icon: <MessageSquare />, route: { view: "ai-playground" } },
    { label: t("Usage"), icon: <BarChart3 />, route: { view: "ai-usage" } },
  ] }],
})];

export default defineApp({
  id: "platform",
  serves: ["platform", "org", "ai", "flow", "agent"],
  surface: "tenant",
  for: (host) => ["platform", "org", "flow", "agent"].some((owner) => !!host.role(owner)) || host.role("ai") === "admin",
  title: t("Tenant console"),
  icon: <SlidersHorizontal />,
  home: { view: "tenant-overview" },
  views,
  nav: tenantNavigation,
});
