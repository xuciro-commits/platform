import { input, addToLibrary } from "./package-inputs";
import { type Locator, type Page, expect, test } from "@playwright/test";
import { open, signIn, submit } from "./support";

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
  test.setTimeout(180_000);
  await signIn(page, "admin-1 · blank");
  await expect(page.getByRole("link", { name: "CRM", exact: true })).toHaveCount(0);

  await open(page, "Delivery", "Marketplace");
  const crm = page.getByRole("listitem").filter({ hasText: /^CRM/ }).filter({ hasText: `crm · ${input("crm").version}` }).first();
  await expect(crm).toBeVisible({ timeout: 30_000 });
  await crm.getByRole("button", { name: "Install", exact: true }).click();
  const wizard = page.getByRole("dialog", { name: /Install from URL or file/ });
  await expect(wizard.getByText(input("crm").sha256, { exact: true })).toBeVisible({ timeout: 30_000 });
  await addToLibrary(wizard);
  await expect(wizard.getByText(/salesrep/).first()).toBeVisible();
  await wizard.getByRole("button", { name: "Continue", exact: true }).click();
  await wizard.getByRole("button", { name: "Install package", exact: true }).click();
  await expect(wizard).toBeHidden({ timeout: 120_000 });
  await expect(page).toHaveURL(/\/delivery\/installations$/, { timeout: 30_000 });
  await expect(page.getByRole("listitem").filter({ hasText: /^CRM/ })).toContainText("active");
  const access = page.getByRole("dialog", { name: "Who can use it" });
  await expect(access).toContainText("Installing grants no access.");
  await expect(access).toContainText("Nobody holds a role in CRM, Places, Business partners yet");
  for (const app of ["CRM", "Places", "Business partners"]) {
    await expect(access.getByRole("combobox", { name: `Role in ${app}`, exact: true })).toBeVisible();
  }
  await expect(access.getByRole("button", { name: "People", exact: true })).toContainText("(you)");
  await expect(page.getByRole("link", { name: "CRM", exact: true })).toHaveCount(0);
  await access.getByRole("button", { name: "Grant access", exact: true }).click();
  await expect(access).toHaveCount(0);
  await expect(page.getByRole("link", { name: "CRM", exact: true }).first()).toBeVisible();
  for (const member of ["admin-1", "user-1", "manager-1"]) {
    await submit(page, "platform.member.grant", "platform.member", member, { app: "party", role: "user" });
  }
  await submit(page, "platform.package.clear-demo", "platform.package", "crm", { id: "crm" });
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
  await expect(page.getByRole("button", { name: "New", exact: true })).toBeVisible();
  await nav.getByRole("link", { name: "Leads", exact: true }).click();
  await page.getByRole("button", { name: "New", exact: true }).click();
  const firstLead = page.getByRole("dialog");
  await firstLead.getByRole("textbox", { name: "Full name" }).fill("林海");
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
  const access = page.getByRole("dialog", { name: "Who can use it" });
  await expect(access.getByRole("combobox", { name: "Role in CRM" })).toBeVisible();
  await access.getByRole("button", { name: "Skip for now", exact: true }).click();
  await expect(access).toHaveCount(0);
  await expect(page.getByText(/stays hidden from anyone without a role/).first()).toBeVisible();

  await signOut();
  await signIn(page, "user-1 · blank");
  await expect(page.getByRole("link", { name: "CRM", exact: true }).first()).toBeVisible();
  await page.getByRole("link", { name: "CRM", exact: true }).first().click();
  await nav.getByRole("link", { name: "Leads", exact: true }).click();
  await expect(page.getByRole("link", { name: "林海", exact: true }).first()).toBeVisible();
});
});
