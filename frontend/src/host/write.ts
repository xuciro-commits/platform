import type { Api } from "./contract";
import { changed, changedScopes } from "./read";
import { refusal, request } from "./request";
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

export async function submit(action: Api.Action, id: string, payload: Record<string, unknown>, expectedRevision: number | undefined, idempotencyKey: string) {
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
  if (action.target || scopes.length > 0) changedScopes(action.target ? [action.target] : undefined, scopes);
  else changed();
  return { ...answer.record, id: answer.record.submission?.target?.id || id };
}
