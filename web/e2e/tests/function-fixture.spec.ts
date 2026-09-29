import { expect, test } from "@playwright/test";
import { decide, fresh, open } from "./host";

for (const fixture of [
  { industry: "hospitality", baseURL: "http://127.0.0.1:18496", builder: "manager", operator: "desk", member: "desk-1", title: "Guest service advice" },
  { industry: "manufacturing", baseURL: "http://127.0.0.1:18497", builder: "supervisor", operator: "operator-l1", member: "op-l1", title: "Inspection advice" },
]) {
  test.describe(fixture.industry, () => {
    test.use({ baseURL: fixture.baseURL });
    test("fixed function cases preserve candidate, answers and identity", async ({ page, request }, testInfo) => {
      const objectID = fresh("OBJ"), functionID = fresh("FN");
      const name = fresh("advice").replace(/[^a-z0-9]/gi, "").toLowerCase(), type = `build.${name}`;
      await decide(request, fixture.builder, "build", "build.object.create", { type: "build.object", id: objectID }, {
        name, title: fixture.title, fields: [{ name: "note", title: "Note", type: "text" }],
      });
      await decide(request, fixture.builder, "build", "build.object.publish", { type: "build.object", id: objectID }, {});
      await decide(request, fixture.builder, "build", "build.function.create", { type: "build.function", id: functionID }, {
        name, title: fixture.title, description: "A suggestion for human review", object: type, fields: ["note"],
        instructions: "Summarize only the source note", roles: ["builder", "user"],
        output: [{ name: "summary", type: "string", required: true, description: "Source summary" }],
        maxInputBytes: 1024, maxOutputBytes: 256, maxTokens: 64,
      });
      await open(page, fixture.builder, "/candidate-test");
      await page.getByRole("combobox", { name: "Candidate kind" }).selectOption("function");
      await page.getByRole("combobox", { name: "Saved function draft" }).selectOption(functionID);
      await page.getByRole("textbox", { name: "Test plan name" }).fill(fixture.title);
      await page.getByRole("textbox", { name: "Member ID (empty: you)" }).fill(fixture.member);
      const source = page.getByRole("group", { name: "Test step 1", exact: true });
      const call = page.getByRole("group", { name: "Test step 2", exact: true });
      await source.getByRole("textbox", { name: "Test inputs (JSON)" }).fill('{"note":"Sample requiring review"}');
      const answer = '{"summary":"Sample advice"}';
      await call.getByRole("textbox", { name: "Provider answer" }).fill(answer);
      await call.getByRole("textbox", { name: "Expected typed answer (JSON, optional)" }).fill(answer);
      await call.getByRole("spinbutton", { name: "Input tokens" }).fill("4");
      await call.getByRole("spinbutton", { name: "Output tokens" }).fill("8");
      await page.getByRole("button", { name: "Save test plan", exact: true }).click();
      await expect(page.getByRole("status").filter({ hasText: "Test plan saved." })).toBeVisible();
      const planID = await page.getByRole("combobox", { name: "Saved test plan" }).inputValue();
      await page.getByRole("button", { name: "Run isolated test" }).focus();
      await page.keyboard.press("Enter");
      await expect(page.getByText("All expected outcomes matched.", { exact: true })).toBeVisible();
      await expect(page.getByText("Function expectation matched", { exact: true })).toBeVisible();
      const candidate = await page.getByText("Tested candidate:").locator("code").innerText();
      const identity = await page.getByText("Fixed test identity:").locator("code").innerText();
      await page.getByText("Tested candidate:").scrollIntoViewIfNeeded();
      await page.screenshot({ path: testInfo.outputPath("function-fixture-results.png"), fullPage: true });
      await page.getByText("Function expectation matched", { exact: true }).scrollIntoViewIfNeeded();
      await page.screenshot({ path: testInfo.outputPath("function-fixture-answer.png"), fullPage: true });
      await page.reload();
      await page.getByRole("combobox", { name: "Saved test plan" }).selectOption(planID);
      await expect(call.getByRole("textbox", { name: "Provider answer" })).toHaveValue(answer);
      await page.getByRole("button", { name: "Run isolated test" }).click();
      await expect(page.getByText("Fixed test identity:").locator("code")).toHaveText(identity);
      // Invalid provider JSON is rejected by the same strict output checker.
      await call.getByRole("textbox", { name: "Provider answer" }).fill("invalid");
      await call.getByRole("combobox", { name: "Expected function state" }).selectOption("rejected");
      await expect(page.getByText("Fixed test identity:")).not.toBeVisible();
      await page.getByRole("button", { name: "Run isolated test" }).click();
      await expect(page.getByText("All expected outcomes matched.", { exact: true })).toBeVisible();
      await expect(page.getByText("Tested candidate:").locator("code")).toHaveText(candidate);
      await expect(page.getByText("Fixed test identity:").locator("code")).not.toHaveText(identity);
      // Missing fixture must not leave a pending call and a green result.
      await call.getByRole("checkbox", { name: "Supply a fixed model answer" }).uncheck();
      await page.getByRole("button", { name: "Run isolated test" }).click();
      await expect(page.getByRole("alert").filter({ hasText: "Provide a fixed answer" })).toBeVisible();
      await page.getByRole("button", { name: "Reload saved plan" }).click();
      await page.setViewportSize({ width: 390, height: 780 });
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBeTruthy();
      await call.getByRole("textbox", { name: "Provider answer" }).scrollIntoViewIfNeeded();
      await page.screenshot({ path: testInfo.outputPath("function-fixture-narrow.png"), fullPage: true });
      const missing = await request.get(`/v1/records/${type}/TEST-1`, { headers: { Authorization: `Bearer ${fixture.operator}` } });
      expect(missing.status()).toBe(404);
      const privatePlan = await request.get(`/v1/records/build.testplan/${planID}`, { headers: { Authorization: `Bearer ${fixture.operator}` } });
      expect([403, 404]).toContain(privatePlan.status());
      const chinese = await page.context().newPage();
      await chinese.addInitScript(() => localStorage.setItem("platform.language", "zh-CN"));
      await open(chinese, fixture.builder, "/candidate-test");
      await chinese.getByRole("combobox", { name: "已保存测试计划" }).selectOption(planID);
      await chinese.getByRole("button", { name: "运行隔离测试" }).click();
      await expect(chinese.getByText("函数预期匹配", { exact: true })).toBeVisible();
      await chinese.getByText("固定测试标识：").or(chinese.getByText("固定测试标识:")).scrollIntoViewIfNeeded();
      await chinese.screenshot({ path: testInfo.outputPath("function-fixture-chinese.png"), fullPage: true });
      await chinese.close();
      // Release review and the test share the next version's exact bytes.
      await page.setViewportSize({ width: 1280, height: 800 });
      await open(page, fixture.builder, "/release-review");
      await page.getByRole("combobox", { name: "Definition kind" }).selectOption("function");
      await page.getByRole("combobox", { name: "Saved draft" }).selectOption(functionID);
      await page.getByRole("button", { name: "Check draft and dependencies" }).click();
      await expect(page.getByText("Draft candidate:").locator("code")).toHaveText(candidate);
    });
  });
}
