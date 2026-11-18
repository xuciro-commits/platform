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

  await page.getByRole("link", { name: "Press line retrofit" }).click();
  await expect(page.getByRole("combobox", { name: "Stage" })).toContainText("Negotiation");
  await expect(page.getByRole("heading", { level: 1, name: "Press line retrofit" })).toBeVisible();

  await expect(page.getByRole("heading", { level: 3, name: "Commercial" })).toBeVisible();
  await expect(page.getByRole("heading", { level: 3, name: "Follow-up" })).toBeVisible();
  await expect(page.getByRole("textbox", { name: "Loss reason" })).toHaveCount(0);
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
  const confirm = page.getByRole("alertdialog");
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

  const confirm = page.getByRole("alertdialog");
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
  await expect(page.getByRole("alertdialog")).toHaveCount(0);
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
  await expect(page).toHaveURL(/\/activity$/);
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
  const failed = decodeURIComponent(href!.split("/").at(-1)!);
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
