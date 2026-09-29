import { expect, test } from "@playwright/test";
import { decide, fresh, open } from "./host";

for (const fixture of [
  { industry: "hospitality", baseURL: "http://127.0.0.1:18496", builder: "manager", member: "desk-1", title: "Guest advice review" },
  { industry: "manufacturing", baseURL: "http://127.0.0.1:18497", builder: "supervisor", member: "op-l1", title: "Inspection advice review" },
]) {
  test.describe(fixture.industry, () => {
    test.use({ baseURL: fixture.baseURL });
    test("save and reload a workflow with a pinned function", async ({ page, request }) => {
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
      await page.getByRole("button", { name: "Publish workflow", exact: true }).click();
      await expect(page.getByRole("status").filter({ hasText: "Installed workflow version 1." })).toBeVisible();
      await page.reload();
      await page.getByRole("button", { name: "1. Call AI function", exact: true }).click();
      await expect(properties.getByRole("combobox", { name: "Published function version" })).toHaveValue(`${name}:1`);
    });
  });
}
