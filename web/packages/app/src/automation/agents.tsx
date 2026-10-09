// Agents in the workspace (ADR-0021): a run's page — its steps with the
// rationale the model gave, what people made of it, and the draft that waits
// for the person it runs for — the assistant, which gives an agent a goal
// about a record, and the global search over every type the member may read.
import "../i18n";
import { Button, Card, Disclosure, useWorkspace, Form, FlowSteps, Input, PageHeader, Panel, RelationCanvas, relationNodeClasses, Select, StatusTag, Tag, Textarea, defineStatuses, t, language,
  type FlowStepEdge, type FlowStepNode, type RelationEdge, type RelationNode } from "@platform/ui";
import type { Api } from "@platform/kernel";
import { useState } from "react";
import { PayloadFields } from "../actions/actions";
import { newId, useHost, useOpenRecord, useRead, useReadQuery } from "../index";

// Generated from the host's Go types (ADR-0023 D7).
export type RunStep = Api.RunStep;
export type RunDraft = Api.Draft;
export type RunSignal = Api.Signal;
export type AgentRun = Api.AgentRunRecord;
export type Citation = Api.Citation;
export type Passage = Api.Passage;
export type Memory = Api.Memory;
type Transcript = Api.Transcript;
export type AgentInfo = Api.AgentInfo;
type Hit = Api.Hit;

export const runStates = defineStatuses({ running: { label: t("Working"), tone: "info" }, waiting: { label: t("Waiting for you"), tone: "warning" },
  done: { label: t("Done"), tone: "success" }, stopped: { label: t("Stopped"), tone: "danger" } });
const signalTones = { confirmed: "success", accepted: "success", approved: "success", changed: "warning", corrected: "warning", bypassed: "neutral",
  rejected: "danger", discarded: "danger", undone: "danger" } as const;

/** One run: read from the member's own runs, or as an administrator of the agent app. */
function useRun(id: string) {
  const mine=useReadQuery<AgentRun[]>("/v1/runs");
  const record=useReadQuery<{record:AgentRun}>(`/v1/records/agent.run/${encodeURIComponent(id)}`);
  return {run:mine.data?.find(run=>run.id===id)??record.data?.record,error:record.isError};
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
      <PayloadFields fields={fields} values={values} onChange={setValues} />
      <div className="flex flex-wrap items-center gap-2">
        <Button size="sm" variant="primary" onClick={() => void decide("agent.run.confirm", target, { payload: values })}>{t("Confirm")}</Button>
        <Input className="w-64" placeholder={t("Why not, for the agent")} value={reason} onChange={(e) => setReason(e.target.value)} />
        <Button size="sm" variant="danger" onClick={() => void decide("agent.run.reject", target, { reason })}>{t("Reject")}</Button>
      </div>
    </Card>
  );
}

