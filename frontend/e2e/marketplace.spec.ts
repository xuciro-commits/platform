import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { createServer } from "node:http";
import type { AddressInfo } from "node:net";
import { fileURLToPath } from "node:url";
import { type Locator, type Page, expect, test } from "@playwright/test";
import { open, property, edit } from "./support";

test.describe.serial("blank tenant", () => {
let page: Page;
let nav: Locator;
const signOut = async () => {
  await page.getByRole("button", { name: "Account", exact: true }).click();
  await page.getByRole("menuitem", { name: "Sign out", exact: true }).click();
};
test.beforeAll(async ({ browser }) => {
  page = await browser.newPage();
  nav = page.getByRole("navigation", { name: "Primary navigation" });
});
test.afterAll(async () => {
  await page.close();
});

test("blank tenant: an administrator installs CRM from the marketplace after reviewing its applications and roles", async () => {
  await page.goto("/");
  await page.getByRole("button", { name: "admin-1 · blank" }).click();
  await expect(page.getByRole("button", { name: "Account", exact: true })).toBeVisible();
  await expect(page.getByRole("link", { name: "CRM", exact: true })).toHaveCount(0);

  await open(page, "Delivery", "Marketplace");
  const crm = page.getByRole("listitem").filter({ hasText: /^CRM/ }).first();
  await expect(crm).toBeVisible();
  await crm.getByRole("button", { name: "Install", exact: true }).click();
  const review = page.getByRole("dialog", { name: /Review and install/ });
  await expect(review.getByText("Applications", { exact: true })).toBeVisible();
  await expect(review.getByText("Dependencies", { exact: true })).toBeVisible();
  await expect(review.getByText(/salesrep/)).toBeVisible();
  await expect(review.getByText("Connections", { exact: true })).toBeVisible();
  await review.getByRole("button", { name: "Install package", exact: true }).click();
  await expect(page).toHaveURL(/\/delivery\/installations$/, { timeout: 30_000 });
  await expect(page.getByRole("listitem").filter({ hasText: /^CRM/ })).toContainText("active");
  await expect(page.getByRole("link", { name: "CRM", exact: true })).toHaveCount(0);
});

test("blank tenant: a package role granted to a member opens CRM for them, its empty pipeline shows every stage, and revoking it closes it", async () => {
  await open(page, "Admin", "Members");
  await page.getByRole("row", { name: /田雪/ }).click();
  const memberPane = page.getByRole("dialog", { name: "Details" }).or(page.getByRole("complementary", { name: "Details" }));
  await memberPane.getByRole("combobox", { name: "Package role", exact: true }).click();
  await expect(page.getByRole("option", { name: /salesrep/ })).toBeVisible();
  await expect(page.getByRole("option", { name: /salesmanager/ })).toBeVisible();
  await expect(page.getByRole("option", { name: /admin|builder/i })).toHaveCount(0);
  await page.getByRole("option", { name: /salesrep/ }).click();
  await memberPane.getByRole("button", { name: "Grant role", exact: true }).click();
  await expect(memberPane.getByRole("listitem").filter({ hasText: /salesrep/ })).toBeVisible();

  await signOut();
  await page.getByRole("button", { name: "user-1 · blank" }).click();
  await expect(page.getByRole("link", { name: "CRM", exact: true })).toBeVisible();
  await page.getByRole("link", { name: "CRM", exact: true }).click();
  await nav.getByRole("link", { name: "Deals", exact: true }).click();
  await page.getByRole("radio", { name: "Board" }).click();
  await expect(page.getByRole("region", { name: "Qualification", exact: true })).toContainText("Nothing in this column yet");
  await expect(page.getByRole("region", { name: "Won", exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "New", exact: true })).toBeVisible();
  await nav.getByRole("link", { name: "Leads", exact: true }).click();
  await page.getByRole("button", { name: "New", exact: true }).click();
  const firstLead = page.getByRole("dialog");
  await firstLead.getByRole("textbox", { name: "Full name" }).fill("林海");
  await firstLead.getByRole("combobox", { name: "Status" }).click();
  await page.getByRole("option", { name: "New", exact: true }).click();
  await firstLead.getByRole("button", { name: "Create", exact: true }).click();
  await expect(page.getByRole("heading", { level: 1, name: "林海", exact: true })).toBeVisible();

  await signOut();
  await page.getByRole("button", { name: "admin-1 · blank" }).click();
  await open(page, "Admin", "Members");
  await page.getByRole("row", { name: /田雪/ }).click();
  await memberPane.getByRole("listitem").filter({ hasText: /salesrep/ }).getByRole("button", { name: "Revoke", exact: true }).click();
  await expect(memberPane.getByRole("listitem").filter({ hasText: /salesrep/ })).toHaveCount(0);

  await signOut();
  await page.getByRole("button", { name: "user-1 · blank" }).click();
  await expect(page.getByRole("button", { name: "Account", exact: true })).toBeVisible();
  await expect(page.getByRole("link", { name: "CRM", exact: true })).toHaveCount(0);

  await signOut();
  await page.getByRole("button", { name: "admin-1 · blank" }).click();
  await open(page, "Admin", "Members");
  await page.getByRole("row", { name: /田雪/ }).click();
  await memberPane.getByRole("combobox", { name: "Package role", exact: true }).click();
  await page.getByRole("option", { name: /salesrep/ }).click();
  await memberPane.getByRole("button", { name: "Grant role", exact: true }).click();
  await expect(memberPane.getByRole("listitem").filter({ hasText: /salesrep/ })).toBeVisible();

  await page.getByRole("row", { name: /赵敏/ }).click();
  await memberPane.getByRole("combobox", { name: "Package role", exact: true }).click();
  await page.getByRole("option", { name: /salesmanager/ }).click();
  await memberPane.getByRole("button", { name: "Grant role", exact: true }).click();
  await expect(memberPane.getByRole("listitem").filter({ hasText: /salesmanager/ })).toBeVisible();
});

test("blank tenant: the sales rep runs a lead to Won and the sales manager sees it on the board and the dashboard", async () => {
  await signOut();
  await page.getByRole("button", { name: "user-1 · blank" }).click();
  await page.getByRole("link", { name: "CRM", exact: true }).click();
  await nav.getByRole("link", { name: "Leads", exact: true }).click();
  await page.getByRole("link", { name: "林海", exact: true }).click();
  await edit(page, "Organization");
  const org = page.getByRole("textbox", { name: "Organization", exact: true });
  await org.fill("星海制造");
  await Promise.all([
    page.waitForResponse((response) => response.url().endsWith("/v1/submissions") && response.ok()),
    org.press("Enter"),
  ]);
  await expect(property(page, "Organization")).toContainText("星海制造");
  await edit(page, "Status");
  await page.getByRole("combobox", { name: "Status" }).fill("Contacted");
  await Promise.all([
    page.waitForResponse((response) => response.url().endsWith("/v1/submissions") && response.ok()),
    page.getByRole("option", { name: "Contacted", exact: true }).click(),
  ]);
  await expect(property(page, "Status")).toContainText("Contacted");
  await expect(property(page, "First response")).toContainText("Fulfilled", { timeout: 20_000 });
  await page.getByRole("button", { name: "Convert to deal", exact: true }).click();
  const convert = page.getByRole("dialog", { name: "Convert to deal", exact: true });
  await convert.getByRole("button", { name: "Convert to deal", exact: true }).click();
  await expect(convert).toBeHidden();
  await expect(page.getByRole("heading", { level: 1, name: "星海制造", exact: true })).toBeVisible();
  await nav.getByRole("link", { name: "Deals", exact: true }).click();
  await page.getByRole("radio", { name: "Board" }).click();
  const card = page.getByRole("region", { name: "Qualification", exact: true }).getByRole("heading", { level: 4, name: "星海制造", exact: true });
  await expect(card).toBeVisible();
  const won = page.getByRole("region", { name: "Won", exact: true });
  const from = (await card.boundingBox())!;
  const to = (await won.boundingBox())!;
  await page.mouse.move(from.x + 2, from.y - 4);
  await page.mouse.down();
  await page.mouse.move(from.x + 40, from.y + 20, { steps: 4 });
  await page.mouse.move(to.x + to.width / 2, to.y + to.height / 2, { steps: 8 });
  await page.mouse.up();
  await expect(won.getByRole("heading", { level: 4, name: "星海制造", exact: true })).toBeVisible();

  await signOut();
  await page.getByRole("button", { name: "manager-1 · blank" }).click();
  await page.getByRole("link", { name: "CRM", exact: true }).click();
  await nav.getByRole("link", { name: "Deals", exact: true }).click();
  await page.getByRole("radio", { name: "Board" }).click();
  await expect(page.getByRole("region", { name: "Won", exact: true }).getByRole("heading", { level: 4, name: "星海制造", exact: true })).toBeVisible();
  await page.goto("/crm/dashboard");
  await expect(page.getByRole("main").getByRole("link", { name: "Won deals", exact: true }).locator("..")).toHaveText(/\D1$/);
});

test("blank tenant: retiring CRM removes its entry and keeps the records, reinstalling brings both back", async () => {
  await signOut();
  await page.getByRole("button", { name: "admin-1 · blank" }).click();
  await open(page, "Delivery", "Installations");
  const crmRow = page.getByRole("listitem").filter({ hasText: /^CRM/ });
  await crmRow.getByRole("button", { name: "Manage installation", exact: true }).click();
  const manage = page.getByRole("dialog", { name: /Manage installation/ });
  await expect(manage.getByRole("button", { name: "Retire package", exact: true })).toBeDisabled();
  await manage.getByRole("button", { name: "Mark for uninstall", exact: true }).click();
  await expect(manage).toBeHidden();
  await expect(crmRow).toContainText("draining");
  await crmRow.getByRole("button", { name: "Manage installation", exact: true }).click();
  await manage.getByRole("button", { name: "Retire package", exact: true }).click();
  await expect(manage).toBeHidden();
  await expect(crmRow).toContainText("retired");
  await expect(page.getByRole("link", { name: "CRM", exact: true })).toHaveCount(0);

  await crmRow.getByRole("button", { name: "Reinstall", exact: true }).click();
  const reinstall = page.getByRole("dialog", { name: /Review and install/ });
  await reinstall.getByRole("button", { name: "Reinstall package", exact: true }).click();
  await expect(reinstall).toBeHidden();
  await expect(crmRow).toContainText("active");

  await signOut();
  await page.getByRole("button", { name: "user-1 · blank" }).click();
  await expect(page.getByRole("link", { name: "CRM", exact: true })).toBeVisible();
  await page.getByRole("link", { name: "CRM", exact: true }).click();
  await nav.getByRole("link", { name: "Deals", exact: true }).click();
  await expect(page.getByRole("link", { name: "星海制造", exact: true }).first()).toBeVisible();
});
});

