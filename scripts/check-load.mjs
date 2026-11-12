#!/usr/bin/env node
import http from "node:http";
import https from "node:https";
import os from "node:os";
import { createGunzip } from "node:zlib";
import { mkdir, readFile, writeFile } from "node:fs/promises";
import { join, resolve } from "node:path";
import { performance } from "node:perf_hooks";

const base = new URL(process.env.PLATFORM_LOAD_BASE ?? "http://127.0.0.1:20296");
const tenant = process.env.PLATFORM_LOAD_TENANT;
const directory = resolve(process.env.PLATFORM_LOAD_OUTPUT ?? ".build/load/latest");
const users = (process.env.PLATFORM_LOAD_USERS ?? "500,1000,5000").split(",").map(Number);
const rows = Number(process.env.PLATFORM_LOAD_ROWS ?? 10000);
const duration = Number(process.env.PLATFORM_LOAD_DURATION ?? 60);
const interval = Number(process.env.PLATFORM_LOAD_INTERVAL ?? 30);
const requestTimeout = Number(process.env.PLATFORM_LOAD_TIMEOUT ?? 30000);
if (!tenant || !users.length || users.some(n => !Number.isInteger(n) || n < 1 || n > 5000) || !Number.isInteger(rows) || rows < 2 || rows > 100000 || !Number.isFinite(duration) || duration <= 0 || !Number.isFinite(interval) || interval <= 0) {
  throw Error("Set PLATFORM_LOAD_TENANT and valid users (1–5000), rows (2–100000), duration and interval");
}
await mkdir(directory, { recursive: true });
const transport = base.protocol === "https:" ? https : http;
const agent = new transport.Agent({ keepAlive: true, maxSockets: 256, maxFreeSockets: 32 });
const fixturePath = join(directory, "fixture.json");
const delay = ms => new Promise(resolve => setTimeout(resolve, ms));
const userSubject = i => `load${String(i + 1).padStart(5, "0")}`;
const rowID = i => `r${String(i).padStart(6, "0")}`;
let measured;
let administrator;
let schemas = new Map();
let sequence = 0;

function request(path, subject = "loadadmin", body, extraHeaders = {}, recordMeasurement = true) {
  const started = performance.now();
  const data = body === undefined ? undefined : JSON.stringify(body);
  return new Promise((resolve, reject) => {
    const headers = { Authorization: `Bearer ${subject}`, "Platform-Tenant": tenant, "Accept-Language": "en", "Accept-Encoding": "gzip", ...extraHeaders };
    if (data !== undefined) Object.assign(headers, { "Content-Type": "application/json", "Content-Length": Buffer.byteLength(data) });
    const req = transport.request(new URL(path, base), { method: data === undefined ? "GET" : "POST", headers, agent }, res => {
      const chunks = [], body = res.headers["content-encoding"] === "gzip" ? res.pipe(createGunzip()) : res;
      body.on("data", chunk => chunks.push(chunk));
      body.on("error", reject);
      body.on("end", () => {
        const elapsed = performance.now() - started;
        if (measured && recordMeasurement) {
          measured.http.latencies.push(elapsed);
          measured.http.statuses[res.statusCode] = (measured.http.statuses[res.statusCode] ?? 0) + 1;
        }
        const raw = Buffer.concat(chunks).toString();
        let value;
        try { value = JSON.parse(raw); } catch { reject(Error(`${path}: HTTP ${res.statusCode}, invalid JSON`)); return; }
        if (res.statusCode < 200 || res.statusCode >= 300 || value.error) {
          reject(Error(`${path}: HTTP ${res.statusCode}: ${JSON.stringify(value.error ?? value).slice(0, 400)}`));
          return;
        }
        resolve(value);
      });
    });
    req.setTimeout(requestTimeout, () => req.destroy(Error("request timeout")));
    req.on("error", error => {
      if (measured && recordMeasurement) measured.http.errors[error.code ?? error.message] = (measured.http.errors[error.code ?? error.message] ?? 0) + 1;
      reject(error);
    });
    req.end(data);
  });
}

