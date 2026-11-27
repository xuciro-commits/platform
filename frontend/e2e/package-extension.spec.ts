import { expect, test } from "@playwright/test";
import { property, signIn } from "./support";

test("extension project: create an extension object from its project page and gate an action on the base record", async ({ page }) => {
  await signIn(page);
  await page.goto("/home/projects");
  await page.getByRole("button", { name: "New", exact: true }).click();
  const projectDialog = page.getByRole("dialog", { name: "New project", exact: true });
  await projectDialog.getByRole("radio", { name: "Extension", exact: true }).click();
  await projectDialog.getByRole("combobox", { name: /^Target application/ }).click();
  await page.getByRole("option", { name: "CRM", exact: true }).click();
  await projectDialog.getByRole("textbox", { name: /^API name/ }).fill("bluestonecrm");
  await projectDialog.getByRole("textbox", { name: /^Name/ }).fill("Bluestone CRM extension");
  await projectDialog.getByRole("button", { name: "Create", exact: true }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Bluestone CRM extension", exact: true })).toBeVisible();

  const moreMenu = page.getByRole("button", { name: "More actions", exact: true });
  if (await moreMenu.isVisible()) {
    await moreMenu.click();
    await expect(page.getByRole("menuitem", { name: /Save resource/i })).toHaveCount(0);
    await page.keyboard.press("Escape");
  }

  await page.getByRole("button", { name: "Extend an object", exact: true }).click();
  const extendDialog = page.getByRole("dialog", { name: "Extend an object", exact: true });
  await extendDialog.getByRole("combobox", { name: /^Extends/ }).click();
  await page.getByRole("option", { name: "Deal · crm.deal", exact: true }).click();
  await extendDialog.getByRole("textbox", { name: /^API name/ }).fill("dealsurvey");
  await extendDialog.getByRole("textbox", { name: /^Name/ }).fill("Deal survey");
  await extendDialog.getByRole("button", { name: "Create", exact: true }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Deal survey", exact: true })).toBeVisible();

  await page.getByRole("button", { name: "Add field", exact: true }).click();
  const fieldDialog = page.getByRole("dialog");
  await fieldDialog.getByRole("textbox", { name: /^Name/ }).fill("surveyedby");
  await fieldDialog.getByRole("textbox", { name: /^Label/ }).fill("Survey engineer");
  await fieldDialog.getByRole("combobox", { name: /^Type/ }).click();
  await page.getByRole("option", { name: "text", exact: true }).click();
  await fieldDialog.getByRole("button", { name: "Save", exact: true }).click();
  await expect(fieldDialog).toBeHidden();

  await page.getByRole("button", { name: "Create action", exact: true }).click();
  const actionDialog = page.getByRole("dialog", { name: "Create action", exact: true });
  await actionDialog.getByRole("textbox", { name: /^API name/ }).fill("requestsurvey");
  await actionDialog.getByRole("textbox", { name: /^What people call it/ }).fill("Request site survey");
  await actionDialog.getByRole("combobox", { name: /^Field to update/ }).click();
  await page.getByRole("option", { name: "Survey engineer", exact: true }).click();
  await actionDialog.getByRole("combobox", { name: /^Available when/ }).click();
  await page.getByRole("option", { name: "Deal · Stage", exact: true }).click();
  await actionDialog.getByRole("combobox", { name: /^Values/ }).click();
  await page.getByRole("option", { name: "Negotiation", exact: true }).click();
  await page.keyboard.press("Escape");
  await actionDialog.getByRole("button", { name: "Save", exact: true }).click();
  await expect(actionDialog).toBeHidden();

  await page.getByRole("button", { name: "Publish", exact: true }).click();
  const publishDialog = page.getByRole("dialog", { name: /Publish/ });
  await expect(publishDialog.getByRole("button", { name: "Publish", exact: true })).toBeEnabled({ timeout: 30_000 });
  await publishDialog.getByRole("button", { name: "Publish", exact: true }).click();
  await expect(publishDialog).toBeHidden();

  await page.goto("/home/projects/bluestonecrm");
  await expect(page.getByRole("link", { name: /Deal survey/ })).toBeVisible();

  await page.goto("/ontology/objects/crm.deal/demo-deal-4");
  await page.getByRole("button", { name: "More actions", exact: true }).click();
  await page.getByRole("menuitem", { name: "Request site survey", exact: true }).click();
  const blockedSurvey = page.getByRole("dialog", { name: "Request site survey", exact: true });
  await blockedSurvey.getByRole("textbox", { name: /^Survey engineer/ }).fill("周工");
  await blockedSurvey.getByRole("button", { name: "Request site survey", exact: true }).click();
  await expect(blockedSurvey.getByText(/Deal · Stage: Negotiation/)).toBeVisible();
  await blockedSurvey.getByRole("button", { name: "Cancel", exact: true }).click();
  await expect(blockedSurvey).toBeHidden();

  await page.goto("/ontology/objects/crm.deal/demo-deal-1");
  await page.getByRole("button", { name: "More actions", exact: true }).click();
  await page.getByRole("menuitem", { name: "Request site survey", exact: true }).click();
  const allowedSurvey = page.getByRole("dialog", { name: "Request site survey", exact: true });
  await allowedSurvey.getByRole("textbox", { name: /^Survey engineer/ }).fill("周工");
  await allowedSurvey.getByRole("button", { name: "Request site survey", exact: true }).click();
  await expect(allowedSurvey).toBeHidden();
  await expect(property(page, "Survey engineer")).toContainText("周工");
});
