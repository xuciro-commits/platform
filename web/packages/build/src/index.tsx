// The builder's UI (ADR-0034): the objects this organisation defines, the pages
// it composes over them, and every page it has published. All of them are pages
// of the host's definition registry, rendered by the same component a code page
// uses — the builder gets the shared list/detail frame, generated forms and
// keyboard behaviour, and nothing here interprets a definition of its own.
import "./i18n";
import { defineApp } from "@platform/app";
import { PageEditor, PagesList } from "./editor";
import { ProcessEditor, ProcessPicker } from "./process";
import { t, type NavSection } from "@platform/ui";
import { AppWindow, Boxes, Hammer, LayoutList, Workflow } from "lucide-react";

/** A page of the builder app, by its name in the registry. */
const page = (name: string) => ({ view: "page", params: { app: "build", kind: "page", name } });

export default defineApp({
  id: "build",
  title: t("Builder"),
  icon: <Hammer />,
  home: page("objects"),
  views: [
    { id: "pages", title: () => t("Pages"), render: () => <PagesList /> },
    { id: "compose", title: () => t("Compose a page"), render: (p) => <PageEditor id={p.id ?? ""} /> },
    { id: "process", title: () => t("States and actions"), render: (p) => p.id ? <ProcessEditor id={p.id} /> : <ProcessPicker /> },
  ],
  opens: { "build.page": "compose" }, // a page record opens where it is composed
  nav: (host): NavSection[] => {
    const own = host.definitions.filter((d) => d.ref.kind === "page" && d.ref.app === "build" && d.source === "tenant");
    return [
      {
        label: t("Builder"),
        items: [
          { label: t("Objects"), icon: <Hammer />, route: page("objects") },
          { label: t("States and actions"), icon: <Workflow />, route: { view: "process" } },
          { label: t("Pages"), icon: <LayoutList />, route: { view: "pages" } },
          { label: t("Applications"), icon: <AppWindow />, route: page("applications") },
        ],
      },
      ...(own.length > 0
        ? [{ label: t("Pages this organisation published"), items: own.map((d) => ({ label: d.page?.title ?? d.ref.name, icon: <Boxes />, route: page(d.ref.name) })) }]
        : []),
    ];
  },
});
