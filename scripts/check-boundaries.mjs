#!/usr/bin/env node
import { execFileSync } from "node:child_process";
import { readdirSync, readFileSync } from "node:fs";

const root = new URL("..", import.meta.url).pathname;
const specPath = "docs/7-system/README.md";
const spec = readFileSync(root + specPath, "utf8");
const module = "platformserver/";

const section = (title) => {
  const start = spec.indexOf(`\n## ${title}`);
  if (start < 0) throw new Error(`${specPath}: missing section "${title}"`);
  const end = spec.indexOf("\n## ", start + 4);
  return spec.slice(start, end < 0 ? undefined : end);
};

const resolve = (name) => {
  if (name === "全部") return "*";
  if (name === "全部 services") return "services";
  if (/^(expr|kernel|platform|host|apps|cmd|services)(\/|$)/.test(name)) return name.replace(/\/\*$/, "");
  return `services/${name}`;
};

const names = (cell) =>
  cell
    .split(/[、，,]/)
    .map((part) => part.trim().replace(/`/g, ""))
    .filter((part) => part && part !== "无")
    .map(resolve);

const allowed = new Map();
for (const line of section("7.2 包结构").split("\n")) {
  const row = line.match(/^\| `([^`]+)` \|[^|]*\|([^|]*)\|$/);
  if (row) allowed.set(row[1].replace(/\/\*$/, ""), names(row[2]));
}

const gaps = new Map();
for (const line of section("现存差距").split("\n")) {
  const row = line.match(/^- 依赖差距：`([^`]+)` → (.+?)。/);
  if (row) gaps.set(row[1], new Set(names(row[2])));
}

const unitOf = (pkg) => {
  const parts = pkg.split("/");
  if (parts[0] === "services") return parts.slice(0, 2).join("/");
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
    problems.push(`${pkg}: unit "${unit}" has no row in ${specPath} §7.2`);
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
    problems.push(`${pkg} → ${target}: not allowed by ${specPath} §7.2 (${unit} may import ${deps.join(", ") || "nothing"})`);
  }
}

for (const [unit, entries] of gaps) {
  for (const entry of entries) {
    if (!usedGaps.has(`${unit} → ${entry}`)) problems.push(`${unit} → ${entry}: gap is closed; remove it from ${specPath} 现存差距`);
  }
}

const visiting = new Set();
const done = new Set();
const visit = (unit, path) => {
  if (done.has(unit)) return;
  if (visiting.has(unit)) {
    problems.push(`${specPath} §7.2 dependency cycle: ${[...path, unit].join(" → ")}`);
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
  hostLines: [lines("host"), 300, "host may not grow; delete or move code out of host to the owning service, see docs/7-system/README.md §7.2"],
  platformLines: [lines("platform"), 300, "platform may not grow; delete or move code to the owning service, see docs/7-system/README.md §7.2"],
  tenantFields: [tenantFields, 0, "new state belongs to the owning service, not the Tenant type"],
  capabilityProbes: [capabilityProbes, 0, "use the owner interface or an options struct instead of probing with a type assertion, see docs/7-system/README.md §7.7"],
  misnamedPackages: [misnamed, 0, "package name must equal its directory name, see docs/7-system/README.md §7.7"],
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
console.log(`boundaries: ${allowed.size} units match ${specPath} §7.2 (${usedGaps.size} recorded gaps)`);
console.log(`shape: host ${shape.hostLines[0]}, platform ${shape.platformLines[0]}, tenant fields ${tenantFields}, probes ${capabilityProbes}, misnamed ${misnamed}`);
