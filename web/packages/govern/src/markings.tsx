import { useHost, useReadQuery } from "@platform/app";
import { Button, DataTable, Notice, PageHeader, t, useWorkspace, type ColumnDef } from "@platform/ui";
import type { Member } from "./shared";

export const markings = ["confidential", "restricted"];
export const markingLabels: Record<string, string> = { confidential: "Confidential", restricted: "Restricted" };
export const markingHelp: Record<string, string> = { confidential: "Opens data marked confidential.", restricted: "Opens data marked confidential or restricted." };

type Row = { marking: string; cleared: Member[] };

export function Markings() {
  const { holds } = useHost();
  const { open } = useWorkspace();
  const members = useReadQuery<Member[]>("/v1/members", undefined, holds("platform", "admin", "auditor"));
  const rows: Row[] = markings.map(marking => ({ marking, cleared: (members.data ?? []).filter(member => (member.grants ?? []).some(grant => grant.app === "platform" && grant.role === marking)) }));
  const columns: ColumnDef<Row>[] = [
    { id: "marking", header: t("Marking"), meta: { width: 160 }, accessorFn: row => t(markingLabels[row.marking]!), cell: ({ row }) => <span className="font-medium">{t(markingLabels[row.original.marking]!)}</span> },
    { id: "help", header: t("What it opens"), meta: { width: 320 }, accessorFn: row => t(markingHelp[row.marking]!), cell: ({ getValue }) => <span className="text-muted">{getValue() as string}</span> },
    { id: "count", header: t("Cleared members"), meta: { width: 140 }, accessorFn: row => row.cleared.length, cell: ({ getValue }) => <span className="tabular-nums">{getValue() as number}</span> },
    { id: "members", header: t("Members"), accessorFn: row => row.cleared.map(member => member.id).join(" "), cell: ({ row }) => <span className="flex flex-wrap gap-1">
      {row.original.cleared.map(member => <Button key={member.id} size="sm" variant="ghost" onClick={() => open({ view: "member", params: { id: member.id } })}>{member.profile.displayName || member.id}</Button>)}
    </span> },
  ];
  return <div className="grid gap-3">
    <PageHeader title={t("Markings")} meta={t("{count} markings", { count: markings.length })} help={t("Marked data opens only to members cleared for every marking it carries. Markings follow data lineage, and no role lifts them. Clear a member on their page.")} />
    {members.isError && <Notice tone="warning" role="alert" message={t("Members could not be loaded.")} />}
    <DataTable data={rows} columns={columns} getRowId={row => row.marking} searchable={false} loading={members.isLoading} height={200} />
  </div>;
}