async function submit(type, id, action, payload, revision, recordMeasurement = true, subject = "loadadmin") {
  const name = `${type}.${action}`;
  if (revision === undefined) revision = action === "create" ? 0 : (await request(`/v1/records/${type}/${encodeURIComponent(id)}`, "loadadmin", undefined, {}, recordMeasurement)).record.revision;
  const version = schemas.get(name);
  const submission = {
    tenantId: tenant, principalId: subject === "loadadmin" ? administrator.principalId : subject, authority: type.split(".")[0], target: { type, id },
    schema: { name, version: version?.family ?? 1 }, expectedRevision: revision,
    idempotencyKey: `load-${Date.now()}-${sequence++}`, payload: Buffer.from(JSON.stringify(payload)).toString("base64"),
  };
  const result = await request("/v1/submissions", subject, submission, version ? { "Platform-Schema-Version": version.version } : {}, recordMeasurement);
  if (!result.record?.changeId) throw Error(`${name}: missing accepted change`);
  return result;
}

async function seed() {
  administrator = await request("/v1/me");
  const project = "loadproject", object = "build.loaditem", draftID = "LOADOBJECT";
  await submit("build.app", project, "create", { name: project, title: "Load verification" });
  await submit("platform.project", project, "save", { title: "Load verification", members: [{ member: administrator.principalId, role: "owner" }] }, 0);
  const parts = Array.from({ length: Math.ceil(rows / 5000) }, (_, index) => ({ dataset: `LOADDATA${index}`, pipeline: `LOADPIPE${index}`, index }));
  for (const part of parts) {
    await submit("build.dataset", part.dataset, "create", { name: `loaddata${part.index}`, title: `Load data ${part.index}`, key: "id", keep: 1, project });
    await submit("build.pipeline", part.pipeline, "create", { name: `loadpipe${part.index}`, title: `Load pipeline ${part.index}`, input: part.dataset, outputObject: object, project });
  }
  await submit("build.object", draftID, "create", {
    name: "loaditem", title: "Load item", project,
    fields: [{ name: "note", title: "Note", type: "text" }, { name: "value", title: "Value", type: "integer" }, { name: "group", title: "Group", type: "text" }],
    access: [{ role: "user", read: "all", edit: true }],
    datasources: parts.map(part => ({ kind: "pipeline", source: part.pipeline, key: "id", columns: ["note", "value", "group"].map(name => ({ column: name, attribute: name })) })),
  });
  const drafts = [{ kind: "object", id: draftID }, ...parts.flatMap(part => [{ kind: "dataset", id: part.dataset }, { kind: "pipeline", id: part.pipeline }])];
  const preview = await request("/v1/releases/preview", "loadadmin", { drafts });
  if (preview.diagnostic || !preview.candidateId) throw Error(`release preview: ${preview.diagnostic ?? "missing candidate"}`);
  await request("/v1/releases/candidates", "loadadmin", { drafts, candidateId: preview.candidateId, key: "load-candidate" });
  await request("/v1/releases/active", "loadadmin", { candidateId: preview.candidateId, key: "load-activate" });
  schemas = new Map((await request("/v1/actions")).map(action => [action.schema, action.schemaVersion]));
  for (const part of parts) {
    const values = Array.from({ length: Math.min(5000, rows - part.index * 5000) }, (_, n) => {
      const i = part.index * 5000 + n;
      return { id: rowID(i), note: `Row ${i}`, value: i, group: `Group ${i % 20}` };
    });
    await submit("build.dataset", part.dataset, "load", { rows: values });
    await submit("build.pipeline", part.pipeline, "run", {});
    const deadline = performance.now() + 240000;
    for (;;) {
      const pipeline = (await request(`/v1/records/build.pipeline/${part.pipeline}`)).record;
      if (pipeline.last?.error || pipeline.last?.failed) throw Error(`pipeline failed for runner ${pipeline.runner}: ${JSON.stringify(pipeline.last)}`);
      if (pipeline.last && !pipeline.requested && !pipeline.last.execution?.pending && pipeline.last.written === values.length) break;
      if (performance.now() > deadline) throw Error(`pipeline did not settle: ${JSON.stringify(pipeline.last)}`);
      await delay(250);
    }
    process.stdout.write(`Seeded ${Math.min(rows, (part.index + 1) * 5000)}/${rows} accepted rows\n`);
  }
  const list = await request(`/v1/records/${object}?limit=25`);
  if (list.total !== rows || !list.records?.length) throw Error(`seed total ${list.total}, expected ${rows}`);
  const fixture = { tenant, object, rows, first: rowID(0), edit: rowID(1), release: preview.candidateId, seededAt: new Date().toISOString() };
  await writeFile(fixturePath, JSON.stringify(fixture, null, 2));
}

