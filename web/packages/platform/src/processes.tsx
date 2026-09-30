// Settings: flows (ADR-0020), agents and their evaluations (ADR-0021).
import { ChainGraph, Records, newId, useHost, useOpenRecord, useReadQuery as useRead, type AgentInfo } from "@platform/app";
import { Button, Checkbox, DataTable, Form, FlowGraph, FlowView, StatusTag, Tag, defineStatuses, PageHeader, Select, useWorkspace, type ColumnDef, type FlowDefinition, type FlowInstanceData, t } from "@platform/ui";
import { useState } from "react";
import type { Api } from "@platform/kernel";
import { type AIModel } from "./shared";

// Flows (ADR-0020): the flows the tenant's apps declare, their instances, and
// one instance drawn with the path it took and why; administrators retry,
// skip, cancel or move stuck and running instances.
export function Flows() {
  const flows = useRead<FlowDefinition[]>("/v1/flows").data ?? [];
  const [shown, setShown] = useState<string>();
  const definition = flows.find((f) => `${f.id}@${f.version}` === shown);
  const columns: ColumnDef<FlowDefinition, any>[] = [
    { accessorKey: "title", header: t("Flow") },
    { accessorKey: "id", header: "ID", meta: { width: 220 }, cell: (c) => <span className="font-mono text-xs">{c.getValue()}</span> },
    { accessorKey: "version", header: t("Version"), meta: { width: 80, align: "right" } },
    { id: "start", header: t("Starts on"), meta: { width: 260 }, accessorFn: (f) => (f.start ?? []).join(", ") },
    { id: "steps", header: t("Steps"), meta: { width: 70, align: "right" }, accessorFn: (f) => f.steps.length },
  ];
  return (
    <>
      <PageHeader title={t("Flows")} description={t("Long-running processes the apps declare. Each instance is a record: open one to see where it stands and why it moved.")} />
      <DataTable data={flows} columns={columns} getRowId={(f) => `${f.id}@${f.version}`} height={180} empty={t("No app declares a flow")}
        selectedId={shown} onRowClick={(f) => setShown(shown === `${f.id}@${f.version}` ? undefined : `${f.id}@${f.version}`)} />
      {definition
        ? <div className="mt-3 grid gap-1"><h2 className="text-sm font-semibold">{definition.title} <span className="font-mono text-xs text-muted">{definition.id} v{definition.version}</span></h2><FlowGraph definition={definition} /></div>
        : flows.length > 0 && <p className="mt-2 text-xs text-muted">{t("Click a flow to see its steps.")}</p>}
      <h2 className="mt-4 mb-2 text-sm font-semibold">{t("Instances")}</h2>
      <Records type="flow.instance" description={t("Every run of every flow, newest changes first.")} />
    </>
  );
}

export function FlowPage({ id }: { id: string }) {
  const { open } = useWorkspace();
  const openRecord = useOpenRecord();
  const { decide, can } = useHost();
  const view = useRead<{ record: FlowInstanceData }>(`/v1/records/flow.instance/${encodeURIComponent(id)}`, 3000).data;
  const flows = useRead<FlowDefinition[]>("/v1/flows").data ?? [];
  const x = view?.record;
  if (!x) return <p className="text-sm text-muted">{t("Loading")} {id}…</p>;
  const definition = flows.find((f) => f.id === x.flow && f.version === x.version);
  const target = { type: "flow.instance", id: x.id };
  const live = !["done", "compensated", "canceled"].includes(x.state);
  const next = flows.some((f) => f.id === x.flow && f.version > x.version);
  return (
    <div className="grid max-w-4xl gap-3">
      <div className="flex flex-wrap gap-2"><Button variant="ghost" onClick={() => open({ view: "inbox" }, { window: "float" })}>{t("Back to inbox")}</Button>
        {x.subject && <Button variant="ghost" onClick={() => openRecord(x.subject!)}>{t("Open related record")}</Button>}
      </div>
      {live && (
        <div className="flex gap-2">
          {can("flow.instance.retry") && <Button size="sm" onClick={() => void decide("flow.instance.retry", target, {})}>{t("Retry")}</Button>}
          {can("flow.instance.move") && next && <Button size="sm" onClick={() => void decide("flow.instance.move", target, {})}>{t("Move to the next version")}</Button>}
          {can("flow.instance.cancel") && <Button size="sm" variant="danger" onClick={() => void decide("flow.instance.cancel", target, {})}>{t("Cancel")}</Button>}
        </div>
      )}
      <ChainGraph of={`flow.instance/${x.id}`} />
      <FlowView definition={definition} instance={x} actions={(k) => live && can("flow.instance.skip") && (k.waits === "stuck" || k.waits === "retry" || k.waits === "undo")
        ? <Button size="sm" variant="ghost" onClick={() => void decide("flow.instance.skip", target, { token: k.id })}>{t("Skip")}</Button> : null} />
    </div>
  );
}

