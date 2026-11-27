import { execFileSync } from "node:child_process";
import { readFileSync, mkdirSync, writeFileSync, mkdtempSync, rmSync } from "node:fs";
import { tmpdir } from "node:os";
import { checkout } from "./package-fixtures.mjs";
import { join } from "node:path";
import { fileURLToPath } from "node:url";

const root = fileURLToPath(new URL("../", import.meta.url));
const lock = JSON.parse(readFileSync(join(root, "package-fixtures.lock.json"), "utf8"));
const scratch = mkdtempSync(join(tmpdir(), "platform-purity-"));
try {
  const source = checkout(scratch);
  const git = args => execFileSync("git", ["-C", source, ...args], { encoding: "utf8" });
  const manifests = git(["ls-tree", "-r", "--name-only", lock.revision, "packages"]).trim().split("\n").filter(x => x.endsWith("/package.yaml"));
  const ids = new Set(), namespaces = new Set();
  for (const path of manifests) {
    for (const match of git(["show", lock.revision + ":" + path]).matchAll(/^(id|namespace):\s*["']?([\w.-]+)["']?\s*(?:#.*)?$/gm)) {
      (match[1] === "id" ? ids : namespaces).add(match[2]);
    }
  }
  if (!ids.size || !namespaces.size) throw Error("Empty package vocabulary");
  const escaped = x => x.replace(/[.*+?^$(){}|[\]\\]/g, "\\$&");
  const alternatives = [...namespaces].sort((a,b) => b.length-a.length).map(escaped).join("|");
  const qualified = new RegExp("(?<![A-Za-z0-9_.-])(?:" + alternatives + ")\\.(?:[a-zA-Z_][a-zA-Z0-9_.-]*)(?:/\\d+)?", "g");
  const paths = execFileSync("git", ["ls-files", "capabilities/server", "frontend/src"], { cwd: root, encoding: "utf8" }).trim().split("\n")
    .filter(p => /\.(go|ts|tsx|js|jsx)$/.test(p) && !/(?:_test\.go|\.(?:test|spec)\.[^.]+)$/.test(p) && !/(?:^|\/)(?:gen|generated|fixtures|testdata|e2e|__tests__)(?:\/|$)/.test(p));
  const hits = [];
  for (const path of paths) {
    const source = readFileSync(join(root,path), "utf8");
    const lines = source.split("\n");
    let lineNumber = 1, previous = 0;
    const tokens = /\/\/[^\n]*|\/\*[\s\S]*?\*\/|"(?:\\.|[^"\\\n])*"|'(?:\\.|[^'\\\n])*'|\x60[^\x60]*\x60/g;
    for (const token of source.matchAll(tokens)) {
      lineNumber += source.slice(previous,token.index).split("\n").length-1;
      previous = token.index;
      const raw = token[0];
      if (raw.startsWith("//") || raw.startsWith("/*")) continue;
      let value = raw.slice(1,-1);
      if (raw[0] === '"') { try { value = JSON.parse(raw); } catch {} }
      if (path.endsWith(".go") && raw.charCodeAt(0) === 96 && /^(?:[A-Za-z][\w-]*:"(?:\\.|[^"\\])*"\s*)+$/.test(value)) continue;
      const words = new Set();
      if (ids.has(value)) words.add(value);
      if (value.endsWith(".") && namespaces.has(value.slice(0,-1))) words.add(value);
      if (!path.endsWith(".go") && raw.charCodeAt(0) === 96) value = value.replace(/\$\{[^}]*\}/g, "");
      for (const match of value.matchAll(qualified)) words.add(match[0]);
      for (const inner of value.matchAll(/"([^"\n]+)"|'([^'\n]+)'/g)) if (ids.has(inner[1] ?? inner[2])) words.add(inner[1] ?? inner[2]);
      if (words.size) hits.push({path,line:lineNumber,context:lines[lineNumber-1].trim(),words:[...words]});
    }
  }
  const collisions = JSON.parse(readFileSync(join(root,"scripts/purity-collisions.json"),"utf8"));
  const counts = new Map();
  const remaining = hits.filter(hit => {
    const allowance = collisions[hit.path]?.find(c => c.context === hit.context && JSON.stringify(c.words) === JSON.stringify(hit.words));
    if (!allowance) return true;
    const key = JSON.stringify([hit.path,hit.context,hit.words]);
    const count = (counts.get(key) ?? 0) + 1;
    counts.set(key,count);
    return count > allowance.count;
  });
  const report = join(root,".build/purity/purity-candidates.json");
  mkdirSync(join(root,".build/purity"),{recursive:true});
  writeFileSync(report,JSON.stringify(remaining,null,2));
  console.log("purity: "+new Set([...ids,...namespaces]).size+" locked identifiers, "+remaining.length+" unclassified candidates, "+(hits.length-remaining.length)+" audited generic collisions");
  for (const hit of remaining) console.log(hit.path+":"+hit.line+" "+hit.words.join(", "));
  console.log("purity report: "+report);
  if(remaining.length)process.exitCode=1;
} finally { rmSync(scratch,{recursive:true,force:true}); }
