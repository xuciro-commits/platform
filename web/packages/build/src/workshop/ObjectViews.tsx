import { useRef, useState } from "react";
import { Eye, Plus } from "lucide-react";
import { newId, useHost } from "@platform/app";
import type { Entry } from "@platform/api";
import { Button, DataTable, Dialog, Form, Input, Notice, PageHeader, Select, t, useUnsavedChanges, useWorkspace, type ColumnDef } from "@platform/ui";
import { useDraftRecord } from "../branches/draft-record";
import type { Project } from "../projects/project";
import { useAuthoringModel } from "../ontology/authoring-model";
import { DraftStatus } from "../editor/workbench";
import { createdPageMembership } from "./created-page";
import { objectViewDraft, objectViewKey, useObjectViews } from "./object-views-model";

export function ObjectViews() {
  const host = useHost(), { open } = useWorkspace();
  const inventory = useObjectViews();
  const [search, setSearch] = useState(""), [creating, setCreating] = useState(false), [busy, setBusy] = useState(false);
  const close = useRef<(() => void) | undefined>(undefined);
  const rows = inventory.rows.filter(({ module, page }) => `${module.title} ${page.title} ${page.name} ${page.object}`.toLowerCase().includes(search.toLowerCase()));
  type Row = typeof rows[number];
  const columns: ColumnDef<Row>[] = [
    { id: "name", header: t("Object view"), meta: { width: 320, pin: "left" }, accessorFn: row => row.page.title, cell: ({ row }) => <span className="flex min-w-0 items-center gap-2"><Eye className="size-4 shrink-0 text-muted" /><span className="grid min-w-0"><span className="truncate font-medium">{row.original.page.title}</span><span className="truncate font-mono text-2xs text-muted">{row.original.page.name}</span></span></span> },
    { id: "module", header: t("Project"), meta: { width: 220 }, accessorFn: row => row.module.title },
    { id: "object", header: t("Object type"), meta: { width: 220 }, accessorFn: row => host.source.entity(row.page.object)?.title ?? row.page.object },
    { id: "state", header: t("Status"), meta: { width: 120 }, accessorFn: row => row.page.state, cell: ({ row }) => <DraftStatus state={row.original.page.state} /> },
    { id: "changed", header: t("Updated"), meta: { width: 180 }, accessorFn: row => row.page.changed?.at ?? "—" },
  ];
  const allowed = host.can("build.page.create") && host.holds("build", "builder");
  return <div className="flex min-h-full flex-col gap-3">
    <PageHeader title={t("Object views")} meta={t("{count} object views", { count: rows.length })} help={t("Workshop pages that receive the current object. Bind a view in the object's Display settings, then publish the object, page and module together.")}
      actions={<><Input type="search" aria-label={t("Search object views")} placeholder={t("Search object views")} value={search} onChange={event => setSearch(event.target.value)} className="w-56" /><Button variant="primary" disabled={!allowed} title={!allowed ? t("Creating an object view requires builder and page creation permission.") : undefined} onClick={() => setCreating(true)}><Plus />{t("New object view")}</Button></>} />
    {inventory.isError ? <Notice tone="danger" message={t("Object views could not be loaded. Reload before choosing a view.")} /> : <div className="min-h-0 flex-1"><DataTable data={rows} columns={columns} getRowId={row => objectViewKey(row.binding)} height="100%" searchable={false} loading={inventory.isLoading}
      onRowClick={({ module, page }) => open({ view: "module", params: { id: module.id, application: module.id, page: page.id } })} empty={search ? t("No object views match.") : t("No object views yet. Create a page for the current object.")} /></div>}
    <Dialog open={creating} onOpenChange={next => { if (!next && !busy) close.current?.(); }} title={t("New object view")}>
      {creating && <ObjectViewCreate modules={(inventory.modules.data?.records ?? []).filter(module => !module.archived)} onBusy={setBusy} closeGuard={close} onClose={() => setCreating(false)} />}
    </Dialog>
  </div>;
}

