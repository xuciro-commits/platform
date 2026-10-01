// Studio owns asset design. Published business pages belong to the navigation
// explicitly chosen for an application (ADR-0036), not an automatic asset dump.
import "./i18n";
import { defineApp } from "@platform/app";
import { PageEditor, PagesList } from "./editor";
import { ProcessEditor, Objects } from "./process";
import { WorkflowEditor, Workflows } from "./workflow";
import { FunctionEditor, Functions } from "./function";
import { ApplicationEditor, Applications } from "./application";
import { CodeEditor, CodeFunctions } from "./code";
import { ReleaseReview, type ReleaseKind } from "./release";
import { CandidateTest } from "./simulate";
import { StudioOverview } from "./studio";
import { StudioTemplates } from "./template-ui";
import { t, type NavSection } from "@platform/ui";
import { AppWindow, Boxes, GitBranch, Hammer, LayoutList } from "lucide-react";

export default defineApp({
  id: "build",
  title: t("Application Studio"),
  icon: <Hammer />,
  home: { view: "studio" },
  views: [
    { id: "studio", title: () => t("Application Studio"), render: () => <StudioOverview /> },
    { id: "studio-templates", title: () => t("Studio templates"), render: (p) => <StudioTemplates key={p.template ?? "templates"} initial={p.template} /> },
    { id: "applications", title: () => t("Applications"), render: () => <Applications /> },
    { id: "application", title: () => t("Application design"), render: (p) => <ApplicationEditor key={p.id ?? "new"} id={p.id ?? "new"} /> },
    { id: "pages", title: () => t("Pages"), render: () => <PagesList /> },
    { id: "compose", title: () => t("Compose a page"), render: (p) => <PageEditor id={p.id ?? ""} /> },
    { id: "process", title: (p) => p.id ? t("Object design") : t("Objects"), render: (p) => p.id ? <ProcessEditor id={p.id} /> : <Objects /> },
    { id: "workflow", title: () => t("Workflows"), render: (p) => p.id ? <WorkflowEditor key={p.id} id={p.id} /> : <Workflows /> },
    { id: "function", title: () => t("AI functions"), render: (p) => p.id ? <FunctionEditor key={p.id} id={p.id} /> : <Functions /> },
    { id: "code", title: () => t("Code functions"), render: (p) => p.id ? <CodeEditor key={p.id} id={p.id} /> : <CodeFunctions /> },
    { id: "release-review", title: () => t("Release review"), render: (p) => <ReleaseReview key={`${p.kind}:${p.id}`} initialKind={["object", "page", "app", "flow", "function", "compute"].includes(p.kind ?? "") ? p.kind as ReleaseKind : "object"} initialID={p.id} /> },
    { id: "candidate-test", title: () => t("Test a candidate"), render: (p) => <CandidateTest key={p.functionId ?? p.processId ?? p.objectId ?? "object"} processId={p.processId} functionId={p.functionId} objectId={p.objectId} /> },
  ],
  opens: { "build.app": "application", "build.page": "compose", "build.object": "process", "build.process": "workflow", "build.function": "function", "build.code": "code" }, // open a semantic asset in its editor
  nav: (host): NavSection[] => {
    const builder = host.role("build") === "builder";
    return [
      {
        label: t("Application Studio"),
        items: [
          { label: t("Overview"), icon: <Boxes />, route: { view: "studio" } },
          { label: t("Applications"), icon: <AppWindow />, route: { view: "applications" } },
          { label: t("Objects"), icon: <Hammer />, route: { view: "process" } },
          { label: t("Pages"), icon: <LayoutList />, route: { view: "pages" } },
          ...(builder ? [{ label: t("Studio templates"), icon: <LayoutList />, route: { view: "studio-templates" } }] : []),
        ],
      },
      ...(builder ? [
        { label: t("Logic"), items: [
            { label: t("Workflows"), icon: <GitBranch />, route: { view: "workflow" } },
            { label: t("AI functions"), icon: <Boxes />, route: { view: "function" } },
            { label: t("Code functions"), icon: <Boxes />, route: { view: "code" } },
        ] },
        { label: t("Delivery"), items: [
            { label: t("Test a candidate"), icon: <Boxes />, route: { view: "candidate-test" } },
            { label: t("Release review"), icon: <Boxes />, route: { view: "release-review" } },
        ] },
      ] : []),
    ];
  },
});
