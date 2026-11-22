import { expect, test } from "@playwright/test";
import { open, property, edit } from "./support";

test.describe.serial("blank tenant", () => {
test("blank tenant: install CRM from the marketplace, grant roles, and run the sales mainline to Won", async ({ page }) => {
  test.setTimeout(120000);
  const signOut = async () => {
    await page.getByRole("button", { name: "Account", exact: true }).click();
    await page.getByRole("menuitem", { name: "Sign out", exact: true }).click();
  };

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
  const nav = page.getByRole("navigation", { name: "Primary navigation" });

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
  await expect(page.getByRole("main").getByRole("link", { name: "Won deals", exact: true })).toBeVisible();
  await expect(page.getByRole("main").getByRole("heading", { level: 1, name: "1", exact: true }).first()).toBeVisible();

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
