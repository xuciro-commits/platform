import { readFileSync, readdirSync, statSync } from "node:fs";
import { dirname, join, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "../frontend");
const src = join(root, "src");
const vendored = (file) => file.startsWith("templates/") || file.startsWith("host/gen/") || file === "astryx-themes.js";

const allowedPackages = [
  "@astryxdesign/core",
  "@astryxdesign/theme-neutral",
  "@codemirror/autocomplete",
  "@codemirror/lint",
  "@codemirror/state",
  "@codemirror/view",
  "@heroicons/react",
  "@stylexjs/stylex",
  "lucide-react",
  "pdfjs-dist",
  "react",
  "react-dom",
  "recharts",
  "@astryxdesign/cli",
  "@playwright/test",
  "@stylexjs/unplugin",
  "@types/react",
  "@types/react-dom",
  "@vitejs/plugin-react",
  "typescript",
  "vite",
];
const allowedImports = [/^\.\.?\//, /^react$/, /^react-dom\/client$/, /^@astryxdesign\//, /^@codemirror\/(autocomplete|lint|state|view)$/, /^@stylexjs\/stylex$/, /^@heroicons\/react\//, /^pdfjs-dist(\/|$)/, /^recharts$/, /^virtual:stylex:/];
const rawElements = { "router.tsx": ["a"], "pages/ExpressionEditor.tsx": ["div"] };
const visibleProps = "label|title|description|placeholder|tooltip|heading|subheading|superheading|aria-label|buttonLabel|cancelLabel|actionLabel";

function walk(dir) {
  return readdirSync(dir).flatMap((name) => {
    const path = join(dir, name);
    return statSync(path).isDirectory() ? walk(path) : [path];
  });
}

const problems = [];
const report = (file, line, message) => problems.push(`frontend/src/${file}:${line}: ${message}`);

{
  const head = readFileSync(join(src, "pages/icons.ts"), "utf8").split('} from "@heroicons')[0];
  const kebab = (name) => name.replace(/Icon$/, "").replace(/([a-z])([A-Z0-9])/g, "$1-$2").replace(/([A-Z0-9])([A-Z][a-z])/g, "$1-$2").toLowerCase();
  const shown = [...new Set(head.match(/\b[A-Z][A-Za-z0-9]*Icon\b/g) ?? [])].filter((name) => name !== "IconType").map(kebab).sort();
  const checked = [...readFileSync(resolve(root, "../capabilities/server/services/devops/descriptor/icons.go"), "utf8").matchAll(/"([a-z0-9-]+)"/g)].map((m) => m[1]).sort();
  if (shown.join() !== checked.join()) {
    problems.push("frontend/src/pages/icons.ts and capabilities/server/services/devops/descriptor/icons.go list different icons; packages are checked against the Go list");
  }
}

const manifest = JSON.parse(readFileSync(join(root, "package.json"), "utf8"));
for (const name of Object.keys({ ...manifest.dependencies, ...manifest.devDependencies })) {
  if (!allowedPackages.includes(name)) {
    problems.push(`frontend/package.json: dependency "${name}" is not approved. Remove it, or ask the owner and then add it to allowedPackages in scripts/check-frontend.mjs.`);
  }
}

const dictionary = readFileSync(join(src, "i18n.ts"), "utf8");
const i18nDir = join(src, "i18n");
const layerOwner = new Map();
const translated = new Set(
  [...dictionary.matchAll(/^\s+(?:"([^"]+)"|([A-Za-z_]\w*)): "/gm)].map((match) => match[1] ?? match[2]),
);
for (const entry of readdirSync(i18nDir).filter((name) => name.endsWith(".json")).sort()) {
  const layer = JSON.parse(readFileSync(join(i18nDir, entry), "utf8"));
  for (const [key, value] of Object.entries(layer)) {
    if (typeof value !== "string" || value.trim() === "") {
      report(`i18n/${entry}`, 1, `translation for "${key}" is empty.`);
    }
    if (layerOwner.has(key)) {
      report(`i18n/${entry}`, 1, `duplicate i18n key "${key}" already defined in i18n/${layerOwner.get(key)}. Keep each L1 layer disjoint.`);
    } else {
      layerOwner.set(key, entry);
    }
    translated.add(key);
  }
}
const needsTranslation = (file, line, text) => {
  if (!translated.has(text)) report(file, line, `"${text}" has no zh-CN entry. Add it to the appropriate JSON layer under src/i18n/.`);
};

