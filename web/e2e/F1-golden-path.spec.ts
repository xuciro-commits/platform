import { expect, signIn, test, unique } from "./kit";

test("F1 builder goes from an empty project to an application a business user acts in", async ({ page, browser }) => {
  const name = unique("orders");
  const objectName = `${name}order`;

  await test.step("1 create a project", async () => {
    await signIn(page, "builder", "/projects");
    await page.getByRole("button", { name: "New project", exact: true }).click();
    const sheet = page.getByRole("dialog", { name: "Create application" });
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

  await test.step("5 build a page in Workshop from a template bound to the draft object type", async () => {
    await page.getByRole("button", { name: "Orders", exact: true }).first().click();
    await page.getByRole("button", { name: "Open Workshop", exact: true }).click();
    await page.getByRole("button", { name: "Toggle inspector panel" }).click();
    await page.getByRole("button", { name: "New page from template…", exact: true }).click();
    const dialog = page.getByRole("dialog", { name: "New page from template" });
    await dialog.getByRole("button", { name: /^List report/ }).click();
    const picker = dialog.getByRole("combobox", { name: "Object" });
    await expect(picker.locator("option", { hasText: "build.app" })).toHaveCount(0);
    const option = picker.locator("option", { hasText: `build.${objectName}` }).first();
    await expect(option).toBeAttached();
    await picker.selectOption((await option.getAttribute("value")) ?? "");
    await expect(dialog.getByRole("checkbox", { name: /^Create/ })).toBeChecked();
    await dialog.getByRole("textbox", { name: "Page name" }).fill(`${name}list`);
    await dialog.getByRole("textbox", { name: "Page title" }).fill("Orders");
    await dialog.getByRole("button", { name: "Create draft in Studio", exact: true }).click();
    await expect(page.getByRole("region", { name: "Page settings" })).toBeVisible();
  });

  await test.step("6 review and save once", async () => {
    await page.getByRole("button", { name: "Review and save", exact: true }).click();
    for (const kind of ["Applications", "Pages", "Object types"]) await expect(page.getByText(`${kind} ·`).first()).toBeVisible();
    await page.getByRole("button", { name: "Save immutable candidate", exact: true }).click();
    await page.getByRole("button", { name: "Activate release", exact: true }).click();
    await expect(page.getByText("Release active for operators.")).toBeVisible({ timeout: 60_000 });
  });

  await test.step("7 a business user opens the application and creates an order", async () => {
    const user = await (await browser.newContext()).newPage();
    await signIn(user, "user");
    await user.getByRole("button", { name: "Applications", exact: true }).first().click();
    await user.getByRole("region", { name: "Business applications" }).getByRole("button", { name: "Orders", exact: true }).click();
    const app = user.getByRole("region", { name: "Orders" });
    await expect(app.getByRole("heading", { name: "Orders", level: 1 })).toBeVisible();
    await expect(app.getByText("No orders")).toBeVisible();
    await app.getByRole("button", { name: "Create order", exact: true }).click();
    const form = user.getByRole("dialog");
    await form.getByRole("textbox", { name: /^Field/ }).fill("First order");
    await form.getByRole("button", { name: "Create", exact: true }).click();
    await expect(app.getByRole("cell", { name: "First order" })).toBeVisible({ timeout: 30_000 });
  });
});
