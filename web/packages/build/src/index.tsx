// The build package contributes one application: Builder (ADR-0054 D2) — the
// workspace where a builder designs applications. Its left rail lists what a
// project owns, by resource: Ontology (object types, action types, relationships,
// shared properties, queries), Interface (modules, templates), Automation
// (automations, flows, runs), Functions (AI, code), Releases (changes, history)
// and Explore. The editors themselves follow ADR-0053; the host still decides
// who holds the build role.
import "./i18n";
import { defineApp, type Host } from "@platform/app";
import { ModuleWorkbench } from "./workshop/ModuleWorkbench";
import { ObjectTypeEditor } from "./ontology/process";
import { ActionTypeEditor, ActionTypes } from "./ontology/action-type";
import { ModelWorkbench } from "./ontology/ModelWorkbench";
import { FlowEditor, Flows } from "./automate/workflow";
import { AutomationEditor, Automations } from "./automate/automation";
import { WorkflowRuns } from "./automate/workflow-runs";
import { LinkTypeEditor, LinkTypes } from "./ontology/link-type";
import { PropertyTypeEditor, PropertyTypes } from "./ontology/property-type";
import { DataSourceEditor, DataSources } from "./ontology/data-source";
import { ConnectionEditor, Connections } from "./ontology/connection";
import { DatasetEditor, Datasets } from "./ontology/dataset";
import { PipelineEditor, Pipelines } from "./ontology/pipeline";
import { WritebackEditor, Writebacks } from "./ontology/writeback";
import { IntegrationHealth } from "./ontology/integration-health";
import { DecisionTableEditor, DecisionTables } from "./functions/decision-table";
import { QueryEditor, Queries } from "./ontology/query";
import { FunctionEditor, Functions } from "./functions/function";
import { CodeEditor, CodeFunctions } from "./functions/code";
import { ReleaseReview, releaseDraftsParam, releaseKinds, type ReleaseKind } from "./releases/release";
import { Changes } from "./releases/changes";
import { ProjectHome, ProjectsList } from "./projects/project";
import { StudioTemplates } from "./workshop/template-ui";
import { t, type NavSection, type View } from "@platform/ui";
import { ApplicationScope } from "./projects/application-scope";
import { Activity, BookOpen, Boxes, Braces, Clock, Compass, Database, GitBranch, GitMerge, Hammer, History, Layers, LayoutList, LayoutTemplate, Link2, PackageCheck, Plug, Search, Send, Sparkles, Table2, Tags, Workflow, Zap } from "lucide-react";

const scoped = (p: Record<string, string | undefined>, node: React.ReactNode) => <ApplicationScope application={p.application}>{node}</ApplicationScope>;
const builder = (host: Host) => host.role("build") === "builder";

