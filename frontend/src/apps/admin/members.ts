import type { Api } from "../../host";
import { listPage } from "../../pages/ListPage";

export default listPage<Api.Member[], Api.Member>({
  title: "Members",
  about: "A member is a person or agent who can sign in to this workspace.",
  read: "/v1/members",
  rows: (answer) => answer,
  id: (member) => member.id,
  columns: [
    { key: "id", header: "Member", text: (member) => member.id },
    {
      key: "roles",
      header: "Roles",
      text: (member) =>
        Object.entries(member.roles)
          .map(([app, role]) => `${app}: ${role}`)
          .join(", "),
    },
    { key: "markings", header: "Markings", width: 200, text: (member) => (member.markings ?? []).join(", ") },
    { key: "timezone", header: "Time zone", width: 140, text: (member) => member.timezone ?? "" },
  ],
  groups: [{ key: "status", label: "Status", of: (member) => member.status ?? "active" }],
});
