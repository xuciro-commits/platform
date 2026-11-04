import { ApplicationSampleAsset } from "./releases/samples";
import "./i18n";
import { Glossary, Knowledge } from "./agents/knowledge";
import { Files, ResourceIdentity } from "./projects/Files";
import { defineApp, useHost, withApp, type AppUI, type Host } from "@platform/app";
import { ModuleWorkbench } from "./workshop/ModuleWorkbench";
import { ObjectViews } from "./workshop/ObjectViews";
import { ObjectTypeEditor } from "./ontology/process";
import { ActionTypeEditor, ActionTypes } from "./ontology/action-type";
import { ModelWorkbench } from "./ontology/ModelWorkbench";
import { FlowEditor } from "./automate/workflow";
import { AutomationEditor, Automations } from "./automate/automation";
import { WorkflowRuns } from "./automate/workflow-runs";
import { LinkTypeEditor, LinkTypes } from "./ontology/link-type";
import { PropertyTypeEditor, PropertyTypes } from "./ontology/property-type";
import { ValueTypeEditor, ValueTypes } from "./ontology/value-type";
import { ObjectTypeGroupEditor, ObjectTypeGroups } from "./ontology/object-type-group";
import { ObservationSourceEditor, ObservationSources } from "./ontology/observation";
import { DataSourceEditor, DataSources, Streams } from "./ontology/data-source";
import { ListenerEditor, Listeners } from "./ontology/listener";
import { ConnectionEditor, Connections } from "./ontology/connection";
import { DatasetEditor, Datasets } from "./ontology/dataset";
import { PipelineEditor, Pipelines } from "./ontology/pipeline";
import { ScheduleEditor, Schedules } from "./ontology/schedule";
import { MediaSetEditor, MediaSets } from "./ontology/media-set";
import { WritebackEditor, Writebacks } from "./ontology/writeback";
import { MatchEditor, Matches } from "./ontology/match";
import { IntegrationHealth } from "./ontology/integration-health";
import { DecisionTableEditor } from "./functions/decision-table";
import { SavedObjectSetEditor, SavedObjectSets } from "./explorer/object-sets";
import { FunctionEditor } from "./functions/function";
import { CodeEditor } from "./functions/code";
import { AgentEditor, Agents } from "./functions/agent";
import { ReleaseReview, releaseDraftsSelection, releaseKinds, type ReleaseKind } from "./releases/release";
import { Branches } from "./branches/Branches";
import { ProposalScreen } from "./branches/Proposal";
import { CandidateTest } from "./releases/simulate";
import { ProjectHome } from "./projects/project";
import { Discover } from "./projects/discover";
import { StudioTemplates } from "./workshop/template-ui";
import { PageHeader, t, type NavItem, type NavSection, type Route, type View } from "@platform/ui";
import type { ReactNode } from "react";
import { ApplicationScope } from "./projects/application-scope";
import { ToolContext } from "./projects/tool-context";
import { studioApplicationID, studioAreaForRoute } from "./projects/studio-model";
import { FunctionCatalog } from "./functions/catalog";
import { Interfaces } from "./ontology/ModelWorkbench";
import { Activity, Bot, Boxes, Braces, CalendarClock, ClipboardCheck, Clock, Code, Database, FolderTree, GitBranch, GitMerge, Layers, LayoutTemplate, Link2, Network, PanelsTopLeft, Plug, Radio, Rocket, Ruler, Search, Shapes, Sparkles, Tags, TableProperties, Workflow, Zap } from "lucide-react";

const builder = (host: Host) => host.holds("build", "builder");
const integrator = (host: Host) => host.holds("build", "integrator");
const publisher = (host: Host) => host.holds("build", "publisher");

