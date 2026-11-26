#!/usr/bin/env node
import { existsSync, readFileSync } from "node:fs";

const root = new URL("..", import.meta.url).pathname;
const dir = "capabilities/server/kernel/store/";
const frozen = 1;
const read = (file) => (existsSync(root + dir + file) ? readFileSync(root + dir + file, "utf8") : undefined);
const constant = (file, name) => {
  const match = (read(file) ?? "").match(new RegExp(`^const ${name} = (\\d+)$`, "m"));
  if (!match) throw new Error(`${dir}${file}: no "const ${name} = <n>"`);
  return Number(match[1]);
};

const problems = [];
const format = constant("postgres.go", "FormatEpoch");
const declared = constant("migration.go", "FrozenEpoch");
if (declared !== frozen) problems.push(`FrozenEpoch is ${declared}; it was frozen at ${frozen} before the first customer tenant and never moves`);
if (format < frozen) problems.push(`FormatEpoch ${format} is before the frozen epoch ${frozen}`);
for (let from = frozen; from < format; from++) {
  const step = `migration_${from}_${from + 1}`;
  const code = read(`${step}.go`);
  if (!code) problems.push(`FormatEpoch is ${format}: ${dir}${step}.go is missing (the migration from epoch ${from})`);
  else if (!new RegExp(`Migrations\\[${from}\\]\\s*=`).test(code)) problems.push(`${dir}${step}.go does not register Migrations[${from}]`);
  if (!read(`${step}_test.go`)) problems.push(`FormatEpoch is ${format}: ${dir}${step}_test.go is missing (a test that upgrades a real epoch ${from} entry)`);
}

if (problems.length) {
  for (const problem of problems) console.error(`epoch: ${problem}`);
  process.exit(1);
}
console.log(`epoch: format epoch ${format}, frozen at ${frozen}, ${format - frozen} migration(s) registered and tested`);
