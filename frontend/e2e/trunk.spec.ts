import { expect, test } from "@playwright/test";

test("the trunk: sign in, open a list, open a record, change a field", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("button", { name: "admin-1 · acceptance" }).click();

  await page.getByRole("link", { name: "Ontology" }).click();
  await page.getByRole("navigation", { name: "Primary navigation" }).getByRole("link", { name: "Objects" }).click();
  await page.getByRole("link", { name: "Opportunities" }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Opportunities" })).toBeVisible();

  await page.getByRole("link", { name: "Press line retrofit" }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Press line retrofit" })).toBeVisible();

  const next = page.getByRole("textbox", { name: "Next step" });
  await expect(next).toHaveValue("Send revised terms");
  await next.fill("Call the plant manager");
  await next.press("Enter");
  await expect(page.getByRole("complementary").getByText("2", { exact: true })).toBeVisible();

  await page.reload();
  await expect(page.getByRole("textbox", { name: "Next step" })).toHaveValue("Call the plant manager");
});
