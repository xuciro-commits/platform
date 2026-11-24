#!/usr/bin/env node
import { randomUUID } from "node:crypto";
import { readFileSync } from "node:fs";
import { enterpriseRosterSteps, resolveMemberProfile } from "./demo-roster.mjs";

const host = process.env.PLATFORM_HOST ?? "http://127.0.0.1:18511";
const defaultFiles = ["./frontend-seed.json", "./crm-project.json"];
const files = process.argv.slice(2).length ? process.argv.slice(2) : defaultFiles;

async function connect(tenant) {
  const headers = { authorization: "Bearer admin", "platform-tenant": tenant, "content-type": "application/json" };
  const call = async (path, body) => {
    const response = await fetch(host + path, { method: body ? "POST" : "GET", headers, body: body ? JSON.stringify(body) : undefined, signal: AbortSignal.timeout(20000) });
    const answer = await response.json();
    if (!response.ok || answer.error) throw new Error(`${path}: ${JSON.stringify(answer.error ?? answer)}`);
    return answer;
  };
  let identity;
  for (let attempt = 0; attempt < 90 && !identity; attempt += 1) {
    identity = await call("/v1/me").catch(() => new Promise((resolve) => setTimeout(() => resolve(undefined), 1000)));
  }
  if (!identity) throw new Error(`no host answered at ${host}`);
  const session = { call, identity, catalogue: await call("/v1/actions"), declarations: await call("/v1/declarations") };
  session.refresh = async () => {
    session.catalogue = await call("/v1/actions");
    session.declarations = await call("/v1/declarations");
  };
  session.authorityFor = (type) => session.declarations.find((entry) => entry.tenantId === identity.tenantId && entry.dataClass === type)?.authorityId;
  session.send = async (schema, type, id, payload, authority, expectedRevision = 0) => {
    const version = session.catalogue.find((entry) => entry.schema === schema)?.schemaVersion;
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
    if (!response.ok || answer.error) throw new Error(`${tenant} ${schema} ${id}: ${JSON.stringify(answer.error ?? answer)}`);
    return answer.record;
  };
  session.submit = (type, id, action, payload, authority = "build", expectedRevision = 0) => session.send(`${type}.${action}`, type, id, payload, authority, expectedRevision);
  session.wordRevisions = new Map();
  session.packageRevision = 0;
  return session;
}

async function importPackage(session, path) {
  const content = JSON.parse(readFileSync(new URL(path, import.meta.url), "utf8"));
  const inspected = await session.call("/v1/packages/inspect", { content });
  const accepted = await session.submit("platform.package", inspected.descriptor.id, "import", { id: inspected.descriptor.id, digest: inspected.digest }, "platform", session.packageRevision);
  session.packageRevision = accepted.revision;
  return content;
}

async function installPackage(session, path) {
  session.packageRevision = 0;
  const descriptor = await importPackage(session, path);
  const installed = await session.submit("platform.package", descriptor.id, "install", { id: descriptor.id, version: descriptor.version }, "platform", session.packageRevision);
  session.packageRevision = installed.revision;
  return descriptor;
}

const partyPackages = ["./frontend-fixtures/geo-0.1.1.json", "./frontend-fixtures/party-0.2.1.json"];

async function members(session, sample) {
  for (const [id, entry] of Object.entries(sample.members ?? {})) {
    await session.submit("platform.profile", id, "update", resolveMemberProfile(id, entry), "platform");
  }
}

async function records(session, sample, installed) {
  let written = 0;
  for (const type of (sample.types ?? []).filter((each) => !each.draft)) {
    const full = `${sample.project.name}.${type.name}`;
    const authority = session.authorityFor(full);
    if (!authority && installed && !installed.has(full)) continue;
    if (!authority) throw new Error(`${full}: no write authority`);
    for (const [index, { id, transition, ...record }] of (type.records ?? []).entries()) {
      const target = id ?? `${type.prefix}-${String(index + 1).padStart(3, "0")}`;
      const created = await session.submit(full, target, "create", record, authority);
      if (transition) await session.submit(full, target, transition.action, transition.payload ?? {}, authority, created.revision);
      written += 1;
    }
  }
  return written;
}

async function reference(session, sample, installed) {
  for (const type of (sample.types ?? []).filter((each) => !each.draft && each.definition.records?.length)) {
    const full = `${sample.project.name}.${type.name}`;
    if (installed && !installed.has(full)) continue;
    for (let attempt = 0; ; attempt += 1) {
      const { records: rows } = await session.call(`/v1/records/${full}?limit=500`);
      if (type.definition.records.every(({ id }) => rows.some((row) => row.id === id))) break;
      if (attempt === 50) throw new Error(`${full}: reference records were not filled`);
      await new Promise((resolve) => setTimeout(resolve, 200));
    }
  }
}

async function filled(session, descriptor) {
  for (const asset of descriptor.blueprint.assets.filter((each) => each.ref.kind === "object" && each.body.records?.length)) {
    for (let attempt = 0; ; attempt += 1) {
      const { records: rows } = await session.call(`/v1/records/${asset.ref.name}?limit=500`);
      if (asset.body.records.every(({ id }) => rows.some((row) => row.id === id))) break;
      if (attempt === 100) throw new Error(`${asset.ref.name}: reference records were not filled`);
      await new Promise((resolve) => setTimeout(resolve, 200));
    }
  }
}

async function words(session, sample) {
  if (!sample.words || Object.keys(sample.words).length === 0) return;
  const authority = session.authorityFor("platform.translation");
  if (!authority) throw new Error("platform.translation: no write authority");
  for (const [language, entries] of Object.entries(sample.words)) {
    let revision = session.wordRevisions.get(language) ?? 0;
    for (const [text, translation] of Object.entries(entries)) {
      const accepted = await session.submit("platform.translation", language, "set", { text, translation }, authority, revision);
      revision = accepted.revision;
    }
    session.wordRevisions.set(language, revision);
  }
}

