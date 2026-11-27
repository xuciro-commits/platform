import type { Api } from "./contract";
import { changed, changedScopes } from "./read";
import { reasonOf, refusal, request } from "./request";
import { t } from "../i18n";
import { currentSession } from "./session";

type Identity = { tenantId: string; principalId: string };
type Declaration = { dataClass: string; authorityId: string };
type Accepted = { record?: { changeId: string; revision: number; submission?: { target?: { id?: string } } }; error?: { code?: string; message?: string } };

let cachedKey = "";
let cachedIdentity: Promise<Identity> | null = null;
let cachedDeclarations: Promise<Declaration[]> | null = null;

function sessionMeta() {
  const session = currentSession();
  const key = `${session?.tenant ?? ""} ${session?.token ?? ""}`;
  if (key !== cachedKey) {
    cachedKey = key;
    cachedIdentity = null;
    cachedDeclarations = null;
  }
  if (!cachedIdentity) cachedIdentity = request<Identity>("/v1/me").catch((err) => { cachedIdentity = null; throw err; });
  if (!cachedDeclarations) cachedDeclarations = request<Declaration[]>("/v1/declarations").catch((err) => { cachedDeclarations = null; throw err; });
  return Promise.all([cachedIdentity, cachedDeclarations]);
}

async function submit(action: Api.Action, id: string, payload: Record<string, unknown>, expectedRevision: number | undefined, idempotencyKey: string) {
  let [identity, declarations] = await sessionMeta();
  let authority = declarations.find((declaration) => declaration.dataClass === action.target)?.authorityId ?? "";
  if (!authority) {
    cachedDeclarations = request<Declaration[]>("/v1/declarations").catch((err) => { cachedDeclarations = null; throw err; });
    declarations = await cachedDeclarations;
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

export async function sendBatch(action: Api.Action, rows: BatchRow[], payload: Record<string, unknown>, retry: boolean) {
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
