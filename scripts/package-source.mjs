#!/usr/bin/env node
import { createHash } from "node:crypto";
import {
  existsSync,
  mkdirSync,
  readFileSync,
  readdirSync,
  statSync,
  writeFileSync,
} from "node:fs";
import { dirname, join, relative, resolve, sep } from "node:path";

const root = resolve(new URL("..", import.meta.url).pathname);
const packagesDir = join(root, "deploy/packages");

const assetDirectories = new Map([
  ["object", "objects"],
  ["action", "actions"],
  ["query", "queries"],
  ["page", "pages"],
  ["app", "applications"],
  ["flow", "flows"],
  ["function", "functions"],
  ["compute", "functions/compute"],
  ["agent", "agents"],
  ["connection", "integrations/connections"],
  ["source", "integrations/sources"],
  ["listener", "integrations/listeners"],
  ["writeback", "integrations/writebacks"],
  ["dataset", "integrations/datasets"],
  ["mediaset", "integrations/media-sets"],
  ["schedule", "integrations/schedules"],
  ["pipeline", "integrations/pipelines"],
  ["matching", "integrations/matching"],
  ["observation", "integrations/observations"],
  ["sample-set", "tests/sample-sets"],
  ["test-plan", "tests/test-plans"],
  ["evaluation-set", "tests/evaluations"],
  ["link-type", "objects/link-types"],
  ["property-type", "objects/property-types"],
  ["value-type", "objects/value-types"],
  ["interface", "objects/interfaces"],
  ["object-type-group", "objects/groups"],
  ["protocol", "protocols"],
  ["product", "products"],
]);

const digest = (value) =>
  `sha256:${createHash("sha256").update(value).digest("hex")}`;
const format = (value, newline) => JSON.stringify(value, null, 2) + (newline ? "\n" : "");
const readJson = (file) => JSON.parse(readFileSync(file, "utf8"));
const readText = (file) => readFileSync(file, "utf8");

function writeJson(file, value) {
  mkdirSync(dirname(file), { recursive: true });
  writeFileSync(file, format(value, true));
}

function inside(base, value) {
  const target = resolve(base, value);
  const rel = relative(base, target);
  if (!rel || rel.startsWith("..") || rel.startsWith(sep)) {
    throw new Error(`${value} escapes ${base}`);
  }
  return target;
}

function sourceDirectories(args) {
  if (args.length) return args.map((value) => resolve(root, value));
  if (!existsSync(packagesDir)) return [];
  return readdirSync(packagesDir)
    .map((name) => join(packagesDir, name))
    .filter(
      (dir) =>
        statSync(dir).isDirectory() &&
        existsSync(join(dir, "package.json")) &&
        existsSync(join(dir, "blueprint.json")),
    );
}

function artifactPath(sourceDir, descriptor) {
  return join(dirname(sourceDir), `${descriptor.id.replaceAll(".", "-")}.json`);
}

function assetDirectory(kind) {
  return assetDirectories.get(kind) ?? `assets/${kind}`;
}

function assetFileName(ref) {
  const name = ref.name
    .replace(/^build\./, "")
    .replace(/[^A-Za-z0-9._-]+/g, "-");
  return `${name || ref.kind}.json`;
}

function allocateAssetPath(ref, used) {
  const directory = assetDirectory(ref.kind);
  const name = assetFileName(ref);
  let path = join(directory, name);
  let suffix = 2;
  while (used.has(path)) {
    path = join(directory, name.replace(/\.json$/, `-${suffix}.json`));
    suffix++;
  }
  used.add(path);
  return path;
}

function packSource(sourceDir) {
  const descriptorFile = join(sourceDir, "package.json");
  const blueprintFile = join(sourceDir, "blueprint.json");
  if (!existsSync(descriptorFile) || !existsSync(blueprintFile)) {
    throw new Error(`${sourceDir}: package.json and blueprint.json are required`);
  }

  const fields = readJson(descriptorFile);
  if ("blueprint" in fields || "artifactDigest" in fields) {
    throw new Error(`${descriptorFile}: blueprint and artifactDigest are generated fields`);
  }
  const sourceBlueprint = readJson(blueprintFile);
  if (!Array.isArray(sourceBlueprint.roots) || !Array.isArray(sourceBlueprint.assets)) {
    throw new Error(`${blueprintFile}: roots and assets must be arrays`);
  }

  const assets = [];
  const refs = new Set();
  for (const entry of sourceBlueprint.assets) {
    if (!entry.ref || !entry.path || entry.body !== undefined) {
      throw new Error(`${blueprintFile}: each asset needs ref and path, not body`);
    }
    const key = `${entry.ref.kind}/${entry.ref.name}`;
    if (refs.has(key)) throw new Error(`${blueprintFile}: duplicate asset ${key}`);
    refs.add(key);
    const asset = {
      ref: entry.ref,
      contractVersion: entry.contractVersion,
      sourceVersion: entry.sourceVersion,
    };
    if (entry.requires !== undefined) asset.requires = entry.requires;
    if (entry.provides !== undefined) asset.provides = entry.provides;
    asset.body = readJson(inside(sourceDir, entry.path));
    assets.push(asset);
  }

  const blueprint = {
    roots: sourceBlueprint.roots,
    assets,
  };
  if (sourceBlueprint.guide !== undefined) {
    blueprint.guide = readText(inside(sourceDir, sourceBlueprint.guide));
  }
  if (sourceBlueprint.setup !== undefined) {
    if (
      !sourceBlueprint.setup ||
      typeof sourceBlueprint.setup !== "object" ||
      typeof sourceBlueprint.setup.steps !== "string"
    ) {
      throw new Error(`${blueprintFile}: setup needs a steps path`);
    }
    const steps = readJson(inside(sourceDir, sourceBlueprint.setup.steps));
    if (!Array.isArray(steps.steps)) {
      throw new Error(`${sourceBlueprint.setup.steps}: expected { steps: [] }`);
    }
    const roles =
      sourceBlueprint.setup.roles === undefined
        ? []
        : readJson(inside(sourceDir, sourceBlueprint.setup.roles));
    if (!Array.isArray(roles)) {
      throw new Error(`${sourceBlueprint.setup.roles}: expected an array`);
    }
    blueprint.setup = { roles, steps: steps.steps };
  }

  const descriptor = { ...fields, blueprint };
  const artifact = format(descriptor, false);
  const lock = {
    format: 1,
    package: descriptor.id,
    version: descriptor.version,
    requires: descriptor.requires ?? [],
    artifact: digest(artifact),
    assets: Object.fromEntries(
      assets
        .map((asset) => [
          `${asset.ref.kind}/${asset.ref.name}`,
          digest(format(asset, false)),
        ])
        .sort(([left], [right]) => left.localeCompare(right)),
    ),
  };
  return { descriptor, artifact, lock };
}

