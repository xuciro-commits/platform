import { createHash } from "node:crypto";
import { readFileSync } from "node:fs";
import { createServer } from "node:http";
import type { AddressInfo } from "node:net";
import { fileURLToPath } from "node:url";
import { type Locator, type Page, expect, test } from "@playwright/test";
import { open, property, edit, signIn, submit } from "./support";

const chooseRole = async (page: Page, pane: Locator, role: RegExp) => {
  await pane.getByRole("combobox", { name: /^Application/ }).click();
  await page.getByRole("option", { name: "CRM", exact: true }).click();
  await pane.getByRole("combobox", { name: /^Role/ }).click();
  await page.getByRole("option", { name: role }).click();
};
const grantRole = async (page: Page, pane: Locator, role: RegExp) => {
  await chooseRole(page, pane, role);
  await pane.getByRole("button", { name: "Grant role", exact: true }).click();
  await expect(pane.getByRole("listitem").filter({ hasText: role })).toBeVisible();
};

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
  await signIn(page, "admin-1 · blank");
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
  await memberPane.getByRole("combobox", { name: /^Application/ }).click();
  await page.getByRole("option", { name: "CRM", exact: true }).click();
  await memberPane.getByRole("combobox", { name: /^Role/ }).click();
  await expect(page.getByRole("option", { name: /salesrep/ })).toBeVisible();
  await expect(page.getByRole("option", { name: /salesmanager/ })).toBeVisible();
  await expect(page.getByRole("option", { name: /admin|builder/i })).toHaveCount(0);
  await page.keyboard.press("Escape");
  await grantRole(page, memberPane, /salesrep/);

  await signOut();
  await signIn(page, "user-1 · blank");
  await expect(page.getByRole("link", { name: "CRM", exact: true }).first()).toBeVisible();
  await page.getByRole("link", { name: "CRM", exact: true }).first().click();
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
  await signIn(page, "admin-1 · blank");
  await open(page, "Admin", "Members");
  await page.getByRole("row", { name: /田雪/ }).click();
  await memberPane.getByRole("listitem").filter({ hasText: /salesrep/ }).getByRole("button", { name: "Revoke", exact: true }).click();
  await expect(memberPane.getByRole("listitem").filter({ hasText: /salesrep/ })).toHaveCount(0);

  await signOut();
  await signIn(page, "user-1 · blank");
  await expect(page.getByRole("link", { name: "CRM", exact: true })).toHaveCount(0);

  await signOut();
  await signIn(page, "admin-1 · blank");
  await open(page, "Admin", "Members");
  await page.getByRole("row", { name: /田雪/ }).click();
  await grantRole(page, memberPane, /salesrep/);

  await page.getByRole("row", { name: /赵敏/ }).click();
  await grantRole(page, memberPane, /salesmanager/);
});

test("blank tenant: the sales rep runs a lead to Won and the sales manager sees it on the board and the dashboard", async () => {
  await signOut();
  await signIn(page, "user-1 · blank");
  await page.getByRole("link", { name: "CRM", exact: true }).first().click();
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
  await convert.getByRole("heading", { name: "Convert to deal", exact: true }).click();
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
  await signIn(page, "manager-1 · blank");
  await page.getByRole("link", { name: "CRM", exact: true }).first().click();
  await nav.getByRole("link", { name: "Deals", exact: true }).click();
  await page.getByRole("radio", { name: "Board" }).click();
  await expect(page.getByRole("region", { name: "Won", exact: true }).getByRole("heading", { level: 4, name: "星海制造", exact: true })).toBeVisible();
  await page.goto("/crm/dashboard");
  await expect(page.getByRole("main").getByRole("link", { name: "Won deals", exact: true }).locator("..")).toHaveText(/\D1$/);
});

test("blank tenant: retiring CRM removes its entry and keeps the records, reinstalling brings both back", async () => {
  await signOut();
  await signIn(page, "admin-1 · blank");
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
  await signIn(page, "user-1 · blank");
  await expect(page.getByRole("link", { name: "CRM", exact: true }).first()).toBeVisible();
  await page.getByRole("link", { name: "CRM", exact: true }).first().click();
  await nav.getByRole("link", { name: "Deals", exact: true }).click();
  await expect(page.getByRole("link", { name: "星海制造", exact: true }).first()).toBeVisible();
});
});

