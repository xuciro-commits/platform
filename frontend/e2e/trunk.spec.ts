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
  await expect(page.getByRole("complementary").getByRole("button", { name: /Owner.*Dana Whitfield/ })).toBeVisible();

  const next = page.getByRole("textbox", { name: "Next step" });
  await expect(next).toHaveValue("Send revised terms");
  await next.fill("Call the plant manager");
  await next.press("Enter");
  await expect(page.getByRole("complementary").getByText("2", { exact: true })).toBeVisible();

  await page.reload();
  await expect(page.getByRole("textbox", { name: "Next step" })).toHaveValue("Call the plant manager");

  await page.getByRole("complementary").getByRole("button", { name: /Owner.*Dana Whitfield/ }).click();
  await page.getByRole("complementary").getByRole("combobox", { name: "Owner", exact: true }).fill("Omar");
  await page.getByRole("option", { name: /Omar Haddad/ }).click();
  await expect(page.getByRole("complementary").getByText("3", { exact: true })).toBeVisible();
  await page.reload();
  await expect(page.getByRole("complementary").getByRole("button", { name: /Owner.*Omar Haddad/ })).toBeVisible();
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

  await second.getByRole("complementary").getByRole("button", { name: /^Owner/ }).click();
  await second.getByRole("complementary").getByRole("combobox", { name: "Owner", exact: true }).fill("Omar");
  await second.getByRole("option", { name: /Omar Haddad/ }).click();
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
  await page.getByRole("link", { name: "Preview", exact: true }).click();
  await expect(page.getByRole("columnheader", { name: "Mobile", exact: true })).toBeVisible();
  await expect(page.getByText("Preview only", { exact: true })).toBeVisible();
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

test("object definition: create and bind an action type before publishing a transition", async ({ page }) => {
  await signIn(page);
  await page.goto("/ontology/objects/build.object/OBJ-workflow");
  await page.getByRole("button", { name: "Create transition", exact: true }).click();
  const transition = page.getByRole("dialog");
  await transition.getByRole("textbox", { name: "What people call it", exact: true }).fill("Activate contact");
  await transition.getByRole("button", { name: "Save", exact: true }).click();
  await expect(page.getByText("Transitions use the linked action types.", { exact: true })).toBeVisible();

  await page.getByRole("button", { name: "Publish", exact: true }).click();
  const publish = page.getByRole("dialog", { name: /Publish/ });
  await publish.getByRole("button", { name: "Publish", exact: true }).click();
  await expect(publish).toBeHidden();
  await page.goto("/ontology/objects/build.workflow");
  await page.getByRole("button", { name: "New", exact: true }).click();
  const create = page.getByRole("dialog");
  await create.getByRole("textbox", { name: /^Name/ }).fill("Ada Lovelace");
  await create.getByRole("button", { name: "Create", exact: true }).click();
  await page.getByRole("button", { name: "Activate contact", exact: true }).click();
  const confirm = page.getByRole("alertdialog");
  await confirm.getByRole("button", { name: "Activate contact", exact: true }).click();
  await expect(page.getByText("Active", { exact: true }).first()).toBeVisible();
});

test("object gate: create a type from zero, preview, publish and transition a record", async ({ page }) => {
  await signIn(page);
  await open(page, "Ontology", "Object types");
  await page.getByRole("button", { name: "New", exact: true }).click();
  const createType = page.getByRole("dialog");
  await createType.getByRole("combobox", { name: /^Project/ }).click();
  await page.getByRole("option", { name: "Sales", exact: true }).click();
  await createType.getByRole("textbox", { name: /^Name/ }).fill("gateitem");
  await createType.getByRole("textbox", { name: /^What people call it/ }).fill("Gate item");
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
    await state.getByRole("textbox", { name: /^Name/ }).fill(name);
    await state.getByRole("textbox", { name: /^What people call it/ }).fill(title);
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
  await page.goto("/ontology/objects/build.gateitem");
  await page.getByRole("button", { name: "New", exact: true }).click();
  const createRecord = page.getByRole("dialog");
  await createRecord.getByRole("button", { name: "Create", exact: true }).click();
  await page.getByRole("button", { name: "Activate gate item", exact: true }).click();
  const confirm = page.getByRole("alertdialog");
  await confirm.getByRole("button", { name: "Activate gate item", exact: true }).click();
  await expect(page.getByText("Active", { exact: true }).first()).toBeVisible();
});

