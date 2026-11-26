import { createElement } from "react";
import type { Api } from "../../host";
import { t } from "../../i18n";
import { listPage } from "../../pages/ListEntry";
import { MemberDirectoryEntry } from "../../pages/MemberDirectoryEntry";
import { MemberRoles } from "../../pages/MemberRoles";
import { roleTitle } from "../../pages/rebac/shared";
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
          .map(([app, role]) => `${t(app)}: ${roleTitle(role)}`)
          .join(", "),
    },
    {
      key: "temporary",
      header: "Temporary grants",
      width: 150,
      text: (member) => {
        const count = (member.grants ?? []).filter((g) => Boolean(g.until)).length + (member.scheduledGrants?.length ?? 0);
        return count > 0 ? t("{count} active").replace("{count}", String(count)) : "";
      },
    },
    { key: "markings", header: "Markings", width: 180, text: (member) => (member.markings ?? []).map((marking) => t(marking)).join(", ") },
    { key: "timezone", header: "Time zone", width: 140, text: (member) => member.timezone ?? "" },
  ],
  groups: [
    { key: "status", label: "Status", of: (member) => t(member.status ?? "active") },
    { key: "kind", label: "Type", of: (member) => (member.agent ? t("Agent") : t("Person")) },
  ],
  actions: { create: "platform.member.add" },
  detail: (_, member) => (member ? createElement(MemberRoles, { member }) : null),
});

export default function MemberEntry() {
  const member = new URLSearchParams(useSearch()).get("member");
  return member ? createElement(MemberDirectoryEntry, { id: member }) : createElement(Members);
}
