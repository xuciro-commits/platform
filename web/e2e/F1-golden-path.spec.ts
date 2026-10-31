import { expect, signIn, test, unique, type Page } from "./kit";

async function release(page: Page, projectId: string, kinds: string[]) {
  await page.goto(`/#/project?id=${projectId}`);
  await page.reload();
  await page.getByRole("button", { name: "Verify and release", exact: true }).click();
  const delivery = page.getByRole("region", { name: "Verification and delivery" });
  await delivery.getByRole("button", { name: "Add the drafts of Orders", exact: true }).click();
  const selection = delivery.getByRole("list", { name: "Joint selection" });
  for (const kind of kinds) await expect(selection.getByText(`${kind} ·`).first()).toBeVisible();
  await delivery.getByRole("button", { name: "Check joint candidate", exact: true }).click();
  await delivery.getByRole("button", { name: "Save immutable candidate", exact: true }).click();
  await delivery.getByRole("button", { name: "Activate release", exact: true }).click();
  await expect(page.getByText("Release active for operators.")).toBeVisible({ timeout: 60_000 });
}

test("F1 builder goes from an empty project to a usable application", async ({ page }) => {
  const name = unique("orders");
  const objectName = `${name}order`;
  let projectId = "";

  await test.step("1 create a project", async () => {
    await signIn(page, "builder", "/projects");
    await page.getByRole("button", { name: "New project", exact: true }).click();
    const sheet = page.getByRole("dialog", { name: "Create application" });
    projectId = await sheet.getByRole("textbox", { name: "ID *" }).inputValue();
    await sheet.getByRole("textbox", { name: "Name *" }).fill(name);
    await sheet.getByRole("textbox", { name: "What people call it *" }).fill("Orders");
    await sheet.getByRole("button", { name: "Create", exact: true }).click();
    await expect(page.getByRole("heading", { name: "Next steps" })).toBeVisible();
  });

  await test.step("3 define an object type from the next step", async () => {
    await page.getByRole("button", { name: "New object type", exact: true }).click();
    const sheet = page.getByRole("dialog", { name: "Create object" });
    await sheet.getByRole("textbox", { name: "Name *" }).fill(objectName);
    await sheet.getByRole("textbox", { name: "What people call it *" }).fill("Order");
    await sheet.getByRole("button", { name: "Create", exact: true }).click();
    await expect(page.getByText(`build.${objectName}`, { exact: true })).toBeVisible();
    const sections = page.getByRole("navigation", { name: "Order" });
    await sections.getByRole("button", { name: "Properties", exact: true }).click();
    await page.getByRole("button", { name: "Add a property", exact: true }).click();
    await expect(page.getByRole("textbox", { name: "Name" })).toHaveValue("field");
    await page.getByRole("button", { name: "Close inspector panel" }).click();
    await sections.getByRole("button", { name: "Lifecycle", exact: true }).click();
    await page.getByRole("button", { name: "Add a state", exact: true }).click();
    await page.getByRole("button", { name: "Close inspector panel" }).click();
  });

  await test.step("4 add an action type", async () => {
    await page.getByRole("navigation", { name: "Order" }).getByRole("button", { name: "Actions", exact: true }).click();
    await page.getByRole("button", { name: "Add an action", exact: true }).click();
    await expect(page.getByRole("button", { name: "Open action type", exact: true })).toBeVisible();
    await page.waitForTimeout(2_000);
    await expect(page.getByRole("status").filter({ hasText: "Saved" }).first()).toBeVisible();
  });

  await test.step("4b review and save so pages can bind the object type", async () => {
    await page.getByRole("button", { name: "Review and save", exact: true }).click();
    await page.getByRole("button", { name: "Save immutable candidate", exact: true }).click();
    await page.getByRole("button", { name: "Activate release", exact: true }).click();
    await expect(page.getByText("Release active for operators.")).toBeVisible({ timeout: 60_000 });
  });

  await test.step("5 build a page in Workshop from a template", async () => {
    await page.goto(`/#/project?id=${projectId}`);
    await page.reload();
    await page.getByRole("button", { name: "Open Workshop", exact: true }).click();
    await page.getByRole("button", { name: "Toggle inspector panel" }).click();
    await page.getByRole("button", { name: "New page from template…", exact: true }).click();
    const dialog = page.getByRole("dialog", { name: "New page from template" });
    await dialog.getByRole("button", { name: /^List report/ }).click();
    const picker = dialog.getByRole("combobox", { name: "Object" });
    const option = picker.locator("option", { hasText: `build.${objectName}` }).first();
    await expect(option).toBeAttached();
    await picker.selectOption((await option.getAttribute("value")) ?? "");
    await dialog.getByRole("textbox", { name: "Page name" }).fill(`${name}list`);
    await dialog.getByRole("textbox", { name: "Page title" }).fill("Orders");
    await dialog.getByRole("button", { name: "Create draft in Studio", exact: true }).click();
    await expect(page.getByRole("region", { name: "Page settings" })).toBeVisible();
  });

  await test.step("6 verify and release the project", async () => {
    await release(page, projectId, ["Applications", "Pages"]);
  });

  await test.step("7 the application is in Applications and opens on its page", async () => {
    await page.reload();
    await page.getByRole("button", { name: "Applications", exact: true }).first().click();
    const business = page.getByRole("region", { name: "Business applications" });
    await business.getByRole("button", { name: "Orders", exact: true }).click();
    const app = page.getByRole("region", { name: "Orders" });
    await expect(app.getByRole("heading", { name: "Orders", level: 1 })).toBeVisible();
    await expect(app.getByRole("row", { name: "ID Field State" })).toBeVisible();
    await expect(app.getByText("No orders")).toBeVisible();
  });
});
