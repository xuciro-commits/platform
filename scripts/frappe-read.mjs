import { existsSync, readFileSync, readdirSync, statSync } from "node:fs";
import { basename, dirname, join } from "node:path";

const args = process.argv.slice(2);
const listTypes = args.includes("--types");
const [appDir, ...knownDirs] = args.filter((arg) => !arg.startsWith("--"));
if (!appDir) {
  console.error("usage: node scripts/frappe-read.mjs <app checkout> [other checkouts whose DocTypes links may point to] [--types]");
  process.exit(2);
}

const mapped = {
  text: ["Data", "Small Text", "Read Only", "Barcode", "Color", "Phone", "Autocomplete", "Icon"],
  longtext: ["Text", "Long Text", "Text Editor", "Code", "Markdown Editor", "HTML Editor"],
  integer: ["Int"],
  decimal: ["Float", "Percent", "Rating"],
  money: ["Currency"],
  boolean: ["Check"],
  date: ["Date"],
  datetime: ["Datetime"],
  choice: ["Select"],
  reference: ["Link"],
  lines: ["Table"],
  references: ["Table MultiSelect"],
  attachment: ["Attach", "Attach Image", "Signature"],
  json: ["JSON", "Geolocation"],
};
const target = Object.fromEntries(Object.entries(mapped).flatMap(([type, sources]) => sources.map((source) => [source, type])));
const layout = new Set(["Section Break", "Column Break", "Tab Break", "Fold"]);
const display = new Set(["HTML", "Button", "Heading", "Image"]);
const conditionKeys = ["depends_on", "mandatory_depends_on", "read_only_depends_on", "collapsible_depends_on"];
const thinController = 3;

function walk(dir) {
  return readdirSync(dir).flatMap((name) => {
    if (name === "node_modules" || name === ".git") return [];
    const path = join(dir, name);
    return statSync(path).isDirectory() ? walk(path) : [path];
  });
}

function docTypes(dir) {
  return walk(dir)
    .filter((path) => path.endsWith(".json") && basename(dirname(dirname(path))) === "doctype" && basename(path, ".json") === basename(dirname(path)))
    .map((path) => ({ path, body: JSON.parse(readFileSync(path, "utf8")) }))
    .filter(({ body }) => body.doctype === "DocType");
}

