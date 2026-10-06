import { useRecordInventory } from "@platform/app";
import { Button, StatusTag, flowStates, t, useWorkspace, type FlowInstanceData } from "@platform/ui";
import { useMemo } from "react";
import type { OwnedResource } from "./project";

// Operate from the project (ADR-0057 H1): the runs of this application's
// flows and automations, the troubled ones first, each a click from the flow
// that produced it. Nothing new is stored: these are the flow app's instances.
const ended = ["done", "canceled", "compensated"];
const trouble = (run: FlowInstanceData) => run.state === "stuck" || run.state === "compensating" || (run.tokens ?? []).some((token) => (token.attempts ?? 0) > 1 || !!token.error);

export function ProjectRuns({ owned }: { owned: OwnedResource[] }) {
  const { open } = useWorkspace();
  const flows = useMemo(() => owned.filter((item) => item.kind.kind === "flow").map((item) => item.ref.name), [owned]);
  const inventory = useRecordInventory<FlowInstanceData>("flow.instance", 1000, flows.length > 0);
  const runs = useMemo(() => (inventory.data?.records ?? []).filter((run) => flows.includes(run.flow.replace(/^build\./, "")))
    .sort((a, b) => Number(trouble(b)) - Number(trouble(a)) || Number(!ended.includes(b.state)) - Number(!ended.includes(a.state)) || (b.trace?.at(-1)?.at ?? "").localeCompare(a.trace?.at(-1)?.at ?? "")), [inventory.data, flows]);
  const counts = { running: runs.filter((run) => !ended.includes(run.state) && !trouble(run)).length, trouble: runs.filter(trouble).length, done: runs.filter((run) => ended.includes(run.state)).length };
  if (!flows.length) return <p className="p-4 text-sm text-muted">{t("This project has no flows or automations yet; their runs appear here.")}</p>;
  return <div className="grid content-start gap-4 p-4">
    <div className="grid grid-cols-3 gap-3 text-sm">
      {([["trouble", t("Need attention"), "text-danger"], ["running", t("In progress"), ""], ["done", t("Ended"), "text-muted"]] as const).map(([key, label, cls]) =>
        <div key={key} className="rounded-md border border-border p-3"><div className={"text-2xl font-semibold tabular-nums " + cls}>{counts[key]}</div><div className="text-xs text-muted">{label}</div></div>)}
    </div>
    {inventory.isLoading ? <p className="text-xs text-muted">{t("Loading…")}</p> : !runs.length ? <p className="text-xs text-muted">{t("No runs yet.")}</p> :
      <ul className="divide-y divide-border rounded-md border border-border">
        {runs.slice(0, 200).map((run) => {
          const name = run.flow.replace(/^build\./, ""), last = run.trace?.at(-1);
          const failing = (run.tokens ?? []).find((token) => !!token.error);
          return <li key={run.id} className="flex items-center gap-3 px-3 py-2 text-sm">
            <StatusTag status={run.state} registry={flowStates} />
            <div className="min-w-0 flex-1">
              <div className="truncate"><span className="font-medium">{run.title}</span> <span className="text-xs text-muted">· {name} v{run.version}</span></div>
              <div className="truncate text-xs text-muted">{failing ? `${failing.step}: ${failing.error}` : last ? `${last.step ?? ""} ${last.what}${last.detail ? ": " + last.detail : ""}`.trim() : run.key}{last?.at ? ` · ${new Date(last.at).toLocaleString()}` : ""}</div>
            </div>
            <Button size="sm" onClick={() => open({ view: "runs", params: { name } })}>{t("Open run")}</Button>
          </li>;
        })}
      </ul>}
  </div>;
}