/** A run's page: goal, state, the draft waiting for the member, each step and why, and what people made of it. */
export function RunView({ id, compact, application }: { id: string; compact?: boolean; application?: string }) {
  const { me, can, decide, role } = useHost();
  const openRecord = useOpenRecord();
  const {run,error}=useRun(id);
  const {open:openWorkspace}=useWorkspace();
  const [open, setOpen] = useState<number>();
  if(error&&!run)return <p role="alert">{t("This agent run is unavailable to you.")}</p>;
  if (!run) return <p className="text-sm text-muted">{t("Loading run")} {id}…</p>;
  const mine = run.onBehalf === me.principalId;
  const live = run.state === "running" || run.state === "waiting";
  return (
    <div className="grid max-w-4xl gap-3">
      {application&&<Button variant="ghost" onClick={()=>openWorkspace({view:"application-runs",params:{app:(application.split(":")[0]??""),name:application.split(":").slice(1).join(":")}})}>{t("Back to application runs")}</Button>}
      {run.definitionVersion&&<p className="break-all font-mono text-xs">{t("Startup definition")}: {run.definitionVersion}</p>}
      <p className="break-all font-mono text-xs">{t("Startup release")}: {run.release||t("No startup activation recorded")}</p>
      {!compact && <PageHeader title={run.title} description={`${run.agent}${run.onBehalf ? ` for ${run.onBehalf}` : ""}${run.flow ? `, in the flow ${run.flow}` : ""}`}
        actions={live && (mine || role("agent") === "admin") && can("agent.run.cancel")
          ? <Button size="sm" variant="danger" onClick={() => void decide("agent.run.cancel", { type: "agent.run", id: run.id }, {})}>{t("Stop")}</Button> : undefined} />}
      <div className="flex flex-wrap items-center gap-2 text-xs text-muted">
        <StatusTag status={run.state} registry={runStates} />
        {run.ref && <Button variant="link" className="font-mono text-xs" onClick={() => openRecord(run.ref!)}>{run.ref}</Button>}
        <span>{run.stepsUsed} {t("turns ·")} {run.tokensUsed} {t("tokens ·")} {run.actionsUsed} actions{run.cost ? ` · $${run.cost.toFixed(4)}` : ""}</span>
        {run.model && <span className="font-mono">{run.model}</span>}
      </div>
      {run.withheld && <Panel role="status" className="text-xs text-muted">{t("Part of this trace came from records you may no longer read, and is left out.")}</Panel>}
      {!compact && <p className="whitespace-pre-wrap text-sm">{run.goal}</p>}
      {run.draft?.[0] && run.state === "waiting" && (mine ? <DraftCard key={run.draft[0].step} run={run} draft={run.draft[0]} />
        : <p className="text-sm text-muted">{t("A draft waits for")} {run.onBehalf} {t("to confirm.")}</p>)}
      {run.state === "running" && <p className="text-sm text-muted">{t("The agent is working…")}</p>}
      {run.stopped && <p className="text-sm text-[var(--tone-danger)]">{t("Stopped:")} {run.stopped}</p>}
      {run.result && <Card className="p-3"><div className="mb-1 text-xs text-muted">{t("Result")}</div><p className="whitespace-pre-wrap text-sm">{run.result}</p></Card>}
      {!compact && run.steps.length > 0 && <RunGraph run={run} onStep={setOpen} />}
      {!compact && <ChainGraph of={`agent.run/${run.id}`} />}
      <div role="list" className="grid gap-1">
        {run.steps.map((s, i) => (
          <Card key={i} className="px-2 py-1 text-sm">
            <Disclosure open={open === i} onToggle={(o) => setOpen(o ? i : undefined)} summary={<>
              <span className="w-5 text-xs text-muted">{i + 1}</span>
              <span className="font-mono text-xs">{s.tool || "—"}</span>
              <span className="flex-1">{s.rationale ?? ""}</span>
              {s.tokens ? <span className="text-xs text-muted">{s.tokens}</span> : null}
            </>}>
              <div className="mt-1 grid gap-1">
                {s.arguments && <pre className="overflow-auto whitespace-pre-wrap font-mono text-xs text-muted">{s.arguments}</pre>}
                <pre className="max-h-64 overflow-auto whitespace-pre-wrap font-mono text-xs">{s.outcome}</pre>
              </div>
            </Disclosure>
            {open !== i && <div className="ml-9 truncate font-mono text-xs text-muted">{s.outcome.split("\n").slice(-1)[0]}</div>}
          </Card>
        ))}
      </div>
      {!!run.citations?.length && (
        <div className="grid gap-1">
          <div className="text-xs text-muted">{t("Sources it read")}</div>
          {run.citations.map((c, i) => (
            <Button key={i} variant="link" className="justify-start text-left" onClick={() => openRecord(c.document.split("#")[0]!)}>
              {c.title} <span className="font-mono text-xs text-muted">{c.document} {t("· passage")} {c.chunk + 1} {t("· step")} {c.step + 1}</span>
            </Button>
          ))}
        </div>
      )}
      {!compact && role("agent") === "admin" && <Transcripts run={run.id} />}
      {!!run.signals?.length && (
        <div className="grid gap-1">
          <div className="text-xs text-muted">{t("What people made of it")}</div>
          {run.signals.map((s, i) => (
            <div key={i} className="flex items-center gap-2 text-sm">
              <Tag label={t(s.kind)} tone={signalTones[s.kind as keyof typeof signalTones] ?? "neutral"} />
              <span>{s.by}</span><span className="text-muted">{s.detail ?? s.value ?? ""}</span>
            </div>
          ))}
        </div>
      )}
    </div>
  );
}

