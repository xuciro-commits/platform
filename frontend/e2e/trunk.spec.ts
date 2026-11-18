import { type Page, expect, test } from "@playwright/test";

async function signIn(page: Page) {
  await page.goto("/");
  await page.getByRole("button", { name: "admin-1 · acceptance" }).click();
}

async function open(page: Page, app: string, entry: string) {
  await page.getByRole("link", { name: app }).click();
  await page.getByRole("navigation", { name: "Primary navigation" }).getByRole("link", { name: entry }).click();
}

test("records: open a list, open a record, change a field in place", async ({ page }) => {
  await signIn(page);
  await open(page, "Ontology", "Objects");
  await page.getByRole("link", { name: "Opportunities" }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Opportunities" })).toBeVisible();
  const retrofit = page.getByRole("row", { name: /Press line retrofit/ });
  await expect(retrofit.getByText("Negotiation", { exact: true })).toBeVisible();
  await expect(retrofit.getByText("184,000", { exact: true })).toBeVisible();
  await expect(retrofit.getByLabel("Negotiation")).toBeVisible();
  await expect(retrofit.getByRole("img", { name: "Yes" })).toBeVisible();
  await expect(retrofit.getByText("1,250.00 USD", { exact: true })).toBeVisible();

  await page.getByRole("link", { name: "Press line retrofit" }).click();
  await expect(page.getByRole("combobox", { name: "Stage" })).toContainText("Negotiation");
  await expect(page.getByRole("heading", { level: 1, name: "Press line retrofit" })).toBeVisible();

  await expect(page.getByRole("heading", { level: 3, name: "Commercial" })).toBeVisible();
  await expect(page.getByRole("heading", { level: 3, name: "Follow-up" })).toBeVisible();
  await expect(page.getByRole("textbox", { name: "Loss reason" })).toHaveCount(0);
  await expect(page.getByRole("switch", { name: "Budget approved" })).toBeChecked();
  await expect(page.getByText("1,250.00 USD", { exact: true })).toBeVisible();
  const owner = page.getByRole("complementary").getByRole("textbox", { name: "Owner" });
  await expect(owner).toHaveValue("Dana Whitfield");

  const next = page.getByRole("textbox", { name: "Next step" });
  await expect(next).toHaveValue("Send revised terms");
  await next.fill("Call the plant manager");
  await next.press("Enter");
  await expect(page.getByRole("complementary").getByText("2", { exact: true })).toBeVisible();

  await page.reload();
  await expect(page.getByRole("textbox", { name: "Next step" })).toHaveValue("Call the plant manager");

  await page.getByRole("complementary").getByRole("textbox", { name: "Owner" }).fill("Omar Haddad");
  await page.getByRole("complementary").getByRole("textbox", { name: "Owner" }).press("Enter");
  await expect(page.getByRole("complementary").getByText("3", { exact: true })).toBeVisible();
  await page.reload();
  await expect(page.getByRole("complementary").getByRole("textbox", { name: "Owner" })).toHaveValue("Omar Haddad");
});

test("sections: a field shows only when its condition holds", async ({ page }) => {
  await signIn(page);
  await open(page, "Ontology", "Objects");
  await page.getByRole("link", { name: "Opportunities" }).click();
  await page.getByRole("link", { name: "Fleet tracking rollout" }).click();
  const reason = page.getByRole("textbox", { name: "Loss reason" });
  await expect(reason).toHaveCount(0);

  await page.getByRole("combobox", { name: "Stage" }).click();
  await page.getByRole("option", { name: "Lost", exact: true }).click();
  await expect(reason).toBeVisible();

  await page.getByRole("combobox", { name: "Stage" }).click();
  await page.getByRole("option", { name: "Proposal", exact: true }).click();
  await expect(reason).toHaveCount(0);
});

test("submit: an answer lost on the way is retried and accepted once", async ({ page }) => {
  await signIn(page);
  await open(page, "Ontology", "Objects");
  await page.getByRole("link", { name: "Opportunities" }).click();
  await page.getByRole("link", { name: "Warehouse scanners" }).click();
  await expect(page.getByRole("complementary").getByText("1", { exact: true })).toBeVisible();

  let dropped = false;
  await page.route("**/v1/submissions", async (route) => {
    if (dropped) return route.continue();
    dropped = true;
    await route.fetch();
    await route.abort("connectionreset");
  });
  const next = page.getByRole("textbox", { name: "Next step" });
  await next.fill("Book the site visit");
  await next.press("Enter");
  await expect(page.getByRole("complementary").getByText("2", { exact: true })).toBeVisible();
  expect(dropped).toBe(true);

  await page.unroute("**/v1/submissions");
  await page.reload();
  await expect(page.getByRole("textbox", { name: "Next step" })).toHaveValue("Book the site visit");
  await expect(page.getByRole("complementary").getByText("2", { exact: true })).toBeVisible();
});

test("actions: archive from the record menu, then undo from the notice", async ({ page }) => {
  await signIn(page);
  await open(page, "Ontology", "Objects");
  await page.getByRole("link", { name: "Opportunities" }).click();
  await page.getByRole("link", { name: "Label printing line" }).click();

  await page.getByRole("button", { name: "More actions" }).click();
  await page.getByRole("menuitem", { name: "Archive" }).click();
  await expect(page.getByRole("alertdialog")).toHaveCount(0);
  const undo = page.getByRole("button", { name: "Undo" });
  await expect(undo).toBeVisible();
  await expect(page.getByText("Archived", { exact: true })).toBeVisible();
  await undo.click();
  await expect(page.getByText("Archived", { exact: true })).toHaveCount(0);

  await page.getByRole("link", { name: "Opportunities" }).click();
  await expect(page.getByRole("link", { name: "Label printing line" })).toBeVisible();
});

test("conflict: the second writer sees both values and chooses", async ({ browser }) => {
  const pages = await Promise.all([browser.newPage(), browser.newPage()]);
  for (const page of pages) {
    await signIn(page);
    await open(page, "Ontology", "Objects");
    await page.getByRole("link", { name: "Opportunities" }).click();
    await page.getByRole("link", { name: "Store replenishment pilot" }).click();
    await expect(page.getByRole("textbox", { name: "Next step" })).toHaveValue("Agree pilot stores");
  }
  const [first, second] = pages;

  await first.getByRole("textbox", { name: "Next step" }).fill("Pilot in four stores");
  await first.getByRole("textbox", { name: "Next step" }).press("Enter");
  await expect(first.getByRole("complementary").getByText("2", { exact: true })).toBeVisible();

  await second.getByRole("complementary").getByRole("textbox", { name: "Owner" }).fill("Omar Haddad");
  await second.getByRole("complementary").getByRole("textbox", { name: "Owner" }).press("Enter");
  await expect(second.getByRole("complementary").getByText("3", { exact: true })).toBeVisible();
  await expect(second.getByRole("dialog")).toHaveCount(0);

  await second.getByRole("textbox", { name: "Next step" }).fill("Pilot in two stores");
  await first.getByRole("textbox", { name: "Next step" }).fill("Pilot in six stores");
  await first.getByRole("textbox", { name: "Next step" }).press("Enter");
  await expect(first.getByRole("complementary").getByText("4", { exact: true })).toBeVisible();
  await second.getByRole("textbox", { name: "Next step" }).press("Enter");
  const choice = second.getByRole("dialog");
  await expect(choice).toContainText("Pilot in six stores");
  await expect(choice).toContainText("Pilot in two stores");
  await choice.getByRole("button", { name: "Use mine" }).click();
  await expect(second.getByRole("complementary").getByText("5", { exact: true })).toBeVisible();

  await first.reload();
  await expect(first.getByRole("textbox", { name: "Next step" })).toHaveValue("Pilot in two stores");
  await Promise.all(pages.map((page) => page.close()));
});

test("publish: a draft type goes live from its record page and takes records", async ({ page }) => {
  await signIn(page);
  await open(page, "Ontology", "Object types");
  await page.getByRole("link", { name: "Contact", exact: true }).click();
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
  await form.getByRole("textbox", { name: "Full name" }).fill("Ines Brandt");
  await form.getByRole("button", { name: "Create" }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Ines Brandt" })).toBeVisible();
  await expect(page.getByRole("textbox", { name: "Customer" })).toBeFocused();
});

test("board: dragging a card to another state runs that transition", async ({ page }) => {
  await signIn(page);
  await open(page, "Ontology", "Object types");
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

test("leave: a typed value is kept when the user walks away without Enter", async ({ page }) => {
  await signIn(page);
  await open(page, "Ontology", "Objects");
  await page.getByRole("link", { name: "Opportunities" }).click();
  await page.getByRole("link", { name: "Furnace monitoring" }).click();
  await page.getByRole("textbox", { name: "Next step" }).fill("Book the site walk");
  await page.getByRole("link", { name: "Opportunities" }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Opportunities" })).toBeVisible();
  await page.reload();
  await page.getByRole("link", { name: "Furnace monitoring" }).click();
  await expect(page.getByRole("textbox", { name: "Next step" })).toHaveValue("Book the site walk");
});

test("shape: every record page has the same header, tabs and rail", async ({ page }) => {
  await signIn(page);
  const visit = async (app: string, entry: string, list: string | null, record: string) => {
    await open(page, app, entry);
    if (list) await page.getByRole("link", { name: list }).click();
    await page.getByRole("link", { name: record, exact: true }).click();
    await expect(page.getByRole("heading", { level: 1, name: record })).toBeVisible();
    await expect(page.getByRole("link", { name: "Details", exact: true })).toBeVisible();
    await expect(page.getByRole("link", { name: "Activity", exact: true })).toBeVisible();
    await expect(page.getByRole("complementary").getByRole("heading", { level: 3, name: "Record" })).toBeVisible();
  };
  await visit("Ontology", "Objects", "Opportunities", "Press line retrofit");
  await page.getByRole("link", { name: "Activity" }).click();
  await expect(page).toHaveURL(/\/activity(?:\?|$)/);
  await expect(page.getByText("Subject: Press line retrofit")).toBeVisible();
  await visit("Ontology", "Objects", "Customers", "Northwind Tools");
  await visit("Ontology", "Object types", null, "Opportunity");
});

test("batch: retries a stale row without submitting a successful row again", async ({ page, context }) => {
  await signIn(page);
  await open(page, "Ontology", "Objects");
  await page.getByRole("link", { name: "Opportunities" }).click();
  for (const name of ["Support contract renewal", "Seasonal stock planning"]) {
    await page.getByRole("row", { name: new RegExp(name) }).getByRole("checkbox").check();
  }
  const editing = await context.newPage();
  const href = await page.getByRole("link", { name: "Seasonal stock planning" }).getAttribute("href");
  await signIn(editing);
  await editing.goto(href!);
  const next = editing.getByRole("textbox", { name: "Next step" });
  await next.fill("Check seasonal targets");
  await next.press("Enter");
  await expect(editing.getByRole("complementary").getByText("2", { exact: true })).toBeVisible();
  await editing.close();

  const archived: string[] = [];
  page.on("request", (request) => {
    const body = request.url().endsWith("/v1/submissions") && request.postDataJSON();
    if (body && body.schema.name === "build.opportunity.archive") archived.push(body.target.id);
  });
  await page.getByRole("button", { name: "Archive opportunity", exact: true }).click();
  await expect(page.getByText("Support contract renewal: Done", { exact: true })).toBeVisible();
  await expect(page.getByText(/Seasonal stock planning:.*changed/)).toBeVisible();
  expect(archived).toHaveLength(2);
  const failed = decodeURIComponent(new URL(href!, "http://local").pathname.split("/").at(-1)!);
  expect(archived).toContain(failed);
  await page.getByRole("button", { name: "Retry failed rows", exact: true }).click();
  await expect(page.getByText("Seasonal stock planning: Done", { exact: true })).toBeVisible();
  expect(archived).toHaveLength(3);
  expect(archived[2]).toBe(failed);
});

test("leave: a refused inline edit asks whether to stay or discard", async ({ page }) => {
  await signIn(page);
  await open(page, "Ontology", "Objects");
  await page.getByRole("link", { name: "Opportunities" }).click();
  await page.getByRole("link", { name: "Fleet tracking rollout" }).click();
  const subject = page.getByRole("textbox", { name: "Subject" });
  await subject.fill("");
  const refused = page.waitForResponse((response) => response.url().endsWith("/v1/submissions") && response.status() >= 400);
  await page.getByRole("link", { name: "Opportunities" }).click();
  await refused;
  const confirm = page.getByRole("alertdialog");
  await expect(confirm).toBeVisible();
  await confirm.getByRole("button", { name: "Stay", exact: true }).click();
  await expect(subject).toHaveValue("");
  await page.getByRole("link", { name: "Opportunities" }).click();
  await confirm.getByRole("button", { name: "Discard", exact: true }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Opportunities" })).toBeVisible();
  await page.getByRole("link", { name: "Fleet tracking rollout" }).click();
  await expect(subject).toHaveValue("Fleet tracking rollout");
});

test("mobile: the navigation drawer contains the application controls", async ({ page }) => {
  await page.setViewportSize({ width: 375, height: 812 });
  await signIn(page);
  await page.getByRole("button", { name: "Open navigation" }).click();
  const drawer = page.getByRole("dialog");
  await expect(drawer.getByRole("button", { name: "Search", exact: true })).toBeVisible();
  await expect(drawer.getByRole("button", { name: "Notifications", exact: true })).toBeVisible();
  await drawer.getByRole("button", { name: "Sign out", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Sign in", exact: true })).toBeVisible();
});

test("reference: search by name, save the selected record and follow its link", async ({ page }) => {
  await signIn(page);
  await open(page, "Ontology", "Objects");
  await page.getByRole("link", { name: "Opportunities" }).click();
  await page.getByRole("link", { name: "Press line retrofit" }).click();
  await page.getByRole("button", { name: /Customer.*Northwind Tools/ }).click();
  await page.getByRole("combobox", { name: "Customer", exact: true }).fill("Hart");
  await page.getByRole("option", { name: /Hartmann/ }).click();
  await expect(page.getByRole("link", { name: "Hartmann Fabrication", exact: true })).toBeVisible();
  await page.reload();
  const customer = page.getByRole("link", { name: "Hartmann Fabrication", exact: true });
  await expect(customer).toBeVisible();
  await customer.hover();
  await expect(page.getByRole("heading", { name: "Hartmann Fabrication", exact: true })).toBeVisible();
  await customer.click();
  await expect(page.getByRole("heading", { level: 1, name: "Hartmann Fabrication", exact: true })).toBeVisible();
});

test("lines: add and edit a child row from its parent record", async ({ page }) => {
  await signIn(page);
  await open(page, "Ontology", "Objects");
  await page.getByRole("link", { name: "Opportunities" }).click();
  await page.getByRole("link", { name: "Press line retrofit" }).click();
  await page.getByRole("button", { name: "Add a row" }).click();
  const form = page.getByRole("dialog");
  await form.getByRole("textbox", { name: "Product" }).fill("Press 200t");
  await form.getByRole("spinbutton", { name: "Quantity" }).fill("2");
  await form.getByRole("spinbutton", { name: "Unit price" }).fill("1000");
  await form.getByRole("spinbutton", { name: "Unit price" }).press("Tab");
  await form.getByRole("button", { name: "Create" }).click();
  await expect(form).toBeHidden();
  const table = page.getByRole("table", { name: "Opportunity lines" });
  await expect(table.getByRole("textbox", { name: "Product" })).toHaveValue("Press 200t");
  await page.reload();
  const quantity = table.getByRole("spinbutton", { name: "Quantity" });
  await expect(quantity).toHaveValue("2");
  await expect(table.getByRole("row", { name: /Totals/ })).toContainText("1,000");
  await quantity.fill("3");
  await quantity.press("Enter");
  await expect(table.getByRole("row", { name: /Totals/ })).toContainText("3");
  await page.reload();
  await expect(quantity).toHaveValue("3");
  await table.getByRole("button", { name: "Archive row" }).click();
  await expect(table.getByRole("textbox", { name: "Product" })).toHaveCount(0);
});

test("activity: a comment and an inline change appear in one timeline", async ({ page }) => {
  await signIn(page);
  await open(page, "Ontology", "Objects");
  await page.getByRole("link", { name: "Opportunities" }).click();
  await page.getByRole("link", { name: "Warehouse scanners" }).click();
  const next = page.getByRole("textbox", { name: "Next step" });
  const before = await next.inputValue();
  await next.fill("Confirm scanner quantities");
  await next.press("Enter");
  await page.getByRole("link", { name: "Activity", exact: true }).click();
  await page.getByRole("textbox", { name: "Write a comment" }).fill("I asked the warehouse lead.");
  await page.getByRole("button", { name: "Post comment" }).click();
  const comment = page.getByText("I asked the warehouse lead.", { exact: true });
  const change = page.getByText(`Next step: ${before} → Confirm scanner quantities`, { exact: true });
  await expect(comment).toBeVisible();
  await expect(change).toBeVisible();
  expect((await comment.boundingBox())!.y).toBeLessThan((await change.boundingBox())!.y);
  const following = page.getByRole("switch", { name: "Follow" });
  await expect(following).toBeChecked();
  await following.click();
  await expect(following).not.toBeChecked();
});

test("links and files: navigate to related records and attach a downloadable file", async ({ page }) => {
  await signIn(page);
  await open(page, "Ontology", "Objects");
  await page.getByRole("link", { name: "Customers" }).click();
  await page.getByRole("link", { name: "Northwind Tools" }).click();
  await page.getByRole("link", { name: "Links", exact: true }).click();
  await expect(page).toHaveURL(/\/links(?:\?|$)/);
  await expect(page.getByRole("link", { name: "Spare parts frame agreement" })).toBeVisible();
  await page.getByRole("link", { name: "View all", exact: true }).click();
  await expect(page).toHaveURL(/build.opportunity\?domain=/);
  await expect(page.getByRole("link", { name: "Spare parts frame agreement" })).toBeVisible();
  await expect(page.getByRole("link", { name: "Warehouse scanners" })).toHaveCount(0);
  await page.getByRole("link", { name: "Spare parts frame agreement" }).click();
  await page.getByRole("navigation", { name: "Tabs" }).getByRole("link", { name: "Files", exact: true }).click();
  await expect(page).toHaveURL(/\/files(?:\?|$)/);
  await page.locator('input[type="file"]').setInputFiles({
    name: "schedule.txt", mimeType: "text/plain", buffer: Buffer.from("Lead time: 6 weeks"),
  });
  await expect(page.getByText("schedule.txt", { exact: true })).toBeVisible();
  const started = page.waitForEvent("download");
  await page.getByRole("button", { name: "Download", exact: true }).click();
  const download = await started;
  expect(download.suggestedFilename()).toBe("schedule.txt");
  expect(await download.failure()).toBeNull();
});

test("restricted fields: a reader sees the field label and no amount", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("button", { name: "user-1 · acceptance" }).click();
  await page.goto("/ontology/objects/build.opportunity/OPP-001");
  await expect(page.getByRole("heading", { level: 1, name: "Press line retrofit" })).toBeVisible();
  await expect(page.getByText("Amount", { exact: true })).toBeVisible();
  await expect(page.getByText("Cannot view", { exact: true })).toBeVisible();
  await expect(page.getByRole("spinbutton", { name: "Amount", exact: true })).toHaveCount(0);
  await expect(page.getByText("184,000", { exact: true })).toHaveCount(0);
});

test("keyboard: create and edit an opportunity without using a pointer", async ({ page }) => {
  await signIn(page);
  await open(page, "Ontology", "Objects");
  await page.getByRole("link", { name: "Opportunities" }).click();
  const tabTo = async (target: ReturnType<Page["getByRole"]>) => {
    await expect(target).toBeVisible();
    for (let index = 0; index < 60 && !await target.evaluate((element) => document.activeElement === element); index++) {
      await page.keyboard.press("Tab");
    }
    await expect(target).toBeFocused();
  };
  await tabTo(page.getByRole("button", { name: "New", exact: true }));
  await page.keyboard.press("Enter");
  const form = page.getByRole("dialog");
  await tabTo(form.getByRole("textbox", { name: "Subject" }));
  await page.keyboard.insertText("Keyboard renewal");
  await tabTo(form.getByRole("button", { name: "Create", exact: true }));
  await page.keyboard.press("Enter");
  await expect(page.getByRole("heading", { level: 1, name: "Keyboard renewal" })).toBeVisible();
  const customer = page.getByRole("combobox", { name: "Customer", exact: true });
  await expect(customer).toBeFocused();
  await page.keyboard.insertText("Northwind");
  await expect(page.getByRole("option", { name: /Northwind Tools/ })).toBeVisible();
  await page.keyboard.press("ArrowDown");
  await page.keyboard.press("Enter");
  await expect(page.getByRole("link", { name: "Northwind Tools", exact: true })).toBeVisible();
  await tabTo(page.getByRole("textbox", { name: "Next step" }));
  await page.keyboard.insertText("Confirm renewal scope");
  await page.keyboard.press("Enter");
  await expect(page.getByRole("complementary").getByText("3", { exact: true })).toBeVisible();
  await tabTo(page.getByRole("link", { name: "Details", exact: true }));
  await page.keyboard.press("?");
  const keys = page.getByRole("dialog", { name: "Keyboard shortcuts" });
  await expect(keys).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(keys).toBeHidden();
  await page.reload();
  await expect(page.getByRole("textbox", { name: "Next step" })).toHaveValue("Confirm renewal scope");
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
  await page.getByRole("link", { name: "Details", exact: true }).click();

  await page.getByRole("button", { name: "Publish", exact: true }).click();
  const publish = page.getByRole("dialog", { name: /Publish/ });
  await expect(publish.getByText(/Records reviewed: \d+/)).toBeVisible();
  await publish.getByRole("button", { name: "Publish", exact: true }).click();
  await expect(publish).toBeHidden();
  await expect(page.getByText("Unpublished changes", { exact: true })).toHaveCount(0);
  await page.getByRole("link", { name: "Open records" }).click();
  await page.getByRole("link", { name: "Press line retrofit" }).click();
  await expect(page.getByRole("textbox", { name: "Mobile", exact: true })).toBeVisible();

  await page.getByRole("link", { name: "Opportunities", exact: true }).click();
  await page.getByRole("link", { name: "Object types", exact: true }).click();
  await page.getByRole("link", { name: "Opportunity", exact: true }).click();
  const row = page.getByRole("row", { name: /mobile/i });
  await row.getByRole("button", { name: /Row actions/ }).click();
  await page.getByRole("menuitem", { name: "Remove", exact: true }).click();
  await page.getByRole("alertdialog").getByRole("button", { name: "Remove", exact: true }).click();
  await page.getByRole("button", { name: "Publish", exact: true }).click();
  await expect(publish.getByText(/mobile · Removed field · 0 records with data/)).toBeVisible();
  await publish.getByRole("button", { name: "Cancel" }).click();

  await page.getByRole("button", { name: "Add state", exact: true }).click();
  await edit.getByRole("textbox", { name: /^Name/ }).fill("left");
  await edit.getByRole("textbox", { name: /^What people call it/ }).fill("Left");
  await edit.getByRole("button", { name: "Save", exact: true }).click();
  await expect(edit).toBeHidden();
  await page.getByRole("button", { name: "Publish", exact: true }).click();
  await expect(publish.getByText(/cannot be reached/)).toBeVisible();
  await expect(publish.getByRole("button", { name: "Publish", exact: true })).toBeDisabled();
});;

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

test("views: filters, order and visible columns survive refresh and return", async ({ page }) => {
  await signIn(page);
  await page.goto("/ontology/objects/build.opportunity");
  await expect(page.getByRole("heading", { level: 1, name: "Opportunities" })).toBeVisible();

  await page.getByRole("radio", { name: "Board" }).click();
  await expect(page).toHaveURL(/view=/);
  await page.getByRole("radio", { name: "Table" }).click();
  await page.getByRole("button", { name: "Columns", exact: true }).click();
  await page.getByRole("checkbox", { name: "Next step", exact: true }).uncheck();
  await page.keyboard.press("Escape");
  await expect(page.getByRole("columnheader", { name: "Next step", exact: true })).toHaveCount(0);

  const at = new URL(page.url());
  const view = JSON.parse(at.searchParams.get("view")!);
  view.filters = [{ field: "stage", operator: "is", value: { type: "enum", value: "Proposal" } }];
  view.sort = [{ sortKey: "amount", direction: "descending" }];
  view.search = "";
  at.searchParams.set("view", JSON.stringify(view));
  await page.goto(at.toString());
  await page.reload();
  const rows = page.getByRole("table").locator("tbody tr").filter({ has: page.getByRole("link") });
  await expect(rows.first()).toContainText("Robot cell integration");
  await page.getByRole("link", { name: "Robot cell integration" }).click();
  await page.getByRole("link", { name: "Opportunities", exact: true }).click();
  await expect(page).toHaveURL(/view=/);
  await expect(rows.first()).toContainText("Robot cell integration");
  await expect(page.getByRole("columnheader", { name: "Next step", exact: true })).toHaveCount(0);
});
