import { test } from "node:test";
import assert from "node:assert/strict";
import { inspectImports, publicExports, readDictionary, validateOwner } from "./catalog-check.mjs";

test("public coverage distinguishes type-only exports and namespace children", async () => {
  const files = new Map([
    ["/test/index.ts", 'export { Button, type Props } from "./button"; export type { Model } from "./model"; export * as field from "./fields"; export function useValue() {}'],
    ["/test/fields.ts", "export const text = () => {}; export const date = () => {}; export type Field = string;"],
  ]);
  const exports = await publicExports("/test/index.ts", async (file) => {
    if (!files.has(file)) throw new Error("missing");
    return files.get(file);
  });
  assert.deepEqual([...exports].sort(), ["Button", "field.date", "field.text", "useValue"]);
  const dictionary = { Button: "按钮", "Run an action": "执行动作" };
  const entries = [{ id: "ui/button", owner: "@platform/ui", name: "Button", summary: "Run an action", layer: 1, authority: "api", maturity: "recommended", scope: "platform", uses: ["code"], tags: [], source: "button.tsx", exports: ["Button"], example: "Buttons" }];
  const errors = validateOwner("ui", entries, ["useValue"], exports, new Set(["Buttons"]), dictionary, new Set());
  assert.ok(errors.some((error) => error.includes("field.text is not registered")));
  assert.ok(errors.some((error) => error.includes("field.date is not registered")));
  assert.ok(!errors.some((error) => error.includes("Props") || error.includes("Model")));
});

test("translation and private imports fail with actionable asset or file context", () => {
  const dictionary = readDictionary("i18n.ts", 'register("zh-CN", { "Open {name}": "打开 {wrong}" });');
  const entry = { id: "ui/button", owner: "@platform/ui", name: "Open {name}", summary: "Missing translation", layer: 1, authority: "api", maturity: "recommended", scope: "platform", uses: ["code"], tags: [], source: "button.tsx", example: "MissingExample" };
  const errors = validateOwner("ui", [entry], [], new Set(), new Set(), dictionary, new Set());
  assert.ok(errors.some((error) => error.includes("placeholder mismatch")));
  assert.ok(errors.some((error) => error.includes("missing zh-CN text")));
  assert.ok(errors.some((error) => error.includes("missing example")));
  const inspected = inspectImports("app.tsx", 'import { Button } from "@platform/ui"; import { privateThing } from "@platform/ui/src/private";', new Map([["ui", { name: "@platform/ui", exports: { ".": "./src/index.ts", "./styles.css": "./src/styles.css" } }]]), [{ ...entry, exports: ["Button"] }]);
  assert.deepEqual(inspected.consumers, ["ui/button"]);
  assert.deepEqual(inspected.errors, ["app.tsx: private import @platform/ui/src/private"]);
});
