import { expect, test } from "@playwright/test";
import { decide, fresh, open } from "./host";

test("switching a test root creates a new plan instead of moving the old one", async ({ page, request }, testInfo) => {
  const first = fresh("OBJECT"), second = fresh("OBJECT"), planID = fresh("PLAN");
  const suffix = Date.now().toString(36);
  for (const [id, name] of [[first, `first${suffix}`], [second, `second${suffix}`]]) {
    await decide(request, "manager", "build", "build.object.create", { type: "build.object", id: id! }, {
      name, title: name, plural: name, fields: [{ name: "note", title: "Note", type: "text" }, { name: "quantity", title: "Quantity", type: "integer" }, { name: "active", title: "Active", type: "boolean" }],
    });
  }
  await decide(request, "manager", "build", "build.testplan.create", { type: "build.testplan", id: planID }, {
    title: "First asset plan", object: first, at: "2026-01-01T09:00:00Z",
    steps: [{ type: `build.first${suffix}`, id: "TEST-1", action: `build.first${suffix}.create`, payload: "{}", expect: "accepted" }],
  });
  const headers = { Authorization: "Bearer manager" };
  const original = (await (await request.get(`/v1/records/build.testplan/${planID}`, { headers })).json()).record;
  await open(page, "manager", `/candidate-test?objectId=${first}`);
  await page.getByRole("combobox", { name: "Saved test plan", exact: true }).selectOption(planID);
  await page.getByRole("combobox", { name: "Saved object draft", exact: true }).selectOption(second);
  await expect(page.getByRole("combobox", { name: "Saved test plan", exact: true }).getByRole("option", { name: "First asset plan" })).toHaveCount(0);
  await expect(page.getByRole("textbox", { name: "Test plan name", exact: true })).toHaveValue("");
  await page.getByRole("textbox", { name: "Test plan name", exact: true }).fill("Second asset plan");
  const createStep = page.getByRole("group", { name: "Test step 1", exact: true });
  const editStep = page.getByRole("group", { name: "Test step 2", exact: true });
  await createStep.getByRole("textbox", { name: "Note", exact: true }).fill("Created with fields");
  await createStep.getByRole("spinbutton", { name: "Quantity", exact: true }).fill("7");
  await createStep.getByRole("checkbox", { name: "Active", exact: true }).check();
  await editStep.getByRole("textbox", { name: "Note", exact: true }).fill("Edited with fields");
  await editStep.getByRole("button", { name: "Advanced JSON", exact: true }).click();
  await expect(editStep.getByRole("textbox", { name: "Test inputs (JSON)" })).toHaveValue('{"note":"Edited with fields"}');
  await editStep.getByRole("button", { name: "Use input fields", exact: true }).click();
  await page.getByRole("button", { name: "Save test plan", exact: true }).click();
  await expect(page.getByText("Test plan saved. Reload it to repeat these fixed inputs.", { exact: true })).toBeVisible();
  const after = (await (await request.get(`/v1/records/build.testplan/${planID}`, { headers })).json()).record;
  expect(after).toMatchObject({ object: first, title: original.title, revision: original.revision });
  const inventory = await (await request.get("/v1/records/build.testplan?limit=500", { headers })).json();
  expect(inventory.records.find((plan: { title: string }) => plan.title === "Second asset plan")).toMatchObject({ object: second });
  await createStep.getByRole("textbox", { name: "Note", exact: true }).fill("Unsaved input");
  await page.getByRole("button", { name: "Reload saved plan", exact: true }).click();
  await expect(createStep.getByRole("textbox", { name: "Note", exact: true })).toHaveValue("Created with fields");
  await expect(createStep.getByRole("spinbutton", { name: "Quantity", exact: true })).toHaveValue("7");
  await expect(createStep.getByRole("checkbox", { name: "Active", exact: true })).toBeChecked();
  await expect(editStep.getByRole("textbox", { name: "Note", exact: true })).toHaveValue("Edited with fields");
  if (process.env.PLATFORM_SCREENSHOTS) await page.screenshot({ path: testInfo.outputPath("typed-test-inputs.png"), fullPage: true });
  await page.getByRole("button", { name: "Run isolated test", exact: true }).click();
  await expect(page.getByText("All expected outcomes matched.", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Review release", exact: true }).click();
  await expect(page.getByRole("combobox", { name: "Definition kind", exact: true })).toHaveValue("object");
  await expect(page.getByRole("combobox", { name: "Saved draft", exact: true })).toHaveValue(second);
  await page.getByRole("button", { name: "Check draft and dependencies", exact: true }).click();
  await page.getByRole("button", { name: "Save immutable candidate", exact: true }).click();
  await expect(page.getByText("Current definitions could not be compared with this candidate.", { exact: true })).toBeVisible();
  await expect(page.getByRole("heading", { name: /^Added/ })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Activate release", exact: true })).toBeEnabled();
});
