import { expect, test } from "@playwright/test";
import { decide, fresh, open } from "./host";

test("overlay workbench runs original record actions and clears closed input through a frozen release", async ({ page, request }, testInfo) => {
  const name = fresh("overlay").replace(/[^a-z0-9]/gi, "").toLowerCase(), id = fresh("PAGE"), object = fresh("OBJ"), record = fresh("NOTE");
  const type = `build.${name}`;
  await decide(request, "manager", "build", "build.object.create", { type: "build.object", id: object }, {
    name, title: "Overlay notes", fields: [{ name: "note", title: "Note", type: "text" }],
    states: [{ name: "open", title: "Open" }, { name: "done", title: "Done" }], actions: [{ name: "close", title: "Complete note", from: ["open"], to: "done" }],
  });
  await decide(request, "manager", "build", "build.object.publish", { type: "build.object", id: object }, {});
  await decide(request, "desk", "build", `${type}.create`, { type, id: record }, { note: "OVERLAY-TASK" });
  await decide(request, "manager", "build", "build.page.create", { type: "build.page", id }, {
    name, title: "Record operation desk", object: type, sections: [
      { widget: "table", title: "Notes", fields: ["note", "state"] },
      { widget: "form", title: "New note", fields: ["note"] },
      { widget: "actions", title: "Record work", actions: [`${type}.close`] },
    ],
  });
  await open(page, "manager", `/compose?id=${id}`);
  const tree = page.getByRole("region", { name: "Widgets and layout", exact: true });
  const inspector = page.getByRole("region", { name: "The widget in hand", exact: true });
  const canvas = page.getByRole("region", { name: "The page", exact: true });
  await tree.getByRole("button", { name: "Add overlay", exact: true }).click();
  await inspector.getByLabel("Overlay title", { exact: true }).fill("Record panel");
  await inspector.getByRole("combobox", { name: "Overlay kind", exact: true }).selectOption("drawer");
  await inspector.getByRole("combobox", { name: "Layout", exact: true }).selectOption("flow");
  for (const title of ["New note", "Record work"]) {
    await tree.getByRole("button", { name: title, exact: true }).click();
    await inspector.getByRole("combobox", { name: "Move widget to", exact: true }).selectOption({ label: "Record panel" });
  }
  await tree.getByRole("button", { name: "Button", exact: true }).click();
  await inspector.getByLabel("Title", { exact: true }).fill("Back to notes");
  await inspector.getByRole("combobox", { name: "Overlay action", exact: true }).selectOption({ label: "Close Record panel" });
  await tree.getByRole("button", { name: "Notes", exact: true }).click();
  await tree.getByRole("button", { name: "Button", exact: true }).click();
  await inspector.getByLabel("Title", { exact: true }).fill("Work on record");
  await inspector.getByRole("combobox", { name: "Overlay action", exact: true }).selectOption({ label: "Open Record panel" });
  await tree.getByRole("button", { name: "Toolbar", exact: true }).click();
  await inspector.getByLabel("Container title", { exact: true }).fill("Record commands");
  await tree.getByRole("button", { name: "Page variables", exact: true }).click();
  await inspector.getByRole("button", { name: "Add variable", exact: true }).click();
  await inspector.getByLabel("Variable label", { exact: true }).fill("Selected note");
  await inspector.getByRole("combobox", { name: "Variable mode", exact: true }).selectOption("resource");
  await inspector.getByRole("combobox", { name: "Source widget", exact: true }).selectOption({ label: "Notes" });
  const selected = await inspector.getByRole("combobox", { name: "Choose page variable", exact: true }).inputValue();
  await inspector.getByRole("button", { name: "Add variable", exact: true }).click();
  await inspector.getByLabel("Variable label", { exact: true }).fill("Has selected note");
  await inspector.getByRole("combobox", { name: "Variable mode", exact: true }).selectOption("derived");
  await inspector.getByRole("combobox", { name: "Operator", exact: true }).selectOption("present");
  await inspector.getByRole("combobox", { name: "Argument source", exact: true }).selectOption(selected);
  const condition = await inspector.getByRole("combobox", { name: "Choose page variable", exact: true }).inputValue();
  await tree.getByRole("button", { name: "Work on record", exact: true }).click();
  await inspector.getByRole("combobox", { name: "Enabled when", exact: true }).selectOption(condition);
  await expect(canvas.getByRole("button", { name: "Work on record", exact: true })).toBeDisabled();
  await canvas.getByRole("row").filter({ hasText: "OVERLAY-TASK" }).click();
  await canvas.getByRole("button", { name: "Work on record", exact: true }).click();
  const preview = page.getByRole("dialog", { name: "Record panel", exact: true });
  await expect(preview.getByText("Actions do not run while you compose.", { exact: true })).toBeVisible();
  await expect(preview.getByRole("button", { name: "Create", exact: true })).toBeDisabled();
  await preview.getByRole("button", { name: "Back to notes", exact: true }).click();
  await page.getByRole("button", { name: "Save", exact: true }).click();
  await expect(page.getByRole("button", { name: "Save", exact: true })).toBeDisabled();
  const saved = (await (await request.get(`/v1/records/build.page/${id}`, { headers: { Authorization: "Bearer manager" } })).json()).record;
  expect(Object.values(saved.document.overlays)).toHaveLength(1);
  expect(saved.document.events).toHaveLength(2);
  if (process.env.PLATFORM_SCREENSHOTS) await page.screenshot({ path: testInfo.outputPath("overlay-designer.png"), fullPage: true });
  await page.getByRole("button", { name: "Review release", exact: true }).click();
  await page.getByRole("button", { name: "Check draft and dependencies", exact: true }).click();
  await page.getByRole("button", { name: "Save immutable candidate", exact: true }).click();
  await page.getByRole("button", { name: "Activate release", exact: true }).click();
  const operation = await page.context().newPage();
  await open(operation, "desk", `/page?app=build&kind=page&name=${name}`);
  const caller = operation.getByRole("button", { name: "Work on record", exact: true });
  await expect(caller).toBeDisabled();
  await operation.getByRole("row").filter({ hasText: "OVERLAY-TASK" }).click();
  await caller.click();
  const panel = operation.getByRole("dialog", { name: "Record panel", exact: true });
  await panel.getByLabel("Note", { exact: true }).fill("Unsaved local note");
  await operation.keyboard.press("Escape");
  await expect(panel).toHaveCount(0);
  await expect(caller).toBeFocused();
  await caller.click();
  await expect(panel.getByLabel("Note", { exact: true })).toHaveValue("");
  if (process.env.PLATFORM_SCREENSHOTS) await operation.screenshot({ path: testInfo.outputPath("overlay-runtime.png"), fullPage: true });
  await panel.getByRole("button", { name: "Complete note", exact: true }).click();
  await expect.poll(async () => (await (await request.get(`/v1/records/${type}/${record}`, { headers: { Authorization: "Bearer desk" } })).json()).record.state).toBe("done");
  await panel.getByRole("button", { name: "Back to notes", exact: true }).click();
  await expect(caller).toBeFocused();
  await caller.click();
  await expect(panel).toBeVisible();
  await panel.getByLabel("Note", { exact: true }).focus();
  // Outside click uses the same close binding and focus return.
  await operation.mouse.click(500, 300);
  await expect(panel).toHaveCount(0);
  await expect(caller).toBeFocused();
  if (process.env.PLATFORM_SCREENSHOTS) {
    await operation.setViewportSize({ width: 390, height: 844 });
    await caller.click();
    await operation.screenshot({ path: testInfo.outputPath("overlay-runtime-narrow.png"), fullPage: true });
  }
  await operation.reload();
  await expect(panel).toHaveCount(0);
  await expect(caller).toBeDisabled();
});
