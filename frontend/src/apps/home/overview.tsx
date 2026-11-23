import type { Api } from "../../host";
import { useRead } from "../../host";
import { WorkspaceWelcome } from "../../pages/WorkspaceWelcome";
import { dashboardPage } from "../../pages/DashboardPage";

type Directory = { resources: Api.ResourceEntry[] };

const ofKind = (answer: Directory, kind: string) => answer.resources.filter((resource) => resource.ref.kind === kind);
const latest = (answer: Directory, kind: string) =>
  ofKind(answer, kind)
    .sort((a, b) => (b.changed.at ?? "").localeCompare(a.changed.at ?? ""))
    .slice(0, 8)
    .map((resource) => ({
      id: resource.id,
      title: resource.title,
      owner: resource.ref.app,
      changed: resource.changed.at ? new Date(resource.changed.at).toLocaleString() : "",
    }));
const columns = [
  { key: "title", header: "Name" },
  { key: "owner", header: "Owner", width: 120 },
  { key: "changed", header: "Updated", width: 180 },
];

const Dashboard = dashboardPage<Directory>({
  title: "Overview",
  read: "/v1/resources",
  metrics: [
    { title: "Object types", href: "/ontology/object-types", value: (answer) => ofKind(answer, "object").length },
    { title: "Action types", href: "/ontology/action-types", value: (answer) => ofKind(answer, "action").length },
    { title: "Modules", href: "/apps/modules", value: (answer) => ofKind(answer, "app").length },
    { title: "Pages", href: "/apps/modules", value: (answer) => ofKind(answer, "page").length },
  ],
  tables: [
    { title: "Recently updated object types", label: "All object types", href: "/ontology/object-types", columns, rows: (answer) => latest(answer, "object") },
    { title: "Recently updated modules", label: "All modules", href: "/apps/modules", columns, rows: (answer) => latest(answer, "app") },
  ],
});

function HomeOverview() {
  const resources = useRead<Directory>("/v1/resources");
  const installed = useRead<Api.ApplicationView[]>("/v1/applications");
  if (resources.status === "ready" && installed.status === "ready" && resources.data.resources.length === 0 && installed.data.length === 0) {
    return <WorkspaceWelcome />;
  }
  return <Dashboard />;
}

export default HomeOverview;
