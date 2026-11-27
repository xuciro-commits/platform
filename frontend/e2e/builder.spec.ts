import { type Page, expect, test } from "@playwright/test";
import { signIn, open, property, metadata, edit, submit } from "./support";

test.describe.serial("builder and applications", () => {
test("publish: a draft type goes live from its record page and takes records", async ({ page }) => {
  await signIn(page);
  await open(page, "Ontology", "Object types");
  await page.getByRole("row").filter({ has: page.getByRole("cell", { name: "sales", exact: true }) }).getByRole("link", { name: "Contact", exact: true }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Contact" })).toBeVisible();
  await expect(page.getByText("Draft", { exact: true })).toBeVisible();

  await page.getByRole("button", { name: "Publish" }).click();
  const confirm = page.getByRole("dialog", { name: /Publish/ });
  await expect(confirm).toContainText("Contact");
  await confirm.getByRole("button", { name: "Publish" }).click();
  await expect(page.getByText("Published", { exact: true })).toBeVisible();

  await page.getByRole("link", { name: "Open records" }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Contacts" })).toBeVisible();

  await page.getByRole("button", { name: "New" }).click();
  const form = page.getByRole("dialog");
  await expect(form.getByRole("textbox")).toHaveCount(1);
  await form.getByRole("textbox", { name: "Full name" }).fill("白依宁");
  await form.getByRole("button", { name: "Create" }).click();
  await expect(page.getByRole("heading", { level: 1, name: "白依宁" })).toBeVisible();
  await expect(page.getByRole("textbox", { name: "Customer" })).toBeFocused();
});


test("board: dragging a card to another state runs that transition", async ({ page }) => {
  await signIn(page);
  await open(page, "Ontology", "Object types");
  await page.getByRole("button", { name: "Manage definitions" }).click();
  await page.getByRole("radio", { name: "Board" }).click();
  const draft = page.getByRole("region", { name: "Draft" });
  const published = page.getByRole("region", { name: "Published" });
  const card = draft.getByRole("heading", { level: 4, name: "Site visit" });
  await expect(card).toBeVisible();

  const from = (await card.boundingBox())!;
  const to = (await published.boundingBox())!;
  await page.mouse.move(from.x + 2, from.y - 4);
  await page.mouse.down();
  await page.mouse.move(from.x + 40, from.y + 20, { steps: 4 });
  await page.mouse.move(to.x + to.width / 2, to.y + to.height / 2, { steps: 8 });
  await page.mouse.up();

  const confirm = page.getByRole("dialog", { name: /Publish/ });
  await expect(confirm).toContainText("Site visit");
  await confirm.getByRole("button", { name: "Publish" }).click();
  await expect(published.getByRole("heading", { level: 4, name: "Site visit" })).toBeVisible();
  await expect(draft.getByRole("heading", { level: 4, name: "Site visit" })).toHaveCount(0);

  const back = (await published.getByRole("heading", { level: 4, name: "Site visit" }).boundingBox())!;
  const there = (await draft.boundingBox())!;
  await page.mouse.move(back.x + 2, back.y - 4);
  await page.mouse.down();
  await page.mouse.move(back.x + 40, back.y + 20, { steps: 4 });
  await page.mouse.move(there.x + there.width / 2, there.y + there.height / 2, { steps: 8 });
  await page.mouse.up();
  await expect(page.getByRole("dialog", { name: /Publish/ })).toHaveCount(0);
  await expect(published.getByRole("heading", { level: 4, name: "Site visit" })).toBeVisible();
});


test("object definition: add a field, publish it and review a removal", async ({ page }) => {
  await signIn(page);
  await open(page, "Ontology", "Object types");
  await page.getByRole("link", { name: "Opportunity", exact: true }).click();
  await page.getByRole("button", { name: "Add field", exact: true }).click();
  const edit = page.getByRole("dialog");
  await edit.getByRole("textbox", { name: /^Name/ }).fill("mobile");
  await edit.getByRole("textbox", { name: /^Label/ }).fill("Mobile");
  await edit.getByRole("combobox", { name: /^Type/ }).click();
  await page.getByRole("option", { name: "text", exact: true }).click();
  await edit.getByRole("button", { name: "Save", exact: true }).click();
  await expect(page.getByText("Unpublished changes", { exact: true })).toBeVisible();
  await page.getByRole("link", { name: "Changes", exact: true }).click();
  await expect(page.getByText("fields.mobile", { exact: true })).toBeVisible();
  await page.getByRole("link", { name: "Preview", exact: true }).click();
  await expect(page.getByRole("columnheader", { name: "Mobile", exact: true })).toBeVisible();
  await expect(page.getByText("Preview only", { exact: true })).toBeVisible();
  await page.getByRole("link", { name: "Details", exact: true }).click();

  await page.getByRole("button", { name: "Publish", exact: true }).click();
  const publish = page.getByRole("dialog", { name: /Publish/ });
  await expect(publish.getByText(/Records reviewed: \d+/)).toBeVisible();
  const firstComparison = publish.getByRole("table", { name: "Changes", exact: true });
  await expect(firstComparison.getByRole("row").filter({ hasText: "mobile" })).toContainText("Added");
  await expect(firstComparison.getByRole("row").filter({ hasText: "mobile" })).toContainText("Mobile · text");
  await publish.getByRole("button", { name: "Publish", exact: true }).click();
  await expect(publish).toBeHidden();
  await expect(page.getByText("Unpublished changes", { exact: true })).toHaveCount(0);
  await page.getByRole("link", { name: "Open records" }).click();
  await page.getByRole("link", { name: "冲压线改造" }).click();
  await page.getByRole("button", { name: /^Show empty fields/ }).click();
  await expect(property(page, "Mobile")).toBeVisible();

  await page.getByRole("link", { name: "Opportunities", exact: true }).click();
  await page.getByRole("link", { name: "Object types", exact: true }).click();
  await page.getByRole("link", { name: "Opportunity", exact: true }).click();
  const row = page.getByRole("row", { name: /mobile/i });
  await row.getByRole("button", { name: /Row actions/ }).click();
  await page.getByRole("menuitem", { name: "Remove", exact: true }).click();
  await page.getByRole("alertdialog").getByRole("button", { name: "Remove", exact: true }).click();
  await page.getByRole("button", { name: "Add field", exact: true }).click();
  await edit.getByRole("textbox", { name: /^Name/ }).fill("wechat");
  await edit.getByRole("textbox", { name: /^Label/ }).fill("WeChat");
  await edit.getByRole("combobox", { name: /^Type/ }).click();
  await page.getByRole("option", { name: "text", exact: true }).click();
  await edit.getByRole("button", { name: "Save", exact: true }).click();
  await expect(edit).toBeHidden();

  await page.getByRole("button", { name: "Publish", exact: true }).click();
  const comparison = publish.getByRole("table", { name: "Changes", exact: true });
  const removedRow = comparison.getByRole("row").filter({ hasText: "mobile" });
  await expect(removedRow).toContainText("Removed");
  await expect(removedRow).toContainText("Mobile · text");
  await expect(removedRow).toContainText("Empty");
  const addedRow = comparison.getByRole("row").filter({ hasText: "wechat" });
  await expect(addedRow).toContainText("Added");
  await expect(addedRow).toContainText("Empty");
  await expect(addedRow).toContainText("WeChat · text");
  await expect(publish.getByText(/mobile · Removed field · 0 records with data/)).toBeVisible();
  await publish.getByRole("button", { name: "Cancel" }).click();

  await page.getByRole("button", { name: "Add state", exact: true }).click();
  await edit.getByRole("textbox", { name: /^API name/ }).fill("left");
  await edit.getByRole("textbox", { name: /^Name/ }).fill("Left");
  let held = false;
  let release!: () => void;
  const gate = new Promise<void>((resolve) => { release = resolve; });
  await page.route("**/v1/records/build.object/sales.opportunity", async (route) => {
    held = true;
    await gate;
    await route.continue();
  });
  try {
    await edit.getByRole("button", { name: "Save", exact: true }).click();
    await expect(edit).toBeHidden();
    await expect.poll(() => held).toBe(true);
    await expect(page.getByRole("button", { name: "Publish", exact: true })).toBeDisabled();
  } finally {
    release();
    await page.unrouteAll({ behavior: "wait" });
  }
  await page.getByRole("button", { name: "Publish", exact: true }).click();
  await expect(publish.getByText(/cannot be reached/)).toBeVisible();
  await expect(publish.getByRole("button", { name: "Publish", exact: true })).toBeDisabled();
  await publish.getByRole("button", { name: "Cancel" }).click();
  const empty = page.getByRole("button", { name: /^Show empty fields/ });
  if (await empty.isVisible()) await empty.click();
  await page.getByRole("button", { name: "Edit Icon", exact: true }).press("Enter");
  await page.getByRole("option", { name: "truck", exact: true }).click();
  await expect(page.getByRole("button", { name: "Edit Icon", exact: true })).toBeAttached();
  await expect(property(page, "Icon")).toHaveText("truck");
});;


test("object definition: a condition picked field by field shows the field only where it holds", async ({ page }) => {
  await signIn(page);
  await open(page, "Ontology", "Object types");
  await page.getByRole("link", { name: "Customer", exact: true }).click();
  await page.getByRole("button", { name: "Add field", exact: true }).click();
  const edit = page.getByRole("dialog");
  await edit.getByRole("textbox", { name: /^Name/ }).fill("discount");
  await edit.getByRole("textbox", { name: /^Label/ }).fill("Strategic discount");
  await edit.getByRole("combobox", { name: /^Type/ }).click();
  await page.getByRole("option", { name: "text", exact: true }).click();
  await edit.getByRole("combobox", { name: /^Only when/ }).click();
  await page.getByRole("option", { name: "Tier", exact: true }).click();
  await edit.getByRole("combobox", { name: /^Values/ }).click();
  await page.getByRole("option", { name: "strategic", exact: true }).click();
  await page.keyboard.press("Escape");
  await edit.getByRole("button", { name: "Edit as text", exact: true }).click();
  await expect(edit.getByRole("textbox", { name: /^Only when/ })).toHaveValue("tier=strategic");
  await edit.getByRole("button", { name: "Save", exact: true }).click();
  await expect(edit).toBeHidden();

  await page.getByRole("button", { name: "Publish", exact: true }).click();
  const publish = page.getByRole("dialog", { name: /Publish/ });
  await publish.getByRole("button", { name: "Publish", exact: true }).click();
  await expect(publish).toBeHidden();
  await page.getByRole("link", { name: "Open records" }).click();
  await page.getByRole("link", { name: "北风工具", exact: true }).click();
  await expect(property(page, "Tier")).toBeVisible();
  const hidden = page.getByRole("button", { name: /^Show empty fields/ });
  if (await hidden.isVisible()) await hidden.click();
  await expect(property(page, "Strategic discount")).toBeVisible();
  await page.getByRole("link", { name: "Customers", exact: true }).click();
  await page.getByRole("link", { name: "阿特拉斯铸造", exact: true }).click();
  await expect(property(page, "Tier")).toBeVisible();
  if (await hidden.isVisible()) await hidden.click();
  await expect(property(page, "Strategic discount")).toHaveCount(0);
});

test("object definition: a formula written in the editor is checked by the host, completes field names and fills the record", async ({ page }) => {
  await signIn(page);
  await open(page, "Ontology", "Object types");
  await page.getByRole("link", { name: "Customer", exact: true }).click();
  await page.getByRole("button", { name: "Add field", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByRole("textbox", { name: /^Name/ }).fill("seats");
  await dialog.getByRole("textbox", { name: /^Label/ }).fill("Seats needed");
  await dialog.getByRole("combobox", { name: /^Type/ }).click();
  await page.getByRole("option", { name: "integer", exact: true }).click();
  const formula = dialog.getByRole("textbox", { name: /^Formula/ });
  await formula.click();
  await page.keyboard.type("employes * 2");
  await expect(dialog.getByText("undeclared formula field employes").first()).toBeVisible();
  await page.keyboard.press("ControlOrMeta+a");
  await page.keyboard.type("emp");
  await page.getByRole("option", { name: /^employees/ }).click();
  await page.keyboard.type(" * 2");
  await expect(formula).toHaveText("employees * 2");
  await expect(dialog.getByText(/undeclared formula field/)).toHaveCount(0);
  await dialog.getByRole("button", { name: "Save", exact: true }).click();
  await expect(dialog).toBeHidden();

  await page.getByRole("button", { name: "Publish", exact: true }).click();
  const publish = page.getByRole("dialog", { name: /Publish/ });
  await publish.getByRole("button", { name: "Publish", exact: true }).click();
  await expect(publish).toBeHidden();
  await page.getByRole("link", { name: "Open records" }).click();
  await page.getByRole("link", { name: "港湾货运", exact: true }).click();
  await edit(page, "Employees");
  const employees = page.getByRole("spinbutton", { name: "Employees", exact: true });
  await employees.fill("900");
  await employees.press("Enter");
  await expect(property(page, "Employees")).toContainText("900");
  await expect(property(page, "Seats needed")).toContainText("1,800");
});

test.describe.serial("gate", () => {
let page: Page;
test.beforeAll(async ({ browser }) => {
  page = await browser.newPage();
});
test.afterAll(async () => {
  await page.close();
});

test("gate: a builder creates a type with fields, states and a transition, publishes it and moves a record through it", async () => {
  await signIn(page);
  await open(page, "Ontology", "Object types");
  await page.getByRole("button", { name: "New", exact: true }).click();
  const createType = page.getByRole("dialog");
  await createType.getByRole("combobox", { name: /^Project/ }).click();
  await page.getByRole("option", { name: "Sales", exact: true }).click();
  await createType.getByRole("textbox", { name: /^API name/ }).fill("gateitem");
  await createType.getByRole("textbox", { name: /^Name/ }).fill("Gate item");
  await createType.getByRole("button", { name: "Create", exact: true }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Gate item", exact: true })).toBeVisible();

  for (const [name, label, type] of [["code", "Code", "text"], ["summary", "Summary", "text"], ["quantity", "Quantity", "integer"]] as const) {
    await page.getByRole("button", { name: "Add field", exact: true }).click();
    const field = page.getByRole("dialog");
    await field.getByRole("textbox", { name: /^Name/ }).fill(name);
    await field.getByRole("textbox", { name: /^Label/ }).fill(label);
    await field.getByRole("combobox", { name: /^Type/ }).click();
    await page.getByRole("option", { name: type, exact: true }).click();
    await field.getByRole("button", { name: "Save", exact: true }).click();
  }
  for (const [name, title] of [["draft", "Draft"], ["active", "Active"]] as const) {
    await page.getByRole("button", { name: "Add state", exact: true }).click();
    const state = page.getByRole("dialog");
    await state.getByRole("textbox", { name: /^API name/ }).fill(name);
    await state.getByRole("textbox", { name: /^Name/ }).fill(title);
    await state.getByRole("button", { name: "Save", exact: true }).click();
  }
  await page.getByRole("button", { name: "Create transition", exact: true }).click();
  const transition = page.getByRole("dialog");
  await transition.getByRole("textbox", { name: "What people call it", exact: true }).fill("Activate gate item");
  await transition.getByRole("button", { name: "Save", exact: true }).click();
  await expect(page.getByText("Transitions use the linked action types.", { exact: true })).toBeVisible();
  await page.getByRole("link", { name: "Preview", exact: true }).click();
  for (const name of ["Code", "Summary", "Quantity"]) await expect(page.getByRole("columnheader", { name, exact: true })).toBeVisible();
  await page.getByRole("link", { name: "Details", exact: true }).click();

  await page.getByRole("button", { name: "Publish", exact: true }).click();
  const publish = page.getByRole("dialog", { name: /Publish/ });
  await publish.getByRole("button", { name: "Publish", exact: true }).click();
  await expect(publish).toBeHidden();
  await page.goto("/ontology/objects/sales.gateitem");
  await page.getByRole("button", { name: "New", exact: true }).click();
  const createRecord = page.getByRole("dialog");
  await createRecord.getByRole("button", { name: "Create", exact: true }).click();
  await page.getByRole("button", { name: "Activate gate item", exact: true }).click();
  const confirm = page.getByRole("alertdialog");
  await confirm.getByRole("button", { name: "Activate gate item", exact: true }).click();
  await expect(page.getByText("Active", { exact: true }).first()).toBeVisible();
});

});


});

test("ontology: consistent resource scopes and grouping", async ({ page }) => {
  await signIn(page);
  const search = async (text: string) => {
    const box = page.getByRole("textbox", { name: "Search this list" });
    await box.fill(text);
    await box.press("Enter");
  };
  await open(page, "Ontology", "Object types");
  await expect(page.getByRole("radio", { name: "Business resources" })).toBeChecked();
  await search("sales.opportunity");
  await expect(page.getByRole("cell", { name: "sales.opportunity", exact: true })).toBeVisible();
  await search("agent.run");
  await expect(page.getByRole("cell", { name: "agent.run", exact: true })).toHaveCount(0);
  await page.getByRole("radio", { name: "Platform resources" }).click();
  await expect(page.getByRole("cell", { name: "agent.run", exact: true })).toBeVisible();
  await open(page, "Ontology", "Action types");
  await search("sales.opportunity.create");
  await expect(page.getByRole("cell", { name: "sales.opportunity.create", exact: true })).toBeVisible();
  await search("agent.run.start");
  await expect(page.getByRole("cell", { name: "agent.run.start", exact: true })).toHaveCount(0);
  await page.getByRole("radio", { name: "All resources" }).click();
  await expect(page.getByRole("cell", { name: "agent.run.start", exact: true })).toBeVisible();
  await page.getByRole("button", { name: "View options", exact: true }).click();
  await page.getByRole("button", { name: "Grouping", exact: true }).click();
  await page.getByRole("radiogroup", { name: "Primary group", exact: true }).getByRole("radio", { name: "Namespace", exact: true }).click();
  await page.getByRole("radiogroup", { name: "Secondary group", exact: true }).getByRole("radio", { name: "Project", exact: true }).click();
  await expect(page.getByRole("radiogroup", { name: "Secondary group", exact: true }).getByRole("radio", { name: "Project", exact: true })).toBeChecked();
  await page.keyboard.press("Escape");
  await open(page, "Ontology", "Link types");
  await search("sales.opportunity");
  await expect(page.getByRole("cell", { name: "Reference field", exact: true }).first()).toBeVisible();
  await open(page, "Ontology", "Object types");
  await page.getByRole("button", { name: "Manage definitions" }).click();
  await expect(page.getByRole("radio", { name: "Board" })).toBeVisible();
  await page.getByRole("button", { name: "Resource catalog" }).click();
  await expect(page.getByRole("radio", { name: "Business resources" })).toBeChecked();
});