function behaviourLines(path) {
  if (!existsSync(path)) return 0;
  const source = readFileSync(path, "utf8").replace(/# begin: auto-generated types[\s\S]*?# end: auto-generated types/, "");
  let inImport = false;
  let inDocstring = false;
  return source.split("\n").filter((raw) => {
    const line = raw.trim();
    if (inDocstring) {
      if (line.includes('"""')) inDocstring = false;
      return false;
    }
    if (inImport) {
      if (line.includes(")")) inImport = false;
      return false;
    }
    if (line.startsWith('"""')) {
      inDocstring = !line.slice(3).includes('"""');
      return false;
    }
    if (/^(from|import)\s/.test(line)) {
      inImport = line.includes("(") && !line.includes(")");
      return false;
    }
    return line !== "" && !line.startsWith("#") && !line.startsWith("class ") && line !== "pass";
  }).length;
}

function scriptLines(path) {
  if (!existsSync(path)) return 0;
  return readFileSync(path, "utf8").split("\n").filter((raw) => raw.trim() !== "" && !raw.trim().startsWith("//")).length;
}

function conditionKind(text) {
  const source = String(text).trim();
  if (!source.startsWith("eval:")) return "when";
  const body = source.slice(5).trim().replace(/;$/, "");
  const literal = `(?:"[^"]*"|'[^']*'|\\d+)`;
  const list = `\\[\\s*${literal}(?:\\s*,\\s*${literal})*\\s*\\]`;
  const single = [`^doc\\.\\w+\\s*={2,3}\\s*${literal}$`, `^in_list\\(\\s*${list}\\s*,\\s*doc\\.\\w+\\s*\\)$`, `^${list}\\.includes\\(\\s*doc\\.\\w+\\s*\\)$`, `^doc\\.\\w+$`];
  if (single.some((pattern) => new RegExp(pattern).test(body))) return "when";
  const rest = body
    .replace(/"[^"]*"|'[^']*'/g, "")
    .replace(/in_list|\.includes/g, "")
    .replace(/doc\.\w+/g, "")
    .replace(/[\d\s()[\],!=<>&|.+\-*/]/g, "");
  return rest === "" ? "expression" : "code";
}

const count = (tally, key, by = 1) => tally.set(key, (tally.get(key) ?? 0) + by);
const ranked = (tally, limit = 12) =>
  [...tally.entries()]
    .sort((a, b) => b[1] - a[1])
    .slice(0, limit)
    .map(([key, value]) => `${key} ${value}`)
    .join(", ") || "none";

const own = docTypes(appDir);
const ownNames = new Set(own.map(({ body }) => body.name));
const knownNames = new Set(knownDirs.flatMap((dir) => docTypes(dir).map(({ body }) => body.name)));

const totals = { fields: 0, data: 0, layout: 0, display: 0, unmapped: 0, linkOwn: 0, linkKnown: 0, linkUnknown: 0, permLevel: 0, defaults: 0, unique: 0, fetched: 0, clientScripts: 0 };
const kinds = new Map();
const unmappedTypes = new Map();
const unknownLinks = new Map();
const conditions = new Map();
const naming = new Map();
const verdicts = new Map();
const roles = new Set();
const lines = [];

for (const { path, body } of own) {
  const fields = body.fields ?? [];
  const unmapped = [];
  let codeConditions = 0;
  for (const field of fields) {
    totals.fields += 1;
    if (layout.has(field.fieldtype)) totals.layout += 1;
    else if (display.has(field.fieldtype)) totals.display += 1;
    else {
      totals.data += 1;
      if (!target[field.fieldtype]) {
        totals.unmapped += 1;
        unmapped.push(field.fieldtype);
        count(unmappedTypes, field.fieldtype);
      }
      if (["Link", "Table", "Table MultiSelect"].includes(field.fieldtype)) {
        if (ownNames.has(field.options)) totals.linkOwn += 1;
        else if (knownNames.has(field.options)) totals.linkKnown += 1;
        else {
          totals.linkUnknown += 1;
          count(unknownLinks, field.options);
        }
      }
      if (field.default !== undefined && field.default !== "") totals.defaults += 1;
      if (field.unique) totals.unique += 1;
      if (field.fetch_from) totals.fetched += 1;
      if (Number(field.permlevel) > 0) totals.permLevel += 1;
    }
    for (const key of conditionKeys) {
      if (!field[key]) continue;
      const kind = conditionKind(field[key]);
      count(conditions, kind);
      if (kind === "code") codeConditions += 1;
    }
  }
  for (const permission of body.permissions ?? []) roles.add(permission.role);
  const kind = body.istable ? "detail lines" : body.issingle ? "single settings record" : body.is_submittable ? "submittable" : body.is_tree ? "tree" : "plain";
  count(kinds, kind);
  count(naming, body.naming_rule || (body.autoname ? String(body.autoname).split(":")[0] : "default"));
  const controller = behaviourLines(path.replace(/\.json$/, ".py"));
  const script = scriptLines(path.replace(/\.json$/, ".js"));
  if (script > 0) totals.clientScripts += 1;
  const verdict = unmapped.length > 0 ? "has a field type with no mapping" : controller > thinController ? "structure converts, behaviour to rebuild" : "converts as is";
  count(verdicts, verdict);
  lines.push(`${body.module}\t${body.name}\t${kind}\tdata fields ${fields.filter((field) => !layout.has(field.fieldtype) && !display.has(field.fieldtype)).length}\tunmapped ${unmapped.join("|") || "-"}\tcode conditions ${codeConditions}\tcontroller ${controller}\tclient script ${script}\t${verdict}`);
}

if (listTypes) console.log(lines.sort().join("\n"));
console.log(`app: ${appDir}`);
console.log(`doctypes: ${own.length} (${ranked(kinds)})`);
console.log(`verdicts: ${ranked(verdicts)}`);
console.log(`fields: ${totals.fields} = data ${totals.data} + layout ${totals.layout} + display only ${totals.display}`);
console.log(`data fields with no mapping: ${totals.unmapped} (${ranked(unmappedTypes)})`);
console.log(`references: inside the app ${totals.linkOwn}, in the other checkouts ${totals.linkKnown}, unresolved ${totals.linkUnknown} (${ranked(unknownLinks, 8)})`);
console.log(`conditions: ${ranked(conditions)}`);
console.log(`needs from the package format: defaults ${totals.defaults}, unique ${totals.unique}, fetched from a reference ${totals.fetched}, field permission levels ${totals.permLevel}`);
console.log(`naming: ${ranked(naming)}`);
console.log(`roles: ${roles.size}; client scripts with content: ${totals.clientScripts}`);
