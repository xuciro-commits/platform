import { defineApp } from "@platform/app";
import {
  Boxes,
  Terminal,
  Network,
  BookOpen,
} from "lucide-react";
import { t, type NavSection } from "@platform/ui";
import { Catalog, CatalogExample, catalogTitle } from "./Catalog";
import { SandboxView } from "./Sandbox";
import { GovernanceView } from "./Governance";

/** Public reference content; runtime access still uses the signed-in host. */
export function catalogNavigation(mode: "builder" | "developer" = "builder"): NavSection[] {
  return [
    {
      label: t("Platform Catalog"),
      items: [
        { label: t("All assets"), icon: <Boxes />, route: { view: "catalog", params: { mode } } },
        { label: t("Sandbox"), icon: <Terminal />, route: { view: "sandbox" } },
        { label: t("Governance"), icon: <Network />, route: { view: "governance" } },
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
  home: { view: "catalog", params: { mode: "builder" } },
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
      id: "governance",
      title: (p) => (p.tab ? t(p.tab === "diffs" ? "Version diffs" : p.tab === "quality" ? "Quality check" : "Impact analysis") : t("Governance")),
      render: (p) => <GovernanceView tab={p.tab} />,
    },
  ],
});