const views: View[] = [
  // Projects
  { id: "projects", title: () => t("Projects"), render: () => <ProjectsList /> },
  { id: "project", title: () => t("Project"), render: (p) => <ApplicationScope key={p.id ?? "new"} application={p.id}><ProjectHome id={p.id ?? ""} /></ApplicationScope> },
  // Ontology
  { id: "object-type", title: (p) => p.id ? t("Object type") : t("Object types"), render: (p) => p.id ? <ApplicationScope key={p.id} application={p.application}><ObjectTypeEditor id={p.id} initialField={p.field} initialAction={p.action} initialAccess={p.access === "true"} initialTab={p.tab} /></ApplicationScope> : scoped(p, <ModelWorkbench key={p.object ?? "catalog"} initialObject={p.object} initialTab={p.tab} />) },
  { id: "action-type", title: () => t("Action type"), render: (p) => p.id ? <ApplicationScope key={p.id} application={p.application}><ActionTypeEditor id={p.id} action={p.action} /></ApplicationScope> : scoped(p, <ActionTypes />) },
  { id: "link-type", title: () => t("Relationships"), render: (p) => p.id ? <ApplicationScope key={p.id} application={p.application}><LinkTypeEditor id={p.id} parent={p.parent} child={p.child} via={p.via} /></ApplicationScope> : <LinkTypes /> },
  { id: "property-type", title: () => t("Shared properties"), render: (p) => p.id ? <ApplicationScope key={p.id} application={p.application}><PropertyTypeEditor id={p.id} /></ApplicationScope> : <PropertyTypes /> },
  { id: "connection", title: (p) => p.id ? t("Connection") : t("Connections"), render: (p) => p.id ? <ApplicationScope key={p.id} application={p.application}><ConnectionEditor id={p.id} /></ApplicationScope> : <Connections /> },
  { id: "data-source", title: (p) => p.id ? t("Data source") : t("Data sources"), render: (p) => p.id ? <ApplicationScope key={p.id} application={p.application}><DataSourceEditor id={p.id} /></ApplicationScope> : <DataSources /> },
  { id: "dataset", title: (p) => p.id ? t("Dataset") : t("Datasets"), render: (p) => p.id ? <ApplicationScope key={p.id} application={p.application}><DatasetEditor id={p.id} /></ApplicationScope> : <Datasets /> },
  { id: "pipeline", title: (p) => p.id ? t("Pipeline") : t("Pipelines"), render: (p) => p.id ? <ApplicationScope key={p.id} application={p.application}><PipelineEditor id={p.id} /></ApplicationScope> : <Pipelines /> },
  { id: "integration-health", title: () => t("Integration health"), render: () => <IntegrationHealth /> },
  { id: "writeback", title: (p) => p.id ? t("Writeback") : t("Writebacks"), render: (p) => p.id ? <ApplicationScope key={p.id} application={p.application}><WritebackEditor id={p.id} /></ApplicationScope> : <Writebacks /> },
  { id: "query", title: () => t("Queries"), render: (p) => p.id ? <ApplicationScope key={p.id} application={p.application}><QueryEditor id={p.id} /></ApplicationScope> : <Queries /> },
  // Interface
  { id: "module", title: (p) => p.page ? t("Page") : p.id ? t("Module") : t("Modules"), render: (p) => <ApplicationScope key={`${p.id ?? ""}:${p.page ?? ""}`} application={p.application ?? p.id}><ModuleWorkbench id={p.id} page={p.page} /></ApplicationScope> },
  { id: "studio-templates", title: () => t("Templates"), render: (p) => <StudioTemplates key={p.template ?? "templates"} initial={p.template} /> },
  // Automation
  { id: "automation", title: (p) => p.id ? t("Automation") : t("Automations"), render: (p) => p.id ? <ApplicationScope key={p.id} application={p.application}><AutomationEditor id={p.id} /></ApplicationScope> : scoped(p, <Automations />) },
  { id: "flow", title: (p) => p.id ? t("Flow") : t("Flows"), render: (p) => p.id ? <ApplicationScope key={p.id} application={p.application}><FlowEditor id={p.id} /></ApplicationScope> : scoped(p, <Flows />) },
  { id: "runs", title: () => t("Runs"), render: (p) => scoped(p, <div className="p-4"><WorkflowRuns name={p.name ?? ""} onStepSelect={() => {}} /></div>) },
  // Functions
  { id: "function", title: () => t("AI functions"), render: (p) => p.id ? <ApplicationScope key={p.id} application={p.application}><FunctionEditor id={p.id} /></ApplicationScope> : <Functions /> },
  { id: "decision-table", title: (p) => p.id ? t("Decision table") : t("Decision tables"), render: (p) => p.id ? <ApplicationScope key={p.id} application={p.application}><DecisionTableEditor id={p.id} /></ApplicationScope> : <DecisionTables /> },
  { id: "code", title: () => t("Code functions"), render: (p) => p.id ? <ApplicationScope key={p.id} application={p.application}><CodeEditor id={p.id} /></ApplicationScope> : <CodeFunctions /> },
  // Releases
  { id: "changes", title: () => t("Changes"), render: (p) => scoped(p, <Changes key={`${p.kind ?? ""}:${p.id ?? ""}`} initialKind={p.kind} initialID={p.id} />) },
  { id: "release-review", title: () => t("Release review"), render: (p) => <ApplicationScope application={p.application}><ReleaseReview key={`${p.kind}:${p.id}:${p.drafts ?? ""}`} initialKind={releaseKinds.includes(p.kind as ReleaseKind) ? p.kind as ReleaseKind : "object"} initialID={p.id} initialDrafts={releaseDraftsParam(p.drafts, releaseKinds)} /></ApplicationScope> },
  { id: "release-history", title: () => t("Release history"), render: (p) => scoped(p, <ReleaseReview key="history" />) },
];

