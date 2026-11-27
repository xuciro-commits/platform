import { randomUUID } from "node:crypto";
import { expect, test } from "@playwright/test";
import { open, signIn, submit } from "./support";

test("custom role draft survives a real permissions refresh", async ({ page }) => {
  await signIn(page, "admin-1 · audit-a");
  await open(page, "Admin", "Roles & permissions");
  await page.getByRole("tab", { name: /Custom roles/ }).click();
  await page.getByRole("button", { name: "Define custom role", exact: true }).first().click();
  const dialog = page.getByRole("dialog");
  await dialog.getByRole("combobox", { name: /^Application/ }).click();
  await page.getByRole("option", { name: /\(platform\)|^platform$/ }).click();
  const suffix = randomUUID().slice(0, 8);
  const title = "Draft " + suffix;
  const key = "draft-" + suffix;
  const concurrent = "concurrent-" + suffix;
  await dialog.getByRole("textbox", { name: /^Display title/ }).fill(title);
  await dialog.getByRole("textbox", { name: /^Role key/ }).fill(key);
  await dialog.getByRole("button", { name: /^Allowed actions/ }).click();
  await page.getByRole("listbox").getByRole("option", { name: /platform\.member\.grant/ }).click();
  await page.keyboard.press("Escape");
  const save = dialog.getByRole("button", { name: "Save role", exact: true });
  await expect(save).toBeEnabled();

  const refreshed = page.waitForResponse((response) => new URL(response.url()).pathname === "/v1/permissions" && response.ok());
  await submit(page, "platform.role.save", "platform.role", concurrent, {
    app: "platform", title: concurrent, actions: ["platform.member.grant"],
  });
  await (await refreshed).finished();
  await expect(page.getByRole("row").filter({ hasText: concurrent })).toBeAttached();
  await expect(dialog.getByRole("textbox", { name: /^Display title/ })).toHaveValue(title);
  await expect(dialog.getByRole("textbox", { name: /^Role key/ })).toHaveValue(key);
  await expect(save).toBeEnabled();
  await save.click();
  await expect(dialog).toBeHidden();
  await expect(page.getByRole("row").filter({ hasText: title })).toBeVisible();
  await submit(page, "platform.role.remove", "platform.role", key, {});
  await submit(page, "platform.role.remove", "platform.role", concurrent, {});
});