const view = (id: string, title: () => string, render: View["render"], documentKey?: View["documentKey"]): View => ({ id, title: () => title(), render, ...(documentKey ? { documentKey } : {}) });
const projectViews: View[] = [
  { id: "projects", title: () => t("Files"), render: () => <Files /> },
 { id:"files",title:()=>t("Files"),render:p=><Files project={p.id} folder={p.folder}/> },
  { id: "project", documentKey: p => p.id ?? "", title: () => t("Project"), render: p => <ProjectHome key={p.id} id={p.id ?? ""} area={p.area} initialSelected={p.asset} initialSection={p.section} initialPlan={p.plan} initialTemplate={p.template} /> },
];
const ontologyViews: View[] = [
  view("discover", () => t("Overview"), () => <Discover />),
  { id: "object-type", documentKey: p => p.id ?? p.object ?? "catalog", title: p => p.id || p.object ? t("Object type") : t("Object types"), render: p => p.id ? <ObjectTypeEditor key={p.id} id={p.id} initialField={p.field} initialAction={p.action} initialAccess={p.access === "true"} initialTab={p.tab} /> : <ModelWorkbench key={p.object ?? "catalog"} initialObject={p.object} initialTab={p.tab} /> },
  view("link-type", () => t("Link types"), p => p.id ? <LinkTypeEditor key={p.id} id={p.id} parent={p.parent} child={p.child} via={p.via} /> : <LinkTypes />),
  view("interfaces", () => t("Interfaces"), () => <Interfaces />),
  view("property-type", () => t("Shared properties"), p => p.id ? <PropertyTypeEditor key={p.id} id={p.id} /> : <PropertyTypes />),
  view("value-type", () => t("Value types"), p => p.id ? <ValueTypeEditor key={p.id} id={p.id} /> : <ValueTypes />),
  view("object-type-group", () => t("Object type groups"), p => p.id ? <ObjectTypeGroupEditor key={p.id} id={p.id} /> : <ObjectTypeGroups />),
  view("action-type", () => t("Action types"), p => p.id ? <ActionTypeEditor key={p.id} id={p.id} action={p.action} object={p.object} /> : <ActionTypes />),
  view("changes", () => t("Branches and proposals"), () => <Branches />),
  { id: "proposal", documentKey: p => p.id ?? "", title: () => t("Proposal"), render: p => <ProposalScreen key={p.id} id={p.id ?? ""} /> },
];
const connectionViews: View[] = [
  view("streams", () => t("Streams"), () => <Streams />),
  view("stream", () => t("Stream"), p => <DataSourceEditor key={p.id} id={p.id ?? "new"} initialStream />),
  view("connection", () => t("Connections"), p => p.id ? <ConnectionEditor key={p.id} id={p.id} /> : <Connections />),
  view("data-source", () => t("Data sources"), p => p.id ? <DataSourceEditor key={p.id} id={p.id} /> : <DataSources />),
  view("observation-source", () => t("Observation sources"), p => p.id ? <ObservationSourceEditor key={p.id} id={p.id} /> : <ObservationSources />),
  view("listener", () => t("Listeners"), p => p.id ? <ListenerEditor key={p.id} id={p.id} /> : <Listeners />),
  view("writeback", () => t("Writebacks"), p => p.id ? <WritebackEditor key={p.id} id={p.id} /> : <Writebacks />),
];
const pipelineViews: View[] = [
  view("pipeline", () => t("Pipelines"), p => p.id ? <PipelineEditor key={p.id} id={p.id} /> : <Pipelines />),
  view("schedules", () => t("Schedules"), p => <Schedules pipeline={p.pipeline} />),
  view("schedule", () => t("Schedule"), p => <ScheduleEditor key={p.id} id={p.id ?? "new"} />),
  view("match", () => t("Matching rules"), p => p.id ? <MatchEditor key={p.id} id={p.id} /> : <Matches />),
];
const datasetViews: View[] = [
  view("media-set", () => t("Media sets"), p => p.id ? <MediaSetEditor key={p.id} id={p.id} /> : <MediaSets />),
  view("dataset", () => t("Datasets"), p => p.id ? <DatasetEditor key={p.id} id={p.id} /> : <Datasets />),
];
const monitoringViews: View[] = [
  view("integration-health", () => t("Integration health"), () => <IntegrationHealth />),
];
const logicViews: View[] = [
  view("functions", () => t("Functions"), () => <FunctionCatalog />),
  view("code-functions", () => t("Code functions"), () => <FunctionCatalog engine="code" />),
  view("decision-tables", () => t("Decision tables"), () => <FunctionCatalog engine="table" />),
  view("ai-functions", () => t("AI functions"), () => <FunctionCatalog engine="ai" />),
  view("logic-flows", () => t("Logic flows"), () => <FunctionCatalog engine="flow" />),
  view("code", () => t("Code function"), p => p.id ? <CodeEditor key={p.id} id={p.id} /> : <FunctionCatalog />),
  view("decision-table", () => t("Decision table"), p => p.id ? <DecisionTableEditor key={p.id} id={p.id} /> : <FunctionCatalog />),
  view("function", () => t("AI function"), p => p.id ? <FunctionEditor key={p.id} id={p.id} /> : <FunctionCatalog />),
  view("flow", () => t("Logic flow"), p => p.id ? <FlowEditor key={p.id} id={p.id} /> : <FunctionCatalog />),
];
const automateViews: View[] = [
  view("automation", () => t("Automations"), p => p.id ? <AutomationEditor key={p.id} id={p.id} /> : <Automations />),
  view("alert", () => t("Automations"), p => p.id ? <AutomationEditor key={p.id} id={`alert-${p.id}`} /> : <Automations />),
  view("runs", () => t("Runs"), p => <WorkflowRuns name={p.name ?? ""} initialRun={p.run ?? p.id} onStepSelect={() => {}} />),
];
const workshopViews: View[] = [
  view("object-views", () => t("Object views"), () => <ObjectViews />),
  view("module", () => t("Workshop"), p => <ModuleWorkbench key={`${p.id ?? ""}:${p.page ?? ""}`} id={p.id} page={p.page} focus={p.focus} />),
  view("studio-templates", () => t("Templates"), p => <StudioTemplates key={p.template ?? "templates"} initial={p.template} />),
];
const releaseViews: View[] = [
  { id: "release-review", title: () => t("Releases"), render: p => {
    const selection = releaseDraftsSelection(p.drafts, releaseKinds);
    return <ReleaseReview key={`${p.kind}:${p.id}:${p.drafts ?? ""}:${p.candidate ?? ""}`} initialKind={releaseKinds.includes(p.kind as ReleaseKind) ? p.kind as ReleaseKind : "object"} initialID={p.id} initialDrafts={selection.drafts} selectionError={selection.error} candidateID={p.candidate} applicationName={p.applicationName} />;
  } },
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
  return <ApplicationScope application={application}>{children}</ApplicationScope>;
}
const publishing = new Set(["release-review"]);
function AuthoringView({ route, children }: { route: Route; children: ReactNode }) {
  const host = useHost();
  const allowed = ["files","projects"].includes(route.view) || host.holds("build","viewer") || builder(host) || integrator(host) && route.view !== "project" && studioAreaForRoute(route) === "integration" || publisher(host) && publishing.has(route.view);
  return allowed ? <>{children}</> : <PageHeader title={t("Projects")} description={t("This authoring view is not available with your current access.")} />;
}
const contextualViews = (views: View[]) => views.map(view => ({ ...view, render: (params: Record<string, string>) => {
  const route = { view: view.id, params };
  return <AuthoringView route={route}>{referenceContext(route, <ResourceIdentity view={view} params={params}/>)}</AuthoringView>;
} }));

