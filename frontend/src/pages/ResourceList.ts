import type { Api } from "../host";
import { t } from "../i18n";
import { listPage } from "./ListEntry";
import type { Dot } from "./Transitions";

const stateDot = (state: string): Dot =>
  state === "active" || state === "published" ? "success" : state === "draft" ? "warning" : "neutral";

export function resourceList({ kind, title, about }: { kind: string; title: string; about: string }) {
  return listPage<{ resources: Api.ResourceEntry[] }, Api.ResourceEntry>({
    title,
    about,
    read: "/v1/resources",
    rows: (answer) => answer.resources.filter((resource) => resource.ref.kind === kind),
    id: (resource) => resource.id,
    columns: [
      { key: "title", header: "Name", text: (resource) => t(resource.title) },
      { key: "owner", header: "Owner", width: 140, text: (resource) => t(resource.ref.app) },
      { key: "project", header: "Project", width: 160, text: (resource) => t(resource.project) },
      { key: "state", header: "State", width: 120, text: (resource) => t(resource.state), dot: (resource) => stateDot(resource.state) },
      { key: "version", header: "Version", width: 90, kind: "number", text: (resource) => String(resource.version), value: (resource) => resource.version },
      { key: "changed", header: "Updated", width: 140, kind: "time", text: (resource) => resource.changed.at ?? "" },
    ],
    groups: [
      { key: "owner", label: "Owner", of: (resource) => t(resource.ref.app) },
      { key: "project", label: "Project", of: (resource) => t(resource.project) },
      { key: "state", label: "State", of: (resource) => t(resource.state) },
    ],
  });
}
