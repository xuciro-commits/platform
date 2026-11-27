import { randomUUID } from "node:crypto";
import { expect, test, type Page } from "@playwright/test";
import { signIn, submit } from "./support";

const sessionHeaders = (page: Page) => page.evaluate(() => {
  const session = JSON.parse(sessionStorage.getItem("platform:session")!);
  return { authorization: `Bearer ${session.token}`, "platform-tenant": session.tenant };
});
const key = () => "audit" + randomUUID().replaceAll("-", "").slice(0, 12);

async function createProject(page: Page, project: string) {
  await submit(page, "build.app.create", "build.app", project, { name: project, title: project });
  const type = project + ".item";
  await submit(page, "build.object.create", "build.object", type, {
    name: "item", project, title: "Audit item", plural: "Audit items", display: "note",
    fields: [{ name: "note", title: "Note", type: "text", required: true, search: true }],
    access: [{ role: "reader", read: "all", create: false, edit: false }],
  });
  await submit(page, "build.object.publish", "build.object", type, {}, 1);
  return type;
}

async function attach(page: Page, type: string, record: string, file: string, bytes: string) {
  const headers = await sessionHeaders(page);
  const uploaded = await page.request.post("/v1/files?name=audit.txt", {
    headers: { ...headers, "content-type": "text/plain" }, data: Buffer.from(bytes),
  });
  expect(uploaded.status()).toBe(200);
  const body = await uploaded.json();
  expect(body.size).toBe(Buffer.byteLength(bytes));
  await submit(page, "files.file.attach", "files.file", file, { ...body, target: type + "/" + record });
}