type Link = { label: string; icon: ReactNode; view: string; params?: Record<string, string> };
const links = (route: Route | undefined, items: Link[]): NavItem[] => items.map(item => ({ label: item.label, icon: item.icon, route: { view: item.view, params: { ...item.params, ...(route?.params?.application ? { application: route.params.application } : {}) } } }));
const section = (label: string, route: Route | undefined, items: Link[]): NavSection => ({ label, items: links(route, items) });
const navigation = (route?: Route) => <ToolContext route={route} />;

export default defineApp({
  id: "build", category: "build", portal: false, title: "Files", icon: <FolderTree />,
  description: "Projects and the resources they own: object types, logic, pages and releases.",
  for: host => builder(host) || host.holds("build","viewer") || host.holds("build","discoverer"),
  home: { view: "files" },
  views: contextualViews(projectViews),
  opens: { "build.app": "project" },
  navigation: route => route?.view === "project" ? navigation(route) : undefined,
  nav: () => [],
});

const ontology = defineApp({
  id: "ontology", surface: "studio", category: "build", title: "Ontology Manager", icon: <Boxes />, referenceContext,
  description: "Object types, links, interfaces, shared properties, value types and action types of the tenant ontology.",
  for: builder,
  home: { view: "object-type" },
  views: contextualViews(ontologyViews),
  opens: { "build.actiontype": "action-type", "build.object": "object-type", "build.linktype": "link-type", "build.propertytype": "property-type", "build.valuetype": "value-type", "build.objecttypegroup": "object-type-group" },
  navigation,
  nav: (_host, route) => [
    section(t("Ontology"), route, [{ label: t("Overview"), icon: <Network />, view: "discover" }]),
    section(t("Semantic"), route, [
      { label: t("Object types"), icon: <Boxes />, view: "object-type" },
      { label: t("Link types"), icon: <Link2 />, view: "link-type" },
      { label: t("Interfaces"), icon: <Layers />, view: "interfaces" },
      { label: t("Shared properties"), icon: <Tags />, view: "property-type" },
      { label: t("Value types"), icon: <Ruler />, view: "value-type" },
      { label: t("Object type groups"), icon: <Shapes />, view: "object-type-group" },
    ]),
    section(t("Kinetic"), route, [{ label: t("Action types"), icon: <Zap />, view: "action-type" }]),
    section(t("Changes"), route, [{ label: t("Branches and proposals"), icon: <GitBranch />, view: "changes" }]),
  ],
});

