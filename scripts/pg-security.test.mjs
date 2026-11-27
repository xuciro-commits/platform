import assert from "node:assert/strict";
import { randomUUID } from "node:crypto";
import { test as nodeTest } from "node:test";
import { measure } from "./timing.mjs";

const test = (purpose, run) => nodeTest(purpose, () => measure(purpose, run));

const host = process.env.PLATFORM_SECURITY_TEST_HOST;
if (!/^http:\/\/127\.0\.0\.1:\d+$/.test(host ?? "")) throw Error("Use make test-security-pg");
const key = () => "audit" + randomUUID().replaceAll("-", "").slice(0, 12);
const seat = (tenant, subject = "admin") => ({ tenant, subject });
const headers = ({ tenant, subject }) => ({ authorization: "Bearer " + subject, "platform-tenant": tenant });
const request = (actor, path, init = {}) => fetch(host + path, {
  ...init, headers: { ...headers(actor), ...init.headers }, signal: AbortSignal.timeout(30_000),
});
async function json(actor, path) {
  const response = await request(actor, path);
  assert.equal(response.status, 200, path);
  return response.json();
}
async function submission(actor, schema, type, id, payload, revision = 0, overrides = {}) {
  const [identity, actions, declarations] = await Promise.all([
    json(actor, "/v1/me"), json(actor, "/v1/actions"), json(actor, "/v1/declarations"),
  ]);
  const action = actions.find(entry => entry.schema === schema);
  assert.ok(action, "missing action " + schema);
  return request(actor, "/v1/submissions", {
    method: "POST",
    headers: { "content-type": "application/json", "platform-schema-version": action.schemaVersion.version },
    body: JSON.stringify({
      tenantId: identity.tenantId, principalId: identity.principalId,
      authority: declarations.find(entry => entry.dataClass === type)?.authorityId ?? "build",
      target: { type, id }, schema: { name: schema, version: action.schemaVersion.family },
      idempotencyKey: randomUUID(), expectedRevision: revision, payload: Buffer.from(JSON.stringify(payload)).toString("base64"),
      ...overrides,
    }),
  });
}
async function submit(actor, ...args) {
  const response = await submission(actor, ...args);
  const answer = await response.json();
  assert.ok(response.ok && !answer.error, JSON.stringify(answer));
  return answer;
}
async function createProject(actor, project) {
  await submit(actor, "build.app.create", "build.app", project, { name: project, title: project });
  const type = project + ".item";
  await submit(actor, "build.object.create", "build.object", type, {
    name: "item", project, title: "Audit item", plural: "Audit items", display: "note",
    fields: [{ name: "note", title: "Note", type: "text", required: true, search: true }],
    access: [{ role: "reader", read: "all", create: false, edit: false }],
  });
  await submit(actor, "build.object.publish", "build.object", type, {}, 1);
  return type;
}
async function attach(actor, type, record, file, bytes) {
  const response = await request(actor, "/v1/files?name=audit.txt", {
    method: "POST", headers: { "content-type": "text/plain" }, body: bytes,
  });
  assert.equal(response.status, 200);
  const body = await response.json();
  assert.equal(body.size, Buffer.byteLength(bytes));
  await submit(actor, "files.file.attach", "files.file", file, { ...body, target: type + "/" + record });
}
const denied = (response, allowed = [403, 404]) => assert.ok(allowed.includes(response.status), "unexpected status " + response.status);

test("租户隔离：直接读取及伪造提交不能跨工作区", async () => {
  const a = seat("audit-a"), b = seat("audit-b"), outsider = seat("library", "user");
  const id = key(), title = "Audit isolation " + id;
  await submit(a, "build.app.create", "build.app", id, { name: id, title });
  const record = "/v1/records/build.app/" + id, list = "/v1/records/build.app?limit=500";
  const before = await json(a, record);
  assert.ok(before.record);
  assert.ok(JSON.stringify(before).includes(title));

  const foreign = await request(b, record);
  denied(foreign);
  assert.ok(!(await foreign.text()).includes(title));
  assert.ok(!JSON.stringify(await json(b, list)).includes(id));

  const forgedId = id + "forged";
  const forged = await submission(a, "build.app.create", "build.app", forgedId, { name: forgedId, title: title + " forged" }, 0, { tenantId: "audit-b" });
  denied(forged, [400, 401, 403, 404, 409]);
  assert.equal((await request(b, record)).status, foreign.status);
  for (const actor of [a, b]) denied(await request(actor, "/v1/records/build.app/" + forgedId));
  assert.equal((await json(seat("audit-a", "user"), "/v1/me")).principalId, "user-1");
  for (const path of [record, list, "/v1/me"]) {
    const response = await request(outsider, path);
    denied(response, [401, 403, 404]);
    assert.ok(!(await response.text()).includes(title));
  }
  assert.deepEqual((await json(a, record)).record, before.record);
});

test("租户隔离：相同业务与附件 ID 的读取、写入和 CSV 导出互不串扰", async () => {
  const a = seat("audit-a"), b = seat("audit-b");
  const project = key(), id = "SAME-ID", file = "same-file", privateFile = "private-file";
  const type = await createProject(a, project);
  assert.equal(await createProject(b, project), type);
  const alpha = "alpha " + project, beta = "beta " + project;
  await submit(a, type + ".create", type, id, { note: alpha });
  await submit(b, type + ".create", type, id, { note: beta });
  await attach(a, type, id, file, alpha);
  await attach(b, type, id, file, beta);
  await attach(a, type, id, privateFile, alpha);
  const record = "/v1/records/" + type + "/" + id, list = "/v1/records/" + type + "?limit=50", csv = "/v1/export/" + type;
  async function check(actor, own, foreign, revision, bytes) {
    const body = await json(actor, record);
    assert.equal(body.record.note, own);
    assert.equal(body.record.revision, revision);
    for (const path of [list, csv]) {
      const response = await request(actor, path);
      assert.equal(response.status, 200);
      const text = await response.text();
      assert.ok(text.includes(own));
      assert.ok(!text.includes(foreign));
    }
    const post = await request(actor, csv, { method: "POST", headers: { "content-type": "application/json" }, body: "{}" });
    assert.equal(post.status, 200);
    const exported = await post.text();
    assert.ok(exported.includes(own));
    assert.ok(!exported.includes(foreign));
    const download = await request(actor, "/v1/files/" + file);
    assert.equal(download.status, 200);
    assert.equal(await download.text(), bytes);
  }
  await check(a, alpha, beta, 1, alpha);
  await check(b, beta, alpha, 1, beta);
  const privateRead = await request(b, "/v1/files/" + privateFile);
  denied(privateRead);
  assert.ok(!(await privateRead.text()).includes(alpha));
  await submit(a, type + ".edit", type, id, { note: alpha + " edited" }, 1);
  await check(a, alpha + " edited", beta, 2, alpha);
  await check(b, beta, alpha, 1, beta);
});
