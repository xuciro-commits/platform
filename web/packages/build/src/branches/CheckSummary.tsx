import type { Api } from "@platform/kernel";
import { Notice, Tag, formatStamp, t } from "@platform/ui";
import { resourceKey, type Titles } from "./diff";
import { resourceKindLabel } from "./labels";

export function checkFailure(check: Api.BranchCheck, titles: Titles): string | undefined {
  if (check.passed) return undefined;
  const failed = check.resources.find(item => !item.accepted);
  if (failed) return `${titles[resourceKey(failed.type, failed.id)] ?? failed.id}: ${failed.message || failed.issues?.[0]?.message || t("The check failed.")}`;
  if (!check.graph.passed) return check.graph.messages[0] || t("The definition graph does not validate.");
  if (check.simulation && check.simulation.passed === false) return check.simulation.messages[0] || t("The simulation failed.");
  if (check.conflicts > 0) return t("{n} conflicts with main need a choice.", { n: check.conflicts });
  return t("The check failed.");
}

export function CheckSummary({ check, titles, stale }: { check: Api.BranchCheck; titles: Titles; stale?: boolean }) {
  return <div className="grid min-w-0 gap-3">
    <div className="flex flex-wrap items-center gap-2">
      <Tag label={check.passed ? t("Passed") : t("Failed")} tone={check.passed ? "success" : "danger"} />
      {stale && <Tag label={t("Stale")} tone="warning" />}
      <span className="min-w-0 text-xs text-muted">{formatStamp(check.at)} · {check.by}</span>
    </div>
    {stale && <Notice tone="warning" message={t("Main has changed since this check. Re-check before merging.")} />}
    {check.conflicts > 0 && <Notice tone="danger" message={t("{n} conflicts with main need a choice.", { n: check.conflicts })} />}
    <section aria-label={t("Whole definition graph")} className="grid gap-1">
      <div className="flex items-center gap-2"><h3 className="text-xs font-semibold">{t("Whole definition graph")}</h3><Tag label={check.graph.passed ? t("Passed") : t("Failed")} tone={check.graph.passed ? "success" : "danger"} /></div>
      {check.graph.messages.map((message, index) => <p key={index} className={`break-words text-xs ${check.graph.passed ? "text-muted" : "text-danger"}`}>{message}</p>)}
    </section>
    <section aria-label={t("Resources")} className="grid gap-1">
      <h3 className="text-xs font-semibold">{t("Resources")}</h3>
      {check.resources.length ? <ul className="divide-y divide-border">
        {check.resources.map(item => <li key={resourceKey(item.type, item.id)} className="grid min-w-0 gap-1 py-1.5">
          <div className="flex min-w-0 items-center gap-2"><Tag label={item.accepted ? t("Passed") : t("Failed")} tone={item.accepted ? "success" : "danger"} /><span className="min-w-0 flex-1 truncate text-sm">{titles[resourceKey(item.type, item.id)] ?? item.id}</span><span className="shrink-0 text-xs text-muted">{resourceKindLabel(item.type)}</span></div>
          {item.message && <p className={`break-words text-xs ${item.accepted ? "text-muted" : "text-danger"}`}>{item.message}</p>}
          {item.issues?.map((issue, index) => <p key={index} className="break-words text-xs text-danger">{issue.message}</p>)}
        </li>)}
      </ul> : <p className="text-xs text-muted">{t("No resources to check.")}</p>}
    </section>
    {check.simulation && <section aria-label={t("Simulation")} className="grid gap-1">
      <div className="flex items-center gap-2"><h3 className="text-xs font-semibold">{t("Simulation")}</h3>{check.simulation.passed !== undefined && <Tag label={check.simulation.passed ? t("Passed") : t("Failed")} tone={check.simulation.passed ? "success" : "danger"} />}<span className="text-xs text-muted">{t("{n} steps", { n: check.simulation.steps })}</span></div>
      {check.simulation.messages.map((message, index) => <p key={index} className="break-words text-xs text-muted">{message}</p>)}
    </section>}
  </div>;
}