test.describe.serial("upgrade tenant", () => {
const previous = fileURLToPath(new URL("../../scripts/frontend-fixtures/crm-0.1.21.json", import.meta.url));

test("upgrade tenant: install CRM from a local package file after checking its origin and SHA-256", async ({ page }) => {
  const bytes = readFileSync(previous);
  const sha256 = createHash("sha256").update(bytes).digest("hex");
  await page.goto("/");
  await page.getByRole("button", { name: "admin-1 · upgrade" }).click();
  await expect(page.getByRole("button", { name: "Account", exact: true })).toBeVisible();

  await open(page, "Delivery", "Marketplace");
  await page.getByRole("button", { name: "More actions", exact: true }).click();
  await page.getByRole("menuitem", { name: "Install from URL or file", exact: true }).click();
  const wizard = page.getByRole("dialog", { name: /Install from URL or file/ });
  await wizard.locator('input[type="file"]').setInputFiles({ name: "crm-0.1.21.json", mimeType: "application/json", buffer: bytes });
  await wizard.getByRole("button", { name: "Inspect package", exact: true }).click();

  await expect(wizard.getByText("crm · 0.1.21", { exact: true })).toBeVisible();
  await expect(wizard.getByText("Local file", { exact: true }).last()).toBeVisible();
  await expect(wizard.getByText(new RegExp(sha256))).toBeVisible();
  await wizard.getByRole("button", { name: "Add to library", exact: true }).click();

  await expect(wizard.getByRole("textbox", { name: /Project API name/ })).toHaveValue("crm");
  await expect(wizard.getByText(/salesrep/).first()).toBeVisible();
  await wizard.getByRole("button", { name: "Continue", exact: true }).click();
  await wizard.getByRole("button", { name: "Install package", exact: true }).click();
  await expect(wizard).toBeHidden({ timeout: 30_000 });

  await open(page, "Delivery", "Installations");
  const crm = page.getByRole("listitem").filter({ hasText: /^CRM/ });
  await expect(crm).toContainText("active");
  await expect(crm).toContainText("0.1.21");
});

test("upgrade tenant: CRM upgrades to a version with a new optional lead field and the lead keeps its values", async ({ page }) => {
  const signIn = async () => {
    await page.goto("/");
    await page.getByRole("button", { name: "admin-1 · upgrade" }).click();
    await expect(page.getByRole("button", { name: "Account", exact: true })).toBeVisible();
  };
  await signIn();
  await open(page, "Admin", "Members");
  await page.getByRole("row", { name: /王丹/ }).click();
  const memberPane = page.getByRole("dialog", { name: "Details" }).or(page.getByRole("complementary", { name: "Details" }));
  await memberPane.getByRole("combobox", { name: "Package role", exact: true }).click();
  await page.getByRole("option", { name: /salesrep/ }).click();
  await memberPane.getByRole("button", { name: "Grant role", exact: true }).click();
  await expect(memberPane.getByRole("listitem").filter({ hasText: /salesrep/ })).toBeVisible();

  await page.getByRole("button", { name: "Account", exact: true }).click();
  await page.getByRole("menuitem", { name: "Sign out", exact: true }).click();
  await signIn();
  const nav = page.getByRole("navigation", { name: "Primary navigation" });
  await page.getByRole("link", { name: "CRM", exact: true }).click();
  await nav.getByRole("link", { name: "Leads", exact: true }).click();
  await page.getByRole("button", { name: "New", exact: true }).click();
  const lead = page.getByRole("dialog");
  await lead.getByRole("textbox", { name: "Full name" }).fill("周宁");
  await lead.getByRole("combobox", { name: "Status" }).click();
  await page.getByRole("option", { name: "New", exact: true }).click();
  await lead.getByRole("button", { name: "Create", exact: true }).click();
  await expect(page.getByRole("heading", { level: 1, name: "周宁", exact: true })).toBeVisible();
  const record = page.url();
  await edit(page, "Email");
  const email = page.getByRole("textbox", { name: "Email", exact: true });
  await email.fill("zhou.ning@example.com");
  await Promise.all([
    page.waitForResponse((response) => response.url().endsWith("/v1/submissions") && response.ok()),
    email.press("Enter"),
  ]);
  await expect(property(page, "Email")).toContainText("zhou.ning@example.com");

  await open(page, "Delivery", "Marketplace");
  await page.getByRole("listitem").filter({ hasText: /^CRM/ }).getByRole("button", { name: "Upgrade", exact: true }).first().click();
  const review = page.getByRole("dialog", { name: /Review and install/ });
  await expect(review.getByText(/crm\.lead/).first()).toBeVisible();
  await expect(review.getByText(/\+ linkedin/)).toBeVisible();
  await review.getByRole("button", { name: "Upgrade package", exact: true }).click();
  await expect(review).toBeHidden({ timeout: 30_000 });
  await open(page, "Delivery", "Installations");
  await expect(page.getByRole("listitem").filter({ hasText: /^CRM/ })).toContainText("0.1.22");

  await page.goto(record);
  await expect(page.getByRole("heading", { level: 1, name: "周宁", exact: true })).toBeVisible();
  await expect(property(page, "Email")).toContainText("zhou.ning@example.com");
  await edit(page, "LinkedIn");
  const linkedin = page.getByRole("textbox", { name: "LinkedIn", exact: true });
  await linkedin.fill("linkedin.com/in/zhouning");
  await Promise.all([
    page.waitForResponse((response) => response.url().endsWith("/v1/submissions") && response.ok()),
    linkedin.press("Enter"),
  ]);
  await expect(property(page, "LinkedIn")).toContainText("linkedin.com/in/zhouning");
});
});

