import { expect, test } from "@playwright/test";
import { decide, fresh, open } from "./host";

for (const fixture of [
  { industry: "hospitality", baseURL: "http://127.0.0.1:18496", builder: "manager", member: "desk-1", title: "Guest advice review" },
  { industry: "manufacturing", baseURL: "http://127.0.0.1:18497", builder: "supervisor", member: "op-l1", title: "Inspection advice review" },
]) {
  test.describe(fixture.industry, () => {
    test.use({ baseURL: fixture.baseURL });
    test("compose a pinned function, wait and human review in a native workflow", async ({ page, request }, testInfo) => {
      const name = fresh("fnflow").replace(/[^a-z0-9]/gi, "").toLowerCase(), type = `build.${name}`;
      const objectID = fresh("OBJ"), functionID = fresh("FN");
      await decide(request, fixture.builder, "build", "build.object.create", { type: "build.object", id: objectID }, {
        name, title: fixture.title, fields: [{ name: "note", title: "Note", type: "text" }],
        states: [{ name: "open", title: "Open" }, { name: "done", title: "Done" }],
        actions: [{ name: "close", title: "Close", from: ["open"], to: "done" }],
      });
      await decide(request, fixture.builder, "build", "build.object.publish", { type: "build.object", id: objectID }, {});
      await decide(request, fixture.builder, "build", "build.function.create", { type: "build.function", id: functionID }, {
        name, title: fixture.title, description: "Advice for review", object: type, fields: ["note"], roles: ["builder", "user"],
        instructions: "Summarise the note", maxInputBytes: 2048, maxTokens: 128, maxOutputBytes: 2048,
        output: [{ name: "summary", type: "string", required: true, description: "Summary" }],
      });
      await decide(request, fixture.builder, "build", "build.function.publish", { type: "build.function", id: functionID }, {});
      await decide(request, fixture.builder, "build", "build.function.edit", { type: "build.function", id: functionID }, { instructions: "New instructions" });
      await decide(request, fixture.builder, "build", "build.function.publish", { type: "build.function", id: functionID }, {});
      await open(page, fixture.builder, "/workflow");
      await page.getByRole("button", { name: "New workflow", exact: true }).click();
      const properties = page.getByRole("region", { name: "Workflow properties" });
      await properties.getByRole("textbox", { name: "Workflow name", exact: true }).fill(name);
      await properties.getByRole("textbox", { name: "Workflow title" }).fill(fixture.title);
      await properties.getByRole("combobox", { name: "Source object" }).selectOption(type);
      await page.getByRole("button", { name: "Add AI function", exact: true }).focus();
      await page.keyboard.press("Enter");
      await properties.getByRole("combobox", { name: "Published function version" }).selectOption(`${name}:1`);
      await page.getByRole("button", { name: "Add human task" }).click();
      await page.getByRole("button", { name: "Add object action" }).click();
      await page.getByRole("button", { name: "2. Human task", exact: true }).click();
      await properties.getByRole("combobox", { name: "After answer approve" }).selectOption("action");
      await page.getByRole("button", { name: "1. Call AI function", exact: true }).click();
      await properties.getByRole("combobox", { name: "Default next step" }).selectOption("review");
      await page.getByRole("button", { name: "Save workflow", exact: true }).click();
      await expect(page.getByRole("button", { name: "Test workflow", exact: true })).toBeEnabled();
      const inventory = await (await request.get("/v1/records/build.process?limit=500", { headers: { Authorization: `Bearer ${fixture.builder}` } })).json();
      const workflow = inventory.records.find((p: { name: string }) => p.name === name);
      expect(workflow.steps[0].function).toEqual({ name, version: 1 });
      await page.getByRole("button", { name: "1. Call AI function", exact: true }).click();
      await page.screenshot({ path: testInfo.outputPath("function-workflow-desktop.png"), fullPage: true, animations: "disabled" });
      await page.getByRole("button", { name: "Test workflow", exact: true }).click();
      await page.getByRole("textbox", { name: "Member ID (empty: you)", exact: true }).fill(fixture.member);
      const first = page.getByRole("group", { name: "Test step 1", exact: true });
      await first.getByRole("textbox", { name: "Test inputs (JSON)" }).fill('{"note":"Please review"}');
      await first.getByRole("checkbox", { name: "Supply a fixed model answer" }).check();
      await first.getByRole("textbox", { name: "Provider answer" }).fill('{"summary":"Review requested"}');
      const second = page.getByRole("group", { name: "Test step 2", exact: true });
      await second.getByRole("combobox", { name: "Test step kind" }).selectOption("clock");
      await page.getByRole("button", { name: "Add a test step" }).click();
      const third = page.getByRole("group", { name: "Test step 3", exact: true });
      await third.getByRole("combobox", { name: "Test step kind" }).selectOption("answer");
      await third.getByRole("textbox", { name: "Test record ID" }).fill("TEST-1");
      await third.getByRole("spinbutton", { name: "Advance clock (seconds)" }).fill("2");
      await page.getByRole("textbox", { name: "Test plan name", exact: true }).fill(fixture.title);
      await page.getByRole("button", { name: "Save test plan", exact: true }).click();
      await expect(page.getByText("Test plan saved. Reload it to repeat these fixed inputs.", { exact: true })).toBeVisible();
      await page.getByRole("button", { name: "Reload saved plan", exact: true }).click();
      await page.getByRole("button", { name: "Run isolated test", exact: true }).click();
      await expect(page.getByText("All expected outcomes matched.", { exact: true })).toBeVisible();
      await expect(page.getByText(`${name} · Version 1 · Ready`, { exact: true }).first()).toBeVisible();
      await expect(page.locator("pre").filter({ hasText: '"state": "done"' })).toHaveCount(1);
      await open(page, fixture.builder, `/workflow?id=${workflow.id}`);
      await page.getByRole("button", { name: "Publish workflow", exact: true }).click();
      await expect(page.getByRole("status").filter({ hasText: "Installed workflow version 1." })).toBeVisible();
      await page.reload();
      await page.getByRole("button", { name: "1. Call AI function", exact: true }).click();
      await expect(properties.getByRole("combobox", { name: "Published function version" })).toHaveValue(`${name}:1`);
      await page.setViewportSize({ width: 390, height: 780 });
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBeTruthy();
      await properties.getByRole("combobox", { name: "Published function version" }).scrollIntoViewIfNeeded();
      await page.screenshot({ path: testInfo.outputPath("function-workflow-narrow.png"), fullPage: true, animations: "disabled" });
      const chinese = await page.context().newPage();
      await chinese.addInitScript(() => localStorage.setItem("platform.language", "zh-CN"));
      await open(chinese, fixture.builder, `/workflow?id=${workflow.id}`);
      await chinese.getByRole("button", { name: "1. Call AI function", exact: true }).click();
      await expect(chinese.getByRole("combobox", { name: "已发布函数版本" })).toHaveValue(`${name}:1`);
      await chinese.getByRole("combobox", { name: "已发布函数版本" }).scrollIntoViewIfNeeded();
      await chinese.screenshot({ path: testInfo.outputPath("function-workflow-chinese.png"), fullPage: true, animations: "disabled" });
      await chinese.close();
    });
  });
}
