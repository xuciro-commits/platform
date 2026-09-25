// Agents in the workspace (ADR-0021): a run's page — its steps with the
// rationale the model gave, what people made of it, and the draft that waits
// for the person it runs for — the assistant, which gives an agent a goal
// about a record, and the global search over every type the member may read.
import { Button, Card, Input, PageHeader, Select, StatusTag, Tag, Textarea, defineStatuses } from "@platform/ui";
import { useState } from "react";
import { newId, useHost, useOpenRecord, useRead, useReadQuery } from "./index";

export type RunStep = { at: string; tool: string; arguments?: string; rationale?: string; outcome: string; tokens?: number };
export type RunDraft = { kind: string; action: string; target: string; type?: string; payload: string; rationale?: string; step: number };
export type RunSignal = { at: string; kind: string; by: string; detail?: string; value?: string };
export type AgentRun = {
  id: string; agent: string; title: string; goal: string; ref?: string; onBehalf?: string; flow?: string; step?: string;
  state: string; model?: string; steps: RunStep[]; stepsUsed: number; tokensUsed: number; actionsUsed: number; cost?: number;
  result?: string; stopped?: string; draft?: RunDraft[]; signals?: RunSignal[];
};
export type AgentInfo = { id: string; app: string; title: string; instructions: string; tools: string[]; budget: { Steps: number; Tokens: number; Actions: number } };
type Hit = { type: string; id: string; title: string };

export const runStates = defineStatuses({ running: { label: "Working", tone: "info" }, waiting: { label: "Waiting for you", tone: "warning" },
  done: { label: "Done", tone: "success" }, stopped: { label: "Stopped", tone: "danger" } });
const signalTones = { confirmed: "success", accepted: "success", changed: "warning", corrected: "warning", bypassed: "neutral", rejected: "danger" } as const;

/** One run: read from the member's own runs, or as an administrator of the agent app. */
function useRun(id: string): AgentRun | undefined {
  const { role } = useHost();
  const mine = useRead<AgentRun[]>("/v1/runs", 1500)?.find((r) => r.id === id);
  const any = useReadQuery<{ record: AgentRun }>(`/v1/records/agent.run/${encodeURIComponent(id)}`, 1500);
  return mine ?? (role("agent") ? any.data?.record : undefined);
}

/** The draft an agent made for the member: its fields, changeable, then confirm or reject. */
function DraftCard({ run, draft }: { run: AgentRun; draft: RunDraft }) {
  const { decide, action } = useHost();
  const declared = action(draft.action.includes("#") ? draft.action.split("#")[1]! : draft.action);
  const [values, setValues] = useState<Record<string, unknown>>(() => { try { return JSON.parse(draft.payload) as Record<string, unknown>; } catch { return {}; } });
  const [reason, setReason] = useState("");
  const target = { type: "agent.run", id: run.id };
  const fields = declared?.payload ?? Object.keys(values).map((name) => ({ name, type: "string", description: name }));
  return (
    <Card className="grid gap-2 border-[var(--tone-warning)] p-3">
      <div className="text-sm font-semibold">{declared?.title ?? draft.action} <span className="font-mono text-xs text-muted">{draft.target}</span></div>
      {draft.rationale && <p className="text-sm text-muted">{draft.rationale}</p>}
      {fields.map((f) => (
        <label key={f.name} className="grid gap-1 text-xs text-muted">{f.description || f.name}
          {f.type === "string" && String(values[f.name] ?? "").length > 60
            ? <Textarea rows={4} value={String(values[f.name] ?? "")} onChange={(e) => setValues({ ...values, [f.name]: e.target.value })} />
            : <Input value={String(values[f.name] ?? "")} onChange={(e) => setValues({ ...values, [f.name]: f.type === "integer" || f.type === "number" ? Number(e.target.value) : e.target.value })} />}
        </label>
      ))}
      <div className="flex flex-wrap items-center gap-2">
        <Button size="sm" variant="primary" onClick={() => void decide("agent.run.confirm", target, { payload: values })}>Confirm</Button>
        <Input className="w-64" placeholder="Why not, for the agent" value={reason} onChange={(e) => setReason(e.target.value)} />
        <Button size="sm" variant="danger" onClick={() => void decide("agent.run.reject", target, { reason })}>Reject</Button>
      </div>
    </Card>
  );
}

