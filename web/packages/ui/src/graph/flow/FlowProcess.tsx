// A flow instance (ADR-0020): its definition drawn as BPMN steps, with the path the
// instance took, where it stands now, and why it moved — the decision trace.
import type { Api } from "@platform/kernel";
import { StatusTag, defineStatuses } from "../../components/StatusTag";
import { t } from "../../i18n";
import { cn } from "../../lib/cn";
import { FlowSteps, type FlowStepEdge, type FlowStepNode } from "./FlowSteps";
import { loops, notationOf } from "./notation";

export type FlowDefinition = Api.FlowDefinition;
export type FlowToken = { id: number; step: string; branch?: string; waits?: string; attempts?: number; due?: string; task?: string; child?: string; error?: string };
export type FlowTrace = { at: string; step?: string; what: string; detail?: string; by?: string };
export type FlowInstanceData = {
  id: string; flow: string; title: string; version: number; key: string; state: string; subject?: string; onBehalf?: string; answer?: string; parent?: string;
  dependencies?: string; release?: string;
  tokens: FlowToken[] | null; undo: { step: string; action: string; target: string }[] | null; trace: FlowTrace[] | null;
};

export const flowStates = defineStatuses({
  running: { label: t("Running"), tone: "info" }, waiting: { label: t("Waiting"), tone: "info" }, done: { label: t("Done"), tone: "success" },
  compensating: { label: t("Undoing"), tone: "warning" }, compensated: { label: t("Undone"), tone: "neutral" }, canceled: { label: t("Canceled"), tone: "neutral" },
  stuck: { label: t("Stuck"), tone: "danger" },
});

/** A run's immutable startup binding, independent of today's active pointer. */
export function FlowReleaseBinding({ dependencies, release }: Pick<FlowInstanceData, "dependencies" | "release">) {
  if (!dependencies && !release) return null;
  return <details className="min-w-0 text-xs text-muted">
    <summary className="cursor-pointer">{t("Release binding")}</summary>
    <p className="mt-2">{t("This run stays on its recorded version.")}</p>
    <dl className="mt-2 grid gap-1 break-all">
      <dt className="font-semibold">{t("Dependency release")}</dt><dd>{dependencies}</dd>
      <dt className="font-semibold">{t("Started under release")}</dt><dd>{release || t("Development run")}</dd>
    </dl>
  </details>;
}

const kinds: Record<string, string> = { act: t("Act"), wait: t("Wait"), ask: t("Ask"), call: t("Sub-flow"), all: t("All of"), any: t("Any of"), agent: t("Agent") };
const waits: Record<string, string> = { ready: "ready", retry: "retrying", wait: "waiting", ask: "asking people", call: "in a sub-flow", join: "waiting for its branches", undo: "retrying an undo", stuck: "stuck" };

/** A flow drawn in BPMN (#122, ADR-0086 D4): its steps from the start event to the
 * end event, each in the shape its kind asks for, and, for an instance, what it did. */
export function FlowGraph({ definition, instance, height = 260 }: { definition: FlowDefinition; instance?: FlowInstanceData; height?: number }) {
  const trace = instance?.trace ?? [];
  const tokens = instance?.tokens ?? [];
  const visited = new Set(trace.map((x) => x.step).filter(Boolean));
  const undone = new Set(trace.filter((x) => x.what === "undone").map((x) => x.step));
  const nodes: FlowStepNode[] = [{ id: "@start", label: t("Start"), detail: (definition.start ?? []).join(", "), notation: "event-start", tone: instance ? "success" : undefined }];
  const edges: FlowStepEdge[] = definition.steps[0] ? [{ from: "@start", to: definition.steps[0]!.name }] : [];
  for (const s of definition.steps) {
    const here = tokens.filter((k) => k.step === s.name);
    nodes.push({
      id: s.name, label: s.title, current: here.length > 0, notation: notationOf(s.kind), loop: loops.has(s.kind),
      detail: here.length ? here.map((k) => waits[k.waits ?? ""] ?? k.waits).join(", ") : undone.has(s.name) ? t("undone") : kinds[s.kind] ?? s.kind,
      tone: here.some((k) => k.waits === "stuck") ? "danger" : here.length ? "info" : undone.has(s.name) ? "neutral" : visited.has(s.name) ? "success" : undefined,
    });
    for (const next of s.next) edges.push({ from: s.name, to: next, dashed: s.chooses, label: s.chooses ? t("as it decides") : undefined });
    if (!s.next.length || s.chooses) edges.push({ from: s.name, to: "@end", dashed: s.chooses });
  }
  // A step that chooses its next step at run time names none; the steps nothing
  // else leads to are the ones it may choose.
  const reached = new Set(edges.map((e) => e.to));
  const orphans = definition.steps.filter((s) => !reached.has(s.name)).map((s) => s.name);
  for (const s of definition.steps) {
    if (s.chooses && !s.next.length) for (const o of orphans) if (o !== s.name) edges.push({ from: s.name, to: o, dashed: true, label: t("as it decides") });
  }
  nodes.push({ id: "@end", label: t("End"), notation: "event-end", tone: instance?.state === "done" ? "success" : undefined });
  return <FlowSteps nodes={nodes} edges={edges} height={height} label={t("Steps")} />;
}

