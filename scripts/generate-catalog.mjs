import { createHash } from "node:crypto";
import { readFileSync, readdirSync, unlinkSync, writeFileSync } from "node:fs";
import { createRequire } from "node:module";
import { dirname, join } from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const root = dirname(dirname(fileURLToPath(import.meta.url)));
const catalog = join(root, "web/apps/catalog");
const generated = join(catalog, "src/gen");
const require = createRequire(pathToFileURL(join(catalog, "package.json")));
const ts = require("typescript");

function moduleURL(file, imports = {}) {
  let source = readFileSync(join(root, file), "utf8");
  for (const [name, url] of Object.entries(imports)) source = source.replace(`from "${name}"`, `from ${JSON.stringify(url)}`);
  const code = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.ESNext, target: ts.ScriptTarget.ES2022 } }).outputText;
  return `data:text/javascript;base64,${Buffer.from(code).toString("base64")}`;
}

const templates = moduleURL("web/packages/build/src/workshop/templates.ts");
const [ui, app, build, zh, query] = await Promise.all([
  import(moduleURL("web/packages/ui/src/catalog.ts")),
  import(moduleURL("web/packages/app/src/catalog.ts")),
  import(moduleURL("web/packages/build/src/catalog.ts", { "./workshop/templates": templates })),
  import(moduleURL("web/packages/ui/src/i18n/zh-CN.ts")),
  import(moduleURL("web/packages/catalog/src/query.ts")),
]);

const previous = JSON.parse(readFileSync(join(generated, "catalog.json"), "utf8"));
const retained = new Map(previous.entries.map((entry) => [entry.id, entry]));
const entries = [...ui.entries, ...app.entries, ...build.entries].map((entry) => {
  const prior = retained.get(entry.id) ?? {};
  return { ...entry, ...Object.fromEntries(["props", "slots", "composed", "relatedTypes"].filter((key) => entry[key] === undefined && prior[key] !== undefined).map((key) => [key, prior[key]])) };
}).sort((a, b) => a.id < b.id ? -1 : a.id > b.id ? 1 : 0);
if (new Set(entries.map((entry) => entry.id)).size !== entries.length) throw new Error("Catalog has duplicate asset IDs");

const dictionary = zh.default;
const phrases = new Set(entries.flatMap((entry) => [entry.name, entry.summary, ...(entry.constraints ?? []), ...(entry.states ?? [])]));
const translations = { "zh-CN": Object.fromEntries([...phrases].filter((phrase) => dictionary[phrase]).sort().map((phrase) => [phrase, dictionary[phrase]])) };
const consumers = {};
const known = new Set(entries.map((entry) => entry.id));
for (const entry of entries) for (const dependency of [...(entry.dependencies ?? []), ...(entry.composed ?? [])]) {
  if (known.has(dependency)) (consumers[dependency] ??= new Set()).add(entry.id);
}
const orderedConsumers = Object.fromEntries(Object.entries(consumers).sort(([a], [b]) => a.localeCompare(b)).map(([id, ids]) => [id, [...ids].sort()]));
const api = previous.api ?? {};
const content = { entries, translations, consumers: orderedConsumers, api };
const revision = `sha256:${createHash("sha256").update(JSON.stringify(content)).digest("hex").slice(0, 16)}`;
const index = { revision, ...content };
writeFileSync(join(generated, "catalog.json"), `${JSON.stringify(index, null, 2)}\n`);

for (const owner of ["@platform/ui", "@platform/app", "@platform/build"]) {
  const slug = owner.slice(owner.lastIndexOf("/") + 1);
  const total = entries.filter((entry) => entry.owner === owner).length;
  const pages = [];
  for (let offset = 0; offset < total;) {
    const file = `${slug}.${offset}.json`;
    const page = query.queryCatalog(index, { owner, offset, limit: 10 });
    if (page.items.length === 0) throw new Error(`Catalog page ${file} is empty`);
    writeFileSync(join(generated, file), `${JSON.stringify(page)}\n`);
    pages.push(file);
    offset = page.nextOffset ?? total;
  }
  for (const file of readdirSync(generated)) if (new RegExp(`^${slug}\\.\\d+\\.json$`).test(file) && !pages.includes(file)) unlinkSync(join(generated, file));
  writeFileSync(join(generated, `${slug}.json`), `${JSON.stringify({ revision, owner, pages, api: api[owner] })}\n`);
}

const loaders = entries.filter((entry) => entry.example).map((entry) =>
  `  ${JSON.stringify(entry.id)}: () => import(${JSON.stringify(`${entry.owner}/catalog/examples`)}).then((module) => ({ default: module.${entry.example} })),`);
writeFileSync(join(generated, "previews.ts"), `import type { ComponentType } from "react";\n\nexport const previewLoaders: Record<string, () => Promise<{ default: ComponentType }>> = {\n${loaders.join("\n")}\n};\n`);
