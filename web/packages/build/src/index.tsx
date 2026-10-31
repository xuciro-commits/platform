import { ApplicationSampleAsset } from "./releases/samples";
import "./i18n";
import { defineApp, useHost, type Host } from "@platform/app";
import { ModuleWorkbench } from "./workshop/ModuleWorkbench";
import { ObjectTypeEditor } from "./ontology/process";
import { ActionTypeEditor, ActionTypes } from "./ontology/action-type";
import { ModelWorkbench } from "./ontology/ModelWorkbench";
import { FlowEditor } from "./automate/workflow";
import { AutomationEditor, Automations } from "./automate/automation";
import { WorkflowRuns } from "./automate/workflow-runs";
import { LinkTypeEditor, LinkTypes } from "./ontology/link-type";
import { PropertyTypeEditor, PropertyTypes } from "./ontology/property-type";
import { ValueTypeEditor, ValueTypes } from "./ontology/value-type";
import { ObservationSourceEditor, ObservationSources } from "./ontology/observation";
import { DataSourceEditor, DataSources } from "./ontology/data-source";
import { ConnectionEditor, Connections } from "./ontology/connection";
import { DatasetEditor, Datasets } from "./ontology/dataset";
import { PipelineEditor, Pipelines } from "./ontology/pipeline";
import { WritebackEditor, Writebacks } from "./ontology/writeback";
import { MatchEditor, Matches } from "./ontology/match";
import { IntegrationHealth } from "./ontology/integration-health";
import { DecisionTableEditor } from "./functions/decision-table";
import { QueryEditor, Queries } from "./ontology/query";
import { FunctionEditor } from "./functions/function";
import { CodeEditor } from "./functions/code";
import { AgentEditor, Agents } from "./functions/agent";
import { AlertEditor, Alerts } from "./functions/alert";
import { ReleaseReview, releaseDraftsSelection, releaseKinds, type ReleaseKind } from "./releases/release";
import { Branches } from "./branches/Branches";
import { ProposalScreen } from "./branches/Proposal";
import { CandidateTest } from "./releases/simulate";
import { ProjectHome, ProjectsList } from "./projects/project";
import { Discover } from "./projects/discover";
import { StudioTemplates } from "./workshop/template-ui";
import { PageHeader, t, type NavItem, type NavSection, type Route, type View } from "@platform/ui";
import type { ReactNode } from "react";
import { ApplicationScope } from "./projects/application-scope";
import { ToolContext } from "./projects/tool-context";
import { studioApplicationID, studioAreaForRoute } from "./projects/studio-model";
import { FunctionCatalog } from "./functions/catalog";
import { Interfaces } from "./ontology/ModelWorkbench";
import { Activity, BellRing, Bot, Boxes, Braces, ClipboardCheck, Database, FolderTree, GitBranch, GitMerge, History, Layers, LayoutTemplate, Link2, Network, PanelsTopLeft, Plug, Radio, Rocket, Ruler, Search, Shapes, Tags, TableProperties, Workflow, Zap } from "lucide-react";

const builder = (host: Host) => host.holds("build", "builder");
const integrator = (host: Host) => host.holds("build", "integrator");
const publisher = (host: Host) => host.holds("build", "publisher");

