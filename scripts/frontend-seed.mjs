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

async function submit(type, id, action, payload, authority = "build") {
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
      expectedRevision: 0,
      payload: Buffer.from(JSON.stringify(payload)).toString("base64"),
    }),
  });
  const answer = await response.json();
  if (!response.ok || answer.error) throw new Error(`${schema} ${id}: ${JSON.stringify(answer.error ?? answer)}`);
}

async function publish(id) {
  const drafts = [{ kind: "object", id }];
  const preview = await call("/v1/releases/preview", { drafts });
  if (preview.diagnostic) throw new Error(`preview ${id}: ${JSON.stringify(preview.diagnostic)}`);
  await call("/v1/releases/candidates", { drafts, candidateId: preview.candidateId, key: randomUUID() });
  await call("/v1/releases/active", { candidateId: preview.candidateId, key: randomUUID() });
}

const projects = await call("/v1/records/build.app?limit=500");
if (projects.records.some((record) => record.name === sample.project.name)) {
  console.log(`frontend-seed: project ${sample.project.name} is already there; nothing to do`);
  process.exit(0);
}

await submit("build.app", sample.project.name, "create", sample.project);
for (const type of sample.types) {
  const id = `OBJ-${type.name}`;
  await submit("build.object", id, "create", { ...type.definition, name: type.name, project: sample.project.name });
  await publish(id);
}

catalogue = await call("/v1/actions");
const declarations = await call("/v1/declarations");
let written = 0;
for (const type of sample.types) {
  const full = `build.${type.name}`;
  const authority = declarations.find((entry) => entry.tenantId === identity.tenantId && entry.dataClass === full)?.authorityId;
  if (!authority) throw new Error(`${full}: no write authority after publishing`);
  for (const [index, record] of type.records.entries()) {
    await submit(full, `${type.prefix}-${String(index + 1).padStart(3, "0")}`, "create", record, authority);
    written += 1;
  }
}
console.log(`frontend-seed: ${sample.types.length} object types and ${written} records in project ${sample.project.name} on ${host}`);