const main = await connect("acceptance");
const projects = await main.call("/v1/records/build.app?limit=500");
const existingProjects = new Set(projects.records.map((record) => record.name));

for (const file of files) {
  const url = file.startsWith(".") || !file.startsWith("/") ? new URL(file, import.meta.url) : new URL(`file://${file}`);
  const sample = JSON.parse(readFileSync(url, "utf8"));
  if (existingProjects.has(sample.project.name)) {
    console.log(`frontend-seed: project ${sample.project.name} is already there; nothing to do`);
    continue;
  }

  await members(main, sample);
  const revisions = new Map();
  const project = await main.submit("build.app", sample.project.name, "create", sample.project);
  revisions.set(`build.app/${sample.project.name}`, project.revision);
  existingProjects.add(sample.project.name);

  const track = (id, record) => revisions.set(`build.object/${id}`, record.revision);
  for (const type of sample.types ?? []) {
    const id = `${sample.project.name}.${type.name}`;
    const { sets, reads, actions, ...definition } = type.definition;
    const fields = definition.fields?.map(({ formulaExpr, ...field }) => (formulaExpr && !JSON.stringify(formulaExpr).includes('"agg"') ? { ...field, formulaExpr } : field));
    track(id, await main.submit("build.object", id, "create", { ...definition, fields, name: type.name, project: sample.project.name }));
    if (!type.draft) track(id, await main.submit("build.object", id, "publish", {}, "build", 1));
  }

  for (const type of (sample.types ?? []).filter((each) => !each.draft)) {
    for (const { name, ...action } of type.definition.actions ?? []) {
      const id = `${sample.project.name}.${type.name}.${name}`;
      await main.submit("build.actiontype", id, "create", { name: id, project: sample.project.name, object: `${sample.project.name}.${type.name}`, definition: { name, ...action } });
      await main.submit("build.actiontype", id, "publish", {}, "build", 1);
    }
    if (type.definition.actions?.length) {
      const id = `${sample.project.name}.${type.name}`;
      track(id, await main.submit("build.object", id, "publish", {}, "build", revisions.get(`build.object/${id}`)));
    }
  }

  for (const type of sample.types ?? []) {
    if (!type.definition.sets?.length) continue;
    const id = `${sample.project.name}.${type.name}`;
    const revision = revisions.get(`build.object/${id}`);
    track(id, await main.submit("build.object", id, "edit", { sets: type.definition.sets, reads: type.definition.reads, fields: type.definition.fields }, "build", revision));
    if (!type.draft) track(id, await main.submit("build.object", id, "publish", {}, "build", revision + 1));
  }

  await main.refresh();
  await reference(main, sample);
  const written = await records(main, sample);

  if (sample.project.name === "sales") {
    for (const path of ["./frontend-fixtures/production-provider-0.1.4.json", "../deploy/packages/production-provider.json"]) await importPackage(main, path);
  }

  const steps = sample.project.name === "sales" ? [...(sample.steps ?? []), ...enterpriseRosterSteps()] : (sample.steps ?? []);
  for (const [schema, type, id, payload] of steps) {
    let authority = main.authorityFor(type);
    if (!authority) {
      await main.refresh();
      authority = main.authorityFor(type);
    }
    if (!authority) throw new Error(`${type}: no write authority`);
    const key = `${type}/${id}`;
    let input = payload;
    if (schema === "build.branch.propose") {
      const { check } = await main.call(`/v1/branches/${encodeURIComponent(id)}/checks`, {});
      if (!check.passed) throw new Error(`branch ${id}: ${JSON.stringify(check)}`);
      input = { ...payload, checks: check };
    }
    const accepted = await main.send(schema, type, id, input, authority, revisions.get(key) ?? 0);
    revisions.set(key, accepted.revision);
  }

  await words(main, sample);
  console.log(`frontend-seed: ${(sample.types ?? []).length} types (${(sample.types ?? []).filter((each) => each.draft).length} drafts), ${written} records and ${steps.length} steps in project ${sample.project.name} on ${host}`);

  if (sample.install) {
    const tenant = await connect(sample.install.tenant);
    for (const path of sample.install.requires ?? []) await installPackage(tenant, path);
    const descriptor = await installPackage(tenant, sample.install.package);
    await tenant.refresh();
    const kinds = new Set(descriptor.blueprint.assets.filter((asset) => asset.ref.kind === "object").map((asset) => asset.ref.name));
    await members(tenant, sample.install);
    if (sample.install.copy === false) await filled(tenant, descriptor);
    const copied = sample.install.copy === false ? 0 : await records(tenant, sample, kinds);
    if (sample.install.copy !== false) {
      await reference(tenant, sample, kinds);
      await words(tenant, sample);
    }
    console.log(`frontend-seed: installed ${descriptor.id}@${descriptor.version} with ${copied} records in tenant ${sample.install.tenant}`);
  }
}

for (const name of ["blank", "upgrade", "publisher", "official"]) {
  const empty = await connect(name).catch(() => undefined);
  if (!empty) continue;
  const existing = await empty.call("/v1/members");
  for (const entry of existing) {
    if (!entry.profile?.displayName || !entry.profile?.avatar) {
      const profile = resolveMemberProfile(entry.id, entry.profile?.displayName || undefined);
      if (profile.displayName) await empty.submit("platform.profile", entry.id, "update", profile, "platform");
    }
  }
  if (name === "official") {
    for (const path of partyPackages) await installPackage(empty, path);
    console.log("frontend-seed: installed geo and party in tenant official");
  }
}
