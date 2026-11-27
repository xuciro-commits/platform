import { expect, test } from "@playwright/test";
import { property, edit, signIn, submit } from "./support";
import { installInput } from "./package-inputs";

test("upgrade tenant: upgrading the original party package retains the unstructured address", async ({ page }) => {
  test.setTimeout(120_000);
  await signIn(page, "admin-1 · party-upgrade");
  await installInput(page, "party-previous");
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

  await installInput(page, "geo");
  await installInput(page, "party", "Upgrade package");
  await page.goto(record);
  await expect(property(page, "Other address details")).toContainText("Legacy address 42");
});
