import { afterEach, expect, test, vi } from "vitest";
import { EdgeClient } from "./client";

const connection = { server: "https://host.invalid", token: "token", tenant: "hotel-next", principal: "desk" };
function setup() {
  const saved = new Map<string, string>();
  const storage = { getItem: vi.fn((key: string) => saved.get(key) ?? null), setItem: vi.fn((key: string, value: string) => { saved.set(key, value); }) };
  vi.stubGlobal("localStorage", storage);
  vi.stubGlobal("navigator", { onLine: true });
  return { saved, storage };
}
function declare(client: EdgeClient) {
  client.authorities.refresh([{ tenantId: client.connection.tenant, dataClass: "build.reservation", authorityId: "build", kind: "AUTHORITY_KIND_TENANT_SERVER", epoch: 1 }]);
  return client;
}
function draft(client: EdgeClient) { return client.draft("build.reservation.confirm", { type: "build.reservation", id: "one" }, { guest: "Guest" }, [], 4, { family: 1, version: "frozen-version" }); }
afterEach(() => vi.unstubAllGlobals());

test("discovery does not open an anonymous outbox; actual member, tenant and host scopes stay separate", async () => {
  const { storage } = setup();
  const discovery = new EdgeClient({ ...connection, tenant: "", principal: "" });
  expect(storage.getItem).not.toHaveBeenCalled();
  expect(() => draft(discovery)).toThrow("identity is not ready");
  discovery.bindIdentity(connection.tenant, connection.principal); declare(discovery); const key = draft(discovery);
  for (const other of [{ principal: "manager" }, { tenant: "manufacturing-next" }, { server: "https://other.invalid" }]) {
    const client = declare(new EdgeClient({ ...connection, ...other }));
    expect(client.authorities.outbox).toHaveLength(0);
    const fetch = vi.fn(); vi.stubGlobal("fetch", fetch); await client.send(); expect(fetch).not.toHaveBeenCalled();
  }
  expect(new EdgeClient(connection).authorities.outbox[0]?.submission.idempotencyKey).toBe(key);
});

test("unreadable or mismatched saved requests block mutation and are never overwritten", async () => {
  const { saved, storage } = setup();
  saved.set(`outbox:${connection.server}:${connection.tenant}:${connection.principal}`, "broken JSON");
  const client = declare(new EdgeClient(connection));
  expect(() => draft(client)).toThrow("could not be opened");
  await expect(client.send()).rejects.toThrow("could not be opened");
  expect(storage.setItem).not.toHaveBeenCalled();
  storage.getItem.mockImplementation(() => { throw Error("storage denied"); });
  expect(() => draft(declare(new EdgeClient(connection)))).toThrow("could not be opened");
  expect(storage.setItem).not.toHaveBeenCalled();
});

test("failed persistence neither reports a draft saved nor starts transport", async () => {
  const { storage } = setup(); const client = declare(new EdgeClient(connection));
  storage.setItem.mockImplementation(() => { throw Error("quota"); });
  expect(() => draft(client)).toThrow("storage is unavailable");
  expect(client.authorities.outbox).toHaveLength(0);
  const fetch = vi.fn(); vi.stubGlobal("fetch", fetch);
  await expect(client.send()).rejects.toThrow("storage is unavailable"); expect(fetch).not.toHaveBeenCalled();
});

test("a crash in flight reopens unknown and checks the exact original request only once at the authority", async () => {
  setup(); const client = declare(new EdgeClient(connection)), key = draft(client);
  let started!: () => void; const observed = new Promise<void>(resolve => { started = resolve; });
  let writes = 0; const bodies: string[] = [], accepted = new Set<string>();
  vi.stubGlobal("fetch", async (_url: string, options: RequestInit) => {
    bodies.push(String(options.body));
    const original = JSON.parse(String(options.body)).idempotencyKey;
    if (!accepted.has(original)) { accepted.add(original); writes++; }
    started();
    return new Promise<Response>((_, reject) => options.signal?.addEventListener("abort", () => reject(Error("process stopped")), { once: true }));
  });
  const inFlight = client.send(); await observed; client.close(); await inFlight;
  const recovered = declare(new EdgeClient(connection));
  expect(recovered.authorities.outbox[0]?.state).toBe("SUBMISSION_STATE_UNKNOWN");
  vi.stubGlobal("fetch", async (_url: string, options: RequestInit) => {
    bodies.push(String(options.body)); const original = JSON.parse(String(options.body)).idempotencyKey;
    if (!accepted.has(original)) { accepted.add(original); writes++; }
    return new Response(JSON.stringify({ record: { changeId: "accepted-once" } }));
  });
  await recovered.send();
  expect(bodies[1]).toBe(bodies[0]); expect(writes).toBe(1);
  expect(recovered.authorities.outbox[0]).toMatchObject({ state: "SUBMISSION_STATE_CONFIRMED", submission: { idempotencyKey: key } });
  expect(new EdgeClient(connection).authorities.outbox[0]?.state).toBe("SUBMISSION_STATE_CONFIRMED");
});