// Agents (ADR-0021): the agents the apps declare with their tools and budgets,
// every run with its trace, and evaluations of a candidate model against what
// people confirmed or corrected.
export function Agents() {
  const agents = useRead<AgentInfo[]>("/v1/agents").data ?? [];
  const columns: ColumnDef<AgentInfo, any>[] = [
    { accessorKey: "title", header: t("Agent") },
    { accessorKey: "id", header: "ID", meta: { width: 200 }, cell: (c) => <span className="font-mono text-xs">{c.getValue()}</span> },
    { id: "tools", header: t("Tools"), meta: { width: 320 }, accessorFn: (a) => a.tools.join(", ") },
    { id: "budget", header: t("Budget per run"), meta: { width: 220 }, accessorFn: (a) => `${a.budget.Steps} turns, ${a.budget.Tokens} tokens, ${a.budget.Actions} actions` },
  ];
  return (
    <>
      <PageHeader title={t("Agents")} description={t("Agents the apps declare. Each is a principal of its own: it does what its tools allow and, for a person, only what they may do; people confirm its drafts. The model is an app setting of Agents.")} />
      <DataTable data={agents} columns={columns} getRowId={(a) => a.id} height={180} empty={t("No app declares an agent")} />
      <AgentsOverview />
      <h2 className="mb-2 mt-4 text-sm font-semibold">{t("Runs")}</h2>
      <Records type="agent.run" description={t("Every run: open one for its steps, the rationale of each, and what people made of it.")} />
      <h2 className="mb-2 mt-4 text-sm font-semibold">{t("Memories")}</h2>
      <Records type="agent.memory" description={t("What agents keep across runs: facts they chose to remember, and proposals from people's corrections, which count once someone keeps them. Open one to keep or forget it.")} />
    </>
  );
}

export function Evaluations() {
  const { decide, can } = useHost();
  const agents = useRead<AgentInfo[]>("/v1/agents").data ?? [];
  const models = useRead<AIModel[]>("/v1/ai-models").data ?? [];
  const [agent, setAgent] = useState("");
  const [model, setModel] = useState("");
  const [suite, setSuite] = useState(false);
  const chosen = agent || agents[0]?.id || "";
  return (
    <>
      <PageHeader title={t("Evaluations")} description={t("A candidate model re-runs an agent's latest runs that people confirmed, changed, rejected, accepted or corrected — dry: it sees what the run saw, its actions are checked, never taken. Each case agrees or differs with what people accepted, or repeats or avoids what they corrected.")} />
      {can("agent.evaluation.start") && (
        <Form className="mb-3 flex flex-wrap items-center gap-2" onSubmit={() => { void decide("agent.evaluation.start", { type: "agent.evaluation", id: newId("EVAL") }, { agent: chosen, model, suite }); }}>
          <Select aria-label={t("Agent")} className="w-64" value={chosen} onChange={(e) => setAgent(e.target.value)}>
            {agents.map((a) => <option key={a.id} value={a.id}>{a.title} · {a.id}</option>)}
          </Select>
          <Select aria-label={t("Candidate model")} className="w-80" value={model} onChange={(e) => setModel(e.target.value)}>
            <option value="">{t("Candidate model…")}</option>
            {models.map((m) => <option key={`${m.provider}/${m.model}`} value={`${m.provider}/${m.model}`}>{m.provider}/{m.model}</option>)}
          </Select>
          <span title={t("The agent's declared cases, three runs each, instead of its past runs")}>
            <Checkbox className="text-sm" checked={suite} onChange={setSuite}>{t("Declared cases")}</Checkbox>
          </span>
          <Button type="submit" variant="primary" disabled={!chosen || !model}>{t("Evaluate")}</Button>
        </Form>
      )}
      <Records type="agent.evaluation" description={t("Reports, newest first; open one for each case.")} />
    </>
  );
}

