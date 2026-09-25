/// <reference types="node" />
import { readdirSync, readFileSync, statSync } from "node:fs";
import { join } from "node:path";
import { expect, test } from "vitest";
import { language, register, t } from "./i18n";

test("English is the source and the fallback; placeholders take values", () => {
  expect(language()).toBe("en"); // jsdom's navigator speaks en-US
  register("en", { "Hello {name}": "Hi {name}" });
  expect(t("Hello {name}", { name: "Ana" })).toBe("Hi Ana");
  expect(t("Not translated {n}", { n: 3 })).toBe("Not translated 3");
  expect(t("Keeps {unknown}", {})).toBe("Keeps {unknown}");
});

// Every text a package passes to t() has a Simplified Chinese translation in
// that package's dictionary, or in the kit's (ADR-0023 6a).
test("every translated text of every package reads in Chinese", () => {
  const web = join(__dirname, "../../..");
  const files = (dir: string): string[] => readdirSync(dir).flatMap((f: string) => {
    const p = join(dir, f);
    return statSync(p).isDirectory() ? (f === "gen" || f === "node_modules" ? [] : files(p)) : /\.tsx?$/.test(f) && !/\.test\./.test(f) ? [p] : [];
  });
  const keys = (file: string) => new Set([...readFileSync(file, "utf8").matchAll(/^\s*("(?:[^"\\]|\\.)*"): /gm)].map((m) => JSON.parse(m[1]!) as string));
  const kit = keys(join(web, "packages/ui/src/i18n/zh-CN.ts"));
  const missing: string[] = [];
  for (const src of ["packages/ui/src", "packages/app/src", "packages/platform/src", "packages/crm/src", "packages/mes/src", "packages/hr/src",
    "packages/helpdesk/src", "packages/hotel/src", "packages/lodging/src", "apps/workspace/src"]) {
    const own = src === "packages/ui/src" ? kit : keys(join(web, src, "i18n.ts"));
    for (const file of files(join(web, src))) {
      for (const m of readFileSync(file, "utf8").matchAll(/\bt\(("(?:[^"\\]|\\.)*")/g)) {
        const text = JSON.parse(m[1]!) as string;
        if (!own.has(text) && !kit.has(text)) missing.push(`${src}: ${text}`);
      }
    }
  }
  expect(missing).toEqual([]);
});
