import { AlertDialog } from "@astryxdesign/core/AlertDialog";
import { Button } from "@astryxdesign/core/Button";
import { useToast } from "@astryxdesign/core/Toast";
import { useRef, useState } from "react";
import { type Api, submit } from "../host";
import { t } from "../i18n";
import { ActionDialog } from "./ActionDialog";
import type { Described, Step, Values } from "./describe";
import { enterable } from "./ParameterInput";

const attempts = 3;

export function reasonOf(problem: unknown) {
  return (problem instanceof Error && problem.message) || t("The platform did not accept this change.");
}

export async function send(action: Api.Action, id: string, payload: Values, revision: number) {
  const key = crypto.randomUUID();
  for (let attempt = 1; ; attempt += 1) {
    try {
      return await submit(action, id, payload, revision, key);
    } catch (problem) {
      if (!(problem instanceof TypeError) || attempt === attempts) throw problem;
    }
  }
}

export type Intent = {
  action: Api.Action;
  id: string;
  revision: number;
  label: string;
  name: string;
  consequence?: string;
  reversible: boolean;
  onRefused: (reason: string) => void;
  onDone?: () => void;
  onCancel?: () => void;
};

export function stepIntent(
  described: Described,
  { transition, action }: Step,
  current: string,
  record: { id: string; revision: number; name: string },
  callbacks: Pick<Intent, "onRefused" | "onDone" | "onCancel">,
): Intent {
  const lifecycle = described.lifecycle;
  const to = transition.to[0];
  const state = lifecycle?.states.find((candidate) => candidate.name === to);
  return {
    action,
    ...record,
    label: t(transition.title),
    consequence: state?.description ? t(state.description) : undefined,
    reversible: to !== current && (lifecycle?.transitions ?? []).some((other) => other.from.includes(to) && other.to.includes(current)),
    ...callbacks,
  };
}

export function useAct(described: Described | undefined) {
  const [pending, setPending] = useState<Intent>();
  const [busy, setBusy] = useState(false);
  const toast = useToast();
  const firing = useRef(false);

  const cancel = () => {
    pending?.onCancel?.();
    setPending(undefined);
  };

  const fire = async (intent: Intent) => {
    intent.onRefused("");
    firing.current = true;
    setBusy(true);
    try {
      const kept = await send(intent.action, intent.id, {}, intent.revision);
      const back = intent.action.reverse ? described?.action(intent.action.reverse) : undefined;
      if (back) {
        const dismiss = toast({
          body: `${t("Done")}: ${intent.label} “${intent.name}”`,
          endContent: (
            <Button
              label={t("Undo")}
              variant="ghost"
              onClick={async () => {
                dismiss();
                try {
                  await send(back, intent.id, {}, kept.revision);
                } catch (problem) {
                  toast({ body: reasonOf(problem), type: "error" });
                }
              }}
            />
          ),
        });
      }
      intent.onDone?.();
    } catch (problem) {
      intent.onRefused(reasonOf(problem));
    }
    firing.current = false;
    setBusy(false);
    setPending(undefined);
  };

  const run = (intent: Intent) => {
    if (intent.action.payload.some(enterable) || !(intent.reversible || intent.action.reverse)) setPending(intent);
    else fire(intent);
  };

  const asks = Boolean(pending?.action.payload.some(enterable));
  const element = !pending || !described ? null : asks ? (
    <ActionDialog
      action={pending.action}
      described={described.entity}
      heading={pending.label}
      confirm={pending.label}
      id={pending.id}
      revision={pending.revision}
      onClose={cancel}
      onDone={() => {
        pending.onDone?.();
        setPending(undefined);
      }}
    />
  ) : (
    <AlertDialog
      isOpen
      onOpenChange={(open) => !open && !firing.current && cancel()}
      title={`${pending.label} “${pending.name}”?`}
      description={pending.consequence || t("This cannot be undone here.")}
      actionLabel={pending.label}
      actionVariant="primary"
      isActionLoading={busy}
      onAction={() => fire(pending)}
    />
  );

  return { run, busy, element };
}
