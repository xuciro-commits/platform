import { expect, test } from "@playwright/test";
import { open, property, edit, signIn, submit } from "./support";
import { input, installInput } from "./package-inputs";

test.describe.serial("upgrade tenant", () => {
test("upgrade tenant: install the official CRM release from a freshly downloaded file with SHA-256", async ({ page }) => {
  test.setTimeout(240_000);
  await signIn(page, "admin-1 · upgrade");
  await installInput(page, "crm", "Install package", true);
  const access = page.getByRole("dialog", { name: "Who can use it" });
  await expect(access).toBeVisible();
  await access.getByRole("button", { name: "Skip for now", exact: true }).click();
  await expect(access).toHaveCount(0);
  await open(page, "Delivery", "Installations");
  const crm = page.getByRole("listitem").filter({ hasText: /^CRM/ });
  await expect(crm).toContainText("active");
  await expect(crm).toContainText(input("crm").version);
});

test("upgrade tenant: CRM upgrades to the next authoritative version and the lead keeps its values", async ({ page }) => {
  test.setTimeout(180_000);
  await signIn(page, "admin-1 · upgrade");
  await submit(page, "platform.member.grant", "platform.member", "admin-1", { app: "crm", role: "salesrep" });
  await page.goto("/crm/crm.lead");
  await page.getByRole("button", { name: "New", exact: true }).click();
  const lead = page.getByRole("dialog");
  await lead.getByRole("textbox", { name: "Full name" }).fill("周宁");
  await lead.getByRole("button", { name: "Create", exact: true }).click();
  await expect(page.getByRole("heading", { level: 1, name: "周宁", exact: true })).toBeVisible();
  const record = page.url();
  await edit(page, "Email");
  const email = page.getByRole("textbox", { name: "Email", exact: true });
  await email.fill("zhou.ning@example.com");
  await email.press("Enter");
  await expect(property(page, "Email")).toContainText("zhou.ning@example.com");
  await installInput(page, "crm-next", "Upgrade package");
  await page.goto("/delivery/installations");
  const access = page.getByRole("dialog", { name: "Who can use it" });
  await expect(access).toContainText("Existing role holders keep their access.");
  await access.getByRole("button", { name: "Skip for now", exact: true }).click();
  await expect(access).toHaveCount(0);
  await page.reload();
  await expect(page.getByRole("link", { name: "CRM", exact: true }).first()).toBeVisible();
  await expect(access).toHaveCount(0);
  await expect(page.getByRole("listitem").filter({ hasText: /^CRM/ })).toContainText(input("crm-next").version);
  await page.goto(record);
  await expect(property(page, "Email")).toContainText("zhou.ning@example.com");
});

});
