import { Button } from "@astryxdesign/core/Button";
import { Icon } from "@astryxdesign/core/Icon";
import { HStack } from "@astryxdesign/core/Layout";
import { MoreMenu } from "@astryxdesign/core/MoreMenu";
import { Tooltip } from "@astryxdesign/core/Tooltip";
import type { Api } from "../host";
import { t } from "../i18n";
import { stepIntent, useAct } from "./act";
import type { Described } from "./describe";

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
  onFailure: (reason: string) => void;
};

export function Transitions({ described, id, values, revision, title, instances, allowed, onFailure }: TransitionsProps) {
  const act = useAct(described);
  const lifecycle = described.lifecycle;
  const current = lifecycle ? String(values[lifecycle.field] ?? "") : "";
  const offered = described.steps.filter((step) => step.transition.from.includes(current));
  const link = instances && typeof values[instances.field] === "string" && values[instances.field] ? `${instances.path}/${encodeURIComponent(String(values[instances.field]))}` : undefined;

  const click = (step: (typeof offered)[number]) => act.run(stepIntent(described, step, current, { id, revision, name: title }, { onRefused: onFailure }));

  const offeredMore = described.other.flatMap((action) => {
    const verdict = allowed?.find((entry) => entry.schema === action.schema);
    return verdict ? [{ action, verdict }] : [];
  });
  const items = offeredMore.map(({ action, verdict }) => {
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
      onClick: () => act.run({ action, id, revision, label, name: title, reversible: false, onRefused: onFailure }),
    };
  });

  return (
    <>
      <HStack gap={2} vAlign="center">
        {link && <Button label={t("Open records")} variant="secondary" href={link} />}
        {offered.map((step, index) => (
          <Button key={step.transition.name} label={t(step.transition.title)} variant={index === 0 ? "primary" : "secondary"} isDisabled={act.busy} onClick={() => click(step)} />
        ))}
        {items.length > 0 && <MoreMenu label={t("More actions")} items={items} alignment="end" />}
      </HStack>
      {act.element}
    </>
  );
}
