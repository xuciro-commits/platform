#!/usr/bin/env node
import { randomUUID } from "node:crypto";
import { readFileSync } from "node:fs";

const host = process.env.PLATFORM_HOST ?? "http://127.0.0.1:18511";
const headers = { authorization: "Bearer admin", "platform-tenant": "acceptance", "content-type": "application/json" };
const sample = JSON.parse(readFileSync(new URL("./frontend-seed.json", import.meta.url), "utf8"));

async function call(path, body) {
  const response = await fetch(host + path, { method: body ? "POST" : "GET", headers, body: body ? JSON.stringify(body) : undefined, signal: AbortSignal.timeout(20000) });
  const answer = await response.json();
  if (!response.ok || answer.error) throw new Error(`${path}: ${JSON.stringify(answer.error ?? answer)}`);
  return answer;
}

async function ready() {
  for (let attempt = 0; attempt < 90; attempt += 1) {
    try {
      return await call("/v1/me");
    } catch {
      await new Promise((resolve) => setTimeout(resolve, 1000));
    }
  }
  throw new Error(`no host answered at ${host}`);
}

const identity = await ready();
let catalogue = await call("/v1/actions");

async function submit(type, id, action, payload, authority = "build", expectedRevision = 0) {
  const schema = `${type}.${action}`;
  const version = catalogue.find((entry) => entry.schema === schema)?.schemaVersion;
  const response = await fetch(host + "/v1/submissions", {
    method: "POST",
    headers: { ...headers, ...(version ? { "platform-schema-version": version.version } : {}) },
    signal: AbortSignal.timeout(20000),
    body: JSON.stringify({
      tenantId: identity.tenantId,
      principalId: identity.principalId,
      authority,
      target: { type, id },
      schema: { name: schema, version: version?.family ?? 1 },
      idempotencyKey: randomUUID(),
      expectedRevision,
      payload: Buffer.from(JSON.stringify(payload)).toString("base64"),
    }),
  });
  const answer = await response.json();
  if (!response.ok || answer.error) throw new Error(`${schema} ${id}: ${JSON.stringify(answer.error ?? answer)}`);
  return answer.record;
}

const projects = await call("/v1/records/build.app?limit=500");
if (projects.records.some((record) => record.name === sample.project.name)) {
  console.log(`frontend-seed: project ${sample.project.name} is already there; nothing to do`);
  process.exit(0);
}

await submit("build.app", sample.project.name, "create", sample.project);
for (const type of sample.types) {
  const id = `OBJ-${type.name}`;
  const { sets, ...definition } = type.definition;
  await submit("build.object", id, "create", { ...definition, name: type.name, project: sample.project.name });
  if (!type.draft) await submit("build.object", id, "publish", {}, "build", 1);
}

for (const type of sample.types) {
  if (!type.definition.sets?.length) continue;
  const id = `OBJ-${type.name}`;
  const revision = type.draft ? 1 : 2;
  await submit("build.object", id, "edit", { sets: type.definition.sets }, "build", revision);
  if (!type.draft) await submit("build.object", id, "publish", {}, "build", revision + 1);
}

catalogue = await call("/v1/actions");
const declarations = await call("/v1/declarations");
let written = 0;
for (const type of sample.types.filter((each) => !each.draft)) {
  const full = `build.${type.name}`;
  const authority = declarations.find((entry) => entry.tenantId === identity.tenantId && entry.dataClass === full)?.authorityId;
  if (!authority) throw new Error(`${full}: no write authority after publishing`);
  for (const [index, record] of type.records.entries()) {
    await submit(full, `${type.prefix}-${String(index + 1).padStart(3, "0")}`, "create", record, authority);
    written += 1;
  }
}
let packageRevision = 0;
for (const path of ["./frontend-fixtures/production-provider-0.1.4.json", "../deploy/packages/production-provider.json"]) {
  const content = JSON.parse(readFileSync(new URL(path, import.meta.url), "utf8"));
  const inspected = await call("/v1/packages/inspect", { content });
  const accepted = await submit("platform.package", inspected.descriptor.id, "import", { id: inspected.descriptor.id, digest: inspected.digest }, "platform", packageRevision);
  packageRevision = accepted.revision;
}
const words = declarations.find((entry) => entry.tenantId === identity.tenantId && entry.dataClass === "platform.translation")?.authorityId;
if (!words) throw new Error("platform.translation: no write authority");
for (const [language, entries] of Object.entries(sample.words)) {
  for (const [revision, [text, translation]] of Object.entries(entries).entries()) await submit("platform.translation", language, "set", { text, translation }, words, revision);
}
console.log(`frontend-seed: ${sample.types.length} object types (${sample.types.filter((each) => each.draft).length} left as drafts) and ${written} records in project ${sample.project.name} on ${host}`);
