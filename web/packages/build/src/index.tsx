// The build package contributes the applications a builder works in
// (ADR-0052 §3.3, ADR-0053 §3): Projects (the applications under design and
// everything they own), Ontology (object types, action types, relationships,
// shared properties, queries), Workshop (modules and their pages), Automate
// (automations, logic flows, runs), AI Functions, Code and Releases (changes,
// history). One package, several entries in the portal: the host still decides
// who holds the build role.
import "./i18n";
import { defineApp, type AppUI, type Host } from "@platform/app";
import { ModuleWorkbench } from "./workshop/ModuleWorkbench";
import { ObjectTypeEditor, ActionTypeEditor } from "./ontology/process";
import { ModelWorkbench } from "./ontology/ModelWorkbench";
import { FlowEditor, Flows } from "./automate/workflow";
import { AutomationEditor, Automations } from "./automate/automation";
import { WorkflowRuns } from "./automate/workflow-runs";
import { LinkTypeEditor, LinkTypes } from "./ontology/link-type";
import { PropertyTypeEditor, PropertyTypes } from "./ontology/property-type";
import { QueryEditor, Queries } from "./ontology/query";
import { FunctionEditor, Functions } from "./functions/function";
import { CodeEditor, CodeFunctions } from "./functions/code";
import { ReleaseReview, releaseDraftsParam, releaseKinds, type ReleaseKind } from "./releases/release";
import { Changes } from "./releases/changes";
import { ProjectHome, ProjectsList } from "./projects/project";
import { StudioTemplates } from "./workshop/template-ui";
import { t, type NavSection, type View } from "@platform/ui";
import { ApplicationScope } from "./projects/application-scope";
import { AppWindow, Boxes, Braces, Clock, Compass, FolderKanban, GitBranch, History, LayoutList, LayoutTemplate, Link2, Network, PackageCheck, Search, Sparkles, Tags, Terminal, Workflow, Zap } from "lucide-react";

const scoped = (p: Record<string, string | undefined>, node: React.ReactNode) => <ApplicationScope application={p.application}>{node}</ApplicationScope>;
const builder = (host: Host) => host.role("build") === "builder";

// --- Ontology: the semantic layer every application is built on.
const ontologyViews: View[] = [
  { id: "object-type", title: (p) => p.id ? t("Object type") : t("Object types"), render: (p) => p.id ? <ApplicationScope key={p.id} application={p.application}><ObjectTypeEditor id={p.id} initialField={p.field} initialAction={p.action} initialAccess={p.access === "true"} initialTab={p.tab} /></ApplicationScope> : scoped(p, <ModelWorkbench key={p.object ?? "catalog"} initialObject={p.object} initialTab={p.tab} />) },
  { id: "action-type", title: () => t("Action type"), render: (p) => p.id ? <ApplicationScope key={p.id} application={p.application}><ActionTypeEditor id={p.id} action={p.action} /></ApplicationScope> : scoped(p, <ModelWorkbench key="actions" initialTab="actions" />) },
  { id: "link-type", title: () => t("Relationships"), render: (p) => p.id ? <ApplicationScope key={p.id} application={p.application}><LinkTypeEditor id={p.id} parent={p.parent} child={p.child} via={p.via} /></ApplicationScope> : <LinkTypes /> },
  { id: "property-type", title: () => t("Shared properties"), render: (p) => p.id ? <ApplicationScope key={p.id} application={p.application}><PropertyTypeEditor id={p.id} /></ApplicationScope> : <PropertyTypes /> },
  { id: "query", title: () => t("Queries"), render: (p) => p.id ? <ApplicationScope key={p.id} application={p.application}><QueryEditor id={p.id} /></ApplicationScope> : <Queries /> },
];
export const ontology = defineApp({
  id: "ontology", serves: ["build"], category: "ontology", title: t("Ontology"), icon: <Network />,
  description: t("Object types, relationships, shared properties and queries: the semantic model every application reads."),
  home: { view: "object-type" }, views: ontologyViews,
  opens: { "build.object": "object-type", "build.linktype": "link-type", "build.propertytype": "property-type", "build.query": "query" },
  nav: (): NavSection[] => [
    { label: t("Ontology"), items: [
      { label: t("Object types"), icon: <Boxes />, route: { view: "object-type" } },
      { label: t("Action types"), icon: <Zap />, route: { view: "action-type" } },
      { label: t("Relationships"), icon: <Link2 />, route: { view: "link-type" } },
      { label: t("Shared properties"), icon: <Tags />, route: { view: "property-type" } },
      { label: t("Queries"), icon: <Search />, route: { view: "query" } },
    ] },
    { label: t("Explore"), items: [
      { label: t("Object Explorer"), icon: <Compass />, route: { view: "explorer" } },
      { label: t("Lineage"), icon: <GitBranch />, route: { view: "lineage" } },
    ] },
  ],
});

// --- Workshop: modules — the pages people use, with their header and navigation.
export const workshop = defineApp({
  id: "workshop", serves: ["build"], category: "build", title: t("Workshop"), icon: <LayoutTemplate />,
  description: t("Compose the modules of an application: pages of typed widgets over the Ontology, with their header and navigation."),
  home: { view: "module" },
  views: [
    { id: "module", title: (p) => p.page ? t("Page") : p.id ? t("Module") : t("Modules"), render: (p) => <ApplicationScope key={`${p.id ?? ""}:${p.page ?? ""}`} application={p.application ?? p.id}><ModuleWorkbench id={p.id} page={p.page} /></ApplicationScope> },
    { id: "studio-templates", title: () => t("Templates"), render: (p) => <StudioTemplates key={p.template ?? "templates"} initial={p.template} /> },
  ],
  opens: { "build.page": "module" },
  nav: () => [{ label: t("Workshop"), items: [
    { label: t("Modules"), icon: <LayoutTemplate />, route: { view: "module" } },
    { label: t("Templates"), icon: <LayoutList />, route: { view: "studio-templates" } },
  ] }],
});

