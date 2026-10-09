import { useHost, useReadQuery, useRecordInventory } from "@platform/app";
import { Button, DataTable, Disclosure, FlowReleaseBinding, FlowCanvas, StatusTag, useFlowArrangement, flowStates, loops, notationOf, t, type FlowNodeStatus, type FlowNode, type ColumnDef, type FlowDefinition, type FlowInstanceData, type FlowCatalog } from "@platform/ui";
import { useEffect, useMemo, useState } from "react";
import { controlEdges, workflowKindTitle, workflowStepClass, type WorkflowDraft } from "./workflow-model";

export type WorkflowRun = FlowInstanceData & { data?: string; outputs?: Record<string, unknown>; withheld?: boolean; sources?: string[] };

export function WorkflowRuns({ name, versions, onStepSelect, onRunSelect }: { name: string; versions?: string[]; onStepSelect: (step: string) => void; onRunSelect?: (run: WorkflowRun | undefined) => void }) {
  const { decide, can } = useHost();
  const inventory = useRecordInventory<WorkflowRun>("flow.instance");
  const definitions = useReadQuery<FlowDefinition[]>("/v1/flows");
  const [selected, setSelected] = useState(""), [step, setStep] = useState("");
  const [error, setError] = useState(""), [busy, setBusy] = useState(false);
  const runs = useMemo(() => (inventory.data?.records ?? []).filter((run) => name ? run.flow === `build.${name}` : run.flow.startsWith("build."))
    .sort((a, b) => (b.trace?.at(-1)?.at ?? "").localeCompare(a.trace?.at(-1)?.at ?? "")), [inventory.data, name]);
  const shown = runs.find((run) => run.id === selected) ?? runs[0];
  useEffect(() => { onRunSelect?.(shown); }, [shown, onRunSelect]);
  useEffect(() => {
    if (!shown || ["done", "canceled", "compensated"].includes(shown.state)) return;
    const timer = setInterval(() => void inventory.refetch(), shown.state === "running" ? 3000 : 10000);
    return () => clearInterval(timer);
  }, [shown?.id, shown?.state, inventory.refetch]);
  const snapshot = useMemo(() => {
    if (!shown) return undefined;
    try { return versions?.[shown.version - 1] ? JSON.parse(versions[shown.version - 1]!) as WorkflowDraft : undefined; } catch { return undefined; }
  }, [versions, shown?.version]);
  const definition = definitions.data?.find((item) => item.id === shown?.flow && item.version === shown?.version);
  const nativeSteps = definition?.steps ?? [];
  const sourceSteps = snapshot?.steps ?? nativeSteps.map((item) => ({ name: item.name, title: item.title, kind: item.kind, next: item.next[0] }));
  const catalog: FlowCatalog = sourceSteps.map((item) => ({ id: item.name, title: workflowKindTitle(item.kind), class: workflowStepClass(item.kind), inputs: [{ id: "in", label: t("In"), type: "flow" }],
    outputs: snapshot ? controlEdges(snapshot).filter((edge) => edge.source === item.name).map((edge) => ({ id: edge.sourcePort, label: edge.label ?? t("Next"), type: "flow" })) : [{ id: "next", label: t("Next"), type: "flow" }] }));
  const edges = snapshot ? controlEdges(snapshot) : nativeSteps.flatMap((item) => item.next.map((next, i) => ({ id: `${item.name}:${i}`, source: item.name, sourcePort: "next", target: next, targetPort: "in" })));
  // The canvas's own arrangement decides where a step sits when the snapshot
  // holds no layout of its own (ADR-0092): one layout brain, declared sizes included.
  const positions = useFlowArrangement(sourceSteps.map((item) => ({ id: item.name, kind: item.name, label: item.title || item.name, position: { x: 0, y: 0 } })), edges, "right", catalog);
  const nodes: FlowNode[] = sourceSteps.map((item) => {
    const token = shown?.tokens?.find((token) => token.step === item.name);
    const visited = shown?.trace?.some((line) => line.step === item.name) || !!shown?.outputs && Object.hasOwn(shown.outputs, item.name);
    const status: FlowNodeStatus = token?.error || token?.waits === "stuck" ? "error" : token ? "waiting" : visited ? "success" : "idle";
    return { id: item.name, kind: item.name, notation: notationOf(item.kind), loop: loops.has(item.kind), label: item.title || item.name, detail: token?.waits ?? item.kind, status, current: !!token,
      position: snapshot?.layout?.[item.name] ?? positions[item.name] ?? { x: 0, y: 0 }, diagnostics: token?.error ? [{ message: token.error, severity: "error" }] : undefined };
  });
  const inspect = (name: string) => { setStep(name); onStepSelect(name); };
  const columns: ColumnDef<WorkflowRun, any>[] = [
    { accessorKey: "title", header: t("Run") },
    { accessorKey: "state", header: t("State"), meta: { width: 110 }, cell: ({ getValue }) => <StatusTag status={getValue()} registry={flowStates} /> },
    { accessorKey: "version", header: t("Version"), meta: { width: 80, align: "right" } },
    { accessorKey: "key", header: t("Run key") },
  ];
  const command = async (schema: string, payload: Record<string, unknown>) => {
    if (!shown) return; setBusy(true); setError("");
    try { if (await decide(schema, { type: "flow.instance", id: shown.id }, payload, { quiet: true, onRefused: setError })) await inventory.refetch(); }
    finally { setBusy(false); }
  };
  const token = shown?.tokens?.find((token) => token.step === step);
  const output = shown?.outputs?.[step];
  const traceColumns: ColumnDef<NonNullable<WorkflowRun["trace"]>[number] & { id: string }, any>[] = [
    { accessorKey: "at", header: t("Time"), cell: ({ getValue }) => new Date(getValue()).toLocaleString() },
    { accessorKey: "step", header: t("Step"), cell: ({ row }) => row.original.step ? <Button variant="link" onClick={() => inspect(row.original.step!)}>{row.original.step}</Button> : "" },
    { accessorKey: "what", header: t("Event") }, { accessorKey: "detail", header: t("Detail") },
  ];
  return <div className="grid gap-3">
    <div className="flex items-center justify-between gap-2"><h3 className="text-xs font-semibold">{t("Execution history")}</h3><Button onClick={() => { void inventory.refetch(); void definitions.refetch(); }}>{t("Refresh runs")}</Button></div>
    {inventory.isError && <p role="alert" className="text-xs text-danger">{t("Run history could not be loaded for this member.")}</p>}
    <DataTable data={runs} columns={columns} getRowId={(run) => run.id} height={160} searchable={false} selectedId={shown?.id} onRowClick={(run) => { setSelected(run.id); setStep(""); }} loading={inventory.isLoading} empty={t("No runs of this workflow yet.")} />
    {shown && <>
      <div className="flex flex-wrap items-center gap-2 text-[11px]"><StatusTag status={shown.state} registry={flowStates} /><code>{shown.id}</code><span className="text-muted">v{shown.version}</span>
        {can("flow.instance.cancel") && !["done", "canceled", "compensated"].includes(shown.state) && <Button disabled={busy} onClick={() => void command("flow.instance.cancel", {})}>{t("Cancel run")}</Button>}
        {token?.waits === "stuck" && can("flow.instance.retry") && <Button disabled={busy} onClick={() => void command("flow.instance.retry", {})}>{t("Retry run")}</Button>}
      </div>
      <FlowReleaseBinding dependencies={shown.dependencies} release={shown.release} />
      {error && <p role="alert" className="text-xs text-danger">{error}</p>}
      {shown.withheld ? <p className="text-xs text-muted">{t("Run data is withheld by the current source permissions.")}</p> : <div className="grid gap-3 lg:grid-cols-[minmax(0,1fr)_300px]">
        <FlowCanvas mode="view" label={t("Execution map")} catalog={catalog} nodes={nodes} edges={edges} height={300} selected={step} onSelect={inspect} storeKey={`workflow-runs:${name}`} />
        <div className="grid content-start gap-2 rounded-lg border border-border p-3"><h4 className="text-xs font-medium">{step ? t("Accepted output: {step}", { step }) : t("Workflow input")}</h4>
          <pre className="max-h-52 overflow-auto rounded bg-background p-2 text-[10px]">{step ? output === undefined ? t("No accepted output for this step yet.") : JSON.stringify(output, null, 2) : shown.data || "{}"}</pre>
          {token && <><h4 className="text-xs font-medium">{t("Current token")}</h4><pre className="max-h-40 overflow-auto rounded bg-background p-2 text-[10px]">{JSON.stringify(token, null, 2)}</pre></>}
        </div>
      </div>}
      <Disclosure summary={<span className="text-xs font-medium">{t("Decision trace")}</span>}><DataTable data={(shown.trace ?? []).map((line, index) => ({ ...line, id: String(index) }))} columns={traceColumns} getRowId={(line) => line.id} height={200} searchable={false} /></Disclosure>
    </>}
  </div>;
}
