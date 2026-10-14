// Changes (ADR-0053 §9): everything in the project that differs from what is
// live — drafts across object types, pages, modules, flows and functions — on
// one list, grouped by kind, with the release review for a chosen set beside
// it. The ReleaseReview keeps the host dialogue (preview, candidate, upgrade,
// activation); this view is the way in and the place to pick the set.
import { useHost, useReadQuery, useRecordInventory } from "@platform/app";
import { Button, PageHeader, Tag, cn, t, useUnsavedChanges } from "@platform/ui";
import type { Api } from "@platform/kernel";
import { PackageCheck } from "lucide-react";
import { useMemo, useState } from "react";
import { useApplicationScope, useApplicationWorkspace } from "../projects/application-scope";
import { kindIcon } from "../projects/project";
import { refKey, resourceRef, resourceKinds, resourceRoute, type ResourceRecord } from "../projects/resources";
import { ReleaseReview, releaseKinds, type ReleaseKind } from "./release";

export function Changes({ initialKind, initialID }: { initialKind?: string; initialID?: string }) {
  const { role } = useHost(), { open } = useApplicationWorkspace();
  const scope = useApplicationScope();
  const { confirmDiscard } = useUnsavedChanges(false, () => {});
  const application = useReadQuery<{ record: Api.Application }>(`/v1/records/build.app/${encodeURIComponent(scope ?? "")}`, undefined, !!scope);
  const held = useMemo(() => scope && application.data?.record ? new Set([
    ...(application.data.record.resources ?? []).map(refKey),
    ...(application.data.record.pages ?? []).map(name => refKey({ app: "build", kind: "page", name })),
  ]) : undefined, [scope, application.data]);
  const kinds = resourceKinds.filter((kind) => releaseKinds.includes(kind.kind as ReleaseKind));
  // One inventory per kind; the kind list is a module constant, so the hook count is stable.
  const inventories = kinds.map((kind) => ({ kind, rows: useRecordInventory<ResourceRecord>(kind.type, 500) }));
  const loading = inventories.some((entry) => entry.rows.isLoading);
  const unreadable = inventories.some((entry) => entry.rows.isError) || !!scope && application.isError;
  const scopePending = !!scope && !application.data;
  const changed = useMemo(() => inventories.flatMap(({ kind, rows }) => (rows.data?.records ?? []).filter((row) => !row.archived && (!scope || !!held?.has(refKey(resourceRef(kind, row.name)))) && (row.state ? row.state === "draft" : !row.version)).map((row) => ({ kind, row }))),
    [scope, held, ...inventories.map((entry) => entry.rows.data)]);
  const [picked, setPicked] = useState<{ kind: ReleaseKind; id: string } | undefined>(initialID && releaseKinds.includes(initialKind as ReleaseKind) ? { kind: initialKind as ReleaseKind, id: initialID } : undefined);
  const mayRelease = role("build") === "builder" || role("build") === "publisher";
  return <div className="flex min-h-0 flex-1 flex-col">
    <div className="px-4 pt-4"><PageHeader title={t("Changes")} description={t("What differs from the live workspace. Pick a draft to review what its release would change, then activate it.")} /></div>
    <div className="grid min-h-0 flex-1 gap-0 lg:grid-cols-[minmax(16rem,22rem)_minmax(0,1fr)]">
      <div className="min-h-0 overflow-auto border-r border-border">
        {kinds.map((kind) => { const rows = changed.filter((entry) => entry.kind.kind === kind.kind); if (!rows.length) return null; return <div key={kind.kind} className="border-b border-border">
          <h3 className="flex items-center gap-2 px-3 py-1.5 text-[11px] font-semibold uppercase tracking-wide text-muted">{t(kind.plural)} <span className="rounded-full bg-row-hover px-1.5 text-[10px]">{rows.length}</span></h3>
          <ul>{rows.map(({ row }) => <li key={row.id}><Button variant="row" type="button" aria-pressed={picked?.kind === kind.kind && picked.id === row.id}
            onClick={() => { if (picked?.kind !== kind.kind || picked.id !== row.id) confirmDiscard(() => setPicked({ kind: kind.kind as ReleaseKind, id: row.id })); }} onDoubleClick={() => confirmDiscard(() => open(resourceRoute(kind, row.id, scope)))}
            className={cn("flex w-full items-center gap-2 px-3 py-1.5 text-left text-sm hover:bg-row-hover", picked?.kind === kind.kind && picked.id === row.id && "bg-row-selected")}>
            <span className="text-muted [&>svg]:size-4">{kindIcon[kind.kind]}</span><span className="min-w-0 flex-1 truncate">{row.title || row.name}</span><Tag label={t("Draft")} tone="warning" />
          </Button></li>)}</ul>
        </div>; })}
        {unreadable && <p role="alert" className="p-4 text-sm text-danger">{t("Some draft inventories are unavailable. An empty list does not mean every draft is live.")}</p>}
        {!unreadable && !changed.length && <p className="p-4 text-sm text-muted">{loading || scopePending ? t("Loading…") : t("Nothing to release. Every draft matches what is live.")}</p>}
      </div>
      <div className="min-h-0 overflow-auto p-4">
        {picked ? <ReleaseReview key={`${picked.kind}:${picked.id}`} initialKind={picked.kind} initialID={picked.id} embedded /> : <div className="grid h-full place-content-center gap-2 text-center text-sm text-muted">
          <PackageCheck className="mx-auto size-8" /><p>{t("Pick a change to review its release.")}</p>
          {mayRelease && <Button variant="ghost" size="sm" onClick={() => open({ view: "release-history" })}>{t("Release history")}</Button>}
        </div>}
      </div>
    </div>
  </div>;
}
