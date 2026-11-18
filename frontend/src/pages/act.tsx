import { AlertDialog } from "@astryxdesign/core/AlertDialog";
import { useState } from "react";
import { type Api, submit } from "../host";
import { t } from "../i18n";
import { ActionDialog } from "./ActionDialog";
import type { Described, Values } from "./describe";
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
};

export function useAct(described: Described) {
  const [pending, setPending] = useState<Intent>();
  const [busy, setBusy] = useState(false);

  const fire = async (intent: Intent) => {
    intent.onRefused("");
    setBusy(true);
    try {
      await send(intent.action, intent.id, {}, intent.revision);
    } catch (problem) {
      intent.onRefused(reasonOf(problem));
    }
    setBusy(false);
    setPending(undefined);
  };

  const run = (intent: Intent) => {
    if (intent.action.payload.some(enterable) || !intent.reversible) setPending(intent);
    else fire(intent);
  };

  const asks = Boolean(pending?.action.payload.some(enterable));
  const element = !pending ? null : asks ? (
    <ActionDialog
      action={pending.action}
      described={described.entity}
      heading={pending.label}
      confirm={pending.label}
      id={pending.id}
      revision={pending.revision}
      onClose={() => setPending(undefined)}
      onDone={() => setPending(undefined)}
    />
  ) : (
    <AlertDialog
      isOpen
      onOpenChange={(open) => !open && !busy && setPending(undefined)}
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
