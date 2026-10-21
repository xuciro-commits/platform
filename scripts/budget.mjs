#!/usr/bin/env node
// 复杂度预算（ADR-0064 §9）：复杂度只减不增。
//
// 过去的做法是"每轮重新审计一遍，再写一份结论"。审计报告本身也在增长，减掉的
// 却是零头（ADR-0064 记录的一轮：净减 72 行）。这里换一种做法：把"系统有多大、
// 有多少入口、有多少份文档"变成十几个数字，记在 .budget.json 里。数字只能变小。
//
//   node scripts/budget.mjs            对照预算；任一数字变大即失败
//   node scripts/budget.mjs --update   把当前实测写回预算（只在真的删掉东西后）
//   node scripts/budget.mjs --json     机读输出
//
// 预算不是目标，是刹车。想加一个应用、一个菜单项、一份 ADR，先在同一次改动里
// 删掉等量的东西；删不动，说明这个新增不是必需的。

import { readdirSync, readFileSync, statSync, writeFileSync, existsSync } from "node:fs";
import { join, relative, sep } from "node:path";

const ROOT = new URL("..", import.meta.url).pathname;
const BUDGET_FILE = join(ROOT, ".budget.json");
const UPDATE = process.argv.includes("--update");
const JSON_OUT = process.argv.includes("--json");

const SKIP = new Set(["node_modules", ".git", ".build", "dist", "coverage", ".cache", "vendor"]);
const walk = (dir, accept) => {
  const out = [];
  for (const entry of readdirSync(dir, { withFileTypes: true })) {
    if (SKIP.has(entry.name)) continue;
    const path = join(dir, entry.name);
    if (entry.isDirectory()) out.push(...walk(path, accept));
    else if (accept(path)) out.push(path);
  }
  return out;
};
const lines = (path) => readFileSync(path, "utf8").split("\n").length - 1;
// 文档按字符计：这些文档的单行常有上千字，按行数统计会让"把巨型单元格写短"看起来毫无收益。
const chars = (path) => readFileSync(path, "utf8").length;
const size = (paths) => paths.reduce((total, path) => total + chars(path), 0);
const k = (value) => `${(value / 1000).toFixed(1)}k 字`;

// --- 文档：总量、最长的一份、最长的一行状态 --------------------------------
const adrDir = join(ROOT, "docs/ADR");
const adrs = readdirSync(adrDir).filter((name) => name.endsWith(".md") && name !== "README.md").map((name) => join(adrDir, name));
// WorkQueue 是施工队列，按当天的工作进出，不是知识库；不计入文档预算。
const allDocs = walk(join(ROOT, "docs"), (path) => path.endsWith(".md") && !path.endsWith(join("docs", "WorkQueue.md")));
const statusLine = (path) => {
  const line = readFileSync(path, "utf8").split("\n").find((text) => /^状态[:：]/.test(text)) ?? "";
  return line.length;
};

// --- Web：入口数量 ---------------------------------------------------------
// 一个 `defineApp({...})` 就是一个门户入口。菜单项和视图是用户实际要点的东西。
const webSources = walk(join(ROOT, "web"), (path) => /\.tsx?$/.test(path) && !/\.test\.(ts|tsx|mjs)$/.test(path) && !path.includes(`${sep}gen${sep}`));
const blocks = [];
for (const path of webSources) {
  const text = readFileSync(path, "utf8");
  for (let at = text.indexOf("defineApp({"); at >= 0; at = text.indexOf("defineApp({", at + 1)) blocks.push({ path, block: objectAt(text, text.indexOf("{", at)) });
}
const apps = blocks.map(({ path, block }) => ({ path, id: /id:\s*["']([^"']+)["']/.exec(block)?.[1] ?? "?", nav: count(block, /label:/g), views: count(block, /title:/g) }));
const distinct = new Set(apps.map((app) => app.id)).size;

// --- 代码：最大的文件、最拥挤的目录 ---------------------------------------
const goFiles = walk(join(ROOT, "capabilities"), (path) => path.endsWith(".go"));
const byDir = new Map();
for (const path of goFiles) {
  const dir = path.slice(0, path.lastIndexOf(sep));
  byDir.set(dir, (byDir.get(dir) ?? 0) + 1);
}
const handWritten = webSources.filter((path) => !path.endsWith(".d.ts"));
// Go 手写生产代码：排除测试与生成物（.pb.go、gen/）。后端减量只在这里计，
// 测试与生成物单列，避免"删断言"或"删生成文件"冒充减量。
const goProduction = walk(ROOT, (path) => path.endsWith(".go") && !path.endsWith("_test.go") && !path.endsWith(".pb.go") && !path.includes(`${sep}gen${sep}`));
const largestOf = (paths) => paths.reduce((best, path) => (lines(path) > (best.lines ?? 0) ? { path, lines: lines(path) } : best), { path: "", lines: 0 });
const largestGo = largestOf(goFiles);
const largestTs = largestOf(handWritten);
const biggestDoc = adrs.reduce((best, path) => (chars(path) > (best.chars ?? 0) ? { path, chars: chars(path) } : best), { path: "", chars: 0 });
const fullestDir = [...byDir].sort((a, b) => b[1] - a[1])[0];

