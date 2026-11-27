import { AlertDialog } from "@astryxdesign/core/AlertDialog";
import { Button } from "@astryxdesign/core/Button";
import { useToast } from "@astryxdesign/core/Toast";
import { useRef, useState } from "react";
import { useRecordHref } from "../recordRoutes";
import { type Api, request, reasonOf, send } from "../host";
import { t } from "../i18n";
import { ActionDialog } from "./ActionDialog";
import type { Described, Step, Values } from "./describe";
import { enterable } from "./fieldRules";
import { PublishDialog } from "./PublishDialog";
import { navigate } from "../router";
import { plain } from "./values";

const dangerPattern = /(?:delete|remove|void|cancel|lost|retire|discard|reject|revoke|terminate|unbind|clear|purge|destroy)/i;

export function isDangerAction(action: Api.Action, stateTone?: string) {
  if (stateTone === "danger") return true;
  return !action.reverse && dangerPattern.test(`${action.schema} ${action.title}`);
}

type Performed = Promise<{ id?: string; revision: number }>;

export type Intent = {
  action: Api.Action;
  described?: Api.EntityInfo;
  id: string;
  revision?: number;
  fixed?: Values;
  initial?: Values;
  prefill?: Values;
  only?: string[];
  label: string;
  name: string;
  consequence?: string;
  targetState?: string;
  danger?: boolean;
  publishProblem?: string;
  reversible: boolean;
  undoAction?: Api.Action;
  perform?: (payload: Values) => Performed;
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
  const reverseStep = to !== current ? described.step(to, current) : undefined;
  const undoAction = action.reverse ? described.action(action.reverse) : reverseStep?.action;
  return {
    action,
    ...record,
    label: t(transition.title),
    consequence: state?.description ? t(state.description) : undefined,
    targetState: state?.title ? t(state.title) : undefined,
    danger: isDangerAction(action, state?.tone),
    reversible: Boolean(undoAction) && !(reverseStep?.action.payload.some(enterable)),
    undoAction: undoAction && !undoAction.payload.some(enterable) ? undoAction : undefined,
    ...callbacks,
  };
}

export function useAct(described?: Described) {
  const [pending, setPending] = useState<Intent>();
  const [busy, setBusy] = useState(false);
  const toast = useToast();
  const firing = useRef(false);
  const href = useRecordHref();

  const land = async (intent: Intent) => {
    const field = intent.action.open;
    const type = field ? described?.field(field)?.ref : undefined;
    if (!field || !type) return;
    const subject = await request<Api.RecordView>(`/v1/records/${intent.action.target}/${encodeURIComponent(intent.id)}`);
    const id = plain(subject.values?.[field]);
    if (typeof id === "string" && id) await navigate(href(type, id));
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
      const kept = intent.perform
        ? await intent.perform(intent.fixed ?? {})
        : await send(intent.action, intent.id, intent.fixed ?? {}, intent.revision);
      const back = intent.undoAction ?? (intent.action.reverse ? described?.action(intent.action.reverse) : undefined);
      if (back) {
        const targetId = kept.id ?? intent.id;
        const dismiss = toast({
          body: `${t("Done")}: ${intent.label} “${intent.name}”`,
          endContent: (
            <Button
              label={t("Undo")}
              variant="ghost"
              onClick={async () => {
                dismiss();
                try {
                  await send(back, targetId, {}, kept.revision);
                } catch (problem) {
                  toast({ body: reasonOf(problem), type: "error" });
                }
              }}
            />
          ),
        });
      } else {
        toast({ body: `${t("Done")}: ${intent.label} “${intent.name}”` });
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
    if (intent.action.schema === "build.object.publish" || intent.action.needsApproval || asked(intent) || intent.danger || !(intent.reversible || intent.action.reverse)) setPending(intent);
    else fire(intent);
  };

  const consequenceOf = (intent: Intent) => {
    if (intent.consequence) return intent.consequence;
    if (intent.targetState) return t("This moves the record to {state} and cannot be undone directly.").replace("{state}", intent.targetState);
    return t("This action takes effect immediately and cannot be undone directly.");
  };

  const element = !pending ? null : asked(pending) ? (
    <ActionDialog
      action={pending.action}
      described={pending.described ?? described?.entity}
      heading={pending.label}
      confirm={pending.initial ? t("Save") : pending.label}
      id={pending.id}
      revision={pending.revision}
      fixed={pending.fixed}
      initial={pending.initial ?? pending.prefill}
      only={pending.only}
      perform={pending.perform ? async (payload) => void (await pending.perform!(payload)) : undefined}
      onClose={cancel}
      onDone={() => {
        toast({ body: `${t("Done")}: ${pending.label} “${pending.name}”` });
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
      description={consequenceOf(pending)}
      cancelLabel={t("Cancel")}
      actionLabel={pending.label}
      actionVariant={pending.danger ? "destructive" : "primary"}
      isActionLoading={busy}
      onAction={() => fire(pending)}
    />
  );

  return { run, busy, element };
}
