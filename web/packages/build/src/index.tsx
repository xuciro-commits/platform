// Studio owns asset design. Published business pages belong to the navigation
// explicitly chosen for an application (ADR-0036), not an automatic asset dump.
import "./i18n";
import { defineApp } from "@platform/app";
import { PageEditor, PagesList } from "./editor";
import { ProcessEditor, Objects } from "./process";
import { ModelWorkbench } from "./model-editor/ModelWorkbench";
import { WorkflowEditor, Workflows } from "./workflow";
import {LinkTypeEditor,LinkTypes} from "./link-type";
import {PropertyTypeEditor,PropertyTypes} from "./property-type";
import { QueryEditor, Queries } from "./query";
import { FunctionEditor, Functions } from "./function";
import { ApplicationEditor, Applications } from "./application";
import { CodeEditor, CodeFunctions } from "./code";
import { ReleaseReview, type ReleaseKind } from "./release";
import { CandidateTest } from "./simulate";
import { StudioOverview } from "./studio";
import { StudioTemplates } from "./template-ui";
import { t, type NavSection } from "@platform/ui";
import { ApplicationScope } from "./application-scope";
import { AppWindow, Boxes, Hammer, LayoutList } from "lucide-react";

export default defineApp({
  id: "build",
  surface: "studio",
  title: t("Application Studio"),
  icon: <Hammer />,
  home: { view: "applications" },
  views: [
    { id: "studio", title: () => t("Application Studio"), render: () => <StudioOverview /> },
    { id: "studio-templates", title: () => t("Studio templates"), render: (p) => <StudioTemplates key={p.template ?? "templates"} initial={p.template} /> },
    { id: "applications", title: () => t("Applications"), render: () => <Applications /> },
    { id: "application", title: () => t("Application design"), render: (p) => <ApplicationEditor key={p.id ?? "new"} id={p.id ?? "new"} /> },
    { id: "pages", title: () => t("Pages"), render: () => <PagesList /> },
    { id: "compose", title: () => t("Compose a page"), render: (p) => <ApplicationScope key={p.id ?? "new"} application={p.application}><PageEditor id={p.id ?? ""} /></ApplicationScope> },
    { id: "process", title: (p) => p.id ? t("Object design") : t("Objects"), render: (p) => p.id ? <ApplicationScope key={p.id} application={p.application}><ProcessEditor id={p.id} initialField={p.field} initialAction={p.action} initialAccess={p.access === "true"} /></ApplicationScope> : <Objects /> },
    { id: "model", title: () => t("Business model"), render: (p) => <ApplicationScope application={p.application}><ModelWorkbench key={p.object ?? "catalog"} initialObject={p.object} initialTab={p.tab} /></ApplicationScope> },
    { id: "workflow", title: () => t("Workflows"), render: (p) => p.id ? <ApplicationScope key={p.id} application={p.application}><WorkflowEditor id={p.id} /></ApplicationScope> : <Workflows /> },
    {id:"link-type",title:()=>t("Relationships"),render:p=>p.id?<ApplicationScope key={p.id} application={p.application}><LinkTypeEditor id={p.id} parent={p.parent} child={p.child} via={p.via}/></ApplicationScope>:<LinkTypes/>},
    {id:"property-type",title:()=>t("Shared properties"),render:p=>p.id?<ApplicationScope key={p.id} application={p.application}><PropertyTypeEditor id={p.id}/></ApplicationScope>:<PropertyTypes/>},
    { id: "query", title: () => t("Queries"), render: (p) => p.id ? <ApplicationScope key={p.id} application={p.application}><QueryEditor id={p.id} /></ApplicationScope> : <Queries /> },
    { id: "function", title: () => t("AI functions"), render: (p) => p.id ? <ApplicationScope key={p.id} application={p.application}><FunctionEditor id={p.id} /></ApplicationScope> : <Functions /> },
    { id: "code", title: () => t("Code functions"), render: (p) => p.id ? <ApplicationScope key={p.id} application={p.application}><CodeEditor id={p.id} /></ApplicationScope> : <CodeFunctions /> },
    { id: "release-review", title: () => t("Release review"), render: (p) => <ApplicationScope application={p.application}><ReleaseReview key={`${p.kind}:${p.id}`} initialKind={["object", "page", "app", "flow", "link-type", "property-type", "query", "function", "compute"].includes(p.kind ?? "") ? p.kind as ReleaseKind : "object"} initialID={p.id} /></ApplicationScope> },
    { id: "candidate-test", title: () => t("Test a candidate"), render: (p) => <ApplicationScope application={p.application}><CandidateTest key={p.functionId ?? p.processId ?? p.objectId ?? "object"} processId={p.processId} functionId={p.functionId} objectId={p.objectId} /></ApplicationScope> },
  ],
  opens: { "build.app": "application", "build.page": "compose", "build.object": "process", "build.process": "workflow", "build.linktype":"link-type", "build.propertytype":"property-type", "build.query": "query", "build.function": "function", "build.code": "code" }, // open a semantic asset in its editor
  nav: (host): NavSection[] => {
    const builder = host.role("build") === "builder";
    return [
      {
        label: t("Application Studio"),
        items: [
          { label: t("Applications"), icon: <AppWindow />, route: { view: "applications" } },
          { label: t("Shared resources"), icon: <Boxes />, route: { view: "studio" } },
          { label: t("Discover capabilities"), icon: <Boxes />, route: { view: "catalog", params: { mode: "builder", surface: "studio" } } },
          ...(builder ? [{ label: t("Studio templates"), icon: <LayoutList />, route: { view: "studio-templates" } }] : []),
        ],
      },
      ...(builder ? [
        { label: t("Delivery"), items: [
            { label: t("Test a candidate"), icon: <Boxes />, route: { view: "candidate-test" } },
            { label: t("Release review"), icon: <Boxes />, route: { view: "release-review" } },
        ] },
      ] : []),
    ];
  },
});
