import { readFileSync } from "node:fs";
import { fileURLToPath } from "node:url";
import { type Page, expect, test } from "@playwright/test";
import { edit, property, signIn } from "./support";

const artifact = fileURLToPath(new URL("../../scripts/frontend-fixtures/crm-official-0.3.1.json", import.meta.url));

test.describe.serial("official tenant", () => {
let page: Page;
test.beforeAll(async ({ browser }) => {
  page = await browser.newPage();
});
test.afterAll(async () => {
  await page.close();
});

async function as(seat: string) {
  await page.goto("/");
  if (await page.getByRole("button", { name: "Account", exact: true }).count()) {
    await page.getByRole("button", { name: "Account", exact: true }).click();
    await page.getByRole("menuitem", { name: "Sign out", exact: true }).click();
  }
  await signIn(page, `${seat} · official`);
}

async function crm(entry: string) {
  await page.goto("/");
  await page.getByRole("link", { name: "CRM", exact: true }).first().click();
  await page.getByRole("navigation", { name: "Primary navigation" }).getByRole("link", { name: entry, exact: true }).click();
  await expect(page.getByRole("main", { name: entry, exact: true })).toBeVisible();
}

test("official tenant: the released CRM installs from its file", async () => {
  await as("admin-1");
  await page.goto("/delivery/marketplace");
  await page.getByRole("button", { name: "More actions", exact: true }).click();
  await page.getByRole("menuitem", { name: "Install from URL or file", exact: true }).click();
  const wizard = page.getByRole("dialog", { name: /Install from URL or file/ });
  await wizard.locator('input[type="file"]').setInputFiles({ name: "crm.json", mimeType: "application/json", buffer: readFileSync(artifact) });
  await wizard.getByRole("button", { name: "Inspect package", exact: true }).click();
  await expect(wizard.getByText("crm · 0.3.1", { exact: true })).toBeVisible();
  await wizard.getByRole("button", { name: "Add to library", exact: true }).click();
  await wizard.getByRole("button", { name: "Continue", exact: true }).click({ timeout: 20_000 });
  await wizard.getByRole("button", { name: "Install package", exact: true }).click();
  await expect(wizard).toBeHidden({ timeout: 30_000 });
});

test("official tenant: a sales rep qualifies a lead and converts it into an account, contact and deal", async () => {
  test.setTimeout(90_000);
  await as("rep-1");
  await crm("Leads");
  await page.getByRole("button", { name: "New", exact: true }).click();
  const lead = page.getByRole("dialog");
  await lead.getByRole("textbox", { name: "Full name" }).fill("周宁");
  await lead.getByRole("button", { name: "Create", exact: true }).click();
  await expect(page.getByRole("heading", { name: "周宁", exact: true }).first()).toBeVisible();
  await edit(page, "Company");
  const company = page.getByRole("textbox", { name: "Company", exact: true });
  await company.fill("曦和精密");
  await company.press("Enter");
  await expect(property(page, "Company")).toContainText("曦和精密");
  await edit(page, "Budget");
  const budget = page.getByRole("spinbutton", { name: "Budget", exact: true });
  await budget.fill("300000");
  await budget.press("Enter");
  await expect(property(page, "Budget")).toContainText("300,000.00");

  await page.getByRole("button", { name: "Convert to deal", exact: true }).click();
  const convert = page.getByRole("dialog", { name: "Convert to deal", exact: true });
  for (const [name, date] of [["Expected close", "December 31, 2026"], ["Next step due", "November 30, 2026"]]) {
    const field = convert.getByRole("combobox", { name, exact: true });
    await field.fill(date);
    await field.press("Tab");
  }
  await convert.getByRole("textbox", { name: "Next step" }).fill("发送方案");
  await convert.getByRole("button", { name: "Convert to deal", exact: true }).click();
  await expect(convert).toBeHidden();
  await expect(page).toHaveURL(/deal\/DEAL-\d{4}-\d{5}$/);
  await expect(page.getByRole("heading", { name: "曦和精密", exact: true }).first()).toBeVisible();
  await expect(property(page, "Amount")).toContainText("300,000.00");
  await expect(page.getByRole("main").getByRole("link", { name: "曦和精密", exact: true }).first()).toBeVisible();
  await expect(page.getByRole("main").getByRole("link", { name: "周宁", exact: true }).first()).toBeVisible();
});

test("official tenant: a rep sees only their own leads and deals while the sales manager sees everyone's", async () => {
  await as("rep-2");
  await crm("Leads");
  await expect(page.getByRole("main", { name: "Leads", exact: true }).getByText("周宁", { exact: true })).toHaveCount(0);
  await crm("Deals");
  await expect(page.getByRole("main", { name: "Deals", exact: true }).getByText("曦和精密", { exact: true })).toHaveCount(0);
  await expect(page.getByRole("navigation", { name: "Primary navigation" }).getByRole("link", { name: "Settings", exact: true })).toHaveCount(0);
  for (const builderApp of ["Ontology", "Delivery", "Admin"]) await expect(page.getByRole("link", { name: builderApp, exact: true })).toHaveCount(0);

  await as("manager-1");
  await crm("Deals");
  await page.getByRole("radio", { name: "Table" }).click();
  await expect(page.getByRole("main", { name: "Deals", exact: true }).getByText("曦和精密", { exact: true }).first()).toBeVisible();
  await expect(page.getByRole("navigation", { name: "Primary navigation" }).getByRole("link", { name: "Settings", exact: true })).toBeVisible();
  for (const builderApp of ["Ontology", "Delivery", "Admin"]) await expect(page.getByRole("link", { name: builderApp, exact: true })).toHaveCount(0);
});

test("official tenant: the deal is won and the CRM home counts its revenue", async () => {
  await as("rep-1");
  await crm("Deals");
  await page.getByRole("radio", { name: "Table" }).click();
  await page.getByRole("main").getByRole("link", { name: "曦和精密", exact: true }).first().click();
  await expect(page.getByRole("heading", { name: "曦和精密", exact: true }).first()).toBeVisible();
  await page.getByRole("button", { name: "Mark won", exact: true }).click();
  const win = page.getByRole("dialog", { name: "Mark won", exact: true });
  await win.getByRole("textbox", { name: "Why we won" }).fill("最匹配");
  await win.getByRole("button", { name: "Mark won", exact: true }).click();
  await expect(win).toBeHidden();
  await expect(page.getByRole("main").getByText("Won", { exact: true }).first()).toBeVisible();

  await as("manager-1");
  await page.goto("/");
  await page.getByRole("link", { name: "CRM", exact: true }).first().click();
  await expect(page.getByText("Won revenue", { exact: true })).toBeVisible();
  await expect(page.getByRole("main").getByText(/368,000/).first()).toBeVisible();
});

test("official tenant: CRM upgrades through the marketplace with an optional field and a renamed label", async () => {
  test.setTimeout(90_000);
  await as("rep-1");
  await crm("Leads");
  await page.getByRole("button", { name: "New", exact: true }).click();
  const lead = page.getByRole("dialog");
  await lead.getByRole("textbox", { name: "Full name" }).fill("升级演练线索");
  await lead.getByRole("button", { name: "Create", exact: true }).click();
  await expect(page.getByRole("heading", { name: "升级演练线索", exact: true }).first()).toBeVisible();
  const record = page.url();
  await edit(page, "Company");
  const company = page.getByRole("textbox", { name: "Company", exact: true });
  await company.fill("升级演练公司");
  await company.press("Enter");
  await expect(property(page, "Company")).toContainText("升级演练公司");

  await as("admin-1");
  await page.goto("/delivery/marketplace");
  await page.getByRole("button", { name: "More actions", exact: true }).click();
  await page.getByRole("menuitem", { name: "Install from URL or file", exact: true }).click();
  const wizard = page.getByRole("dialog", { name: /Install from URL or file/ });
  const next = fileURLToPath(new URL("../../scripts/frontend-fixtures/crm-official-0.3.2.json", import.meta.url));
  await wizard.locator('input[type="file"]').setInputFiles({ name: "crm-0.3.2.json", mimeType: "application/json", buffer: readFileSync(next) });
  await wizard.getByRole("button", { name: "Inspect package", exact: true }).click();
  await expect(wizard.getByText("crm · 0.3.2", { exact: true })).toBeVisible();
  await wizard.getByRole("button", { name: "Add to library", exact: true }).click();
  await expect(wizard.getByRole("button", { name: "Continue", exact: true })).toBeEnabled({ timeout: 30_000 });
  await wizard.getByRole("button", { name: "Cancel", exact: true }).click();
  await expect(wizard).toBeHidden();

  const card = page.getByRole("listitem").filter({ hasText: /^CRM/ }).filter({ hasText: "crm · 0.3.2" });
  await card.getByRole("button", { name: "Upgrade", exact: true }).click();
  const review = page.getByRole("dialog", { name: /Review and install/ });
  await expect(review.getByText(/\+ externalref/)).toBeVisible();
  await expect(review.getByText(/~ company \(Company → Company name\)/)).toBeVisible();
  await review.getByRole("button", { name: "Upgrade package", exact: true }).click();
  await expect(review).toBeHidden({ timeout: 30_000 });
  await page.goto("/delivery/installations");
  await expect(page.getByRole("listitem").filter({ hasText: /^CRM/ })).toContainText("0.3.2");

  await as("rep-1");
  await page.goto(record);
  await expect(property(page, "Company name")).toContainText("升级演练公司");
  await edit(page, "External reference");
  const external = page.getByRole("textbox", { name: "External reference", exact: true });
  await external.fill("UPGRADE-001");
  await external.press("Enter");
  await expect(property(page, "External reference")).toContainText("UPGRADE-001");
  await page.reload();
  await expect(property(page, "External reference")).toContainText("UPGRADE-001");
});
});