test("applications: review and install a package from the marketplace", async ({ page }) => {
  await signIn(page);
  await open(page, "Delivery", "Marketplace");
  const provider = page.getByRole("listitem").filter({ hasText: "application.production-provider · 0.1.4" });
  await expect(provider).toBeVisible();
  await provider.getByRole("button", { name: "Review and install", exact: true }).click();
  const review = page.getByRole("dialog", { name: /Review and install/ });
  await expect(review).toContainText("application.production-provider");
  await review.getByRole("button", { name: "Install package", exact: true }).click();
  await expect(page).toHaveURL(/\/delivery\/installations$/);
  const installed = page.getByRole("listitem").filter({ hasText: "Production batch provider" });
  await expect(installed).toContainText("0.1.4");
  await expect(installed.getByRole("button", { name: "Manage installation", exact: true })).toBeVisible();
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

test("views: filters, order and visible columns survive refresh and return", async ({ page }) => {
  const recordRequests: URLSearchParams[] = [];
  page.on("request", (request) => {
    const url = new URL(request.url());
    if (url.pathname === "/v1/records/build.opportunity") recordRequests.push(url.searchParams);
  });
  await signIn(page);
  await page.goto("/ontology/objects/build.opportunity");
  await expect(page.getByRole("heading", { level: 1, name: "Opportunities" })).toBeVisible();
  await expect(page.getByText(/\d+ \/ \d+/, { exact: true })).toBeVisible();
  expect(recordRequests.at(-1)?.get("limit")).toBe("100");
  expect(recordRequests.at(-1)?.get("actions")).toBe("1");

  await page.getByRole("radio", { name: "Board" }).click();
  await expect(page).toHaveURL(/view=/);
  await page.getByRole("radio", { name: "Table" }).click();
  await page.getByRole("button", { name: "View options", exact: true }).click();
  await page.getByRole("button", { name: "Remove Next step", exact: true }).click();
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
  await expect.poll(() => recordRequests.at(-1)?.get("domain")).toContain("proposal");
  expect(recordRequests.at(-1)?.get("sort")).toBe("-amount");
  const rows = page.getByRole("table").locator("tbody tr").filter({ has: page.getByRole("link") });
  await expect(rows.first()).toContainText("Robot cell integration");
  await page.getByRole("link", { name: "Robot cell integration" }).click();
  await page.getByRole("link", { name: "Opportunities", exact: true }).click();
  await expect(page).toHaveURL(/view=/);
  await expect(rows.first()).toContainText("Robot cell integration");
  await expect(page.getByRole("columnheader", { name: "Next step", exact: true })).toHaveCount(0);
});

test("views: ten thousand rows stay windowed and load at the scroll edge", async ({ page }) => {
  const offsets: number[] = [];
  await page.route("**/v1/records/build.opportunity?*", async (route) => {
    const url = new URL(route.request().url());
    const offset = Number(url.searchParams.get("offset") ?? 0);
    const limit = Number(url.searchParams.get("limit") ?? 100);
    offsets.push(offset);
    const records = Array.from({ length: limit }, (_, index) => {
      const ordinal = offset + index + 1;
      return {
        id: `GEN-${String(ordinal).padStart(4, "0")}`,
        revision: 1,
        subject: `Generated opportunity ${String(ordinal).padStart(4, "0")}`,
        stage: "proposal",
        customer: null,
        amount: ordinal,
        budgetapproved: false,
        deposit: null,
        next: "",
        closing: "2027-01-01",
        changed: { at: "2026-11-18T00:00:00Z" },
      };
    });
    await route.fulfill({ json: { records, total: 10000, actions: {}, refs: {}, formats: {} } });
  });
  await signIn(page);
  await page.goto("/ontology/objects/build.opportunity");
  const resultCount = page;
  await expect(resultCount.getByText("100 / 10000", { exact: true })).toBeVisible();
  const table = page.getByRole("table");
  const scroller = page.getByRole("region", { name: "Records" });
  await expect(scroller).toBeVisible();
  expect(await table.locator("tbody tr").count()).toBeLessThan(100);

  await scroller.evaluate((element) => element.scrollTo({ top: element.scrollHeight }));
  await expect.poll(() => offsets.includes(100)).toBe(true);
  await expect(resultCount.getByText("200 / 10000", { exact: true })).toBeVisible();
  await scroller.evaluate((element) => element.scrollTo({ top: element.scrollHeight }));
  const secondPage = page.getByRole("link", { name: "Generated opportunity 0199" });
  await expect(secondPage).toBeVisible();
  await secondPage.click();
  await expect(page).toHaveURL(/build\.opportunity\/GEN-0199/);
  await page.goBack();
  await expect(resultCount.getByText(/200 \/ 10000|300 \/ 10000/)).toBeVisible();
  await expect(page.getByRole("link", { name: "Generated opportunity 0199" })).toBeVisible();
  await page.getByRole("radio", { name: "Board" }).click();
  const previousOffset = offsets.at(-1);
  const more = page.getByRole("button", { name: "Load more" }).first();
  await expect(more).toBeVisible();
  await more.click();
  await expect.poll(() => offsets.at(-1)).not.toBe(previousOffset);
});

test("views: save, pin and reopen a filtered view from the sidebar", async ({ page }) => {
  const view = {
    version: 1,
    layout: "table",
    columns: ["subject", "stage", "customer", "amount", "budgetapproved", "deposit", "next", "closing"],
    sort: [],
    filters: [{ field: "stage", operator: "is", value: { type: "enum", value: "Proposal" } }],
    search: "",
    group: "none",
    lanes: "",
  };
  await signIn(page);
  await page.goto(`/ontology/objects/build.opportunity?view=${encodeURIComponent(JSON.stringify(view))}`);
  await expect(page.getByRole("link", { name: "Robot cell integration" })).toBeVisible();

  await page.getByRole("button", { name: "Create saved view", exact: true }).click();
  const create = page.getByRole("dialog");
  await expect(create).toContainText("Stage is Proposal");
  await create.getByRole("textbox", { name: "View name" }).fill("My proposals");
  await create.getByRole("button", { name: "Create", exact: true }).click();
  await expect(page).toHaveURL(/saved=/);

  const sidebar = page.getByRole("navigation", { name: "Primary navigation" });
  await expect(sidebar.getByRole("link", { name: "My proposals" })).toBeVisible();
  await page.getByRole("button", { name: "Saved views", exact: true }).click();
  await expect(page.getByRole("button", { name: "My proposals", exact: true })).toHaveAttribute("aria-pressed", "true");
  await page.getByRole("button", { name: "Edit saved view", exact: true }).click();
  const edit = page.getByRole("dialog");
  await edit.getByRole("checkbox", { name: "Pin to the top", exact: true }).check();
  await edit.getByRole("button", { name: "Save", exact: true }).click();
  await expect(edit).toHaveCount(0);
  await page.getByRole("button", { name: "Edit saved view", exact: true }).click();
  await expect(page.getByRole("dialog").getByRole("checkbox", { name: "Pin to the top", exact: true })).toBeChecked();
  await page.keyboard.press("Escape");
  await page.getByRole("button", { name: "All", exact: true }).click();
  await expect(page).not.toHaveURL(/saved=/);

  await sidebar.getByRole("link", { name: "My proposals" }).click();
  await expect(page).toHaveURL(/saved=/);
  await expect(page.getByRole("link", { name: "Robot cell integration" })).toBeVisible();
  await page.reload();
  await expect(page.getByRole("link", { name: "Robot cell integration" })).toBeVisible();
});

test("views: a published type opens with its declared default view", async ({ page }) => {
  const requests: URLSearchParams[] = [];
  page.on("request", (request) => {
    const url = new URL(request.url());
    if (url.pathname === "/v1/records/build.customer") requests.push(url.searchParams);
  });
  await signIn(page);
  await page.goto("/ontology/objects/build.customer");
  await expect(page.getByRole("columnheader", { name: "Region", exact: true })).toBeVisible();
  await page.getByRole("button", { name: "View options", exact: true }).click();
  await page.getByRole("button", { name: "Grouping", exact: true }).click();
  await expect(page.getByRole("radio", { name: "Region", exact: true })).toBeChecked();
  await page.keyboard.press("Escape");
  await expect.poll(() => requests.at(-1)?.get("sort")).toBe("company");
  const rows = page.getByRole("table").locator("tbody tr").filter({ has: page.getByRole("link") });
  await expect(rows.first()).toContainText("Atlas Castings");
  await expect(rows.filter({ hasText: "Alder Medical Devices" })).toHaveCount(1);
  await expect(page.getByRole("table").getByText("europe", { exact: true }).first()).toBeVisible();
  await expect(page.getByRole("table").getByText("northamerica", { exact: true }).first()).toBeVisible();
});

test("files: an attachment field stores a media set item on the record", async ({ page }) => {
  await signIn(page);
  await page.goto("/ontology/objects/build.opportunity/OPP-001");
  await expect(page.getByText("Brief", { exact: true })).toBeVisible();
  await page.locator('input[type="file"]').setInputFiles({
    name: "brief.txt",
    mimeType: "text/plain",
    buffer: Buffer.from("Opportunity brief"),
  });
  await expect(page.getByText("brief.txt", { exact: true })).toBeVisible();
  const readAttachment = () => page.evaluate(async () => {
    const session = JSON.parse(sessionStorage.getItem("platform:session") ?? "{}") as { token: string; tenant: string };
    const response = await fetch("/v1/records/build.opportunity/OPP-001", {
      headers: { authorization: `Bearer ${session.token}`, "platform-tenant": session.tenant },
    });
    const answer = await response.json() as { values?: { brief?: { mediaSet?: string; item?: string } } };
    return answer.values?.brief;
  });
  await expect.poll(async () => (await readAttachment())?.mediaSet).toMatch(/^MEDIA-/);
  const attachment = await readAttachment();
  expect(attachment?.mediaSet).toMatch(/^MEDIA-/);
  expect(attachment?.item).toMatch(/^[0-9a-f]{64}$/);
  await page.reload();
  await expect(page.getByText("brief.txt", { exact: true })).toBeVisible();
});

test("list: one bar searches and filters, identities are quiet and references open their record", async ({ page }) => {
  await signIn(page);
  await page.goto("/ontology/objects");
  const identity = page.getByRole("cell", { name: "build.opportunity", exact: true });
  await expect(identity.locator('[data-type="code"]')).toBeVisible();
  await page.goto("/ontology/objects/build.opportunity");
  const table = page.getByRole("table");
  await expect(table.getByRole("link", { name: "Northwind Tools" }).first()).toBeVisible();
  const bar = page.getByRole("textbox", { name: "Search this list" });
  await bar.fill("Press");
  await bar.press("Enter");
  await expect(page).toHaveURL(/%22search%22%3A%22Press%22/);
  const found = table.locator("tbody tr").filter({ has: page.getByRole("link") });
  await expect(found).toHaveCount(1);
  const customer = found.getByRole("link").nth(1);
  const name = (await customer.textContent()) ?? "";
  await customer.click();
  await expect(page.getByRole("heading", { level: 1, name, exact: true })).toBeVisible();
});

test("record fields: values read as values and become inputs when focused", async ({ page }) => {
  await signIn(page);
  await page.goto("/ontology/objects/build.opportunity/OPP-001");
  const amount = page.getByRole("spinbutton", { name: "Amount", exact: true });
  await expect(amount).toHaveValue("184,000");
  await amount.focus();
  await expect(amount).toHaveValue("184000");
  await amount.blur();
  await expect(amount).toHaveValue("184,000");
});

test("files: an attachment waits for its project before offering file selection", async ({ page }) => {
  await signIn(page);
  const requested = Promise.withResolvers<void>();
  const ready = Promise.withResolvers<void>();
  await page.route("**/v1/records/build.app?limit=200", async (route) => {
    requested.resolve();
    await ready.promise;
    await route.continue();
  });
  await page.goto("/ontology/objects/build.opportunity/OPP-003");
  await requested.promise;
  const input = page.locator('input[type="file"]');
  try {
    await expect(page.getByText("Brief", { exact: true })).toBeVisible();
    await expect(input).toHaveCount(0);
  } finally {
    ready.resolve();
  }
  await expect(input).toBeEnabled();
});

test("applications: install the sample version and run a contributed record action", async ({ page }) => {
  await signIn(page);
  await open(page, "Delivery", "Marketplace");
  const provider = page.getByRole("listitem").filter({ hasText: "application.production-provider · 0.1.5" });
  await provider.getByRole("button", { name: "Review and install", exact: true }).click();
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
  const shopOrder = page.getByRole("textbox", { name: "First shop order", exact: true });
  await expect(shopOrder).toHaveValue("WO-1042");
  await page.reload();
  await expect(shopOrder).toHaveValue("WO-1042");
});

test("applications: shared records respect roles and keep their history after retirement", async ({ page, browser }) => {
  await signIn(page);
  await open(page, "Production order provider", "Production orders");
  const entry = page.getByRole("link", { name: "PO-1042", exact: true });
  const appRecord = new URL((await entry.getAttribute("href"))!, page.url());
  const recordURL = `/ontology/objects/build.batchproviderorder/${appRecord.pathname.split("/").at(-1)}`;
  const schema = "build.batchproviderorder.setshoporder";
  const member = await browser.newPage();
  await member.goto("/");
  await member.getByRole("button", { name: "builder-1 · acceptance" }).click();
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
  await expect(member.getByRole("textbox", { name: "First shop order", exact: true })).toHaveValue("WO-1043");
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

  await member.getByRole("button", { name: "Sign out", exact: true }).click();
  await member.getByRole("button", { name: "user-1 · acceptance" }).click();
  await member.goto(recordURL);
  await expect(member.getByRole("heading", { name: "PO-1042", exact: true })).toBeVisible();
  await expect(member.getByText("WO-1043", { exact: true })).toBeVisible();
  const menu = member.getByRole("button", { name: "More actions" });
  await menu.click();
  await expect(member.getByRole("menuitem", { name: "Start upstream production", exact: true })).toBeVisible();
  await expect(member.getByRole("menuitem", { name: "Set shop order", exact: true })).toHaveCount(0);
  expect((await direct(member)).code).toBe("ERROR_CODE_POLICY_DENIED");

  await open(page, "Delivery", "Installations");
  const installation = page.getByRole("listitem").filter({ hasText: "Production batch provider" });
  const manage = installation.getByRole("button", { name: "Manage installation", exact: true });
  await manage.click();
  const review = page.getByRole("dialog", { name: /Manage installation/ });
  await expect(review.getByText("Business records retained: 1", { exact: true })).toBeVisible();
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
  await expect(review.getByText("Business records retained: 1", { exact: true })).toBeVisible();
  await page.goto(appRecord.href);
  await expect(page.getByRole("textbox", { name: "First shop order", exact: true })).toHaveValue("WO-1043");
  const retiredMenu = page.getByRole("button", { name: "More actions" });
  if (await retiredMenu.isVisible()) await retiredMenu.click();
  await expect(page.getByRole("menuitem", { name: "Set shop order", exact: true })).toHaveCount(0);

  await member.goto(appRecord.href);
  await expect(member).toHaveURL((url) => url.pathname === recordURL);
  await expect(member.getByText("WO-1043", { exact: true })).toBeVisible();
  if (await menu.isVisible()) await menu.click();
  await expect(member.getByRole("menuitem", { name: "Start upstream production", exact: true })).toHaveCount(0);
  await expect(member.getByRole("link", { name: "Production order provider", exact: true })).toHaveCount(0);
  await member.goto(historyURL!);
  await expect(member).toHaveURL((url) => url.pathname === `${recordURL}/activity`);
  await expect(member.getByText(/First shop order: .*WO-1043/)).toBeVisible();
  await member.close();
});

test("inbox: work assigned on a record is finished from the inbox", async ({ page }) => {
  await signIn(page);
  await page.goto("/ontology/objects/build.opportunity/OPP-002");
  await page.getByRole("button", { name: "Assign work", exact: true }).click();
  const assign = page.getByRole("dialog");
  await assign.getByRole("textbox", { name: /^Title/ }).fill("Call the plant manager");
  await assign.getByRole("textbox", { name: "Assignee", exact: true }).fill("admin-1");
  await assign.getByRole("button", { name: "Create", exact: true }).click();
  await expect(assign).toHaveCount(0);

  await page.getByRole("button", { name: "Notifications", exact: true }).click();
  await page.getByRole("menuitem", { name: /Inbox/ }).click();
  await expect(page).toHaveURL(/\/home\/inbox/);
  await page.getByRole("link", { name: /To do/ }).click();
  await page.getByRole("row", { name: /Call the plant manager/ }).click();
  const pane = page.getByRole("complementary", { name: "Item" });
  await expect(pane.getByRole("heading", { name: "Call the plant manager" })).toBeVisible();
  await expect(pane.getByRole("link", { name: "Spare parts frame agreement" })).toBeVisible();
  await pane.getByRole("button", { name: "Done", exact: true }).click();
  await page.getByRole("link", { name: /Closed/ }).click();
  await expect(page.getByRole("row", { name: /Call the plant manager/ })).toBeVisible();
});

test("members: an opportunity owner is shown by name and can be reassigned", async ({ page }) => {
  await signIn(page);
  await open(page, "Ontology", "Objects");
  await page.getByRole("link", { name: "Opportunities" }).click();
  const row = page.getByRole("row", { name: /Furnace monitoring/ });
  const ownerCell = row.getByRole("cell", { name: /Dana Whitfield/ });
  await expect(ownerCell.getByRole("img", { name: "Dana Whitfield" })).toBeVisible();
  await expect(ownerCell.getByText("Dana Whitfield", { exact: true })).toBeVisible();

  await row.getByRole("link", { name: "Furnace monitoring" }).click();
  const properties = page.getByRole("complementary");
  const selected = properties.getByRole("button", { name: /Owner.*Dana Whitfield/ });
  await expect(selected).toBeVisible();
  await expect(properties.getByRole("img", { name: "Dana Whitfield" })).toBeVisible();
  await selected.click();
  await properties.getByRole("combobox", { name: "Owner", exact: true }).fill("Omar");
  await page.getByRole("option", { name: /Omar Haddad/ }).click();
  await expect(properties.getByText("2", { exact: true })).toBeVisible();
  await page.reload();
  await expect(properties.getByRole("button", { name: /Owner.*Omar Haddad/ })).toBeVisible();
  await expect(properties.getByRole("img", { name: "Omar Haddad" })).toBeVisible();
});

test("record fields: an attachment preserves the horizontal label alignment", async ({ page }) => {
  await signIn(page);
  await page.goto("/ontology/objects/build.opportunity/OPP-001");
  const brief = page.getByText("Brief", { exact: true });
  const customer = page.getByText("Customer", { exact: true });
  const amount = page.getByText("Amount", { exact: true });
  await expect(brief).toBeVisible();
  await expect(customer).toBeVisible();
  await expect(amount).toBeVisible();
  const briefBox = await brief.boundingBox();
  const customerBox = await customer.boundingBox();
  const amountBox = await amount.boundingBox();
  expect(briefBox).not.toBeNull();
  expect(customerBox).not.toBeNull();
  expect(amountBox).not.toBeNull();
  expect(customerBox?.x).toBe(amountBox?.x);
  expect(briefBox?.x).toBe(customerBox?.x);
});

test("record fields: a date uses its field name for assistive technology", async ({ page }) => {
  await signIn(page);
  await page.goto("/ontology/objects/build.opportunity/OPP-001");
  const date = page.getByRole("combobox", { name: "Expected close", exact: true });
  await expect(date).toBeVisible();
  await expect(date).toHaveAttribute("aria-label", "Expected close");
});

test("list: chips filter on the host, a row opens its details, the selection band replaces the bar", async ({ page }) => {
  const requests: URLSearchParams[] = [];
  page.on("request", (request) => {
    const url = new URL(request.url());
    if (url.pathname === "/v1/records/build.opportunity") requests.push(url.searchParams);
  });
  await signIn(page);
  await page.goto("/ontology/objects/build.opportunity");
  const header = page;
  await header.getByRole("combobox", { name: "Stage filter", exact: true }).click();
  await page.getByRole("option", { name: "Proposal", exact: true }).click();
  await page.keyboard.press("Escape");
  await expect.poll(() => requests.at(-1)?.get("domain")).toContain("proposal");
  const rows = page.getByRole("table").locator("tbody tr").filter({ has: page.getByRole("link") });
  await expect(rows).toHaveCount(5);
  await expect(header.getByText("5 / 5", { exact: true })).toBeVisible();

  await rows.filter({ hasText: "Robot cell integration" }).getByRole("cell").nth(2).click();
  const details = page.getByRole("complementary", { name: "Details" });
  await expect(details.getByRole("heading", { level: 2, name: "Robot cell integration" })).toBeVisible();
  await expect(details.getByText("Kestrel Robotics").first()).toBeVisible();
  await expect(page).toHaveURL(/build\.opportunity\?/);
  await details.getByRole("button", { name: "Close details", exact: true }).click();
  await expect(details).toHaveCount(0);

  await rows.first().getByRole("checkbox").check();
  await expect(header.getByText("1 selected")).toBeVisible();
  await expect(header.getByRole("textbox", { name: "Search this list" })).toHaveCount(0);
  await header.getByRole("button", { name: "Clear selection", exact: true }).click();
  await header.getByRole("button", { name: "Clear all", exact: true }).click();
  await expect.poll(() => rows.count()).toBeGreaterThan(5);

  const unapproved = { version: 1, layout: "table", columns: ["subject", "budgetapproved"], sort: [], filters: [{ field: "budgetapproved", operator: "is_false", value: { type: "empty" } }], search: "", group: "none", lanes: "" };
  await page.goto(`/ontology/objects/build.opportunity?view=${encodeURIComponent(JSON.stringify(unapproved))}`);
  await expect.poll(() => requests.at(-1)?.get("domain")).toBe(JSON.stringify([["budgetapproved", "!=", true]]));
  await expect.poll(() => rows.count()).toBeGreaterThan(5);
  await expect(rows.filter({ hasText: "Press line retrofit" })).toHaveCount(0);
});


test("record fields: the date label aligns with the other commercial fields", async ({ page }) => {
  await signIn(page);
  await page.goto("/ontology/objects/build.opportunity/OPP-001");
  const commercial = page.getByRole("heading", { name: "Commercial", exact: true }).locator("..");
  const labels = ["Amount", "Budget approved", "Proposed deposit", "Expected close"]
    .map((name) => commercial.getByText(name, { exact: true }));
  for (const label of labels) await expect(label).toBeVisible();
  const boxes = await Promise.all(labels.map((label) => label.boundingBox()));
  for (const box of boxes) expect(box?.x).toBe(boxes[0]!.x);
});

test("inbox: a comment mention opens the referenced record from the Mentions tab", async ({ page, browser, baseURL }) => {
  const author = await browser.newPage({ baseURL });
  await author.goto("/");
  await author.getByRole("button", { name: "user-1 · acceptance" }).click();
  const record = "/ontology/objects/build.opportunity/OPP-002";
  await author.goto(`${record}/activity`);
  const message = "@admin-1 Please review the spare parts agreement.";
  await author.getByRole("textbox", { name: "Write a comment" }).fill(message);
  await author.getByRole("button", { name: "Post comment" }).click();
  await expect(author.getByText(message, { exact: true })).toBeVisible();
  await author.close();

  await page.setViewportSize({ width: 1024, height: 768 });
  await signIn(page);
  await page.goto("/home/inbox");
  await page.getByRole("link", { name: /^Mentions \d+ unread$/ }).click();
  const mention = page.getByRole("row", { name: /Please review the spare parts agreement/ });
  await expect(mention).toBeVisible();
  await mention.click();
  await expect(page.getByText(message, { exact: true })).toBeVisible();
  const link = page.getByRole("link", { name: "Spare parts frame agreement", exact: true });
  await expect(link).toHaveAttribute("href", record);
  await page.getByRole("button", { name: "Inbox", exact: true }).click();
  await page.getByRole("link", { name: /^Notifications/ }).click();
  await expect(mention).toBeVisible();
});

test("command palette: open a record and the inbox with the keyboard", async ({ page }) => {
  await signIn(page);
  const palette = page.getByRole("dialog", { name: "Command palette" });
  const search = palette.getByRole("combobox", { name: "Search", exact: true });

  await page.keyboard.press("ControlOrMeta+K");
  await search.fill("Press line retrofit");
  await expect(palette.getByRole("group", { name: "Objects" }).getByRole("option", { name: "Press line retrofit" })).toBeVisible();
  await search.press("Enter");
  await expect(page).toHaveURL(/\/ontology\/objects\/build\.opportunity\/OPP-001$/);
  await expect(page.getByRole("heading", { name: "Press line retrofit", exact: true })).toBeVisible();

  await page.keyboard.press("ControlOrMeta+K");
  await expect(palette.getByRole("group", { name: "Recent" }).getByRole("option", { name: "Press line retrofit" })).toBeVisible();
  await search.fill("> Archive");
  await expect(palette.getByRole("group", { name: "Commands" }).getByRole("option", { name: "Archive opportunity", exact: true })).toBeVisible();
  await search.press("Enter");
  await expect(page.getByText("Archived", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Undo", exact: true }).click();
  await expect(page.getByText("Archived", { exact: true })).toHaveCount(0);

  await page.keyboard.press("ControlOrMeta+K");
  await search.fill("Inbox");
  await expect(palette.getByRole("option", { name: "Inbox", exact: true })).toBeVisible();
  await search.press("Enter");
  await expect(page).toHaveURL(/\/home\/inbox$/);
  await expect(page.getByRole("heading", { name: "Inbox", exact: true })).toBeVisible();
});
