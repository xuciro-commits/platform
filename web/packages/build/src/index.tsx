// The build package contributes the applications a builder works in
// (ADR-0052 §3.3): Ontology (object types, relationships, shared properties
// and queries), Workshop (pages), Automate (workflows), AI Functions, Code,
// Projects (the applications under design and their shared resources) and
// Releases (test and seal candidates; activate a release). One package, several
// entries in the portal: the host still decides who holds the build role.
import "./i18n";
import { defineApp, type AppUI, type Host } from "@platform/app";
import { PageEditor, PagesList } from "./editor";
import { ProcessEditor, Objects } from "./process";
import { ModelWorkbench } from "./model-editor/ModelWorkbench";
import { WorkflowEditor, Workflows } from "./workflow";
import { LinkTypeEditor, LinkTypes } from "./link-type";
import { PropertyTypeEditor, PropertyTypes } from "./property-type";
import { QueryEditor, Queries } from "./query";
import { FunctionEditor, Functions } from "./function";
import { ApplicationEditor, Applications } from "./application";
import { CodeEditor, CodeFunctions } from "./code";
import { ReleaseReview, releaseDraftsParam, releaseKinds, type ReleaseKind } from "./release";
import { CandidateTest } from "./simulate";
import { StudioOverview } from "./studio";
import { StudioTemplates } from "./template-ui";
import { t, type NavSection, type View } from "@platform/ui";
import { ApplicationScope } from "./application-scope";
import { AppWindow, Boxes, Braces, Compass, FlaskConical, FolderKanban, GitBranch, LayoutList, LayoutTemplate, Link2, Network, PackageCheck, Search, Sparkles, Tags, Terminal, Workflow } from "lucide-react";

const scoped = (p: Record<string, string | undefined>, node: React.ReactNode) => <ApplicationScope application={p.application}>{node}</ApplicationScope>;
const builder = (host: Host) => host.role("build") === "builder";