function distribution(values) {
  const sorted = [...values].sort((a, b) => a - b);
  const at = fraction => sorted.length ? Math.round(sorted[Math.min(sorted.length - 1, Math.ceil(sorted.length * fraction) - 1)] * 100) / 100 : null;
  return { count: sorted.length, p50Ms: at(0.5), p95Ms: at(0.95), p99Ms: at(0.99), maxMs: at(1) };
}

function connect(index, fixture, stage) {
  const started = performance.now();
  const query = `/v1/records/${fixture.object}?limit=25`;
  return new Promise((resolve, reject) => {
    let ready = false, ended = false, buffer = "";
    const req = transport.get(new URL(`/v1/changes?watch=${encodeURIComponent(JSON.stringify([query]))}`, base), {
      agent: false, headers: { Authorization: `Bearer ${userSubject(index)}`, "Platform-Tenant": tenant, "Accept-Language": "en", "Accept-Encoding": "gzip" },
    }, response => {
      if (response.statusCode !== 200) { req.destroy(Error(`SSE HTTP ${response.statusCode}`)); return; }
      const body = response.headers["content-encoding"] === "gzip" ? response.pipe(createGunzip()) : response;
      body.setEncoding("utf8");
      body.on("data", chunk => {
        buffer += chunk;
        for (let end; (end = buffer.indexOf("\n\n")) >= 0;) {
          const event = buffer.slice(0, end);
          buffer = buffer.slice(end + 2);
          if (!event.startsWith("event: snapshot\n")) continue;
          let frame;
          try { frame = JSON.parse(event.slice(event.indexOf("data: ") + 6)); } catch { req.destroy(Error("invalid SSE JSON")); return; }
          stage.frames++;
          for (const result of frame.results ?? []) {
            if (result.status !== 200 || result.body?.total !== fixture.rows || !result.body.records?.length) {
              req.destroy(Error(`SSE result ${result.status}, total ${result.body?.total}`)); return;
            }
            if (!ready) {
              ready = true;
              clearTimeout(timer);
              stage.handshakes.push(performance.now() - started);
              stage.ready++;
              resolve();
            }
            if (stage.probe && !stage.notifications.has(index) && result.body.records.some(record => record.note === stage.probe.note)) {
              stage.notifications.set(index, performance.now() - stage.probe.started);
            }
          }
        }
      });
      body.on("end", () => fail(Error("SSE ended")));
      body.on("error", fail);
    });
    function fail(error) {
      if (ended || stage.stopping) return;
      ended = true;
      clearTimeout(timer);
      stage.streamErrors[error.code ?? error.message] = (stage.streamErrors[error.code ?? error.message] ?? 0) + 1;
      if (!ready) reject(error);
    }
    const timer = setTimeout(() => req.destroy(Error("SSE first frame timeout")), Number(process.env.PLATFORM_LOAD_CONNECT_TIMEOUT ?? 120000));
    req.on("error", fail);
    stage.streams.push(req);
  });
}