/** A flow instance: its graph, where it waits and what can be done there, and why it moved. */
export function FlowRun({ definition, instance, actions, onStepSelect }: {
  definition?: FlowDefinition; instance: FlowInstanceData; actions?: (token: FlowToken) => React.ReactNode;
  onStepSelect?: (step: string) => void;
}) {
  const trace = instance.trace ?? [];
  const tokens = instance.tokens ?? [];
  const title = (step: string) => definition?.steps.find((s) => s.name === step)?.title ?? step;
  return (
    <div className="grid gap-4">
      <div className="flex flex-wrap items-center gap-2 text-sm">
        <StatusTag status={instance.state} registry={flowStates} />
        <span className="font-semibold">{instance.title}</span>
        <span className="text-xs text-muted">{instance.flow} {t("· version")} {instance.version}{instance.onBehalf ? ` · ${t("on behalf of")} ${instance.onBehalf}` : ""}{instance.parent ? ` · ${t("called by")} ${instance.parent}` : ""}</span>
      </div>
      <FlowReleaseBinding dependencies={instance.dependencies} release={instance.release} />
      {definition && <FlowGraph definition={definition} instance={instance} />}
      {tokens.length > 0 && (
        <ul className="grid gap-1.5" aria-label={t("Where it stands")}>
          {tokens.map((k) => (
            <li key={k.id} className={cn("flex flex-wrap items-center gap-2 rounded-md border px-2 py-1.5 text-sm",
              k.waits === "stuck" ? "border-[var(--tone-danger)]" : k.step === "@undo" ? "border-[var(--tone-warning)]" : "border-[var(--tone-info)]")}>
              {k.step === "@undo"
                ? <span>{t("Undoing:")} {(instance.undo ?? []).map((u) => `${u.step} (${u.action} ${u.target})`).reverse().join(", ") || t("nothing left")}</span>
                : <span className="font-medium">{title(k.step)}</span>}
              {k.step !== "@undo" && <span className="text-xs text-[var(--tone-info)]">{waits[k.waits ?? ""] ?? k.waits}{k.attempts ? t(", attempt {n}", { n: k.attempts + 1 }) : ""}{k.due ? t(", until {when}", { when: new Date(k.due).toLocaleString() }) : ""}</span>}
              {k.error && <span className="text-xs text-[var(--tone-danger)]">{k.error}</span>}
              {actions?.(k)}
            </li>
          ))}
        </ul>
      )}
      <section>
        <h3 className="mb-1 text-xs uppercase text-muted">{t("Why it moved")}</h3>
        <table className="w-full text-sm">
          <tbody>
            {trace.map((line, i) => (
              <tr key={i} className="border-b border-border align-top">
                <td className="w-40 py-1 pr-2 text-xs text-muted tabular-nums">{new Date(line.at).toLocaleString()}</td>
                <td className="w-28 py-1 pr-2 font-mono text-xs">{line.step && onStepSelect
                  ? <button type="button" className="text-left text-primary underline-offset-2 hover:underline focus-visible:underline" onClick={() => onStepSelect(line.step!)}>{title(line.step)}</button>
                  : line.step ?? ""}</td>
                <td className="w-28 py-1 pr-2">{line.what}</td>
                <td className="py-1 pr-2">{line.detail}</td>
                <td className="w-28 py-1 text-xs text-muted">{line.by}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </section>
    </div>
  );
}