/** A run's page: goal, state, the draft waiting for the member, each step and why, and what people made of it. */
export function RunView({ id, compact }: { id: string; compact?: boolean }) {
  const { me, can, decide, role } = useHost();
  const openRecord = useOpenRecord();
  const run = useRun(id);
  const [open, setOpen] = useState<number>();
  if (!run) return <p className="text-sm text-muted">Loading run {id}…</p>;
  const mine = run.onBehalf === me.principalId;
  const live = run.state === "running" || run.state === "waiting";
  return (
    <div className="grid max-w-4xl gap-3">
      {!compact && <PageHeader title={run.title} description={`${run.agent}${run.onBehalf ? ` for ${run.onBehalf}` : ""}${run.flow ? `, in the flow ${run.flow}` : ""}`}
        actions={live && (mine || role("agent") === "admin") && can("agent.run.cancel")
          ? <Button size="sm" variant="danger" onClick={() => void decide("agent.run.cancel", { type: "agent.run", id: run.id }, {})}>Stop</Button> : undefined} />}
      <div className="flex flex-wrap items-center gap-2 text-xs text-muted">
        <StatusTag status={run.state} registry={runStates} />
        {run.ref && <button type="button" className="font-mono underline" onClick={() => openRecord(run.ref!)}>{run.ref}</button>}
        <span>{run.stepsUsed} turns · {run.tokensUsed} tokens · {run.actionsUsed} actions{run.cost ? ` · $${run.cost.toFixed(4)}` : ""}</span>
        {run.model && <span className="font-mono">{run.model}</span>}
      </div>
      {!compact && <p className="whitespace-pre-wrap text-sm">{run.goal}</p>}
      {run.draft?.[0] && run.state === "waiting" && (mine ? <DraftCard key={run.draft[0].step} run={run} draft={run.draft[0]} />
        : <p className="text-sm text-muted">A draft waits for {run.onBehalf} to confirm.</p>)}
      {run.state === "running" && <p className="text-sm text-muted">The agent is working…</p>}
      {run.stopped && <p className="text-sm text-[var(--tone-danger)]">Stopped: {run.stopped}</p>}
      {run.result && <Card className="p-3"><div className="mb-1 text-xs text-muted">Result</div><p className="whitespace-pre-wrap text-sm">{run.result}</p></Card>}
      <ol className="grid gap-1">
        {run.steps.map((s, i) => (
          <li key={i} className="rounded-md border border-border bg-surface px-3 py-2 text-sm">
            <button type="button" className="flex w-full items-baseline gap-2 text-left" onClick={() => setOpen(open === i ? undefined : i)}>
              <span className="w-5 text-xs text-muted">{i + 1}</span>
              <span className="font-mono text-xs">{s.tool || "—"}</span>
              <span className="flex-1">{s.rationale ?? ""}</span>
              {s.tokens ? <span className="text-xs text-muted">{s.tokens}</span> : null}
            </button>
            <div className={open === i ? "mt-2 grid gap-1" : "hidden"}>
              {s.arguments && <pre className="overflow-auto whitespace-pre-wrap font-mono text-xs text-muted">{s.arguments}</pre>}
              <pre className="max-h-64 overflow-auto whitespace-pre-wrap font-mono text-xs">{s.outcome}</pre>
            </div>
            {open !== i && <div className="ml-7 truncate font-mono text-xs text-muted">{s.outcome.split("\n").slice(-1)[0]}</div>}
          </li>
        ))}
      </ol>
      {!!run.signals?.length && (
        <div className="grid gap-1">
          <div className="text-xs text-muted">What people made of it</div>
          {run.signals.map((s, i) => (
            <div key={i} className="flex items-center gap-2 text-sm">
              <Tag label={s.kind} tone={signalTones[s.kind as keyof typeof signalTones] ?? "neutral"} />
              <span>{s.by}</span><span className="text-muted">{s.detail ?? s.value ?? ""}</span>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

/**
 * The assistant: give one of your apps' agents a goal, about a record when
 * opened from one. It works on your behalf, within what you may do, and every
 * action it wants waits for you as a draft (ADR-0021 D6).
 */
export function Assistant({ about }: { about?: string }) {
  const { me, decide, can } = useHost();
  const agents = (useRead<AgentInfo[]>("/v1/agents") ?? []).filter((a) => me.apps.some((x) => x.id === a.app));
  const runs = useRead<AgentRun[]>("/v1/runs", 3000) ?? [];
  const type = about?.split("/")[0] ?? "";
  const suited = [...agents].sort((a, b) => Number(type.startsWith(`${b.app}.`)) - Number(type.startsWith(`${a.app}.`)));
  const [agent, setAgent] = useState("");
  const [goal, setGoal] = useState("");
  const [current, setCurrent] = useState<string>();
  const chosen = agent || suited[0]?.id || "";
  const start = async () => {
    const id = newId("RUN");
    if (await decide("agent.run.start", { type: "agent.run", id }, { agent: chosen, goal, ...(about ? { ref: about } : {}) })) { setCurrent(id); setGoal(""); }
  };
  const earlier = runs.filter((r) => r.id !== current && (!about || r.ref === about));
  return (
    <div className="grid max-w-3xl gap-3">
      <PageHeader title="Assistant" description={about ? `Ask an agent about ${about}. It drafts; you confirm.` : "Ask one of your apps' agents. It works on your behalf, within what you may do; you confirm what it drafts."} />
      {agents.length === 0 ? <p className="text-sm text-muted">None of your apps declares an agent.</p> : (
        <form className="grid gap-2" onSubmit={(e) => { e.preventDefault(); if (goal.trim()) void start(); }}>
          <Select aria-label="Agent" value={chosen} onChange={(e) => setAgent(e.target.value)}>
            {suited.map((a) => <option key={a.id} value={a.id}>{a.title} · {a.id}</option>)}
          </Select>
          <Textarea aria-label="Goal" rows={3} placeholder="What should it do?" value={goal} onChange={(e) => setGoal(e.target.value)} />
          <div><Button type="submit" variant="primary" disabled={!goal.trim() || !can("agent.run.start")}>Ask</Button></div>
        </form>
      )}
      {current && <RunView id={current} compact />}
      {earlier.length > 0 && <div className="grid gap-1">
        <div className="text-xs text-muted">Earlier{about ? " about this record" : ""}</div>
        {earlier.slice(0, 10).map((r) => (
          <button key={r.id} type="button" className="flex items-center gap-2 rounded-md px-2 py-1 text-left text-sm hover:bg-row-hover" onClick={() => setCurrent(r.id)}>
            <StatusTag status={r.state} registry={runStates} /><span className="truncate">{r.title}</span>
          </button>
        ))}
      </div>}
    </div>
  );
}

/** Global search: every record of every type the member may read, by text. */
export function Search({ initial = "" }: { initial?: string }) {
  const { client, entities } = useHost();
  const openRecord = useOpenRecord();
  const [q, setQ] = useState(initial);
  const [hits, setHits] = useState<Hit[]>();
  const run = async (text: string) => setHits(text.trim() ? await client.get<Hit[]>(`/v1/search?q=${encodeURIComponent(text)}`) : undefined);
  const title = (type: string) => entities.find((e) => e.type === type)?.title ?? type;
  return (
    <div className="grid max-w-3xl gap-3">
      <PageHeader title="Search" description="Records of every app you work in, by text, within what you may see." />
      <form onSubmit={(e) => { e.preventDefault(); void run(q); }}>
        <Input aria-label="Search" autoFocus placeholder="Search records" value={q} onChange={(e) => setQ(e.target.value)} />
      </form>
      {hits && (hits.length === 0 ? <p className="text-sm text-muted">Nothing found.</p> : (
        <ul className="grid gap-1">
          {hits.map((h) => (
            <li key={`${h.type}/${h.id}`}>
              <button type="button" className="flex w-full items-baseline gap-2 rounded-md px-2 py-1 text-left text-sm hover:bg-row-hover" onClick={() => openRecord(h)}>
                <span className="w-40 shrink-0 truncate text-xs text-muted">{title(h.type)}</span>
                <span className="truncate">{h.title}</span><span className="font-mono text-xs text-muted">{h.id}</span>
              </button>
            </li>
          ))}
        </ul>
      ))}
    </div>
  );
}