export function createDataConnection(host: Host, operations: { views: View[]; streamLinks: () => NavItem[]; webhookLinks: (host: Host) => NavItem[] }): AppUI {
  const authoring = builder(host) || integrator(host);
  return defineApp({
    id: "data-connection", serves: ["build", "platform"], category: "build", title: "Data Connection", icon: <Database />,
    description: "Connections, syncs and streams that bring data in, and exports, endpoints and webhooks that carry it out.",
    for: host => builder(host) || integrator(host) || host.holds("platform", "admin"),
    home: { view: authoring ? "connection" : "endpoints" },
    views: [...(authoring ? contextualViews(connectionViews) : []), ...operations.views],
    opens: authoring ? { "build.connection": "connection", "build.source": "data-source", "build.observation": "observation-source", "build.listener": "listener", "build.writeback": "writeback" } : {},
    navigation: authoring ? navigation : undefined,
    nav: (host, route) => [
      ...(authoring ? [section(t("Sources"), route, [
        { label: t("Connections"), icon: <Plug />, view: "connection" },
        { label: t("Syncs"), icon: <Database />, view: "data-source" },
      ])] : []),
      { label: t("Streams"), items: authoring ? links(route, [{ label: t("Streams"), icon: <Radio />, view: "streams" }, { label: t("Observation sources"), icon: <Radio />, view: "observation-source" }]) : operations.streamLinks() },
      ...(authoring ? [section(t("Exports"), route, [{ label: t("Writebacks"), icon: <Link2 />, view: "writeback" }])] : []),
      ...(authoring ? [section(t("Listeners"), route, [{ label: t("Listeners"), icon: <Radio />, view: "listener" }])] : []),
      { label: t("Webhooks"), items: operations.webhookLinks(host) },
    ].filter(section => section.items.length > 0),
  });
}

const pipelineBuilder = defineApp({
  id: "pipeline-builder", category: "build", title: "Pipeline Builder", icon: <Workflow />,
  description: "Pipelines that transform datasets, and matching rules that resolve records into objects.",
  for: builder,
  home: { view: "pipeline" },
  views: contextualViews(pipelineViews),
  opens: { "build.pipeline": "pipeline", "build.schedule": "schedule", "build.match": "match" },
  navigation,
  nav: (_host, route) => [section(t("Transform"), route, [
    { label: t("Pipelines"), icon: <Workflow />, view: "pipeline" },
    { label: t("Schedules"), icon: <CalendarClock />, view: "schedules" },
    { label: t("Matching rules"), icon: <GitMerge />, view: "match" },
  ])],
});

const datasetPreview = defineApp({
  id: "dataset-preview", category: "build", title: "Dataset Preview", icon: <TableProperties />,
  description: "Datasets: schema, rows and the object types they back.",
  for: builder,
  home: { view: "dataset" },
  views: contextualViews(datasetViews),
  opens: { "build.dataset": "dataset", "build.mediaset": "media-set" },
  navigation,
  nav: (_host, route) => [section(t("Datasets"), route, [{ label: t("Datasets"), icon: <TableProperties />, view: "dataset" }, { label: t("Media sets"), icon: <Layers />, view: "media-set" }])],
});

export const integrationMonitoring: Pick<AppUI, "for" | "views" | "nav"> = {
  for: host => builder(host) || integrator(host),
  views: contextualViews(monitoringViews),
  nav: (_host, route) => [section(t("Monitoring"), route, [{ label: t("Integration health"), icon: <Activity />, view: "integration-health" }])],
};

const logic = defineApp({
  id: "logic", category: "build", title: "Logic", icon: <Braces />,
  description: "Functions that compute — code, decision tables, AI and flows — and the queries they read.",
  for: builder,
  home: { view: "functions" },
  views: contextualViews(logicViews),
  opens: { "build.table": "decision-table", "build.process": "flow", "build.function": "function", "build.code": "code" },
  navigation,
  nav: (_host, route) => [section(t("Logic"), route, [
    { label: t("Functions"), icon: <Braces />, view: "functions" },
    { label: t("Code functions"), icon: <Code />, view: "code-functions" },
    { label: t("Decision tables"), icon: <TableProperties />, view: "decision-tables" },
    { label: t("AI functions"), icon: <Sparkles />, view: "ai-functions" },
    { label: t("Logic flows"), icon: <Workflow />, view: "logic-flows" },
  ])],
});

