#!/usr/bin/env node
import { randomUUID } from "node:crypto";
import { readFileSync } from "node:fs";
import { enterpriseRosterSteps, resolveMemberProfile } from "./demo-roster.mjs";

const host = process.env.PLATFORM_HOST ?? "http://127.0.0.1:18511";
const defaultFiles = ["./frontend-seed.json", ...(process.env.PLATFORM_LEGACY_CRM_FIXTURE === "1" ? ["./crm-project.json"] : [])];
const files = process.argv.slice(2).length ? process.argv.slice(2) : defaultFiles;

async function connect(tenant, subject = "admin") {
  const headers = { authorization: "Bearer " + subject, "platform-tenant": tenant, "content-type": "application/json" };
  const call = async (path, body) => {
    const response = await fetch(host + path, { method: body ? "POST" : "GET", headers, body: body ? JSON.stringify(body) : undefined, signal: AbortSignal.timeout(120000) });
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
      signal: AbortSignal.timeout(120000),
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

const partyPackages = ["./frontend-fixtures/geo-0.1.2.json", "./frontend-fixtures/party-0.2.2.json"];

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

async function filledDemo(session, descriptor) {
  const expected = new Map();
  for (const seed of descriptor.blueprint.seed ?? []) {
    if (seed.kind === "data") continue;
    if (!expected.has(seed.type)) expected.set(seed.type, []);
    expected.get(seed.type).push(seed.id);
  }
  const deadline = Date.now() + 120_000;
  while (expected.size) {
    for (const [type, ids] of expected) {
      const { records: rows } = await session.call(`/v1/records/${type}?limit=500`);
      const present = new Set(rows.map((row) => row.id));
      if (ids.every((id) => present.has(id))) expected.delete(type);
    }
    if (!expected.size) break;
    if (Date.now() >= deadline) throw new Error(`${session.identity.tenantId}: ${descriptor.id} demo records not filled: ${JSON.stringify([...expected])}`);
    await new Promise((resolve) => setTimeout(resolve, 200));
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

const tenantPackages = {
  crm: {
    packages: [...partyPackages, "./frontend-fixtures/crm-official-0.3.0.json"],
    grants: {},
    verifyDemo: true,
    verifyDemoAs: "manager",
    profiles: { root: "系统管理员", "admin-1": "王丹", "builder-1": "韩奥", "manager-1": "赵敏", "user-1": "田雪" },
  },
  official: { packages: partyPackages, grants: {} },
  sim: { packages: partyPackages, grants: {} },
  phone: {
    packages: [...partyPackages, "./frontend-fixtures/call-0.1.0.json", "./frontend-fixtures/phone-0.1.1.json", "./frontend-fixtures/crm-official-0.3.3.json"],
    grants: { "user-1": [["party", "user"], ["phone", "agent"], ["crm", "salesrep"]], "manager-1": [["party", "user"], ["phone", "gateway"]] },
    bind: ["call/1"],
  },
  publisher: {
    verifyDemo: true,
    packages: [...partyPackages, "./frontend-fixtures/projects-0.1.1.json", "./frontend-fixtures/assets-0.1.0.json"],
    grants: { "admin-1": [["party", "user"], ["projects", "manager"], ["assets", "custodian"]], "user-1": [["party", "user"], ["projects", "member"], ["assets", "staff"]] },
  },
};

await Promise.all(["blank", "rebac", "upgrade", "publisher", "official", "phone", "sim", "crm"].map(async (name) => {
  const empty = await connect(name).catch(() => undefined);
  if (!empty) return;
  const plan = tenantPackages[name];
  if (plan?.profiles) await members(empty, { members: plan.profiles });
  const existing = await empty.call("/v1/members");
  for (const entry of existing) {
    if (!entry.profile?.displayName || !entry.profile?.avatar) {
      const profile = resolveMemberProfile(entry.id, entry.profile?.displayName || undefined);
      if (profile.displayName) await empty.submit("platform.profile", entry.id, "update", profile, "platform");
    }
  }
  if (!plan) return;
  const installed = [];
  for (const path of plan.packages) installed.push(await installPackage(empty, path));
  await empty.refresh();
  for (const [member, roles] of Object.entries(plan.grants)) {
    let revision = 0;
    for (const [app, role] of roles) {
      const accepted = await empty.submit("platform.member", member, "grant", { app, role }, "platform", revision);
      revision = accepted.revision;
    }
  }
  for (const protocol of plan.bind ?? []) {
    const providers = (await empty.call("/v1/protocols")).find((entry) => entry.id === protocol)?.providers ?? [];
    if (providers.length !== 1) throw new Error(`${name}: ${protocol} has ${providers.length} providers, expected one`);
    await empty.submit("platform.protocol", protocol, "bind", { provider: providers[0] }, "platform");
  }
  if (plan.verifyDemo) {
    const viewer = plan.verifyDemoAs ? await connect(name, plan.verifyDemoAs) : empty;
    for (const descriptor of installed) await filledDemo(viewer, descriptor);
  }
  console.log(`frontend-seed: installed ${installed.map((descriptor) => descriptor.id).join(", ")} in tenant ${name}`);
}));
