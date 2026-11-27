import type { Api } from "./contract";
import { changed, changedScopes } from "./read";
import { reasonOf, refusal, request } from "./request";
import { t } from "../i18n";
import { currentSession, requireSession, type Session } from "./session";

type Identity = { tenantId: string; principalId: string };
type Declaration = { dataClass: string; authorityId: string };
type Accepted = { record?: { changeId: string; revision: number; submission?: { target?: { id?: string } } }; error?: { code?: string; message?: string } };

type SessionMeta = {
  session: Session | null;
  identity?: Promise<Identity>;
  declarations?: Promise<Declaration[]>;
};
let cached: SessionMeta | undefined;

function declarationsOf(meta: SessionMeta) {
  const pending = request<Declaration[]>("/v1/declarations").catch((err) => {
    if (meta.declarations === pending) meta.declarations = undefined;
    throw err;
  });
  meta.declarations = pending;
  return pending;
}

function sessionMeta(session: Session | null) {
  requireSession(session);
  if (!cached || cached.session !== session) cached = { session };
  const meta = cached;
  if (!meta.identity) {
    const pending = request<Identity>("/v1/me").catch((err) => {
      if (meta.identity === pending) meta.identity = undefined;
      throw err;
    });
    meta.identity = pending;
  }
  if (!meta.declarations) declarationsOf(meta);
  return meta as SessionMeta & { identity: Promise<Identity>; declarations: Promise<Declaration[]> };
}

async function submit(session: Session | null, action: Api.Action, id: string, payload: Record<string, unknown>, expectedRevision: number | undefined, idempotencyKey: string) {
  const meta = sessionMeta(session);
  let [identity, declarations] = await Promise.all([meta.identity, meta.declarations]);
  requireSession(session);
  let authority = declarations.find((declaration) => declaration.dataClass === action.target)?.authorityId ?? "";
  if (!authority) {
    declarations = await declarationsOf(meta);
    requireSession(session);
    authority = declarations.find((declaration) => declaration.dataClass === action.target)?.authorityId ?? "";
  }
  const bytes = new TextEncoder().encode(JSON.stringify(payload));
  const answer = await request<Accepted>("/v1/submissions", {
    headers: action.schemaVersion ? { "Platform-Schema-Version": action.schemaVersion.version } : {},
    body: {
      tenantId: identity.tenantId,
      principalId: identity.principalId,
      authority,
      target: { type: action.target, id },
      schema: { name: action.schema, version: action.schemaVersion?.family ?? 1 },
      idempotencyKey,
      expectedRevision,
      payload: btoa(String.fromCharCode(...bytes)),
    },
  });
  requireSession(session);
  if (!answer.record) throw refusal(200, answer);
  const scopes: string[] = [];
  if (authority) scopes.push(`owner:${authority}`);
  if (action.target === "notification" || action.schema.includes("notification")) scopes.push("notifications");
  if (!action.schema.endsWith(".edit")) changed();
  else if (action.target || scopes.length > 0) changedScopes(action.target ? [action.target] : undefined, scopes);
  else changed();
  return { ...answer.record, id: answer.record.submission?.target?.id || id };
}

const attempts = 3;

export async function send(action: Api.Action, id: string, payload: Record<string, unknown>, revision?: number) {
  const session = currentSession();
  const key = crypto.randomUUID();
  for (let attempt = 1; ; attempt += 1) {
    requireSession(session);
    try {
      return await submit(session, action, id, payload, revision, key);
    } catch (problem) {
      if (!(problem instanceof TypeError) || attempt === attempts) throw problem;
    }
  }
}

export type BatchRow = { id: string; name: string; revision: number; reason?: string };

export async function sendBatch(action: Api.Action, rows: BatchRow[], payload: Record<string, unknown>, retry: boolean) {
  const session = currentSession();
  const results: BatchRow[] = [];
  for (const row of rows) {
    try {
      requireSession(session);
      let revision = row.revision;
      if (retry) {
        const current = await request<Api.RecordView>(`/v1/records/${action.target}/${encodeURIComponent(row.id)}`);
        const choice = current.actions?.find((candidate) => candidate.schema === action.schema);
        if (!choice?.allowed) throw new Error(choice?.reason || t("This action is no longer available."));
        revision = (current.record as { revision: number }).revision;
      }
      requireSession(session);
      await send(action, row.id, payload, revision);
      results.push({ ...row, reason: undefined });
    } catch (problem) {
      results.push({ ...row, reason: reasonOf(problem) });
    }
  }
  return results;
}
