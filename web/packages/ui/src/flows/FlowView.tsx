// A flow instance (ADR-0020): its definition drawn as steps, with the path the
// instance took, where it stands now, and why it moved — the decision trace.
import { StatusTag, defineStatuses } from "../components/StatusTag";
import { cn } from "../lib/cn";
import { t } from "../i18n";

export type FlowStep = { name: string; title: string; kind: string; next: string[]; chooses?: boolean };
export type FlowDefinition = { id: string; app: string; title: string; version: number; start: string[]; steps: FlowStep[] };
export type FlowToken = { id: number; step: string; branch?: string; waits?: string; attempts?: number; due?: string; task?: string; child?: string; error?: string };
export type FlowTrace = { at: string; step?: string; what: string; detail?: string; by?: string };
export type FlowInstanceData = {
  id: string; flow: string; title: string; version: number; key: string; state: string; onBehalf?: string; answer?: string; parent?: string;
  tokens: FlowToken[] | null; undo: { step: string; action: string; target: string }[] | null; trace: FlowTrace[] | null;
};

export const flowStates = defineStatuses({
  running: { label: t("Running"), tone: "info" }, waiting: { label: t("Waiting"), tone: "info" }, done: { label: t("Done"), tone: "success" },
  compensating: { label: t("Undoing"), tone: "warning" }, compensated: { label: t("Undone"), tone: "neutral" }, canceled: { label: t("Canceled"), tone: "neutral" },
  stuck: { label: t("Stuck"), tone: "danger" },
});

const kinds: Record<string, string> = { act: t("Act"), wait: t("Wait"), ask: t("Ask"), call: t("Sub-flow"), all: t("All of"), any: t("Any of"), agent: t("Agent") };
const waits: Record<string, string> = { ready: "ready", retry: "retrying", wait: "waiting", ask: "asking people", call: "in a sub-flow", join: "waiting for its branches", undo: "retrying an undo", stuck: "stuck" };

/** The steps of a flow, marked by what the instance did in each. */
export function FlowView({ definition, instance, actions }: {
  definition?: FlowDefinition; instance: FlowInstanceData; actions?: (token: FlowToken) => React.ReactNode;
}) {
  const trace = instance.trace ?? [];
  const tokens = instance.tokens ?? [];
  const visited = new Set(trace.map((t) => t.step).filter(Boolean));
  const undone = new Set(trace.filter((t) => t.what === "undone").map((t) => t.step));
  return (
    <div className="grid gap-4">
      <div className="flex flex-wrap items-center gap-2 text-sm">
        <StatusTag status={instance.state} registry={flowStates} />
        <span className="font-semibold">{instance.title}</span>
        <span className="text-xs text-muted">{instance.flow} {t("· version")} {instance.version}{instance.onBehalf ? ` · ${t("on behalf of")} ${instance.onBehalf}` : ""}{instance.parent ? ` · ${t("called by")} ${instance.parent}` : ""}</span>
      </div>
      <ol className="grid gap-1.5" aria-label={t("Steps")}>
        {(definition?.steps ?? []).map((s, i) => {
          const here = tokens.filter((t) => t.step === s.name);
          return (
            <li key={s.name} className={cn("grid grid-cols-[24px_1fr] gap-2 rounded-md border px-2 py-1.5 text-sm",
              here.length ? "border-[var(--tone-info)] bg-row-selected" : "border-border bg-surface")}>
              <span className={cn("mt-0.5 grid size-5 place-items-center rounded-full text-xs font-medium",
                here.some((t) => t.waits === "stuck") ? "bg-[var(--tone-danger)] text-white"
                : here.length ? "bg-[var(--tone-info)] text-white"
                : undone.has(s.name) ? "bg-border text-muted line-through"
                : visited.has(s.name) ? "bg-[var(--tone-success)] text-white" : "bg-border text-muted")}>{i + 1}</span>
              <span className="grid gap-0.5">
                <span className="flex flex-wrap items-center gap-2">
                  <span className="font-medium">{s.title}</span>
                  <span className="text-xs text-muted">{kinds[s.kind] ?? s.kind}{s.chooses ? " → " + t("as it decides") + (s.next.length ? ` (${t("or")} ${s.next.join(", ")})` : "") : s.next.length ? ` → ${s.next.join(", ")}` : " → " + t("end")}</span>
                </span>
                {here.map((k) => (
                  <span key={k.id} className="flex flex-wrap items-center gap-2 text-xs">
                    <span className="text-[var(--tone-info)]">{waits[k.waits ?? ""] ?? k.waits}{k.attempts ? t(", attempt {n}", { n: k.attempts + 1 }) : ""}{k.due ? t(", until {when}", { when: new Date(k.due).toLocaleString() }) : ""}</span>
                    {k.error && <span className="text-[var(--tone-danger)]">{k.error}</span>}
                    {actions?.(k)}
                  </span>
                ))}
              </span>
            </li>
          );
        })}
        {tokens.filter((k) => k.step === "@undo").map((k) => (
          <li key={k.id} className="rounded-md border border-[var(--tone-warning)] px-2 py-1.5 text-sm">
            {t("Undoing:")} {(instance.undo ?? []).map((u) => `${u.step} (${u.action} ${u.target})`).reverse().join(", ") || t("nothing left")}
            {k.error && <span className="ml-2 text-xs text-[var(--tone-danger)]">{k.error}</span>} {actions?.(k)}
          </li>
        ))}
      </ol>
      <section>
        <h3 className="mb-1 text-xs uppercase text-muted">{t("Why it moved")}</h3>
        <table className="w-full text-sm">
          <tbody>
            {trace.map((t, i) => (
              <tr key={i} className="border-b border-border align-top">
                <td className="w-40 py-1 pr-2 text-xs text-muted tabular-nums">{new Date(t.at).toLocaleString()}</td>
                <td className="w-28 py-1 pr-2 font-mono text-xs">{t.step ?? ""}</td>
                <td className="w-28 py-1 pr-2">{t.what}</td>
                <td className="py-1 pr-2">{t.detail}</td>
                <td className="w-28 py-1 text-xs text-muted">{t.by}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </section>
    </div>
  );
}
