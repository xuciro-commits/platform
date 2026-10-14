import { expect, test } from "./kit";

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
  await builder.open(page, `/application-runs?app=build&name=${application.name}&application=build:${application.name}`);
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
