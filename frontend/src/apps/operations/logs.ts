import type { Api } from "../../host";
import { t } from "../../i18n";
import { listPage } from "../../pages/ListEntry";
import type { Dot } from "../../pages/Transitions";

const levels = ["debug", "info", "warn", "error"];
const sources = ["request", "submit", "run", "worker", "data", "app"];
const dots: Record<string, Dot> = { debug: "neutral", info: "accent", warn: "warning", error: "error" };
const read = "/v1/monitoring/logs?limit=50";

export default listPage<Api.ObservationPage, Api.Observation>({
  title: "Logs",
  about: "A log entry is one thing the platform observed: a request, a submission, a run or a worker step.",
  read,
  rows: (answer) => answer.entries,
  more: (answer) => (answer.hasMore && answer.nextCursor ? `${read}&cursor=${encodeURIComponent(answer.nextCursor)}` : undefined),
  id: (entry) => entry.id,
  columns: [
    { key: "message", header: "Message", text: (entry) => entry.message },
    { key: "level", header: "Level", width: 110, kind: "enum", choices: levels, text: (entry) => t(entry.level), value: (entry) => entry.level, dot: (entry) => dots[entry.level] },
    { key: "source", header: "Source", width: 110, kind: "enum", choices: sources, text: (entry) => t(entry.source), value: (entry) => entry.source },
    { key: "at", header: "When", width: 140, kind: "time", text: (entry) => entry.at },
    { key: "member", header: "Member", width: 140, kind: "member", text: (entry) => entry.member ?? "" },
    { key: "route", header: "Route", width: 220, kind: "code", text: (entry) => entry.route ?? "" },
    { key: "outcome", header: "Outcome", width: 110, hiddenByDefault: true, text: (entry) => (entry.outcome ? t(entry.outcome) : "") },
    { key: "app", header: "App", width: 110, kind: "code", hiddenByDefault: true, text: (entry) => entry.app ?? "" },
    { key: "trace", header: "Trace", kind: "code", hiddenByDefault: true, text: (entry) => entry.traceId ?? "" },
    { key: "request", header: "Request", kind: "code", hiddenByDefault: true, text: (entry) => entry.requestId ?? "" },
    { key: "attributes", header: "Attributes", kind: "code", hiddenByDefault: true, text: (entry) => Object.entries(entry.attributes ?? {}).map(([name, value]) => `${name}=${value}`).join(" ") },
  ],
  groups: [
    { key: "level", label: "Level", of: (entry) => t(entry.level), order: levels.map((level) => t(level)) },
    { key: "source", label: "Source", of: (entry) => t(entry.source) },
  ],
});
