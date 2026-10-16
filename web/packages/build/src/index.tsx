import { ApplicationSampleAsset } from "./releases/samples";
// ADR-0108: one application context, five workspaces, original editors.
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
import { ObservationSourceEditor, ObservationSources } from "./ontology/observation";
import { DataSourceEditor, DataSources } from "./ontology/data-source";
import { ConnectionEditor, Connections } from "./ontology/connection";
import { DatasetEditor, Datasets } from "./ontology/dataset";
import { PipelineEditor, Pipelines } from "./ontology/pipeline";
import { WritebackEditor, Writebacks } from "./ontology/writeback";
import { MatchEditor, Matches } from "./ontology/match";
import { IntegrationHealth } from "./ontology/integration-health";
import { DecisionTableEditor, DecisionTables } from "./functions/decision-table";
import { QueryEditor, Queries } from "./ontology/query";
import { FunctionEditor, Functions } from "./functions/function";
import { CodeEditor, CodeFunctions } from "./functions/code";
import { AgentEditor, Agents } from "./functions/agent";
import { AlertEditor, Alerts } from "./functions/alert";
import { ReleaseReview, releaseDraftsSelection, releaseKinds, type ReleaseKind } from "./releases/release";
import { Changes } from "./releases/changes";
import { CandidateTest } from "./releases/simulate";
import { ProjectHome, ProjectsList } from "./projects/project";
import { StudioTemplates } from "./workshop/template-ui";
import { t, type NavSection, type Route, type View } from "@platform/ui";
import type { ReactNode } from "react";
import { ApplicationScope } from "./projects/application-scope";
import { StudioNavigation } from "./projects/studio-navigation";
import { studioApplicationID } from "./projects/studio-model";
import { Activity, BookOpen, Database, Fingerprint, FolderKanban, GitMerge, Hammer, Layers, Plug, Send } from "lucide-react";

const builder = (host: Host) => host.role("build") === "builder";
// An integrator connects, maps and runs integrations without designing objects or pages (ADR-0075).
const integrator = (host: Host) => host.role("build") === "integrator";
const integration = (): NavSection => ({ label: t("Integration"), items: [
  { label: t("Connections"), icon: <Plug />, route: { view: "connection" } },
  { label: t("Observation sources"), icon: <Activity />, route: { view: "observation-source" } },
  { label: t("Data sources"), icon: <Database />, route: { view: "data-source" } },
  { label: t("Datasets"), icon: <Layers />, route: { view: "dataset" } },
  { label: t("Pipelines"), icon: <GitMerge />, route: { view: "pipeline" } },
  { label: t("Writebacks"), icon: <Send />, route: { view: "writeback" } },
  { label: t("Matching rules"), icon: <Fingerprint />, route: { view: "match" } },
  { label: t("Integration health"), icon: <Activity />, route: { view: "integration-health" } },
] });