// --- Automate: automations (trigger → effects) and logic flows; their runs.
export const automate = defineApp({
  id: "automate", serves: ["build"], category: "build", title: t("Automate"), icon: <Workflow />,
  description: t("Automations that run when records change; logic flows for branching work; the runs of both."),
  home: { view: "automation" },
  views: [
    { id: "automation", title: (p) => p.id ? t("Automation") : t("Automations"), render: (p) => p.id ? <ApplicationScope key={p.id} application={p.application}><AutomationEditor id={p.id} /></ApplicationScope> : scoped(p, <Automations />) },
    { id: "flow", title: (p) => p.id ? t("Flow") : t("Flows"), render: (p) => p.id ? <ApplicationScope key={p.id} application={p.application}><FlowEditor id={p.id} /></ApplicationScope> : scoped(p, <Flows />) },
    { id: "runs", title: () => t("Runs"), render: (p) => scoped(p, <div className="p-4"><WorkflowRuns name={p.name ?? ""} onStepSelect={() => {}} /></div>) },
  ],
  opens: { "build.process": "flow" },
  nav: () => [{ label: t("Automate"), items: [
    { label: t("Automations"), icon: <Zap />, route: { view: "automation" } },
    { label: t("Flows"), icon: <Workflow />, route: { view: "flow" } },
    { label: t("Runs"), icon: <Clock />, route: { view: "runs" } },
  ] }],
});

// --- AI Functions and Code: logic the Ontology's actions call.
export const aiFunctions = defineApp({
  id: "ai-functions", serves: ["build"], category: "build", title: t("AI Functions"), icon: <Sparkles />,
  description: t("Functions a model answers, bound to object types and reviewed before release."),
  home: { view: "function" },
  views: [{ id: "function", title: () => t("AI functions"), render: (p) => p.id ? <ApplicationScope key={p.id} application={p.application}><FunctionEditor id={p.id} /></ApplicationScope> : <Functions /> }],
  opens: { "build.function": "function" },
  nav: () => [{ label: t("AI Functions"), items: [{ label: t("Functions"), icon: <Sparkles />, route: { view: "function" } }] }],
});
export const code = defineApp({
  id: "code", serves: ["build"], category: "developer", title: t("Code"), icon: <Terminal />,
  description: t("Code functions with typed inputs and outputs, versioned with the application."),
  home: { view: "code" },
  views: [{ id: "code", title: () => t("Code functions"), render: (p) => p.id ? <ApplicationScope key={p.id} application={p.application}><CodeEditor id={p.id} /></ApplicationScope> : <CodeFunctions /> }],
  opens: { "build.code": "code" },
  nav: () => [{ label: t("Code"), items: [{ label: t("Code functions"), icon: <Braces />, route: { view: "code" } }] }],
});

// --- Releases: what changed, review and activate a release, and the history.
const releaseView: View = { id: "release-review", title: () => t("Release review"), render: (p) => <ApplicationScope application={p.application}><ReleaseReview key={`${p.kind}:${p.id}:${p.drafts ?? ""}`} initialKind={releaseKinds.includes(p.kind as ReleaseKind) ? p.kind as ReleaseKind : "object"} initialID={p.id} initialDrafts={releaseDraftsParam(p.drafts, releaseKinds)} /></ApplicationScope> };
export const releases = defineApp({
  id: "releases", serves: ["build"], category: "operate", title: t("Releases"), icon: <PackageCheck />,
  description: t("See what changed, review what a release would do and activate it; look back at the history."),
  home: { view: "changes" },
  views: [
    { id: "changes", title: () => t("Changes"), render: (p) => scoped(p, <Changes key={`${p.kind ?? ""}:${p.id ?? ""}`} initialKind={p.kind} initialID={p.id} />) },
    releaseView,
    { id: "release-history", title: () => t("Release history"), render: (p) => scoped(p, <ReleaseReview key="history" />) },
  ],
  nav: () => [{ label: t("Releases"), items: [
    { label: t("Changes"), icon: <PackageCheck />, route: { view: "changes" } },
    { label: t("Release history"), icon: <History />, route: { view: "release-history" } },
  ] }],
});

/** The builder's applications beside Projects (the default export). */
export const contributions: AppUI[] = [ontology, workshop, automate, aiFunctions, code, releases];

// --- Projects: the applications under design and everything they own.
export default defineApp({
  id: "build",
  surface: "studio",
  category: "build",
  description: t("The applications under design: each project owns its object types, module, automations and functions, and releases them together."),
  title: t("Projects"),
  icon: <FolderKanban />,
  home: { view: "projects" },
  views: [
    { id: "projects", title: () => t("Projects"), render: () => <ProjectsList /> },
    { id: "project", title: () => t("Project"), render: (p) => <ApplicationScope key={p.id ?? "new"} application={p.id}><ProjectHome id={p.id ?? ""} /></ApplicationScope> },
  ],
  opens: { "build.app": "project" },
  nav: (host): NavSection[] => [
    { label: t("Projects"), items: [
      { label: t("All projects"), icon: <AppWindow />, route: { view: "projects" } },
      ...(builder(host) ? [{ label: t("Templates"), icon: <LayoutList />, route: { view: "studio-templates" } }] : []),
      { label: t("Discover capabilities"), icon: <Compass />, route: { view: "catalog", params: { mode: "builder", surface: "studio" } } },
    ] },
  ],
});
