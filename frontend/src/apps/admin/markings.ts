import type { Api } from "../../host";
import { t } from "../../i18n";
import { listPage } from "../../pages/ListEntry";

export default listPage<Api.Marking[], Api.Marking>({
  title: "Markings",
  about: "A marking protects the data it is applied to: only members and teams cleared for it can see that data.",
  read: "/v1/markings",
  rows: (answer) => answer,
  id: (marking) => marking.id,
  columns: [
    { key: "title", header: "Marking", text: (marking) => marking.title },
    { key: "state", header: "State", width: 120, kind: "enum", choices: ["Active", "Archived"], text: (marking) => t(marking.archived ? "Archived" : "Active"), value: (marking) => (marking.archived ? "Archived" : "Active"), dot: (marking) => (marking.archived ? "neutral" : "success") },
    { key: "classification", header: "Classification", width: 130, boolean: true, text: (marking) => (marking.classification ? t("Yes") : "") },
    { key: "cleared", header: "Cleared for me", width: 130, boolean: true, text: (marking) => (marking.cleared ? t("Yes") : "") },
    { key: "members", header: "Members", hiddenByDefault: true, text: (marking) => (marking.members ?? []).join(", ") },
    { key: "teams", header: "Teams", hiddenByDefault: true, text: (marking) => (marking.teams ?? []).join(", ") },
  ],
  groups: [{ key: "kind", label: "Kind", of: (marking) => t(marking.classification ? "Classification" : "Marking") }],
  actions: { create: "platform.marking.save", row: (marking) => (marking.archived ? [] : [{ schema: "platform.marking.archive" }]) },
});
