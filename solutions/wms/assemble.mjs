// A reproducible Application Studio probe. All definitions and business writes
// use the existing host APIs; this file is neither an application runtime nor
// an alternative definition interpreter.
import { readFile } from "node:fs/promises";
import { createHash } from "node:crypto";

const base = process.env.PLATFORM_URL ?? "http://127.0.0.1:18505";
const builder = process.env.PLATFORM_BUILDER_TOKEN;
const operator = process.env.PLATFORM_OPERATOR_TOKEN;
const approver = process.env.PLATFORM_APPROVER_TOKEN;
if (!builder || !operator || !approver) throw new Error("Set PLATFORM_BUILDER_TOKEN, PLATFORM_OPERATOR_TOKEN and PLATFORM_APPROVER_TOKEN for the target host");
const fixture = JSON.parse(await readFile(new URL("definition.json", import.meta.url), "utf8"));
const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));
const hash = (value) => createHash("sha256").update(JSON.stringify(value)).digest("hex").slice(0, 20);
async function api(path, token = builder, body) {
  const response = await fetch(`${base}${path}`, {
    method: body === undefined ? "GET" : "POST",
    headers: { Authorization: `Bearer ${token}`, "Content-Type": "application/json", "Accept-Language": "en" },
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const text = await response.text();
  const result = text ? JSON.parse(text) : {};
  if (!response.ok || result.error) throw new Error(`${path}: ${JSON.stringify(result.error ?? result)}`);
  return result;
}
const identities = new Map();
async function submit(type, id, verb, payload = {}, token = builder, key = hash(payload)) {
  if (!identities.has(token)) identities.set(token, await api("/v1/me", token));
  const member = identities.get(token);
  return api("/v1/submissions", token, {
    tenantId: member.tenantId, principalId: member.principalId,
    authority: type.split(".")[0], idempotencyKey: `wms:${id}:${verb}:${key}`,
    schema: { name: `${type}.${verb}`, version: 1 }, target: { type, id },
    payload: Buffer.from(JSON.stringify(payload)).toString("base64"),
  });
}
async function record(type, id, token = builder) {
  const response = await fetch(`${base}/v1/records/${type}/${encodeURIComponent(id)}`, { headers: { Authorization: `Bearer ${token}` } });
  if (response.status === 404) return undefined;
  if (!response.ok) throw new Error(`Read ${type}/${id}: HTTP ${response.status}`);
  return (await response.json()).record;
}
async function ensure(type, definition, token = builder) {
  const { id, ...payload } = definition;
  const existing = await record(type, id, token);
  if (!existing) await submit(type, id, "create", payload, token);
  else if (Object.entries(payload).some(([key, value]) => JSON.stringify(existing[key]) !== JSON.stringify(value))) {
    await submit(type, id, "edit", payload, token);
  }
}
async function waitFor(type, id, predicate, token = builder, timeout = 90000) {
  const deadline = Date.now() + timeout;
  while (Date.now() < deadline) {
    const value = await record(type, id, token);
    if (predicate(value)) return value;
    await sleep(500);
  }
  throw new Error(`Timed out waiting for ${type}/${id}`);
}
async function activate(kind, id) {
  const preview = await api("/v1/releases/preview", builder, { kind, id });
  if (!preview.candidateId) throw new Error(preview.diagnostic ?? "No candidate produced");
  await api("/v1/releases/candidates", builder, { kind, id, candidateId: preview.candidateId, key: `wms:save:${preview.candidateId}` });
  await api("/v1/releases/active", builder, { candidateId: preview.candidateId, key: `wms:activate:${preview.candidateId}` });
  console.log(`Activated ${kind}: ${preview.candidateId}; ${preview.included.length} assets`);
  return preview;
}

async function configureApprover() {
  const [author, worker, supervisor] = await Promise.all([api("/v1/me", builder), api("/v1/me", operator), api("/v1/me", approver)]);
  if (new Set([author.principalId, worker.principalId, supervisor.principalId]).size !== 3) {
    throw new Error("Builder, operator and business approver must be separate members");
  }
  const role = supervisor.profile.roles.build;
  if (role && role !== "supervisor") throw new Error("Choose a business approver without a different Build role");
  if (!role) await submit("platform.member", supervisor.principalId, "grant", { app: "build", role: "supervisor" });
  console.log("Business supervisor configured through the original member role action");
}

async function assemble() {
  // Start with an independent draft application, then attach its resources.
  const application = await record("build.app", fixture.application.id);
  if (!application) await ensure("build.app", { ...fixture.application, pages: [], groups: [], resources: [] });
  console.log("WMS application created");
  for (const object of fixture.objects) {
    await ensure("build.object", object);
    await submit("build.object", object.id, "publish", {}, builder, hash(object));
    console.log(`Installed ${object.name}`);
  }
  await configureApprover();
  const source = await readFile(new URL("pallets.go", import.meta.url), "utf8");
  let code = await record("build.code", fixture.code.id);
  // This probe installs the first retained algorithm version. Re-running it
  // does not publish source edits or upgrade an existing version family.
  if (!code?.published) {
    await ensure("build.code", { ...fixture.code, source });
    await submit("build.code", fixture.code.id, "compile", {}, builder, hash({ ...fixture.code, source }));
    code = await waitFor("build.code", fixture.code.id, (value) => ["compiled", "failed"].includes(value?.state));
    if (code.state === "failed") throw new Error(code.diagnostics);
    await activate("compute", fixture.code.id);
  } else {
    const published = JSON.parse(code.versions[0]);
    if (published.name !== fixture.code.name || hash(published.input) !== hash(fixture.code.input) || hash(published.output) !== hash(fixture.code.output)) {
      throw new Error("The retained algorithm contract differs from this probe; use an empty probe host");
    }
  }
  await ensure("build.process", fixture.process);
  await submit("build.process", fixture.process.id, "publish", {}, builder, hash(fixture.process));
  for (const page of fixture.pages) {
    await ensure("build.page", page);
    await submit("build.page", page.id, "publish", {}, builder, hash(page));
  }
  await ensure("build.app", fixture.application);
  await submit("build.app", fixture.application.id, "publish", {}, builder, hash(fixture.application));
  // Explicit application membership closes published objects, pages, flow
  // and code through the original application candidate and activation.
  const preview = await activate("app", fixture.application.id);
  if (!preview.included.some((ref) => ref.kind === "flow" && ref.name === `build.${fixture.process.name}`)) {
    throw new Error("The application release did not include its native receiving flow");
  }
  // Example business data is separate from the controlled definitions.
  await ensure("build.wmsitem", { id: "WMS-ITEM-001", sku: "ITEM-001", name: "Precision fastener / 精密紧固件", unit: "pcs", unitsperpallet: 50 });
  await ensure("build.wmslocation", { id: "WMS-LOC-A01", code: "A-01", zone: "Receiving / 收货区", description: "Receiving probe destination / 收货验证库位" });
  await ensure("build.wmsreceipt", { id: "WMS-RECEIPT-001", number: "RCV-001", supplier: "Sample supplier / 示例供应商", arrival: new Date().toISOString().slice(0, 10) }, operator);
  await ensure("build.wmsreceiptline", { id: "WMS-LINE-001", receipt: "WMS-RECEIPT-001", item: "WMS-ITEM-001", expected: 125, lot: "LOT-001" }, operator);
  if (!await record("build.wmstask", "WMS-TASK-001", operator)) {
    await ensure("build.wmstask", { id: "WMS-TASK-001", name: "PUT-001", line: "WMS-LINE-001", location: "WMS-LOC-A01", quantity: 125, unitsperpallet: 50 }, operator);
  }
  console.log(`Open ${base}/#/page?app=build&kind=page&name=wmsreceipts`);
}

async function receive() {
  const initial = await record("build.wmstask", "WMS-TASK-001", operator);
  async function closeReceipt() {
    const receipt = await record("build.wmsreceipt", "WMS-RECEIPT-001", operator);
    // This single-line probe explicitly closes its receipt. The platform
    // does not yet enforce parent totals/completion across all child tasks.
    if (receipt.state === "receiving") await submit("build.wmsreceipt", receipt.id, "close", {}, operator);
  }
  if (initial?.state === "done") { await closeReceipt(); console.log("PUT-001 is already completed; receipt closed"); return; }
  const receipt = await record("build.wmsreceipt", "WMS-RECEIPT-001", operator);
  if (receipt.state === "open") await submit("build.wmsreceipt", receipt.id, "start", {}, operator);
  if (initial.state === "open") await submit("build.wmstask", initial.id, "start", {}, operator);
  const pending = await waitFor("build.wmstask", initial.id, (value) => value?.state === "pending", operator);
  if (pending.pallets !== 3) throw new Error(`Expected 3 pallets, got ${pending.pallets}`);
  const inbox = await api("/v1/inbox", approver);
  const task = inbox.find((entry) => entry.ref?.startsWith("work.approval/") && entry.title.includes(initial.id));
  if (!task) throw new Error("Warehouse supervisor received no approval task");
  await submit("work.approval", task.ref.slice("work.approval/".length), "approve", { note: "Receiving quantities and destination checked / 已核对数量与库位" }, approver);
  const done = await waitFor("build.wmstask", initial.id, (value) => value?.state === "done", operator);
  await closeReceipt();
  console.log(JSON.stringify({ operation: done.id, state: done.state, quantity: done.quantity, pallets: done.pallets, completedBy: done.completedby,
    approvedBy: (await api("/v1/me", approver)).principalId }, null, 2));
}

const command = process.argv[2] ?? "assemble";
if (command === "assemble") await assemble();
else if (command === "receive") await receive();
else throw new Error("Usage: node solutions/wms/assemble.mjs [assemble|receive]");
