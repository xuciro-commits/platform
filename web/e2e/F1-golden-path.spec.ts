import { expect, signIn, test, unique } from "./kit";

test("F1 builder goes from an empty project to an application a business user acts in", async ({ page, browser }) => {
  const name = unique("orders");
  const objectName = `${name}order`;
  const label = `Orders ${name.slice(6)}`;

  await test.step("1 create a project", async () => {
    await signIn(page, "builder", "/projects");
    await page.getByRole("button", { name: "New project", exact: true }).click();
    const sheet = page.getByRole("dialog", { name: "Create project" });
    await sheet.getByRole("textbox", { name: "Name *" }).fill(name);
    await sheet.getByRole("textbox", { name: "What people call it *" }).fill(label);
    await sheet.getByRole("button", { name: "Create", exact: true }).click();
    await expect(page.getByRole("heading", { name: "Next steps" })).toBeVisible();
  });

  await test.step("2 bring data in as a dataset", async () => {
    await page.getByRole("button", { name: "New", exact: true }).first().click();
    await page.getByRole("menuitem", { name: "Dataset", exact: true }).click();
    await page.getByRole("textbox", { name: "Dataset name" }).fill(`${name}rows`);
    await page.getByRole("textbox", { name: "Dataset title" }).fill("Order rows");
    await page.getByRole("button", { name: "Create", exact: true }).click();
    await expect(page.getByRole("button", { name: "Upload CSV" })).toBeVisible();
    await page.locator('input[type="file"]').setInputFiles({ name: "orders.csv", mimeType: "text/csv", buffer: Buffer.from("Order no,Customer,Amount\nSO-1,Acme,120\nSO-2,Globex,80\n") });
    await expect(page.getByRole("cell", { name: "Globex" })).toBeVisible();
  });

  await test.step("3 create an object type from the dataset", async () => {
    await page.getByRole("button", { name: "Create object type", exact: true }).click();
    const sheet = page.getByRole("dialog", { name: "Create object type" });
    await expect(sheet.getByRole("combobox", { name: "Primary key column *" })).toHaveValue("Order no");
    await expect(sheet.getByRole("combobox", { name: "Title column *" })).toHaveValue("Customer");
    await sheet.getByRole("textbox", { name: "What people call it *" }).fill("Order");
    await sheet.getByRole("textbox", { name: "Name *" }).fill(objectName);
    await sheet.getByRole("button", { name: "Create", exact: true }).click();
    await expect(page.getByText(`build.${objectName}`, { exact: true })).toBeVisible();
  });

  await test.step("4 add an action type", async () => {
    await page.getByRole("navigation", { name: "Order" }).getByRole("button", { name: "Actions", exact: true }).click();
    await page.getByRole("button", { name: "Add an action", exact: true }).click();
    await expect(page.getByRole("button", { name: "Open action type", exact: true })).toBeVisible();
    await expect(page.getByRole("status").filter({ hasText: "Unsaved changes" })).toBeVisible();
  });

  await test.step("5 build a page in Workshop from a template bound to the draft object type", async () => {
    await page.getByRole("button", { name: label, exact: true }).first().click();
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
    await expect(dialog.getByRole("group", { name: "Offered actions" }).getByRole("checkbox")).toHaveCount(4);
    await dialog.getByRole("textbox", { name: "Page name" }).fill(`${name}list`);
    await dialog.getByRole("textbox", { name: "Page title" }).fill("Orders");
    await dialog.getByRole("button", { name: "Create draft in Studio", exact: true }).click();
    await expect(page.getByRole("region", { name: "Page settings" })).toBeVisible();
  });

  await test.step("6 review and save once, leaving another builder's drafts out", async () => {
    const other = await (await browser.newContext()).newPage();
    await signIn(other, "admin", "/projects");
    await other.getByRole("button", { name: "New project", exact: true }).click();
    const sheet = other.getByRole("dialog", { name: "Create project" });
    await sheet.getByRole("textbox", { name: "Name *" }).fill(`${name}other`);
    await sheet.getByRole("textbox", { name: "What people call it *" }).fill(`Other ${name.slice(6)}`);
    await sheet.getByRole("button", { name: "Create", exact: true }).click();
    await expect(other.getByRole("heading", { name: "Next steps" })).toBeVisible();
    await other.context().close();
    await page.getByRole("button", { name: "Review and save", exact: true }).click();
    const provenance = page.getByRole("list", { name: "Draft provenance" });
    for (const kind of ["Applications", "Pages", "Object types", "Datasets"]) await expect(provenance.getByText(`${kind} ·`).first()).toBeVisible();
    await expect(provenance.getByText("Applications ·")).toHaveCount(1);
    await page.getByRole("button", { name: "Save immutable candidate", exact: true }).click();
    await page.getByRole("button", { name: "Activate release", exact: true }).click();
    await expect(page.getByText("Release active for operators.")).toBeVisible({ timeout: 60_000 });
  });

  await test.step("7 a business user sees the dataset rows as orders and creates one", async () => {
    const user = await (await browser.newContext()).newPage();
    await signIn(user, "user");
    await user.getByRole("button", { name: "Applications", exact: true }).first().click();
    await user.getByRole("region", { name: "Business applications" }).getByRole("button", { name: label, exact: true }).click();
    const app = user.getByRole("region", { name: "Orders" });
    await expect(app.getByRole("heading", { name: "Orders", level: 1 })).toBeVisible();
    await expect(app.getByRole("cell", { name: "Globex" })).toBeVisible({ timeout: 30_000 });
    await app.getByRole("button", { name: "Create order", exact: true }).click();
    const form = user.getByRole("dialog", { name: "Create order" });
    await expect(form.getByRole("button", { name: "Source editor" })).toHaveCount(0);
    await form.getByRole("textbox", { name: /^Order no/ }).fill("SO-3");
    await form.getByRole("textbox", { name: /^Customer/ }).fill("Initech");
    await form.getByRole("button", { name: "Create", exact: true }).click();
    await expect(app.getByRole("cell", { name: "Initech" })).toBeVisible({ timeout: 30_000 });
  });
});
