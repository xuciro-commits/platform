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

test("official tenant: an extension project adds fields, an action and columns to CRM leads under its own access", async () => {
  test.setTimeout(120_000);
  const grantRole = async (memberId: string, appTitle: string, roleLabel: string) => {
    await page.goto(`/admin/members?member=${encodeURIComponent(memberId)}`);
    await page.getByRole("combobox", { name: "Application", exact: true }).click();
    await page.getByRole("option", { name: appTitle, exact: true }).click();
    await page.getByRole("combobox", { name: "Role", exact: true }).click();
    await page.getByRole("option", { name: roleLabel, exact: true }).click();
    await page.getByRole("button", { name: "Grant role", exact: true }).click();
    await expect(page.getByText(`${appTitle} · ${roleLabel}`, { exact: true })).toBeVisible();
  };

  await as("admin-1");
  await grantRole("admin-1", "Build", "Builder");

  await page.goto("/home/projects");
  await page.getByRole("button", { name: "New", exact: true }).click();
  const projectDialog = page.getByRole("dialog", { name: "New project", exact: true });
  await projectDialog.getByRole("radio", { name: "Extension", exact: true }).click();
  await projectDialog.getByRole("combobox", { name: /^Target application/ }).click();
  await page.getByRole("option", { name: "CRM", exact: true }).click();
  await projectDialog.getByRole("textbox", { name: /^API name/ }).fill("crmext");
  await projectDialog.getByRole("textbox", { name: /^Name/ }).fill("CRM qualification");
  await projectDialog.getByRole("button", { name: "Create", exact: true }).click();
  await expect(page.getByRole("heading", { level: 1, name: "CRM qualification", exact: true })).toBeVisible();
  await expect(page.getByRole("link", { name: "CRM qualification", exact: true })).toHaveCount(0);

  await page.goto("/home/overview");
  await page.getByRole("link", { name: "Ontology", exact: true }).first().click();
  await page.getByRole("navigation", { name: "Primary navigation" }).getByRole("link", { name: "Object types", exact: true }).click();
  await page.getByRole("button", { name: "New", exact: true }).click();
  const objectDialog = page.getByRole("dialog");
  if (await objectDialog.getByRole("combobox", { name: /^Project/ }).count()) {
    await objectDialog.getByRole("combobox", { name: /^Project/ }).click();
    await page.getByRole("option", { name: "CRM qualification", exact: true }).click();
  }
  await objectDialog.getByRole("combobox", { name: /^Extends/ }).click();
  await page.getByRole("option", { name: "Lead · crm.lead", exact: true }).click();
  await objectDialog.getByRole("textbox", { name: /^API name/ }).fill("leadext");
  await objectDialog.getByRole("textbox", { name: /^Name/ }).fill("Lead qualification");
  await objectDialog.getByRole("button", { name: "Create", exact: true }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Lead qualification", exact: true })).toBeVisible();
  await expect(page.getByRole("cell", { name: "base", exact: true })).toBeVisible();

  for (const [name, label] of [["sourcechannel", "Source channel"], ["verifiednote", "Qualification note"]] as const) {
    await page.getByRole("button", { name: "Add field", exact: true }).click();
    const fieldDialog = page.getByRole("dialog");
    await fieldDialog.getByRole("textbox", { name: /^Name/ }).fill(name);
    await fieldDialog.getByRole("textbox", { name: /^Label/ }).fill(label);
    await fieldDialog.getByRole("combobox", { name: /^Type/ }).click();
    await page.getByRole("option", { name: "text", exact: true }).click();
    await fieldDialog.getByRole("button", { name: "Save", exact: true }).click();
    await expect(fieldDialog).toBeHidden();
  }

  await page.getByRole("button", { name: "Add access rule", exact: true }).click();
  const repRule = page.getByRole("dialog");
  await repRule.getByRole("textbox", { name: /^Role/ }).fill("salesrep");
  await repRule.getByRole("checkbox", { name: "Create", exact: true }).click();
  await repRule.getByRole("checkbox", { name: "Edit", exact: true }).click();
  await repRule.getByRole("button", { name: "Save", exact: true }).click();
  await expect(repRule).toBeHidden();

  await page.getByRole("button", { name: "Add access rule", exact: true }).click();
  const managerRule = page.getByRole("dialog");
  await managerRule.getByRole("textbox", { name: /^Role/ }).fill("salesmanager");
  await managerRule.getByRole("button", { name: "Save", exact: true }).click();
  await expect(managerRule).toBeHidden();

  await page.getByRole("button", { name: "Create action", exact: true }).click();
  const actionDialog = page.getByRole("dialog", { name: "Create action", exact: true });
  await actionDialog.getByRole("textbox", { name: /^API name/ }).fill("verify");
  await actionDialog.getByRole("textbox", { name: /^What people call it/ }).fill("Verify qualification");
  await actionDialog.getByRole("combobox", { name: /^Field to update/ }).click();
  await page.getByRole("option", { name: "Qualification note", exact: true }).click();
  await actionDialog.getByRole("combobox", { name: /^Taken by/ }).click();
  await page.getByRole("option", { name: "salesrep", exact: true }).click();
  await actionDialog.getByRole("heading", { name: "Create action", exact: true }).click();
  await actionDialog.getByRole("button", { name: "Save", exact: true }).click();
  await expect(actionDialog).toBeHidden();
  await expect(page.getByText("Transitions use the linked action types.", { exact: true })).toBeVisible();

  await page.getByRole("button", { name: "Publish", exact: true }).click();
  const publish = page.getByRole("dialog", { name: /Publish/ });
  await publish.getByRole("button", { name: "Publish", exact: true }).click();
  await expect(publish).toBeHidden();
  await expect(page.getByText("Published", { exact: true }).first()).toBeVisible();

  await grantRole("rep-1", "Crmext", "salesrep");
  await grantRole("manager-1", "Crmext", "salesmanager");

  await page.goto("/delivery/installations");
  await expect(page.getByRole("listitem").filter({ hasText: /^CRM/ })).toContainText("0.3.2");

  await as("rep-1");
  await crm("Leads");
  await page.getByRole("button", { name: "New", exact: true }).click();
  const newLead = page.getByRole("dialog");
  await newLead.getByRole("textbox", { name: "Full name" }).fill("陆衡");
  await newLead.getByRole("button", { name: "Create", exact: true }).click();
  await expect(page.getByRole("heading", { name: "陆衡", exact: true }).first()).toBeVisible();
  await expect(page.getByRole("heading", { name: "Lead qualification", exact: true })).toBeVisible();

  await edit(page, "Source channel");
  const channelInput = page.getByRole("textbox", { name: "Source channel", exact: true });
  await channelInput.fill("行业峰会");
  await channelInput.press("Enter");
  await expect(property(page, "Source channel")).toContainText("行业峰会");

  await page.getByRole("button", { name: "More actions", exact: true }).click();
  await page.getByRole("menuitem", { name: "Verify qualification", exact: true }).click();
  const verifyDialog = page.getByRole("dialog", { name: "Verify qualification", exact: true });
  await verifyDialog.getByRole("textbox", { name: /^Qualification note/ }).fill("预算与决策链已核实");
  await verifyDialog.getByRole("button", { name: "Verify qualification", exact: true }).click();
  await expect(verifyDialog).toBeHidden();
  await expect(property(page, "Qualification note")).toContainText("预算与决策链已核实");
  await page.reload();
  await expect(property(page, "Source channel")).toContainText("行业峰会");
  await expect(property(page, "Qualification note")).toContainText("预算与决策链已核实");

  await crm("Leads");
  await page.getByRole("radio", { name: "Table" }).click();
  await page.getByRole("button", { name: "View options", exact: true }).click();
  await page.getByRole("button", { name: "Add Source channel", exact: true }).click();
  await page.getByRole("button", { name: "Add Qualification note", exact: true }).click();
  await page.keyboard.press("Escape");
  await expect(page.getByRole("columnheader", { name: "Source channel", exact: true })).toBeVisible();
  await expect(page.getByRole("cell", { name: "行业峰会", exact: true })).toBeVisible();
  await expect(page.getByRole("cell", { name: "预算与决策链已核实", exact: true })).toBeVisible();

  await as("manager-1");
  await crm("Leads");
  await page.getByRole("radio", { name: "Table" }).click();
  await page.getByRole("button", { name: "View options", exact: true }).click();
  await page.getByRole("button", { name: "Add Source channel", exact: true }).click();
  await page.keyboard.press("Escape");
  await expect(page.getByRole("cell", { name: "行业峰会", exact: true })).toBeVisible();
  await page.getByRole("main").getByRole("link", { name: "陆衡", exact: true }).first().click();
  await expect(page.getByRole("heading", { name: "Lead qualification", exact: true })).toBeVisible();
  await expect(property(page, "Source channel")).toContainText("行业峰会");
  await expect(property(page, "Qualification note")).toContainText("预算与决策链已核实");
  await expect(page.getByRole("button", { name: "Edit Source channel", exact: true })).toHaveCount(0);
  const managerMenu = page.getByRole("button", { name: "More actions", exact: true });
  if (await managerMenu.count()) {
    await managerMenu.click();
    await expect(page.getByRole("menuitem", { name: "Verify qualification", exact: true })).toHaveCount(0);
    await page.keyboard.press("Escape");
  }

  await as("rep-2");
  await crm("Leads");
  await page.getByRole("radio", { name: "Table" }).click();
  await page.getByRole("button", { name: "View options", exact: true }).click();
  await expect(page.getByRole("button", { name: "Add Source channel", exact: true })).toHaveCount(0);
  await page.keyboard.press("Escape");
  await page.getByRole("button", { name: "New", exact: true }).click();
  const otherLead = page.getByRole("dialog");
  await otherLead.getByRole("textbox", { name: "Full name" }).fill("沈岚");
  await otherLead.getByRole("button", { name: "Create", exact: true }).click();
  await expect(page.getByRole("heading", { name: "沈岚", exact: true }).first()).toBeVisible();
  await expect(page.getByRole("heading", { name: "Lead qualification", exact: true })).toHaveCount(0);
  const rep2Menu = page.getByRole("button", { name: "More actions", exact: true });
  if (await rep2Menu.count()) {
    await rep2Menu.click();
    await expect(page.getByRole("menuitem", { name: "Verify qualification", exact: true })).toHaveCount(0);
    await page.keyboard.press("Escape");
  }
  const deniedStatus = await page.evaluate(async () => {
    const session = JSON.parse(sessionStorage.getItem("platform:session")!) as { token: string; tenant: string };
    const response = await fetch("/v1/records/crmext.leadext?limit=5", {
      headers: { Authorization: `Bearer ${session.token}`, "Platform-Tenant": session.tenant },
    });
    return response.status;
  });
  expect(deniedStatus).toBeGreaterThanOrEqual(400);
});
});
