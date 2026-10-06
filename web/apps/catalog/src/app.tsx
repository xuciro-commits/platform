// The Asset Library (ADR-0054 D3–D4): public reference content for builders and
// developers — the assets with their live examples, and a sandbox for
// prototypes. Runtime access still uses the signed-in host.
import { defineApp } from "@platform/app";
import { BookOpen, Boxes, Terminal } from "lucide-react";
import { t, type NavSection } from "@platform/ui";
import { Catalog, catalogTitle } from "./Catalog";
import { SandboxView } from "./Sandbox";

export function catalogNavigation(): NavSection[] {
  return [{ label: t("Asset Library"), items: [
    { label: t("All assets"), icon: <Boxes />, route: { view: "catalog" } },
    { label: t("Sandbox"), icon: <Terminal />, route: { view: "sandbox" } },
  ] }];
}

export default defineApp({
  id: "catalog",
  surface: "developer",
  category: "developer",
  description: t("Reusable UI, App API and patterns, with live examples and a sandbox."),
  get title() { return t("Asset Library"); },
  icon: <BookOpen />,
  home: { view: "catalog" },
  nav: (host) => [
    ...catalogNavigation(),
    ...(host.role("build") === "builder" || host.role("platform") === "admin" ? [{ label: t("Data diagnostics"), items: [
      { label: t("Records"), route: { view: "records", params: { surface: "developer" } } },
      { label: t("Definitions"), route: { view: "definitions", params: { surface: "developer" } } },
    ] }] : []),
  ],
  views: [
    { id: "catalog", title: (p) => catalogTitle(p.id, p.layer), render: (p) => <Catalog initialID={p.id} initialLayer={p.layer} initialExpand={p.expand === "1"} /> },
    { id: "sandbox", title: () => t("Sandbox"), render: () => <SandboxView /> },
  ],
});
