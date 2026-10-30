import type { Api } from "@platform/kernel";
import { Button, DataTable, PageHeader, StatusTag, formatStamp, t, useWorkspace, type ColumnDef } from "@platform/ui";
import { useState } from "react";
import { useBranches } from "./api";
import { NewBranchDialog } from "./BranchDialogs";
import { useCurrentBranch } from "./current";
import { branchStatuses } from "./Proposal";

export function Branches() {
  const { open } = useWorkspace();
  const list = useBranches();
  const { id: current, select } = useCurrentBranch();
  const [creating, setCreating] = useState(false);
  const rows = [...(list.data ?? [])].sort((a, b) => (b.changed.at ?? "").localeCompare(a.changed.at ?? ""));
  const columns: ColumnDef<Api.BranchDetail, any>[] = [
    { id: "title", header: t("Branch"), accessorFn: row => row.title, cell: ({ row: { original } }) => <span className="flex min-w-0 items-baseline gap-2"><span className="truncate">{original.title}</span><span className="font-mono text-xs text-muted">{original.name}</span>{original.id === current && <span className="text-xs text-primary">{t("current")}</span>}</span>, meta: { width: 280 } },
    { id: "state", header: t("Status"), accessorFn: row => row.state, cell: ({ row: { original } }) => <StatusTag status={original.state} registry={branchStatuses} />, meta: { width: 110 } },
    { id: "resources", header: t("Changed resources"), accessorFn: row => row.resources.length, meta: { width: 140 } },
    { id: "reviews", header: t("Approvals"), accessorFn: row => row.proposal ? `${row.proposal.reviews.filter(review => review.decision === "approve").length} / ${row.proposal.reviewers.length}` : "—", meta: { width: 110 } },
    { id: "by", header: t("Changed by"), accessorFn: row => row.changed.by ?? "" },
    { id: "at", header: t("Changed at"), accessorFn: row => formatStamp(row.changed.at) },
  ];
  return <div className="flex min-h-0 flex-1 flex-col gap-4 p-4">
    <PageHeader title={t("Branches and proposals")} help={t("A branch collects definition changes. Review them, propose the branch, and merge it once its checks and approvals pass. Merging seals a release candidate; activating it publishes.")}
      actions={<Button variant="primary" onClick={() => setCreating(true)}>{t("New branch")}</Button>} />
    {list.isError ? <p role="alert" className="text-sm text-danger">{list.error.message}</p>
      : <DataTable data={rows} columns={columns} getRowId={row => row.id} loading={list.isPending} height={360} searchable={false} rowHeight={28}
        empty={t("No branches yet. Create one to collect changes for review.")} onRowClick={row => open({ view: "proposal", params: { id: row.id } })} />}
    <NewBranchDialog open={creating} onOpenChange={setCreating} onCreated={branch => select(branch.id)} />
  </div>;
}
