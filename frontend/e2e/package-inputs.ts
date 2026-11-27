import { type Locator, type Page, expect } from "@playwright/test";
import { packageInput } from "../../scripts/package-fixtures.mjs";

export const input = packageInput;

export async function selectPackage(wizard: Locator, name: string, file = false) {
  const entry = input(name);
  if (entry.url && !file) {
    await wizard.getByRole("radio", { name: "HTTPS address", exact: true }).click();
    await wizard.getByRole("textbox", { name: /Package URL/ }).fill(entry.url);
  } else {
    await wizard.locator('input[type="file"]').setInputFiles({ name: entry.id + ".json", mimeType: "application/json", buffer: entry.bytes });
  }
  await wizard.getByRole("textbox", { name: "Expected SHA-256", exact: true }).fill(entry.sha256);
  return entry;
}

export async function packageWizard(page: Page, name: string, file = false) {
  await page.goto("/delivery/marketplace");
  await page.getByRole("button", { name: "More actions", exact: true }).click();
  await page.getByRole("menuitem", { name: "Install from URL or file", exact: true }).click();
  const wizard = page.getByRole("dialog", { name: /Install from URL or file/ });
  const entry = await selectPackage(wizard, name, file);
  await wizard.getByRole("button", { name: "Inspect package", exact: true }).click();
  await expect(wizard.getByText(`${entry.id} · ${entry.version}`, { exact: true })).toBeVisible({ timeout: 30_000 });
  await expect(wizard.getByText(entry.sha256, { exact: true })).toBeVisible();
  return wizard;
}

export async function addToLibrary(wizard: Locator) {
  await wizard.getByRole("button", { name: /^(Add to library|Install dependencies and continue)$/ }).click();
  await expect(wizard.getByRole("button", { name: "Continue", exact: true })).toBeEnabled({ timeout: 120_000 });
}

export async function installInput(page: Page, name: string, action = "Install package", file = false) {
  const wizard = await packageWizard(page, name, file);
  await addToLibrary(wizard);
  await wizard.getByRole("button", { name: "Continue", exact: true }).click();
  await wizard.getByRole("button", { name: action, exact: true }).click();
  await expect(wizard).toBeHidden({ timeout: 120_000 });
}
