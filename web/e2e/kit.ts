import { expect, test, type Page } from "@playwright/test";

export type { Page };

export { expect, test };

let counter = 0;

export const unique = (prefix: string) => `${prefix}${Date.now().toString(36)}${counter++}`;

export async function signIn(page: Page, subject: string, route = "") {
  await page.addInitScript(token => sessionStorage.setItem("workspace:identity", token), subject);
  await page.goto(`/#${route}`);
  await expect(page.getByRole("button", { name: "Home", exact: true }).first()).toBeVisible();
}
