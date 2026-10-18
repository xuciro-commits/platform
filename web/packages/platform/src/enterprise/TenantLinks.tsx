import { useHost, useRecordInventory } from "@platform/app";
import { Button, DataTable, Dialog, Form, Input, Notice, Select, t, useUnsavedChanges } from "@platform/ui";
import { useState } from "react";
import { MODEL, live, type Model, type TenantLink } from "./model";

type Preview = { link: string; base: string; content: string; connectionRevision: number; slice: unknown; changes: { kind: string; id: string; name: string }[] };
type Connection = { id: string; title?: string; name?: string; archived?: boolean };

/** Connections keep network credentials; previewed bytes are the import payload. */
export function TenantLinks({ model, day }: { model: Model; day: string }) {
  const host = useHost();
  const connections = useRecordInventory<Connection>("build.connection");
  const [editing, setEditing] = useState<TenantLink>();
  const [existingID, setExistingID] = useState<string>();
  const [preview, setPreview] = useState<Preview>();
  const [busy, setBusy] = useState(false), [error, setError] = useState("");
  const [removing, setRemoving] = useState<TenantLink>();
  useUnsavedChanges(!!editing, () => setEditing(undefined), busy);
  const readPreview = async (link: TenantLink) => {
    setBusy(true); setError(""); setPreview(undefined);
    try { setPreview(await host.client.get<Preview>(`/v1/enterprise-links/${encodeURIComponent(link.id)}/preview` + "\n{}", true)); }
    catch (cause) { setError(cause instanceof Error ? cause.message : t("The tenant link could not be previewed.")); }
    finally { setBusy(false); }
  };
  return <div className="grid content-start gap-4 p-4">
    <div className="flex flex-wrap items-start justify-between gap-3"><div><h2 className="text-base font-semibold">{t("Tenant links")}</h2><p className="max-w-2xl text-sm text-muted">{t("Link an organisation to another tenant through an authorized connection. Preview changes before importing its published enterprise structure.")}</p></div>{host.can("enterprise.tenant-link.save") && <Button onClick={() => { setExistingID(undefined); setEditing({ id: "", organization: "", tenant: "", root: "", connection: "" }); }}>{t("Add tenant link")}</Button>}</div>
    {error && <Notice tone="warning" message={error} />}
    {connections.isError && <Notice tone="info" message={t("Connections are not readable with your current access. Ask a builder to configure a connection.")} />}
    <DataTable data={model.tenantLinks ?? []} getRowId={link => link.id} searchable={false} height={320} columns={[
      { id: "organization", header: t("Organisation"), accessorFn: link => model.elements.find(element => element.id === link.organization)?.name ?? link.organization },
      { accessorKey: "tenant", header: t("Source tenant ID") }, { accessorKey: "root", header: t("Source root ID") }, { accessorKey: "connection", header: t("Connection") },
      { accessorKey: "updatedAt", header: t("Last imported") },
      { id: "actions", header: t("Actions"), cell: ({ row }) => <div className="flex flex-wrap gap-1">
        {host.can("enterprise.tenant-link.save") && <Button size="sm" variant="ghost" onClick={() => { setExistingID(row.original.id); setEditing({ ...row.original }); }}>{t("Edit")}</Button>}
        {host.can("enterprise.slice.import") && <Button size="sm" variant="ghost" disabled={busy} onClick={() => void readPreview(row.original)}>{t("Preview import")}</Button>}
        {host.can("enterprise.tenant-link.remove") && <Button size="sm" variant="ghost" onClick={() => setRemoving(row.original)}>{t("Remove")}</Button>}
      </div> },
    ]} />
    {editing && <Dialog open onOpenChange={open => !open && !busy && setEditing(undefined)} title={t("Tenant link settings")}><Form className="grid gap-3" onSubmit={async () => {
      setBusy(true); setError(""); const { id, organization, tenant, root, connection } = editing;
      try { if (await host.decide("enterprise.tenant-link.save", { type: "enterprise.tenant-link", id }, { organization, tenant, root, connection }, { onRefused: setError })) setEditing(undefined); }
      finally { setBusy(false); }
    }}>
      <label className="grid gap-1 text-xs">{t("Link ID")}<Input value={editing.id} disabled={!!existingID} onChange={event => setEditing({ ...editing, id: event.target.value })} required /></label>
      <label className="grid gap-1 text-xs">{t("Organisation")}<Select value={editing.organization} onChange={event => setEditing({ ...editing, organization: event.target.value })} required><option value="">{t("Choose an organisation")}</option>{model.elements.filter(element => element.stereotype === "ActualOrganization" && live(element, day)).map(element => <option key={element.id} value={element.id}>{element.name}</option>)}</Select></label>
      <label className="grid gap-1 text-xs">{t("Source tenant ID")}<Input value={editing.tenant} onChange={event => setEditing({ ...editing, tenant: event.target.value })} required /></label>
      <label className="grid gap-1 text-xs">{t("Source root ID")}<Input value={editing.root} onChange={event => setEditing({ ...editing, root: event.target.value })} required /></label>
      <label className="grid gap-1 text-xs">{t("Connection")}<Select value={editing.connection} onChange={event => setEditing({ ...editing, connection: event.target.value })} required><option value="">{t("Choose a connection")}</option>{connections.data?.records.filter(connection => !connection.archived).map(connection => <option key={connection.id} value={connection.id}>{connection.title || connection.name || connection.id}</option>)}{editing.connection && !connections.data?.records.some(connection => connection.id === editing.connection) && <option value={editing.connection}>{editing.connection}</option>}</Select></label>
      {error && <p role="alert" className="text-xs text-danger">{error}</p>}
      <div className="flex justify-end gap-2"><Button variant="ghost" onClick={() => setEditing(undefined)}>{t("Cancel")}</Button><Button type="submit" disabled={!editing.id || !editing.organization || !editing.tenant || !editing.root || !editing.connection}>{t("Save")}</Button></div>
    </Form></Dialog>}
    {preview && <Dialog open wide onOpenChange={open => !open && !busy && setPreview(undefined)} title={t("Review tenant import")}><Form className="grid gap-3" onSubmit={async () => {
      setBusy(true); setError("");
      try { const { link, base, content, connectionRevision, slice } = preview; if (await host.decide("enterprise.slice.import", { type: MODEL, id: link }, { link, base, content, connectionRevision, slice }, { onRefused: setError })) setPreview(undefined); }
      finally { setBusy(false); }
    }}>
      <p className="text-sm text-muted">{t("Confirm imports exactly the previewed content. If the model or connection changed, preview again.")}</p>
      <DataTable data={preview.changes} getRowId={change => `${change.kind}:${change.id}`} searchable={false} height={320} columns={[{ accessorKey: "kind", header: t("Change") }, { accessorKey: "name", header: t("Name") }, { accessorKey: "id", header: t("Identity") }]} />
      {error && <p role="alert" className="text-xs text-danger">{error}</p>}
      <div className="flex justify-end gap-2"><Button variant="ghost" disabled={busy} onClick={() => setPreview(undefined)}>{t("Cancel")}</Button><Button type="submit" disabled={busy}>{t("Confirm import")}</Button></div>
    </Form></Dialog>}
    {removing && <Dialog open onOpenChange={open => !open && setRemoving(undefined)} title={t("Remove tenant link")}><Form className="grid gap-3" onSubmit={async () => { if (await host.decide("enterprise.tenant-link.remove", { type: "enterprise.tenant-link", id: removing.id }, {}, { onRefused: setError })) setRemoving(undefined); }}><p className="text-sm text-muted">{t("Removing a link stops future imports. Existing imported history remains.")}</p>{error && <p role="alert">{error}</p>}<div className="flex justify-end gap-2"><Button variant="ghost" onClick={() => setRemoving(undefined)}>{t("Cancel")}</Button><Button variant="danger" type="submit">{t("Remove")}</Button></div></Form></Dialog>}
  </div>;
}
