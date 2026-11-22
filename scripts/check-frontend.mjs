import { readFileSync, readdirSync, statSync } from "node:fs";
import { dirname, join, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "../frontend");
const src = join(root, "src");
const vendored = (file) => file.startsWith("templates/") || file.startsWith("host/gen/") || file === "astryx-themes.js";

const allowedPackages = [
  "@astryxdesign/core",
  "@astryxdesign/theme-neutral",
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
const allowedImports = [/^\.\.?\//, /^react$/, /^react-dom\/client$/, /^@astryxdesign\//, /^@stylexjs\/stylex$/, /^@heroicons\/react\//, /^pdfjs-dist(\/|$)/, /^recharts$/, /^virtual:stylex:/];
const rawElements = { "router.tsx": ["a"] };
const visibleProps = "label|title|description|placeholder|tooltip|heading|subheading|superheading|aria-label|buttonLabel";

function walk(dir) {
  return readdirSync(dir).flatMap((name) => {
    const path = join(dir, name);
    return statSync(path).isDirectory() ? walk(path) : [path];
  });
}

const problems = [];
const report = (file, line, message) => problems.push(`frontend/src/${file}:${line}: ${message}`);

const manifest = JSON.parse(readFileSync(join(root, "package.json"), "utf8"));
for (const name of Object.keys({ ...manifest.dependencies, ...manifest.devDependencies })) {
  if (!allowedPackages.includes(name)) {
    problems.push(`frontend/package.json: dependency "${name}" is not approved. Remove it, or ask the owner and then add it to allowedPackages in scripts/check-frontend.mjs.`);
  }
}

const dictionary = readFileSync(join(src, "i18n.ts"), "utf8");
const valueDictionary = JSON.parse(readFileSync(join(src, "i18n-values.json"), "utf8"));
const translated = new Set([
  ...[...dictionary.matchAll(/^\s+(?:"([^"]+)"|([A-Za-z_]\w*)): "/gm)].map((match) => match[1] ?? match[2]),
  ...Object.keys(valueDictionary),
]);
const needsTranslation = (file, line, text) => {
  if (!translated.has(text)) report(file, line, `"${text}" has no zh-CN entry. Add it to zhCN in src/i18n.ts.`);
};

const menu = new Set();
let menuApp = "";
for (const text of readFileSync(join(src, "apps.ts"), "utf8").split("\n")) {
  const app = text.match(/^ {4}id: "([^"]+)"/);
  if (app) menuApp = app[1];
  const item = text.match(/\{ id: "([^"]+)", label:/);
  if (item) menu.add(`${menuApp}/${item[1]}`);
}

for (const path of walk(src)) {
  const file = relative(src, path);
  if (vendored(file) || !/\.(ts|tsx)$/.test(file) || file.endsWith(".d.ts")) continue;
  const lines = readFileSync(path, "utf8").split("\n");
  const entry = file.match(/^apps\/([^/]+\/[^/]+)\.tsx?$/);
  if (entry && !menu.has(entry[1])) {
    report(file, 1, `"${entry[1]}" is not an entry in src/apps.ts. Name the file after the entry on your task card (apps/<app>/<entry>.ts); put helper files in a subfolder.`);
  }
  if (entry && !lines.some((text) => text.startsWith("export default "))) {
    report(file, 1, "an entry file must default-export its page, for example: export default listPage({ ... }).");
  }
  if (lines.length > 600) report(file, 1, `${lines.length} lines. Keep a file under 600 lines: split it by responsibility.`);
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
