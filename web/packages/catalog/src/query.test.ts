import { describe, expect, it } from "vitest";
import { batchCatalog, getCatalogEntry, MAX_RESPONSE_BYTES, queryCatalog, responseBytes } from "./query";
import type { CatalogEntry, CatalogIndex } from "./types";

const entry = (id: string, extra: Partial<CatalogEntry> = {}): CatalogEntry => ({
  id, owner: "ui", name: "Button", summary: "Trigger a supported action", layer: 1,
  authority: "api", maturity: "recommended", scope: "platform", uses: ["code"],
  tags: ["action"], source: "web/packages/ui/src/primitives/button.tsx", ...extra,
});
const index = (entries: CatalogEntry[]): CatalogIndex => ({
  revision: "test-build", entries, translations: { "zh-CN": { Button: "按钮", "Trigger a supported action": "触发支持的动作" } },
});

describe("offline Catalog discovery", () => {
  it("finds Chinese and English terms without changing identity or usage eligibility", () => {
    const result = queryCatalog(index([entry("ui/button"), entry("ui/old", { maturity: "deprecated" })]), { query: "按钮", language: "zh-CN", maturity: "recommended", use: "code" });
    expect(result.items).toHaveLength(1);
    expect(result.items[0]).toMatchObject({ id: "ui/button", name: "按钮", uses: ["code"], availability: "reference", authority: "api" });
    expect(queryCatalog(index([entry("ui/button")]), { use: "widget" }).items).toHaveLength(0);
  });

  it("pages summaries and batch details within item and byte budgets", () => {
    const data = index(Array.from({ length: 30 }, (_, i) => entry(`ui/button-${String(i).padStart(2, "0")}`, { summary: "按钮".repeat(500) })));
    const first = queryCatalog(data, { limit: 100 });
    expect(first.items.length).toBeGreaterThan(0);
    expect(first.items.length).toBeLessThanOrEqual(10);
    expect(responseBytes(first)).toBeLessThanOrEqual(MAX_RESPONSE_BYTES);
    const second = queryCatalog(data, { offset: first.nextOffset });
    expect(second.items[0]?.id).not.toBe(first.items[0]?.id);
    const batch = batchCatalog(data, data.entries.map((entry) => entry.id), { fields: ["source"] });
    expect(responseBytes(batch)).toBeLessThanOrEqual(MAX_RESPONSE_BYTES);
    expect(batch.nextOffset).toBeGreaterThan(0);
  });

  it("pages optional documents and never exposes whole snippets by default", () => {
    const data = index([entry("ui/button", { snippet: "x".repeat(20_000), exports: ["Button"], type: "ActionButtonProps" })]);
    data.api = { ui: { source: "public.ts", symbols: ["useValue"], types: Array.from({ length: 30 }, (_, i) => `Type${i}`) } };
    expect(queryCatalog(data, { query: "ActionButtonProps" }).items[0]?.id).toBe("ui/button");
    expect(getCatalogEntry(data, "ui/button")?.entry.snippet).toBeUndefined();
    const detail = getCatalogEntry(data, "ui/button", { fields: ["snippet"] });
    expect(detail?.entry.snippet).toHaveLength(1000);
    expect(detail?.nextFieldOffsets).toEqual({ snippet: 1000 });
    expect(getCatalogEntry(data, "ui/button", { fields: ["snippet"], offset: 1000 })?.nextFieldOffsets).toEqual({ snippet: 2000 });
    expect(responseBytes(detail)).toBeLessThanOrEqual(MAX_RESPONSE_BYTES);
    expect(getCatalogEntry(data, "ui/button", { fields: ["api"] })?.entry.api?.types).toHaveLength(10);
    expect(getCatalogEntry(data, "ui/button", { fields: ["api"] })?.nextFieldOffsets).toEqual({ api: 10 });
    expect(getCatalogEntry(data, "missing")).toBeUndefined();
  });
});
