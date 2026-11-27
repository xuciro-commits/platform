import { createHash } from "node:crypto";
import { readFileSync, readdirSync, lstatSync, realpathSync } from "node:fs";
import { join, relative, resolve, sep } from "node:path";
import { fileURLToPath } from "node:url";
const lock = JSON.parse(readFileSync(new URL("./package-fixtures.lock.json", import.meta.url), "utf8"));
const sha = (data) => createHash("sha256").update(data).digest("hex");

let verifiedRoot;
export function fixtureRoot() {
  if (verifiedRoot) return verifiedRoot;
  const root = realpathSync(process.env.PLATFORM_FIXTURES_DIR || fileURLToPath(new URL("../../platform-packages/fixtures/platform/", import.meta.url)));
  const raw = readFileSync(join(root, "manifest.json"));
  if (sha(raw) !== lock.manifestSha256) throw Error("Platform fixture manifest differs from package-fixtures.lock.json");
  const manifest = JSON.parse(raw);
  if (manifest.version !== 1 || !Array.isArray(manifest.files)) throw Error("Invalid fixture manifest");
  const expected = new Map();
  for (const entry of manifest.files) {
    if (!/^(packages|test-packages|frontend-fixtures)\/[a-zA-Z0-9_.\/-]+$/.test(entry.path) || entry.path.split("/").some(x => !x || x === "." || x === "..") || expected.has(entry.path)) throw Error("Invalid fixture entry " + entry.path);
    expected.set(entry.path, entry);
  }
  function walk(dir) {
    for (const name of readdirSync(dir)) {
      const path = join(dir, name), stat = lstatSync(path), rel = relative(root, path).split(sep).join("/");
      if (stat.isSymbolicLink()) throw Error("Unexpected fixture symlink " + rel);
      if (stat.isDirectory()) { walk(path); continue; }
      if (!stat.isFile()) throw Error("Unexpected fixture " + rel);
      if (rel === "README.md" || rel === "manifest.json") continue;
      const entry = expected.get(rel), bytes = readFileSync(path);
      if (!entry || entry.bytes !== bytes.length || entry.sha256 !== sha(bytes)) throw Error("Fixture digest mismatch " + rel);
      expected.delete(rel);
    }
  }
  walk(root);
  if (expected.size) throw Error("Missing fixtures: " + [...expected.keys()].join(", "));
  verifiedRoot = root;
  return root;
}

export function fixtureRelease(path) {
  const url = lock.releases[path];
  if (!url) return undefined;
  return { url, expectedSha256: "sha256:" + sha(readFileSync(join(fixtureRoot(), path))) };
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  if (process.argv[2] === "--revision") {
    if (!/^[0-9a-f]{40}$/.test(lock.revision ?? "")) throw Error("Package fixture commit has not been published and pinned");
    console.log(lock.revision);
  } else console.log(fixtureRoot());
}
