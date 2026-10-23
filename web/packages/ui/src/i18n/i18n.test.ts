/// <reference types="node" />
import { existsSync, readdirSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";
import { createRequire } from "node:module";
import { expect, test } from "vitest";
import { language, register, t } from ".";

test("English is the source and the fallback; placeholders take values", () => {
  expect(language()).toBe("en"); // jsdom's navigator speaks en-US
  register("en", { "Hello {name}": "Hi {name}" });
  expect(t("Hello {name}", { name: "Ana" })).toBe("Hi Ana");
  expect(t("Not translated {n}", { n: 3 })).toBe("Not translated 3");
  expect(t("Keeps {unknown}", {})).toBe("Keeps {unknown}");
});

// Every text a package passes to t() has a Simplified Chinese translation in
// that package's dictionary, or its declared dependencies'.
test("every translated text of every package reads in Chinese", () => {
  const web = join(__dirname, "../../../..");
  const { readDictionary } = createRequire(import.meta.url)(join(web, "../scripts/catalog-check.mjs")) as { readDictionary: (file: string, text: string) => Record<string, string> };
  const files = (dir: string): string[] => readdirSync(dir).flatMap((f: string) => {
    const p = join(dir, f);
    return statSync(p).isDirectory() ? (f === "gen" || f === "node_modules" ? [] : files(p)) : /\.tsx?$/.test(f) && !/\.test\./.test(f) ? [p] : [];
  });
  const packages = ["packages", "apps"].flatMap(dir => readdirSync(join(web, dir)).map(name => join(web, dir, name)))
    .filter(dir => existsSync(join(dir, "package.json")))
    .map(dir => ({ dir, ...JSON.parse(readFileSync(join(dir, "package.json"), "utf8")) as { name: string; dependencies?: Record<string, string> } }));
  const byName = new Map(packages.map(pkg => [pkg.name, pkg]));
  const keys = (file: string) => Object.keys(readDictionary(file, readFileSync(file, "utf8")));
  const available = (name: string, visited = new Set<string>()): string[] => {
    const pkg = byName.get(name);
    if (!pkg || visited.has(name)) return [];
    visited.add(name);
    const file = join(pkg.dir, pkg.name === "@platform/ui" ? "src/i18n/zh-CN.ts" : "src/i18n.ts");
    return [...(existsSync(file) ? keys(file) : []), ...Object.keys(pkg.dependencies ?? {}).flatMap(dependency => available(dependency, visited))];
  };
  const missing: string[] = [];
  // The kit, and every package or app with its own dictionary, a new one included.
  const sources = ["packages", "apps"].flatMap((d) => readdirSync(join(web, d)).map((p) => `${d}/${p}/src`))
    .filter((src) => src === "packages/ui/src" || existsSync(join(web, src, "i18n.ts")));
  expect(sources).toEqual(expect.arrayContaining([
    "packages/ui/src", "packages/app/src", "packages/build/src",
    "packages/platform/src", "apps/workspace/src", "apps/catalog/src",
  ]));
  for (const src of sources) {
    const pkg = packages.find(pkg => join(pkg.dir, "src") === join(web, src))!;
    const translations = new Set(available(pkg.name));
    for (const file of files(join(web, src))) {
      for (const m of readFileSync(file, "utf8").matchAll(/\bt\(("(?:[^"\\]|\\.)*")/g)) {
        const text = JSON.parse(m[1]!) as string;
        if (!translations.has(text)) missing.push(`${src}: ${text}`);
      }
    }
  }
  expect(missing).toEqual([]);
});
