import type { Api } from "../../host";
import { listPage } from "../../pages/ListEntry";

const read = "/v1/audit?limit=100";

export default listPage<Api.AuditPage, Api.AuditEntry>({
  title: "Audit",
  about: "The audit trail records who did what, in which app, to which target, and when.",
  read,
  rows: (answer) => answer.entries,
  more: (answer) => (answer.hasMore && answer.nextCursor ? `${read}&cursor=${encodeURIComponent(answer.nextCursor)}` : undefined),
  id: (entry) => entry.id ?? `${entry.at} ${entry.member} ${entry.action}`,
  columns: [
    { key: "action", header: "Action", kind: "code", text: (entry) => entry.action },
    { key: "member", header: "Member", width: 160, kind: "member", text: (entry) => entry.member },
    { key: "app", header: "App", width: 120, kind: "code", text: (entry) => entry.app },
    { key: "target", header: "Target", kind: "code", text: (entry) => entry.target ?? "" },
    { key: "at", header: "When", width: 140, kind: "time", text: (entry) => entry.at },
  ],
  groups: [{ key: "app", label: "App", of: (entry) => entry.app }],
});