const metrics = {
  "docs.adr": { value: adrs.length, of: "份 ADR（不含索引）" },
  "docs.adrChars": { value: size(adrs), of: "ADR 总字数" },
  "docs.longestAdr": { value: Math.max(...adrs.map(chars)), of: `最长一份：${relative(ROOT, biggestDoc.path)}` },
  "docs.statusLine": { value: Math.max(...adrs.map(statusLine)), of: "最长状态行字符数" },
  "docs.topLevel": { value: readdirSync(join(ROOT, "docs")).filter((name) => name.endsWith(".md")).length, of: "docs/ 顶层文档" },
  "docs.chars": { value: size(allDocs), of: "docs/ 全部字数" },
  "ui.definitions": { value: apps.length, of: "defineApp 定义数" },
  "ui.apps": { value: distinct, of: "不同应用 id 数" },
  "ui.navItems": { value: apps.reduce((total, app) => total + app.nav, 0), of: "菜单项总数" },
  "ui.navItemsMax": { value: Math.max(...apps.map((app) => app.nav)), of: `菜单最多：${apps.sort((a, b) => b.nav - a.nav)[0]?.id}` },
  "ui.views": { value: apps.reduce((total, app) => total + app.views, 0), of: "视图总数" },
  "code.largestGo": { value: largestGo.lines, of: `最长 Go 文件：${relative(ROOT, largestGo.path)}` },
  "code.largestTs": { value: largestTs.lines, of: `最长手写 TS：${relative(ROOT, largestTs.path)}` },
  "code.fullestPackage": { value: fullestDir[1], of: `文件最多的 Go 目录：${relative(ROOT, fullestDir[0])}` },
  "code.goProduction": { value: goProduction.reduce((total, path) => total + lines(path), 0), of: "手写 Go 生产代码行" },
  "code.goFiles": { value: goProduction.length, of: "手写 Go 生产文件" },
};

if (UPDATE) {
  writeFileSync(BUDGET_FILE, JSON.stringify(Object.fromEntries(Object.entries(metrics).map(([name, { value }]) => [name, value])), null, 2) + "\n");
  console.log(`budget: 已写入 ${relative(ROOT, BUDGET_FILE)}`);
  process.exit(0);
}

if (!existsSync(BUDGET_FILE)) {
  console.error("budget: 缺少 .budget.json；先运行 node scripts/budget.mjs --update");
  process.exit(2);
}
const budget = JSON.parse(readFileSync(BUDGET_FILE, "utf8"));
const rows = Object.entries(metrics).map(([name, { value, of }]) => ({ name, of, now: value, limit: budget[name] ?? value, over: budget[name] !== undefined && value > budget[name] }));
if (JSON_OUT) {
  console.log(JSON.stringify(Object.fromEntries(rows.map((row) => [row.name, { now: row.now, limit: row.limit, over: row.over }])), null, 2));
  process.exit(rows.some((row) => row.over) ? 1 : 0);
}

const width = Math.max(...rows.map((row) => row.name.length));
console.log(`${"指标".padEnd(width)}  ${"现在".padStart(6)} ${"上限".padStart(6)}  说明`);
const show = (name, value) => (name.includes("Chars") || name === "docs.chars" || name === "docs.longestAdr" ? k(value) : String(value));
for (const row of rows) {
  const change = row.now === row.limit ? "" : row.now < row.limit ? ` ↓${show(row.name, row.limit - row.now)}` : ` ↑${show(row.name, row.now - row.limit)}`;
  console.log(`${row.name.padEnd(width)}  ${show(row.name, row.now).padStart(8)} ${show(row.name, row.limit).padStart(8)}  ${row.of}${change}`);
}
const over = rows.filter((row) => row.over);
if (over.length === 0) {
  console.log("\nbudget: 通过。数字只能变小；删掉东西之后用 --update 下调上限。");
  process.exit(0);
}
console.error(`\nbudget: ${over.map((row) => row.name).join("、")} 超出上限。`);
console.error("先删等量的东西，再 --update 写下新的上限；不要让检查跟着代码一起长。");
process.exit(1);

// --- 小工具 ----------------------------------------------------------------
function count(text, pattern) {
  return (text.match(pattern) ?? []).length;
}
// 从 `{` 开始取成对的一段源码；跳过字符串与注释，模板字面量整体跳过。
function objectAt(text, start) {
  let depth = 0;
  for (let at = start; at < text.length; at++) {
    const char = text[at];
    if (char === "/" && text[at + 1] === "/") { at = text.indexOf("\n", at); if (at < 0) break; continue; }
    if (char === "/" && text[at + 1] === "*") { at = text.indexOf("*/", at) + 1; continue; }
    if (char === '"' || char === "'" || char === "`") { at = skipString(text, at); continue; }
    if (char === "{") depth++;
    else if (char === "}" && --depth === 0) return text.slice(start, at + 1);
  }
  return text.slice(start);
}
function skipString(text, start) {
  const quote = text[start];
  for (let at = start + 1; at < text.length; at++) {
    if (text[at] === "\\") { at++; continue; }
    if (text[at] === quote) return at;
  }
  return text.length;
}
