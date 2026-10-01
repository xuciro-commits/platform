import { defineApp } from "@platform/app";
import {
  Boxes,
  SlidersHorizontal,
  Palette,
  Layers,
  LayoutTemplate,
  Terminal,
  Sparkles,
  FileCode,
  LayoutGrid,
  Workflow,
  FileUp,
  Network,
  GitCompare,
  ShieldCheck,
  BookOpen,
} from "lucide-react";
import { t, type NavSection } from "@platform/ui";
import { Catalog, CatalogExample, catalogTitle } from "./Catalog";
import { SandboxView } from "./Sandbox";
import { PageBuilderView } from "./PageBuilder";
import { GovernanceView } from "./Governance";

/** Public reference content; runtime access still uses the signed-in host. */
export function catalogNavigation(mode: "builder" | "developer" = "builder"): NavSection[] {
  return [
    {
      label: t("Components"),
      items: [
        { label: t("All assets"), icon: <Boxes />, route: { view: "catalog", params: { mode } } },
        { label: t("Inspector"), icon: <SlidersHorizontal />, route: { view: "catalog", params: { mode: "developer" } } },
        { label: t("Design rules"), icon: <Palette />, route: { view: "catalog", params: { layer: "0", mode } } },
        { label: t("App Shell / Workspace"), icon: <LayoutGrid />, route: { view: "catalog", params: { id: "pattern/workspace", mode } } },
        { label: t("Primitives"), icon: <Layers />, route: { view: "catalog", params: { layer: "1", mode } } },
        { label: t("Business templates"), icon: <LayoutTemplate />, route: { view: "catalog", params: { layer: "5", mode } } },
      ],
    },
    {
      label: t("Sandbox"),
      items: [
        { label: t("Live playground"), icon: <Terminal />, route: { view: "sandbox", params: { tab: "playground" } } },
        { label: t("Inspirations"), icon: <Sparkles />, route: { view: "sandbox", params: { tab: "inspirations" } } },
        { label: t("Convert component"), icon: <FileCode />, route: { view: "sandbox", params: { tab: "convert" } } },
      ],
    },
    {
      label: t("Page builder"),
      items: [
        { label: t("Block library"), icon: <LayoutGrid />, route: { view: "page-builder", params: { tab: "blocks" } } },
        { label: t("Visual composer"), icon: <Workflow />, route: { view: "page-builder", params: { tab: "composer" } } },
        { label: t("Export page"), icon: <FileUp />, route: { view: "page-builder", params: { tab: "export" } } },
      ],
    },
    {
      label: t("Governance"),
      items: [
        { label: t("Impact analysis"), icon: <Network />, route: { view: "governance", params: { tab: "impact" } } },
        { label: t("Version diffs"), icon: <GitCompare />, route: { view: "governance", params: { tab: "diffs" } } },
        { label: t("Quality check"), icon: <ShieldCheck />, route: { view: "governance", params: { tab: "quality" } } },
      ],
    },
  ];
}

export default defineApp({
  id: "catalog",
  get title() {
    return t("Platform Catalog");
  },
  icon: <BookOpen />,
  home: { view: "catalog" },
  nav: () => catalogNavigation(),
  views: [
    {
      id: "catalog-example",
      title: (p) => `${catalogTitle(p.id)} · ${t("Live example")}`,
      render: (p) => <CatalogExample id={p.id} mode={p.mode === "builder" ? "builder" : "developer"} />,
    },
    {
      id: "catalog",
      title: (p) => catalogTitle(p.id, p.layer),
      render: (p) => (
        <Catalog
          initialID={p.id}
          initialLayer={p.layer}
          initialMode={p.mode === "developer" ? "developer" : "builder"}
        />
      ),
    },
    {
      id: "sandbox",
      title: (p) => (p.tab ? t(p.tab === "inspirations" ? "Inspirations" : p.tab === "convert" ? "Convert component" : "Live playground") : t("Sandbox")),
      render: (p) => <SandboxView tab={p.tab} />,
    },
    {
      id: "page-builder",
      title: (p) => (p.tab ? t(p.tab === "composer" ? "Visual composer" : p.tab === "export" ? "Export page" : "Block library") : t("Page builder")),
      render: (p) => <PageBuilderView tab={p.tab} />,
    },
    {
      id: "governance",
      title: (p) => (p.tab ? t(p.tab === "diffs" ? "Version diffs" : p.tab === "quality" ? "Quality check" : "Impact analysis") : t("Governance")),
      render: (p) => <GovernanceView tab={p.tab} />,
    },
  ],
});
