// Automations (ADR-0053 §7): a rule reads as one sentence — when a record of
// an object type reaches a state, run these effects. Until the dedicated
// automation record lands in the Go contract (§10 P5), an automation is a
// build.process flow with a trigger (object + start state, not manual); this
// view shows those flows as cards and opens the flow map for the effects.
import { useHost, useReadQuery } from "@platform/app";
import { Button, Input, PageHeader, Tag, cn, t } from "@platform/ui";
import { Clock, Play, Plus, Workflow, Zap } from "lucide-react";
import { useState } from "react";
import { useApplicationWorkspace } from "../projects/application-scope";
import type { WorkflowDraft } from "./workflow-model";

type Row = WorkflowDraft & { version?: number; published?: string };

const effectKinds = (steps: WorkflowDraft["steps"]) => {
  const counts: Record<string, number> = {};
  for (const step of steps ?? []) counts[step.kind] = (counts[step.kind] ?? 0) + 1;
  return Object.entries(counts).map(([kind, n]) => `${n} × ${t(kind)}`).join(" · ");
};

/** Automations as cards: trigger on the left, effects on the right, state as a tag. */
export function Automations() {
  const { role, entities } = useHost(), { open } = useApplicationWorkspace();
  const query = useReadQuery<{ records?: Row[]; items?: Row[] }>("/v1/records/build.process?limit=200");
  const [search, setSearch] = useState("");
  const rows = (query.data?.records ?? query.data?.items ?? []).filter((row) => row.object && !row.manual);
  const needle = search.trim().toLowerCase();
  const visible = rows.filter((row) => !needle || `${row.title} ${row.name} ${row.object} ${row.when}`.toLowerCase().includes(needle));
  const titleOf = (type: string) => entities.find((entity) => entity.type === type)?.title ?? type;
  const stateOf = (type: string, name: string) => entities.find((entity) => entity.type === type)?.lifecycle?.states.find((state) => state.name === name)?.title ?? name;
  return <div className="grid content-start gap-3 p-4">
    <PageHeader title={t("Automations")} description={t("When a record reaches a state, run effects. Each automation is one sentence; open it to shape the effects on the map.")}
      actions={role("build") === "builder" && <Button onClick={() => open({ view: "flow", params: { id: "new", kind: "automation" } })}><Plus />{t("New automation")}</Button>} />
    <div className="flex items-center gap-2">
      <Input type="search" aria-label={t("Filter automations")} placeholder={t("Filter automations")} value={search} onChange={(event) => setSearch(event.target.value)} className="max-w-xs" />
      <Button variant="ghost" size="sm" onClick={() => open({ view: "runs" })}><Clock />{t("Runs")}</Button>
      <Button variant="ghost" size="sm" onClick={() => open({ view: "flow" })}><Workflow />{t("All flows")}</Button>
    </div>
    {query.isLoading && <p className="text-sm text-muted">{t("Loading…")}</p>}
    {!query.isLoading && !visible.length && <div className="rounded-md border border-dashed border-border p-8 text-center text-sm text-muted">
      <p>{rows.length ? t("No automation matches the filter.") : t("No automations yet.")}</p>
      {!rows.length && <p className="mt-1 text-xs">{t("Create one: choose the object type and the state that triggers it, then add the effects.")}</p>}
    </div>}
    <ul className="grid gap-2 md:grid-cols-2 xl:grid-cols-3">{visible.map((row) => <li key={row.id}>
      <Button variant="row" onClick={() => open({ view: "flow", params: { id: row.id } })}
        className={cn("grid w-full gap-2 border-border bg-surface p-3 text-left hover:border-primary")}>
        <span className="flex items-center gap-2"><Zap className="size-4 text-primary" /><span className="min-w-0 flex-1 truncate text-sm font-medium">{row.title || row.name}</span>
          <Tag label={row.version ? t("Active · v{n}", { n: row.version }) : t("Draft")} tone={row.version ? "success" : "warning"} /></span>
        <span className="text-xs text-muted"><span className="font-medium text-foreground">{t("When")}</span> {t("a {object} reaches {state}", { object: titleOf(row.object), state: stateOf(row.object, row.when) })}</span>
        <span className="text-xs text-muted"><span className="font-medium text-foreground">{t("Then")}</span> {effectKinds(row.steps) || t("nothing yet")}</span>
        <span className="flex items-center gap-1 text-[11px] text-muted"><Play className="size-3" />{t("{n} steps", { n: row.steps?.length ?? 0 })}</span>
      </Button>
    </li>)}</ul>
  </div>;
}
