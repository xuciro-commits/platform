import { expect, fresh, slug, test } from "./kit";

test("application capacity, original decisions and headless runtime remain visible", async ({ page, builder, operator }, testInfo) => {
  const object = await builder.object({ title: "Runtime case object", fields: [{ name: "note", title: "Note", type: "text" }] });
  const application = await builder.application({ title: "Runtime case application", resources: [{ app: "build", kind: "object", name: object.type }] });
  await builder.open(page, `/project?id=${application.id}`);
  await page.getByRole("tab", { name: "Settings", exact: true }).click();
  const capacity = page.getByRole("spinbutton", { name: "Expected records: Runtime case object", exact: true });
  await capacity.fill("1");
  await expect.poll(async () => (await builder.record("build.app", application.id)).profile?.expectedRecords?.[object.type]).toBe(1);
  await page.getByRole("textbox", { name: "Target recovery time", exact: true }).fill("30m");
  await expect.poll(async () => (await builder.record("build.app", application.id)).profile?.rto).toBe("30m");
  await builder.decide("build.app.publish", { type: "build.app", id: application.id }, {});
  const first = `${application.id}-FIRST`, second = `${application.id}-SECOND`;
  await operator.decide(`${object.type}.create`, { type: object.type, id: first }, { note: "First" });
  await operator.decide(`${object.type}.create`, { type: object.type, id: second }, { note: "Second" });
  await builder.open(page, "/portal");
  await page.getByRole("button", { name: "Runtime case application", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Application overview", exact: true })).toBeVisible();
  await expect(page.getByText("No business pages are available in this application. You can view its activity and diagnose problems from application runs.", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "View application runs", exact: true }).click();
  await expect(page.getByText("Current content version", { exact: true })).toBeVisible();
  await expect(page.getByRole("alert").filter({ hasText: "scale exceeded: 2 / 1" })).toBeVisible();
  await page.getByText("Diagnose a record", { exact: true }).click();
  await page.getByRole("textbox", { name: "Record ID", exact: true }).fill(first);
  await page.getByRole("button", { name: "Diagnose", exact: true }).click();
  await expect(page.getByText(`${object.type}.create`, { exact: true }).first()).toBeVisible();
  await expect(page.getByText(/^Definition content: schema.sha256\./)).toBeVisible();
  await expect(page.getByRole("combobox", { name: "Run kind", exact: true })).toBeVisible();
  await expect(page.getByRole("combobox", { name: "Run state", exact: true })).toBeVisible();
  if (process.env.PLATFORM_SCREENSHOTS) await page.screenshot({ path: testInfo.outputPath("application-runtime.png"), fullPage: true });
});

test("published evaluation sets open in their original test-plan editor and release review", async ({ page, builder }) => {
  const object = await builder.object({ title: "Evaluation object", fields: [{ name: "note", title: "Note", type: "text" }] });
  const functionID = fresh("FUNCTION"), functionName = slug("advice");
  await builder.decide("build.function.create", { type: "build.function", id: functionID }, {
    name: functionName, title: "Evaluation advice", description: "Summarise a synthetic note", object: object.type,
    fields: ["note"], roles: ["user", "builder"], model: "fixture/probe",
    instructions: "Summarise the provided note", maxInputBytes: 4096, maxOutputBytes: 1024, maxTokens: 256,
    output: [{ name: "summary", type: "string", required: true, description: "A short factual summary" }],
  });
  await builder.decide("build.function.publish", { type: "build.function", id: functionID }, {});
  const planID = fresh("PLAN"), name = slug("quality");
  await builder.decide("build.testplan.create", { type: "build.testplan", id: planID }, {
    name, title: "Versioned evaluation", function: functionID, model: "fixture/probe", at: "2026-10-14T09:00:00Z",
    steps: [{ type: object.type, id: "synthetic", action: `${object.type}.create`, payload: '{"note":"Synthetic"}', expect: "accepted" }],
    evaluation: [{ minQuality: 1, maxCostUsd: 0.1, maxLatencyMillis: 30000, cases: [{ name: "synthetic", input: { note: "Synthetic" }, expected: { summary: "Synthetic" } }] }],
  });
  await builder.decide("build.testplan.publish", { type: "build.testplan", id: planID }, {});
  await builder.open(page, `/evaluation-set?id=${planID}`);
  await expect(page.getByRole("textbox", { name: "Evaluation set name", exact: true })).toHaveValue(name);
  await expect(page.getByRole("button", { name: "Publish evaluation set", exact: true })).toBeEnabled();
  await page.getByRole("button", { name: "Publish evaluation set", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Release review", exact: true })).toBeVisible();
});