async function runStage(count, fixture) {
  const stage = { users: count, ready: 0, streams: [], streamErrors: {}, handshakes: [], frames: 0, notifications: new Map(), http: { latencies: [], statuses: {}, errors: {} }, operations: [], failures: {}, kinds: {} };
  const connectedAt = performance.now();
  let next = 0;
  await Promise.all(Array.from({ length: Math.min(25, count) }, async () => {
    while (next < count) {
      const i = next++;
      try { await connect(i, fixture, stage); } catch {}
    }
  }));
  stage.connectionMs = performance.now() - connectedAt;
  process.stdout.write(`${count} users: ${stage.ready} SSE ready in ${(stage.connectionMs / 1000).toFixed(1)}s\n`);
  if (stage.ready !== count) {
    stage.stopping = true;
    stage.streams.forEach(stream => stream.destroy());
    return { users: count, ready: stage.ready, connectionMs: stage.connectionMs, streamErrors: stage.streamErrors, failed: true };
  }
  measured = stage;
  const started = performance.now(), pending = new Set();
  let operation = 0;
  const issue = () => {
    const n = operation++, member = n % count, kind = n % 20 < 16 ? "list" : n % 20 < 19 ? "record" : "edit";
    const begin = performance.now();
    const perform = kind === "edit"
      ? submit(fixture.object, rowID(Math.floor(n / 20) % rows), "edit", { note: `Edited ${count}-${n}` }, 1, true, userSubject(member))
      : request(kind === "list" ? `/v1/records/${fixture.object}?limit=25` : `/v1/records/${fixture.object}/${rowID(member % rows)}`, userSubject(member));
    const result = perform.then(value => {
      if (kind === "list" && (value.total !== fixture.rows || value.records.length !== 25 && fixture.rows >= 25)) throw Error("incorrect list window");
    }).catch(error => { stage.failures[error.message] = (stage.failures[error.message] ?? 0) + 1; }).finally(() => {
      const elapsed = performance.now() - begin;
      stage.operations.push(elapsed);
      (stage.kinds[kind] ??= []).push(elapsed);
      pending.delete(result);
    });
    pending.add(result);
  };
  const probeAt = started + duration * 500;
  let probe;
  const deadline = started + duration * 1000;
  while (performance.now() < deadline) {
    const now = performance.now();
    while (started + operation * interval * 1000 / count <= now) issue();
    if (!probe && now >= probeAt) {
      stage.probe = { note: `Probe ${count}-${Date.now()}`, started: performance.now() };
      probe = submit(fixture.object, fixture.first, "edit", { note: stage.probe.note }, undefined, false).then(() => { stage.probe.acceptedMs = performance.now() - stage.probe.started; }).catch(error => { stage.probe.error = error.message; });
    }
    await delay(20);
  }
  await Promise.all([...pending, probe]);
  const elapsed = performance.now() - started;
  measured = undefined;
  stage.stopping = true;
  stage.streams.forEach(stream => stream.destroy());
  const result = {
    users: count, ready: stage.ready, connectionMs: stage.connectionMs, handshake: distribution(stage.handshakes), streamErrors: stage.streamErrors,
    durationSeconds: duration, elapsedSeconds: elapsed / 1000, targetOpsPerSecond: count / interval,
    completedOpsPerSecond: stage.operations.length / (elapsed / 1000), operations: distribution(stage.operations),
    kinds: Object.fromEntries(Object.entries(stage.kinds).map(([kind, latencies]) => [kind, distribution(latencies)])), operationErrors: stage.failures,
    http: { ...distribution(stage.http.latencies), statuses: stage.http.statuses, transportErrors: stage.http.errors },
    notifications: { frames: stage.frames, received: stage.notifications.size, expected: count, probeAcceptedMs: stage.probe?.acceptedMs, probeError: stage.probe?.error, ...distribution([...stage.notifications.values()]) },
  };
  result.failed = Object.keys(stage.failures).length > 0 || Object.keys(stage.streamErrors).length > 0 || !!stage.probe?.error || stage.notifications.size !== count
    || result.kinds.list.p95Ms > 1000 || result.kinds.record.p95Ms > 1000 || result.notifications.p95Ms > 2000;
  process.stdout.write(`${count} users: ${result.completedOpsPerSecond.toFixed(1)} ops/s, p95 ${result.operations.p95Ms}ms, errors ${Object.values(stage.failures).reduce((a, b) => a + b, 0)}, update ${stage.notifications.size}/${count}\n`);
  return result;
}