function splitArtifact(file, sourceDir, force) {
  const descriptor = readJson(file);
  if (!descriptor.blueprint || !descriptor.id || !descriptor.version) {
    throw new Error(`${file}: not a platform package descriptor`);
  }
  sourceDir = sourceDir ?? join(dirname(file), descriptor.id);
  if (
    existsSync(sourceDir) &&
    readdirSync(sourceDir).length > 0 &&
    !force
  ) {
    throw new Error(`${sourceDir}: already exists; pass --force to replace it`);
  }

  const fields = { ...descriptor };
  delete fields.blueprint;
  delete fields.artifactDigest;

  const used = new Set();
  const entries = [];
  for (const asset of descriptor.blueprint.assets) {
    const path = allocateAssetPath(asset.ref, used);
    writeJson(join(sourceDir, path), asset.body);
    const entry = {
      ref: asset.ref,
      path,
      contractVersion: asset.contractVersion,
      sourceVersion: asset.sourceVersion,
    };
    if (asset.requires !== undefined) entry.requires = asset.requires;
    if (asset.provides !== undefined) entry.provides = asset.provides;
    entries.push(entry);
  }

  const blueprint = {
    roots: descriptor.blueprint.roots,
    assets: entries,
  };
  if (descriptor.blueprint.guide !== undefined) {
    mkdirSync(join(sourceDir, "docs"), { recursive: true });
    writeFileSync(join(sourceDir, "docs/guide.md"), descriptor.blueprint.guide);
    blueprint.guide = "docs/guide.md";
  }
  if (descriptor.blueprint.setup !== undefined) {
    writeJson(join(sourceDir, "setup.json"), {
      steps: descriptor.blueprint.setup.steps,
    });
    blueprint.setup = { steps: "setup.json" };
    if ((descriptor.blueprint.setup.roles?.length ?? 0) > 0) {
      writeJson(join(sourceDir, "roles/requirements.json"), descriptor.blueprint.setup.roles);
      blueprint.setup.roles = "roles/requirements.json";
    }
  }

  writeJson(join(sourceDir, "package.json"), fields);
  writeJson(join(sourceDir, "blueprint.json"), blueprint);
  const packed = packSource(sourceDir);
  writeJson(join(sourceDir, "package.lock.json"), packed.lock);
  return { sourceDir, descriptor: packed.descriptor, assets: entries.length };
}

function writePackage(sourceDir) {
  const packed = packSource(sourceDir);
  writeFileSync(artifactPath(sourceDir, packed.descriptor), packed.artifact);
  writeJson(join(sourceDir, "package.lock.json"), packed.lock);
  return packed;
}

function checkPackage(sourceDir) {
  const packed = packSource(sourceDir);
  const file = artifactPath(sourceDir, packed.descriptor);
  if (!existsSync(file)) throw new Error(`${file}: missing generated artifact`);
  if (readText(file) !== packed.artifact) {
    throw new Error(`${file}: generated artifact is stale; run make packages`);
  }
  const lockFile = join(sourceDir, "package.lock.json");
  if (!existsSync(lockFile) || readText(lockFile) !== format(packed.lock, true)) {
    throw new Error(`${lockFile}: generated lock is stale; run make packages`);
  }
  return packed;
}

const [command, ...args] = process.argv.slice(2);
if (command === "split") {
  const file = args[0];
  if (!file) throw new Error("usage: package-source.mjs split <artifact.json> [sourceDir]");
  const flags = args.filter((value) => value.startsWith("--"));
  const positional = args.filter((value) => !value.startsWith("--"));
  const result = splitArtifact(
    resolve(root, positional[0]),
    positional[1] ? resolve(root, positional[1]) : undefined,
    flags.includes("--force"),
  );
  console.log(
    `split ${result.descriptor.id}@${result.descriptor.version} -> ${relative(root, result.sourceDir)} (${result.assets} assets)`,
  );
} else if (command === "pack") {
  const directories = sourceDirectories(args);
  if (directories.length === 0) throw new Error("no package sources found");
  for (const directory of directories) {
    const packed = writePackage(directory);
    console.log(
      `packed ${packed.descriptor.id}@${packed.descriptor.version} -> ${relative(root, artifactPath(directory, packed.descriptor))}`,
    );
  }
} else if (command === "check") {
  const directories = sourceDirectories(args);
  if (directories.length === 0) throw new Error("no package sources found");
  for (const directory of directories) {
    const packed = checkPackage(directory);
    console.log(
      `checked ${packed.descriptor.id}@${packed.descriptor.version} (${packed.descriptor.blueprint.assets.length} assets)`,
    );
  }
} else {
  throw new Error("usage: package-source.mjs split|pack|check ...");
}
