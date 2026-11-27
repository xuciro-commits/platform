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


async function openScopeProbe(page: Page) {
  await page.route("**/read-scope-probe.html", route => route.fulfill({ contentType: "text/html", body: "<!doctype html><html><body></body></html>" }));
  await page.goto("/read-scope-probe.html");
  await page.evaluate(async () => {
    const refreshPath = "/@react-refresh";
    const refresh = (await import(refreshPath)).default;
    refresh.injectIntoGlobalHook(window);
    Object.assign(window, { $RefreshReg$: () => {}, $RefreshSig$: () => (type: unknown) => type, __vite_plugin_react_preamble_installed__: true });
  });
}

test("tenant isolation: read hooks never display a previous session or path while loading", async ({ page }) => {
  await page.route("**/v1/read-scope-probe/**", async route => {
    const tenant = route.request().headers()["platform-tenant"];
    const path = new URL(route.request().url()).pathname.split("/").at(-1);
    await route.fulfill({ json: { value: tenant + ":" + path } });
  });
  await openScopeProbe(page);
  await page.evaluate(async () => {
    const modulePath = "/e2e/read-scope-probe.tsx";
    const { mountReadScopeProbe } = await import(modulePath);
    Object.assign(window, { readScopeProbe: mountReadScopeProbe() });
  });
  const invoke = (method: "latest" | "switchTenant" | "changePath") => page.evaluate(name =>
    (window as unknown as { readScopeProbe: Record<string, () => unknown> }).readScopeProbe[name](), method);
  await expect.poll(() => invoke("latest")).toEqual({ single: "audit-a:one", many: "audit-a:one" });
  const switched = await invoke("switchTenant");
  expect(switched).not.toEqual([]);
  expect(JSON.stringify(switched)).not.toContain("audit-a:one");
  await expect.poll(() => invoke("latest")).toEqual({ single: "audit-b:one", many: "audit-b:one" });
  const changed = await invoke("changePath");
  expect(changed).not.toEqual([]);
  expect(JSON.stringify(changed)).not.toContain("audit-b:one");
  await expect.poll(() => invoke("latest")).toEqual({ single: "audit-b:two", many: "audit-b:two" });
});


test("tenant isolation: late authentication and writes cannot act on a new session", async ({ page }) => {
  await openScopeProbe(page);
  const result = await page.evaluate(async () => {
    const modulePath = "/e2e/read-scope-probe.tsx";
    const { probeSessionRequests } = await import(modulePath);
    return probeSessionRequests();
  });
  expect(result).toEqual({ afterOld401: "audit-b", write: "refused", submitted: [], current401Ended: true });
});


test("tenant isolation: a refused application catalog cannot resurrect cached record routes", async ({ page }) => {
  let refused = false;
  await page.route("**/v1/applications", route => refused
    ? route.fulfill({ status: 403, json: { error: "refused" } })
    : route.fulfill({ json: [{ name: "private-app", menu: [{ entries: [{ object: "probe.object", name: "records" }] }] }] }));
  await openScopeProbe(page);
  await page.evaluate(async () => {
    const modulePath = "/e2e/read-scope-probe.tsx";
    const { mountRouteScopeProbe } = await import(modulePath);
    Object.assign(window, { routeScopeProbe: mountRouteScopeProbe() });
  });
  const invoke = (method: "latest" | "refresh") => page.evaluate(name =>
    (window as unknown as { routeScopeProbe: Record<string, () => unknown> }).routeScopeProbe[name](), method);
  await expect.poll(() => invoke("latest")).toEqual({ status: "ready", href: "/private-app/records/one" });
  refused = true;
  await invoke("refresh");
  await expect.poll(() => invoke("latest")).toEqual({ status: "refused", href: "/ontology/objects/probe.object/one" });
});


test("tenant isolation: formatted person names cannot survive a session switch or directory refusal", async ({ page }) => {
  let refused = false;
  await page.route("**/v1/member-directory", route => {
    const tenant = route.request().headers()["platform-tenant"];
    return refused ? route.fulfill({ status: 403, json: { error: "refused" } })
      : route.fulfill({ json: [{ id: "same-id", name: tenant === "audit-a" ? "Private Alpha" : "Private Beta" }] });
  });
  await page.route("**/v1/enterprise", route => route.fulfill({ json: { elements: [], relationships: [] } }));
  await openScopeProbe(page);
  await page.evaluate(async () => {
    const modulePath = "/e2e/read-scope-probe.tsx";
    const { mountPersonScopeProbe } = await import(modulePath);
    Object.assign(window, { personScopeProbe: mountPersonScopeProbe() });
  });
  const invoke = (method: "latest" | "refresh" | "switchTenant") => page.evaluate(name =>
    (window as unknown as { personScopeProbe: Record<string, () => unknown> }).personScopeProbe[name](), method);
  await expect.poll(() => invoke("latest")).toEqual({ status: "ready", name: "Private Alpha" });
  expect(await invoke("switchTenant")).toEqual({ status: "loading", name: "Unnamed member" });
  await expect.poll(() => invoke("latest")).toEqual({ status: "ready", name: "Private Beta" });
  refused = true;
  await invoke("refresh");
  await expect.poll(() => invoke("latest")).toEqual({ status: "refused", name: "Unnamed member" });
});
