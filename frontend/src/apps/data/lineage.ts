import type { Api } from "../../host";
import { t } from "../../i18n";
import { recordHref } from "../entries";
import { listPage } from "../../pages/ListEntry";

type Node = Api.LineageNode & { upstream: number; downstream: number };

export default listPage<Api.Lineage, Node>({
  title: "Lineage",
  about: "Lineage shows how data flows from connections and sources through datasets and pipelines into objects.",
  read: "/v1/lineage",
  rows: (answer) => answer.nodes.map((node) => ({
    ...node,
    upstream: answer.edges.filter((edge) => edge.to === node.id).length,
    downstream: answer.edges.filter((edge) => edge.from === node.id).length,
  })),
  id: (node) => node.id,
  link: (node) => (node.record && node.canOpen ? recordHref(`build.${node.kind}`, node.record) : ""),
  columns: [
    { key: "title", header: "Name", text: (node) => node.title || node.name },
    { key: "kind", header: "Kind", width: 120, text: (node) => t(node.kind), value: (node) => node.kind },
    { key: "status", header: "Status", width: 120, text: (node) => t(node.status), value: (node) => node.status, dot: (node) => (node.status === "current" ? "success" : "warning") },
    { key: "upstream", header: "Upstream", width: 110, kind: "number", text: (node) => String(node.upstream), value: (node) => node.upstream },
    { key: "downstream", header: "Downstream", width: 120, kind: "number", text: (node) => String(node.downstream), value: (node) => node.downstream },
    { key: "state", header: "State", width: 110, text: (node) => (node.state ? t(node.state) : "") },
    { key: "name", header: "API name", kind: "code", hiddenByDefault: true, text: (node) => node.name },
  ],
  groups: [
    { key: "kind", label: "Kind", of: (node) => t(node.kind) },
    { key: "project", label: "Project", of: (node) => node.project ?? "" },
  ],
});
