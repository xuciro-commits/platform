import { expect, test } from "@playwright/test";
import { open, signIn } from "./support";

test.describe.serial("ReBAC governance and self-service lifecycle", () => {
  test("self-service request, withdraw, inbox & admin approval, and temporary grant revocation", async ({ page }) => {
    await signIn(page, "user-1 · blank");
    await expect(page.getByText("Request access to an application or project to start working in this workspace.")).toBeVisible();
    await page.getByRole("button", { name: "Request access", exact: true }).click();

    let dialog = page.getByRole("dialog");
    await dialog.getByRole("combobox", { name: /^Application/ }).click();
    await page.getByRole("option", { name: /\(platform\)/ }).click();
    await dialog.getByRole("combobox", { name: /^Role/ }).click();
    await page.getByRole("option", { name: /Auditor/ }).click();
    await dialog.getByRole("radio", { name: "7 days", exact: true }).click();
    await dialog.getByRole("textbox", { name: /^Business reason/ }).fill("Draft request to withdraw");
    await dialog.getByRole("button", { name: "Submit request", exact: true }).click();
    await expect(dialog).toBeHidden();

    await page.getByRole("button", { name: "Account", exact: true }).click();
    await page.getByRole("menuitem", { name: "Settings", exact: true }).click();
    const settings = page.getByRole("dialog", { name: "Settings" });
    await settings.getByRole("navigation", { name: "Settings sections" }).getByText("Access & delegation", { exact: true }).click();
    await expect(settings.getByText(/Draft request to withdraw/)).toBeVisible();
    await settings.getByRole("button", { name: "Withdraw", exact: true }).click();
    await expect(settings.getByText("withdrawn", { exact: false })).toBeVisible();

    await settings.getByRole("button", { name: "Request access", exact: true }).click();
    dialog = page.getByRole("dialog").last();
    await dialog.getByRole("combobox", { name: /^Application/ }).click();
    await page.getByRole("option", { name: /\(platform\)/ }).click();
    await dialog.getByRole("combobox", { name: /^Role/ }).click();
    await page.getByRole("option", { name: /Auditor/ }).click();
    await dialog.getByRole("radio", { name: "30 days", exact: true }).click();
    await dialog.getByRole("textbox", { name: /^Business reason/ }).fill("Quarterly SOC2 compliance review");
    await dialog.getByRole("button", { name: "Submit request", exact: true }).click();
    await settings.getByRole("button", { name: "Close", exact: true }).click();

    await signIn(page, "admin-1 · blank");
    await open(page, "Home", "Inbox");
    const inboxRow = page.getByRole("row").filter({ hasText: /Access request/i }).first();
    await expect(inboxRow).toBeVisible();
    await inboxRow.click();
    await expect(page.getByText("Quarterly SOC2 compliance review", { exact: true })).toBeVisible();
    await page.getByRole("textbox", { name: /^Decision note/ }).fill("Approved via Inbox");
    await page.getByRole("button", { name: "Approve", exact: true }).click();
    await expect(page.getByText("Access request approved.")).toBeVisible();

    await open(page, "Admin", "Access requests");
    await page.getByRole("radio", { name: "Approved", exact: true }).click();
    await expect(page.getByText("Quarterly SOC2 compliance review", { exact: true })).toBeVisible();

    await page.getByRole("tab", { name: /Temporary grants/ }).click();
    const tempRow = page.getByRole("row").filter({ hasText: "user-1" });
    await expect(tempRow).toContainText("Auditor");
    await tempRow.getByRole("button", { name: "Revoke", exact: true }).click();
    await expect(tempRow).toHaveCount(0);
  });

  test("peer temporary role delegation and early revocation in settings", async ({ page }) => {
    await signIn(page, "manager-1 · crm");
    await page.getByRole("button", { name: "Account", exact: true }).click();
    await page.getByRole("menuitem", { name: "Settings", exact: true }).click();
    const settings = page.getByRole("dialog", { name: "Settings" });
    await settings.getByRole("navigation", { name: "Settings sections" }).getByText("Access & delegation", { exact: true }).click();

    await settings.getByRole("combobox", { name: "Colleague", exact: true }).click();
    await page.getByRole("option", { name: /user-1/ }).click();
    await settings.getByRole("textbox", { name: "Handover note", exact: true }).fill("Conference travel");
    await settings.getByRole("button", { name: "Delegate roles", exact: true }).click();
    await expect(page.getByText(/Delegated .* roles until/)).toBeVisible();
    await settings.getByRole("button", { name: "Close", exact: true }).click();

    await signIn(page, "user-1 · crm");
    await page.getByRole("button", { name: "Account", exact: true }).click();
    await page.getByRole("menuitem", { name: "Settings", exact: true }).click();
    const userSettings = page.getByRole("dialog", { name: "Settings" });
    await userSettings.getByRole("navigation", { name: "Settings sections" }).getByText("Access & delegation", { exact: true }).click();
    await expect(userSettings.getByText(/Delegated by/)).toBeVisible();
    await userSettings.getByRole("button", { name: "Close", exact: true }).click();

    await signIn(page, "manager-1 · crm");
    await page.getByRole("button", { name: "Account", exact: true }).click();
    await page.getByRole("menuitem", { name: "Settings", exact: true }).click();
    const managerSettings = page.getByRole("dialog", { name: "Settings" });
    await managerSettings.getByRole("navigation", { name: "Settings sections" }).getByText("Access & delegation", { exact: true }).click();
    await managerSettings.getByRole("combobox", { name: "End an active delegation to", exact: true }).click();
    await page.getByRole("option", { name: /user-1/ }).click();
    await managerSettings.getByRole("button", { name: "End delegation", exact: true }).click();
    await expect(page.getByText("Delegation ended.")).toBeVisible();
    await managerSettings.getByRole("button", { name: "Close", exact: true }).click();
  });

  test("roles & object ReBAC matrix, custom roles, unit inspection, project access, and 9-layer inspector", async ({ page }) => {
    await signIn(page, "admin-1 · acceptance");

    await open(page, "Admin", "Organization");
    const main = page.getByRole("main");
    await main.getByRole("row").filter({ hasText: /^Sales\s*department/ }).click();
    const orgPanel = page.getByRole("complementary", { name: "Organization" });
    await expect(orgPanel.getByRole("heading", { name: "Placed people & members" })).toBeVisible();
    await expect(orgPanel.getByRole("heading", { name: "Unit-scoped role grants" })).toBeVisible();

    await open(page, "Admin", "Roles & permissions");
    await page.getByRole("combobox", { name: "Application or package space", exact: true }).click();
    await page.getByRole("option", { name: /sales/i }).first().click();
    await expect(main.getByRole("heading", { name: "Object relationship scopes (ReBAC)" })).toBeVisible();
    await expect(main.getByRole("row").filter({ hasText: "sales.opportunity" })).toBeVisible();

    await page.getByRole("tab", { name: /Custom roles/ }).click();
    await page.getByRole("button", { name: "Define custom role", exact: true }).first().click();
    const roleDialog = page.getByRole("dialog");
    await roleDialog.getByRole("combobox", { name: /^Application/ }).click();
    await page.getByRole("option", { name: /sales/i }).first().click();
    await roleDialog.getByRole("textbox", { name: /^Display title/ }).fill("Opportunity Creator");
    await roleDialog.getByRole("combobox", { name: /^Allowed actions/ }).click();
    await page.getByRole("listbox").getByRole("option").first().click();
    await page.keyboard.press("Escape");
    await roleDialog.getByRole("button", { name: "Save role", exact: true }).click();
    await expect(roleDialog).toBeHidden();
    const customRow = main.getByRole("row").filter({ hasText: "Opportunity Creator" });
    await expect(customRow).toBeVisible();
    await customRow.getByRole("button", { name: "Remove", exact: true }).click();
    await page.getByRole("alertdialog").getByRole("button", { name: "Remove", exact: true }).click();
    await expect(customRow).toHaveCount(0);

    await open(page, "Home", "Projects");
    await main.getByRole("link", { name: /Sales|sales/ }).first().click();
    await page.getByRole("link", { name: "Access", exact: true }).click();
    await expect(main.getByText(/Owners manage project access/)).toBeVisible();

    await open(page, "Admin", "Policies & inspector");
    await page.getByRole("combobox", { name: "Member", exact: true }).click();
    await page.getByRole("option", { name: /田雪|user-1/ }).first().click();
    await page.getByRole("combobox", { name: "Object type", exact: true }).click();
    await page.getByRole("option", { name: /sales\.opportunity/ }).click();
    await page.getByRole("textbox", { name: "Record ID", exact: true }).fill("OPP-001");
    await page.getByRole("button", { name: "Evaluate access", exact: true }).click();
    await expect(main.getByText("Access allowed", { exact: true })).toBeVisible();
    await expect(main.getByText("Record relationship scope", { exact: true })).toBeVisible();
    await expect(main.getByText("amount", { exact: true })).toBeVisible();

    await page.getByRole("button", { name: "New guardrail policy", exact: true }).click();
    const policyDialog = page.getByRole("dialog");
    await policyDialog.getByRole("textbox", { name: /^Policy title/ }).fill("Block opportunity reads");
    await policyDialog.getByRole("textbox", { name: /^Permission schema or prefix/ }).fill("*");
    await policyDialog.getByRole("combobox", { name: "Restrict to member", exact: true }).click();
    await page.getByRole("option", { name: /田雪|user-1/ }).first().click();
    await policyDialog.getByRole("button", { name: "Save policy", exact: true }).click();
    await expect(policyDialog).toBeHidden();

    await page.getByRole("button", { name: "Evaluate access", exact: true }).click();
    await expect(main.getByText("Access denied", { exact: true })).toBeVisible();

    await page.getByRole("tab", { name: /Guardrail policies/ }).click();
    const polRow = main.getByRole("row").filter({ hasText: "Block opportunity reads" });
    await expect(polRow).toBeVisible();
    await polRow.getByRole("button", { name: "Remove", exact: true }).click();
    await page.getByRole("alertdialog").getByRole("button", { name: "Remove", exact: true }).click();
    await expect(polRow).toHaveCount(0);
  });
});
