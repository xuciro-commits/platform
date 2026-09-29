import { useReadQuery, useRecordInventory } from "@platform/app";
import { Button, Card, DataTable, FlowView, PageHeader, StatusTag, flowStates, t,
  type ColumnDef, type FlowDefinition, type FlowInstanceData } from "@platform/ui";
import { useMemo, useState } from "react";

export function WorkflowRuns({ name, onStepSelect }: { name: string; onStepSelect: (step: string) => void }) {
  const inventory = useRecordInventory<FlowInstanceData>("flow.instance");
  const definitions = useReadQuery<FlowDefinition[]>("/v1/flows");
  const [selected, setSelected] = useState("");
  const runs = useMemo(() => (inventory.data?.records ?? []).filter((run) => run.flow === `build.${name}`)
    .sort((a, b) => (b.trace?.at(-1)?.at ?? "").localeCompare(a.trace?.at(-1)?.at ?? "")), [inventory.data, name]);
  const shown = runs.find((run) => run.id === selected) ?? runs[0];
  const definition = definitions.data?.find((item) => item.id === shown?.flow && item.version === shown?.version);
  const columns: ColumnDef<FlowInstanceData, any>[] = [
    { accessorKey: "title", header: t("Run") },
    { accessorKey: "state", header: t("State"), meta: { width: 110 }, cell: ({ getValue }) => <StatusTag status={getValue()} registry={flowStates} /> },
    { accessorKey: "version", header: t("Version"), meta: { width: 80, align: "right" } },
    { id: "release", header: t("Release"), accessorFn: (run) => run.release ? t("Active") : t("Development"), meta: { width: 110 } },
  ];
  return <div className="grid gap-3">
    <PageHeader title={t("Run history")} description={t("Select a run to inspect its steps, outcome and exact starting version.")}
      actions={<Button onClick={() => { void inventory.refetch(); void definitions.refetch(); }}>{t("Refresh runs")}</Button>} />
    {inventory.isError && <Card role="alert" className="p-3 text-sm text-danger">{t("Run history could not be loaded for this member.")}</Card>}
    <DataTable data={runs} columns={columns} getRowId={(run) => run.id} height={200} searchable={false}
      selectedId={shown?.id} onRowClick={(run) => setSelected(run.id)} loading={inventory.isLoading}
      empty={t("No runs of this workflow yet.")} />
    {shown && <Card className="min-w-0 overflow-x-auto p-3"><FlowView definition={definition} instance={shown} onStepSelect={onStepSelect} /></Card>}
  </div>;
}