test.each(["ERROR_CODE_POLICY_DENIED", "ERROR_CODE_UNKNOWN_SCHEMA"])("reconnection keeps frozen inputs and accepts the current authority's %s refusal", async code => {
  setup(); const client = declare(new EdgeClient(connection)); draft(client);
  const original = JSON.stringify(client.authorities.outbox[0]?.submission);
  const recovered = declare(new EdgeClient(connection)); let headers: Headers | undefined;
  vi.stubGlobal("fetch", async (_url: string, options: RequestInit) => {
    headers = new Headers(options.headers); expect(String(options.body)).toBe(original);
    return new Response(JSON.stringify({ error: { code, message: "Current declaration refuses this saved request" } }), { status: 403 });
  });
  await recovered.send();
  expect(headers?.get("Platform-Schema-Version")).toBe("frozen-version");
  expect(recovered.authorities.outbox[0]).toMatchObject({ state: "SUBMISSION_STATE_REJECTED", outcome: code });
  expect(JSON.stringify(recovered.authorities.outbox[0]?.submission)).toBe(original);
});

test("switching identity stops the old sender before its remaining requests leave", async () => {
  setup(); const client = declare(new EdgeClient(connection)); draft(client); draft(client);
  let started!: () => void; const observed = new Promise<void>(resolve => { started = resolve; });
  const fetch = vi.fn(async (_url: string, options: RequestInit) => { started(); return new Promise<Response>((_, reject) => options.signal?.addEventListener("abort", () => reject(Error("closed")), { once: true })); });
  vi.stubGlobal("fetch", fetch); const sending = client.send(); await observed; client.close(); await sending;
  await expect(client.send()).rejects.toThrow("identity changed"); expect(fetch).toHaveBeenCalledTimes(1);
  const next = new EdgeClient({ ...connection, principal: "manager" }); expect(next.authorities.outbox).toHaveLength(0);
  expect(new EdgeClient(connection).authorities.outbox.map(entry => entry.state)).toEqual(["SUBMISSION_STATE_UNKNOWN", "SUBMISSION_STATE_PENDING"]);
});


test("an answer that cannot be persisted remains recoverable under its original key", async () => {
  const { storage } = setup(); const client = declare(new EdgeClient(connection)), key = draft(client);
  vi.stubGlobal("fetch", async () => {
    storage.setItem.mockImplementationOnce(() => { throw Error("disk full after answer"); });
    return new Response(JSON.stringify({ record: { changeId: "accepted" } }));
  });
  await expect(client.send()).rejects.toThrow("storage is unavailable");
  const recovered = new EdgeClient(connection);
  expect(recovered.authorities.outbox[0]).toMatchObject({ state: "SUBMISSION_STATE_UNKNOWN", submission: { idempotencyKey: key } });
});

test("recovery and a concurrent decision share a serial sender without dropping the new request", async () => {
  setup();const client=declare(new EdgeClient(connection)),original=draft(client),bodies:string[]=[];
  let started!:()=>void,release!:()=>void;
  const observed=new Promise<void>(resolve=>{started=resolve;}),held=new Promise<void>(resolve=>{release=resolve;});
  vi.stubGlobal("fetch",async(_url:string,options:RequestInit)=>{
    bodies.push(String(options.body));
    if(bodies.length===1){started();await held;}
    return new Response(JSON.stringify({record:{changeId:`accepted-${bodies.length}`}}));
  });
  const recovery=client.send();await observed;
  const next=draft(client),decision=client.send();
  release();await Promise.all([recovery,decision]);
  expect(bodies.map(body=>JSON.parse(body).idempotencyKey)).toEqual([original,next]);
  expect(client.authorities.outbox.map(entry=>entry.state)).toEqual(["SUBMISSION_STATE_CONFIRMED","SUBMISSION_STATE_CONFIRMED"]);
  await client.send();expect(bodies).toHaveLength(2);
});
