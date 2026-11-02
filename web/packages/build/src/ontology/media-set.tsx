import { useState } from "react";
import { useHost, useReadQuery } from "@platform/app";
import { Button, DataTable, FilePicker, Input, PageHeader, Panel, Select, StructuredEditor, Textarea, t, type ColumnDef } from "@platform/ui";
import { ResourceControls } from "../editor/workbench";
import { ResourceList } from "../editor/ResourceList";
import { useDefinitionDraft } from "../session/definition-draft";
import { useApplicationWorkspace } from "../projects/application-scope";

type Item = { hash: string; name: string; contentType: string; size: number; by: string; at: string };
type Draft = { id: string; revision: number; name: string; title: string; description?: string; version?: number; items?: Item[] };
type Version = { id: string; version: number; items: Item[] };
const empty = (): Draft => ({ id: "", revision: 0, name: "", title: "" });
export function MediaSets() {
  const { source } = useHost(), { open } = useApplicationWorkspace();
  return <div className="grid gap-3"><PageHeader title={t("Media sets")} actions={<Button variant="primary" onClick={() => open({ view: "media-set", params: { id: "new" } })}>{t("New media set")}</Button>} /><ResourceList source={source} type="build.mediaset" fields={["title", "name", "version"]} onOpen={record => open({ view: "media-set", params: { id: record.id } })} /></div>;
}
export function MediaSetEditor({ id }: { id: string }) {
  const { client, decide } = useHost(), { open } = useApplicationWorkspace();
  const { draft, structured, query, dirty, busy, error, setError, patch, perform, save, discardChanges } = useDefinitionDraft<Draft>({ id, type: "build.mediaset", view: "media-set", empty, failure: t("The media set could not be saved or loaded. Your draft is still here."), payload: ({ name, title, description }) => ({ name, title, description: description ?? "" }) });
  const [version, setVersion] = useState(0);
  const versions = useReadQuery<{ records: Version[] }>(`/v1/records/build.mediasetversion?domain=${encodeURIComponent(JSON.stringify([["mediaSet", "=", id]]))}&sort=-version&limit=200`, 15000, id !== "new");
  if (id !== "new" && !draft.id) return <PageHeader title={t("Media sets")} description={query.isError ? t("The media set could not be loaded.") : t("Loading…")} />;
  const items = version ? versions.data?.records.find(row => row.version === version)?.items ?? [] : draft.items ?? [];
  return <div className="grid gap-3"><PageHeader crumbs={[{ label: t("Media sets"), onClick: () => open({ view: "media-set" }) }]} title={draft.title || t("New media set")} actions={<>{draft.id && !dirty && <FilePicker disabled={busy} onFile={file => void perform(async () => { const upload = await client.upload(file, file.name); if (await decide("build.mediaset.add", { type: "build.mediaset", id }, upload, { expectedRevision: draft.revision, onRefused: setError })) await query.refetch(); })}>{t("Upload file")}</FilePicker>}<ResourceControls type="build.mediaset" record={draft} dirty={dirty} busy={busy} route={{ view: "media-set", params: { id } }} onCancel={discardChanges} onSave={() => perform(save)} /></>} />
    {error && <Panel role="alert" className="text-danger">{error}</Panel>}<StructuredEditor {...structured}><Panel title={t("Media set")} className="grid gap-3"><label className="grid gap-1 text-xs">{t("Name")}<Input value={draft.name} onChange={event => patch({ name: event.target.value })} /></label><label className="grid gap-1 text-xs">{t("Title")}<Input value={draft.title} onChange={event => patch({ title: event.target.value })} /></label><label className="grid gap-1 text-xs">{t("Description")}<Textarea value={draft.description ?? ""} onChange={event => patch({ description: event.target.value })} /></label></Panel></StructuredEditor>
    {draft.id && <Panel title={t("Files")} className="grid gap-2"><Select aria-label={t("Version")} value={String(version)} onChange={event => setVersion(Number(event.target.value))}><option value="0">{t("Current version")}</option>{versions.data?.records.map(row => <option key={row.id} value={row.version}>v{row.version}</option>)}</Select><DataTable data={items} getRowId={item => item.hash} searchable={false} columns={[
      { id: "name", header: t("Name"), accessorFn: item => item.name }, { id: "size", header: t("Size"), accessorFn: item => item.size }, { id: "type", header: t("Type"), accessorFn: item => item.contentType },
      { id: "actions", header: t("Actions"), cell: context => <div className="flex gap-2"><Button size="sm" onClick={() => void perform(async () => { const file = await client.downloadMedia(id, context.row.original.hash, version || undefined); const url = URL.createObjectURL(file.blob); const link = document.createElement("a"); link.href = url; link.download = context.row.original.name; link.click(); URL.revokeObjectURL(url); })}>{t("Download")}</Button>{!version && <Button size="sm" disabled={dirty || busy} onClick={() => void perform(async () => { if (await decide("build.mediaset.remove", { type: "build.mediaset", id }, { hash: context.row.original.hash }, { expectedRevision: draft.revision, onRefused: setError })) await query.refetch(); })}>{t("Remove")}</Button>}</div> },
    ] as ColumnDef<Item, unknown>[]} /></Panel>}</div>;
}
