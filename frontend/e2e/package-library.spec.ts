import { createHash } from "node:crypto";
import { createServer } from "node:http";
import type { AddressInfo } from "node:net";
import { expect, test } from "@playwright/test";
import { open, signIn } from "./support";
import { input } from "./package-inputs";

test.describe.serial("library tenant", () => {

test("library tenant: a remote catalog lists its package beside the built-in index, filters by its listing, and its entry installs after the digest is checked", async ({ page }) => {
  const { bytes, version } = input("production-provider");
  const sha256 = "sha256:" + createHash("sha256").update(bytes).digest("hex");
  const catalog = JSON.stringify({ version: 1, title: "Acme packages", packages: [
    { id: "application.production-provider", version, title: "Acme production", description: "CRM from the Acme library", url: "./crm.json", sha256,
      listing: { industries: ["general"], capabilities: ["sales"], keywords: ["pipeline"], stage: "preview", source: { ecosystem: "frappe", app: "crm", relation: "redesign" } } },
    { id: "acme.demo", version: "1.0.0", title: "Acme demo", url: "./crm.json", sha256, listing: { capabilities: ["hr"], stage: "test" } },
  ] });
  const server = createServer((request, response) => {
    if (request.url === "/catalog.json") response.writeHead(200, { "content-type": "application/json" }).end(catalog);
    else if (request.url === "/crm.json") response.writeHead(200, { "content-type": "application/json" }).end(bytes);
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

    const remote = page.getByRole("listitem").filter({ hasText: "Acme production" });
    await expect(remote).toContainText("Acme packages");
    await expect(remote).toContainText(`application.production-provider · ${version}`);
    await expect(page.getByRole("listitem").filter({ hasText: /^CRM/ }).filter({ hasText: `crm · ${input("crm").version}` })).toBeVisible();
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
    await expect(remote).toBeVisible();
    await choose("Domain filter", "All domains");
    await choose("Stage filter", "Released packages");
    await page.getByRole("textbox", { name: "Search", exact: true }).fill("pipeline");
    await expect(remote).toBeVisible();
    await page.getByRole("textbox", { name: "Search", exact: true }).fill("");

    await remote.getByRole("button", { name: "Install", exact: true }).click();
    const wizard = page.getByRole("dialog", { name: /Install from URL or file/ });
    await expect(wizard.getByText(address.replace("catalog.json", "crm.json"), { exact: true })).toBeVisible({ timeout: 20_000 });
    await expect(wizard.getByText(sha256, { exact: true })).toBeVisible();
    await wizard.getByRole("button", { name: "Add to library", exact: true }).click();
    await expect(wizard.getByRole("textbox", { name: /Project API name/ })).toHaveValue("application.production-provider");
    await wizard.getByRole("button", { name: "Continue", exact: true }).click();
    await wizard.getByRole("button", { name: "Install package", exact: true }).click();
    await expect(wizard).toBeHidden({ timeout: 30_000 });
    await open(page, "Delivery", "Installations");
    await expect(page.getByRole("listitem").filter({ hasText: "Production batch provider" })).toContainText(version);
  } finally {
    server.close();
  }
});
});
