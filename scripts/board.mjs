#!/usr/bin/env node
import { appendFileSync, existsSync, mkdirSync, readFileSync, readdirSync, rmSync, statSync, watchFile, writeFileSync } from "node:fs";
import { execFileSync } from "node:child_process";
import { basename, dirname, join } from "node:path";

const agents = ["lead", "ship", "studio", "kit", "fire", "owner"];
const kinds = ["say", "assign", "idea", "block", "review", "merged", "contract"];
const git = (...args) => execFileSync("git", args, { encoding: "utf8" }).trim();
const common = git("rev-parse", "--path-format=absolute", "--git-common-dir");
const board = process.env.PLATFORM_BOARD ?? join(common, "board");
const log = join(board, "events.log");
for (const dir of ["req", "locks", "seen"]) mkdirSync(join(board, dir), { recursive: true });

const [command, ...rest] = process.argv.slice(2);
const flags = {};
const words = [];
for (let i = 0; i < rest.length; i++) {
  if (rest[i].startsWith("--")) flags[rest[i].slice(2)] = rest[++i];
  else words.push(rest[i]);
}
const fail = (message) => {
  console.error(`board: ${message}`);
  process.exit(1);
};
const top = git("rev-parse", "--show-toplevel");
const primary = dirname(common);
const as = flags.as ?? process.env.PLATFORM_AGENT ?? (top === primary ? "lead" : agents.includes(basename(top)) ? basename(top) : undefined);
if (!["status", "show", "help", undefined].includes(command) && !agents.includes(as)) {
  fail(`run inside .build/<name> or pass --as one of ${agents.join(", ")}`);
}

const now = () => new Date().toISOString().slice(0, 19) + "Z";
const hours = (file) => (Date.now() - statSync(file).mtimeMs) / 3_600_000;
const text = (from) => words.slice(from).join(" ").replace(/\s+/g, " ").trim();
const post = (to, kind, ref, body) => appendFileSync(log, [now(), as, to, kind, ref || "-", body].join("\t") + "\n");
const create = (file, body) => {
  try {
    writeFileSync(file, body, { flag: "wx" });
    return true;
  } catch (error) {
    if (error.code === "EEXIST") return false;
    throw error;
  }
};
const field = (file, name) => (existsSync(file) ? (readFileSync(file, "utf8").match(new RegExp(`^${name}: (.*)$`, "m")) ?? [])[1] : undefined);
const request = (id) => {
  const dir = join(board, "req", id ?? "");
  if (!id || !existsSync(join(dir, "00-open.md"))) fail(`no request ${id}`);
  return dir;
};
const lines = () => (existsSync(log) ? readFileSync(log, "utf8").split("\n").filter(Boolean) : []);
const mine = (line) => {
  const [, from, to] = line.split("\t");
  return from !== as && (to === as || to === "all" || as === "lead");
};
const lock = (name, ref) => {
  const dir = join(board, "locks", name);
  try {
    mkdirSync(dir);
  } catch (error) {
    if (error.code !== "EEXIST") throw error;
    fail(`${name} is held by ${field(join(dir, "holder"), "by") ?? "someone still writing it"}`);
  }
  writeFileSync(join(dir, "holder"), `by: ${as}\nfor: ${ref ?? "-"}\nsince: ${now()}\n`);
  post("all", "lock", name, ref ?? "");
};
const unlock = (name, force) => {
  const dir = join(board, "locks", name);
  if (!existsSync(dir)) fail(`${name} is not locked`);
  const holder = field(join(dir, "holder"), "by");
  if (holder !== as && !(force === "yes" && ["lead", "owner"].includes(as))) fail(`${name} is held by ${holder}; only lead or owner may --force yes`);
  rmSync(dir, { recursive: true });
  post("all", "unlock", name, holder === as ? "" : `taken back from ${holder}`);
};

const alerts = () => {
  const out = [];
  for (const id of readdirSync(join(board, "req")).filter((id) => !id.endsWith(".claim"))) {
    const open = join(board, "req", id, "00-open.md");
    const taken = join(board, "req", id, "01-taken.md");
    if (!existsSync(open) || existsSync(join(board, "req", id, "02-done.md"))) continue;
    if (!existsSync(taken)) {
      if (hours(open) > 2) out.push(`${id} open ${hours(open).toFixed(1)}h, nobody took it (to ${field(open, "to")})`);
    } else if (hours(taken) > 12) out.push(`${id} taken by ${field(taken, "by")} ${hours(taken).toFixed(1)}h ago, not done`);
  }
  for (const name of readdirSync(join(board, "locks"))) {
    const holder = join(board, "locks", name, "holder");
    if (existsSync(holder) && hours(holder) > 12) out.push(`lock ${name} held by ${field(holder, "by")} for ${hours(holder).toFixed(1)}h`);
  }
  return out;
};

const read = () => {
  const seen = join(board, "seen", as);
  const all = existsSync(log) ? readFileSync(log) : Buffer.alloc(0);
  const from = existsSync(seen) ? Number(readFileSync(seen, "utf8")) : 0;
  writeFileSync(seen, String(all.length));
  return all.subarray(Math.min(from, all.length)).toString("utf8").split("\n").filter(Boolean).filter(mine);
};

