import { describe, it, expect } from "vitest";
import { setLanguage } from "@platform/ui";
import catalogApp, { catalogNavigation } from "./app";

describe("Platform Catalog Navigation & App Configuration", () => {
  it("translates product title to 平台资产库 in Chinese and Platform Catalog in English", () => {
    setLanguage("zh-CN");
    expect(catalogApp.title).toBe("平台资产库");

    setLanguage("en");
    expect(catalogApp.title).toBe("Platform Catalog");
  });

  it("provides 4 Apple Music-style grouped sections without numeric prefixes", () => {
    setLanguage("zh-CN");
    const sections = catalogNavigation();
    expect(sections).toHaveLength(4);

    const labels = sections.map((s) => s.label);
    expect(labels).toEqual(["组件库", "代码沙箱", "页面组装", "发布与维护"]);

    // Ensure no 1, 2, 3, 4 numeric prefixes
    for (const section of sections) {
      expect(section.label).not.toMatch(/^[0-9一二三四1-4]/);
      for (const item of section.items) {
        expect(item.label).not.toMatch(/^[0-9一二三四1-4]/);
        expect(item.icon).toBeDefined();
        expect(item.route).toBeDefined();
        expect(item.route.view).toBeTruthy();
      }
    }
  });

  it("translates properly in English as well", () => {
    setLanguage("en");
    const sections = catalogNavigation();
    expect(sections).toHaveLength(4);

    const labels = sections.map((s) => s.label);
    expect(labels).toEqual(["Components", "Sandbox", "Page builder", "Governance"]);
  });

  it("registers all required views in catalogApp", () => {
    const viewIds = catalogApp.views.map((v) => v.id);
    expect(viewIds).toContain("catalog");
    expect(viewIds).toContain("sandbox");
    expect(viewIds).toContain("page-builder");
    expect(viewIds).toContain("governance");
  });

  it("renders Sandbox view and provides inspirations including industrial instrument prototype", () => {
    setLanguage("zh-CN");
    const sandbox = catalogApp.views.find((v) => v.id === "sandbox");
    expect(sandbox).toBeDefined();
    expect(sandbox?.title({ tab: "inspirations" })).toBe("灵感收藏");
    expect(sandbox?.title({ tab: "playground" })).toBe("在线调试");
  });
});
