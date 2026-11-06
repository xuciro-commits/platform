import fs from "node:fs";
import path from "node:path";

const root = path.resolve(import.meta.dirname, "..");
const sourceRoots = ["web/packages", "web/apps"];
const sourceFiles = [];

function walk(dir) {
  for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
    const file = path.join(dir, entry.name);
    if (entry.isDirectory()) {
      if (!["node_modules", "dist", "gen"].includes(entry.name)) walk(file);
    } else if (/\.(tsx|ts)$/.test(entry.name)) {
      sourceFiles.push(file);
    }
  }
}

for (const sourceRoot of sourceRoots) walk(path.join(root, sourceRoot));

const dictionaryFile = (file) => /i18n\.ts$/.test(file) || /i18n\/[^/]+\.ts$/.test(file);
const dictionaries = new Set();

for (const file of sourceFiles.filter(dictionaryFile)) {
  const source = fs.readFileSync(file, "utf8");
  for (const match of source.matchAll(/"((?:[^"\\]|\\.)*)"\s*:\s*"/g)) {
    try {
      dictionaries.add(JSON.parse(`"${match[1]}"`));
    } catch {
      continue;
    }
  }
}

const missing = new Map();
const add = (text, file) => {
  if (!text || dictionaries.has(text)) return;
  if (!missing.has(text)) missing.set(text, []);
  missing.get(text).push(file);
};

for (const file of sourceFiles) {
  if (dictionaryFile(file)) continue;
  const source = fs.readFileSync(file, "utf8");
  for (const match of source.matchAll(/\bt\(\s*"((?:[^"\\]|\\.)*)"\s*(?:,|\))/g)) {
    try {
      add(JSON.parse(`"${match[1]}"`), file);
    } catch {
      continue;
    }
  }
  if (!source.includes("defineApp(")) continue;
  for (const match of source.matchAll(/description\s*:\s*"((?:[^"\\]|\\.)*)"/g)) {
    try {
      add(JSON.parse(`"${match[1]}"`), file);
    } catch {
      continue;
    }
  }
}

if (missing.size) {
  console.error(`i18n: ${missing.size} missing dictionary entries`);
  for (const [text, files] of [...missing.entries()].sort(([a], [b]) => a.localeCompare(b))) {
    console.error(`${text}\t${[...new Set(files)].map((file) => path.relative(root, file)).join(", ")}`);
  }
  process.exit(1);
}

console.log(`i18n: ${dictionaries.size} keys, no missing literal t() calls or defineApp descriptions`);