test.describe.serial("library tenant", () => {
const previous = fileURLToPath(new URL("../../scripts/frontend-fixtures/crm-0.1.21.json", import.meta.url));

test("library tenant: a remote catalog lists its package beside the built-in index, filters by its listing, and its entry installs after the digest is checked", async ({ page }) => {
  const bytes = readFileSync(previous);
  const sha256 = "sha256:" + createHash("sha256").update(bytes).digest("hex");
  const catalog = JSON.stringify({ version: 1, title: "Acme packages", packages: [
    { id: "crm", version: "0.1.21", title: "Acme CRM", description: "CRM from the Acme library", url: "./crm-0.1.21.json", sha256,
      listing: { industries: ["general"], capabilities: ["sales"], keywords: ["pipeline"], stage: "preview", source: { ecosystem: "frappe", app: "crm", relation: "redesign" } } },
    { id: "acme.demo", version: "1.0.0", title: "Acme demo", url: "./crm-0.1.21.json", sha256, listing: { capabilities: ["hr"], stage: "test" } },
  ] });
  const server = createServer((request, response) => {
    if (request.url === "/catalog.json") response.writeHead(200, { "content-type": "application/json" }).end(catalog);
    else if (request.url === "/crm-0.1.21.json") response.writeHead(200, { "content-type": "application/json" }).end(bytes);
    else response.writeHead(404).end();
  });
  await new Promise<void>((resolve) => server.listen(0, "127.0.0.1", resolve));
  const address = `http://127.0.0.1:${(server.address() as AddressInfo).port}/catalog.json`;
  try {
    await page.goto("/");
    await page.getByRole("button", { name: "admin-1 · library" }).click();
    await expect(page.getByRole("button", { name: "Account", exact: true })).toBeVisible();
    await open(page, "Delivery", "Marketplace");
    await page.getByRole("button", { name: "More actions", exact: true }).click();
    await page.getByRole("menuitem", { name: "Manage library", exact: true }).click();
    const manager = page.getByRole("dialog", { name: "Manage library" });
    await expect(manager.getByText("Official", { exact: true })).toBeVisible();
    await expect(manager.getByRole("button", { name: "Add official library", exact: true })).toHaveCount(0);
    await expect(manager.getByRole("button", { name: "Remove", exact: true })).toHaveCount(0);
    await manager.getByRole("textbox", { name: /Library URL/ }).fill(address);
    await manager.getByRole("textbox", { name: /Library name/ }).fill("Acme packages");
    await manager.getByRole("button", { name: "Add library", exact: true }).click();
    await expect(manager.getByText("2 packages", { exact: true })).toBeVisible({ timeout: 20_000 });
    await expect(manager.getByRole("button", { name: "Remove", exact: true })).toHaveCount(1);
    await manager.getByRole("button", { name: "Close", exact: true }).last().click();

    const remote = page.getByRole("listitem").filter({ hasText: "Acme CRM" });
    await expect(remote).toContainText("Acme packages");
    await expect(remote).toContainText("crm · 0.1.21");
    await expect(page.getByRole("listitem").filter({ hasText: /^CRM/ }).filter({ hasText: "crm · 0.1.22" })).toBeVisible();
    await expect(remote).toContainText("Preview");
    await expect(remote).toContainText("Redesigned after crm");
    const demo = page.getByRole("listitem").filter({ hasText: "Acme demo" });
    await expect(demo).toHaveCount(0);
    const choose = async (filter: string, option: string) => {
      await page.getByRole("combobox", { name: filter, exact: true }).click();
      await page.getByRole("option", { name: option, exact: true }).click();
    };
    await choose("Stage filter", "Test packages");
    await expect(demo).toBeVisible();
    await expect(remote).toHaveCount(0);
    await choose("Stage filter", "All stages");
    await choose("Capability filter", "Sales");
    await expect(remote).toBeVisible();
    await expect(demo).toHaveCount(0);
    await expect(page.getByRole("listitem").filter({ hasText: "crm · 0.1.22" })).toHaveCount(0);
    await choose("Capability filter", "All capabilities");
    await choose("Stage filter", "Released packages");
    await page.getByRole("textbox", { name: "Search", exact: true }).fill("pipeline");
    await expect(remote).toBeVisible();
    await page.getByRole("textbox", { name: "Search", exact: true }).fill("");

    await remote.getByRole("button", { name: "Install", exact: true }).click();
    const wizard = page.getByRole("dialog", { name: /Install from URL or file/ });
    await expect(wizard.getByText(address.replace("catalog.json", "crm-0.1.21.json"), { exact: true })).toBeVisible({ timeout: 20_000 });
    await expect(wizard.getByText(sha256, { exact: true })).toBeVisible();
    await wizard.getByRole("button", { name: "Add to library", exact: true }).click();
    await expect(wizard.getByRole("textbox", { name: /Project API name/ })).toHaveValue("crm");
    await wizard.getByRole("button", { name: "Continue", exact: true }).click();
    await wizard.getByRole("button", { name: "Install package", exact: true }).click();
    await expect(wizard).toBeHidden({ timeout: 30_000 });
    await open(page, "Delivery", "Installations");
    await expect(page.getByRole("listitem").filter({ hasText: /^CRM/ })).toContainText("0.1.21");
  } finally {
    server.close();
  }
});
});