try {
  if (process.argv.includes("--seed")) await seed();
  else if (process.argv.includes("--run")) {
    const fixture = JSON.parse(await readFile(fixturePath, "utf8"));
    administrator = await request("/v1/me");
    schemas = new Map((await request("/v1/actions")).map(action => [action.schema, action.schemaVersion]));
    const restored = await request(`/v1/records/${fixture.object}?limit=25`, userSubject(0));
    if (restored.total !== fixture.rows) throw Error(`restart total ${restored.total}, expected ${fixture.rows}`);
    const result = {
      at: new Date().toISOString(), base: base.origin, fixture,
      hardware: { platform: os.platform(), release: os.release(), arch: os.arch(), cpus: os.cpus().length, cpu: os.cpus()[0]?.model, totalMemoryBytes: os.totalmem(), node: process.version },
      profile: { users, configuredMembers: Math.max(...users) + 1, durationSeconds: duration, secondsPerUserOperation: interval, listPercent: 80, recordPercent: 15, editPercent: 5, listLimit: 25, requestTimeoutMs: requestTimeout, distinctMembers: true, database: "isolated PostgreSQL after restart" },
      boundaries: ["One loopback host and one tenant; client and server share this machine", "Development bearer subjects exclude external OIDC latency", "Each business edit uses a distinct member and record; admin performs seed and endpoint fixture operations", "Latency includes HTTP response parsing; no browser/render measurements", "Notification probe is an extra write, excluded from business operation and HTTP statistics", "Notification probe starts before its accepted write and ends when each client sees that value", "Endpoint baselines run after SSE connections close; member.add includes complete preparation, commit and publication", "Online users describe open SSE connections; throughput is measured separately"], stages: [],
    };
    for (const count of users) {
      result.stages.push(await runStage(count, fixture));
      await writeFile(join(directory, "results.json"), JSON.stringify(result, null, 2));
      if (result.stages.at(-1).failed) break;
      await delay(500);
    }
    result.endpoints = [];
    for (const endpoint of [{ name: "/v1/apps", concurrency: 1, requests: 100 }, { name: "/v1/apps", concurrency: 8, requests: 100 }, { name: "platform.member.add", concurrency: 1, requests: 50 }]) {
      process.stdout.write(`Starting ${endpoint.name} c=${endpoint.concurrency}, ${endpoint.requests} requests\n`);
      const stats = { http: { latencies: [], statuses: {}, errors: {} } }, errors = {};
      let next = 0;
      measured = stats;
      const started = performance.now();
      await Promise.all(Array.from({ length: endpoint.concurrency }, async () => {
        while (next < endpoint.requests) {
          const index = next++;
          try {
            if (endpoint.name === "/v1/apps") await request(endpoint.name);
            else await submit("platform.member", `bench${index}`, "add", { subject: `user:loadbench${index}@example.test` }, 0);
          } catch (error) { errors[error.message] = (errors[error.message] ?? 0) + 1; }
        }
      }));
      const elapsedSeconds = (performance.now() - started) / 1000;
      measured = undefined;
      const summary = { ...endpoint, elapsedSeconds, requestsPerSecond: endpoint.requests / elapsedSeconds, ...distribution(stats.http.latencies), statuses: stats.http.statuses, transportErrors: stats.http.errors, errors };
      result.endpoints.push(summary);
      await writeFile(join(directory, "results.json"), JSON.stringify(result, null, 2));
      process.stdout.write(`${endpoint.name} c=${endpoint.concurrency}: ${summary.requestsPerSecond.toFixed(1)} req/s, p95 ${summary.p95Ms}ms, errors ${Object.values(errors).reduce((a, b) => a + b, 0)}\n`);
    }
    if (result.stages.some(stage => stage.failed)) process.exitCode = 1;
    if (result.endpoints.some(endpoint => Object.keys(endpoint.errors).length)) process.exitCode = 1;
  } else throw Error("Use --seed or --run");
} finally { agent.destroy(); }
