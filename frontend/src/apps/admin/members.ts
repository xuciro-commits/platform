import { createElement } from "react";
import type { Api } from "../../host";
import { listPage } from "../../pages/ListEntry";
import { MemberDirectoryEntry } from "../../pages/MemberDirectoryEntry";
import { MemberRoles } from "../../pages/MemberRoles";
import { useSearch } from "../../router";

const Members = listPage<Api.MemberView[], Api.MemberView>({
  title: "Members",
  about: "A member is a person or agent who can sign in to this workspace.",
  read: "/v1/members",
  rows: (answer) => answer,
  id: (member) => member.id,
  columns: [
    { key: "id", header: "Member", kind: "member", text: (member) => member.profile?.displayName || member.id },
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
  detail: (_, member) => (member ? createElement(MemberRoles, { member }) : null),
});

export default function MemberEntry() {
  const member = new URLSearchParams(useSearch()).get("member");
  return member ? createElement(MemberDirectoryEntry, { id: member }) : createElement(Members);
}
