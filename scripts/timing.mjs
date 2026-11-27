import { spawn } from "node:child_process";
import { appendFileSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { join, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const seconds = ms => (ms / 1000).toFixed(3) + "s";
const clock = () => {
  const now = new Date();
  return now.toTimeString().slice(0, 8) + "." + String(now.getMilliseconds()).padStart(3, "0");
};
const color = (code, value) => process.stderr.isTTY && !process.env.NO_COLOR ? "\x1b[" + code + "m" + value + "\x1b[0m" : value;
export function note(message) {
  if (!process.env.PLATFORM_TIMING_FILE) return;
  const elapsed = Math.max(0, Date.now() - Number(process.env.PLATFORM_TIMING_START));
  process.stderr.write(color("90", clock()) + "  " + color("36", ("+" + seconds(elapsed)).padStart(10)) + "  " + message + "\n");
}
export function begin(label) {
  if (!process.env.PLATFORM_TIMING_FILE) return () => {};
  const started = performance.now();
  const scope = process.env.PLATFORM_TIMING_SCOPE;
  const name = scope ? scope + " · " + label : label;
  note("→ " + name);
  let ended = false;
  return (ok = true) => {
    if (ended) return;
    ended = true;
    const ms = performance.now() - started;
    appendFileSync(process.env.PLATFORM_TIMING_FILE, JSON.stringify({ name, ms, ok }) + "\n");
    note(color(ok ? "32" : "31", ok ? "✓" : "✗") + " " + name + " (" + seconds(ms) + ")");
  };
}
export function measure(label, fn) {
  const end = begin(label);
  try {
    const value = fn();
    if (value && typeof value.then === "function") return value.then(result => { end(); return result; }, error => { end(false); throw error; });
    end();
    return value;
  } catch (error) {
    end(false);
    throw error;
  }
}
async function execute(file, args) {
  const child = spawn(file, args, { stdio: "inherit" });
  const interrupt = () => child.kill("SIGINT");
  const terminate = () => child.kill("SIGTERM");
  process.on("SIGINT", interrupt);
  process.on("SIGTERM", terminate);
  try {
    return await new Promise((resolve, reject) => {
      child.once("error", reject);
      child.once("exit", (code, signal) => resolve(code ?? (signal === "SIGINT" ? 130 : signal === "SIGTERM" ? 143 : 1)));
    });
  } finally {
    process.off("SIGINT", interrupt);
    process.off("SIGTERM", terminate);
  }
}
async function main(args) {
  const [mode, label, file, ...rest] = args;
  if (mode !== "run" || !label || !file) throw Error("Usage: timing.mjs run <label> <command> [args...]");
  const outer = !process.env.PLATFORM_TIMING_FILE;
  let dir;
  if (outer) {
    dir = mkdtempSync(join(tmpdir(), "platform-timing-"));
    process.env.PLATFORM_TIMING_FILE = join(dir, "phases.jsonl");
    writeFileSync(process.env.PLATFORM_TIMING_FILE, "", { mode: 0o600 });
    process.env.PLATFORM_TIMING_START = String(Date.now());
  }
  const started = performance.now();
  const end = begin(label);
  let code = 1;
  try {
    code = await execute(file, rest);
  } finally {
    end(code === 0);
    if (outer) {
      const rows = readFileSync(process.env.PLATFORM_TIMING_FILE, "utf8").split("\n").filter(Boolean).map(line => JSON.parse(line));
      const total = performance.now() - started;
      const stages = rows.slice(0, -1).sort((a, b) => b.ms - a.ms);
      process.stderr.write("\n" + color("1", "EIDOS · 耗时排行") + "（包含嵌套与并发阶段，不直接相加）\n");
      const max = stages[0]?.ms || 1;
      for (const row of stages.slice(0, 15)) {
        const bar = "█".repeat(Math.max(1, Math.round(row.ms / max * 24)));
        process.stderr.write("  " + (row.ok ? "✓ " : "✗ ") + seconds(row.ms).padStart(10) + "  " + color(row.ok ? "36" : "31", bar.padEnd(24)) + "  " + row.name + "\n");
      }
      const output = resolve(fileURLToPath(new URL("../.build/timing/", import.meta.url)));
      const name = Date.now() + "-" + process.pid + ".json";
      try {
        mkdirSync(output, { recursive: true });
        writeFileSync(join(output, name), JSON.stringify({ command: label, exitCode: code, totalMs: total, phases: stages }, null, 2));
        process.stderr.write("总耗时 " + seconds(total) + " · " + (code === 0 ? "完成" : "失败（" + code + "）") + " · .build/timing/" + name + "\n");
      } finally {
        rmSync(dir, { recursive: true, force: true });
      }
    }
  }
  process.exitCode = code;
}
if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) await main(process.argv.slice(2));
