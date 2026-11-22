#!/usr/bin/env node
import { execFileSync } from "node:child_process";
import { readdirSync } from "node:fs";
import { resolve } from "node:path";

const root = resolve(new URL("..", import.meta.url).pathname);
const specs = readdirSync(resolve(root, "frontend/e2e"))
  .filter((name) => name.endsWith(".spec.ts"))
  .map((name) => name.replace(/\.spec\.ts$/, ""))
  .sort();

const ALL = "all";
const rules = [
  [/^(docs|study|idea)\//, []],
  [/\.md$/, []],
  [/^\.(githooks|claude|grepai|codegraph)\//, []],
  [/^frontend\/e2e\/([a-z-]+)\.spec\.ts$/, (match) => [match[1]]],
  [/^frontend\/src\/apps\/delivery\//, ["builder", "marketplace"]],
  [/^frontend\/src\/apps\/admin\//, ["workspace", "marketplace"]],
  [/^frontend\/src\/apps\/(ontology|logic|apps)\//, ["builder", "records"]],
  [/^frontend\/src\/apps\/(home|ai|data|operations|host)\//, ["workspace"]],
  [/^frontend\/src\/pages\/(Package[A-Za-z]*|FormWizard|MemberRoles)\.tsx$/, ["builder", "marketplace"]],
  [/^frontend\/src\/pages\/(CalendarView|AppDashboard|DashboardPage|ImportWizard)\.tsx$/, ["crm", "workspace"]],
  [/^deploy\/packages\/crm(\.json|\/)/, ["crm", "marketplace"]],
  [/^deploy\/packages\//, ["builder"]],
];

function changedFiles(base) {
  const git = (...args) => execFileSync("git", args, { cwd: root, encoding: "utf8" }).split("\n").filter(Boolean);
  const files = base ? git("diff", "--name-only", `${base}...HEAD`) : [];
  files.push(...git("diff", "--name-only", "HEAD"), ...git("ls-files", "--others", "--exclude-standard"));
  return [...new Set(files)];
}

function affected(file) {
  for (const [pattern, target] of rules) {
    const match = file.match(pattern);
    if (!match) continue;
    const chosen = typeof target === "function" ? target(match) : target;
    if (chosen.some((spec) => !specs.includes(spec))) return ALL;
    return chosen;
  }
  return ALL;
}

const [base] = process.argv.slice(2);
const chosen = new Set();
const reasons = [];
for (const file of changedFiles(base)) {
  const target = affected(file);
  if (target === ALL) {
    reasons.push(`${file} -> all`);
    chosen.clear();
    chosen.add(ALL);
    break;
  }
  for (const spec of target) chosen.add(spec);
  if (target.length) reasons.push(`${file} -> ${target.join(", ")}`);
}

for (const reason of reasons) console.error(reason);
if (chosen.has(ALL)) console.log(ALL);
else console.log([...chosen].sort().map((spec) => `e2e/${spec}.spec.ts`).join(" "));
