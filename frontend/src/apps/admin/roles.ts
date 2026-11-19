import type { Api } from "../../host";
import { listPage } from "../../pages/ListEntry";

export default listPage<Api.AccessConfig, Api.CustomRole>({
  title: "Business roles",
  about: "A business role is a set of an app's actions this workspace defines; members hold it like any role.",
  read: "/v1/access",
  rows: (answer) => answer.roles,
  id: (role) => role.id,
  columns: [
    { key: "title", header: "Role", text: (role) => role.title || role.id },
    { key: "app", header: "App", width: 120, kind: "code", text: (role) => role.app },
    { key: "actions", header: "Actions", kind: "code", text: (role) => role.actions.join(", ") },
  ],
  groups: [{ key: "app", label: "App", of: (role) => role.app }],
  actions: { create: "platform.role.save", row: () => [{ schema: "platform.role.remove" }] },
});
