import type { Api } from "./contract";
import { changed } from "./read";
import { refusal, request } from "./request";

type Identity = { tenantId: string; principalId: string };
type Declaration = { dataClass: string; authorityId: string };
type Accepted = { record?: { changeId: string; revision: number }; error?: { code?: string; message?: string } };

export async function submit(action: Api.Action, id: string, payload: Record<string, unknown>, expectedRevision: number | undefined, idempotencyKey: string) {
  const [identity, declarations] = await Promise.all([request<Identity>("/v1/me"), request<Declaration[]>("/v1/declarations")]);
  const bytes = new TextEncoder().encode(JSON.stringify(payload));
  const answer = await request<Accepted>("/v1/submissions", {
    headers: action.schemaVersion ? { "Platform-Schema-Version": action.schemaVersion.version } : {},
    body: {
      tenantId: identity.tenantId,
      principalId: identity.principalId,
      authority: declarations.find((declaration) => declaration.dataClass === action.target)?.authorityId ?? "",
      target: { type: action.target, id },
      schema: { name: action.schema, version: action.schemaVersion?.family ?? 1 },
      idempotencyKey,
      expectedRevision,
      payload: btoa(String.fromCharCode(...bytes)),
    },
  });
  if (!answer.record) throw refusal(200, answer);
  changed();
  return answer.record;
}
