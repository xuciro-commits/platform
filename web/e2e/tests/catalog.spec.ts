import { expect, test } from "@playwright/test";
import { open } from "./host";

test("Catalog leads a builder to a controlled Studio draft", async ({ page, request }, testInfo) => {
  await open(page, "manager", "/home");
  // An ordinary example parameter must not replace the application shell.
  await page.goto("/?preview=ui/button#/home");
  await page.getByRole("button", { name: "Platform Catalog", exact: true }).click();
  const appMenu = page.getByRole("button", { name: "Apps", exact: true }).first();
  await appMenu.click();
  await expect(page.getByRole("menuitemradio", { name: /Platform Catalog/ })).toHaveAttribute("aria-checked", "true");
  await page.keyboard.press("Escape");
  await page.getByRole("button", { name: "Primitives", exact: true }).click();
  await page.getByRole("button", { name: /^Button L1/ }).click();
  await expect(page.locator('[data-catalog-asset="ui/button"]:visible')).toBeVisible();
  await page.getByRole("button", { name: "All assets", exact: true }).click();
  const asset = page.locator('[data-catalog-asset="scenario/record-handling"]:visible');
  await expect(asset.getByRole("button", { name: "Create in Studio" })).toBeVisible();
  await expect(asset.getByRole("button", { name: "Copy developer example" })).toHaveCount(0);
  await expect(asset.getByText("Synthetic fixtures. This preview does not call a tenant host.")).toBeVisible();
  if (process.env.PLATFORM_SCREENSHOTS) await page.screenshot({ path: testInfo.outputPath("platform-catalog.png"), fullPage: true });
  await asset.getByRole("button", { name: "Create in Studio" }).click();
  await appMenu.click();
  await expect(page.getByRole("menuitemradio", { name: /Application Studio/ })).toHaveAttribute("aria-checked", "true");
  await page.keyboard.press("Escape");
  await page.getByRole("combobox", { name: "Object", exact: true }).selectOption("crm.opportunity");
  const name = `catalog${Date.now().toString(36)}`;
  await page.getByRole("textbox", { name: "Page name", exact: true }).fill(name);
  await page.getByRole("textbox", { name: "Page title", exact: true }).fill("Catalog record handling");
  await page.getByRole("button", { name: "Create draft in Studio" }).click();
  await expect(page.getByRole("heading", { name: "Catalog record handling", exact: true }).first()).toBeVisible();
  const records = await (await request.get("/v1/records/build.page?limit=500", { headers: { Authorization: "Bearer manager" } })).json();
  const draft = records.records.find((record: { name: string }) => record.name === name);
  expect(draft).toMatchObject({ name, object: "crm.opportunity", state: "draft" });
  expect(draft.description).toContain("build/record-handling");
  expect(draft.sections.map((section: { widget: string }) => section.widget)).toEqual(expect.arrayContaining(["table", "detail", "actions"]));
  const definitions = await (await request.get("/v1/definitions", { headers: { Authorization: "Bearer manager" } })).json();
  expect(definitions.some((definition: { ref: { name: string } }) => definition.ref.name === `build.${name}`)).toBe(false);
});