const view = (id: string, title: () => string, render: View["render"], documentKey?: View["documentKey"]): View => ({ id, title: () => title(), render, ...(documentKey ? { documentKey } : {}) });
const projectViews: View[] = [
  { id: "projects", title: () => t("Files"), render: () => <ProjectsList /> },
  { id: "project", documentKey: p => p.id ?? "", title: () => t("Project"), render: p => <ProjectHome key={p.id} id={p.id ?? ""} area={p.area} initialSelected={p.asset} initialSection={p.section} initialPlan={p.plan} initialTemplate={p.template} /> },
];
const ontologyViews: View[] = [
  view("discover", () => t("Overview"), () => <Discover />),
  { id: "object-type", documentKey: p => p.id ?? p.object ?? "catalog", title: p => p.id || p.object ? t("Object type") : t("Object types"), render: p => p.id ? <ObjectTypeEditor key={p.id} id={p.id} initialField={p.field} initialAction={p.action} initialAccess={p.access === "true"} initialTab={p.tab} /> : <ModelWorkbench key={p.object ?? "catalog"} initialObject={p.object} initialTab={p.tab} /> },
  view("link-type", () => t("Link types"), p => p.id ? <LinkTypeEditor key={p.id} id={p.id} parent={p.parent} child={p.child} via={p.via} /> : <LinkTypes />),
  view("interfaces", () => t("Interfaces"), () => <Interfaces />),
  view("property-type", () => t("Shared properties"), p => p.id ? <PropertyTypeEditor key={p.id} id={p.id} /> : <PropertyTypes />),
  view("value-type", () => t("Value types"), p => p.id ? <ValueTypeEditor key={p.id} id={p.id} /> : <ValueTypes />),
  view("action-type", () => t("Action types"), p => p.id ? <ActionTypeEditor key={p.id} id={p.id} action={p.action} /> : <ActionTypes />),
  view("changes", () => t("Branches and proposals"), () => <Branches />),
  { id: "proposal", documentKey: p => p.id ?? "", title: () => t("Proposal"), render: p => <ProposalScreen key={p.id} id={p.id ?? ""} /> },
];
const connectionViews: View[] = [
  view("connection", () => t("Connections"), p => p.id ? <ConnectionEditor key={p.id} id={p.id} /> : <Connections />),
  view("data-source", () => t("Data sources"), p => p.id ? <DataSourceEditor key={p.id} id={p.id} /> : <DataSources />),
  view("observation-source", () => t("Observation sources"), p => p.id ? <ObservationSourceEditor key={p.id} id={p.id} /> : <ObservationSources />),
  view("writeback", () => t("Writebacks"), p => p.id ? <WritebackEditor key={p.id} id={p.id} /> : <Writebacks />),
];
const pipelineViews: View[] = [
  view("pipeline", () => t("Pipelines"), p => p.id ? <PipelineEditor key={p.id} id={p.id} /> : <Pipelines />),
  view("match", () => t("Matching rules"), p => p.id ? <MatchEditor key={p.id} id={p.id} /> : <Matches />),
];
const datasetViews: View[] = [
  view("dataset", () => t("Datasets"), p => p.id ? <DatasetEditor key={p.id} id={p.id} /> : <Datasets />),
];
const monitoringViews: View[] = [
  view("integration-health", () => t("Integration health"), () => <IntegrationHealth />),
];
const logicViews: View[] = [
  view("functions", () => t("Functions"), () => <FunctionCatalog />),
  view("code", () => t("Code function"), p => p.id ? <CodeEditor key={p.id} id={p.id} /> : <FunctionCatalog />),
  view("decision-table", () => t("Decision table"), p => p.id ? <DecisionTableEditor key={p.id} id={p.id} /> : <FunctionCatalog />),
  view("function", () => t("AI function"), p => p.id ? <FunctionEditor key={p.id} id={p.id} /> : <FunctionCatalog />),
  view("flow", () => t("Logic flow"), p => p.id ? <FlowEditor key={p.id} id={p.id} /> : <FunctionCatalog />),
  view("runs", () => t("Runs"), p => <WorkflowRuns name={p.name ?? ""} onStepSelect={() => {}} />),
  view("query", () => t("Queries"), p => p.id ? <QueryEditor key={p.id} id={p.id} /> : <Queries />),
];
const automateViews: View[] = [
  view("automation", () => t("Automations"), p => p.id ? <AutomationEditor key={p.id} id={p.id} /> : <Automations />),
  view("alert", () => t("Alert rules"), p => p.id ? <AlertEditor key={p.id} id={p.id} /> : <Alerts />),
];
const workshopViews: View[] = [
  view("module", () => t("Workshop"), p => <ModuleWorkbench key={`${p.id ?? ""}:${p.page ?? ""}`} id={p.id} page={p.page} focus={p.focus} />),
  view("studio-templates", () => t("Templates"), p => <StudioTemplates key={p.template ?? "templates"} initial={p.template} />),
];
const releaseViews: View[] = [
  { id: "release-review", title: () => t("Release review"), render: p => {
    const selection = releaseDraftsSelection(p.drafts, releaseKinds);
    return <ReleaseReview key={`${p.kind}:${p.id}:${p.drafts ?? ""}:${p.candidate ?? ""}`} initialKind={releaseKinds.includes(p.kind as ReleaseKind) ? p.kind as ReleaseKind : "object"} initialID={p.id} initialDrafts={selection.drafts} selectionError={selection.error} candidateID={p.candidate} applicationName={p.applicationName} />;
  } },
  view("release-history", () => t("Release history"), () => <ReleaseReview key="history" />),
  view("test-plan", () => t("Business test plans"), p => <CandidateTest key={p.id ?? "new"} planId={p.id} applicationId={p.application} />),
  view("evaluation-set", () => t("Evaluation set"), p => <CandidateTest key={p.id ?? "new"} planId={p.id === "new" ? undefined : p.id} evaluationSet />),
  view("sample-set", () => t("Sample set"), p => <ApplicationSampleAsset key={p.id ?? "new"} id={p.id} />),
];
const agentViews: View[] = [
  view("agent", () => t("Agent definitions"), p => p.id ? <AgentEditor key={p.id} id={p.id} /> : <Agents />),
];

