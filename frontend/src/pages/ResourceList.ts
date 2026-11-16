import type { Api } from "../host";
import { listPage } from "./ListPage";

export function resourceList({ kind, title, about }: { kind: string; title: string; about: string }) {
  return listPage<{ resources: Api.ResourceEntry[] }, Api.ResourceEntry>({
    title,
    about,
    read: "/v1/resources",
    rows: (answer) => answer.resources.filter((resource) => resource.ref.kind === kind),
    id: (resource) => resource.id,
    columns: [
      { key: "title", header: "Name", text: (resource) => resource.title },
      { key: "owner", header: "Owner", width: 140, text: (resource) => resource.ref.app },
      { key: "project", header: "Project", width: 160, text: (resource) => resource.project },
      { key: "state", header: "State", width: 110, text: (resource) => resource.state },
      { key: "version", header: "Version", width: 90, text: (resource) => String(resource.version) },
      { key: "changed", header: "Updated", width: 180, text: (resource) => (resource.changed.at ? new Date(resource.changed.at).toLocaleString() : "") },
    ],
    groups: [
      { key: "owner", label: "Owner", of: (resource) => resource.ref.app },
      { key: "project", label: "Project", of: (resource) => resource.project },
      { key: "state", label: "State", of: (resource) => resource.state },
    ],
  });
}