switch (command) {
  case "say": {
    const kind = flags.kind ?? "say";
    if (!kinds.includes(kind)) fail(`--kind must be one of ${kinds.join(", ")}`);
    if (![...agents, "all"].includes(flags.to)) fail("--to must name an agent or all");
    post(flags.to, kind, flags.ref, text(0));
    break;
  }
  case "ask": {
    if (!agents.includes(flags.to)) fail("--to must name an agent");
    const used = readdirSync(join(board, "req")).map((id) => Number.parseInt(id.slice(1), 10)).filter(Number.isFinite);
    let n = Math.max(0, ...used) + 1;
    while (!create(join(board, "req", `R${n}.claim`), as)) n++;
    mkdirSync(join(board, "req", `R${n}`));
    writeFileSync(join(board, "req", `R${n}`, "00-open.md"), `from: ${as}\nto: ${flags.to}\nfor: ${flags.ref ?? "-"}\nopened: ${now()}\n\n${text(0)}\n`);
    post(flags.to, "ask", `R${n}`, text(0));
    console.log(`R${n}`);
    break;
  }
  case "take": {
    const dir = request(words[0]);
    if (!create(join(dir, "01-taken.md"), `by: ${as}\ntaken: ${now()}\n\n${text(1)}\n`)) fail(`${words[0]} is already taken by ${field(join(dir, "01-taken.md"), "by")}`);
    post(field(join(dir, "00-open.md"), "from"), "take", words[0], text(1));
    break;
  }
  case "done": {
    const dir = request(words[0]);
    if (field(join(dir, "01-taken.md"), "by") !== as) fail(`${words[0]} is not taken by ${as}`);
    if (!flags.sha) fail("--sha names the commit on main that answers the request");
    if (!create(join(dir, "02-done.md"), `by: ${as}\nsha: ${flags.sha}\ndone: ${now()}\n\n${text(1)}\n`)) fail(`${words[0]} is already done`);
    post(field(join(dir, "00-open.md"), "from"), "done", words[0], `${flags.sha} ${text(1)}`);
    break;
  }
  case "show": {
    const dir = request(words[0]);
    for (const name of readdirSync(dir).sort()) console.log(`--- ${name}\n${readFileSync(join(dir, name), "utf8")}`);
    break;
  }
  case "lock":
    if (!/^[\w-]+$/.test(words[0] ?? "")) fail("lock needs a name: kernel, main, shared-<area> or a task id");
    lock(words[0], flags.ref);
    break;
  case "unlock":
    unlock(words[0] ?? "", flags.force);
    break;
  case "land": {
    const branch = git("branch", "--show-current");
    if (!branch || branch === "main") fail("land from your own task branch inside .build/<name>");
    if (git("status", "--porcelain")) fail("commit or drop your changes first");
    try {
      git("merge-base", "--is-ancestor", "main", "HEAD");
    } catch {
      fail("main moved: git rebase main, regenerate hot files, rerun your checks, then land again");
    }
    lock("main", flags.ref ?? branch);
    try {
      execFileSync("make", ["check"], { stdio: "inherit" });
      execFileSync("git", ["-C", primary, "merge", "--ff-only", branch], { stdio: "inherit" });
      if (flags.push !== "no") execFileSync("git", ["-C", primary, "push", "origin", "main"], { stdio: "inherit" });
    } catch {
      unlock("main");
      fail("land stopped; main is unchanged unless the push alone failed (then push from the primary checkout)");
    }
    unlock("main");
    const sha = git("rev-parse", "--short", "HEAD");
    post("all", "merged", flags.ref ?? branch, `${sha} ${git("log", "-1", "--format=%s")}`);
    console.log(`landed ${sha} on main`);
    break;
  }
  case "inbox":
    for (const line of read()) console.log(line);
    for (const alert of alerts()) console.log(`ALERT\t${alert}`);
    break;
  case "watch":
    for (const line of read()) console.log(line);
    watchFile(log, { interval: 1000 }, () => {
      for (const line of read()) console.log(line);
    });
    break;
  case "status": {
    for (const id of readdirSync(join(board, "req")).filter((id) => !id.endsWith(".claim")).sort()) {
      const dir = join(board, "req", id);
      if (existsSync(join(dir, "02-done.md"))) continue;
      const by = field(join(dir, "01-taken.md"), "by");
      console.log(`${id}\t${by ? `taken by ${by}` : "open"}\tfrom ${field(join(dir, "00-open.md"), "from")} for ${field(join(dir, "00-open.md"), "for")}`);
    }
    for (const name of readdirSync(join(board, "locks"))) {
      const holder = join(board, "locks", name, "holder");
      console.log(`lock ${name}\t${field(holder, "by")}\tsince ${field(holder, "since")}`);
    }
    for (const line of lines().slice(-10)) console.log(line);
    for (const alert of alerts()) console.log(`ALERT\t${alert}`);
    break;
  }
  default:
    console.log(`usage: node scripts/board.mjs <command>   (lead in the primary checkout, else the .build/<name> folder, or --as)
  inbox                              new events for you since last read, then alerts
  watch                              the same, kept open (for a monitor)
  say --to <agent|all> [--kind k] [--ref id] <text>   kinds: ${kinds.join(", ")}
  ask --to <agent> [--ref task] <text>               open a request; prints its id
  take <R#> <interface sketch>       take a request (only one taker wins)
  done <R#> --sha <commit> <how to use it>
  show <R#>                          the request and its answers
  lock <name> [--ref id]             kernel, main, shared-<area> or a task id; unlock <name> [--force yes]
  land [--ref task] [--push no]      make check, fast-forward main to your branch, push, announce
  status                             open requests, locks, last events, alerts`);
}
