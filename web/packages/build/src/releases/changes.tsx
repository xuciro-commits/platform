// Discovery and field differences come from the original Build declarations.
// Selecting changes opens the existing release review, not another publisher.
import { useReadQuery } from "@platform/app";
import type { Api } from "@platform/kernel";
import { Button, Checkbox, DataTable, Disclosure, PageHeader, Panel, Tag, t, useUnsavedChanges, type ColumnDef } from "@platform/ui";
import { useState } from "react";
import { useApplicationScope, useApplicationWorkspace } from "../projects/application-scope";
import { resourceKinds, resourceRoute } from "../projects/resources";
import { releaseDraftsValue, type JointChoice, type ReleaseKind } from "./release";
import { releaseSelectionProblem } from "./selection";

export const draftChangeKind = (change: Api.DraftChange): ReleaseKind => change.type === "build.app" ? "app" : change.type === "build.migration-plan" ? "migration" : change.type === "build.process" ? "flow"
  : resourceKinds.find(kind => kind.type === change.type && (kind.ref ?? kind.kind) === change.ref.kind && (kind.kind !== "automation" || change.variant === "automation") && (kind.kind !== "flow" || change.variant !== "automation"))?.kind as ReleaseKind;
const changeKey = (change: Api.DraftChange) => `${change.type}/${change.id}`;

export function useSavedDraftChanges(application?: string, detail?: Pick<Api.DraftChange, "type" | "id">, enabled = true) {
  const params = new URLSearchParams();
  if (application) params.set("application", application);
  if (detail) { params.set("type", detail.type); params.set("id", detail.id); }
  return useReadQuery<Api.ReleaseDraftChanges>(`/v1/releases/drafts/changes${params.size ? `?${params}` : ""}`, undefined, enabled);
}