const switches = defineStatuses({ working: { label: t("Working"), tone: "success" }, suspended: { label: t("Suspended"), tone: "danger" } });

// Every agent, declared and outside (ADR-0029 D4, D5): what it did, what it
// cost, what people made of its work, and the switch that stops it.
function AgentsOverview() {
  const { decide, can } = useHost();
  const rows = useRead<Api.AgentOverview[]>("/v1/agent-overview");
  if (rows.isError) return null; // an administrator's view
  const judged = (o: Api.AgentOverview, kind: string) => o.judged[kind] ?? 0;
  const columns: ColumnDef<Api.AgentOverview, any>[] = [
    { accessorKey: "member", header: t("Agent"), cell: ({ row: { original: o } }) => <span className="flex items-center gap-1.5"><span className="font-mono text-xs">{o.member}</span>{o.outside && <Tag label={t("outside")} />}</span> },
    { id: "state", header: t("State"), meta: { width: 110 }, accessorFn: (o) => o.suspended ? "suspended" : "working", cell: (c) => <StatusTag status={c.getValue()} registry={switches} /> },
    { accessorKey: "runs", header: t("Runs"), meta: { width: 70, align: "right" } },
    { accessorKey: "actions", header: t("Actions"), meta: { width: 80, align: "right" } },
    { id: "accepted", header: t("Accepted"), meta: { width: 90, align: "right" }, accessorFn: (o) => judged(o, "confirmed") + judged(o, "approved") + judged(o, "accepted") },
    { id: "changed", header: t("Changed"), meta: { width: 90, align: "right" }, accessorFn: (o) => judged(o, "changed") + judged(o, "corrected") },
    { id: "refused", header: t("Refused"), meta: { width: 90, align: "right" }, accessorFn: (o) => judged(o, "rejected") + judged(o, "discarded") + judged(o, "undone") },
    { accessorKey: "tokens", header: t("Tokens"), meta: { width: 100, align: "right" } },
    { accessorKey: "cost", header: t("Cost (USD)"), meta: { width: 100, align: "right" }, cell: (c) => c.getValue().toFixed(4) },
    { id: "switch", header: "", meta: { width: 110 }, cell: ({ row: { original: o } }) => o.suspended
      ? can("agent.resume") && <Button size="sm" onClick={() => void decide("agent.resume", { type: "agent.switch", id: o.member }, {})}>{t("Resume")}</Button>
      : can("agent.suspend") && <Button size="sm" variant="danger" onClick={() => void decide("agent.suspend", { type: "agent.switch", id: o.member }, {})}>{t("Suspend")}</Button> },
  ];
  return (
    <>
      <h2 className="mb-1 mt-4 text-sm font-semibold">{t("Overview")}</h2>
      <p className="mb-2 text-xs text-muted">{t("Every agent, the apps' and those outside that act as one: what it did, what it cost from the usage kept, and what people made of its work. Suspending one stops its runs at their next step and refuses its calls.")}</p>
      <DataTable data={rows.data ?? []} columns={columns} getRowId={(o) => o.member} height={220} empty={t("No agent yet")} />
    </>
  );
}
