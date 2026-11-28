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
async function submission(actor, schema, type, id, payload, revision = 0, overrides = {}, extraHeaders = {}, query = "") {
  const [identity, actions, declarations] = await Promise.all([
    json(actor, "/v1/me"), json(actor, "/v1/actions"), json(actor, "/v1/declarations"),
  ]);
  const action = actions.find(entry => entry.schema === schema);
  assert.ok(action, "missing action " + schema);
  return request(actor, "/v1/submissions" + query, {
    method: "POST",
    headers: { "content-type": "application/json", "platform-schema-version": action.schemaVersion.version, ...extraHeaders },
    body: JSON.stringify({
      tenantId: identity.tenantId, principalId: identity.principalId,
      authority: declarations.find(entry => entry.dataClass === type)?.authorityId ?? "build",
      target: { type, id }, schema: { name: schema, version: action.schemaVersion.family },
      idempotencyKey: randomUUID(), expectedRevision: revision, payload: Buffer.from(JSON.stringify(payload)).toString("base64"),
      ...overrides,
    }),
  });
}
async function rawSubmission(actor, authority, schema, type, id, payload, overrides = {}, extraHeaders = {}, query = "") {
  const identity = await json(actor, "/v1/me");
  return request(actor, "/v1/submissions" + query, {
    method: "POST",
    headers: { "content-type": "application/json", ...extraHeaders },
    body: JSON.stringify({
      tenantId: identity.tenantId, principalId: identity.principalId,
      authority, target: { type, id }, schema: { name: schema, version: 1 },
      idempotencyKey: randomUUID(), expectedRevision: 0, payload: Buffer.from(JSON.stringify(payload)).toString("base64"),
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
async function attach(actor, type, record, file, bytes, contentType = "text/plain") {
  const response = await request(actor, "/v1/files?name=audit.txt", {
    method: "POST", headers: { "content-type": contentType }, body: bytes,
  });
  assert.equal(response.status, 200);
  const body = await response.json();
  assert.equal(body.size, Buffer.byteLength(bytes));
  await submit(actor, "files.file.attach", "files.file", file, { ...body, target: type + "/" + record });
  return body;
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
  const uploadedAlpha = await attach(a, type, id, file, alpha);
  await attach(b, type, id, file, beta);
  const uploadedPrivate = await attach(a, type, id, privateFile, alpha + "-secret");
  const crossAttach = await submission(b, "files.file.attach", "files.file", "stolen-hash", {
    hash: uploadedPrivate.hash, name: "stolen.txt", contentType: "text/plain", size: uploadedPrivate.size, target: type + "/" + id,
  });
  denied(crossAttach, [400, 403, 404]);
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
    assert.ok((download.headers.get("content-security-policy") ?? "").includes("sandbox"));
    assert.ok((download.headers.get("content-disposition") ?? "").startsWith("attachment;"));
    assert.equal(await download.text(), bytes);
  }
  assert.ok(uploadedAlpha.hash);
  await check(a, alpha, beta, 1, alpha);
  await check(b, beta, alpha, 1, beta);
  const privateRead = await request(b, "/v1/files/" + privateFile);
  denied(privateRead);
  assert.ok(!(await privateRead.text()).includes(alpha));
  await submit(a, type + ".edit", type, id, { note: alpha + " edited" }, 1);
  await check(a, alpha + " edited", beta, 2, alpha);
  await check(b, beta, alpha, 1, beta);
});

test("身份与边界：请求头、查询参数与内部负载均不可改写租户、成员、调用者、批准人与跳数", async () => {
  const adminA = seat("audit-a", "admin"), userA = seat("audit-a", "user");
  const forgedMember = await rawSubmission(
    userA, "build", "build.app.create", "build.app", key(), { name: key(), title: "Forged" },
    { principalId: "admin-1" },
    { "x-principal-id": "admin-1", "x-approved-by": "admin-1" },
    "?tenant=audit-b&member=admin-1&principalId=admin-1&approvedBy=admin-1&hops=0",
  );
  denied(forgedMember, [403]);

  const forgedDelegationHeader = await rawSubmission(
    adminA, "build", "build.app.create", "build.app", key(), { name: key(), title: "Bad Caller" },
    {}, { "platform-ai-caller": JSON.stringify({ kind: "delegation", name: "spoof" }) },
  );
  denied(forgedDelegationHeader, [400]);

  for (const [authority, schema, type, payload] of [
    ["work", "work.question.request", "work.question", { requester: "admin-1", title: "Spoof", options: ["ok", "no"] }],
    ["work", "work.approval.request", "work.approval", { requester: "admin-1" }],
    ["agent", "agent.delegation.ingest", "agent.delegation.event", { event: { id: "e1", type: "t", hops: -9 } }],
  ]) {
    const res = await rawSubmission(adminA, authority, schema, type, key(), payload);
    denied(res, [400, 403, 404]);
  }

  const simAsAdmin = await request(userA, "/v1/simulate?as=admin-1", {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({
      target: { type: "build.app", id: key() },
      schema: { name: "build.app.create", version: 1 },
      payload: Buffer.from(JSON.stringify({ name: "simx", title: "Sim" })).toString("base64"),
    }),
  });
  denied(simAsAdmin, [403]);

  const userLogs = await json(userA, "/v1/monitoring/logs?member=admin-1");
  for (const entry of userLogs.entries ?? []) {
    assert.equal(entry.member, "user-1");
  }
});

test("上传隔离、个人令牌跨租户拒绝、单会话撤销、退出全部设备与跨站写入防护", async () => {
  const a = seat("audit-a", "admin"), b = seat("audit-b", "admin"), userA = seat("audit-a", "user");

  const meRes = await request(a, "/v1/me");
  assert.equal(meRes.status, 200);
  assert.notEqual(meRes.headers.get("access-control-allow-origin"), "*");

  const emptyUp = await request(a, "/v1/files?name=empty.bin", { method: "POST", headers: { "content-type": "application/octet-stream" }, body: "" });
  assert.equal(emptyUp.status, 400);
  for (const [name, ctype, body] of [
    ["xss.png", "image/png", "<svg onload='alert(1)'/>"],
    ["xss.pdf", "application/pdf", "<html><script>alert(1)</script></html>"],
    ["evil.svg", "image/svg+xml", "<svg xmlns='http://www.w3.org/2000/svg'/>"],
  ]) {
    const bad = await request(a, "/v1/files?name=" + name, { method: "POST", headers: { "content-type": ctype }, body });
    assert.equal(bad.status, 415, name);
  }
  const pngBytes = Buffer.from("89504e470d0a1a0a0000000d49484452000000010000000108060000001f15c4890000000a49444154789c63000100000500010d0a2db40000000049454e44ae426082", "hex");
  const avatarUp = await request(a, "/v1/avatars", { method: "POST", headers: { "content-type": "image/png" }, body: pngBytes });
  assert.equal(avatarUp.status, 200);
  const avatarInfo = await avatarUp.json();
  const avatarHash = avatarInfo.avatar.replace(/^sha256:/, "");
  const ownAvatar = await request(a, "/v1/avatars/" + avatarHash);
  assert.equal(ownAvatar.status, 200);
  assert.ok((ownAvatar.headers.get("content-security-policy") ?? "").includes("sandbox"));
  const crossAvatar = await request(b, "/v1/avatars/" + avatarHash);
  denied(crossAvatar, [404]);

  const tok1Id = key(), tok2Id = key();
  await submit(a, "platform.token.issue", "platform.token", tok1Id, { label: "Audit PAT 1" });
  await submit(a, "platform.token.issue", "platform.token", tok2Id, { label: "Audit PAT 2" });
  denied(await request(b, "/v1/tokens/" + tok1Id + "/secret"), [404]);
  denied(await request(userA, "/v1/tokens/" + tok1Id + "/secret"), [404]);
  const pat1 = (await json(a, "/v1/tokens/" + tok1Id + "/secret")).secret;
  const pat2 = (await json(a, "/v1/tokens/" + tok2Id + "/secret")).secret;
  assert.ok(pat1.startsWith("pat_") && pat2.startsWith("pat_"));
  denied(await request(a, "/v1/tokens/" + tok1Id + "/secret"), [404]);

  const patActor1 = { tenant: "audit-a", subject: pat1 };
  const patActor2 = { tenant: "audit-a", subject: pat2 };
  assert.equal((await json(patActor1, "/v1/me")).principalId, "admin-1");
  assert.equal((await json(patActor2, "/v1/me")).principalId, "admin-1");
  denied(await request({ tenant: "audit-b", subject: pat1 }, "/v1/me"), [401]);
  denied(await request({ tenant: "audit-b", subject: pat1 }, "/v1/export/build.app"), [401]);

  const crossSiteWrite = await request(patActor1, "/v1/sessions/end-others", {
    method: "POST", headers: { "content-type": "application/json", "sec-fetch-site": "cross-site" }, body: "{}",
  });
  assert.equal(crossSiteWrite.status, 403);
  const evilOriginWrite = await request(patActor1, "/v1/sessions/end-others", {
    method: "POST", headers: { "content-type": "application/json", origin: "https://evil.example" }, body: "{}",
  });
  assert.equal(evilOriginWrite.status, 403);

  const sessions = await json(patActor1, "/v1/sessions");
  const pat2Session = sessions.find(s => !s.current && !s.ended && s.token === tok2Id);
  assert.ok(pat2Session, "expected active pat2 session");
  const revokeOne = await request(patActor1, "/v1/sessions/" + pat2Session.id + "/end", {
    method: "POST", headers: { "content-type": "application/json" }, body: "{}",
  });
  assert.equal(revokeOne.status, 200);
  denied(await request(patActor2, "/v1/me"), [401]);
  assert.equal((await request(patActor1, "/v1/me")).status, 200);

  const endAll = await request(patActor1, "/v1/sessions/end-all", {
    method: "POST", headers: { "content-type": "application/json" }, body: "{}",
  });
  assert.equal(endAll.status, 200);
  denied(await request(patActor1, "/v1/me"), [401]);
});