/** Builder: one workspace, one left rail, grouped by what a project owns (ADR-0054 D2). */
export default defineApp({
  id: "build",
  surface: "studio",
  category: "build",
  title: t("Builder"),
  description: t("Design applications: their object types, modules, automations and functions, and release them together."),
  icon: <Hammer />,
  home: { view: "projects" },
  views,
  opens: { "build.app": "project", "build.object": "object-type", "build.linktype": "link-type", "build.propertytype": "property-type", "build.query": "query", "build.page": "module", "build.process": "flow", "build.function": "function", "build.code": "code" },
  nav: (host): NavSection[] => builder(host) ? [
    { label: t("Ontology"), items: [
      { label: t("Object types"), icon: <Boxes />, route: { view: "object-type" } },
      { label: t("Action types"), icon: <Zap />, route: { view: "action-type" } },
      { label: t("Relationships"), icon: <Link2 />, route: { view: "link-type" } },
      { label: t("Shared properties"), icon: <Tags />, route: { view: "property-type" } },
      { label: t("Queries"), icon: <Search />, route: { view: "query" } },
      { label: t("Connections"), icon: <Plug />, route: { view: "connection" } },
      { label: t("Data sources"), icon: <Database />, route: { view: "data-source" } },
      { label: t("Datasets"), icon: <Layers />, route: { view: "dataset" } },
      { label: t("Pipelines"), icon: <GitMerge />, route: { view: "pipeline" } },
      { label: t("Writebacks"), icon: <Send />, route: { view: "writeback" } },
      { label: t("Integration health"), icon: <Activity />, route: { view: "integration-health" } },
    ] },
    { label: t("Interface"), items: [
      { label: t("Modules"), icon: <LayoutTemplate />, route: { view: "module" } },
      { label: t("Templates"), icon: <LayoutList />, route: { view: "studio-templates" } },
    ] },
    { label: t("Logic"), items: [
      { label: t("Automations"), icon: <Zap />, route: { view: "automation" } },
      { label: t("Flows"), icon: <Workflow />, route: { view: "flow" } },
      { label: t("Runs"), icon: <Clock />, route: { view: "runs" } },
    ] },
    { label: t("Functions"), items: [
      { label: t("AI functions"), icon: <Sparkles />, route: { view: "function" } },
      { label: t("Code functions"), icon: <Braces />, route: { view: "code" } },
      { label: t("Decision tables"), icon: <Table2 />, route: { view: "decision-table" } },
    ] },
    { label: t("Releases"), items: [
      { label: t("Changes"), icon: <PackageCheck />, route: { view: "changes" } },
      { label: t("Release history"), icon: <History />, route: { view: "release-history" } },
    ] },
    { label: t("Explore"), items: [
      { label: t("Object Explorer"), icon: <Compass />, route: { view: "explorer" } },
      { label: t("Lineage"), icon: <GitBranch />, route: { view: "lineage" } },
      { label: t("Asset Library"), icon: <BookOpen />, route: { view: "catalog" } },
    ] },
  ] : [ // a publisher reviews and activates; the editors stay read-only for them
    { label: t("Releases"), items: [
      { label: t("Changes"), icon: <PackageCheck />, route: { view: "changes" } },
      { label: t("Release history"), icon: <History />, route: { view: "release-history" } },
    ] },
  ],
});
