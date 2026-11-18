import { AlertDialog } from "@astryxdesign/core/AlertDialog";
import { Button } from "@astryxdesign/core/Button";
import { HStack } from "@astryxdesign/core/Layout";
import { useState } from "react";
import { type Api, submit } from "../host";
import { t } from "../i18n";
import { ActionDialog } from "./ActionDialog";
import type { Described } from "./describe";
import { enterable } from "./ParameterInput";

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
  onFailure: (reason: string) => void;
};

export function Transitions({ described, id, values, revision, title, instances, onFailure }: TransitionsProps) {
  const [chosen, setChosen] = useState<Api.TransitionInfo>();
  const [busy, setBusy] = useState(false);
  const lifecycle = described.lifecycle;
  const current = lifecycle ? String(values[lifecycle.field] ?? "") : "";
  const offered = described.steps.filter((step) => step.transition.from.includes(current)).map((step) => step.transition);
  const actionOf = (transition: Api.TransitionInfo) => described.steps.find((step) => step.transition === transition)?.action;
  const target = (transition: Api.TransitionInfo) => transition.to[0];
  const reversible = (transition: Api.TransitionInfo) =>
    target(transition) !== current && (lifecycle?.transitions ?? []).some((other) => other.from.includes(target(transition)) && other.to.includes(current));
  const link = instances && typeof values[instances.field] === "string" && values[instances.field] ? `${instances.path}/${encodeURIComponent(String(values[instances.field]))}` : undefined;

  const run = async (transition: Api.TransitionInfo) => {
    const action = actionOf(transition);
    if (!action) return;
    onFailure("");
    setBusy(true);
    try {
      await submit(action, id, {}, revision);
    } catch (problem) {
      onFailure((problem instanceof Error && problem.message) || t("The platform did not accept this change."));
    }
    setBusy(false);
    setChosen(undefined);
  };

  const click = (transition: Api.TransitionInfo) => {
    const action = actionOf(transition);
    if (action?.payload.some(enterable) || !reversible(transition)) setChosen(transition);
    else run(transition);
  };

  const chosenAction = chosen ? actionOf(chosen) : undefined;
  const asksInput = Boolean(chosenAction?.payload.some(enterable));
  const state = chosen ? lifecycle?.states.find((candidate) => candidate.name === target(chosen)) : undefined;

  return (
    <>
      <HStack gap={2} vAlign="center">
        {link && <Button label={t("Open records")} variant="secondary" href={link} />}
        {offered.map((transition, index) => (
          <Button key={transition.name} label={t(transition.title)} variant={index === 0 ? "primary" : "secondary"} isDisabled={busy} onClick={() => click(transition)} />
        ))}
      </HStack>
      {chosen && chosenAction && asksInput && (
        <ActionDialog
          action={chosenAction}
          described={described.entity}
          heading={t(chosen.title)}
          confirm={t(chosen.title)}
          id={id}
          revision={revision}
          onClose={() => setChosen(undefined)}
          onDone={() => setChosen(undefined)}
        />
      )}
      {chosen && !asksInput && (
        <AlertDialog
          isOpen
          onOpenChange={(open) => !open && !busy && setChosen(undefined)}
          title={`${t(chosen.title)} “${title}”?`}
          description={state?.description ? t(state.description) : t("This cannot be undone here.")}
          actionLabel={t(chosen.title)}
          actionVariant="primary"
          isActionLoading={busy}
          onAction={() => run(chosen)}
        />
      )}
    </>
  );
}
