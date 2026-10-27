import { expect, pageUIProfile, runtime, shots, test } from "./kit";

test("a released application keeps its language, grouped navigation and responsive record workspace", async ({ page, builder, operator }, info) => {
  test.setTimeout(120_000);
  const object = await builder.object({ title: "Experience record", fields: [{ name: "note", title: "Specimen note", type: "text" }] });
  const businessText = "Original customer wording";
  await builder.records(object.type, [{ note: businessText }]);
  const workspace = await builder.page({
    title: "Record workspace", object: object.type,
    sections: [
      { id: "records", widget: "table", configVersion: 1, title: "Available specimens", fields: ["note"] },
      { id: "detail", widget: "detail", configVersion: 1, title: "Selected specimen", fields: ["note"] },
      { id: "history", widget: "text", configVersion: 1, title: "Supporting context", text: "REF-001" },
    ],
    document: {
      formatVersion: 2, uiProfile: pageUIProfile, root: "root",
      nodes: {
        root: { kind: "columns", gap: 16, children: ["records", "panel"] },
        records: { kind: "widget", section: "records", size: { weight: 3, minWidth: 420, maxHeight: 640, scroll: "auto" } },
        panel: { kind: "tabs", title: "Specimen context", activeVariable: "contextTab", children: ["detail", "history"], size: { weight: 2, minWidth: 320, maxHeight: 640, scroll: "auto" } },
        detail: { kind: "widget", section: "detail", title: "Current specimen" },
        history: { kind: "widget", section: "history", title: "Reference context" },
      },
      variables: { contextTab: { scope: "page", type: "string", mode: "state", initial: "detail" } },
    },
  });
  const reference = await builder.page({ title: "Reference page", object: object.type });
  const words = {
    "Experience application": "体验应用", "Experience record": "体验记录", "Experience records": "体验记录", "Record workspace": "记录工作区", "Reference page": "参考页面",
    "Current work": "当前工作", "Reference material": "参考资料", "Specimen note": "样本备注", "Available specimens": "可用样本",
    "Selected specimen": "选中样本", "Supporting context": "辅助说明", "Specimen context": "样本上下文", "Current specimen": "当前样本",
    "Reference context": "参考说明",
    [businessText]: "业务原文不可替换为此译文",
  };
  const application = await builder.application({
    title: "Experience application", pages: [workspace.name, reference.name],
    groups: [{ title: "Current work", pages: [workspace.name] }, { title: "Reference material", pages: [reference.name] }],
    languages: { "zh-CN": words },
  });
  const candidate = await builder.candidate([{ kind: "page", id: workspace.id }, { kind: "page", id: reference.id }, { kind: "app", id: application.id }]);
  await builder.decide("build.app.edit", { type: "build.app", id: application.id }, { languages: { "zh-CN": { ...words, "Record workspace": "未发布的草稿名称" } } });
  await builder.activate(candidate);

  const live = await runtime(page, operator, workspace.name);
  const route = `/page?app=build&kind=page&name=${workspace.name}&application=build:${application.name}&instance=main`;
  await operator.open(live, route);
  const me = await operator.read<{ principalId: string; tenantId: string }>("me");
  const selectLanguage = async (language: "English" | "简体中文") => {
    await live.getByRole("button", { name: `${me.principalId} · ${me.tenantId}`, exact: true }).click();
    await live.getByRole("menuitemradio", { name: language, exact: true }).click();
  };
  try {
    const navigation = live.getByRole("navigation", { name: "Main", exact: true });
    await expect(navigation.getByRole("button", { name: "Record workspace", exact: true })).toHaveAttribute("aria-current", "page");
    const work = navigation.locator("details").filter({ has: live.locator("summary").filter({ hasText: /^Current work$/ }) });
    await work.locator("summary").click();
    await expect(work.getByRole("button", { name: "Record workspace", exact: true })).not.toBeVisible();
    await navigation.getByRole("button", { name: "Reference page", exact: true }).click();
    await expect(navigation.getByRole("button", { name: "Reference page", exact: true })).toHaveAttribute("aria-current", "page");
    await operator.open(live, route);
    await expect(work.getByRole("button", { name: "Record workspace", exact: true })).toBeVisible();
    await expect(work.getByRole("button", { name: "Record workspace", exact: true })).toHaveAttribute("aria-current", "page");

    await selectLanguage("简体中文");
    await expect(live.locator("html")).toHaveAttribute("lang", "zh-CN");
    await expect(live.getByRole("button", { name: "切换应用", exact: true })).toHaveText("体验应用");
    const chineseNavigation = live.getByRole("navigation", { name: "主导航", exact: true });
    await expect(chineseNavigation.getByRole("button", { name: "记录工作区", exact: true })).toHaveAttribute("aria-current", "page");
    await expect(chineseNavigation.locator("summary").filter({ hasText: /^当前工作$/ })).toBeVisible();
    await expect(live.getByRole("columnheader", { name: "样本备注", exact: true })).toBeVisible();
    await live.getByRole("row").filter({ has: live.getByRole("cell", { name: businessText, exact: true }) }).click();
    await expect(live.getByRole("heading", { name: "选中样本", exact: true })).toBeVisible();
    await expect(live.getByText(businessText, { exact: true })).toHaveCount(2);
    await live.getByRole("tab", { name: "参考说明", exact: true }).click();
    await expect(live.getByRole("heading", { name: "辅助说明", exact: true })).toBeVisible();
    await expect(live.getByText("REF-001", { exact: true })).toBeVisible();
    await shots(live, live.locator("body"), "application-experience-zh", info);

    await live.setViewportSize({ width: 390, height: 844 });
    await live.getByRole("tab", { name: "当前样本", exact: true }).click();
    await expect(live.getByRole("heading", { name: "选中样本", exact: true })).toBeVisible();
    await expect(live.getByRole("cell", { name: businessText, exact: true })).toBeVisible();
    await live.getByRole("tab", { name: "参考说明", exact: true }).click();
    await expect(live.getByText("REF-001", { exact: true })).toBeVisible();
    await live.setViewportSize({ width: 1280, height: 720 });

    await selectLanguage("English");
    await expect(live.locator("html")).toHaveAttribute("lang", "en");
    await expect(live.getByRole("navigation", { name: "Main", exact: true }).getByRole("button", { name: "Record workspace", exact: true })).toHaveAttribute("aria-current", "page");
    await expect(live.getByRole("columnheader", { name: "Specimen note", exact: true })).toBeVisible();
    await expect(live.getByRole("cell", { name: businessText, exact: true })).toBeVisible();
  } catch (error) {
    await live.screenshot({ path: info.outputPath("application-experience-failure.png") });
    throw error;
  } finally {
    await operator.decide("platform.profile.update", { type: "platform.profile", id: me.principalId }, { language: "en" }, "platform");
    await live.close();
  }
});
