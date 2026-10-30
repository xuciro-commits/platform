import type { Api } from "@platform/kernel";
import { useHost } from "@platform/app";
import { Button, Disclosure, SegmentedChoice, Tag, t } from "@platform/ui";
import { ChevronDown, ChevronRight } from "lucide-react";
import { useState } from "react";
import { changeOpLabel, changeOpTone, pathLabel, resourceKindLabel, resourceTitle, valueText } from "./labels";

export type Titles = Record<string, string>;
export const resourceKey = (type: string, id: string) => `${type}/${id}`;
export const resolutionKey = (type: string, id: string, path: string) => `${type}/${id}#${path}`;

export function Value({ value, className }: { value: unknown; className?: string }) {
  return <span className={`min-w-0 break-words font-mono text-xs ${value === undefined || value === null || value === "" ? "text-muted" : ""} ${className ?? ""}`}>{valueText(value)}</span>;
}

function ChangeRow({ type, change }: { type: string; change: Api.PathChange }) {
  const entity = useHost().source.entity(type);
  return <li className="grid min-w-0 gap-0.5 py-1 sm:grid-cols-[minmax(8rem,14rem)_minmax(0,1fr)] sm:gap-3" title={change.path || undefined}>
    <span className="min-w-0 break-words text-xs text-muted">{pathLabel(change.path, entity)}</span>
    <span className="flex min-w-0 flex-wrap items-baseline gap-1.5 text-xs">
      {change.op !== "add" && <Value value={change.before} className="text-muted line-through" />}
      {change.op === "replace" && <span aria-hidden className="text-muted">→</span>}
      {change.op !== "remove" && <Value value={change.after} />}
      {change.op === "remove" && <Tag label={t("Removed")} tone="danger" />}
    </span>
  </li>;
}

export function ConflictPicker({ type, conflict, take, onTake }: { type: string; conflict: Api.PathConflict; take?: "branch" | "main"; onTake: (take: "branch" | "main") => void }) {
  const entity = useHost().source.entity(type);
  return <li className="grid min-w-0 gap-2 rounded-md border border-border p-2" style={{ borderLeftColor: "var(--tone-danger)", borderLeftWidth: 3 }}>
    <span className="min-w-0 break-words text-xs font-medium" title={conflict.path || undefined}>{pathLabel(conflict.path, entity)}</span>
    <div className="grid min-w-0 gap-2 text-xs sm:grid-cols-3">
      <div className="grid min-w-0 gap-0.5"><span className="text-muted">{t("Before")}</span><Value value={conflict.base} /></div>
      <div className="grid min-w-0 gap-0.5"><span className="text-muted">{t("On main now")}</span><Value value={conflict.main} /></div>
      <div className="grid min-w-0 gap-0.5"><span className="text-muted">{t("In this branch")}</span><Value value={conflict.branch} /></div>
    </div>
    <SegmentedChoice compact label={t("Keep which value for {path}", { path: pathLabel(conflict.path, entity) })} value={take ?? ""}
      options={[{ value: "branch", label: t("Keep the branch value") }, { value: "main", label: t("Take the main value") }]} onChange={value => onTake(value as "branch" | "main")} />
    {!take && <span role="status" className="text-xs text-warning">{t("Pick one value to continue.")}</span>}
  </li>;
}

export function ResourceDiff({ diff, titles, status, resolutions, onResolve, initiallyOpen = false }: {
  diff: Api.BranchResourceDiff; titles: Titles; status?: Api.BranchResourceStatus;
  resolutions: Record<string, "branch" | "main">; onResolve?: (path: string, take: "branch" | "main") => void; initiallyOpen?: boolean;
}) {
  const [open, setOpen] = useState(initiallyOpen || diff.conflicts.length > 0);
  const [all, setAll] = useState(false);
  const title = titles[resourceKey(diff.type, diff.id)] ?? resourceTitle(diff.id, diff.merged);
  const shown = all ? diff.branch : diff.branch.slice(0, 40);
  return <section className="min-w-0 rounded-md border border-border" aria-label={title}>
    <Button variant="row" type="button" aria-expanded={open} className="flex w-full min-w-0 items-center gap-2 px-3 py-2 text-left hover:bg-row-hover" onClick={() => setOpen(!open)}>
      {open ? <ChevronDown aria-hidden className="size-4 shrink-0 text-muted" /> : <ChevronRight aria-hidden className="size-4 shrink-0 text-muted" />}
      <Tag label={changeOpLabel(diff.op)} tone={changeOpTone(diff.op)} />
      <span className="min-w-0 flex-1 truncate text-sm font-medium">{title}</span>
      <span className="shrink-0 text-xs text-muted">{resourceKindLabel(diff.type)}</span>
      {diff.conflicts.length > 0 && <Tag label={t("{n} conflicts", { n: diff.conflicts.length })} tone="danger" />}
      {status?.drifted && diff.conflicts.length === 0 && <Tag label={t("Main changed")} tone="warning" />}
    </Button>
    {open && <div className="grid min-w-0 gap-3 border-t border-border px-3 py-2">
      {diff.conflicts.length > 0 && <ul className="grid gap-2" aria-label={t("Conflicts")}>
        {diff.conflicts.map(conflict => <ConflictPicker key={conflict.path} type={diff.type} conflict={conflict} take={resolutions[resolutionKey(diff.type, diff.id, conflict.path)]} onTake={take => onResolve?.(conflict.path, take)} />)}
      </ul>}
      {diff.branch.length > 0 ? <ul className="divide-y divide-border" aria-label={t("Changes in this branch")}>{shown.map(change => <ChangeRow key={change.path + change.op} type={diff.type} change={change} />)}</ul>
        : <p className="text-xs text-muted">{t("No field differs from the base.")}</p>}
      {diff.branch.length > shown.length && <Button size="sm" variant="ghost" className="w-fit" onClick={() => setAll(true)}>{t("Show all {n} changes", { n: diff.branch.length })}</Button>}
      {diff.main.length > 0 && <Disclosure summary={<span className="text-xs text-muted">{t("Changed on main since this branch started ({n})", { n: diff.main.length })}</span>}>
        <ul className="divide-y divide-border">{diff.main.map(change => <ChangeRow key={change.path + change.op} type={diff.type} change={change} />)}</ul>
      </Disclosure>}
    </div>}
  </section>;
}
