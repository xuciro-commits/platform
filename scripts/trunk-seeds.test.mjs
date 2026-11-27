import assert from "node:assert/strict";
import { readdirSync, readFileSync } from "node:fs";
import { test } from "node:test";
import { layers, seedPlan, securityAPIPlan } from "./trunk-seeds.mjs";

test("every Trunk spec belongs to exactly one of four scenarios", () => {
  const files = readdirSync(new URL("../frontend/e2e/", import.meta.url))
    .filter(name => name.endsWith(".spec.ts")).map(name => name.replace(/\.spec\.ts$/, "")).sort();
  const assigned = Object.values(layers).flat();
  assert.equal(Object.keys(layers).length, 4);
  assert.deepEqual(assigned.sort(), files);
  assert.equal(new Set(assigned).size, assigned.length);
  assert.throws(() => seedPlan(["unknown.spec.ts"]), /No scenario assigned/);
});

test("development and ordinary runtime do not create unrelated tenants", () => {
  assert.deepEqual(seedPlan([], true).tenants, ["acceptance"]);
  assert.deepEqual(seedPlan(["records.spec.ts"]).tenants, ["acceptance"]);
  const workspace = seedPlan(["workspace.spec.ts"]);
  assert.deepEqual(workspace.tenants, ["acceptance", "publisher"]);
  assert.deepEqual(workspace.seed, ["acceptance"]);
  assert.equal(workspace.cache, true);
});

test("security and real installation scenarios do not clone seeded state", () => {
  const security = seedPlan(["tenant-security.spec.ts"]);
  assert.deepEqual(security.tenants, ["audit-a"]);
  assert.deepEqual(securityAPIPlan.tenants, ["audit-a", "audit-b", "library"]);
  assert.equal(securityAPIPlan.cache, false);
  assert.deepEqual(security.seed, []);
  for (const spec of ["tenant-security", "official", "marketplace", "package-upgrade", "party-upgrade", "package-library", "package-apps", "connector", "phone"]) {
    assert.equal(seedPlan([spec + ".spec.ts"]).cache, false);
  }
  assert.equal(seedPlan([]).cache, false);
});

test("scenario tenant templates have unique stable IDs and Chinese display names", () => {
  const tenants = ["frontend-tenants.json", "frontend-security-tenants.json"]
    .flatMap(name => JSON.parse(readFileSync(new URL(name, import.meta.url), "utf8")));
  const byId = new Map(tenants.map(t => [t.id, t]));
  assert.equal(byId.size, tenants.length);
  assert.equal(new Set(tenants.map(t => t.name)).size, tenants.length);
  for (const id of new Set([...seedPlan([]).tenants, ...securityAPIPlan.tenants])) {
    assert.match(id, /^[a-z][a-z0-9-]+$/);
    assert.match(byId.get(id).name, /\p{Script=Han}/u);
  }
});

test("Trunk specs do not silently disable or focus tests", () => {
  const dir = new URL("../frontend/e2e/", import.meta.url);
  const names = new Set();
  for (const file of readdirSync(dir).filter(name => name.endsWith(".spec.ts"))) {
    const source = readFileSync(new URL(file, dir), "utf8");
    assert.doesNotMatch(source, /\btest(?:\.describe)?\.(?:skip|fixme|only)\s*\(/, file);
    for (const match of source.matchAll(/\btest\(("(?:[^"\\]|\\.)*")/g)) {
      const name = JSON.parse(match[1]);
      assert.ok(!names.has(name), "Repeated Trunk title: " + name);
      names.add(name);
    }
  }
});

test("delivery specs prepare only the tenant needed for their lifecycle", () => {
  for (const [spec, tenant] of [["marketplace", "blank"], ["package-upgrade", "upgrade"], ["party-upgrade", "party-upgrade"], ["package-library", "library"], ["package-apps", "publisher"]]) {
    assert.deepEqual(seedPlan([spec + ".spec.ts"]).tenants, [tenant]);
  }
});


test("generic builder and CRM business cases avoid unrelated package installations", () => {
  assert.equal(seedPlan(["builder.spec.ts"]).acceptanceCrm, false);
  assert.deepEqual(seedPlan(["crm.spec.ts"]).tenants, ["crm"]);
  assert.equal(seedPlan(["crm-records.spec.ts"]).acceptanceCrm, true);
  assert.equal(seedPlan(["package-extension.spec.ts"]).acceptanceCrm, true);
});
