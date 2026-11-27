import { type Page, expect, test } from "@playwright/test";
import { signIn, open, property, metadata, edit } from "./support";

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


test("object definition: create and bind an action type before publishing a transition", async ({ page }) => {
  await signIn(page);
  await page.goto("/ontology/objects/build.object/sales.workflow");
  await page.getByRole("button", { name: "Create transition", exact: true }).click();
  const transition = page.getByRole("dialog");
  await transition.getByRole("textbox", { name: "What people call it", exact: true }).fill("Activate contact");
  await transition.getByRole("button", { name: "Save", exact: true }).click();
  await expect(page.getByText("Transitions use the linked action types.", { exact: true })).toBeVisible();

  await page.getByRole("button", { name: "Publish", exact: true }).click();
  const publish = page.getByRole("dialog", { name: /Publish/ });
  await publish.getByRole("button", { name: "Publish", exact: true }).click();
  await expect(publish).toBeHidden();
  await page.goto("/ontology/objects/sales.workflow");
  await page.getByRole("button", { name: "New", exact: true }).click();
  const create = page.getByRole("dialog");
  await create.getByRole("textbox", { name: /^Name/ }).fill("Ada Lovelace");
  await create.getByRole("button", { name: "Create", exact: true }).click();
  await page.getByRole("button", { name: "Activate contact", exact: true }).click();
  const confirm = page.getByRole("alertdialog");
  await confirm.getByRole("button", { name: "Activate contact", exact: true }).click();
  await expect(page.getByText("Active", { exact: true }).first()).toBeVisible();
});


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