function ObjectViewCreate({ modules, onBusy, closeGuard, onClose }: { modules: Project[]; onBusy: (busy: boolean) => void; closeGuard: { current: (() => void) | undefined }; onClose: () => void }) {
  const host = useHost(), { open } = useWorkspace(), model = useAuthoringModel();
  const [moduleID, setModuleID] = useState(""), [objectType, setObjectType] = useState(""), [name, setName] = useState(""), [title, setTitle] = useState("");
  const [pageID] = useState(() => newId("PAGE"));
  const [busy, setBusy] = useState(false), [pending, setPending] = useState(false), [created, setCreated] = useState(false), [error, setError] = useState("");
  const lock = useRef(false), request = useRef<(ReturnType<typeof objectViewDraft> & { project?: string }) | undefined>(undefined), outcome = useRef<Entry | undefined>(undefined);
  const { query } = useDraftRecord<Project>("build.app", moduleID, !!moduleID);
  const { markSaved, confirmOwnDiscard } = useUnsavedChanges(!!moduleID || !!name || !!title || !!objectType || pending || created, () => {}, busy);
  closeGuard.current = () => { if (!lock.current) confirmOwnDiscard(onClose); };
  const ontology = new Set((model.drafts.data?.records ?? []).filter(record => !record.archived).map(record => `build.${record.name}`));
  const objects = model.entities.filter(entity => ontology.has(entity.type));
  const object = objects.find(entity => entity.type === objectType);
  const valid = !!query.data?.record && !query.isLoading && !query.isPlaceholderData && !query.isError && !query.data.record.archived && objects.some(entity => entity.type === objectType) && /^[a-z][a-z0-9]*$/.test(name) && !!title.trim();
  const locked = busy || pending || created;
  const create = async () => {
    if (lock.current || !host.can("build.page.create") || !host.holds("build", "builder") || !request.current && !created && !valid) return;
    lock.current = true; setBusy(true); onBusy(true); setError("");
    try {
      if (!created) {
        request.current ??= { ...objectViewDraft(object!, name, title.trim(), model.actions.filter(action => action.target === objectType).map(action => action.schema)), project: moduleID };
        const confirmed = await host.decide("build.page.create", { type: "build.page", id: pageID }, request.current, { expectedRevision: outcome.current?.submission.expectedRevision ?? 0, schemaVersion: outcome.current?.schemaVersion, originalKey: outcome.current?.submission.idempotencyKey, onRefused: setError, onOutcome: entry => {
          outcome.current = entry;
          setPending(["SUBMISSION_STATE_PENDING", "SUBMISSION_STATE_SENDING", "SUBMISSION_STATE_UNKNOWN"].includes(entry.state));
          if (entry.state === "SUBMISSION_STATE_REJECTED" || entry.state === "SUBMISSION_STATE_CONFLICT") { request.current = undefined; outcome.current = undefined; }
        } });
        if (!confirmed) return;
        setCreated(true); setPending(false); request.current = undefined;
      }
      const refreshed = await query.refetch(), module = refreshed.isSuccess ? refreshed.data?.record : undefined;
      if (!module || module.archived) { setError(t("The page was created but could not be added to this module. Retry adding the same page.")); return; }
      const { record, pages } = await createdPageMembership(host.client, pageID, module.pages ?? []);
      if (!module.pages?.includes(record.name) && !await host.decide("build.app.edit", { type: "build.app", id: moduleID }, { pages }, { expectedRevision: module.revision, quiet: true, onRefused: setError })) return;
      markSaved(); onClose(); open({ view: "module", params: { id: moduleID, application: moduleID, page: pageID } });
    } catch (failure) {
      if (request.current) setPending(true);
      setError(failure instanceof Error ? failure.message : t("The page was created but could not be added to this module. Retry adding the same page."));
    } finally { lock.current = false; setBusy(false); onBusy(false); }
  };
  return <Form onSubmit={() => create()} className="grid gap-3 p-4">
    <label className="grid gap-1 text-xs">{t("Module")}<Select value={moduleID} disabled={locked} onChange={event => setModuleID(event.target.value)} required><option value="">{t("Choose a module")}</option>{modules.map(module => <option key={module.id} value={module.id}>{module.title} · {module.name}</option>)}</Select></label>
    <label className="grid gap-1 text-xs">{t("Object type")}<Select value={objectType} disabled={locked} onChange={event => setObjectType(event.target.value)} required><option value="">{t("Choose an object")}</option>{objects.map(object => <option key={object.type} value={object.type}>{object.title} · {object.type}</option>)}</Select></label>
    <label className="grid gap-1 text-xs">{t("Page name")}<Input aria-label={t("Page name")} value={name} disabled={locked} onChange={event => setName(event.target.value.trim())} required /><span className="text-muted">{t("Use lowercase letters and digits; start with a letter.")}</span></label>
    <label className="grid gap-1 text-xs">{t("Page title")}<Input value={title} disabled={locked} onChange={event => setTitle(event.target.value)} required /></label>
    <p className="text-xs text-muted">{t("The page starts with a currentObject record input and a record view. Saved object drafts are supported; publish all bindings together.")}</p>
    {(model.drafts.isError || query.isError) && <Notice tone="danger" message={t("Saved object drafts or module settings could not be loaded.")} />}
    {!model.drafts.isPending && !model.drafts.isError && !objects.length && <p className="text-xs text-muted">{t("Create and save an object type before creating its view.")}</p>}
    {!modules.length && <p className="text-xs text-muted">{t("No projects yet. Create one in Projects.")}</p>}
    {pending && <Notice tone="warning" message={t("Page creation is unconfirmed. Its original ID and payload are retained; retry does not create a second page.")} />}
    {created && <Notice tone="warning" message={t("A page was created but its module membership is unconfirmed. Retry the same page; creating another page would duplicate the work.")} />}
    {(pending || created) && <code className="text-xs text-muted">{pageID}</code>}
    {error && <Notice tone="danger" role="alert" message={error} />}
    <div className="flex justify-end gap-2"><Button variant="ghost" disabled={busy} onClick={() => closeGuard.current?.()}>{t("Cancel")}</Button>{created && <Button variant="ghost" disabled={busy} onClick={() => { markSaved(); onClose(); }}>{t("Keep page without module membership")}</Button>}<Button type="submit" variant="primary" loading={busy} disabled={!pending && !created && !valid}>{created ? t("Retry adding created page") : pending ? t("Retry original page creation") : t("Create object view")}</Button></div>
  </Form>;
}
