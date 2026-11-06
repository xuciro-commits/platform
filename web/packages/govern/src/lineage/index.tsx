import { appRegistration, defineApp } from "@platform/app";
import { t } from "@platform/ui";
import { Network } from "lucide-react";
import { LineagePage } from "./LineagePage";

export const dataLineage = defineApp({
  ...appRegistration("data-lineage"),
  icon: <Network />,
  description: "How data moves from connections through sources, datasets and pipelines into object types and back through writebacks.",
  views: [{
    id: "lineage",
    title: () => t("Data Lineage"),
    documentKey: params => !params.project && (params.mode === "overview" || (!params.mode && !params.focus && !params.node && !params.ref)) ? "overview" : "graph",
    render: params => <LineagePage params={params} />,
  }],
  nav: () => [{ label: t("Data Lineage"), items: [
    { label: t("Overview"), icon: <Network />, route: { view: "lineage" } },
    { label: t("Graph"), icon: <Network />, route: { view: "lineage", params: { mode: "focus" } } },
  ] }],
});
