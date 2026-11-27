import { Button } from "@astryxdesign/core/Button";
import { InfoTip } from "@astryxdesign/core/InfoTip";
import { HStack } from "@astryxdesign/core/Layout";
import { MoreMenu } from "@astryxdesign/core/MoreMenu";
import type { Api } from "../host";
import { t } from "../i18n";
import { navigate } from "../router";
import { usePageCommands } from "../commandScope";
import { isDangerAction, stepIntent, useAct } from "./act";
import type { Described, Step, Values } from "./describe";
import type { ContributedAction } from "./RecordExtensions";

export type Instances = { field: string; path: string };
export const dots = { info: "accent", success: "success", warning: "warning", danger: "error", neutral: "neutral" } as const;
export type Dot = (typeof dots)[keyof typeof dots] | "blue" | "purple" | "cyan" | "pink" | "green" | "orange" | "red";
export type TransitionsProps = {
  described: Described;
  id: string;
  values: Record<string, unknown>;
  revision: number;
  title: string;
  instances?: Instances;
  allowed?: Api.RecordAction[];
  contributed?: ContributedAction[];
  publishProblem?: string;
  refreshing?: boolean;
  printHref?: string;
  onFailure: (reason: string) => void;
};

function Gated({ label, verdict, busy, danger, onClick }: { label: string; verdict: Api.RecordAction; busy: boolean; danger?: boolean; onClick: () => void }) {
  return (
    <HStack gap={1} vAlign="center">
      <Button label={label} variant={danger ? "destructive" : "primary"} isDisabled={!verdict.allowed || busy} onClick={onClick} />
      {!verdict.allowed && verdict.reason && <InfoTip label={`${t("Why unavailable")} · ${label}`} content={verdict.reason} />}
    </HStack>
  );
}

export function Transitions({ described, id, values, revision, title, instances, allowed, contributed = [], publishProblem, refreshing, printHref, onFailure }: TransitionsProps) {
  const act = useAct(described);
  const busy = act.busy || Boolean(refreshing);
  const lifecycle = described.lifecycle;
  const current = lifecycle ? String(values[lifecycle.field] ?? "") : "";
  const verdicts = new Map(allowed?.map((entry) => [entry.schema, entry]));
  const offered = described.steps.flatMap((step) => {
    const verdict = verdicts.get(step.action.schema);
    return verdict && step.transition.from.includes(current) ? [{ step, verdict }] : [];
  });
  const link = instances && typeof values[instances.field] === "string" && values[instances.field] ? `${instances.path}/${encodeURIComponent(String(values[instances.field]))}` : undefined;

  const click = (step: Step, verdict: Api.RecordAction) => act.run({ ...stepIntent(described, step, current, { id, revision, name: title }, { onRefused: onFailure }), publishProblem, prefill: verdict.inputs as Values | undefined });

  const offeredMore = described.other.flatMap((action) => {
    const verdict = verdicts.get(action.schema);
    return verdict ? [{ action, verdict }] : [];
  });
  const declared = new Set(lifecycle?.transitions.filter((transition) => transition.from.includes(current)).map((transition) => transition.schema));
  const lead = offered.length ? undefined : offeredMore.find(({ action }) => declared.has(action.schema));
  const run = (action: Api.Action, verdict: Api.RecordAction) => act.run({
    action,
    id,
    revision,
    label: t(action.title),
    name: title,
    danger: isDangerAction(action),
    reversible: Boolean(action.reverse),
    prefill: verdict.inputs as Values | undefined,
    onRefused: onFailure,
  });
  const runContributed = (entry: ContributedAction) => act.run({
    action: entry.action,
    described: entry.described,
    id: entry.id,
    revision: entry.revision,
    label: t(entry.action.title),
    name: title,
    danger: isDangerAction(entry.action),
    reversible: Boolean(entry.action.reverse),
    prefill: entry.verdict.inputs as Values | undefined,
    perform: entry.perform,
    onRefused: onFailure,
  });
  const tones = ["success", "info", "neutral", "warning", "danger"];
  const toneOf = (step: Step) => lifecycle?.states.find((state) => state.name === step.transition.to[0])?.tone ?? "neutral";
  const rank = ({ step }: (typeof offered)[number]) => tones.indexOf(toneOf(step));
  const first = offered.reduce<(typeof offered)[number] | undefined>((best, entry) => (!best || rank(entry) < rank(best) ? entry : best), undefined);
  const rest = offered.filter((entry) => entry !== first);
  const items = [
    ...rest.map(({ step, verdict }) => ({
      id: `step:${step.transition.name}`,
      label: t(step.transition.title),
      description: verdict.allowed ? undefined : verdict.reason,
      isDisabled: !verdict.allowed || busy,
      onClick: () => click(step, verdict),
    })),
    ...offeredMore.filter((entry) => entry !== lead).map(({ action, verdict }) => ({
      id: action.schema,
      label: t(action.title),
      description: verdict.allowed ? undefined : verdict.reason,
      isDisabled: !verdict.allowed || busy,
      onClick: () => run(action, verdict),
    })),
    ...contributed.map((entry) => ({
      id: entry.key,
      label: t(entry.action.title),
      description: entry.verdict.allowed ? undefined : entry.verdict.reason,
      isDisabled: !entry.verdict.allowed || busy,
      onClick: () => runContributed(entry),
    })),
    ...(printHref ? [{ id: "record.print", label: t("Print"), description: undefined, isDisabled: false, onClick: () => void navigate(printHref) }] : []),
  ];

  usePageCommands(busy ? [] : [
    ...(first?.verdict.allowed ? [{ id: `step:${first.step.transition.name}`, label: t(first.step.transition.title), run: () => click(first.step, first.verdict) }] : []),
    ...(lead?.verdict.allowed ? [{ id: lead.action.schema, label: t(lead.action.title), run: () => run(lead.action, lead.verdict) }] : []),
    ...items.filter((item) => !item.isDisabled).map((item) => ({ id: item.id, label: item.label, run: item.onClick })),
  ]);

  return (
    <>
      <HStack gap={2} vAlign="center">
        {link && <Button label={t("Open records")} variant="secondary" href={link} />}
        {first && <Gated label={t(first.step.transition.title)} verdict={first.verdict} busy={busy} danger={isDangerAction(first.step.action, toneOf(first.step))} onClick={() => click(first.step, first.verdict)} />}
        {lead && <Gated label={t(lead.action.title)} verdict={lead.verdict} busy={busy} danger={isDangerAction(lead.action)} onClick={() => run(lead.action, lead.verdict)} />}
        {items.length > 0 && <MoreMenu label={t("More actions")} items={items} alignment="end" />}
      </HStack>
      {act.element}
    </>
  );
}