export function Changes({ initialKind, initialID }: { initialKind?: string; initialID?: string }) {
  const scope = useApplicationScope(), { open, transfer } = useApplicationWorkspace();
  const query = useSavedDraftChanges(scope), rows = query.data?.changes ?? [];
  const [picked, setPicked] = useState<{ kind?: string; id?: string }>({ kind: initialKind, id: initialID });
  const [checked, setChecked] = useState<string[]>([]);
  const [selectionError, setSelectionError] = useState("");
  const current = rows.find(row => row.id === picked.id && draftChangeKind(row) === picked.kind);
  const detail = useSavedDraftChanges(scope, current, !!current);
  const original = detail.data?.changes.find(row => row.type === current?.type && row.id === current?.id);
  const { confirmDiscard } = useUnsavedChanges(false, () => {});
  const selected = rows.filter(row => checked.includes(changeKey(row)));
  // The complete selection travels through the workspace's original view
  // call; the route carries only the anchor draft. Legacy links keep the
  // JSON-in-URL form, so the fallback below stays valid.
  const review = (selection: Api.DraftChange[]) => {
    const drafts: JointChoice[] = selection.map(change => ({ kind: draftChangeKind(change), id: change.id }));
    if (!drafts.length) return;
    const problem = releaseSelectionProblem(drafts);
    if (problem) { setSelectionError(t(problem)); return; }
    const route = { view: "release-review" as const, params: { kind: drafts[0]!.kind, id: drafts[0]!.id, ...(scope ? { application: scope } : {}) } };
    if (transfer) transfer("", route, { drafts }, () => {});
    else open({ ...route, params: { ...route.params, drafts: releaseDraftsValue(drafts) } });
  };
  const columns: ColumnDef<Api.DraftChange, any>[] = [
    { id: "selected", header: t("Select"), meta: { width: 50 }, cell: ({ row: { original: change } }) => <span onClick={event => event.stopPropagation()}><Checkbox ariaLabel={t("Select {title}", { title: change.title })} checked={checked.includes(changeKey(change))} onChange={value => { setSelectionError(""); setChecked(current => value ? [...current, changeKey(change)] : current.filter(key => key !== changeKey(change))); }} /></span> },
    { id: "title", header: t("Resource"), accessorKey: "title", meta: { width: 220 } },
    { id: "fields", header: t("Changed fields"), accessorFn: row => row.fields.map(field => field.name).join(", ") },
    { id: "state", header: t("Status"), accessorFn: row => row.diagnostic ? t("Needs review") : row.published ? t("Modified") : t("Draft"), cell: ({ getValue }) => <Tag label={getValue()} tone="warning" /> },
    { id: "by", header: t("Changed by"), accessorFn: row => row.changed.by },
    { id: "at", header: t("Changed at"), accessorFn: row => row.changed.at ? new Date(row.changed.at).toLocaleString() : "" },
  ];
  const kind = current && resourceKinds.find(kind => kind.kind === (current.variant === "automation" ? "automation" : draftChangeKind(current)));
  return <div className="flex min-h-0 flex-1 flex-col gap-4 p-4">
    <PageHeader title={t("Changes")} help={t("Saved changes compared with retained publications. Unsaved editor input is not included.")} />
    <div className="flex flex-wrap items-center gap-2">
      <Button variant="primary" disabled={!selected.length || selected.length > 128 || query.isError} onClick={() => review(selected)}>{t("Review selected changes")}</Button>
      <Button variant="ghost" onClick={() => open({ view: "release-history" })}>{t("Release history")}</Button>
      {selected.length > 128 && <p role="alert" className="text-sm text-warning">{t("A joint candidate accepts at most 128 drafts. Select a supported application closure.")}</p>}
      {selectionError && <p role="alert" className="text-sm text-warning">{selectionError}</p>}
    </div>
    {query.isError ? <p role="alert">{t("Saved changes could not be loaded.")}</p> : <DataTable data={rows} columns={columns} getRowId={changeKey} selectedId={current && changeKey(current)} loading={query.isPending}
      height={280} empty={t("No saved changes. Unsaved editor input is not included.")} onRowClick={row => confirmDiscard(() => setPicked({ kind: draftChangeKind(row), id: row.id }))} />}
    {current && <Panel role="region" aria-label={t("Saved definition differences")} className="grid min-w-0 gap-3">
      <h2 className="text-sm font-semibold">{current.title}</h2>
      <p className="break-all font-mono text-xs text-muted">{current.ref.app}/{current.ref.kind}/{current.ref.name}</p>
      {detail.isError ? <p role="alert">{t("Saved changes could not be loaded.")}</p> : detail.isPending ? <p>{t("Loading…")}</p> : <>
        {original?.diagnostic && <p role="alert" className="text-sm text-danger">{original.diagnostic}</p>}
        {original?.fields.map(field => <Disclosure key={field.name} summary={field.name}><div className="grid min-w-0 gap-3 md:grid-cols-2">
          <div className="min-w-0"><h3 className="mb-1 text-xs font-semibold">{t("Published")}</h3><pre className="max-h-64 overflow-auto whitespace-pre-wrap break-all text-xs">{field.before === undefined ? "—" : JSON.stringify(field.before, null, 2)}</pre></div>
          <div className="min-w-0"><h3 className="mb-1 text-xs font-semibold">{t("Saved draft")}</h3><pre className="max-h-64 overflow-auto whitespace-pre-wrap break-all text-xs">{field.after === undefined ? "—" : JSON.stringify(field.after, null, 2)}</pre></div>
        </div></Disclosure>)}
        {!original && <p className="text-sm text-muted">{t("No saved changes. Unsaved editor input is not included.")}</p>}
      </>}
      <div className="flex flex-wrap gap-2">
        {kind && <Button size="sm" onClick={() => open(resourceRoute(kind, current.id, scope))}>{t("Open")}</Button>}
        {current.type === "build.app" && <Button size="sm" onClick={() => open({ view: "project", params: { id: current.id } })}>{t("Open")}</Button>}
        <Button size="sm" onClick={() => review([current])}>{t("Review and publish…")}</Button>
      </div>
    </Panel>}
  </div>;
}