/** A run drawn as a process (#122, ADR-0086 D4): the goal as a start event, each
 * tool it called in turn as a service task, the sources it read as data objects, and
 * how it ended as an end event. */
function RunGraph({ run, onStep }: { run: AgentRun; onStep: (i: number) => void }) {
  const refused = (s: RunStep) => /^(refused|error|stopped|ERROR_CODE_)/i.test(s.outcome);
  const nodes: FlowStepNode[] = [{ id: "goal", label: run.title, detail: run.agent, notation: "event-start", tone: "success" }];
  const edges: FlowStepEdge[] = [];
  run.steps.forEach((s, i) => {
    nodes.push({ id: `step-${i}`, label: `${i + 1}. ${s.tool || t("answer")}`, detail: s.rationale ?? s.outcome.split("\n")[0], notation: "service-task", tone: refused(s) ? "danger" : "success" });
    edges.push({ from: i ? `step-${i - 1}` : "goal", to: `step-${i}` });
  });
  (run.citations ?? []).forEach((c, i) => {
    nodes.push({ id: `source-${i}`, label: c.title, detail: t("source"), notation: "data-object", tone: "neutral" });
    edges.push({ from: `step-${Math.min(c.step, run.steps.length - 1)}`, to: `source-${i}`, dashed: true });
  });
  const last = run.steps.length ? `step-${run.steps.length - 1}` : "goal";
  const end = run.state === "waiting" ? { label: t("Draft waits"), tone: "warning" as const } : run.state === "running" ? { label: t("Working"), tone: "info" as const }
    : run.stopped ? { label: t("Stopped"), tone: "danger" as const } : { label: t("Done"), tone: "success" as const };
  nodes.push({ id: "end", ...end, notation: run.stopped ? "event-terminate" : "event-end", current: run.state === "running" || run.state === "waiting", detail: run.stopped ?? run.draft?.[0]?.action });
  edges.push({ from: last, to: "end" });
  return <FlowSteps nodes={nodes} edges={edges} height={200} label={t("Steps")} onOpen={(n) => n.id.startsWith("step-") && onStep(Number(n.id.slice(5)))} />;
}

const chainTone = (state?: string) => state === "done" || state === "delivered" ? "success" as const
  : state === "stopped" || state === "stuck" || state === "failed" || state === "rejected" || state === "discarded" ? "danger" as const
  : state === "held" || state === "waiting" ? "warning" as const : state ? "info" as const : undefined;

/**
 * The chain a run or a flow instance belongs to (ADR-0029 D6): the record, the
 * flows, the runs their steps started and the effects those caused, as the
 * member may read them; a node opens its record.
 */
