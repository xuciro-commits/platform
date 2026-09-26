// Settings: flows (ADR-0020), agents and their evaluations (ADR-0021).
import { Records, newId, useHost, useReadQuery as useRead, type AgentInfo } from "@platform/app";
import { Button, DataTable, FlowView, Input, PageHeader, Select, type ColumnDef, type FlowDefinition, type FlowInstanceData, t } from "@platform/ui";
import { useState } from "react";
import { type AIModel } from "./shared";

// Flows (ADR-0020): the flows the tenant's apps declare, their instances, and
// one instance drawn with the path it took and why; administrators retry,
// skip, cancel or move stuck and running instances.
export function Flows() {
  const flows = useRead<FlowDefinition[]>("/v1/flows").data ?? [];
  const columns: ColumnDef<FlowDefinition, any>[] = [
    { accessorKey: "title", header: t("Flow") },
    { accessorKey: "id", header: "ID", meta: { width: 220 }, cell: (c) => <span className="font-mono text-xs">{c.getValue()}</span> },
    { accessorKey: "version", header: t("Version"), meta: { width: 80, align: "right" } },
    { id: "start", header: t("Starts on"), meta: { width: 260 }, accessorFn: (f) => f.start.join(", ") },
    { id: "steps", header: t("Steps"), meta: { width: 70, align: "right" }, accessorFn: (f) => f.steps.length },
  ];
  return (
    <>
      <PageHeader title={t("Flows")} description={t("Long-running processes the apps declare. Each instance is a record: open one to see where it stands and why it moved.")} />
      <DataTable data={flows} columns={columns} getRowId={(f) => `${f.id}@${f.version}`} height={180} empty={t("No app declares a flow")} />
      <h2 className="mt-4 mb-2 text-sm font-semibold">{t("Instances")}</h2>
      <Records type="flow.instance" description={t("Every run of every flow, newest changes first.")} />
    </>
  );
}

export function FlowPage({ id }: { id: string }) {
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
      {live && (
        <div className="flex gap-2">
          {can("flow.instance.retry") && <Button size="sm" onClick={() => void decide("flow.instance.retry", target, {})}>{t("Retry")}</Button>}
          {can("flow.instance.move") && next && <Button size="sm" onClick={() => void decide("flow.instance.move", target, {})}>{t("Move to the next version")}</Button>}
          {can("flow.instance.cancel") && <Button size="sm" variant="danger" onClick={() => void decide("flow.instance.cancel", target, {})}>{t("Cancel")}</Button>}
        </div>
      )}
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
  const chosen = agent || agents[0]?.id || "";
  return (
    <>
      <PageHeader title={t("Evaluations")} description={t("A candidate model re-runs an agent's latest runs that people confirmed, changed, rejected, accepted or corrected — dry: it sees what the run saw, its actions are checked, never taken. Each case agrees or differs with what people accepted, or repeats or avoids what they corrected.")} />
      {can("agent.evaluation.start") && (
        <form className="mb-3 flex flex-wrap items-center gap-2" onSubmit={(e) => { e.preventDefault(); void decide("agent.evaluation.start", { type: "agent.evaluation", id: newId("EVAL") }, { agent: chosen, model }); }}>
          <Select aria-label={t("Agent")} className="w-64" value={chosen} onChange={(e) => setAgent(e.target.value)}>
            {agents.map((a) => <option key={a.id} value={a.id}>{a.title} · {a.id}</option>)}
          </Select>
          <Input aria-label={t("Candidate model")} className="w-80" list="enabled-models" placeholder={t("Candidate model, provider/model")} value={model} onChange={(e) => setModel(e.target.value)} />
          <datalist id="enabled-models">{models.map((m) => <option key={`${m.provider}/${m.model}`} value={`${m.provider}/${m.model}`} />)}</datalist>
          <Button type="submit" variant="primary" disabled={!chosen || !model}>{t("Evaluate")}</Button>
        </form>
      )}
      <Records type="agent.evaluation" description={t("Reports, newest first; open one for each case.")} />
    </>
  );
}