test("gate: a type of the same name in another project is its own type with its own records", async () => {
  await open(page, "Ontology", "Object types");
  await page.getByRole("button", { name: "New", exact: true }).click();
  const twin = page.getByRole("dialog");
  await twin.getByRole("combobox", { name: /^Project/ }).click();
  await page.getByRole("option", { name: "CRM", exact: true }).click();
  await twin.getByRole("textbox", { name: /^API name/ }).fill("gateitem");
  await twin.getByRole("textbox", { name: /^Name/ }).fill("CRM gate item");
  await twin.getByRole("button", { name: "Create", exact: true }).click();
  await expect(page.getByRole("heading", { level: 1, name: "CRM gate item", exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Add field", exact: true }).click();
  const twinField = page.getByRole("dialog");
  await twinField.getByRole("textbox", { name: /^Name/ }).fill("code");
  await twinField.getByRole("textbox", { name: /^Label/ }).fill("Code");
  await twinField.getByRole("combobox", { name: /^Type/ }).click();
  await page.getByRole("option", { name: "text", exact: true }).click();
  await twinField.getByRole("button", { name: "Save", exact: true }).click();
  await expect(twinField).toBeHidden();
  await expect(page.getByRole("cell", { name: "Code", exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Publish", exact: true }).click();
  const publishTwin = page.getByRole("dialog", { name: /Publish/ });
  await publishTwin.getByRole("button", { name: "Publish", exact: true }).click();
  await expect(publishTwin).toBeHidden();
  await page.goto("/ontology/objects/crm.gateitem");
  await page.getByRole("button", { name: "New", exact: true }).click();
  await page.getByRole("dialog").getByRole("button", { name: "Create", exact: true }).click();
  await expect(page).toHaveURL(/\/ontology\/objects\/crm\.gateitem\/[^/]+$/);
  const twinRecord = new URL(page.url()).pathname.split("/").at(-1)!;
  await page.goto("/ontology/objects/sales.gateitem");
  await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
  await expect(page.locator(`a[href$="/${twinRecord}"]`)).toHaveCount(0);
});

test("gate: an installed package contributes an app whose record action runs", async () => {
  await open(page, "Delivery", "Marketplace");
  await expect(page.getByRole("listitem").filter({ hasText: "application.production-provider · 0.1.5" })).toBeVisible();
  await page.getByRole("button", { name: "More actions", exact: true }).click();
  await page.getByRole("menuitem", { name: "Show all versions", exact: true }).click();
  const provider = page.getByRole("listitem").filter({ hasText: "application.production-provider · 0.1.4" });
  await provider.getByRole("button", { name: "Install", exact: true }).click();
  const review = page.getByRole("dialog", { name: /Review and install/ });
  await expect(review).toContainText("application.production-provider");
  await review.getByRole("button", { name: "Install package", exact: true }).click();
  await expect(page).toHaveURL(/\/delivery\/installations$/, { timeout: 20_000 });
  const installed = page.getByRole("listitem").filter({ hasText: "Production batch provider" });
  await expect(installed).toContainText("0.1.4");

  await page.reload();
  await page.getByRole("link", { name: "Production order provider", exact: true }).click();
  await page.getByRole("navigation", { name: "Primary navigation" }).getByRole("link", { name: "Production orders", exact: true }).click();
  await page.getByRole("button", { name: "New", exact: true }).click();
  const order = page.getByRole("dialog");
  await order.getByRole("textbox", { name: /^Order number/ }).fill("GATE-1");
  await order.getByRole("textbox", { name: /^Product/ }).fill("Gate valve");
  await order.getByRole("spinbutton", { name: /^Ordered quantity/ }).fill("12");
  await order.getByRole("spinbutton", { name: /^Unit production cost/ }).fill("40");
  await page.keyboard.press("Tab");
  await order.getByRole("button", { name: "Create", exact: true }).click();
  await expect(page.getByRole("heading", { level: 1, name: "GATE-1", exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Start upstream production", exact: true }).click();
  const contributed = page.getByRole("dialog", { name: "Start upstream production", exact: true });
  await contributed.getByRole("textbox", { name: /^Shop order/ }).fill("WO-GATE-1");
  await contributed.getByRole("button", { name: "Start upstream production", exact: true }).click();
  await expect(contributed).toBeHidden();
  await expect(page.getByText("Running", { exact: true }).first()).toBeVisible();
});

test("gate: builders and users see no administration entries", async ({ browser }) => {
  for (const seat of ["builder-1", "user-1"]) {
    const context = await browser.newContext();
    const member = await context.newPage();
    await member.goto("/");
    await member.getByRole("button", { name: seat, exact: true }).click();
    await expect(member.getByRole("link", { name: "Home", exact: true })).toBeVisible();
    await expect(member.getByRole("link", { name: "Admin", exact: true })).toHaveCount(0);
    await expect(member.getByRole("link", { name: "Host", exact: true })).toHaveCount(0);
    await context.close();
  }
});
});


test("applications: an installed package contributes a menu and an object list", async ({ page }) => {
  await signIn(page);
  const app = page.getByRole("link", { name: "Production order provider", exact: true });
  await expect(app).toBeVisible();
  await app.click();
  const navigation = page.getByRole("navigation", { name: "Primary navigation" });
  await navigation.getByRole("link", { name: "Production orders", exact: true }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Production orders", exact: true })).toBeVisible();
  await expect(page).toHaveURL(/\/productionprovider\/batchproviderorder$/);

  await page.evaluate(async () => {
    const session = JSON.parse(sessionStorage.getItem("platform:session")!);
    const headers = { Authorization: `Bearer ${session.token}`, "Platform-Tenant": session.tenant, "Content-Type": "application/json" };
    const get = async (path: string) => (await fetch(path, { headers })).json();
    const catalog = await get("/v1/actions");
    const identity = await get("/v1/me");
    for (const name of ["platform.package.drain", "platform.package.retire"]) {
      const action = catalog.find((entry: { schema: string }) => entry.schema === name);
      const payload = new TextEncoder().encode(JSON.stringify({ id: "application.production-provider" }));
      const response = await fetch("/v1/submissions", { method: "POST", headers: { ...headers, "Platform-Schema-Version": action.schemaVersion.version }, body: JSON.stringify({
        tenantId: identity.tenantId,
        principalId: identity.principalId,
        authority: "platform",
        target: { type: "platform.package", id: "application.production-provider" },
        schema: { name, version: action.schemaVersion.family },
        idempotencyKey: crypto.randomUUID(),
        payload: btoa(String.fromCharCode(...payload)),
      }) });
      const answer = await response.json();
      if (!response.ok || answer.error) throw new Error(JSON.stringify(answer));
    }
  });
  await page.reload();
  await expect(page.getByRole("link", { name: "Production order provider", exact: true })).toHaveCount(0);
});


test("applications: install the sample version and run a contributed record action", async ({ page }) => {
  await signIn(page);
  await open(page, "Delivery", "Marketplace");
  const provider = page.getByRole("listitem").filter({ hasText: "application.production-provider · 0.1.5" });
  await provider.getByRole("button", { name: "Install", exact: true }).click();
  const review = page.getByRole("dialog", { name: /Review and install/ });
  await expect(review.getByText("Installation scope", { exact: true })).toBeVisible();
  await review.getByRole("button", { name: "Install package", exact: true }).click();
  await expect(page).toHaveURL(/\/delivery\/installations$/);
  await expect(page.getByRole("listitem").filter({ hasText: "Production batch provider" })).toContainText("0.1.5");

  await open(page, "Production order provider", "Production orders");
  await page.getByRole("button", { name: "New", exact: true }).click();
  const order = page.getByRole("dialog");
  await order.getByRole("textbox", { name: /^Order number/ }).fill("PO-1042");
  await order.getByRole("textbox", { name: /^Product/ }).fill("Precision gearbox");
  await order.getByRole("spinbutton", { name: /^Ordered quantity/ }).fill("25");
  await order.getByRole("spinbutton", { name: /^Unit production cost/ }).fill("180");
  await page.keyboard.press("Tab");
  await order.getByRole("button", { name: "Create", exact: true }).click();
  await expect(page.getByRole("heading", { level: 1, name: "PO-1042", exact: true })).toBeVisible();
  await expect(page).toHaveURL(/\/productionprovider\/batchproviderorder\/[^/?]+$/);

  await page.getByRole("button", { name: "More actions" }).click();
  await page.getByRole("menuitem", { name: "Set shop order", exact: true }).click();
  const action = page.getByRole("dialog", { name: "Set shop order", exact: true });
  await action.getByRole("textbox", { name: /^Shop order/ }).fill("WO-1042");
  await action.getByRole("button", { name: "Set shop order", exact: true }).click();
  await expect(action).toBeHidden();
  const shopOrder = property(page, "First shop order");
  await expect(shopOrder).toHaveText("WO-1042");
  await page.reload();
  await expect(shopOrder).toHaveText("WO-1042");
});


test("applications: shared records respect roles and keep their history after retirement", async ({ page, browser }) => {
  await signIn(page);
  await open(page, "Production order provider", "Production orders");
  const entry = page.getByRole("link", { name: "PO-1042", exact: true });
  const appRecord = new URL((await entry.getAttribute("href"))!, page.url());
  const recordURL = `/ontology/objects/build.batchproviderorder/${appRecord.pathname.split("/").at(-1)}`;
  const schema = "build.batchproviderorder.setshoporder";
  const member = await browser.newPage();
  await signIn(member, "builder-1 · acceptance");
  await member.goto(appRecord.href);
  await expect(member.getByRole("heading", { name: "PO-1042", exact: true })).toBeVisible();
  await member.getByRole("button", { name: "More actions" }).click();
  await member.getByRole("menuitem", { name: "Set shop order", exact: true }).click();
  const action = member.getByRole("dialog", { name: "Set shop order", exact: true });
  await action.getByRole("textbox", { name: /^Shop order/ }).fill("WO-1043");
  const submitted = member.waitForRequest((request) => request.url().endsWith("/v1/submissions") && request.postDataJSON().schema.name === schema);
  await action.getByRole("button", { name: "Set shop order", exact: true }).click();
  const sent = await submitted;
  await expect(action).toBeHidden();
  await expect(property(member, "First shop order")).toHaveText("WO-1043");
  const historyURL = await member.getByRole("link", { name: "Activity", exact: true }).getAttribute("href");
  expect(new URL(historyURL!, member.url()).pathname).toBe(`${appRecord.pathname}/activity`);

  const direct = (actor: Page) => actor.evaluate(async ({ submission, version }) => {
    const session = JSON.parse(sessionStorage.getItem("platform:session")!);
    const headers = { authorization: `Bearer ${session.token}`, "platform-tenant": session.tenant };
    const identity = await (await fetch("/v1/me", { headers })).json();
    const response = await fetch("/v1/submissions", {
      method: "POST",
      headers: { ...headers, "content-type": "application/json", "platform-schema-version": version },
      body: JSON.stringify({
        tenantId: identity.tenantId,
        principalId: identity.principalId,
        authority: submission.authority,
        target: submission.target,
        schema: submission.schema,
        idempotencyKey: crypto.randomUUID(),
        payload: btoa(JSON.stringify({ shoporder: "WO-DENIED" })),
      }),
    });
    return (await response.json()).error as { code: string; message: string };
  }, { submission: sent.postDataJSON(), version: sent.headers()["platform-schema-version"] });

  await member.getByRole("button", { name: "Account", exact: true }).click();
  await member.getByRole("menuitem", { name: "Sign out", exact: true }).click();
  await signIn(member, "user-1 · acceptance");
  await member.goto(recordURL);
  await expect(member.getByRole("heading", { name: "PO-1042", exact: true })).toBeVisible();
  await expect(member.getByText("WO-1043", { exact: true })).toBeVisible();
  await expect(member.getByRole("button", { name: "Start upstream production", exact: true })).toBeEnabled();
  const menu = member.getByRole("button", { name: "More actions" });
  if (await menu.isVisible()) await menu.click();
  await expect(member.getByRole("menuitem", { name: "Set shop order", exact: true })).toHaveCount(0);
  expect((await direct(member)).code).toBe("ERROR_CODE_POLICY_DENIED");

  await open(page, "Delivery", "Installations");
  const installation = page.getByRole("listitem").filter({ hasText: "Production batch provider" });
  const manage = installation.getByRole("button", { name: "Manage installation", exact: true });
  await manage.click();
  const review = page.getByRole("dialog", { name: /Manage installation/ });
  const retained = metadata(review, "Business records retained");
  await expect(retained).toHaveText(/^[1-9]\d*$/);
  const kept = await retained.innerText();
  await review.getByRole("button", { name: "Mark for uninstall", exact: true }).click();
  await expect(review).toBeHidden();
  await expect(installation).toContainText("draining");
  await manage.click();
  await review.getByRole("button", { name: "Retire package", exact: true }).click();
  await expect(review).toBeHidden();
  await expect(installation).toContainText("retired");
  await expect(page.getByRole("link", { name: "Production order provider", exact: true })).toHaveCount(0);
  expect(await direct(page)).toMatchObject({ code: "ERROR_CODE_POLICY_DENIED", message: expect.stringContaining("The package contribution is retired") });
  await manage.click();
  await expect(retained).toHaveText(kept);
  await page.goto(appRecord.href);
  await expect(property(page, "First shop order")).toHaveText("WO-1043");
  const retiredMenu = page.getByRole("button", { name: "More actions" });
  if (await retiredMenu.isVisible()) await retiredMenu.click();
  await expect(page.getByRole("menuitem", { name: "Set shop order", exact: true })).toHaveCount(0);

  await member.goto(appRecord.href);
  await expect(member).toHaveURL((url) => url.pathname === recordURL);
  await expect(member.getByText("WO-1043", { exact: true })).toBeVisible();
  await expect(member.getByRole("button", { name: "Start upstream production", exact: true })).toHaveCount(0);
  if (await menu.isVisible()) await menu.click();
  await expect(member.getByRole("menuitem", { name: "Start upstream production", exact: true })).toHaveCount(0);
  await expect(member.getByRole("link", { name: "Production order provider", exact: true })).toHaveCount(0);
  await member.goto(historyURL!);
  await expect(member).toHaveURL((url) => url.pathname === `${recordURL}/activity`);
  await expect(member.getByRole("listitem", { name: /First shop order: .*WO-1043/ })).toBeVisible();
  await member.close();
});


test("delivery: a builder publishes a version, which installs beside its source project under another name as its own app", async ({ page }) => {
  await signIn(page);
  await page.goto("/delivery/versions");
  await expect(page.getByRole("heading", { level: 1, name: "Versions", exact: true })).toBeVisible();
  await page.getByRole("button", { name: "New", exact: true }).first().click();
  const release = page.getByRole("dialog", { name: "Publish product version", exact: true });
  await release.getByRole("combobox", { name: "Product" }).click();
  await page.getByRole("option", { name: "CRM · Create product", exact: true }).click();
  await expect(release.getByRole("textbox", { name: "Version" })).toHaveValue("0.1.0");
  await expect(release.getByText("Convert to deal", { exact: true })).toBeVisible();
  await release.getByRole("button", { name: "Publish product version", exact: true }).click();
  await expect(page).toHaveURL(/\/delivery\/versions\/crm%400\.1\.0$/);
  await expect(page.getByRole("heading", { level: 1, name: "0.1.0", exact: true })).toBeVisible();
  await page.goto("/delivery/versions");
  await page.getByRole("button", { name: "New", exact: true }).first().click();
  await release.getByRole("combobox", { name: "Product" }).click();
  await page.getByRole("option", { name: "CRM · CRM", exact: true }).click();
  await expect(release.getByRole("textbox", { name: "Version" })).toHaveValue("0.1.1");
  await release.getByRole("button", { name: "Cancel", exact: true }).click();

  await page.goto("/delivery/marketplace");
  await expect(page.getByRole("listitem").filter({ hasText: /crm · 0\.1\.22/ })).toBeVisible();
  await page.getByRole("button", { name: "More actions", exact: true }).click();
  await page.getByRole("menuitem", { name: "Show all versions", exact: true }).click();
  await page.getByRole("listitem").filter({ hasText: "crm · 0.1.0" }).getByRole("button", { name: "Install" }).click();
  const review = page.getByRole("dialog");
  const name = review.getByRole("textbox", { name: "Project API name" });
  await expect(name).toHaveValue("crm");
  const install = review.getByRole("button", { name: "Install package", exact: true });
  await expect(install).toBeEnabled({ timeout: 30_000 });
  await install.click();
  await expect(review.getByText("A project named crm already exists; install under another name")).toBeVisible();
  await name.fill("pipeline");
  await review.getByRole("button", { name: "Install package", exact: true }).click();
  await expect(page).toHaveURL(/\/delivery\/installations$/, { timeout: 30_000 });
  await expect(page.getByRole("listitem").filter({ hasText: "crm · 0.1.0" })).toContainText("pipeline");
  await page.locator('a[href="/pipeline/dashboard"]').first().click();
  await expect(page.getByRole("main").getByRole("link", { name: "Leads", exact: true })).toBeVisible();
  await page.locator('a[href="/pipeline/pipeline.lead"]').first().click();
  await expect(page).toHaveURL(/\/pipeline\/pipeline\.lead$/);
  await expect(page.getByRole("heading", { level: 1, name: "Leads", exact: true })).toBeVisible();
  await expect(page.getByRole("heading", { level: 2, name: "Nothing here yet", exact: true })).toBeVisible();
  await page.goto("/ontology/objects/crm.lead");
  await expect(page.getByText("林子航", { exact: true })).toBeVisible();
});
});

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
  await page.getByRole("option", { name: "Deal · Status", exact: true }).click();
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

  await page.goto("/ontology/objects/crm.deal");
  await page.getByRole("link", { name: "先锋车队门户", exact: true }).click();
  await page.getByRole("button", { name: "More actions", exact: true }).click();
  await page.getByRole("menuitem", { name: "Request site survey", exact: true }).click();
  const blockedSurvey = page.getByRole("dialog", { name: "Request site survey", exact: true });
  await blockedSurvey.getByRole("textbox", { name: /^Survey engineer/ }).fill("周工");
  await blockedSurvey.getByRole("button", { name: "Request site survey", exact: true }).click();
  await expect(blockedSurvey.getByText(/Deal · Status: Negotiation/)).toBeVisible();
  await blockedSurvey.getByRole("button", { name: "Cancel", exact: true }).click();
  await expect(blockedSurvey).toBeHidden();

  await page.goto("/ontology/objects/crm.deal");
  await page.getByRole("link", { name: "曦和工厂客户关系系统上线", exact: true }).click();
  await page.getByRole("button", { name: "More actions", exact: true }).click();
  await page.getByRole("menuitem", { name: "Request site survey", exact: true }).click();
  const allowedSurvey = page.getByRole("dialog", { name: "Request site survey", exact: true });
  await allowedSurvey.getByRole("textbox", { name: /^Survey engineer/ }).fill("周工");
  await allowedSurvey.getByRole("button", { name: "Request site survey", exact: true }).click();
  await expect(allowedSurvey).toBeHidden();
  await expect(property(page, "Survey engineer")).toContainText("周工");
});
