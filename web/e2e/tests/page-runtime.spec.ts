import { expect, test } from "@playwright/test";
import { pageUIProfile, decide, fresh, open } from "./host";

test("typed page variables drive tabs and visibility through saved candidate activation", async ({ page, request }, testInfo) => {
  const name = fresh("tabs").replace(/[^a-z0-9]/gi, "").toLowerCase(), id = fresh("PAGE"), object = fresh("OBJ");
  await decide(request, "manager", "build", "build.object.create", { type: "build.object", id: object }, {
    name, title: "Tabbed notes", fields: [{ name: "note", title: "Note", type: "text" }],
  });
  await decide(request, "manager", "build", "build.object.publish", { type: "build.object", id: object }, {});
  await decide(request, "desk", "build", `build.${name}.create`, { type: `build.${name}`, id: fresh("NOTE") }, { note: "Runtime note" });
  await decide(request, "manager", "build", "build.page.create", { type: "build.page", id }, {
    name, title: "Variable notebook", object: `build.${name}`, sections: [
      { widget: "table", title: "Notes", fields: ["note"] },
      { widget: "detail", title: "Note detail", fields: ["note"] },
      { widget: "text", title: "Guidance", text: "Inspect the selected note." },
    ],
  });
  await open(page, "manager", `/compose?id=${id}`);
  const tree = page.getByRole("region", { name: "Widgets and layout", exact: true });
  const inspector = page.getByRole("region", { name: "The widget in hand", exact: true });
  await tree.getByRole("button", { name: "Notes", exact: true }).click();
  await tree.getByRole("button", { name: "Group with next in tabs", exact: true }).click();
  await inspector.getByLabel("Container title", { exact: true }).fill("Notebook views");
  await inspector.getByLabel("Tab 1 title", { exact: true }).fill("Browse");
  await inspector.getByLabel("Tab 2 title", { exact: true }).fill("Inspect");
  await tree.getByRole("button", { name: "Page variables", exact: true }).click();
  await inspector.getByLabel("Variable label", { exact: true }).fill("Active view");
  const activeID = await inspector.getByLabel("Choose page variable", { exact: true }).inputValue();
  await inspector.getByRole("button", { name: "Add variable", exact: true }).click();
  await inspector.getByLabel("Variable label", { exact: true }).fill("Show guidance");
  await inspector.getByRole("combobox", { name: "Value type", exact: true }).selectOption("boolean");
  await inspector.getByRole("combobox", { name: "Variable mode", exact: true }).selectOption("derived");
  await inspector.getByRole("combobox", { name: "Argument source", exact: true }).nth(0).selectOption(activeID);
  await inspector.getByRole("combobox", { name: "Use tab identity", exact: true }).selectOption({ label: "Inspect" });
  const visibleID = await inspector.getByLabel("Choose page variable", { exact: true }).inputValue();
  await tree.getByRole("button", { name: "Guidance", exact: true }).click();
  await inspector.getByRole("combobox", { name: "Visible when", exact: true }).selectOption(visibleID);
  await page.getByRole("button", { name: "Save", exact: true }).click();
  await expect(page.getByRole("button", { name: "Save", exact: true })).toBeDisabled();
  const record = (await (await request.get(`/v1/records/build.page/${id}`, { headers: { Authorization: "Bearer manager" } })).json()).record;
  expect(record.document.uiProfile).toBe(pageUIProfile);
  expect(record.document.variables[visibleID].expression.args[0].variable).toBe(activeID);
  if (process.env.PLATFORM_SCREENSHOTS) {
    await tree.getByRole("button", { name: "Page variables", exact: true }).click();
    await inspector.getByRole("combobox", { name: "Choose page variable", exact: true }).selectOption(visibleID);
    await page.screenshot({ path: testInfo.outputPath("tabs-designer.png"), fullPage: true, animations: "disabled" });
  }
  await page.getByRole("button", { name: "Review release", exact: true }).click();
  await page.getByRole("button", { name: "Check draft and dependencies", exact: true }).click();
  await page.getByRole("button", { name: "Save immutable candidate", exact: true }).click();
  await page.getByRole("button", { name: "Activate release", exact: true }).click();
  const operation = await page.context().newPage();
  await open(operation, "desk", `/page?app=build&kind=page&name=${name}`);
  await expect(operation.getByRole("tab", { name: "Browse", exact: true })).toHaveAttribute("aria-selected", "true");
  await expect(operation.getByText("Inspect the selected note.", { exact: true })).toHaveCount(0);
  await operation.getByRole("row").filter({ hasText: "Runtime note" }).click();
  await operation.getByRole("tab", { name: "Inspect", exact: true }).click();
  await expect(operation.getByRole("tabpanel", { name: "Inspect", exact: true }).getByRole("definition").filter({ hasText: /^Runtime note$/ })).toBeVisible();
  await expect(operation.getByText("Inspect the selected note.", { exact: true })).toBeVisible();
  // A second page session starts from initial state and does not follow the first.
  const separate = await page.context().newPage();
  await open(separate, "desk", `/page?app=build&kind=page&name=${name}`);
  await expect(separate.getByRole("tab", { name: "Browse", exact: true })).toHaveAttribute("aria-selected", "true");
  await expect(operation.getByRole("tab", { name: "Inspect", exact: true })).toHaveAttribute("aria-selected", "true");
  if (process.env.PLATFORM_SCREENSHOTS) await operation.screenshot({ path: testInfo.outputPath("tabs-runtime.png"), fullPage: true, animations: "disabled" });
  await operation.reload();
  await expect(operation.getByRole("tab", { name: "Browse", exact: true })).toHaveAttribute("aria-selected", "true");
  await expect(operation.getByText("Inspect the selected note.", { exact: true })).toHaveCount(0);
  if (process.env.PLATFORM_SCREENSHOTS) {
    await separate.setViewportSize({ width: 390, height: 844 });
    await separate.reload();
    await separate.getByRole("row").filter({ hasText: "Runtime note" }).click();
    await separate.getByRole("tab", { name: "Inspect", exact: true }).click();
    await separate.getByRole("definition").filter({ hasText: /^Runtime note$/ }).waitFor();
    await separate.screenshot({ path: testInfo.outputPath("tabs-runtime-narrow.png"), fullPage: true, animations: "disabled" });
  }
});
