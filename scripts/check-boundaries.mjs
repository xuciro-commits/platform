#!/usr/bin/env node
import { execFileSync } from "node:child_process";
import { readdirSync, readFileSync } from "node:fs";

const root = new URL("..", import.meta.url).pathname;
const tablePath = "capabilities/server/boundaries.json";
const table = JSON.parse(readFileSync(root + tablePath, "utf8"));
const module = "platformserver/";

const allowed = new Map(Object.entries(table.imports));
const gaps = new Map(Object.entries(table.exceptions).map(([unit, targets]) => [unit, new Set(targets)]));

const unitOf = (pkg) => {
  if (pkg === "services/actions/catalog" || pkg === "services/devops/descriptor" || pkg === "services/resources/publication") return pkg;
  const parts = pkg.split("/");
  if (parts[0] === "services" || (parts[0] === "kernel" && parts[1] === "protocol")) return parts.slice(0, 2).join("/");
  return parts[0];
};

const covers = (prefix, pkg) => prefix === "*" || pkg === prefix || pkg.startsWith(prefix + "/");

const output = execFileSync("go", ["list", "-f", "{{.ImportPath}} {{.Name}}{{range .Imports}} {{.}}{{end}}", "./..."], {
  cwd: root + "capabilities/server",
  encoding: "utf8",
  maxBuffer: 64 << 20,
});

const problems = [];
const usedGaps = new Set();
let misnamed = 0;
for (const line of output.trim().split("\n")) {
  const [importer, name, ...imports] = line.split(" ");
  if (name !== "main" && name !== importer.split("/").pop()) misnamed++;
  const pkg = importer.slice(module.length);
  const unit = unitOf(pkg);
  const deps = allowed.get(unit);
  if (!deps) {
    problems.push(`${pkg}: unit "${unit}" has no entry under imports in ${tablePath}; add it there and describe the package in docs/backend.md`);
    continue;
  }
  for (const imported of imports) {
    if (!imported.startsWith(module)) continue;
    const target = imported.slice(module.length);
    if (unitOf(target) === unit || deps.some((dep) => covers(dep, target))) continue;
    const gap = [...(gaps.get(unit) ?? [])].find((entry) => covers(entry, target));
    if (gap) {
      usedGaps.add(`${unit} → ${gap}`);
      continue;
    }
    problems.push(`${pkg} → ${target}: not allowed by ${tablePath} (${unit} may import ${deps.join(", ") || "nothing"}); call the other service through its api.go interface, see docs/backend.md`);
  }
}

for (const [unit, entries] of gaps) {
  for (const entry of entries) {
    if (!usedGaps.has(`${unit} → ${entry}`)) problems.push(`${unit} → ${entry}: this exception is no longer used; remove it from exceptions in ${tablePath}`);
  }
}

const visiting = new Set();
const done = new Set();
const visit = (unit, path) => {
  if (done.has(unit)) return;
  if (visiting.has(unit)) {
    problems.push(`${tablePath} dependency cycle: ${[...path, unit].join(" → ")}`);
    return;
  }
  visiting.add(unit);
  for (const dep of allowed.get(unit) ?? []) {
    for (const other of allowed.keys()) if (other !== unit && covers(dep, other)) visit(other, [...path, unit]);
  }
  visiting.delete(unit);
  done.add(unit);
};
for (const unit of allowed.keys()) visit(unit, []);

const server = root + "capabilities/server/";
const goFiles = (dirs) =>
  dirs.flatMap((dir) =>
    readdirSync(server + dir, { recursive: true })
      .filter((file) => file.endsWith(".go") && !file.endsWith("_test.go"))
      .map((file) => readFileSync(`${server}${dir}/${file}`, "utf8")),
  );
const lines = (dir) => goFiles([dir]).reduce((sum, text) => sum + text.split("\n").length - 1, 0);

const hostSource = readFileSync(server + "host/host.go", "utf8").split("\n");
const tenantStart = hostSource.indexOf("type Tenant struct {");
let tenantFields = 0;
if (tenantStart < 0) problems.push("shape: type Tenant struct moved out of host/host.go; update its location in scripts/check-boundaries.mjs");
else {
  const tenantEnd = hostSource.indexOf("}", tenantStart);
  tenantFields = hostSource.slice(tenantStart + 1, tenantEnd).filter((line) => line.trim()).length;
}

const probe = /,\s*ok\s*:?=\s*[^=\n]*\.\((?!(?:type|string|bool|float64|int\d*|any|error)\)|map\[|\[\])[^)]*\)/;
const capabilityProbes = goFiles(["."])
  .flatMap((text) => text.split("\n"))
  .filter((line) => probe.test(line)).length;

const shape = {
  hostLines: [lines("host"), 300, "host may not grow; put new code in the owning service, see docs/backend.md"],
  platformLines: [lines("platform"), 300, "platform may not grow; put new code in the owning service, see docs/backend.md"],
  tenantFields: [tenantFields, 0, "new state belongs to the owning service, not the Tenant type"],
  capabilityProbes: [capabilityProbes, 0, "use the owner interface or an options struct instead of probing with a type assertion, see docs/backend.md"],
  misnamedPackages: [misnamed, 0, "package name must equal its directory name, see docs/backend.md"],
};
const baselinePath = "scripts/shape-baseline.json";
const baseline = JSON.parse(readFileSync(root + baselinePath, "utf8"));
for (const key of Object.keys(baseline)) if (!(key in shape)) problems.push(`shape: unknown key ${key} in ${baselinePath}`);
for (const [key, [value, slack, fix]] of Object.entries(shape)) {
  if (!(key in baseline)) problems.push(`shape: ${key} missing from ${baselinePath}`);
  else if (value > baseline[key]) problems.push(`shape: ${key} grew to ${value} (baseline ${baseline[key]}): ${fix}`);
  else if (value < baseline[key] - slack) problems.push(`shape: ${key} is ${value}, baseline ${baseline[key]}: lower it in ${baselinePath} to ${value}`);
}

if (problems.length) {
  console.error(problems.join("\n"));
  process.exit(1);
}
console.log(`boundaries: ${allowed.size} units match ${tablePath} (${usedGaps.size} exceptions in use)`);
console.log(`shape: host ${shape.hostLines[0]}, platform ${shape.platformLines[0]}, tenant fields ${tenantFields}, probes ${capabilityProbes}, misnamed ${misnamed}`);
