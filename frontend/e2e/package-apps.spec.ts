import { expect, test } from "@playwright/test";
import { open, property, edit, signIn, submit } from "./support";
import { selectPackage } from "./package-inputs";

test.describe.serial("publisher tenant", () => {
test("publisher tenant: a master data package installs under Admin → Master data instead of the application rail", async ({ page }) => {

  await signIn(page, "admin-1 · publisher");

  await open(page, "Delivery", "Marketplace");
  await page.getByRole("button", { name: "More actions", exact: true }).click();
  await page.getByRole("menuitem", { name: "Install from URL or file", exact: true }).click();
  const wizard = page.getByRole("dialog", { name: /Install from URL or file/ });
  const release = await selectPackage(wizard, "uom");
  await wizard.getByRole("button", { name: "Inspect package", exact: true }).click();
  await expect(wizard.getByText("uom · 0.1.1", { exact: true })).toBeVisible();
  await expect(wizard.getByText(release.url, { exact: true }).first()).toBeVisible();
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

});
