import { useEffect, useState } from "react";
import { resourceEntryRoute, useHost, useReadQuery } from "@platform/app";
import type { Api } from "@platform/kernel";
import { Button, DataTable, Input, Notice, PageHeader, Panel, Tag, Toggles, t, useWorkspace } from "@platform/ui";
import type { Member } from "./shared";

export const markings = ["confidential", "restricted"];
export const markingLabels: Record<string, string> = { confidential: "Confidential", restricted: "Restricted" };
export const markingHelp: Record<string, string> = { confidential: "Opens data marked confidential.", restricted: "Opens data marked confidential or restricted." };


export function Markings() {
  const host = useHost(), { open } = useWorkspace();
  const query = useReadQuery<Api.Marking[]>("/v1/markings");
  const members = useReadQuery<Member[]>("/v1/members", undefined, host.holds("platform", "admin", "auditor"));
  const cleared = (host.me?.profile?.markings ?? []).includes;
  return <div className="grid gap-3">
    <PageHeader title={t("Markings")} help={t("Grant classification and compartment clearances to members or teams. Every marking is required, including for administrators.")} actions={host.can("platform.marking.save") && <Button onClick={() => open({ view: "marking", params: { id: "new" } })}>{t("New marking")}</Button>} />
    {query.isError && <Notice tone="danger" message={t("Markings could not be loaded.")} />}
    <DataTable data={query.data ?? []} getRowId={row => row.id} loading={query.isLoading} onRowClick={row => open({ view: "marking", params: { id: row.rid } })} columns={[
      { accessorKey: "title", header: t("Marking") },
      { id: "kind", header: t("Kind"), cell: ({ row }) => t(row.original.classification ? "Classification" : "Compartment") },
      { id: "members", header: t("Cleared members"), accessorFn: row => (members.data ?? []).filter(member => member.markings?.includes(row.id)).length },
      { id: "teams", header: t("Cleared teams"), accessorFn: row => row.teams?.length ?? 0 },
      { id: "you", header: t("Your clearance"), accessorFn: row => cleared(row.id) ? t("Cleared") : t("Not cleared"), cell: ({ row }) => cleared(row.original.id) ? <Tag label={t("Cleared")} tone="success" /> : <Tag label={t("Not cleared")} /> },
      { id: "status", header: t("Status"), cell: ({ row }) => <Tag label={t(row.original.archived ? "Archived" : "Active")} /> },
    ]} />
  </div>;
}

type Draft = { id: string; title: string; members: string[]; teams: string[]; declassifiers: string[]; declassifierTeams: string[] };
const empty = (): Draft => ({ id: "", title: "", members: [], teams: [], declassifiers: [], declassifierTeams: [] });

export function MarkingEditor({ id }: { id: string }) {
  const host = useHost(), { open } = useWorkspace();
  const query = useReadQuery<Api.Marking[]>("/v1/markings");
  const current = query.data?.find(marking => marking.id === id || marking.rid === id);
  const resources = useReadQuery<Api.ResourceEntry[]>(`/v1/markings/${encodeURIComponent(current?.id ?? "")}/resources`, undefined, !!current && host.holds("platform", "admin", "auditor"));
  const members = useReadQuery<Member[]>("/v1/members", undefined, host.holds("platform", "admin", "auditor"));
  const access = useReadQuery<Api.AccessConfig>("/v1/access", undefined, host.holds("platform", "admin", "auditor"));
  const [draft, setDraft] = useState(empty), [busy, setBusy] = useState(false), [error, setError] = useState("");
  useEffect(() => { setDraft(current ? { id: current.id, title: current.title, members: current.members ?? [], teams: current.teams ?? [], declassifiers: current.declassifiers ?? [], declassifierTeams: current.declassifierTeams ?? [] } : empty()); }, [id, current]);
  const change = (values: Partial<Draft>) => setDraft(previous => ({ ...previous, ...values }));
  const submit = async (schema: string) => {
    setBusy(true); setError("");
    const { id: target, ...payload } = draft;
    if (await host.decide(schema, { type: "platform.marking", id: target }, schema.endsWith(".archive") ? {} : payload, { expectedRevision: current?.revision ?? 0, quiet: true, onRefused: setError })) { await query.refetch(); open({ view: "markings" }); }
    setBusy(false);
  };
  if (id !== "new" && !current) return <Notice tone="warning" message={t(query.isError ? "Markings could not be loaded." : query.isPending ? "Loading…" : "This marking is unavailable.")} />;
  const memberOptions = (members.data ?? []).map(member => ({ value: member.id, label: member.profile.displayName || member.id }));
  const teamOptions = (access.data?.teams ?? []).map(team => ({ value: team.id, label: team.name }));
  return <div className="grid gap-3">
    <PageHeader title={current?.title || t("New marking")} crumbs={[{ label: t("Markings"), onClick: () => open({ view: "markings" }) }]} meta={current?.rid && <code>{current.rid}</code>} actions={<>
      {current && !current.classification && !current.archived && host.can("platform.marking.archive") && <Button variant="danger" disabled={busy} onClick={() => void submit("platform.marking.archive")}>{t("Archive")}</Button>}
      {host.can("platform.marking.save") && <Button variant="primary" disabled={busy || !draft.id || !draft.title.trim()} onClick={() => void submit("platform.marking.save")}>{t(current?.archived ? "Restore" : "Save")}</Button>}
    </>} />
    {error && <Notice tone="danger" message={error} />}
    <fieldset disabled={busy || !host.can("platform.marking.save")} className="grid gap-3">
      <Panel className="grid gap-2"><label>{t("Marking ID")}<Input aria-label={t("Marking ID")} value={draft.id} disabled={!!current} onChange={event => change({ id: event.target.value })} /></label><label>{t("Marking title")}<Input aria-label={t("Marking title")} value={draft.title} onChange={event => change({ title: event.target.value })} /></label></Panel>
      <Panel role="region" aria-label={t("Data clearance")} title={t("Data clearance")} className="grid gap-2"><span>{t("Members")}</span><Toggles options={memberOptions} value={draft.members} onChange={members => change({ members })} /><span>{t("Teams")}</span><Toggles options={teamOptions} value={draft.teams} onChange={teams => change({ teams })} /></Panel>
      <Panel role="region" aria-label={t("Declassification permission")} title={t("Declassification permission")} className="grid gap-2" help={t("Clearance is also required. Declassification runs only as a pipeline step and is recorded in the audit trail.")}><span>{t("Members")}</span><Toggles options={memberOptions} value={draft.declassifiers} onChange={declassifiers => change({ declassifiers })} /><span>{t("Teams")}</span><Toggles options={teamOptions} value={draft.declassifierTeams} onChange={declassifierTeams => change({ declassifierTeams })} /></Panel>
    </fieldset>
    {current && <Panel role="region" aria-label={t("Marked resources")} title={t("Marked resources")}><DataTable data={resources.data ?? []} getRowId={row => row.id} loading={resources.isLoading} onRowClick={row => { if (row.canOpen) open(resourceEntryRoute(row, host.opens)); }} columns={[{ accessorKey: "title", header: t("Name") }, { id: "kind", header: t("Kind"), accessorFn: row => row.ref.kind }]} /></Panel>}
  </div>;
}
