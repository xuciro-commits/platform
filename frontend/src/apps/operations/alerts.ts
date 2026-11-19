import type { Api } from "../../host";
import { t } from "../../i18n";
import { listPage } from "../../pages/ListEntry";

export default listPage<Api.MonitoringAlert[], Api.MonitoringAlert>({
  title: "Alerts",
  about: "An alert tells the holders of a role when something needs a person: a failed run, a stuck queue, a refused change.",
  read: "/v1/monitoring/alerts",
  rows: (answer) => answer,
  id: (alert) => alert.id,
  columns: [
    { key: "title", header: "Alert", text: (alert) => alert.title },
    { key: "message", header: "Message", text: (alert) => alert.message },
    { key: "role", header: "Role", width: 140, kind: "code", text: (alert) => alert.role },
    { key: "object", header: "Object", width: 160, kind: "code", text: (alert) => alert.object ?? "" },
    { key: "repeat", header: "Repeats", width: 100, boolean: true, text: (alert) => (alert.repeat ? t("Yes") : "") },
  ],
  groups: [{ key: "role", label: "Role", of: (alert) => alert.role }],
});