export function ChainGraph({ of, title = t("Chain") }: { of: string; title?: string }) {
  const openRecord = useOpenRecord();
  const chain = useReadQuery<Api.Chain>(`/v1/chain/${of.split("/").map(encodeURIComponent).join("/")}`).data;
  if (!chain || chain.nodes.length < 2) return null;
  const nodes: RelationNode[] = chain.nodes.map((n) => ({ id: n.ref, label: n.title, class: n.kind,
    // The class word comes from the kit's table (ADR-0090 D1); the run's own
    // state still rides beside it, and still decides the tone.
    caption: [t(relationNodeClasses[n.kind]?.title ?? n.kind), n.state].filter(Boolean).join(" · "),
    detail: n.ref, tone: chainTone(n.state) }));
  const edges: RelationEdge[] = chain.edges.map((e) => ({ id: `${e.from}>${e.to}`, source: e.from, target: e.to }));
  return (
    <div className="grid gap-1">
      <h3 className="text-xs uppercase text-muted">{title}</h3>
      <RelationCanvas layout="layered" nodes={nodes} edges={edges} selected={of} height={200} label={title}
        onSelect={(id) => { if (id && !id.startsWith("platform.effect/")) openRecord(id); }} />
    </div>
  );
}

/** Every model call of a run in full, for agent administrators (ADR-0022 D8). */
function Transcripts({ run }: { run: string }) {
  const [shown, setShown] = useState(false);
  const calls = useReadQuery<Transcript[]>(`/v1/transcripts?run=${encodeURIComponent(run)}`).data;
  if (!calls?.length) return null;
  return (
    <div className="grid gap-1">
      <Button variant="link" className="justify-start text-xs text-muted" onClick={() => setShown(!shown)}>
        {shown ? t("Hide") : t("Show")} the {calls.length} {t("model calls in full")}
      </Button>
      {shown && calls.map((c, i) => (
        <Card key={i} className="px-2 py-1 text-xs">
          <Disclosure summary={<>{c.at} · {c.model} · {c.outcome}</>}>
            <pre className="max-h-80 overflow-auto whitespace-pre-wrap font-mono">{JSON.stringify({ request: c.request, answer: c.answer }, null, 2)}</pre>
          </Disclosure>
        </Card>
      ))}
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
  const runs = useRead<AgentRun[]>("/v1/runs") ?? [];
  const type = about?.split("/")[0] ?? "";
  const suited = [...agents].sort((a, b) => Number(type.startsWith(`${b.app}.`)) - Number(type.startsWith(`${a.app}.`)));
  const [agent, setAgent] = useState("");
  const [goal, setGoal] = useState("");
  const [current, setCurrent] = useState<string>();
  const chosen = agent || suited[0]?.id || "";
  const start = async () => {
    const id = newId("RUN");
    if (await decide("agent.run.start", { type: "agent.run", id }, { agent: chosen, goal, ...(about ? { ref: about } : {}), ...(language() !== "en" ? { language: language() } : {}) })) { setCurrent(id); setGoal(""); }
  };
  const earlier = runs.filter((r) => r.id !== current && (!about || r.ref === about));
  return (
    <div className="grid max-w-3xl gap-3">
      <PageHeader title={t("Assistant")} description={about ? `Ask an agent about ${about}. It drafts; you confirm.` : t("Ask one of your apps' agents. It works on your behalf, within what you may do; you confirm what it drafts.")} />
      {agents.length === 0 ? <p className="text-sm text-muted">{t("None of your apps declares an agent.")}</p> : (
        <Form className="grid gap-2" onSubmit={() => { if (goal.trim()) void start(); }}>
          <Select aria-label={t("Agent")} value={chosen} onChange={(e) => setAgent(e.target.value)}>
            {suited.map((a) => <option key={a.id} value={a.id}>{a.title} · {a.id}</option>)}
          </Select>
          <Textarea aria-label={t("Goal")} rows={3} placeholder={t("What should it do?")} value={goal} onChange={(e) => setGoal(e.target.value)} />
          <div><Button type="submit" variant="primary" disabled={!goal.trim() || !can("agent.run.start")}>{t("Ask")}</Button></div>
        </Form>
      )}
      {current && <RunView id={current} compact />}
      <Remembered />
      {earlier.length > 0 && <div className="grid gap-1">
        <div className="text-xs text-muted">{t("Earlier")}{about ? " about this record" : ""}</div>
        {earlier.slice(0, 10).map((r) => (
          <Button key={r.id} variant="row" onClick={() => setCurrent(r.id)}>
            <StatusTag status={r.state} registry={runStates} /><span className="truncate">{r.title}</span>
          </Button>
        ))}
      </div>}
    </div>
  );
}

/** What agents remember about the member (ADR-0022 D5): proposals from their corrections to keep, and facts to forget. */
function Remembered() {
  const { decide } = useHost();
  const memories = useRead<Memory[]>("/v1/memories") ?? [];
  if (memories.length === 0) return null;
  const act = (m: Memory, t: string) => void decide(`agent.memory.${t}`, { type: "agent.memory", id: m.id }, {});
  return (
    <div className="grid gap-1">
      <div className="text-xs text-muted">{t("What agents remember about you")}</div>
      {memories.map((m) => (
        <Card key={m.id} className="flex items-center gap-2 px-3 py-2 text-sm">
          <Tag label={m.state === "proposed" ? t("proposed") : t("remembered")} tone={m.state === "proposed" ? "warning" : "success"} />
          <span className="flex-1">{m.withheld ? <em className="text-muted">{t("It came from records you may no longer read.")}</em> : m.fact}
            {" "}<span className="text-xs text-muted">{m.agent}{m.expires ? ` · ${t("until {date}", { date: m.expires.slice(0, 10) })}` : ` · ${t("kept")}`}</span></span>
          {(m.state === "proposed" || m.expires) && <Button size="sm" onClick={() => act(m, "keep")}>{t("Keep")}</Button>}
          <Button size="sm" variant="ghost" onClick={() => act(m, "forget")}>{t("Forget")}</Button>
        </Card>
      ))}
    </div>
  );
}

/** Global search: every record of every type the member may read, by text. */
export function Search({ initial = "" }: { initial?: string }) {
  const { client, entities } = useHost();
  const openRecord = useOpenRecord();
  const [q, setQ] = useState(initial);
  const [hits, setHits] = useState<Hit[]>();
  const [passages, setPassages] = useState<Passage[]>([]);
  const run = async (text: string) => {
    const q = encodeURIComponent(text);
    setHits(text.trim() ? await client.get<Hit[]>(`/v1/search?q=${q}`) : undefined);
    setPassages(text.trim() ? await client.get<Passage[]>(`/v1/knowledge?q=${q}`) : []);
  };
  const title = (type: string) => entities.find((e) => e.type === type)?.title ?? type;
  return (
    <div className="grid max-w-3xl gap-3">
      <PageHeader title={t("Search")} description={t("Records of every app you work in, and the knowledge you may read, by text, within what you may see.")} />
      <Form onSubmit={() => void run(q)}>
        <Input aria-label={t("Search")} autoFocus placeholder={t("Search records")} value={q} onChange={(e) => setQ(e.target.value)} />
      </Form>
      {passages.length > 0 && (
        <div className="grid gap-2">
          <div className="text-xs text-muted">{t("Knowledge")}</div>
          {passages.map((p) => (
            <Card key={`${p.document}#${p.chunk}`} className="p-3 text-sm">
              <Button variant="link" className="font-medium" onClick={() => openRecord(p.document.split("#")[0]!)}>{p.title}</Button>
              <p className="mt-1 line-clamp-4 whitespace-pre-wrap text-muted">{p.text}</p>
            </Card>
          ))}
        </div>
      )}
      {hits && (hits.length === 0 ? <p className="text-sm text-muted">{t("No records found.")}</p> : (
        <ul className="grid gap-1">
          {hits.map((h) => (
            <li key={`${h.type}/${h.id}`}>
              <Button variant="row" onClick={() => openRecord(h)}>
                <span className="w-40 shrink-0 truncate text-xs text-muted">{title(h.type)}</span>
                <span className="truncate">{h.title}</span><span className="font-mono text-xs text-muted">{h.id}</span>
              </Button>
            </li>
          ))}
        </ul>
      ))}
    </div>
  );
}
