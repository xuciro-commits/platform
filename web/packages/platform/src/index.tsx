// Settings, the platform app's UI in the workspace (#93, ADR-0010 part 3, ADR-0018):
// members and their role in each app, the organisation, the apps a tenant runs
// with their protocol graph, the capability matrix read from the registry,
// connectors and endpoints, app settings, owned work (ADR-0013), AI providers
// and usage (ADR-0015), flows (ADR-0020), agents and their evaluations
// (ADR-0021) and the audit trail. Every change is a decision.
import "./i18n";
import { defineApp } from "@platform/app";
import { type View, t } from "@platform/ui";
import { BarChart3, BookA, BookOpen, Blocks, Bot, BrainCircuit, Cable, FlaskConical, Grid3x3, History, MessageSquare, Network, PlugZap, Route, SlidersHorizontal, Users, Workflow } from "lucide-react";
import { Members, MemberDetail, Organization } from "./people";
import { Apps, Matrix, Protocols } from "./apps";
import { Automation, Integrations, AppSettingsView, Audit } from "./operations";
import { AIProviders, AIPlayground, AIUsage } from "./ai";
import { Flows, FlowPage, Agents, Evaluations } from "./processes";
import { Glossary, Knowledge } from "./knowledge";

const views: View[] = [
  { id: "knowledge", title: () => t("Knowledge"), render: () => <Knowledge /> },
  { id: "glossary", title: () => t("Glossary"), render: () => <Glossary /> },
  { id: "agents", title: () => t("Agents"), render: () => <Agents /> },
  { id: "evaluations", title: () => t("Evaluations"), render: () => <Evaluations /> },
  { id: "flows", title: () => t("Flows"), render: () => <Flows /> },
  { id: "flow", title: (p) => p.id ?? t("Flow"), render: (p) => <FlowPage id={p.id ?? ""} /> },
  { id: "members", title: () => t("Members"), render: () => <Members /> },
  { id: "member", title: (p) => p.id ?? t("Member"), render: (p) => <MemberDetail id={p.id ?? ""} /> },
  { id: "organization", title: () => t("Organisation"), render: () => <Organization /> },
  { id: "apps", title: () => t("Apps"), render: () => <Apps /> },
  { id: "matrix", title: () => t("Capability matrix"), render: () => <Matrix /> },
  { id: "protocols", title: () => t("Protocols"), render: () => <Protocols /> },
  { id: "automation", title: () => t("Automation"), render: () => <Automation /> },
  { id: "integrations", title: () => t("Integrations"), render: () => <Integrations /> },
  { id: "app-settings", title: () => t("App settings"), render: () => <AppSettingsView /> },
  { id: "audit", title: () => t("Audit"), render: () => <Audit /> },
  { id: "ai-providers", title: () => t("AI providers"), render: () => <AIProviders /> },
  { id: "ai-playground", title: () => t("AI playground"), render: () => <AIPlayground /> },
  { id: "ai-usage", title: () => t("AI usage"), render: () => <AIUsage /> },
];


// Settings is the platform app's UI (ADR-0018): shown to members with a role in
// the platform, the organisation or AI; each section to those it concerns.
export default defineApp({
  id: "platform",
  title: t("Settings"),
  icon: <SlidersHorizontal />,
  home: { view: "members" },
  opens: { "flow.instance": "flow" },
  views,
  nav: (host) => {
    const nav = (label: string, icon: React.ReactNode, view: string) => ({ label, icon, route: { view } });
    const admin = !!host.role("platform");
    return [
      ...(admin || host.role("org") ? [{ label: t("Access"), items: [...(admin ? [nav(t("Members"), <Users />, "members")] : []), nav(t("Organisation"), <Network />, "organization")] }] : []),
      ...(admin ? [{ label: t("Apps"), items: [nav(t("Apps"), <Blocks />, "apps"), nav(t("App settings"), <SlidersHorizontal />, "app-settings"), nav(t("Capability matrix"), <Grid3x3 />, "matrix"), nav(t("Protocols"), <Cable />, "protocols")] }] : []),
      ...(host.role("ai") ? [{ label: "AI", items: [...(host.role("ai") === "admin" ? [nav(t("Providers and models"), <Bot />, "ai-providers")] : []), nav(t("Playground"), <MessageSquare />, "ai-playground"), nav(t("Usage"), <BarChart3 />, "ai-usage")] }] : []),
      ...(admin ? [{ label: t("Operations"), items: [nav(t("Integrations"), <PlugZap />, "integrations"), nav(t("Automation"), <Workflow />, "automation"), nav(t("Audit"), <History />, "audit")] }] : []),
      ...(host.role("flow") || host.role("agent") ? [{ label: t("Processes"), items: [...(host.role("flow") ? [nav(t("Flows"), <Route />, "flows")] : []),
        ...(host.role("agent") ? [nav(t("Agents"), <BrainCircuit />, "agents"), nav(t("Evaluations"), <FlaskConical />, "evaluations")] : [])] }] : []),
      ...(host.role("knowledge") ? [{ label: t("Knowledge"), items: [nav(t("Documents"), <BookOpen />, "knowledge"), nav(t("Glossary"), <BookA />, "glossary")] }] : []),
    ];
  },
});
