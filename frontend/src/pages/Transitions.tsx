import { Button } from "@astryxdesign/core/Button";
import { Icon } from "@astryxdesign/core/Icon";
import { HStack } from "@astryxdesign/core/Layout";
import { MoreMenu } from "@astryxdesign/core/MoreMenu";
import { Tooltip } from "@astryxdesign/core/Tooltip";
import type { Api } from "../host";
import { t } from "../i18n";
import { usePageCommands } from "../shell/CommandSearch";
import { stepIntent, useAct } from "./act";
import type { Described, Step } from "./describe";

export type Instances = { field: string; path: string };
export const dots = { info: "accent", success: "success", warning: "warning", danger: "error", neutral: "neutral" } as const;
export type Dot = (typeof dots)[keyof typeof dots];
export type TransitionsProps = {
  described: Described;
  id: string;
  values: Record<string, unknown>;
  revision: number;
  title: string;
  instances?: Instances;
  allowed?: Api.RecordAction[];
  publishProblem?: string;
  onFailure: (reason: string) => void;
};

export function Transitions({ described, id, values, revision, title, instances, allowed, publishProblem, onFailure }: TransitionsProps) {
  const act = useAct(described);
  const lifecycle = described.lifecycle;
  const current = lifecycle ? String(values[lifecycle.field] ?? "") : "";
  const verdicts = new Map(allowed?.map((entry) => [entry.schema, entry]));
  const offered = described.steps.flatMap((step) => {
    const verdict = verdicts.get(step.action.schema);
    return verdict && step.transition.from.includes(current) ? [{ step, verdict }] : [];
  });
  const link = instances && typeof values[instances.field] === "string" && values[instances.field] ? `${instances.path}/${encodeURIComponent(String(values[instances.field]))}` : undefined;

  const click = (step: Step) => act.run({ ...stepIntent(described, step, current, { id, revision, name: title }, { onRefused: onFailure }), publishProblem });

  const offeredMore = described.other.flatMap((action) => {
    const verdict = verdicts.get(action.schema);
    return verdict ? [{ action, verdict }] : [];
  });
  const declared = new Set(lifecycle?.transitions.filter((transition) => transition.from.includes(current)).map((transition) => transition.schema));
  const lead = offered.length ? undefined : offeredMore.find(({ action }) => declared.has(action.schema));
  const run = (action: Api.Action) => act.run({ action, id, revision, label: t(action.title), name: title, reversible: false, onRefused: onFailure });
  const items = offeredMore.filter((entry) => entry !== lead).map(({ action, verdict }) => {
    const label = t(action.title);
    return {
      id: action.schema,
      label,
      endContent: verdict.allowed || !verdict.reason ? undefined : (
        <Tooltip content={verdict.reason}>
          <Icon icon="info" />
        </Tooltip>
      ),
      isDisabled: !verdict.allowed || act.busy,
      onClick: () => run(action),
    };
  });

  usePageCommands(act.busy ? [] : [
    ...offered.filter(({ verdict }) => verdict.allowed).map(({ step }) => ({
      id: `step:${step.transition.name}`, label: t(step.transition.title), run: () => click(step),
    })),
    ...(lead?.verdict.allowed ? [{ id: lead.action.schema, label: t(lead.action.title), run: () => run(lead.action) }] : []),
    ...items.filter((item) => !item.isDisabled).map((item) => ({ id: item.id, label: item.label, run: item.onClick })),
  ]);

  return (
    <>
      <HStack gap={2} vAlign="center">
        {link && <Button label={t("Open records")} variant="secondary" href={link} />}
        {offered.map(({ step, verdict }, index) => (
          <Button key={step.transition.name} label={t(step.transition.title)} variant={index === 0 ? "primary" : "secondary"}
            isDisabled={!verdict.allowed || act.busy} tooltip={verdict.allowed ? undefined : verdict.reason} onClick={() => click(step)} />
        ))}
        {lead && (
          <Button label={t(lead.action.title)} variant="primary" isDisabled={!lead.verdict.allowed || act.busy}
            tooltip={lead.verdict.allowed ? undefined : lead.verdict.reason} onClick={() => run(lead.action)} />
        )}
        {items.length > 0 && <MoreMenu label={t("More actions")} items={items} alignment="end" />}
      </HStack>
      {act.element}
    </>
  );
}