// --- Ontology: the semantic layer every application is built on.
const ontologyViews: View[] = [
  { id: "model", title: () => t("Object types"), render: (p) => scoped(p, <ModelWorkbench key={p.object ?? "catalog"} initialObject={p.object} initialTab={p.tab} />) },
  { id: "process", title: (p) => p.id ? t("Object type") : t("Object types"), render: (p) => p.id ? <ApplicationScope key={p.id} application={p.application}><ProcessEditor id={p.id} initialField={p.field} initialAction={p.action} initialAccess={p.access === "true"} /></ApplicationScope> : <Objects /> },
  { id: "link-type", title: () => t("Relationships"), render: (p) => p.id ? <ApplicationScope key={p.id} application={p.application}><LinkTypeEditor id={p.id} parent={p.parent} child={p.child} via={p.via} /></ApplicationScope> : <LinkTypes /> },
  { id: "property-type", title: () => t("Shared properties"), render: (p) => p.id ? <ApplicationScope key={p.id} application={p.application}><PropertyTypeEditor id={p.id} /></ApplicationScope> : <PropertyTypes /> },
  { id: "query", title: () => t("Queries"), render: (p) => p.id ? <ApplicationScope key={p.id} application={p.application}><QueryEditor id={p.id} /></ApplicationScope> : <Queries /> },
];
export const ontology = defineApp({
  id: "ontology", serves: ["build"], category: "ontology", title: t("Ontology"), icon: <Network />,
  description: t("Object types, relationships, shared properties and queries: the semantic model every application reads."),
  home: { view: "model" }, views: ontologyViews,
  opens: { "build.object": "process", "build.linktype": "link-type", "build.propertytype": "property-type", "build.query": "query" },
  nav: (): NavSection[] => [
    { label: t("Ontology"), items: [
      { label: t("Object types"), icon: <Boxes />, route: { view: "model" } },
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

// --- Workshop: pages people use.
export const workshop = defineApp({
  id: "workshop", serves: ["build"], category: "build", title: t("Workshop"), icon: <LayoutTemplate />,
  description: t("Compose the pages of an application from typed blocks over the Ontology."),
  home: { view: "pages" },
  views: [
    { id: "pages", title: () => t("Pages"), render: () => <PagesList /> },
    { id: "compose", title: () => t("Compose a page"), render: (p) => <ApplicationScope key={p.id ?? "new"} application={p.application}><PageEditor id={p.id ?? ""} /></ApplicationScope> },
  ],
  opens: { "build.page": "compose" },
  nav: () => [{ label: t("Workshop"), items: [
    { label: t("Pages"), icon: <LayoutTemplate />, route: { view: "pages" } },
    { label: t("Templates"), icon: <LayoutList />, route: { view: "studio-templates" } },
  ] }],
});

// --- Automate: workflows that react to decisions and time.
export const automate = defineApp({
  id: "automate", serves: ["build"], category: "build", title: t("Automate"), icon: <Workflow />,
  description: t("Design the workflows that run when decisions are taken; follow their runs."),
  home: { view: "workflow" },
  views: [{ id: "workflow", title: () => t("Workflows"), render: (p) => p.id ? <ApplicationScope key={p.id} application={p.application}><WorkflowEditor id={p.id} /></ApplicationScope> : <Workflows /> }],
  opens: { "build.process": "workflow" },
  nav: (host) => [{ label: t("Automate"), items: [
    { label: t("Workflows"), icon: <Workflow />, route: { view: "workflow" } },
    ...(host.role("flow") ? [{ label: t("Workflow runs"), icon: <FlaskConical />, route: { view: "flows" } }] : []),
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

// --- Releases: test a candidate, review and activate a release (builder and publisher).
const releaseView: View = { id: "release-review", title: () => t("Release review"), render: (p) => <ApplicationScope application={p.application}><ReleaseReview key={`${p.kind}:${p.id}:${p.drafts ?? ""}`} initialKind={releaseKinds.includes(p.kind as ReleaseKind) ? p.kind as ReleaseKind : "object"} initialID={p.id} initialDrafts={releaseDraftsParam(p.drafts, releaseKinds)} /></ApplicationScope> };
export const releases = defineApp({
  id: "releases", serves: ["build"], category: "operate", title: t("Releases"), icon: <PackageCheck />,
  description: t("Test candidates, review what a release changes and activate it."),
  home: { view: "release-review" },
  views: [releaseView,
    { id: "candidate-test", title: () => t("Test a candidate"), render: (p) => <ApplicationScope application={p.application}><CandidateTest key={p.functionId ?? p.processId ?? p.objectId ?? "object"} processId={p.processId} functionId={p.functionId} objectId={p.objectId} /></ApplicationScope> }],
  nav: (host) => [{ label: t("Releases"), items: [
    { label: t("Release review"), icon: <PackageCheck />, route: { view: "release-review" } },
    ...(builder(host) ? [{ label: t("Test a candidate"), icon: <FlaskConical />, route: { view: "candidate-test" } }] : []),
  ] }],
});

/** The builder's applications beside Projects (the default export). */
export const contributions: AppUI[] = [ontology, workshop, automate, aiFunctions, code, releases];

// --- Projects: the applications under design, their shared resources and templates.
export default defineApp({
  id: "build",
  surface: "studio",
  category: "build",
  description: t("The applications under design: their pages, object types, workflows and functions, and the templates they start from."),
  title: t("Projects"),
  icon: <FolderKanban />,
  home: { view: "applications" },
  views: [
    { id: "studio", title: () => t("Shared resources"), render: () => <StudioOverview /> },
    { id: "studio-templates", title: () => t("Templates"), render: (p) => <StudioTemplates key={p.template ?? "templates"} initial={p.template} /> },
    { id: "applications", title: () => t("Projects"), render: () => <Applications /> },
    { id: "application", title: () => t("Application design"), render: (p) => <ApplicationEditor key={p.id ?? "new"} id={p.id ?? "new"} /> },
  ],
  opens: { "build.app": "application" },
  nav: (host): NavSection[] => [
    { label: t("Projects"), items: [
      { label: t("All projects"), icon: <AppWindow />, route: { view: "applications" } },
      { label: t("Shared resources"), icon: <Boxes />, route: { view: "studio" } },
      ...(builder(host) ? [{ label: t("Templates"), icon: <LayoutList />, route: { view: "studio-templates" } }] : []),
      { label: t("Discover capabilities"), icon: <Compass />, route: { view: "catalog", params: { mode: "builder", surface: "studio" } } },
    ] },
  ],
});
