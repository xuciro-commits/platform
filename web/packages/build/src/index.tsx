// The builder's UI (ADR-0034): the objects this organisation defines, the pages
// it composes over them, and every page it has published. All of them are pages
// of the host's definition registry, rendered by the same component a code page
// uses — the builder gets the shared list/detail frame, generated forms and
// keyboard behaviour, and nothing here interprets a definition of its own.
import "./i18n";
import { defineApp } from "@platform/app";
import { PageEditor, PagesList } from "./editor";
import { ProcessEditor, ProcessPicker } from "./process";
import { WorkflowEditor, Workflows } from "./workflow";
import { FunctionEditor, Functions } from "./function";
import { CodeEditor, CodeFunctions } from "./code";
import { ReleaseReview, type ReleaseKind } from "./release";
import { CandidateTest } from "./simulate";
import { StudioOverview } from "./studio";
import { StudioTemplates } from "./template-ui";
import { t, type NavSection } from "@platform/ui";
import { AppWindow, Boxes, GitBranch, Hammer, LayoutList } from "lucide-react";

/** A page of the builder app, by its name in the registry. */
const page = (name: string) => ({ view: "page", params: { app: "build", kind: "page", name } });

export default defineApp({
  id: "build",
  title: t("Application Studio"),
  icon: <Hammer />,
  home: { view: "studio" },
  views: [
    { id: "studio", title: () => t("Application Studio"), render: () => <StudioOverview /> },
    { id: "studio-templates", title: () => t("Studio templates"), render: (p) => <StudioTemplates key={p.template ?? "templates"} initial={p.template} /> },
    { id: "pages", title: () => t("Pages"), render: () => <PagesList /> },
    { id: "compose", title: () => t("Compose a page"), render: (p) => <PageEditor id={p.id ?? ""} /> },
    { id: "process", title: () => t("Object design"), render: (p) => p.id ? <ProcessEditor id={p.id} /> : <ProcessPicker /> },
    { id: "workflow", title: () => t("Workflows"), render: (p) => p.id ? <WorkflowEditor key={p.id} id={p.id} /> : <Workflows /> },
    { id: "function", title: () => t("AI functions"), render: (p) => p.id ? <FunctionEditor key={p.id} id={p.id} /> : <Functions /> },
    { id: "code", title: () => t("Code functions"), render: (p) => p.id ? <CodeEditor key={p.id} id={p.id} /> : <CodeFunctions /> },
    { id: "release-review", title: () => t("Release review"), render: (p) => <ReleaseReview key={`${p.kind}:${p.id}`} initialKind={["object", "page", "app", "flow", "function", "compute"].includes(p.kind ?? "") ? p.kind as ReleaseKind : "object"} initialID={p.id} /> },
    { id: "candidate-test", title: () => t("Test a candidate"), render: (p) => <CandidateTest key={p.functionId ?? p.processId ?? p.objectId ?? "object"} processId={p.processId} functionId={p.functionId} objectId={p.objectId} /> },
  ],
  opens: { "build.page": "compose", "build.object": "process", "build.process": "workflow", "build.function": "function", "build.code": "code" }, // open a semantic asset in its editor
  nav: (host): NavSection[] => {
    const own = host.definitions.filter((d) => d.ref.kind === "page" && d.ref.app === "build" && d.source === "tenant");
    return [
      {
        label: t("Application Studio"),
        items: [
          { label: t("Overview"), icon: <Boxes />, route: { view: "studio" } },
          { label: t("Objects"), icon: <Hammer />, route: page("objects") },
          { label: t("Process and access"), icon: <GitBranch />, route: { view: "process" } },
          { label: t("Pages"), icon: <LayoutList />, route: { view: "pages" } },
          { label: t("Applications"), icon: <AppWindow />, route: page("applications") },
          ...(host.role("build") === "builder" ? [
            { label: t("Studio templates"), icon: <LayoutList />, route: { view: "studio-templates" } },
            { label: t("Workflows"), icon: <GitBranch />, route: { view: "workflow" } },
            { label: t("AI functions"), icon: <Boxes />, route: { view: "function" } },
            { label: t("Code functions"), icon: <Boxes />, route: { view: "code" } },
            { label: t("Test a candidate"), icon: <Boxes />, route: { view: "candidate-test" } },
            { label: t("Release review"), icon: <Boxes />, route: { view: "release-review" } },
          ] : []),
        ],
      },
      ...(own.length > 0
        ? [{ label: t("Pages this organisation published"), items: own.map((d) => ({ label: d.page?.title ?? d.ref.name, icon: <Boxes />, route: page(d.ref.name) })) }]
        : []),
    ];
  },
});
