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
  const actions = await (await request.get("/v1/actions", { headers })).json();
  const binding = actions.find((action: any) => action.schema === schema)?.schemaVersion;
  const body = {
    tenantId: me.tenantId, principalId: me.principalId, authority: app, idempotencyKey: `e2e-${Date.now()}-${n++}`,
    schema: { name: schema, version: binding?.family ?? 1 }, target, payload: Buffer.from(JSON.stringify(payload)).toString("base64"),
  };
  const answer = await (await request.post("/v1/submissions", { headers: { ...headers, ...(binding ? { "Platform-Schema-Version": binding.version } : {}) }, data: body })).json();
  if (answer.error) throw new Error(`${schema} ${target.id}: ${JSON.stringify(answer.error)}`);
  return { ...answer, key: body.idempotencyKey };
}

/** Opens the workspace as the member with token, at a route (the part after #). */
export async function open(page: Page, token: string, route: string) {
  // A hash-only navigation does not run init scripts. Read the current member
  // from this document's test URL instead of accumulating token-capturing
  // scripts that can restore an earlier identity when the same page reloads.
  await page.addInitScript(() => {
    const member = new URL(location.href).searchParams.get("workspaceTestMember");
    if (member) sessionStorage.setItem("workspace:identity", member);
  });
  await page.goto(`/?workspaceTestMember=${encodeURIComponent(token)}#${route}`);
}

/** Task surfaces share the shell and leave existing editor tabs mounted. */
/** Opens a platform application from the header's application switcher; "Home" goes to the rail's Home. */
export async function switchWorkspace(page: Page, title: string) {
  if (title === "Home") { await page.getByRole("button", { name: "Home", exact: true }).first().click(); return; }
  await page.getByRole("button", { name: "Switch application", exact: true }).click();
  await page.getByRole("menuitemradio", { name: title, exact: true }).click();
}

/** A fresh ID, so tests never meet each other's records on the shared host. */
export const fresh = (prefix: string) => `${prefix}-${Date.now().toString(36).toUpperCase()}${n++}`;

// Keep request-count assertions within one revision; unrelated tenant inputs
// legitimately invalidate all readers. Explicit local writes still refresh.
export async function stableReadRevision(page: Page) {
  await page.route("**/v1/changes", route => route.fulfill({ status: 200, contentType: "text/event-stream", body: "" }));
}

/** Retains an old action catalog in both HTTP discovery and live snapshots.
 * Other reads remain live; release lets the original refresh learn current definitions. */
export async function retainActionCatalog(page: Page, actions: unknown[]) {
  await page.addInitScript(retained => {
    const state: { retained?: unknown[]; current?: any[] } = { retained };
    (window as any).__retainedActionCatalog = state;
    const fetchOriginal = window.fetch.bind(window);
    window.fetch = async (input, init) => {
      const response = await fetchOriginal(input, init);
      const path = new URL(typeof input === "string" ? input : input instanceof URL ? input.href : input.url, location.href).pathname;
      if (path === "/v1/actions" && response.ok) {
        state.current = await response.clone().json();
        if (state.retained) return new Response(JSON.stringify(state.retained), { status: response.status, headers: response.headers });
      }
      if (path !== "/v1/changes" || !response.ok || !response.body) return response;
      let buffer = "";
      const stream = response.body.pipeThrough(new TextDecoderStream()).pipeThrough(new TransformStream<string, string>({
        transform(chunk, controller) {
          buffer += chunk;
          let end: number;
          while ((end = buffer.indexOf("\n\n")) >= 0) {
            let event = buffer.slice(0, end);
            buffer = buffer.slice(end + 2);
            if (event.startsWith("event: snapshot\n")) {
              const frame = JSON.parse(event.slice(event.indexOf("data: ") + 6));
              for (const result of frame.results ?? []) {
                if (result.path === "/v1/actions" && result.status === 200) {
                  state.current = result.body;
                  if (state.retained) result.body = state.retained;
                }
              }
              event = `event: snapshot\ndata: ${JSON.stringify(frame)}`;
            }
            controller.enqueue(event + "\n\n");
          }
        },
      })).pipeThrough(new TextEncoderStream());
      return new Response(stream, { status: response.status, headers: response.headers });
    };
  }, actions);
  return {
    release: () => page.evaluate(() => { (window as any).__retainedActionCatalog.retained = undefined; }),
    currentFamily: (schema: string) => page.evaluate(name => (window as any).__retainedActionCatalog.current?.find((action: any) => action.schema === name)?.schemaVersion.family, schema),
  };
}
