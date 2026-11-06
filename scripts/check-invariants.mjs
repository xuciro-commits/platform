#!/usr/bin/env node
import { execFile, execFileSync } from "node:child_process";
import { existsSync, mkdirSync, readFileSync, writeFileSync } from "node:fs";
import { availableParallelism } from "node:os";

const root = new URL("..", import.meta.url).pathname;
const pkg = root + "capabilities/server/host";
const binary = root + ".build/check/host.test";
const timings = root + ".build/check/invariant-timings.json";
const started = Date.now();

mkdirSync(root + ".build/check", { recursive: true });
execFileSync("go", ["test", "-c", "-o", binary, "."], { cwd: pkg, stdio: "inherit" });
const compiled = Date.now();
const known = existsSync(timings) ? JSON.parse(readFileSync(timings, "utf8")) : {};
const tests = execFileSync(binary, ["-test.list", "^TestInvariant"], { cwd: pkg, encoding: "utf8" })
  .split("\n")
  .filter((name) => name.startsWith("TestInvariant"))
  .sort((a, b) => (known[b] ?? Infinity) - (known[a] ?? Infinity));

const failures = [];
const slow = [];
let next = 0;
const worker = async () => {
  while (next < tests.length) {
    const name = tests[next++];
    const begin = Date.now();
    await new Promise((done) =>
      execFile(binary, ["-test.run", `^${name}$`, "-test.count", "1"], { cwd: pkg, maxBuffer: 64 << 20 }, (error, stdout, stderr) => {
        if (error) failures.push(`--- ${name}\n${(stdout + stderr).slice(-6000)}`);
        slow.push([Date.now() - begin, name]);
        done();
      }),
    );
  }
};
await Promise.all(Array.from({ length: Math.min(availableParallelism(), tests.length) }, worker));

slow.sort((a, b) => b[0] - a[0]);
writeFileSync(timings, JSON.stringify(Object.fromEntries(slow.map(([ms, name]) => [name, ms]))));
const seconds = (ms) => (ms / 1000).toFixed(1) + "s";
if (failures.length) {
  console.error(failures.join("\n\n"));
  console.error(`invariants: ${failures.length} of ${tests.length} failed`);
  process.exit(1);
}
console.log(`invariants: ${tests.length} passed in ${seconds(Date.now() - started)} (build ${seconds(compiled - started)}); slowest ${slow.slice(0, 3).map(([ms, name]) => `${name} ${seconds(ms)}`).join(", ")}`);