const editorViews: View[] = [
  { id: "projects", title: () => t("Applications"), render: () => <ProjectsList /> },
  { id: "project", documentKey: p => p.id ?? "", title: () => t("Application Studio"), render: p => <ProjectHome key={p.id} id={p.id ?? ""} area={p.area} initialSelected={p.asset} initialSection={p.section} initialPlan={p.plan} /> },
  { id: "object-type", title: p => p.id ? t("Object type") : t("Model"), render: p => p.id ? <ObjectTypeEditor key={p.id} id={p.id} initialField={p.field} initialAction={p.action} initialAccess={p.access === "true"} initialTab={p.tab} /> : <ModelWorkbench key={p.object ?? "catalog"} initialObject={p.object} initialTab={p.tab} /> },
  { id: "action-type", title: () => t("Action type"), render: p => p.id ? <ActionTypeEditor key={p.id} id={p.id} action={p.action} /> : <ActionTypes /> },
  { id: "link-type", title: () => t("Relationships"), render: p => p.id ? <LinkTypeEditor key={p.id} id={p.id} parent={p.parent} child={p.child} via={p.via} /> : <LinkTypes /> },
  { id: "property-type", title: () => t("Shared properties"), render: p => p.id ? <PropertyTypeEditor key={p.id} id={p.id} /> : <PropertyTypes /> },
  { id: "query", title: () => t("Queries"), render: p => p.id ? <QueryEditor key={p.id} id={p.id} /> : <Queries /> },
  { id: "module", title: p => p.page ? t("Page") : t("Application interface"), render: p => <ModuleWorkbench key={`${p.id ?? ""}:${p.page ?? ""}`} id={p.id} page={p.page} focus={p.focus} /> },
  { id: "studio-templates", title: () => t("Templates"), render: p => <StudioTemplates key={p.template ?? "templates"} initial={p.template} /> },
  { id: "automation", title: () => t("Automations"), render: p => p.id ? <AutomationEditor key={p.id} id={p.id} /> : <Automations /> },
  { id: "flow", title: () => t("Flows"), render: p => p.id ? <FlowEditor key={p.id} id={p.id} /> : <Flows /> },
  { id: "runs", title: () => t("Runs"), render: p => <WorkflowRuns name={p.name ?? ""} onStepSelect={() => {}} /> },
  { id: "connection", title: () => t("Connections"), render: p => p.id ? <ConnectionEditor key={p.id} id={p.id} /> : <Connections /> },
  { id: "observation-source", title: () => t("Observation sources"), render: p => p.id ? <ObservationSourceEditor key={p.id} id={p.id} /> : <ObservationSources /> },
  { id: "data-source", title: () => t("Data sources"), render: p => p.id ? <DataSourceEditor key={p.id} id={p.id} /> : <DataSources /> },
  { id: "dataset", title: () => t("Datasets"), render: p => p.id ? <DatasetEditor key={p.id} id={p.id} /> : <Datasets /> },
  { id: "pipeline", title: () => t("Pipelines"), render: p => p.id ? <PipelineEditor key={p.id} id={p.id} /> : <Pipelines /> },
  { id: "writeback", title: () => t("Writebacks"), render: p => p.id ? <WritebackEditor key={p.id} id={p.id} /> : <Writebacks /> },
  { id: "match", title: () => t("Matching rules"), render: p => p.id ? <MatchEditor key={p.id} id={p.id} /> : <Matches /> },
  { id: "integration-health", title: () => t("Integration health"), render: () => <IntegrationHealth /> },
  { id: "function", title: () => t("AI functions"), render: p => p.id ? <FunctionEditor key={p.id} id={p.id} /> : <Functions /> },
  { id: "decision-table", title: () => t("Decision tables"), render: p => p.id ? <DecisionTableEditor key={p.id} id={p.id} /> : <DecisionTables /> },
  { id: "code", title: () => t("Code functions"), render: p => p.id ? <CodeEditor key={p.id} id={p.id} /> : <CodeFunctions /> },
  { id: "agent", title: () => t("Agents"), render: p => p.id ? <AgentEditor key={p.id} id={p.id} /> : <Agents /> },
  { id: "alert", title: () => t("Alert rules"), render: p => p.id ? <AlertEditor key={p.id} id={p.id} /> : <Alerts /> },
  {id:"sample-set",title:()=>t("Sample set"),render:p=><ApplicationSampleAsset id={p.id}/>},
  { id: "evaluation-set", title: () => t("Evaluation set"), render: p => <CandidateTest key={p.id ?? "new"} planId={p.id} /> },
  { id: "test-plan", title: () => t("Business test plan"), render: p => <CandidateTest key={p.id ?? "new"} planId={p.id} applicationId={p.application} /> },
  { id: "changes", title: () => t("Changes"), render: p => <Changes key={`${p.kind ?? ""}:${p.id ?? ""}`} initialKind={p.kind} initialID={p.id} /> },
  { id: "release-review", title: () => t("Release review"), render: p => {
    const selection = releaseDraftsSelection(p.drafts, releaseKinds);
    return <ReleaseReview key={`${p.kind}:${p.id}:${p.drafts ?? ""}`} initialKind={releaseKinds.includes(p.kind as ReleaseKind) ? p.kind as ReleaseKind : "object"} initialID={p.id} initialDrafts={selection.drafts} selectionError={selection.error} />;
  } },
  { id: "release-history", title: () => t("Release history"), render: () => <ReleaseReview key="history" /> },
];
function referenceContext(route: Route, children: ReactNode) {
  const params = route.params ?? {};
  const application = studioApplicationID(route) ?? (route.view === "module" ? params.id : undefined);
  return <ApplicationScope application={application} route={route}>{children}</ApplicationScope>;
}
const views = editorViews.map(view => ({ ...view, render: (params: Record<string, string>) => referenceContext({ view: view.id, params }, view.render(params)) }));

/** Build owns the contextual navigation; the Shell retains all editor tabs. */
export default defineApp({
  id: "build", surface: "studio", category: "build", title: t("Builder"),
  referenceContext,
  description: t("Build, verify and deliver one business application."), icon: <Hammer />,
  home: { view: "projects" }, views,
  opens: { "build.app": "project", "build.object": "object-type", "build.linktype": "link-type", "build.propertytype": "property-type", "build.query": "query", "build.page": "module", "build.process": "flow", "build.function": "function", "build.code": "code", "build.agent": "agent", "build.alertrule": "alert", "build.observation": "observation-source" },
  navigation: route => <StudioNavigation route={route} />,
  nav: (host, route): NavSection[] => {
    if (integrator(host)) return [integration()];
    if (!builder(host)) return [{ label: t("Verify"), items: [
      { label: t("Changes"), route: { view: "changes" } },
      { label: t("Release history"), route: { view: "release-history" } },
    ] }];
    const application = studioApplicationID(route);
    return [
      { label: t("Applications"), items: [{ label: t("All applications"), icon: <FolderKanban />, route: { view: "projects" } }] },
      { label: t("Shared rules"), items: [
        { label: t("Decision tables"), route: { view: "decision-table" } },
        { label: t("Automations"), route: { view: "automation" } },
        { label: t("Alert rules"), route: { view: "alert" } },
      ] },
      { label: t("Delivery"), items: [
        { label: t("Integration health"), route: { view: "integration-health", params: application ? { application } : undefined } },
        { label: t("Release history"), route: { view: "release-history", params: application ? { application } : undefined } },
      ] },
      { label: t("Explore"), items: [{ label: t("Asset Library"), icon: <BookOpen />, route: { view: "catalog" } }] },
    ];
  },
});
