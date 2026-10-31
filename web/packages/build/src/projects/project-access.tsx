import { useHost, useReadQuery } from "@platform/app";
import type { Api } from "@platform/kernel";
import { Button, ContextHelp, DataTable, Notice, Select, t } from "@platform/ui";
import { Trash2 } from "lucide-react";
import { useState } from "react";

const roles = ["owner", "editor", "viewer"] as const;
const roleLabels: Record<string, string> = { owner: "Owner", editor: "Editor", viewer: "Viewer" };
const roleHelp: Record<string, string> = {
  owner: "Edits the project's resources and manages who has access.",
  editor: "Edits the project's resources.",
  viewer: "Opens the project's resources without editing them.",
};

export function ProjectAccess({ project, title }: { project: string; title: string }) {
  const { decide, me } = useHost();
  const query = useReadQuery<Api.ProjectAccessView>(`/v1/projects/${encodeURIComponent(project)}/access`);
  const view = query.data;
  const [person, setPerson] = useState("");
  const [role, setRole] = useState<string>("editor");
  const [busy, setBusy] = useState(false);
  const members = view?.members ?? [];
  const name = (id: string) => view?.people?.find(item => item.id === id)?.name ?? id;
  const save = async (next: Api.ProjectMember[]) => {
    setBusy(true);
    try { if (await decide("platform.project.save", { type: "platform.project", id: project }, { title, members: next })) await query.refetch(); }
    finally { setBusy(false); }
  };
  const candidates = (view?.people ?? []).filter(item => !members.some(member => member.member === item.id));
  const firstRole = members.length ? role : "owner";
  return <div className="grid max-w-3xl content-start gap-3 p-4">
    <div className="flex items-center gap-2">
      <h2 className="min-w-0 flex-1 text-sm font-semibold">{t("Project access")}</h2>
      <ContextHelp title={t("Project access")}>
        {roles.map(item => <p key={item}><strong>{t(roleLabels[item]!)}</strong>: {t(roleHelp[item]!)}</p>)}
        <p>{t("Builders and administrators reach every project. Publishing stays with builders.")}</p>
      </ContextHelp>
    </div>
    {query.isError && <Notice tone="danger" role="alert" message={t("Project access could not be loaded.")} />}
    <DataTable data={members} getRowId={item => item.member} searchable={false} loading={query.isPending} height={Math.max(150, Math.min(360, 34 + members.length * 36))} rowHeight={36}
      empty={t("No members yet. Builders and administrators reach every project.")} columns={[
        { id: "person", header: t("Member"), accessorFn: item => name(item.member), cell: ({ row: { original } }) => <span className="truncate">{name(original.member)}{original.member === me.profile.id ? ` · ${t("you")}` : ""}</span> },
        { id: "role", header: t("Role"), meta: { width: 180 }, accessorFn: item => item.role ?? "", cell: ({ row: { original } }) => view?.manage
          ? <Select aria-label={t("Role of {member}", { member: name(original.member) })} value={original.role ?? ""} disabled={busy} onChange={event => void save(members.map(item => item.member === original.member ? { ...item, role: event.target.value } : item))}>
              {roles.map(item => <option key={item} value={item}>{t(roleLabels[item]!)}</option>)}
            </Select>
          : <span>{t(roleLabels[original.role ?? ""] ?? original.role ?? "")}</span> },
        ...(view?.manage ? [{ id: "remove", header: "", meta: { width: 48 }, cell: ({ row: { original } }: { row: { original: Api.ProjectMember } }) =>
          <Button size="icon" variant="ghost" aria-label={t("Remove {member}", { member: name(original.member) })} title={t("Remove")} disabled={busy} onClick={() => void save(members.filter(item => item.member !== original.member))}><Trash2 /></Button> }] : []),
      ]} />
    {view?.manage && <div className="flex flex-wrap items-center gap-2">
      <Select aria-label={t("Person")} value={person} className="w-64" disabled={busy} onChange={event => setPerson(event.target.value)}>
        <option value="">{t("Choose a person…")}</option>
        {candidates.map(item => <option key={item.id} value={item.id}>{item.name}</option>)}
      </Select>
      <Select aria-label={t("Role")} value={firstRole} className="w-36" disabled={busy || !members.length} onChange={event => setRole(event.target.value)}>
        {roles.map(item => <option key={item} value={item}>{t(roleLabels[item]!)}</option>)}
      </Select>
      <Button variant="primary" disabled={busy || !person} onClick={() => void save([...members, { member: person, role: firstRole }]).then(() => setPerson(""))}>{t("Add member")}</Button>
      {!members.length && <span className="text-xs text-muted">{t("The first member is the project's owner.")}</span>}
    </div>}
  </div>;
}
