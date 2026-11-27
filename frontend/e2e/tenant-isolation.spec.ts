import { expect, test } from "@playwright/test";
import { signIn, submit } from "./support";

test("tenant isolation: direct reads and forged submissions cannot cross workspaces", async ({ page, browser, baseURL }) => {
  await signIn(page, "admin-1 · acceptance");
  const id = "audit" + Date.now().toString(36);
  const title = "Audit isolation " + id;
  await submit(page, "build.app.create", "build.app", id, { name: id, title });
  const session = await page.evaluate(() => JSON.parse(sessionStorage.getItem("platform:session")!) as { token: string; tenant: string });
  const headers = (tenant: string) => ({ authorization: `Bearer ${session.token}`, "platform-tenant": tenant });
  const record = "/v1/records/build.app/" + id;
  const list = "/v1/records/build.app?limit=500";
  const own = await page.request.get(record, { headers: headers("acceptance") });
  expect(own.status()).toBe(200);
  const before = await own.json();
  expect(before.record).toBeTruthy();
  expect(JSON.stringify(before)).toContain(title);

  const foreign = await page.request.get(record, { headers: headers("publisher") });
  expect([403, 404]).toContain(foreign.status());
  expect(await foreign.text()).not.toContain(title);
  const foreignList = await page.request.get(list, { headers: headers("publisher") });
  expect(foreignList.status()).toBe(200);
  expect(await foreignList.text()).not.toContain(id);

  const identity = await page.request.get("/v1/me", { headers: headers("acceptance") }).then(r => r.json());
  const catalog = await page.request.get("/v1/actions", { headers: headers("acceptance") }).then(r => r.json());
  const action = catalog.find((a: { schema: string }) => a.schema === "build.app.create");
  expect(action).toBeTruthy();
  const forgedId = id + "forged";
  const forgedRecord = "/v1/records/build.app/" + forgedId;
  const forged = await page.request.post("/v1/submissions", {
    headers: { ...headers("acceptance"), "Platform-Schema-Version": action.schemaVersion.version },
    data: { tenantId: "publisher", principalId: identity.principalId, authority: "build",
      target: { type: "build.app", id: forgedId }, schema: { name: "build.app.create", version: action.schemaVersion.family },
      idempotencyKey: id + "-forged", expectedRevision: 0,
      payload: Buffer.from(JSON.stringify({ name: forgedId, title: title + " forged" })).toString("base64") },
  });
  expect([400, 401, 403, 404, 409]).toContain(forged.status());
  expect((await page.request.get(record, { headers: headers("publisher") })).status()).toBe(foreign.status());
  for (const tenant of ["acceptance", "publisher"]) {
    const refused = await page.request.get(forgedRecord, { headers: headers(tenant) });
    expect([403, 404]).toContain(refused.status());
  }

  const context = await browser.newContext({ baseURL });
  try {
    const reader = await context.newPage();
    await signIn(reader, "user-1 · acceptance");
    const readerSession = await reader.evaluate(() => JSON.parse(sessionStorage.getItem("platform:session")!) as { token: string });
    for (const path of [record, list, "/v1/me"]) {
      const denied = await reader.request.get(path, { headers: { authorization: `Bearer ${readerSession.token}`, "platform-tenant": "library" } });
      expect([401, 403, 404]).toContain(denied.status());
      expect(await denied.text()).not.toContain(title);
    }
  } finally {
    await context.close();
  }
  const unchanged = await page.request.get(record, { headers: headers("acceptance") });
  expect(unchanged.status()).toBe(200);
  expect((await unchanged.json()).record).toEqual(before.record);
});