test.describe.serial("upgrade tenant", () => {
const previous = fileURLToPath(new URL("../../scripts/frontend-fixtures/crm-0.1.21.json", import.meta.url));

test("upgrade tenant: install CRM from a local package file after checking its origin and SHA-256", async ({ page }) => {
  const bytes = readFileSync(previous);
  const sha256 = createHash("sha256").update(bytes).digest("hex");
  await signIn(page, "admin-1 · upgrade");

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
  const enter = () => signIn(page, "admin-1 · upgrade");
  await enter();
  await open(page, "Admin", "Members");
  await page.getByRole("row", { name: /王丹/ }).click();
  const memberPane = page.getByRole("dialog", { name: "Details" }).or(page.getByRole("complementary", { name: "Details" }));
  await grantRole(page, memberPane, /salesrep/);

  await page.getByRole("button", { name: "Account", exact: true }).click();
  await page.getByRole("menuitem", { name: "Sign out", exact: true }).click();
  await enter();
  const nav = page.getByRole("navigation", { name: "Primary navigation" });
  await page.getByRole("link", { name: "CRM", exact: true }).first().click();
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

test("upgrade tenant: upgrading the original party package retains the unstructured address", async ({ page }) => {
  test.setTimeout(120_000);
  await signIn(page, "admin-1 · upgrade");
  const importFixture = async (name: string, version: string, action: "Install package" | "Upgrade package") => {
    await page.goto("/delivery/marketplace");
    await page.getByRole("button", { name: "More actions", exact: true }).click();
    await page.getByRole("menuitem", { name: "Install from URL or file", exact: true }).click();
    const wizard = page.getByRole("dialog", { name: /Install from URL or file/ });
    const file = fileURLToPath(new URL(`../../scripts/frontend-fixtures/${name}-${version}.json`, import.meta.url));
    await wizard.locator('input[type="file"]').setInputFiles(file);
    await wizard.getByRole("button", { name: "Inspect package", exact: true }).click();
    await expect(wizard.getByText(`${name} · ${version}`, { exact: true })).toBeVisible();
    await wizard.getByRole("button", { name: "Add to library", exact: true }).click();
    await wizard.getByRole("button", { name: "Continue", exact: true }).click({ timeout: 30_000 });
    await wizard.getByRole("button", { name: action, exact: true }).click();
    await expect(wizard).toBeHidden({ timeout: 30_000 });
  };
  await importFixture("party", "0.1.0", "Install package");
  await submit(page, "platform.member.grant", "platform.member", "admin-1", { app: "party", role: "user" });
  await page.goto("/party/party.organization");
  await page.getByRole("button", { name: "New", exact: true }).click();
  const form = page.getByRole("dialog");
  await form.getByRole("textbox", { name: /^Name(?: Required)?$/ }).fill("Legacy address company");
  await form.getByRole("button", { name: "Create", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Legacy address company", exact: true }).first()).toBeVisible();
  const record = page.url();
  await edit(page, "Address");
  const address = page.getByRole("textbox", { name: "Address", exact: true });
  await address.fill("Legacy address 42");
  await address.press("Tab");
  await expect(property(page, "Address")).toContainText("Legacy address 42");

  await importFixture("geo", "0.1.2", "Install package");
  await importFixture("party", "0.2.2", "Upgrade package");
  await page.goto(record);
  await expect(property(page, "Other address details")).toContainText("Legacy address 42");
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
    await signIn(page, "admin-1 · library");
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
    await choose("Domain filter", "Sales");
    await expect(remote).toBeVisible();
    await expect(demo).toHaveCount(0);
    await expect(page.getByRole("listitem").filter({ hasText: "crm · 0.1.22" })).toHaveCount(0);
    await choose("Domain filter", "All domains");
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

test.describe.serial("publisher tenant", () => {
test("publisher tenant: a master data package installs under Admin → Master data instead of the application rail", async ({ page }) => {
  const bytes = readFileSync(fileURLToPath(new URL("../../scripts/frontend-fixtures/uom-0.1.1.json", import.meta.url)));
  await signIn(page, "admin-1 · publisher");

  await open(page, "Delivery", "Marketplace");
  await page.getByRole("button", { name: "More actions", exact: true }).click();
  await page.getByRole("menuitem", { name: "Install from URL or file", exact: true }).click();
  const wizard = page.getByRole("dialog", { name: /Install from URL or file/ });
  await wizard.locator('input[type="file"]').setInputFiles({ name: "uom-0.1.1.json", mimeType: "application/json", buffer: bytes });
  await wizard.getByRole("button", { name: "Inspect package", exact: true }).click();
  await expect(wizard.getByText("uom · 0.1.1", { exact: true })).toBeVisible();
  await wizard.getByRole("button", { name: "Add to library", exact: true }).click();
  await wizard.getByRole("button", { name: "Continue", exact: true }).click({ timeout: 20_000 });
  await wizard.getByRole("button", { name: "Install package", exact: true }).click();
  await expect(wizard).toBeHidden({ timeout: 30_000 });

  await page.goto("/");
  await open(page, "Admin", "Master data");
  const list = page.getByRole("main", { name: "Master data", exact: true });
  await expect(list.getByRole("link", { name: "Units", exact: true })).toBeVisible();
  await expect(page.getByRole("link", { name: "Units of measure", exact: true })).toHaveCount(0);
  await list.getByRole("link", { name: "Units", exact: true }).click();
  await expect(page.getByRole("main").getByText("kg", { exact: true }).first()).toBeVisible();

  await open(page, "Delivery", "Marketplace");
  await page.getByRole("combobox", { name: "Kind filter", exact: true }).click();
  await page.getByRole("option", { name: "Master data package", exact: true }).click();
  const card = page.getByRole("listitem").filter({ hasText: "Units of measure" }).filter({ hasText: "Local" });
  await expect(card).toContainText("Master data package");
  await expect(page.getByRole("listitem").filter({ hasText: "Application package" })).toHaveCount(0);
  await card.click();
  await expect(page.getByRole("dialog").getByText("Listed under Admin → Master data; it adds no application to the rail.", { exact: true })).toBeVisible();
});

test("publisher tenant: projects lays its tasks on the Gantt chart and hours logged against a task add up on it", async ({ page }) => {
  await signIn(page, "admin-1 · publisher");
  const view = { version: 1, layout: "gantt", dates: "startdate", until: "enddate", after: "after", columns: ["title", "project", "stage", "assignee", "startdate", "enddate", "hours", "estimate"],
    sort: [{ sortKey: "startdate", direction: "ascending" }], lanes: "stage", group: "none", filters: [], search: "", month: "2026-11" };
  await page.goto(`/projects/projects.task?view=${encodeURIComponent(JSON.stringify(view))}`);
  await expect(page.getByRole("radio", { name: "Gantt", exact: true })).toBeChecked();
  await expect(page.getByRole("button", { name: "Wireframes (2026-11-19 – 2026-11-27)", exact: true })).toBeVisible();
  await expect(page.getByRole("img", { name: "Dependency: Discovery workshop → Wireframes", exact: true })).toBeVisible();

  await page.goto("/projects/projects.timelog");
  await page.getByRole("button", { name: "New", exact: true }).click();
  const entry = page.getByRole("dialog");
  await entry.getByRole("combobox", { name: /^Task(?: Required)?$/ }).fill("Wireframes");
  await page.getByRole("option", { name: /Wireframes/ }).click();
  const hours = entry.getByRole("spinbutton", { name: /^Hours(?: Required)?$/ });
  await hours.fill("2.5");
  await hours.press("Tab");
  await entry.getByRole("button", { name: "Create", exact: true }).click();
  await expect(entry).toBeHidden();

  await page.goto("/projects/projects.task/demo-task-2");
  await expect(page.getByRole("heading", { level: 1, name: "Wireframes", exact: true })).toBeVisible();
  await expect(property(page, "Logged hours")).toContainText("11.5");
});

test("publisher tenant: the custodian checks an asset out to a member, who sees only that asset and reports a fault on it", async ({ page }) => {
  await signIn(page, "admin-1 · publisher");
  await page.goto("/assets/assets.asset/demo-asset-2");
  await expect(page.getByRole("heading", { level: 1, name: "Design laptop 16", exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Check out", exact: true }).click();
  const checkout = page.getByRole("dialog", { name: "Check out", exact: true });
  await checkout.getByRole("combobox", { name: /^Check out to(?: Required)?$/ }).fill("田雪");
  await page.getByRole("option", { name: /田雪/ }).click();
  await checkout.getByRole("button", { name: "Check out", exact: true }).click();
  await expect(checkout).toBeHidden();
  await expect(property(page, "Checked out to")).toContainText("田雪");

  await signIn(page, "user-1 · publisher");
  await page.goto("/assets/assets.asset");
  await expect(page.getByRole("link", { name: "Design laptop 16", exact: true })).toBeVisible();
  await expect(page.getByRole("link", { name: "Forklift FL-2", exact: true })).toHaveCount(0);
  await page.goto("/assets/assets.maintenance");
  await page.getByRole("button", { name: "New", exact: true }).click();
  const report = page.getByRole("dialog");
  await report.getByRole("textbox", { name: /^Problem or task(?: Required)?$/ }).fill("Battery drains in an hour");
  await report.getByRole("combobox", { name: /^Asset(?: Required)?$/ }).fill("Design");
  await page.getByRole("option", { name: /Design laptop 16/ }).click();
  await report.getByRole("button", { name: "Create", exact: true }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Battery drains in an hour", exact: true })).toBeVisible();

  await signIn(page, "admin-1 · publisher");
  await page.goto("/assets/assets.asset/demo-asset-2");
  await expect(property(page, "Open work orders")).toContainText("1");
});


test("publisher tenant: a business partner stores country and division references and reads their names", async ({ page }) => {
  await signIn(page, "admin-1 · publisher");
  await submit(page, "platform.member.grant", "platform.member", "admin-1", { app: "geo", role: "user" });
  await page.goto("/party/party.organization");
  await page.getByRole("button", { name: "New", exact: true }).click();
  const form = page.getByRole("dialog");
  await form.getByRole("textbox", { name: /^Name(?: Required)?$/ }).fill("Structured address company");
  await form.getByRole("button", { name: "Create", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Structured address company", exact: true }).first()).toBeVisible();

  await edit(page, "Country or region");
  await page.getByRole("combobox", { name: "Country or region", exact: true }).fill("China");
  await page.getByRole("option", { name: /^China$/ }).click();
  await expect(property(page, "Country or region")).toContainText("China");
  await edit(page, "Region");
  await page.getByRole("combobox", { name: "Region", exact: true }).fill("110105");
  await page.getByRole("option", { name: /北京市朝阳区/ }).click();
  await expect(property(page, "Region")).toContainText("北京市朝阳区");
  await edit(page, "Street address");
  const street = page.getByRole("textbox", { name: "Street address", exact: true });
  await street.fill("1 Jianguo Road");
  await street.press("Enter");
  await expect(property(page, "Street address")).toContainText("1 Jianguo Road");
  await page.reload();
  await expect(property(page, "Country or region")).toContainText("China");
  await expect(property(page, "Region")).toContainText("北京市朝阳区");
  await expect(property(page, "Street address")).toContainText("1 Jianguo Road");
});

test.fixme("publisher tenant: CRM 0.3.1 to 0.3.2 review shows RecordComparison diffs, blocks incompatible extensions, and preserves compatible extensions after upgrade", async ({ page }) => {
  test.setTimeout(300_000);
  await signIn(page, "admin-1 · publisher");

  await open(page, "Delivery", "Marketplace");
  await page.getByRole("button", { name: "More actions", exact: true }).click();
  await page.getByRole("menuitem", { name: "Install from URL or file", exact: true }).click();
  const installWizard = page.getByRole("dialog", { name: /Install from URL or file/ });
  const crm31 = fileURLToPath(new URL("../../scripts/frontend-fixtures/crm-official-0.3.1.json", import.meta.url));
  await installWizard.locator('input[type="file"]').setInputFiles({ name: "crm-0.3.1.json", mimeType: "application/json", buffer: readFileSync(crm31) });
  await installWizard.getByRole("button", { name: "Inspect package", exact: true }).click();
  await expect(installWizard.getByText("crm · 0.3.1", { exact: true })).toBeVisible();
  await installWizard.getByRole("button", { name: "Add to library", exact: true }).click();
  await installWizard.getByRole("button", { name: "Continue", exact: true }).click({ timeout: 60_000 });
  await installWizard.getByRole("button", { name: "Install package", exact: true }).click();
  await expect(installWizard).toBeHidden({ timeout: 60_000 });

  await submit(page, "platform.member.grant", "platform.member", "admin-1", { app: "crm", role: "salesrep" });

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

  const createExtensionObject = async (apiName: string, title: string) => {
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
    await objectDialog.getByRole("textbox", { name: /^API name/ }).fill(apiName);
    await objectDialog.getByRole("textbox", { name: /^Name/ }).fill(title);
    await objectDialog.getByRole("button", { name: "Create", exact: true }).click();
    await expect(page.getByRole("heading", { level: 1, name: title, exact: true })).toBeVisible();
  };

  const addTextField = async (name: string, label: string) => {
    await page.getByRole("button", { name: "Add field", exact: true }).click();
    const fieldDialog = page.getByRole("dialog");
    await fieldDialog.getByRole("textbox", { name: /^Name/ }).fill(name);
    await fieldDialog.getByRole("textbox", { name: /^Label/ }).fill(label);
    await fieldDialog.getByRole("combobox", { name: /^Type/ }).click();
    await page.getByRole("option", { name: "text", exact: true }).click();
    await fieldDialog.getByRole("button", { name: "Save", exact: true }).click();
    await expect(fieldDialog).toBeHidden();
    await expect(page.getByRole("cell", { name, exact: true })).toBeVisible();
  };

  const publishCurrentObject = async (republish = false) => {
    if (republish) await expect(page.getByText("Unpublished changes", { exact: true })).toBeVisible();
    await page.getByRole("button", { name: "Publish", exact: true }).click();
    const publishDialog = page.getByRole("dialog", { name: /Publish/ });
    await expect(publishDialog.getByRole("button", { name: "Publish", exact: true })).toBeEnabled({ timeout: 60_000 });
    await publishDialog.getByRole("button", { name: "Publish", exact: true }).click();
    await expect(publishDialog).toBeHidden({ timeout: 60_000 });
    await expect(page.getByText("Published", { exact: true }).first()).toBeVisible();
  };

  await createExtensionObject("leadext", "Lead qualification");
  await addTextField("sourcechannel", "Source channel");
  await addTextField("verifiednote", "Qualification note");

  await page.getByRole("button", { name: "Add access rule", exact: true }).click();
  const repRule = page.getByRole("dialog");
  await repRule.getByRole("textbox", { name: /^Role/ }).fill("salesrep");
  await repRule.getByRole("checkbox", { name: "Create", exact: true }).click();
  await repRule.getByRole("checkbox", { name: "Edit", exact: true }).click();
  await repRule.getByRole("button", { name: "Save", exact: true }).click();
  await expect(repRule).toBeHidden();

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
  await publishCurrentObject();

  await createExtensionObject("conflict", "Lead legacy reference");
  const conflictUrl = page.url();
  await addTextField("externalref", "Legacy reference");
  await publishCurrentObject();

  await submit(page, "platform.member.grant", "platform.member", "admin-1", { app: "crmext", role: "salesrep" });

  await page.goto("/");
  await page.getByRole("link", { name: "CRM", exact: true }).first().click();
  await page.getByRole("navigation", { name: "Primary navigation" }).getByRole("link", { name: "Leads", exact: true }).click();
  await page.getByRole("button", { name: "New", exact: true }).click();
  const leadDialog = page.getByRole("dialog");
  await leadDialog.getByRole("textbox", { name: "Full name" }).fill("林岚");
  await leadDialog.getByRole("button", { name: "Create", exact: true }).click();
  await expect(page.getByRole("heading", { level: 1, name: "林岚", exact: true }).first()).toBeVisible();
  const leadUrl = page.url();

  await edit(page, "Company");
  const companyInput = page.getByRole("textbox", { name: "Company", exact: true });
  await companyInput.fill("远航智造");
  await companyInput.press("Enter");
  await expect(property(page, "Company")).toContainText("远航智造");

  await edit(page, "Source channel");
  const channelInput = page.getByRole("textbox", { name: "Source channel", exact: true });
  await channelInput.fill("生态伙伴");
  await channelInput.press("Enter");
  await expect(property(page, "Source channel")).toContainText("生态伙伴");

  await page.getByRole("button", { name: "More actions", exact: true }).click();
  await page.getByRole("menuitem", { name: "Verify qualification", exact: true }).click();
  const verifyDialog = page.getByRole("dialog", { name: "Verify qualification", exact: true });
  await verifyDialog.getByRole("textbox", { name: /^Qualification note/ }).fill("预算已核实");
  await verifyDialog.getByRole("button", { name: "Verify qualification", exact: true }).click();
  await expect(verifyDialog).toBeHidden();
  await expect(property(page, "Qualification note")).toContainText("预算已核实");

  await page.goto("/delivery/marketplace");
  await page.getByRole("button", { name: "More actions", exact: true }).click();
  await page.getByRole("menuitem", { name: "Install from URL or file", exact: true }).click();
  const nextWizard = page.getByRole("dialog", { name: /Install from URL or file/ });
  const crm32 = fileURLToPath(new URL("../../scripts/frontend-fixtures/crm-official-0.3.2.json", import.meta.url));
  await nextWizard.locator('input[type="file"]').setInputFiles({ name: "crm-0.3.2.json", mimeType: "application/json", buffer: readFileSync(crm32) });
  await nextWizard.getByRole("button", { name: "Inspect package", exact: true }).click();
  await expect(nextWizard.getByText("crm · 0.3.2", { exact: true })).toBeVisible();
  await nextWizard.getByRole("button", { name: "Add to library", exact: true }).click();
  await expect(nextWizard.getByRole("button", { name: "Continue", exact: true })).toBeVisible({ timeout: 30_000 });
  await nextWizard.getByRole("button", { name: "Cancel", exact: true }).click();
  await expect(nextWizard).toBeHidden();

  const card32 = page.getByRole("listitem").filter({ hasText: /^CRM/ }).filter({ hasText: "crm · 0.3.2" });
  await card32.getByRole("button", { name: "Upgrade", exact: true }).click();
  const blockedReview = page.getByRole("dialog", { name: /Review and install/ });
  const leadDiff = blockedReview.getByRole("table", { name: "Changes", exact: true }).filter({ hasText: "externalref" });
  await expect(leadDiff).toBeVisible();
  await expect(leadDiff.getByRole("columnheader", { name: "0.3.1", exact: true })).toBeVisible();
  await expect(leadDiff.getByRole("columnheader", { name: "0.3.2", exact: true })).toBeVisible();
  await expect(leadDiff.getByRole("row", { name: /externalref.*Added/ })).toBeVisible();
  await expect(leadDiff.getByRole("row", { name: /company.*Changed.*Company.*Company name/ })).toBeVisible();

  const compatibleItem = blockedReview.getByRole("listitem").filter({ hasText: "crmext.leadext" });
  await expect(compatibleItem).toContainText("crmext");
  await expect(compatibleItem).toContainText("Shape compatible");
  await expect(compatibleItem).toContainText("sourcechannel, verifiednote");
  await expect(compatibleItem).toContainText("OK");

  const incompatibleItem = blockedReview.getByRole("listitem").filter({ hasText: "crmext.conflict" });
  await expect(incompatibleItem).toContainText("crmext");
  await expect(incompatibleItem).toContainText("Incompatible shape");
  await expect(incompatibleItem).toContainText(/conflicts with base crm\.lead/);
  await expect(incompatibleItem).toContainText("Error");

  await expect(blockedReview.getByText("Loading…", { exact: true })).toHaveCount(0, { timeout: 60_000 });
  await expect(blockedReview.getByText("This version cannot be used as it stands", { exact: true })).toBeVisible();
  await expect(blockedReview.getByText(/crmext\.conflict.*conflicts with base crm\.lead/).first()).toBeVisible();
  await expect(blockedReview.getByRole("button", { name: "Upgrade package", exact: true })).toBeDisabled();
  await blockedReview.getByRole("button", { name: "Cancel", exact: true }).click();
  await expect(blockedReview).toBeHidden();

  await page.goto("/delivery/installations");
  await expect(page.getByRole("listitem").filter({ hasText: /^CRM/ })).toContainText("0.3.1");

  await page.goto(conflictUrl);
  await page.getByRole("button", { name: "Row actions: Legacy reference", exact: true }).click();
  await page.getByRole("menuitem", { name: "Edit", exact: true }).click();
  const editRowDialog = page.getByRole("dialog", { name: "Edit row", exact: true });
  await editRowDialog.getByRole("textbox", { name: /^Name/ }).fill("legacyref");
  await editRowDialog.getByRole("button", { name: "Save", exact: true }).click();
  await expect(editRowDialog).toBeHidden();
  await expect(page.getByRole("cell", { name: "legacyref", exact: true })).toBeVisible();
  await publishCurrentObject(true);

  await page.goto("/delivery/marketplace");
  await card32.getByRole("button", { name: "Upgrade", exact: true }).click();
  const readyReview = page.getByRole("dialog", { name: /Review and install/ });
  await expect(readyReview.getByRole("listitem").filter({ hasText: "crmext.leadext" })).toContainText("OK");
  await expect(readyReview.getByRole("listitem").filter({ hasText: "crmext.conflict" })).toContainText("OK");
  await expect(readyReview.getByRole("button", { name: "Upgrade package", exact: true })).toBeEnabled();
  await readyReview.getByRole("button", { name: "Upgrade package", exact: true }).click();
  await expect(readyReview).toBeHidden({ timeout: 60_000 });

  await page.goto("/delivery/installations");
  await expect(page.getByRole("listitem").filter({ hasText: /^CRM/ })).toContainText("0.3.2");

  await page.goto(leadUrl);
  await expect(property(page, "Company name")).toContainText("远航智造");
  await expect(property(page, "Source channel")).toContainText("生态伙伴");
  await expect(property(page, "Qualification note")).toContainText("预算已核实");

  await page.getByRole("button", { name: "More actions", exact: true }).click();
  await page.getByRole("menuitem", { name: "Verify qualification", exact: true }).click();
  const postUpgradeVerify = page.getByRole("dialog", { name: "Verify qualification", exact: true });
  await postUpgradeVerify.getByRole("textbox", { name: /^Qualification note/ }).fill("升级后复核通过");
  await postUpgradeVerify.getByRole("button", { name: "Verify qualification", exact: true }).click();
  await expect(postUpgradeVerify).toBeHidden();
  await expect(property(page, "Qualification note")).toContainText("升级后复核通过");

  await edit(page, "External reference");
  const externalRef = page.getByRole("textbox", { name: "External reference", exact: true });
  await externalRef.fill("UPG-2026-01");
  await externalRef.press("Enter");
  await expect(property(page, "External reference")).toContainText("UPG-2026-01");

  await page.reload();
  await expect(property(page, "Company name")).toContainText("远航智造");
  await expect(property(page, "Source channel")).toContainText("生态伙伴");
  await expect(property(page, "Qualification note")).toContainText("升级后复核通过");
  await expect(property(page, "External reference")).toContainText("UPG-2026-01");
});
});