const menu = new Set();
let menuApp = "";
for (const text of readFileSync(join(src, "apps.ts"), "utf8").split("\n")) {
  const app = text.match(/^ {4}id: "([^"]+)"/);
  if (app) menuApp = app[1];
  const item = text.match(/\{ id: "([^"]+)", label:/);
  if (item) menu.add(`${menuApp}/${item[1]}`);
}

for (const entry of menu) {
  const files = ["ts", "tsx"].filter(ext => {
    try { return statSync(join(src, "apps", entry + "." + ext)).isFile(); } catch { return false; }
  });
  if (files.length !== 1) report("apps.ts", 1, `"${entry}" must have exactly one registered page module (found ${files.length}).`);
}

for (const path of walk(src)) {
  const file = relative(src, path);
  if (vendored(file) || !/\.(ts|tsx)$/.test(file) || file.endsWith(".d.ts")) continue;
  const content = readFileSync(path, "utf8");
  const lines = content.split("\n");
  for (const match of content.matchAll(/\b(?:from\s*|import\s*\(?\s*)["'](\.[^"']+)["']/g)) {
    const target = relative(src, resolve(dirname(path), match[1]));
    const line = content.slice(0, match.index).split("\n").length;
    if (/^apps\/entries(?:\.ts)?$/.test(target) && !["main.tsx", "shell/Shell.tsx", "shell/CommandSearch.tsx"].includes(file)) {
      report(file, line, "the entry registry belongs to application assembly; use recordRoutes for record links.");
    }
    if (/^shell\/CommandSearch(?:\.tsx)?$/.test(target) && file !== "shell/Shell.tsx") {
      report(file, line, "use commandScope for page commands instead of importing the command palette.");
    }
    if (file.startsWith("host/") && /^(?:apps(?:\/|\.ts$)|pages\/|semantic\/|shell\/)/.test(target)) {
      report(file, line, "host contracts and transport cannot depend on presentation or application assembly.");
    }
    if (file === "pages/fieldRules.ts" && target !== "host") {
      report(file, line, "field rules depend only on the host contract, not on input components.");
    }
    if (file.startsWith("semantic/") && /^pages\/(?:act|ParameterInput|RecordPeek)(?:\.tsx)?$/.test(target)) {
      report(file, line, "semantic values use host actions and peekScope, not action or record editing screens.");
    }
    if (file === "i18n.ts" && !/^i18n\/[^/]+\.json$/.test(target)) {
      report(file, line, "i18n is a leaf: import only its translation dictionaries and external libraries.");
    }
    if (file !== "i18n.ts" && /^i18n\//.test(target)) {
      report(file, line, "translation dictionaries are private to i18n.ts; use its public API.");
    }
  }
  const entry = file.match(/^apps\/([^/]+\/[^/]+)\.tsx?$/);
  if (entry && !menu.has(entry[1])) {
    report(file, 1, `"${entry[1]}" is not an entry in src/apps.ts. Name the file after the entry on your task card (apps/<app>/<entry>.ts); put helper files in a subfolder.`);
  }
  if (entry && !lines.some((text) => text.startsWith("export default "))) {
    report(file, 1, "an entry file must default-export its page, for example: export default listPage({ ... }).");
  }
  if (lines.length > 600) report(file, 1, `${lines.length} lines. Keep a file under 600 lines: split it by responsibility.`);
  if (file === "main.tsx" && (!content.includes("@astryxdesign/core/locales/zh-CN.generated.js") || !/<InternationalizationProvider[^>]*\bmessages=/.test(content))) {
    report(file, 1, "main.tsx must wire @astryxdesign/core/locales/zh-CN.generated.js into <InternationalizationProvider messages={...}> (L0 i18n layer).");
  }
  lines.forEach((text, index) => {
    const line = index + 1;
    const trimmed = text.trim();
    for (const match of text.matchAll(/(?:from|import)\s*\(?\s*"([^"]+)"/g)) {
      const target = match[1];
      if (!allowedImports.some((pattern) => pattern.test(target))) {
        report(file, line, `import "${target}" is not allowed. Use Astryx, heroicons or a file under frontend/src; never import from web/ or a new package.`);
      } else if (target.startsWith(".") && relative(src, resolve(dirname(path), target)).startsWith("..")) {
        report(file, line, `import "${target}" leaves frontend/src. Only import files inside frontend/src.`);
      }
    }
    if (/^\/\/(?!\/ <reference)/.test(trimmed) || /^\/\*/.test(trimmed) || /^\*/.test(trimmed) || /\{\/\*/.test(text)) {
      report(file, line, "comment found. Delete it: this repository has no code comments; names carry the meaning.");
    }
    if (file !== "i18n.ts" && /[一-鿿]/.test(text)) {
      report(file, line, "Chinese text in source. Write the English text, pass it through t(), and add the translation to zhCN in src/i18n.ts.");
    }
    if (/\bfetch\s*\(|XMLHttpRequest/.test(text) && !file.startsWith("host/")) {
      report(file, line, "direct network call. Read and write only through the functions in src/host/.");
    }
    if (/\bstyle=\{\{|\bclassName=/.test(text)) {
      report(file, line, "inline style or className. Use the component's props; if that is not enough, stop and report instead of styling by hand.");
    }
    if (/["'`]#[0-9a-fA-F]{3,8}["'`]|\brgba?\(|\bhsla?\(|["'`]\d+(?:px|rem|em)["'`]/.test(text)) {
      report(file, line, "hard-coded colour or size. Use a component prop or a theme token.");
    }
    if (file.endsWith(".tsx")) {
      for (const match of text.matchAll(/(?<![\w)\]])<([a-z][a-z0-9]*)(?=[\s>/])/g)) {
        if (!(rawElements[file] ?? []).includes(match[1])) {
          report(file, line, `raw <${match[1]}> element. Use an Astryx component (run: pnpm astryx search "<what you need>").`);
        }
      }
      for (const match of text.matchAll(new RegExp(`\\b(?:${visibleProps})="([^"]*[A-Za-z][^"]*)"`, "g"))) {
        report(file, line, `visible text "${match[1]}" is not translated. Write it as {t("${match[1]}")}.`);
      }
      const between = text.match(/>([^<>{}=()]*[A-Za-z][^<>{}=()]*)</);
      if (between && between[1].trim()) report(file, line, `visible text "${between[1].trim()}" is not translated. Write it as {t("${between[1].trim()}")}.`);
    }
    for (const match of text.matchAll(/\bt\("([^"]+)"\)/g)) needsTranslation(file, line, match[1]);
    if (/\bt\(`[^`]*\$\{/.test(text)) {
      report(file, line, "dynamic template literal inside t(`...`). Use a static pattern string like t(\"Open {app}\").replace(\"{app}\", value).");
    }
    if (file === "apps.ts" || file.startsWith("apps/") || file.startsWith("pages/")) {
      for (const match of text.matchAll(/\b(?:label|title|header|about): "([^"]+)"/g)) needsTranslation(file, line, match[1]);
    }
  });
}

if (problems.length > 0) {
  console.error(problems.join("\n"));
  console.error(`\nfrontend: ${problems.length} problem(s). Fix each one as its message says; do not edit scripts/check-frontend.mjs to make it pass.`);
  process.exit(1);
}
console.log(`frontend: ${translated.size} translations, source holds the rules`);
