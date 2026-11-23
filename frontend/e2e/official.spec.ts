import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { type Page, expect, test } from "@playwright/test";

const artifact = fileURLToPath(new URL("../../scripts/frontend-fixtures/crm-official-0.2.0.json", import.meta.url));

test.describe.serial("official tenant", () => {
let page: Page;
test.beforeAll(async ({ browser }) => {
  page = await browser.newPage();
});
test.afterAll(async () => {
  await page.close();
});

test("official tenant: the official CRM installs from its released file", async () => {
  await page.goto("/");
  await page.getByRole("button", { name: "admin-1 · official" }).click();
  await expect(page.getByRole("button", { name: "Account", exact: true })).toBeVisible();
  await page.goto("/delivery/marketplace");
  await page.getByRole("button", { name: "More actions", exact: true }).click();
  await page.getByRole("menuitem", { name: "Install from URL or file", exact: true }).click();
  const wizard = page.getByRole("dialog", { name: /Install from URL or file/ });
  await wizard.locator('input[type="file"]').setInputFiles({ name: "crm.json", mimeType: "application/json", buffer: readFileSync(artifact) });
  await wizard.getByRole("button", { name: "Inspect package", exact: true }).click();
  await expect(wizard.getByText("crm · 0.2.0", { exact: true })).toBeVisible();
  await wizard.getByRole("button", { name: "Add to library", exact: true }).click();
  await wizard.getByRole("button", { name: "Continue", exact: true }).click({ timeout: 20_000 });
  await wizard.getByRole("button", { name: "Install package", exact: true }).click();
  await expect(wizard).toBeHidden({ timeout: 30_000 });
});

test("official tenant: settings are one page, and an activity added on an account's timeline is completed there", async () => {
  await page.getByRole("button", { name: "Account", exact: true }).click();
  await page.getByRole("menuitem", { name: "Sign out", exact: true }).click();
  await page.getByRole("button", { name: "manager-1 · official" }).click();
  await page.getByRole("link", { name: "CRM", exact: true }).click();
  const nav = page.getByRole("navigation", { name: "Primary navigation" });
  await nav.getByRole("link", { name: "Settings", exact: true }).click();
  await expect(page.getByText("Deal stages", { exact: true })).toBeVisible();
  await expect(page.getByText("Lost reasons", { exact: true })).toBeVisible();

  await nav.getByRole("link", { name: "Accounts", exact: true }).click();
  await page.getByRole("button", { name: "New", exact: true }).click();
  const create = page.getByRole("dialog");
  await create.getByRole("textbox", { name: "Account" }).fill("曦和精密");
  await create.getByRole("button", { name: "Create", exact: true }).click();
  await expect(page.getByRole("heading", { level: 1, name: "曦和精密" })).toBeVisible();
  await page.getByRole("tab", { name: "Activity" }).click();
  await page.getByRole("button", { name: "Add Activity", exact: true }).click();
  const add = page.getByRole("dialog");
  await add.getByRole("textbox", { name: "Subject" }).fill("给曦和打电话");
  await add.getByRole("button", { name: "Create", exact: true }).click();
  await expect(add).toBeHidden();
  await expect(page.getByRole("main").getByRole("link", { name: "给曦和打电话", exact: true })).toBeVisible();
  await page.getByRole("main").getByRole("button", { name: "Complete", exact: true }).click();
  const done = page.getByRole("dialog");
  await done.getByRole("textbox", { name: "Outcome" }).fill("约好下周演示");
  await done.getByRole("button", { name: "Complete", exact: true }).click();
  await expect(done).toBeHidden();
  await expect(page.getByRole("main").getByText("Done", { exact: true })).toBeVisible();
  await expect(page.getByRole("main").getByRole("button", { name: "Reopen", exact: true })).toBeVisible();
});
});
