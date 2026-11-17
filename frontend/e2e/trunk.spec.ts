import { type Page, expect, test } from "@playwright/test";

async function signIn(page: Page) {
  await page.goto("/");
  await page.getByRole("button", { name: "admin-1 · acceptance" }).click();
}

async function open(page: Page, app: string, entry: string) {
  await page.getByRole("link", { name: app }).click();
  await page.getByRole("navigation", { name: "Primary navigation" }).getByRole("link", { name: entry }).click();
}

test("records: open a list, open a record, change a field in place", async ({ page }) => {
  await signIn(page);
  await open(page, "Ontology", "Objects");
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

test("publish: a draft type goes live from its record page and takes records", async ({ page }) => {
  await signIn(page);
  await open(page, "Ontology", "Object types");
  await page.getByRole("link", { name: "Contact", exact: true }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Contact" })).toBeVisible();
  await expect(page.getByText("Draft", { exact: true })).toBeVisible();

  await page.getByRole("button", { name: "Publish" }).click();
  const confirm = page.getByRole("alertdialog");
  await expect(confirm).toContainText("Contact");
  await confirm.getByRole("button", { name: "Publish" }).click();
  await expect(page.getByText("Published", { exact: true })).toBeVisible();

  await page.getByRole("link", { name: "Open records" }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Contacts" })).toBeVisible();

  await page.getByRole("button", { name: "New" }).click();
  const form = page.getByRole("dialog");
  await form.getByRole("textbox", { name: "Full name" }).fill("Ines Brandt");
  await form.getByRole("button", { name: "Create" }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Ines Brandt" })).toBeVisible();
});
