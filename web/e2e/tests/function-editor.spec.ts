import { expect, test } from "@playwright/test";
import { decide, fresh, open } from "./host";

for (const fixture of [
  { industry: "hospitality", baseURL: "http://127.0.0.1:18496", builder: "manager", member: "desk-1", title: "Guest request advice" },
  { industry: "manufacturing", baseURL: "http://127.0.0.1:18497", builder: "supervisor", member: "op-l1", title: "Inspection advice" },
]) {
  test.describe(fixture.industry, () => {
    test.use({ baseURL: fixture.baseURL });
    test("compose, test and publish a typed function", async ({ page, request }) => {
      const objectID = fresh("OBJ"), name = fresh("fn").replace(/[^a-z0-9]/gi, "").toLowerCase(), type = `build.${name}`;
      await decide(request, fixture.builder, "build", "build.object.create", { type: "build.object", id: objectID }, {
        name, title: fixture.title, fields: [{ name: "note", title: "Note", type: "text" }, { name: "day", title: "Day", type: "date" }],
      });
      await decide(request, fixture.builder, "build", "build.object.publish", { type: "build.object", id: objectID }, {});
      await open(page, fixture.builder, "/function");
      await page.getByRole("button", { name: "New AI function", exact: true }).click();
      const properties = page.getByRole("region", { name: "Function properties" });
      const stages = page.getByRole("region", { name: "Function stages" });
      await properties.getByRole("textbox", { name: "Function name", exact: true }).fill(name);
      await properties.getByRole("textbox", { name: "Function title" }).fill(fixture.title);
      await properties.getByRole("textbox", { name: "Function description" }).fill("A bounded suggestion for review");
      await stages.getByRole("button", { name: "Record inputs", exact: true }).click();
      await properties.getByRole("combobox", { name: "Source object" }).selectOption(type);
      await expect(properties.getByRole("checkbox", { name: /Day/ })).not.toBeVisible();
      await properties.getByRole("checkbox", { name: /Note/ }).check();
      // Canvas selection reaches the same inspector as keyboard stage buttons.
      const modelNode = page.getByRole("figure", { name: "Function map", exact: true }).locator('[data-id="model"]');
      await modelNode.focus();
      await modelNode.press("Enter");
      await properties.getByRole("textbox", { name: "Model instructions" }).fill("Use only the source note. Return a summary and whether review is needed.");
      await stages.getByRole("button", { name: "Strict output", exact: true }).focus();
      await page.keyboard.press("Enter");
      const summary = properties.getByRole("group", { name: "Output field 1", exact: true });
      await summary.getByRole("textbox", { name: "Output description" }).fill("Factual summary");
      await properties.getByRole("button", { name: "Add output field" }).click();
      const review = properties.getByRole("group", { name: "Output field 2", exact: true });
      await review.getByRole("textbox", { name: "Output field name" }).fill("review");
      await review.getByRole("combobox", { name: "Output type" }).selectOption("boolean");
      await review.getByRole("textbox", { name: "Output description" }).fill("Needs human review");
      await page.getByRole("button", { name: "Save function", exact: true }).focus();
      await page.keyboard.press("Enter");
      await expect(page.getByRole("button", { name: "Test function", exact: true })).toBeEnabled();
      const records = await (await request.get("/v1/records/build.function?limit=500", { headers: { Authorization: `Bearer ${fixture.builder}` } })).json();
      const fn = records.records.find((record: { name: string }) => record.name === name);
      expect(fn.fields).toEqual(["note"]);
      expect(fn.output[1]).toMatchObject({ name: "review", type: "boolean", required: true });
      // Conflicts preserve local edits; explicit reload restores saved state.
      await properties.getByRole("textbox", { name: "Function description" }).fill("Local conflicting revision");
      await decide(request, fixture.builder, "build", "build.function.edit", { type: "build.function", id: fn.id }, { description: "Remote revision" });
      await page.getByRole("button", { name: "Save function", exact: true }).click();
      await expect(page.getByRole("alert").filter({ hasText: /CONFLICT|changed|revision/i })).toBeVisible();
      await expect(properties.getByRole("textbox", { name: "Function description" })).toHaveValue("Local conflicting revision");
      await page.getByRole("button", { name: "Reload saved function" }).click();
      await expect(properties.getByRole("textbox", { name: "Function description" })).toHaveValue("Remote revision");
      await page.getByRole("button", { name: "Test function", exact: true }).click();
      await expect(page.getByRole("combobox", { name: "Saved function draft" })).toHaveValue(fn.id);
      await page.getByRole("textbox", { name: "Member ID (empty: you)" }).fill(fixture.member);
      await page.getByRole("group", { name: "Test step 1", exact: true }).getByRole("textbox", { name: "Test inputs (JSON)" }).fill('{"note":"Needs a review"}');
      await page.getByRole("group", { name: "Test step 2", exact: true }).getByRole("textbox", { name: "Provider answer" }).fill('{"summary":"Review needed","review":true}');
      await page.getByRole("button", { name: "Run isolated test" }).click();
      await expect(page.getByText("All expected outcomes matched.", { exact: true })).toBeVisible();
      const candidate = await page.getByText("Tested candidate:").locator("code").innerText();
      await open(page, fixture.builder, "/release-review");
      await page.getByRole("combobox", { name: "Definition kind" }).selectOption("function");
      await page.getByRole("combobox", { name: "Saved draft" }).selectOption(fn.id);
      await page.getByRole("button", { name: "Check draft and dependencies" }).click();
      await expect(page.getByText("Draft candidate:").locator("code")).toHaveText(candidate);
      await page.getByRole("button", { name: "Save immutable candidate" }).click();
      await expect(page.getByRole("button", { name: "Activate release" })).toBeDisabled();
      await open(page, fixture.builder, `/function?id=${fn.id}`);
      await page.getByRole("button", { name: "Publish function", exact: true }).click();
      await expect(page.getByRole("status").filter({ hasText: "Installed function version 1." })).toBeVisible();
      await expect(properties.getByRole("textbox", { name: "Function name", exact: true })).toBeDisabled();
      await page.reload();
      await expect(page.getByRole("status").filter({ hasText: "Installed function version 1." })).toBeVisible();
      await stages.getByRole("button", { name: "Record inputs", exact: true }).click();
      await expect(properties.getByRole("combobox", { name: "Source object" })).toBeDisabled();
      await stages.getByRole("button", { name: "Model inference", exact: true }).click();
      await properties.getByRole("textbox", { name: "Model instructions" }).fill("Updated instructions for later calls");
      await page.getByRole("button", { name: "Publish function", exact: true }).click();
      await expect(page.getByRole("status").filter({ hasText: "Installed function version 2." })).toBeVisible();
      await page.reload();
      await expect(page.getByRole("status").filter({ hasText: "Installed function version 2." })).toBeVisible();
    });
  });
}