test("tenant isolation: direct reads and forged submissions cannot cross workspaces", async ({ page, browser, baseURL }) => {
  await signIn(page, "admin-1 · acceptance");
  const id = key();
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

test("tenant isolation: identical business and attachment IDs remain separate in reads, writes and CSV exports", async ({ page, browser, baseURL }) => {
  await signIn(page, "admin-1 · audit-a");
  const other = await browser.newContext({ baseURL });
  try {
    const peer = await other.newPage();
    await signIn(peer, "admin-1 · audit-b");
    const project = key(), id = "SAME-ID", file = "same-file", privateFile = "private-file";
    const type = await createProject(page, project);
    expect(await createProject(peer, project)).toBe(type);
    const a = "alpha " + project, b = "beta " + project;
    await submit(page, type + ".create", type, id, { note: a });
    await submit(peer, type + ".create", type, id, { note: b });
    await attach(page, type, id, file, a);
    await attach(peer, type, id, file, b);
    await attach(page, type, id, privateFile, a);
    const record = "/v1/records/" + type + "/" + id;
    const list = "/v1/records/" + type + "?limit=50";
    const csv = "/v1/export/" + type;
    const check = async (seat: Page, own: string, foreign: string, revision: number) => {
      const headers = await sessionHeaders(seat);
      const response = await seat.request.get(record, { headers });
      expect(response.status()).toBe(200);
      const body = await response.json();
      expect(body.record.note).toBe(own);
      expect(body.record.revision).toBe(revision);
      for (const path of [list, csv]) {
        const result = await seat.request.get(path, { headers });
        expect(result.status()).toBe(200);
        const text = await result.text();
        expect(text).toContain(own);
        expect(text).not.toContain(foreign);
      }
      const post = await seat.request.post(csv, { headers, data: {} });
      expect(post.status()).toBe(200);
      expect(await post.text()).toContain(own);
      expect(await post.text()).not.toContain(foreign);
      const download = await seat.request.get("/v1/files/" + file, { headers });
      expect(download.status()).toBe(200);
      expect(await download.text()).toBe(own === a + " edited" ? a : own);
    };
    await check(page, a, b, 1);
    await check(peer, b, a, 1);
    const denied = await peer.request.get("/v1/files/" + privateFile, { headers: await sessionHeaders(peer) });
    expect([403, 404]).toContain(denied.status());
    expect(await denied.text()).not.toContain(a);
    await submit(page, type + ".edit", type, id, { note: a + " edited" }, 1);
    await check(page, a + " edited", b, 2);
    await check(peer, b, a, 1);
  } finally {
    await other.close();
  }
});

test("tenant isolation: an existing bearer and live subscription lose record, export and file access after role revocation", async ({ page, browser, baseURL }) => {
  await signIn(page, "admin-1 · audit-a");
  const project = key(), id = "PRIVATE", file = key(), note = "revocation " + project;
  const type = await createProject(page, project);
  await submit(page, type + ".create", type, id, { note });
  await attach(page, type, id, file, note);
  const memberPath = "/v1/members";
  const adminHeaders = await sessionHeaders(page);
  const member = await page.request.get(memberPath, { headers: adminHeaders });
  expect(member.status()).toBe(200);
  expect((await member.json()).some((entry: { id: string }) => entry.id === "user-1")).toBe(true);
  await submit(page, "platform.member.grant", "platform.member", "user-1", { app: project, role: "reader" });
  const other = await browser.newContext({ baseURL });
  try {
    const reader = await other.newPage();
    await signIn(reader, "user-1 · audit-a");
    await reader.goto("/ontology/objects/" + type + "/" + id);
    await expect(reader.getByRole("heading", { name: note, exact: true }).first()).toBeVisible();
    const old = await sessionHeaders(reader);
    const record = "/v1/records/" + type + "/" + id;
    const csv = "/v1/export/" + type;
    const download = "/v1/files/" + file;
    const direct = await reader.request.get(record, { headers: old });
    expect(direct.status()).toBe(200);
    expect((await direct.json()).record.note).toBe(note);
    for (const path of [csv, download]) {
      const response = await reader.request.get(path, { headers: old });
      expect(response.status()).toBe(200);
      expect(await response.text()).toContain(note);
    }
    await reader.evaluate(({ record, old }) => {
      const state = { frames: [] as { sequence: number; results: { path: string; status: number; body: unknown }[] }[], failure: "" };
      Object.assign(window, { auditLive: state });
      void (async () => {
        try {
          const response = await fetch("/v1/changes?watch=" + encodeURIComponent(JSON.stringify([record])), { headers: old });
          if (!response.ok || !response.body) throw Error("live status " + response.status);
          const stream = response.body.pipeThrough(new TextDecoderStream()).getReader();
          let buffer = "";
          for (;;) {
            const { value, done } = await stream.read();
            if (done) break;
            buffer += value;
            let split;
            while ((split = buffer.indexOf("\n\n")) >= 0) {
              const frame = buffer.slice(0, split);
              buffer = buffer.slice(split + 2);
              const line = frame.split("\n").find(line => line.startsWith("data: "));
              if (line) state.frames.push(JSON.parse(line.slice(6)));
            }
          }
        } catch (error) {
          state.failure = String(error);
        }
      })();
    }, { record, old });
    const frames = () => reader.evaluate(() => (window as unknown as { auditLive: { frames: { sequence: number; results: { path: string; status: number; body: unknown }[] }[]; failure: string } }).auditLive);
    await expect.poll(async () => (await frames()).frames.some(frame => frame.results.some(result => result.path === record && result.status === 200 && JSON.stringify(result.body).includes(note)))).toBe(true);
    const granted = await page.request.get(memberPath, { headers: adminHeaders });
    expect(granted.status()).toBe(200);
    expect((await granted.json()).find((entry: { id: string }) => entry.id === "user-1").roles[project]).toBe("reader");
    await submit(page, "platform.member.revoke", "platform.member", "user-1", { app: project, role: "reader" });
    const revoked = await page.request.get(memberPath, { headers: adminHeaders });
    expect(revoked.status()).toBe(200);
    expect((await revoked.json()).find((entry: { id: string }) => entry.id === "user-1").roles?.[project]).toBeUndefined();
    await expect(reader.getByText(note, { exact: true })).toHaveCount(0);
    for (const path of [record, download]) {
      const denied = await reader.request.get(path, { headers: old });
      expect([403, 404], path).toContain(denied.status());
      expect(await denied.text()).not.toContain(note);
    }
    for (const denied of [await reader.request.get(csv, { headers: old }), await reader.request.post(csv, { headers: old, data: {} })]) {
      expect(denied.status()).toBe(200);
      const text = await denied.text();
      expect(text).not.toContain(note);
      expect(text).not.toContain(id);
      expect(text.trim().split(/\r?\n/)).toHaveLength(1);
    }
    await expect.poll(async () => {
      const state = await frames();
      expect(state.failure).toBe("");
      const latest = state.frames.at(-1)?.results.find(result => result.path === record);
      return latest && [403, 404].includes(latest.status) && !JSON.stringify(latest.body).includes(note);
    }).toBe(true);
    await submit(page, type + ".edit", type, id, { note: note + " changed" }, 1);
    const denied = await reader.request.get(record, { headers: old });
    expect([403, 404]).toContain(denied.status());
    expect(await denied.text()).not.toContain(note);
  } finally {
    await other.close();
  }
});
