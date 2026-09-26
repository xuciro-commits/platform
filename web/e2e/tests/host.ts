// What the tests need of the host: decisions made over its API as a member,
// and the workspace opened as a member (development tokens: the token is the subject).
import type { APIRequestContext, Page } from "@playwright/test";

let n = 0;

/** Submits a decision as the member with token; resolves to the host's answer. */
export async function decide(request: APIRequestContext, token: string, app: string, schema: string, target: { type: string; id: string }, payload: unknown) {
  const headers = { Authorization: `Bearer ${token}` };
  const me = await (await request.get("/v1/me", { headers })).json();
  const body = {
    tenantId: me.tenantId, principalId: me.principalId, authority: app, idempotencyKey: `e2e-${Date.now()}-${n++}`,
    schema: { name: schema, version: 1 }, target, payload: Buffer.from(JSON.stringify(payload)).toString("base64"),
  };
  const answer = await (await request.post("/v1/submissions", { headers, data: body })).json();
  if (answer.error) throw new Error(`${schema} ${target.id}: ${JSON.stringify(answer.error)}`);
  return { ...answer, key: body.idempotencyKey };
}

/** Opens the workspace as the member with token, at a route (the part after #). */
export async function open(page: Page, token: string, route: string) {
  await page.addInitScript((t) => sessionStorage.setItem("workspace:identity", t), token);
  await page.goto(`/#${route}`);
}

/** A fresh ID, so tests never meet each other's records on the shared host. */
export const fresh = (prefix: string) => `${prefix}-${Date.now().toString(36).toUpperCase()}${n++}`;
