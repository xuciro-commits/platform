import type { Api } from "../../host";
import { t } from "../../i18n";
import { treeList } from "../../pages/TreeList";
import type { Dot } from "../../pages/Transitions";

type Step = { name: string; route: string; at: string; end: number; outcome: string; count: number };

const rank = ["ok", "warning", "error"];
const dots: Record<string, Dot> = { ok: "success", warning: "warning", error: "error" };
const words: Record<string, string> = { ok: "OK", warning: "Warning", error: "Error" };
const outcomeOf = (entry: Api.Observation) => (entry.level === "error" || entry.outcome === "failed" ? "error" : entry.level === "warn" || entry.outcome === "refused" ? "warning" : "ok");
const time = (at: string) => new Date(at).getTime();

export default treeList<Api.ObservationPage, Step>({
  title: "Traces",
  about: "A trace follows one request through the platform: every step it took, how long it ran and how it ended.",
  read: "/v1/monitoring/traces?limit=200",
  nodes: (answer) => {
    const traced = answer.entries.filter((entry) => entry.traceId);
    const traces = [...new Set(traced.map((entry) => entry.traceId!))];
    return [
      ...traces.map((trace) => ({ id: trace, row: { name: trace, route: "", at: "", end: 0, outcome: "ok", count: 0 } })),
      ...traced.map((entry) => ({
        id: entry.id,
        parent: entry.traceId,
        row: { name: entry.message, route: entry.route ?? "", at: entry.at, end: time(entry.at) + Number(entry.attributes?.durationMs ?? 0), outcome: outcomeOf(entry), count: 0 },
      })),
    ];
  },
  rollup: (_, steps) => {
    const ordered = [...steps].sort((a, b) => time(a.at) - time(b.at));
    return {
      name: ordered.find((step) => step.route)?.route ?? ordered[0].name,
      route: "",
      at: ordered[0].at,
      end: Math.max(...steps.map((step) => step.end)),
      outcome: rank[Math.max(...steps.map((step) => rank.indexOf(step.outcome)))],
      count: steps.length,
    };
  },
  columns: [
    { key: "name", header: "Step", text: (step) => step.name },
    { key: "outcome", header: "Outcome", width: 120, text: (step) => t(words[step.outcome]), dot: (step) => dots[step.outcome] },
    { key: "at", header: "Started", width: 140, kind: "time", text: (step) => step.at },
    { key: "duration", header: "Duration", width: 120, kind: "number", text: (step) => `${Math.max(0, step.end - time(step.at))} ms`, value: (step) => step.end - time(step.at) },
    { key: "count", header: "Steps", width: 90, kind: "number", text: (step) => (step.count ? String(step.count) : ""), value: (step) => step.count },
  ],
});