const automate = defineApp({
  id: "automate", category: "build", title: "Automate", icon: <Zap />,
  description: "Triggers that run actions and functions on events, schedules and conditions.",
  for: builder,
  home: { view: "automation" },
  views: contextualViews(automateViews),
  opens: { "build.process/automation": "automation", "build.alertrule": "alert" },
  navigation,
  nav: (_host, route) => [section(t("Automate"), route, [
    { label: t("Automations"), icon: <Zap />, view: "automation" },
    { label: t("Runs"), icon: <Clock />, view: "runs" },
  ])],
});

const workshop = defineApp({
  id: "workshop", category: "build", title: "Workshop", icon: <PanelsTopLeft />,
  description: "Pages, navigation and headers of applications, built from the ontology.",
  for: builder,
  home: { view: "module" },
  views: contextualViews(workshopViews),
  opens: { "build.page": "module" },
  navigation,
  nav: (_host, route) => [section(t("Workshop"), route, [
    { label: t("Modules"), icon: <PanelsTopLeft />, view: "module" },
    { label: t("Object views"), icon: <Shapes />, view: "object-views" },
    { label: t("Templates"), icon: <LayoutTemplate />, view: "studio-templates" },
  ])],
});

const devops = defineApp({
  id: "devops", category: "govern", title: "DevOps", icon: <Rocket />,
  description: "Package project resources into product versions: test, review and activate releases.",
  for: host => builder(host) || publisher(host),
  home: { view: "release-review" },
  views: contextualViews(releaseViews),
  opens: { "build.testplan": "test-plan", "build.testplan/evaluation-set": "evaluation-set", "build.sample-set": "sample-set" },
  navigation,
  nav: (host, route) => [
    section(t("Releases"), route, [
      { label: t("Releases"), icon: <Rocket />, view: "release-review" },
    ]),
    ...(builder(host) ? [section(t("Verification"), route, [{ label: t("Business test plans"), icon: <ClipboardCheck />, view: "test-plan" }])] : []),
  ],
});

export function createAgentStudio(host: Host, operations: Pick<AppUI, "for" | "views" | "nav">): AppUI {
  const authoring = builder(host);
  const operating = !operations.for || operations.for(host);
  return defineApp({
    id: "agents", serves: ["build", "agent", "knowledge"], category: "build", title: "Agent Studio", icon: <Bot />,
    description: "AI agents, what they are allowed to do, their runs and evaluations.",
    for: host => builder(host) || !!host.role("knowledge") || !operations.for || operations.for(host),
    home: { view: operating ? "agents" : authoring ? "agent" : "knowledge" },
    views: [...(authoring ? contextualViews(agentViews) : []), ...(operating ? operations.views : []), view("knowledge",()=>t("Retrieval sources"),()=><Knowledge/>),view("glossary",()=>t("Glossary"),()=><Glossary/>)],
    opens: authoring ? { "build.agent": "agent" } : {},
    navigation: authoring ? navigation : undefined,
    nav: (host, route) => [
      ...(operating ? operations.nav(host, route) : []),
 ...(host.role("knowledge") ? [section(t("Retrieval"),route,[{label:t("Retrieval sources"),icon:<Database/>,view:"knowledge"},{label:t("Glossary"),icon:<Tags/>,view:"glossary"}])]:[]),
      ...(authoring ? [section(t("Build"), route, [{ label: t("Agent definitions"), icon: <Shapes />, view: "agent" }])] : []),
    ],
  });
}

export const contributions = [ontology, pipelineBuilder, datasetPreview, logic, automate, workshop, devops];

export function extendObjectExplorer(base: AppUI, host: Host): AppUI {
  if (!builder(host)) return base;
  return withApp(base, {
    views: [...base.views, ...contextualViews([view("object-set", () => t("Object sets"), params => params.id ? <SavedObjectSetEditor key={params.id} id={params.id} /> : <SavedObjectSets />)])],
    opens: { ...base.opens, "build.query": "object-set" },
    nav: (host, route) => [...base.nav(host, route), { label: t("Object sets"), items: [{ label: t("Saved object sets"), icon: <Search />, route: { view: "object-set" } }] }],
  });
}
