import type { Api } from "../../host";
import { t } from "../../i18n";
import { listPage } from "../../pages/ListEntry";
import type { Dot } from "../../pages/Transitions";

const statuses = ["ok", "failed", "pending", "running", "warning", "paused", "cancelled", "unknown"];
const types = ["sync", "build", "automation", "function", "delivery"];
const dots: Record<string, Dot> = { ok: "success", failed: "error", warning: "warning", running: "accent" };
const page = 50;

export default listPage<Api.MonitoringRunPage, Api.MonitoringRun>({
  title: "Runs",
  about: "A run is one execution of a sync, build, automation, function or delivery, with its outcome.",
  read: `/v1/monitoring/runs?limit=${page}`,
  rows: (answer) => answer.runs,
  more: (answer) => (answer.offset + answer.runs.length < answer.total ? `/v1/monitoring/runs?limit=${page}&offset=${answer.offset + answer.runs.length}` : undefined),
  id: (run) => run.id,
  link: (run, recordHref) => {
    const [type, ...id] = (run.recordRef ?? "").split("/");
    return id.length ? recordHref(type, id.join("/")) : "";
  },
  columns: [
    { key: "title", header: "Run", text: (run) => run.title },
    { key: "status", header: "Status", width: 130, kind: "enum", choices: statuses, text: (run) => t(run.status), value: (run) => run.status, dot: (run) => dots[run.status] ?? "neutral" },
    { key: "type", header: "Type", width: 130, kind: "enum", choices: types, text: (run) => t(run.type), value: (run) => run.type },
    { key: "source", header: "Source", width: 120, kind: "code", text: (run) => run.source },
    { key: "when", header: "When", width: 140, kind: "time", text: (run) => run.when ?? "" },
    { key: "owner", header: "Owner status", width: 140, kind: "code", hiddenByDefault: true, text: (run) => run.ownerStatus ?? "" },
  ],
  groups: [
    { key: "type", label: "Type", of: (run) => t(run.type) },
    { key: "status", label: "Status", of: (run) => t(run.status) },
  ],
  actions: { row: (run) => (run.retrySchema && run.retryTarget ? [{ schema: run.retrySchema, id: run.retryTarget.id }] : []) },
});