function referenceContext(route: Route, children: ReactNode) {
  const params = route.params ?? {};
  const application = studioApplicationID(route) ?? (route.view === "module" ? params.id : undefined);
  return <ApplicationScope application={application} route={route}>{children}</ApplicationScope>;
}
const publishing = new Set(["release-review", "release-history"]);
function AuthoringView({ route, children }: { route: Route; children: ReactNode }) {
  const host = useHost();
  const allowed = builder(host) || integrator(host) && route.view !== "project" && studioAreaForRoute(route) === "integration" || publisher(host) && publishing.has(route.view);
  return allowed ? <>{children}</> : <PageHeader title={t("Projects")} description={t("This authoring view is not available with your current access.")} />;
}
const contextualViews = (views: View[]) => views.map(view => ({ ...view, render: (params: Record<string, string>) => {
  const route = { view: view.id, params };
  return <AuthoringView route={route}>{referenceContext(route, view.render(params))}</AuthoringView>;
} }));

type Link = { label: string; icon: ReactNode; view: string; params?: Record<string, string> };
const links = (route: Route | undefined, items: Link[]): NavItem[] => items.map(item => ({ label: item.label, icon: item.icon, route: { view: item.view, params: { ...item.params, ...(route?.params?.application ? { application: route.params.application } : {}) } } }));
const section = (label: string, route: Route | undefined, items: Link[]): NavSection => ({ label, items: links(route, items) });
const navigation = (route?: Route) => <ToolContext route={route} />;

export default defineApp({
  id: "build", category: "build", title: t("Files"), icon: <FolderTree />,
  description: t("Projects and the resources they own: object types, logic, pages and releases."),
  for: builder,
  home: { view: "projects" },
  views: contextualViews(projectViews),
  opens: { "build.app": "project" },
  nav: () => [],
});

const ontology = defineApp({
  id: "ontology", surface: "studio", category: "build", title: t("Ontology Manager"), icon: <Boxes />, referenceContext,
  description: t("Object types, links, interfaces, shared properties, value types and action types of the tenant ontology."),
  for: builder,
  home: { view: "object-type" },
  views: contextualViews(ontologyViews),
  opens: { "build.object": "object-type", "build.linktype": "link-type", "build.propertytype": "property-type", "build.valuetype": "value-type" },
  navigation,
  nav: (_host, route) => [
    section(t("Ontology"), route, [{ label: t("Overview"), icon: <Network />, view: "discover" }]),
    section(t("Semantic"), route, [
      { label: t("Object types"), icon: <Boxes />, view: "object-type" },
      { label: t("Link types"), icon: <Link2 />, view: "link-type" },
      { label: t("Interfaces"), icon: <Layers />, view: "interfaces" },
      { label: t("Shared properties"), icon: <Tags />, view: "property-type" },
      { label: t("Value types"), icon: <Ruler />, view: "value-type" },
    ]),
    section(t("Kinetic"), route, [{ label: t("Action types"), icon: <Zap />, view: "action-type" }]),
    section(t("Changes"), route, [{ label: t("Branches and proposals"), icon: <GitBranch />, view: "changes" }]),
  ],
});

const connection = defineApp({
  id: "data-connection", category: "build", title: t("Data Connection"), icon: <Database />,
  description: t("Connections, syncs and streams that bring data in, and exports, endpoints and webhooks that carry it out."),
  for: host => builder(host) || integrator(host),
  home: { view: "connection" },
  views: contextualViews(connectionViews),
  opens: { "build.connection": "connection", "build.source": "data-source", "build.observation": "observation-source", "build.writeback": "writeback" },
  navigation,
  nav: (_host, route) => [
    section(t("Sources"), route, [
      { label: t("Connections"), icon: <Plug />, view: "connection" },
      { label: t("Data sources"), icon: <Database />, view: "data-source" },
    ]),
    section(t("Streams"), route, [{ label: t("Observation sources"), icon: <Radio />, view: "observation-source" }]),
    section(t("Exports"), route, [{ label: t("Writebacks"), icon: <Link2 />, view: "writeback" }]),
  ],
});

