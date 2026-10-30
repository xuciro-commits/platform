import { useEffect, useReducer, type ReactNode } from "react";
import type { Api } from "@platform/kernel";
import type { RecordSource } from "./Records";

export type ActionAvailability = { allowed: boolean; reason?: string };
export type ActionTarget = { type: string; id: string; revision: number };

const answers = new Map<string, ActionAvailability | null>();
const pending = new Map<string, Promise<void>>();
const limit = 512;
const debounce = 120;

export function actionAvailability(result: Api.ActionCheckResult): ActionAvailability {
  const failed = result.conditions.find(condition => !condition.allowed);
  const denied = result.authorization?.allow === false;
  if (!denied && !failed && (result.allowed || (result.issues?.length ?? 0) > 0)) return { allowed: true };
  return { allowed: false, reason: failed?.message || result.message || result.authorization?.reason || undefined };
}

export function useActionAvailability(source: RecordSource, target: ActionTarget | undefined, schemas: readonly string[]): Record<string, ActionAvailability | undefined> {
  const [, refresh] = useReducer((n: number) => n + 1, 0);
  const keyOf = (schema: string) => JSON.stringify([source.scope, schema, target?.type, target?.id, target?.revision]);
  const wanted = target && source.checkAction ? [...new Set(schemas)] : [];
  const signature = JSON.stringify([source.scope, target, wanted]);
  useEffect(() => {
    if (!target || !source.checkAction) return;
    let active = true;
    const handle = setTimeout(() => {
      for (const schema of wanted) {
        const key = keyOf(schema);
        if (answers.has(key) || pending.has(key)) continue;
        const request = source.checkAction!(schema, { type: target.type, id: target.id }).then(
          result => { answers.set(key, actionAvailability(result)); },
          () => { answers.set(key, null); },
        ).finally(() => {
          pending.delete(key);
          while (answers.size > limit) answers.delete(answers.keys().next().value!);
        });
        pending.set(key, request);
        void request.then(() => { if (active) refresh(); });
      }
    }, debounce);
    return () => { active = false; clearTimeout(handle); };
  }, [signature]);
  return Object.fromEntries(wanted.map(schema => [schema, answers.get(keyOf(schema)) ?? undefined]));
}

export function Reasoned({ reason, children }: { reason?: string; children: ReactNode }) {
  return reason ? <span title={reason} className="inline-flex">{children}</span> : <>{children}</>;
}
