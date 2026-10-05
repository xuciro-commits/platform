// What the tests need of the host: decisions made over its API as a member,
// and the workspace opened as a member (development tokens: the token is the subject).
import { readFileSync } from "node:fs";
import type { APIRequestContext, Page } from "@playwright/test";

// The editor writes the current profile from the shared application API descriptor.
export const pageUIProfile = JSON.parse(readFileSync(new URL("../../../capabilities/server/platform/pageui/widgets.json", import.meta.url), "utf8")).uiProfile as string;

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

/** Task surfaces share the shell and leave existing editor tabs mounted. */
export async function switchWorkspace(page: Page, title: string) {
  await page.getByRole("button", { name: "Workspaces", exact: true }).click();
  await page.getByRole("menuitemradio", { name: title, exact: true }).click();
}

/** A fresh ID, so tests never meet each other's records on the shared host. */
export const fresh = (prefix: string) => `${prefix}-${Date.now().toString(36).toUpperCase()}${n++}`;

// Keep request-count assertions within one revision; unrelated tenant inputs
// legitimately invalidate all readers. Explicit local writes still refresh.
export async function stableReadRevision(page: Page) {
 await page.route("**/v1/changes",route=>route.fulfill({status:200,contentType:"text/event-stream",body:""}));
}