const pipelineBuilder = defineApp({
  id: "pipeline-builder", category: "build", title: t("Pipeline Builder"), icon: <Workflow />,
  description: t("Pipelines that transform datasets, and matching rules that resolve records into objects."),
  for: builder,
  home: { view: "pipeline" },
  views: contextualViews(pipelineViews),
  opens: { "build.pipeline": "pipeline", "build.match": "match" },
  navigation,
  nav: (_host, route) => [section(t("Transform"), route, [
    { label: t("Pipelines"), icon: <Workflow />, view: "pipeline" },
    { label: t("Matching rules"), icon: <GitMerge />, view: "match" },
  ])],
});

const datasetPreview = defineApp({
  id: "dataset-preview", category: "build", title: t("Dataset Preview"), icon: <TableProperties />,
  description: t("Datasets: schema, rows and the object types they back."),
  for: builder,
  home: { view: "dataset" },
  views: contextualViews(datasetViews),
  opens: { "build.dataset": "dataset" },
  navigation,
  nav: (_host, route) => [section(t("Datasets"), route, [{ label: t("Datasets"), icon: <TableProperties />, view: "dataset" }])],
});

const monitoring = defineApp({
  id: "monitoring", category: "govern", title: t("Monitoring"), icon: <Activity />,
  description: t("Health, runs, queued and failed work, and every delivery the host attempted."),
  for: host => builder(host) || integrator(host),
  home: { view: "integration-health" },
  views: contextualViews(monitoringViews),
  nav: (_host, route) => [section(t("Monitoring"), route, [{ label: t("Integration health"), icon: <Activity />, view: "integration-health" }])],
});

const logic = defineApp({
  id: "logic", category: "build", title: t("Logic"), icon: <Braces />,
  description: t("Functions that compute — code, decision tables, AI and flows — and the queries they read."),
  for: builder,
  home: { view: "functions" },
  views: contextualViews(logicViews),
  opens: { "build.process": "flow", "build.function": "function", "build.code": "code", "build.query": "query" },
  navigation,
  nav: (_host, route) => [section(t("Logic"), route, [
    { label: t("Functions"), icon: <Braces />, view: "functions" },
    { label: t("Queries"), icon: <Search />, view: "query" },
  ])],
});

const automate = defineApp({
  id: "automate", category: "build", title: t("Automate"), icon: <Zap />,
  description: t("Triggers that run actions and functions on events, schedules and conditions."),
  for: builder,
  home: { view: "automation" },
  views: contextualViews(automateViews),
  opens: { "build.alertrule": "alert" },
  navigation,
  nav: (_host, route) => [section(t("Automate"), route, [
    { label: t("Automations"), icon: <Zap />, view: "automation" },
    { label: t("Alert rules"), icon: <BellRing />, view: "alert" },
  ])],
});

const workshop = defineApp({
  id: "workshop", category: "build", title: t("Workshop"), icon: <PanelsTopLeft />,
  description: t("Pages, navigation and headers of applications, built from the ontology."),
  for: builder,
  home: { view: "module" },
  views: contextualViews(workshopViews),
  opens: { "build.page": "module" },
  navigation,
  nav: (_host, route) => [section(t("Workshop"), route, [
    { label: t("Modules"), icon: <PanelsTopLeft />, view: "module" },
    { label: t("Templates"), icon: <LayoutTemplate />, view: "studio-templates" },
  ])],
});

const devops = defineApp({
  id: "devops", category: "govern", title: t("DevOps"), icon: <Rocket />,
  description: t("Package project resources into product versions: test, review and activate releases."),
  for: host => builder(host) || publisher(host),
  home: { view: "release-review" },
  views: contextualViews(releaseViews),
  navigation,
  nav: (host, route) => [
    section(t("Releases"), route, [
      { label: t("Release review"), icon: <Rocket />, view: "release-review" },
      { label: t("Release history"), icon: <History />, view: "release-history" },
    ]),
    ...(builder(host) ? [section(t("Verification"), route, [{ label: t("Business test plans"), icon: <ClipboardCheck />, view: "test-plan" }])] : []),
  ],
});

const agentDefinitions = defineApp({
  id: "agents", category: "build", title: t("Agent Studio"), icon: <Bot />,
  description: t("AI agents, what they are allowed to do, their runs and evaluations."),
  for: builder,
  home: { view: "agent" },
  views: contextualViews(agentViews),
  opens: { "build.agent": "agent" },
  navigation,
  nav: (_host, route) => [section(t("Build"), route, [{ label: t("Agent definitions"), icon: <Shapes />, view: "agent" }])],
});

export const contributions = [ontology, connection, pipelineBuilder, datasetPreview, logic, automate, workshop, agentDefinitions, devops, monitoring];
