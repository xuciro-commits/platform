import { AlertDialog } from "@astryxdesign/core/AlertDialog";
import { Button } from "@astryxdesign/core/Button";
import { useToast } from "@astryxdesign/core/Toast";
import { useRef, useState } from "react";
import { recordHref } from "../apps/entries";
import { type Api, request, submit, useRead } from "../host";
import { t } from "../i18n";
import { ActionDialog } from "./ActionDialog";
import type { Described, Step, Values } from "./describe";
import { enterable } from "./ParameterInput";
import { PublishDialog } from "./PublishDialog";
import { navigate, usePath } from "../router";
import { plain } from "./values";

const attempts = 3;

export function reasonOf(problem: unknown) {
  return (problem instanceof Error && problem.message) || t("The platform did not accept this change.");
}

export async function send(action: Api.Action, id: string, payload: Values, revision?: number) {
  const key = crypto.randomUUID();
  for (let attempt = 1; ; attempt += 1) {
    try {
      return await submit(action, id, payload, revision, key);
    } catch (problem) {
      if (!(problem instanceof TypeError) || attempt === attempts) throw problem;
    }
  }
}

export type BatchRow = { id: string; name: string; revision: number; reason?: string };

export async function sendBatch(action: Api.Action, rows: BatchRow[], payload: Values, retry: boolean) {
  const results: BatchRow[] = [];
  for (const row of rows) {
    try {
      let revision = row.revision;
      if (retry) {
        const current = await request<Api.RecordView>(`/v1/records/${action.target}/${encodeURIComponent(row.id)}`);
        const choice = current.actions?.find((candidate) => candidate.schema === action.schema);
        if (!choice?.allowed) throw new Error(choice?.reason || t("This action is no longer available."));
        revision = (current.record as { revision: number }).revision;
      }
      await send(action, row.id, payload, revision);
      results.push({ ...row, reason: undefined });
    } catch (problem) {
      results.push({ ...row, reason: reasonOf(problem) });
    }
  }
  return results;
}

export type Intent = {
  action: Api.Action;
  id: string;
  revision?: number;
  fixed?: Values;
  initial?: Values;
  label: string;
  name: string;
  consequence?: string;
  publishProblem?: string;
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
  to = transition.to[0],
): Intent {
  const lifecycle = described.lifecycle;
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

export function useAct(described?: Described) {
  const [pending, setPending] = useState<Intent>();
  const [busy, setBusy] = useState(false);
  const toast = useToast();
  const firing = useRef(false);
  const path = usePath();
  const views = useRead<Api.ApplicationView[]>("/v1/applications");

  const land = async (intent: Intent) => {
    const field = intent.action.open;
    const type = field ? described?.field(field)?.ref : undefined;
    if (!field || !type) return;
    const subject = await request<Api.RecordView>(`/v1/records/${intent.action.target}/${encodeURIComponent(intent.id)}`);
    const id = plain(subject.values?.[field]);
    if (typeof id !== "string" || !id) return;
    const app = path.split("/")[1];
    const page = views.status === "ready"
      ? views.data.find((view) => (view.name || view.app) === app)?.menu?.flatMap((group) => group.entries).find((entry) => entry.object === type)
      : undefined;
    await navigate(page ? `/${app}/${page.name}/${encodeURIComponent(id)}` : recordHref(type, id));
  };

  const cancel = () => {
    pending?.onCancel?.();
    setPending(undefined);
  };

  const fire = async (intent: Intent) => {
    intent.onRefused("");
    firing.current = true;
    setBusy(true);
    try {
      const kept = await send(intent.action, intent.id, intent.fixed ?? {}, intent.revision);
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
      await land(intent);
    } catch (problem) {
      intent.onRefused(reasonOf(problem));
    }
    firing.current = false;
    setBusy(false);
    setPending(undefined);
  };

  const asked = (intent: Intent) => intent.action.payload.some((parameter) => enterable(parameter) && !(parameter.name in (intent.fixed ?? {})));
  const run = (intent: Intent) => {
    if (intent.action.schema === "build.object.publish" || intent.action.needsApproval || asked(intent) || !(intent.reversible || intent.action.reverse)) setPending(intent);
    else fire(intent);
  };

  const element = !pending ? null : asked(pending) ? (
    <ActionDialog
      action={pending.action}
      described={described?.entity}
      heading={pending.label}
      confirm={pending.initial ? t("Save") : pending.label}
      id={pending.id}
      revision={pending.revision}
      fixed={pending.fixed}
      initial={pending.initial}
      onClose={cancel}
      onDone={() => {
        pending.onDone?.();
        setPending(undefined);
        void land(pending);
      }}
    />
  ) : pending.action.schema === "build.object.publish" ? (
    <PublishDialog intent={pending} cancel={cancel} confirm={() => fire(pending)} busy={busy} />
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
