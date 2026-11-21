import { type Page, expect, test } from "@playwright/test";
import type { Api } from "../src/host";

async function signIn(page: Page) {
  await page.goto("/");
  await page.getByRole("button", { name: "admin-1 · acceptance" }).click();
}

async function open(page: Page, app: string, entry: string) {
  await page.getByRole("link", { name: app }).click();
  await page.getByRole("navigation", { name: "Primary navigation" }).getByRole("link", { name: entry }).click();
}

test("legacy links: open records and mapped views, and explain unavailable views", async ({ page }) => {
  await signIn(page);
  const session = await page.evaluate(() => JSON.parse(sessionStorage.getItem("platform:session")!) as { token: string; tenant: string });
  const response = await page.request.get("/v1/resources", {
    headers: { authorization: `Bearer ${session.token}`, "platform-tenant": session.tenant },
  });
  expect(response.ok()).toBeTruthy();
  const directory: Api.ResourceDirectory = await response.json();
  const resource = directory.resources.find((entry) => entry.type === "build.object" && entry.draft === "OBJ-opportunity");
  expect(resource?.id).toMatch(/^ri\./);

  await page.goto("/#/objects/sales.opportunity/OPP-001");
  await expect(page).toHaveURL("/ontology/objects/sales.opportunity/OPP-001");
  await expect(page.getByRole("heading", { level: 1, name: "冲压线改造" })).toBeVisible();

  await page.goto(`/#/object-type/${resource!.id}`);
  await expect(page).toHaveURL("/ontology/object-types/OBJ-opportunity");
  await expect(page.getByRole("heading", { level: 1, name: "Opportunity", exact: true })).toBeVisible();

  await page.goto("/#/dataset");
  await expect(page).toHaveURL("/data/datasets");
  await expect(page.getByRole("heading", { level: 1, name: "Datasets", exact: true })).toBeVisible();

  await page.goto("/#/apps");
  await expect(page).toHaveURL("/delivery/marketplace");
  await expect(page.getByRole("heading", { level: 1, name: "Marketplace", exact: true })).toBeVisible();

  await page.goto("/#/demo");
  await expect(page).toHaveURL("/home/overview");
  const notifications = page.getByRole("region", { name: "Notifications", exact: true });
  await expect(notifications.getByText("This legacy link has no corresponding page in the new frontend.", { exact: true })).toBeVisible();
});

test("records: open a list, open a record, change a field in place", async ({ page }) => {
  await signIn(page);
  await open(page, "Ontology", "Objects");
  await page.getByRole("link", { name: "Opportunities" }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Opportunities" })).toBeVisible();
  const retrofit = page.getByRole("row", { name: /冲压线改造/ });
  await expect(retrofit.getByText("Negotiation", { exact: true })).toBeVisible();
  await expect(retrofit.getByText("184,000", { exact: true })).toBeVisible();
  await expect(retrofit.getByLabel("Negotiation")).toBeVisible();
  await expect(retrofit.getByRole("img", { name: "Yes" })).toBeVisible();
  await expect(retrofit.getByText("1,250.00 USD", { exact: true })).toBeVisible();

  await page.getByRole("link", { name: "冲压线改造" }).click();
  await expect(page.getByRole("combobox", { name: "Stage" })).toContainText("Negotiation");
  await expect(page.getByRole("heading", { level: 1, name: "冲压线改造" })).toBeVisible();

  await expect(page.getByRole("heading", { level: 3, name: "Commercial" })).toBeVisible();
  await expect(page.getByRole("heading", { level: 3, name: "Follow-up" })).toBeVisible();
  await expect(page.getByRole("textbox", { name: "Loss reason" })).toHaveCount(0);
  await expect(page.getByRole("switch", { name: "Budget approved" })).toBeChecked();
  await expect(page.getByRole("spinbutton", { name: "Proposed deposit", exact: true })).toHaveValue("1,250.00");
  await expect(page.getByRole("complementary").getByRole("button", { name: /Owner.*王丹/ })).toBeVisible();

  const next = page.getByRole("textbox", { name: "Next step" });
  await expect(next).toHaveValue("发送修订后的条款");
  await next.fill("Call the plant manager");
  await next.press("Enter");
  await expect(page.getByRole("complementary").getByText("2", { exact: true })).toBeVisible();

  await page.reload();
  await expect(page.getByRole("textbox", { name: "Next step" })).toHaveValue("Call the plant manager");

  await page.getByRole("complementary").getByRole("button", { name: /Owner.*王丹/ }).click();
  await page.getByRole("complementary").getByRole("combobox", { name: "Owner", exact: true }).fill("韩奥");
  await page.getByRole("option", { name: /韩奥/ }).click();
  await expect(page.getByRole("complementary").getByText("3", { exact: true })).toBeVisible();
  await page.reload();
  await expect(page.getByRole("complementary").getByRole("button", { name: /Owner.*韩奥/ })).toBeVisible();
});

test("cells: a list cell edits in place with the input its type asks for", async ({ page }) => {
  await signIn(page);
  await open(page, "Ontology", "Objects");
  await page.getByRole("link", { name: "Opportunities" }).click();
  const row = page.getByRole("row", { name: /会员数据集成/ });
  await row.getByText("Lost", { exact: true }).click();
  await expect(page.getByRole("complementary", { name: "Details" })).toBeVisible();
  await row.getByText("Lost", { exact: true }).click();
  await page.getByRole("option", { name: "Qualifying", exact: true }).click();
  await expect(row.getByText("Qualifying", { exact: true })).toBeVisible();
  await page.reload();
  const saved = page.getByRole("row", { name: /会员数据集成/ });
  await expect(saved.getByText("Qualifying", { exact: true })).toBeVisible();
  await saved.getByText("Qualifying", { exact: true }).click();
  await saved.getByText("Qualifying", { exact: true }).click();
  await page.getByRole("option", { name: "Lost", exact: true }).click();
  await expect(saved.getByText("Lost", { exact: true })).toBeVisible();
});

test("sections: a field shows only when its condition holds", async ({ page }) => {
  await signIn(page);
  await open(page, "Ontology", "Objects");
  await page.getByRole("link", { name: "Opportunities" }).click();
  await page.getByRole("link", { name: "车队追踪上线" }).click();
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
  await page.getByRole("link", { name: "仓库扫码枪" }).click();
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
  await page.getByRole("link", { name: "标签打印线" }).click();

  await page.getByRole("button", { name: "More actions" }).click();
  await page.getByRole("menuitem", { name: "Archive" }).click();
  await expect(page.getByRole("alertdialog")).toHaveCount(0);
  const undo = page.getByRole("button", { name: "Undo" });
  await expect(undo).toBeVisible();
  await expect(page.getByText("Archived", { exact: true })).toBeVisible();
  await undo.click();
  await expect(page.getByText("Archived", { exact: true })).toHaveCount(0);

  await page.getByRole("link", { name: "Opportunities" }).click();
  await expect(page.getByRole("link", { name: "标签打印线" })).toBeVisible();
});

test("conflict: the second writer sees both values and chooses", async ({ browser }) => {
  const pages = await Promise.all([browser.newPage(), browser.newPage()]);
  for (const page of pages) {
    await signIn(page);
    await open(page, "Ontology", "Objects");
    await page.getByRole("link", { name: "Opportunities" }).click();
    await page.getByRole("link", { name: "门店补货试点" }).click();
    await expect(page.getByRole("textbox", { name: "Next step" })).toHaveValue("商定试点门店");
  }
  const [first, second] = pages;

  await first.getByRole("textbox", { name: "Next step" }).fill("Pilot in four stores");
  await first.getByRole("textbox", { name: "Next step" }).press("Enter");
  await expect(first.getByRole("complementary").getByText("2", { exact: true })).toBeVisible();

  await second.getByRole("complementary").getByRole("button", { name: /^Owner/ }).click();
  await second.getByRole("complementary").getByRole("combobox", { name: "Owner", exact: true }).fill("韩奥");
  await second.getByRole("option", { name: /韩奥/ }).click();
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
  await form.getByRole("textbox", { name: "Full name" }).fill("白依宁");
  await form.getByRole("button", { name: "Create" }).click();
  await expect(page.getByRole("heading", { level: 1, name: "白依宁" })).toBeVisible();
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
  await page.getByRole("link", { name: "熔炉监测" }).click();
  await page.getByRole("textbox", { name: "Next step" }).fill("Book the site walk");
  await page.getByRole("link", { name: "Opportunities" }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Opportunities" })).toBeVisible();
  await page.reload();
  await page.getByRole("link", { name: "熔炉监测" }).click();
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
  await visit("Ontology", "Objects", "Opportunities", "冲压线改造");
  await page.getByRole("link", { name: "Activity" }).click();
  await expect(page).toHaveURL(/\/activity(?:\?|$)/);
  await expect(page.getByText("Subject: 冲压线改造")).toBeVisible();
  await visit("Ontology", "Objects", "Customers", "北风工具");
  await visit("Ontology", "Object types", null, "Opportunity");
});

test("batch: retries a stale row without submitting a successful row again", async ({ page, context }) => {
  await signIn(page);
  await open(page, "Ontology", "Objects");
  await page.getByRole("link", { name: "Opportunities" }).click();
  for (const name of ["支持合同续约", "季节性备货计划"]) {
    await page.getByRole("row", { name: new RegExp(name) }).getByRole("checkbox").check();
  }
  const editing = await context.newPage();
  const href = await page.getByRole("link", { name: "季节性备货计划" }).getAttribute("href");
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
    if (body && body.schema.name === "sales.opportunity.archive") archived.push(body.target.id);
  });
  await page.getByRole("button", { name: "Archive opportunity", exact: true }).click();
  await expect(page.getByText("支持合同续约: Done", { exact: true })).toBeVisible();
  await expect(page.getByText(/季节性备货计划:.*changed/)).toBeVisible();
  expect(archived).toHaveLength(2);
  const failed = decodeURIComponent(new URL(href!, "http://local").pathname.split("/").at(-1)!);
  expect(archived).toContain(failed);
  await page.getByRole("button", { name: "Retry failed rows", exact: true }).click();
  await expect(page.getByText("季节性备货计划: Done", { exact: true })).toBeVisible();
  expect(archived).toHaveLength(3);
  expect(archived[2]).toBe(failed);
});

test("leave: a refused inline edit asks whether to stay or discard", async ({ page }) => {
  await signIn(page);
  await open(page, "Ontology", "Objects");
  await page.getByRole("link", { name: "Opportunities" }).click();
  await page.getByRole("link", { name: "车队追踪上线" }).click();
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
  await page.getByRole("link", { name: "车队追踪上线" }).click();
  await expect(subject).toHaveValue("车队追踪上线");
});

test("mobile: the navigation drawer contains the application controls", async ({ page }) => {
  await page.setViewportSize({ width: 375, height: 812 });
  await signIn(page);
  await page.getByRole("button", { name: "Open navigation" }).click();
  const drawer = page.getByRole("dialog");
  await expect(drawer.getByRole("button", { name: "Search", exact: true })).toBeVisible();
  await expect(drawer.getByRole("button", { name: "Notifications", exact: true })).toBeVisible();
  await drawer.getByRole("button", { name: "Account", exact: true }).click();
  await page.getByRole("menuitem", { name: "Sign out", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Sign in", exact: true })).toBeVisible();
});

test("reference: search by name, save the selected record and follow its link", async ({ page }) => {
  await signIn(page);
  await open(page, "Ontology", "Objects");
  await page.getByRole("link", { name: "Opportunities" }).click();
  await page.getByRole("link", { name: "冲压线改造" }).click();
  await page.getByRole("button", { name: /Customer.*北风工具/ }).click();
  await page.getByRole("combobox", { name: "Customer", exact: true }).fill("哈特");
  await page.getByRole("option", { name: /哈特曼/ }).click();
  await expect(page.getByRole("link", { name: "哈特曼制造", exact: true })).toBeVisible();
  await page.reload();
  const customer = page.getByRole("link", { name: "哈特曼制造", exact: true });
  await expect(customer).toBeVisible();
  await customer.hover();
  await expect(page.getByRole("heading", { name: "哈特曼制造", exact: true })).toBeVisible();
  await customer.click();
  await expect(page.getByRole("heading", { level: 1, name: "哈特曼制造", exact: true })).toBeVisible();
});

test("lines: add and edit a child row from its parent record", async ({ page }) => {
  await signIn(page);
  await open(page, "Ontology", "Objects");
  await page.getByRole("link", { name: "Opportunities" }).click();
  await page.getByRole("link", { name: "冲压线改造" }).click();
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
  await page.getByRole("link", { name: "仓库扫码枪" }).click();
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
  await page.getByRole("link", { name: "北风工具" }).click();
  await page.getByRole("link", { name: "Links", exact: true }).click();
  await expect(page).toHaveURL(/\/links(?:\?|$)/);
  await expect(page.getByRole("link", { name: "备件框架协议" })).toBeVisible();
  await page.getByRole("link", { name: "View all", exact: true }).click();
  await expect(page).toHaveURL(/sales.opportunity\?domain=/);
  await expect(page.getByRole("link", { name: "备件框架协议" })).toBeVisible();
  await expect(page.getByRole("link", { name: "仓库扫码枪" })).toHaveCount(0);
  await page.getByRole("link", { name: "备件框架协议" }).click();
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
  await page.goto("/ontology/objects/sales.opportunity/OPP-001");
  await expect(page.getByRole("heading", { level: 1, name: "冲压线改造" })).toBeVisible();
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
  await page.keyboard.insertText("北风");
  await expect(page.getByRole("option", { name: /北风工具/ })).toBeVisible();
  await page.keyboard.press("Enter");
  await expect(page.getByRole("link", { name: "北风工具", exact: true })).toBeVisible();
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
  await page.getByRole("link", { name: "冲压线改造" }).click();
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
  await edit.getByRole("textbox", { name: /^API name/ }).fill("left");
  await edit.getByRole("textbox", { name: /^Name/ }).fill("Left");
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

test("gate: build a type and use it, install a package and use its action, then check each role", async ({ page, browser }) => {
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

  await open(page, "Delivery", "Marketplace");
  const provider = page.getByRole("listitem").filter({ hasText: "application.production-provider · 0.1.4" });
  await provider.getByRole("button", { name: "Review and install", exact: true }).click();
  const review = page.getByRole("dialog", { name: /Review and install/ });
  await expect(review).toContainText("application.production-provider");
  await review.getByRole("button", { name: "Install package", exact: true }).click();
  await expect(page).toHaveURL(/\/delivery\/installations$/);
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
  await page.getByRole("button", { name: "More actions" }).click();
  await page.getByRole("menuitem", { name: "Start upstream production", exact: true }).click();
  const contributed = page.getByRole("dialog", { name: "Start upstream production", exact: true });
  await contributed.getByRole("textbox", { name: /^Shop order/ }).fill("WO-GATE-1");
  await contributed.getByRole("button", { name: "Start upstream production", exact: true }).click();
  await expect(contributed).toBeHidden();
  await expect(page.getByText("Running", { exact: true }).first()).toBeVisible();

  for (const seat of ["builder-1", "user-1"]) {
    const context = await browser.newContext();
    const member = await context.newPage();
    await member.goto("/");
    await member.getByRole("button", { name: `${seat} · acceptance` }).click();
    await expect(member.getByRole("link", { name: "Home", exact: true })).toBeVisible();
    await expect(member.getByRole("link", { name: "Admin", exact: true })).toHaveCount(0);
    await expect(member.getByRole("link", { name: "Host", exact: true })).toHaveCount(0);
    await context.close();
  }
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
    if (url.pathname === "/v1/records/sales.opportunity" && url.searchParams.has("actions")) recordRequests.push(url.searchParams);
  });
  await signIn(page);
  await page.goto("/ontology/objects/sales.opportunity");
  await expect(page.getByRole("heading", { level: 1, name: "Opportunities" })).toBeVisible();
  await expect(page.getByRole("group", { name: "Filter", exact: true }).getByText(/^\d+$/, { exact: true })).toBeVisible();
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
  await expect(rows.first()).toContainText("机器人工作站集成");
  await page.getByRole("link", { name: "机器人工作站集成" }).click();
  await page.getByRole("link", { name: "Opportunities", exact: true }).click();
  await expect(page).toHaveURL(/view=/);
  await expect(rows.first()).toContainText("机器人工作站集成");
  await expect(page.getByRole("columnheader", { name: "Next step", exact: true })).toHaveCount(0);
});

test("list: metadata controls default columns, layout and empty states", async ({ page }) => {
  await signIn(page);
  await open(page, "Ontology", "Object types");
  const apiName = page.getByRole("columnheader", { name: "API name", exact: true });
  await expect(apiName).toBeVisible();
  await expect(page.getByRole("columnheader", { name: "Plural", exact: true })).toHaveCount(0);
  await apiName.getByRole("button").click();
  await expect(apiName).toHaveAttribute("aria-sort", "ascending");
  await page.getByRole("button", { name: "View options", exact: true }).click();
  await expect(page.getByRole("button", { name: "Add Plural", exact: true })).toBeVisible();
  await page.keyboard.press("Escape");

  const search = page.getByRole("textbox", { name: "Search this list" });
  await search.fill("No object matches this phrase");
  await search.press("Enter");
  await expect(page.getByRole("heading", { name: "No matches", exact: true })).toBeVisible();
  await page.getByRole("main", { name: "Object types", exact: true }).getByRole("button", { name: "Clear all", exact: true }).click();
  await expect(apiName).toBeVisible();

  await page.route("**/v1/records/build.object?*", (route) => route.fulfill({ json: { records: [], total: 0 } }));
  await page.reload();
  await expect(page.getByRole("heading", { name: "Nothing here yet", exact: true })).toBeVisible();
  await expect(page.getByRole("main", { name: "Object types", exact: true }).getByRole("button", { name: "New", exact: true })).toBeVisible();
  await page.unroute("**/v1/records/build.object?*");

  await page.goto("/ontology/objects/sales.opportunity");
  await page.getByRole("radio", { name: "Board" }).click();
  await expect(page.getByRole("region", { name: "Proposal", exact: true })).toBeVisible();
  await expect(page.getByText(/Amount · Total ·/).first()).toBeVisible();

  await open(page, "Operations", "Logs");
  await expect(page.getByRole("table")).toBeVisible();
  await expect(page.getByRole("radio", { name: "Board" })).toHaveCount(0);
  await expect(page.getByRole("table").getByRole("checkbox")).toHaveCount(0);
});

test("views: ten thousand rows stay windowed and load at the scroll edge", async ({ page }) => {
  const offsets: number[] = [];
  await page.route("**/v1/records/sales.opportunity?*", async (route) => {
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
  await page.goto("/ontology/objects/sales.opportunity");
  const resultCount = page;
  await expect(resultCount.getByText("10000", { exact: true })).toBeVisible();
  const table = page.getByRole("table");
  const scroller = page.getByRole("region", { name: "Records" });
  await expect(scroller).toBeVisible();
  expect(await table.locator("tbody tr").count()).toBeLessThan(100);

  await scroller.evaluate((element) => element.scrollTo({ top: element.scrollHeight }));
  await expect.poll(() => offsets.includes(100)).toBe(true);
  await scroller.evaluate((element) => element.scrollTo({ top: element.scrollHeight }));
  const secondPage = page.getByRole("link", { name: "Generated opportunity 0199" });
  await expect(secondPage).toBeVisible();
  await secondPage.click();
  await expect(page).toHaveURL(/sales\.opportunity\/GEN-0199/);
  await page.goBack();
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
  const preference = page.waitForResponse((response) => {
    const url = decodeURIComponent(response.url());
    return url.includes("/v1/records/work.viewpreference") && url.includes("admin-1@sales.opportunity");
  });
  await page.goto(`/ontology/objects/sales.opportunity?view=${encodeURIComponent(JSON.stringify(view))}`);
  expect((await preference).status()).toBe(200);
  await expect(page.getByRole("link", { name: "机器人工作站集成" })).toBeVisible();

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
  const search = page.getByRole("textbox", { name: "Search this list" });
  await search.fill("机器人");
  await search.press("Enter");
  await expect(page.getByText("Unsaved view changes")).toBeVisible();
  await page.getByRole("button", { name: "Reset view", exact: true }).click();
  await expect(search).toHaveValue("");
  await expect(page.getByText("Unsaved view changes")).toHaveCount(0);
  await search.fill("机器人");
  await search.press("Enter");
  await page.getByRole("button", { name: "Save", exact: true }).click();
  await expect(page.getByText("Unsaved view changes")).toHaveCount(0);
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
  await expect(page.getByRole("link", { name: "机器人工作站集成" })).toBeVisible();
  await page.reload();
  await expect(page.getByRole("link", { name: "机器人工作站集成" })).toBeVisible();
});

test("views: a published type opens with its declared default view", async ({ page }) => {
  const requests: URLSearchParams[] = [];
  page.on("request", (request) => {
    const url = new URL(request.url());
    if (url.pathname === "/v1/records/sales.customer") requests.push(url.searchParams);
  });
  await signIn(page);
  await page.goto("/ontology/objects/sales.customer");
  await expect(page.getByRole("columnheader", { name: "Company", exact: true })).toBeVisible();
  await expect(page.getByRole("columnheader", { name: "Region", exact: true })).toHaveCount(0);
  await page.getByRole("button", { name: "View options", exact: true }).click();
  await page.getByRole("button", { name: "Grouping", exact: true }).click();
  await expect(page.getByRole("radio", { name: "Region", exact: true })).toBeChecked();
  await page.keyboard.press("Escape");
  await expect.poll(() => requests.at(-1)?.get("sort")).toBe("company");
  const rows = page.getByRole("table").locator("tbody tr").filter({ has: page.getByRole("link") });
  await expect(rows.nth(1)).toContainText("子午诊所");
  await expect(rows.filter({ hasText: "桤木医疗器械" })).toHaveCount(1);
  await expect(page.getByRole("row", { name: /^Collapse group Europe/ })).toBeVisible();
  await expect(page.getByRole("row", { name: /^Collapse group North America/ })).toBeVisible();
});

test("files: an attachment field stores a media set item on the record", async ({ page }) => {
  await signIn(page);
  await page.goto("/ontology/objects/sales.opportunity/OPP-001");
  await expect(page.getByText("Brief", { exact: true })).toBeVisible();
  await page.locator('input[type="file"]').setInputFiles({
    name: "brief.txt",
    mimeType: "text/plain",
    buffer: Buffer.from("Opportunity brief"),
  });
  await expect(page.getByText("brief.txt", { exact: true })).toBeVisible();
  const readAttachment = () => page.evaluate(async () => {
    const session = JSON.parse(sessionStorage.getItem("platform:session") ?? "{}") as { token: string; tenant: string };
    const response = await fetch("/v1/records/sales.opportunity/OPP-001", {
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
  const identity = page.getByRole("cell", { name: "sales.opportunity", exact: true });
  await expect(identity.locator('[data-type="code"]')).toBeVisible();
  await page.goto("/ontology/objects/sales.opportunity");
  const table = page.getByRole("table");
  await expect(table.getByRole("link", { name: "北风工具" }).first()).toBeVisible();
  const bar = page.getByRole("textbox", { name: "Search this list" });
  await bar.fill("冲压");
  await bar.press("Enter");
  await expect(page).toHaveURL(new RegExp(encodeURIComponent('"search":"冲压"')));
  const found = table.locator("tbody tr").filter({ has: page.getByRole("link") });
  await expect(found).toHaveCount(1);
  const customer = found.getByRole("link").nth(1);
  const name = (await customer.textContent()) ?? "";
  await customer.click();
  await expect(page.getByRole("heading", { level: 1, name, exact: true })).toBeVisible();
});

test("record fields: values read as values and become inputs when focused", async ({ page }) => {
  await signIn(page);
  await page.goto("/ontology/objects/sales.opportunity/OPP-001");
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
  await page.goto("/ontology/objects/sales.opportunity/OPP-003");
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

  await member.getByRole("button", { name: "Account", exact: true }).click();
  await member.getByRole("menuitem", { name: "Sign out", exact: true }).click();
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
  await expect(review.getByText("Business records retained: 2", { exact: true })).toBeVisible();
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
  await expect(review.getByText("Business records retained: 2", { exact: true })).toBeVisible();
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
  await page.goto("/ontology/objects/sales.opportunity/OPP-002");
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
  await expect(pane.getByRole("link", { name: "备件框架协议" })).toBeVisible();
  await pane.getByRole("button", { name: "Done", exact: true }).click();
  await page.getByRole("link", { name: /Closed/ }).click();
  await expect(page.getByRole("row", { name: /Call the plant manager/ })).toBeVisible();
});

test("members: an opportunity owner is shown by name and can be reassigned", async ({ page }) => {
  await signIn(page);
  await open(page, "Ontology", "Objects");
  await page.getByRole("link", { name: "Opportunities" }).click();
  const row = page.getByRole("row", { name: /熔炉监测/ });
  const ownerCell = row.getByRole("cell", { name: /王丹/ });
  await expect(ownerCell.getByRole("img", { name: "王丹" })).toBeVisible();
  await expect(ownerCell.getByText("王丹", { exact: true })).toBeVisible();

  await row.getByRole("link", { name: "熔炉监测" }).click();
  const properties = page.getByRole("complementary");
  const selected = properties.getByRole("button", { name: /Owner.*王丹/ });
  await expect(selected).toBeVisible();
  await expect(properties.getByRole("img", { name: "王丹" })).toBeVisible();
  await selected.click();
  await properties.getByRole("combobox", { name: "Owner", exact: true }).fill("韩奥");
  await page.getByRole("option", { name: /韩奥/ }).click();
  await expect(properties.getByText("2", { exact: true })).toBeVisible();
  await page.reload();
  await expect(properties.getByRole("button", { name: /Owner.*韩奥/ })).toBeVisible();
  await expect(properties.getByRole("img", { name: "韩奥" })).toBeVisible();
});

test("record fields: an attachment preserves the horizontal label alignment", async ({ page }) => {
  await signIn(page);
  await page.goto("/ontology/objects/sales.opportunity/OPP-001");
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
  await page.goto("/ontology/objects/sales.opportunity/OPP-001");
  const date = page.getByRole("combobox", { name: "Expected close", exact: true });
  await expect(date).toBeVisible();
  await expect(date).toHaveAttribute("aria-label", "Expected close");
});

test("list: chips filter on the host, a row opens its details and selection opens the bottom bar", async ({ page }) => {
  await page.setViewportSize({ width: 1500, height: 900 });
  const requests: URLSearchParams[] = [];
  page.on("request", (request) => {
    const url = new URL(request.url());
    if (url.pathname === "/v1/records/sales.opportunity") requests.push(url.searchParams);
  });
  await signIn(page);
  await page.goto("/ontology/objects/sales.opportunity");
  const header = page;
  await header.getByRole("combobox", { name: "Stage filter", exact: true }).click();
  await page.getByRole("option", { name: "Proposal", exact: true }).click();
  await page.keyboard.press("Escape");
  await expect.poll(() => requests.filter((params) => params.has("actions")).at(-1)?.get("domain")).toContain("proposal");
  const rows = page.getByRole("table").locator("tbody tr").filter({ has: page.getByRole("link") });
  await expect(rows).toHaveCount(5);
  await expect(header.getByText(/^5 \/ [1-9]\d*$/).filter({ hasNotText: "5 / 5" })).toBeVisible();

  await rows.filter({ hasText: "机器人工作站集成" }).getByRole("cell").nth(2).click();
  const details = page.getByRole("complementary", { name: "Details" });
  await expect(details.getByRole("heading", { level: 2, name: "机器人工作站集成" })).toBeVisible();
  await expect(details.getByText("红隼机器人").first()).toBeVisible();
  await expect(page).toHaveURL(/sales\.opportunity\?/);
  await details.getByRole("button", { name: "Close details", exact: true }).click();
  await expect(details).toHaveCount(0);

  await rows.first().getByRole("checkbox").check();
  const batch = page.getByRole("region", { name: "Batch actions" });
  await expect(batch.getByText("1 selected")).toBeVisible();
  await expect(header.getByRole("textbox", { name: "Search this list" })).toBeVisible();
  await batch.getByRole("button", { name: "Clear selection", exact: true }).click();
  await header.getByRole("button", { name: "Clear all", exact: true }).click();
  await expect.poll(() => rows.count()).toBeGreaterThan(5);

  const unapproved = { version: 1, layout: "table", columns: ["subject", "budgetapproved"], sort: [], filters: [{ field: "budgetapproved", operator: "is_false", value: { type: "empty" } }], search: "", group: "none", lanes: "" };
  await page.goto(`/ontology/objects/sales.opportunity?view=${encodeURIComponent(JSON.stringify(unapproved))}`);
  await expect.poll(() => requests.filter((params) => params.has("actions")).at(-1)?.get("domain")).toBe(JSON.stringify([["budgetapproved", "!=", true]]));
  await expect.poll(() => rows.count()).toBeGreaterThan(5);
  await expect(rows.filter({ hasText: "冲压线改造" })).toHaveCount(0);
});


test("record fields: the date label aligns with the other commercial fields", async ({ page }) => {
  await signIn(page);
  await page.goto("/ontology/objects/sales.opportunity/OPP-001");
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
  const record = "/ontology/objects/sales.opportunity/OPP-002";
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
  const link = page.getByRole("link", { name: "备件框架协议", exact: true });
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
  await search.fill("冲压线改造");
  await expect(palette.getByRole("group", { name: "Objects" }).getByRole("option", { name: "冲压线改造" })).toBeVisible();
  await search.press("Enter");
  await expect(page).toHaveURL(/\/ontology\/objects\/sales\.opportunity\/OPP-001$/);
  await expect(page.getByRole("heading", { name: "冲压线改造", exact: true })).toBeVisible();

  await page.keyboard.press("ControlOrMeta+K");
  await expect(palette.getByRole("group", { name: "Recent" }).getByRole("option", { name: "冲压线改造" })).toBeVisible();
  await search.fill("> Archive");
  await expect(palette.getByRole("group", { name: "Commands" }).getByRole("option", { name: "Archive opportunity", exact: true })).toBeVisible();
  await search.press("Enter");
  await expect(page.getByText("Archived", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Undo", exact: true }).click();
  await expect(page.getByText("Archived", { exact: true })).toHaveCount(0);

  await page.keyboard.press("ControlOrMeta+K");
  await search.fill("Opportunity");
  await expect(palette.getByRole("group", { name: "Objects" }).getByRole("option", { name: "冲压线改造" })).toBeVisible();
  await palette.getByRole("group", { name: "Resources" }).getByRole("option", { name: "Opportunity", exact: true }).click();
  await expect(page).toHaveURL(/\/ontology\/object-types\/OBJ-opportunity$/);
  await expect(page.getByRole("heading", { name: "Opportunity", exact: true })).toBeVisible();

  await page.keyboard.press("ControlOrMeta+K");
  await search.fill("Inbox");
  await expect(palette.getByRole("option", { name: "Inbox", exact: true })).toBeVisible();
  await search.press("Enter");
  await expect(page).toHaveURL(/\/home\/inbox$/);
  await expect(page.getByRole("heading", { name: "Inbox", exact: true })).toBeVisible();
});

test("member menu: language and appearance live in the profile, sign out returns to sign-in", async ({ page }) => {
  await signIn(page);
  const choose = async (menu: string, item: string, option: string | RegExp) => {
    await page.getByRole("button", { name: menu, exact: true }).click();
    await page.getByRole("menuitem", { name: item, exact: true }).click();
    await page.getByRole("menuitemradio", { name: option }).click();
  };

  await choose("Account", "Language", /中文/);
  await expect(page.getByRole("button", { name: "搜索", exact: true }).first()).toBeVisible();
  await page.reload();
  await expect(page.getByRole("button", { name: "账户", exact: true })).toBeVisible();

  await choose("账户", "外观", "深色");
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  await page.reload();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");

  await choose("账户", "外观", "跟随系统");
  await choose("账户", "语言", "自动");
  await expect(page.getByRole("button", { name: "Account", exact: true })).toBeVisible();

  await page.getByRole("button", { name: "Account", exact: true }).click();
  await page.getByRole("menuitem", { name: "Sign out", exact: true }).click();
  await expect(page.getByRole("button", { name: "admin-1 · acceptance" })).toBeVisible();
});

test("vocabulary: a workspace translation wins over the shipped one and reverts", async ({ page }) => {
  await signIn(page);
  await open(page, "Admin", "Language and vocabulary");
  await page.getByRole("textbox", { name: "Search this list" }).fill("Opportunity");
  await page.getByRole("textbox", { name: "Search this list" }).press("Enter");
  const row = page.getByRole("row").filter({ hasText: "Types and fields" }).filter({ has: page.getByRole("cell", { name: "Opportunity", exact: true }) });
  await expect(row).toContainText("Missing");
  await row.getByRole("cell").nth(1).click();
  await row.getByRole("cell").nth(1).click();
  await row.getByRole("textbox").fill("商机");
  await row.getByRole("textbox").press("Enter");
  await expect(row).toContainText("商机");
  await expect(row).toContainText("Overridden");
  await expect(row).toContainText("This workspace");
  await row.getByRole("cell", { name: "Opportunity", exact: true }).click();
  await page.getByRole("button", { name: "Revert to the shipped translation", exact: true }).click();
  await page.getByRole("alertdialog").getByRole("button", { name: "Revert to the shipped translation", exact: true }).click();
  await expect(row).toContainText("Missing");
});

test("tenants: switching keeps the entry and replaces what the page shows", async ({ page }) => {
  await signIn(page);
  await open(page, "Ontology", "Object types");
  const opportunity = page.getByRole("row").filter({ hasText: "opportunityline" });
  await expect(opportunity).toBeVisible();
  const tenant = async (name: string) => {
    await page.getByRole("button", { name: "Account", exact: true }).click();
    await page.getByRole("group", { name: "Tenant" }).getByRole("menuitemradio", { name, exact: true }).click();
  };

  await tenant("publisher");
  await expect(page).toHaveURL(/\/ontology\/object-types$/);
  await expect(page.getByRole("heading", { level: 2, name: "Nothing here yet", exact: true })).toBeVisible();
  await expect(opportunity).toHaveCount(0);

  await tenant("acceptance");
  await expect(opportunity).toBeVisible();
});

test("sign-in: a work account signs in by password or single sign-on and lands where it was", async ({ page }) => {
  await page.route("**/v1/sign-in", (route) => route.fulfill({ json: { issuer: "http://localhost:8497/auth/v1/", client: "platform-web" } }));
  await page.route("**/auth/v1/oidc/token", (route) => {
    const form = new URLSearchParams(route.request().postData() ?? "");
    const refused = form.get("grant_type") === "password" ? form.get("password") !== "right" : !form.get("code_verifier") || form.get("code") !== "granted";
    return route.fulfill(refused ? { status: 400, json: { error: "invalid_grant" } } : { json: { access_token: "admin", token_type: "Bearer" } });
  });
  await page.route("**/auth/v1/oidc/authorize?*", (route) => {
    const asked = new URL(route.request().url()).searchParams;
    return route.fulfill({ status: 302, headers: { location: `${asked.get("redirect_uri")}?code=granted&state=${asked.get("state")}` } });
  });
  const objectTypes = page.getByRole("heading", { name: "Object types", exact: true });

  await page.goto("/ontology/object-types");
  await page.getByRole("textbox", { name: "Email" }).fill("admin@platform.test");
  await page.getByLabel("Password").fill("wrong");
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(page.getByText("The email or password is not right.").first()).toBeVisible();
  await page.getByLabel("Password").fill("right");
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(objectTypes).toBeVisible();
  await expect(page).toHaveURL(/\/ontology\/object-types$/);

  await page.getByRole("button", { name: "Account", exact: true }).click();
  await page.getByRole("menuitem", { name: "Sign out", exact: true }).click();
  await page.goto("/#/object-type");
  await page.getByRole("button", { name: "Continue with single sign-on", exact: true }).click();
  await expect(objectTypes).toBeVisible();
  await expect(page).toHaveURL(/\/ontology\/object-types$/);
});

test("settings: the profile is edited in place and the member menu follows", async ({ page }) => {
  await signIn(page);
  const openSettings = async () => {
    await page.getByRole("button", { name: "Account", exact: true }).click();
    await page.getByRole("menuitem", { name: "Settings", exact: true }).click();
    return page.getByRole("dialog", { name: "Settings" });
  };
  let settings = await openSettings();
  const name = settings.getByRole("textbox", { name: "Display name", exact: true });
  const before = await name.inputValue();
  await name.fill("Dana W.");
  await name.press("Enter");
  await settings.getByRole("navigation", { name: "Settings sections" }).getByText("Login & security", { exact: true }).click();
  await expect(settings.getByText("This session", { exact: true })).toBeVisible();
  await expect(settings.getByRole("button", { name: "Sign out others", exact: true })).toBeDisabled();
  await settings.getByRole("button", { name: "Close", exact: true }).click();

  await page.getByRole("button", { name: "Account", exact: true }).click();
  await expect(page.getByRole("menuitem", { name: /Dana W\./ })).toBeVisible();
  await page.keyboard.press("Escape");

  settings = await openSettings();
  await settings.getByRole("textbox", { name: "Display name", exact: true }).fill(before);
  await settings.getByRole("textbox", { name: "Display name", exact: true }).press("Enter");
  await settings.getByRole("button", { name: "Close", exact: true }).click();
  await page.getByRole("button", { name: "Account", exact: true }).click();
  await expect(page.getByRole("menuitem", { name: new RegExp(before) })).toBeVisible();
});

test("entries: record types open as lists and dropped entries are gone from the menu", async ({ page }) => {
  await signIn(page);
  for (const [app, entry] of [["Logic", "Automations"], ["Apps", "Object views"], ["AI", "Evaluations"]]) {
    await open(page, app, entry);
    await expect(page.getByRole("heading", { level: 1, name: entry, exact: true })).toBeVisible();
  }
  const menu = page.getByRole("navigation", { name: "Primary navigation" });
  await page.getByRole("link", { name: "Home" }).click();
  await expect(menu.getByRole("link", { name: "Recent", exact: true })).toHaveCount(0);
  await page.getByRole("link", { name: "Apps" }).click();
  await expect(menu.getByRole("link", { name: "Templates", exact: true })).toHaveCount(0);
});

test("reads: each read list opens, the audit loads its next page, and a marking is created and archived", async ({ page }) => {
  await signIn(page);
  const session = await page.evaluate(() => JSON.parse(sessionStorage.getItem("platform:session")!) as { token: string; tenant: string });
  const headers = { authorization: `Bearer ${session.token}`, "platform-tenant": session.tenant };
  for (let index = 0; index < 120; index += 1) expect((await page.request.get("/v1/audit?limit=1", { headers })).ok()).toBeTruthy();

  const main = page.getByRole("main");
  for (const [app, entry] of [
    ["Home", "Proposals"], ["Operations", "Runs"], ["Operations", "Logs"], ["Operations", "Alerts"], ["Operations", "Audit"],
    ["Admin", "Teams"], ["Admin", "Business roles"], ["Admin", "Markings"], ["Admin", "Credentials"], ["Admin", "Models"],
    ["Admin", "Support grants"], ["Admin", "Language and vocabulary"], ["Data", "Lineage"], ["AI", "Knowledge"],
  ]) {
    await open(page, app, entry);
    await expect(page.getByRole("heading", { level: 1, name: entry, exact: true })).toBeVisible();
    await expect(main.getByRole("table").or(main.getByText("Nothing here yet")).first()).toBeVisible();
  }

  await page.getByRole("textbox", { name: "Search this list" }).fill("retrofit");
  await page.getByRole("textbox", { name: "Search this list" }).press("Enter");
  await expect(main.getByRole("table").or(main.getByText("No matches")).first()).toBeVisible();

  await open(page, "Operations", "Audit");
  await expect(page.getByText("100+", { exact: true })).toBeVisible();
  await page.getByRole("region", { name: "Records" }).evaluate((node) => node.scrollTo(0, node.scrollHeight));
  await expect(page.getByText("100+", { exact: true })).toHaveCount(0);
  await expect.poll(async () => Number.parseInt((await page.getByRole("group", { name: "Filter", exact: true }).getByText(/^\d+\+?$/).textContent()) ?? "", 10)).toBeGreaterThan(100);

  await open(page, "Admin", "Markings");
  await page.getByRole("button", { name: "New", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByRole("textbox", { name: "title" }).fill("Trunk marking");
  await dialog.getByRole("button", { name: "Create", exact: true }).click();
  await expect(dialog).toHaveCount(0);
  await main.getByRole("row", { name: /Trunk marking/ }).click();
  await page.getByRole("complementary").getByRole("button", { name: "Archive marking", exact: true }).click();
  await page.getByRole("alertdialog").getByRole("button", { name: "Archive marking", exact: true }).click();
  await expect(main.getByRole("row", { name: /Trunk marking.*Archived/ })).toBeVisible();
});

test("teams: create a team with two members, open it and remove it", async ({ page }) => {
  await signIn(page);
  await open(page, "Admin", "Teams");
  await page.getByRole("button", { name: "New", exact: true }).click();
  const form = page.getByRole("dialog");
  await form.getByRole("textbox", { name: /^Name/ }).fill("Trunk review team");
  const members = form.getByRole("button", { name: /^Members/ });
  await members.click();
  const options = page.getByRole("listbox");
  await options.getByRole("option", { name: "韩奥", exact: true }).click();
  await options.getByRole("option", { name: "田雪", exact: true }).click();
  await page.keyboard.press("Escape");
  await form.getByRole("button", { name: "Create", exact: true }).click();
  await expect(form).toBeHidden();

  const team = page.getByRole("row", { name: /Trunk review team/ });
  await team.click();
  const details = page.getByRole("complementary", { name: "Details" });
  await expect(details.getByRole("heading", { name: "Trunk review team" })).toBeVisible();
  await details.getByRole("button", { name: "Edit", exact: true }).click();
  await expect(members).toContainText("韩奥");
  await expect(members).toContainText("田雪");
  await form.getByRole("button", { name: "Cancel", exact: true }).click();
  await details.getByRole("button", { name: "Remove team", exact: true }).click();
  await page.getByRole("alertdialog").getByRole("button", { name: "Remove team", exact: true }).click();
  await expect(team).toHaveCount(0);
});

test("trees: the organization rolls people up its units and a trace opens to its steps", async ({ page }) => {
  await signIn(page);
  const main = page.getByRole("main");
  await open(page, "Admin", "Organization");
  await expect(main.getByRole("row").filter({ hasText: "Acme Industries" })).toContainText("3");
  const sales = main.getByRole("row").filter({ hasText: /^Sales\s*department/ });
  await expect(sales).toContainText("2");
  await expect(main.getByRole("row").filter({ hasText: "Sales North" })).toHaveCount(0);
  await sales.click();
  await page.keyboard.press("ArrowRight");
  await expect(main.getByRole("row").filter({ hasText: "Sales North" })).toContainText("1");
  await page.getByRole("radio", { name: "Sites" }).click();
  await expect(main.getByRole("row").filter({ hasText: /^Operations/ })).toContainText("1");

  await open(page, "Operations", "Traces");
  const traces = main.getByRole("row").filter({ hasText: /^GET / });
  await expect(traces.first()).toBeVisible();
  const before = await main.getByRole("row").count();
  await traces.first().click();
  await page.keyboard.press("ArrowRight");
  await expect.poll(() => main.getByRole("row").count()).toBeGreaterThan(before);
  await expect(main.getByRole("row").filter({ hasText: "Request completed" }).first()).toBeVisible();
});

test("dashboards: metrics and health show their blocks", async ({ page }) => {
  await signIn(page);
  const main = page.getByRole("main");

  await open(page, "Operations", "Metrics");
  await expect(main.getByRole("heading", { level: 1, name: "Metrics", exact: true })).toBeVisible();
  await expect(main.getByRole("heading", { name: "Requests", exact: true })).toBeVisible();

  await open(page, "Operations", "Health");
  await expect(main.getByRole("heading", { level: 1, name: "Health", exact: true })).toBeVisible();
  await expect(main.getByRole("heading", { name: "Failed work", exact: true })).toBeVisible();
  await expect(main.getByRole("heading", { name: "Readiness", exact: true })).toBeVisible();
});

test("settings: an app's setting changes in place and survives a reload", async ({ page }) => {
  await signIn(page);
  await open(page, "Admin", "Settings");
  await expect(page.getByRole("heading", { level: 1, name: "Settings", exact: true })).toBeVisible();
  await page.getByRole("main").getByRole("button", { name: "Agent Studio", exact: true }).click();
  await expect(page.getByRole("heading", { level: 2, name: "Agent Studio", exact: true })).toBeVisible();
  const days = page.getByRole("textbox", { name: "Days transcripts are kept" });
  const saved = () => page.waitForResponse((response) => response.url().endsWith("/v1/submissions"));
  await expect(days).not.toHaveValue("");
  const before = await days.inputValue();
  await days.fill("45");
  await Promise.all([saved(), days.press("Enter")]);
  await page.reload();
  await page.getByRole("main").getByRole("button", { name: "Agent Studio", exact: true }).click();
  await expect(days).toHaveValue("45");
  await days.fill(before);
  await Promise.all([saved(), days.press("Enter")]);
});

test("menu: every entry in every app opens a page with its own title", async ({ page }) => {
  await signIn(page);
  const menu = page.getByRole("navigation", { name: "Primary navigation" });
  for (const app of ["Home", "Data", "Ontology", "Logic", "Apps", "AI", "Delivery", "Operations", "Admin", "Host"]) {
    await page.getByRole("link", { name: app, exact: true }).click();
    for (const name of await menu.getByRole("link").allInnerTexts()) {
      await menu.getByRole("link", { name: name.trim(), exact: true }).click();
      await expect(page.getByRole("heading", { level: 1, name: name.trim(), exact: true })).toBeVisible();
    }
  }
});

test("crm: every CRM type can be created, edited and filtered, and lead and deal move on the kanban", async ({ page }) => {
  test.setTimeout(120000);
  await signIn(page);
  const dragCard = async (fromColumn: string, cardName: string, toColumn: string) => {
    const source = page.getByRole("region", { name: fromColumn, exact: true });
    const target = page.getByRole("region", { name: toColumn, exact: true });
    const card = source.getByRole("heading", { level: 4, name: cardName, exact: true });
    await expect(card).toBeVisible();
    const from = (await card.boundingBox())!;
    const to = (await target.boundingBox())!;
    await page.mouse.move(from.x + 2, from.y - 4);
    await page.mouse.down();
    await page.mouse.move(from.x + 40, from.y + 20, { steps: 4 });
    await page.mouse.move(to.x + to.width / 2, to.y + to.height / 2, { steps: 8 });
    await page.mouse.up();
    await expect(target.getByRole("heading", { level: 4, name: cardName, exact: true })).toBeVisible();
  };

  type Case = {
    type: string;
    plural: string;
    filterTerm: string;
    filterHit: string;
    create: (form: ReturnType<Page["getByRole"]>) => Promise<void>;
    createdTitle: string;
    editField: string;
    editValue: string;
  };
  const cases: Case[] = [
    {
      type: "crm.leadstatus", plural: "Lead statuses", filterTerm: "Nurture", filterHit: "Nurture",
      create: async (form) => { await form.getByRole("textbox", { name: "Lead status" }).fill("Dormant"); },
      createdTitle: "Dormant", editField: "Lead status", editValue: "Dormant lead",
    },
    {
      type: "crm.dealstatus", plural: "Deal statuses", filterTerm: "Negotiation", filterHit: "Negotiation",
      create: async (form) => { await form.getByRole("textbox", { name: "Deal status" }).fill("Legal Review"); },
      createdTitle: "Legal Review", editField: "Deal status", editValue: "Contract Review",
    },
    {
      type: "crm.leadsource", plural: "Lead sources", filterTerm: "展会", filterHit: "展会",
      create: async (form) => { await form.getByRole("textbox", { name: "Source name" }).fill("Partner Webinar"); },
      createdTitle: "Partner Webinar", editField: "Source name", editValue: "Partner Event",
    },
    {
      type: "crm.industry", plural: "Industries", filterTerm: "物流", filterHit: "物流",
      create: async (form) => { await form.getByRole("textbox", { name: "Industry" }).fill("Aerospace"); },
      createdTitle: "Aerospace", editField: "Industry", editValue: "Aerospace & Defense",
    },
    {
      type: "crm.territory", plural: "Territories", filterTerm: "长三角", filterHit: "长三角",
      create: async (form) => { await form.getByRole("textbox", { name: "Territory" }).fill("Nordics"); },
      createdTitle: "Nordics", editField: "Territory", editValue: "Nordic Region",
    },
    {
      type: "crm.lostreason", plural: "Lost reasons", filterTerm: "价格", filterHit: "价格",
      create: async (form) => { await form.getByRole("textbox", { name: "Lost reason" }).fill("Timeline Mismatch"); },
      createdTitle: "Timeline Mismatch", editField: "Lost reason", editValue: "Schedule Mismatch",
    },
    {
      type: "crm.organization", plural: "Organizations", filterTerm: "曦和", filterHit: "曦和精密",
      create: async (form) => { await form.getByRole("textbox", { name: "Organization" }).fill("Orion Systems"); },
      createdTitle: "Orion Systems", editField: "Website", editValue: "https://orion.example",
    },
    {
      type: "crm.crmcontact", plural: "CRM Contacts", filterTerm: "魏", filterHit: "魏清",
      create: async (form) => { await form.getByRole("textbox", { name: "Full name" }).fill("Nadia Chen"); },
      createdTitle: "Nadia Chen", editField: "Designation", editValue: "Operations Lead",
    },
    {
      type: "crm.crmproduct", plural: "Products", filterTerm: "CRM-CORE", filterHit: "CRM-CORE",
      create: async (form) => {
        await form.getByRole("textbox", { name: "Product code" }).fill("CRM-TRN");
        await form.getByRole("textbox", { name: "Product name" }).fill("Admin Training");
        await form.getByRole("spinbutton", { name: "Standard rate" }).fill("1200");
        await form.getByRole("spinbutton", { name: "Standard rate" }).press("Tab");
      },
      createdTitle: "Admin Training", editField: "Product name", editValue: "Admin Training Pack",
    },
    {
      type: "crm.lead", plural: "Leads", filterTerm: "林子", filterHit: "林子航",
      create: async (form) => {
        await form.getByRole("textbox", { name: "Full name" }).fill("Sven Lindholm");
        await form.getByRole("combobox", { name: "Status" }).click();
        await page.getByRole("option", { name: "New", exact: true }).click();
      },
      createdTitle: "Sven Lindholm", editField: "Organization", editValue: "Baltic Robotics",
    },
    {
      type: "crm.deal", plural: "Deals", filterTerm: "曦和工厂", filterHit: "曦和工厂客户关系系统上线",
      create: async (form) => {
        await form.getByRole("textbox", { name: "Deal" }).fill("Orion phase one");
        await form.getByRole("combobox", { name: "Status" }).click();
        await page.getByRole("option", { name: "Qualification", exact: true }).click();
      },
      createdTitle: "Orion phase one", editField: "Deal", editValue: "Orion phase one rollout",
    },
    {
      type: "crm.dealline", plural: "Deal product lines", filterTerm: "ERP 连接器", filterHit: "ERP 连接器模块",
      create: async (form) => {
        await form.getByRole("combobox", { name: "Deal" }).fill("曦和");
        await page.getByRole("option", { name: /曦和工厂客户关系系统上线/ }).click();
        await form.getByRole("textbox", { name: "Product name" }).fill("Custom Workshop");
        await form.getByRole("spinbutton", { name: "Quantity" }).fill("2");
        await form.getByRole("spinbutton", { name: "Rate" }).fill("500");
        await form.getByRole("spinbutton", { name: "Rate" }).press("Tab");
      },
      createdTitle: "Custom Workshop", editField: "Product name", editValue: "Custom Workshop Day",
    },
    {
      type: "crm.crmtask", plural: "Tasks", filterTerm: "北辰", filterHit: "给北辰发需求调研资料",
      create: async (form) => {
        await form.getByRole("textbox", { name: "Title" }).fill("Confirm CRM pilot dates");
        await form.getByRole("combobox", { name: "Status" }).click();
        await page.getByRole("option", { name: "Todo", exact: true }).click();
      },
      createdTitle: "Confirm CRM pilot dates", editField: "Title", editValue: "Confirm CRM pilot schedule",
    },
    {
      type: "crm.note", plural: "Notes", filterTerm: "北辰", filterHit: "北辰首次通话纪要",
      create: async (form) => { await form.getByRole("textbox", { name: "Title" }).fill("Pilot kickoff notes"); },
      createdTitle: "Pilot kickoff notes", editField: "Title", editValue: "Pilot kickoff summary",
    },
  ];

  for (const entry of cases) {
    await page.goto(`/ontology/objects/${entry.type}`);
    await expect(page.getByRole("heading", { level: 1, name: entry.plural, exact: true })).toBeVisible();
    const search = page.getByRole("textbox", { name: "Search this list" });
    await search.fill(entry.filterTerm);
    await search.press("Enter");
    await expect(page.getByRole("link", { name: entry.filterHit, exact: true }).first()).toBeVisible();
    await page.getByRole("button", { name: "New", exact: true }).click();
    const form = page.getByRole("dialog");
    await entry.create(form);
    await form.getByRole("button", { name: "Create", exact: true }).click();
    await expect(page.getByRole("heading", { level: 1, name: entry.createdTitle, exact: true })).toBeVisible();
    const field = page.getByRole("textbox", { name: entry.editField, exact: true });
    await field.fill(entry.editValue);
    await field.press("Enter");
    await expect(page.getByRole("complementary").getByText("2", { exact: true })).toBeVisible();
  }

  await page.goto("/ontology/objects/crm.deal/DEL-001");
  const lines = page.getByRole("table", { name: "Deal product lines" });
  await expect(lines).toBeVisible();
  await expect.poll(() => lines.getByRole("textbox", { name: "Product name" }).evaluateAll((inputs) => inputs.map((input) => (input as HTMLInputElement).value))).toContain("平台核心席位");
  await expect(page.getByText("19,500.00 USD", { exact: true }).first()).toBeVisible();
  const added = lines.getByRole("row").filter({ hasText: "1,000.00 USD" });
  await added.getByRole("spinbutton", { name: "Quantity" }).fill("3");
  await added.getByRole("spinbutton", { name: "Quantity" }).press("Enter");
  await expect(lines.getByText("1,500.00 USD", { exact: true })).toBeVisible();
  await page.reload();
  await expect(page.getByText("20,000.00 USD", { exact: true }).first()).toBeVisible();
  await page.goto("/ontology/objects/crm.dealline");
  const lineSearch = page.getByRole("textbox", { name: "Search this list" });
  await lineSearch.fill("Custom Workshop");
  await lineSearch.press("Enter");
  await page.getByRole("link", { name: "Custom Workshop Day", exact: true }).first().click();
  await page.getByRole("combobox", { name: "Deal", exact: true }).fill("先锋");
  await page.getByRole("option", { name: /先锋车队门户/ }).click();
  await expect(page.getByRole("link", { name: "先锋车队门户" })).toBeVisible();
  await page.goto("/ontology/objects/crm.deal/DEL-001");
  await expect(page.getByText("18,500.00 USD", { exact: true }).first()).toBeVisible();

  await page.goto("/ontology/objects/crm.note");
  await expect(page.getByRole("columnheader", { name: "Related to" })).toBeVisible();
  await expect(page.getByRole("row", { name: /曦和采购时间线/ }).getByText("曦和工厂客户关系系统上线", { exact: true })).toBeVisible();
  await page.goto("/ontology/objects/crm.crmtask/TSK-001");
  await expect(page.getByRole("radio", { name: "Lead", exact: true })).toBeChecked();
  await page.getByRole("radio", { name: "Deal", exact: true }).click();
  await page.getByRole("combobox", { name: "Deal", exact: true }).fill("曦和");
  await page.getByRole("option", { name: "曦和工厂客户关系系统上线" }).click();
  await expect(page.getByRole("link", { name: "曦和工厂客户关系系统上线" })).toBeVisible();
  await page.reload();
  await expect(page.getByRole("radio", { name: "Deal", exact: true })).toBeChecked();
  await expect(page.getByRole("link", { name: "曦和工厂客户关系系统上线" })).toBeVisible();

  await page.goto("/ontology/objects/crm.lead");
  await page.getByRole("radio", { name: "Board" }).click();
  await dragCard("New", "林子航", "Contacted");
  await page.reload();
  await expect(page.getByRole("region", { name: "Contacted", exact: true }).getByRole("heading", { level: 4, name: "林子航", exact: true })).toBeVisible();

  await page.goto("/ontology/objects/crm.deal");
  await page.getByRole("radio", { name: "Board" }).click();
  await dragCard("Qualification", "先锋车队门户", "Demo/Making");
  await page.reload();
  await expect(page.getByRole("region", { name: "Demo/Making", exact: true }).getByRole("heading", { level: 4, name: "先锋车队门户", exact: true })).toBeVisible();
});

async function crmMainline(page: Page, go: (type: string, plural: string) => Promise<unknown>, person: string, company: string) {
  await go("crm.lead", "Leads");
  await page.getByRole("button", { name: "New", exact: true }).click();
  const lead = page.getByRole("dialog");
  await lead.getByRole("textbox", { name: "Full name" }).fill(person);
  await lead.getByRole("combobox", { name: "Status" }).click();
  await page.getByRole("option", { name: "New", exact: true }).click();
  await lead.getByRole("button", { name: "Create", exact: true }).click();
  await expect(page.getByRole("heading", { level: 1, name: person, exact: true })).toBeVisible();
  const leadPage = page.url();
  const organization = page.getByRole("textbox", { name: "Organization", exact: true });
  await organization.fill(company);
  await organization.press("Enter");
  await expect(page.getByRole("complementary").getByText("2", { exact: true })).toBeVisible();
  const revenue = page.getByRole("spinbutton", { name: "Annual revenue", exact: true });
  await revenue.fill("250000");
  await revenue.press("Enter");
  await expect(page.getByRole("complementary").getByText("3", { exact: true })).toBeVisible();
  await page.reload();
  await expect(page.getByRole("spinbutton", { name: "Annual revenue", exact: true })).toHaveValue(/250,000\.00/);

  for (const [type, fill] of [
    ["crm.note", async (form: ReturnType<Page["getByRole"]>) => { await form.getByRole("textbox", { name: "Title" }).fill(`${company} discovery call`); }],
    ["crm.crmtask", async (form: ReturnType<Page["getByRole"]>) => {
      await form.getByRole("textbox", { name: "Title" }).fill(`Send ${company} the pilot plan`);
      await form.getByRole("combobox", { name: "Status" }).click();
      await page.getByRole("option", { name: "Todo", exact: true }).click();
    }],
  ] as const) {
    await go(type, type === "crm.note" ? "Notes" : "Tasks");
    await page.getByRole("button", { name: "New", exact: true }).click();
    const form = page.getByRole("dialog");
    await fill(form);
    await form.getByRole("button", { name: "Create", exact: true }).click();
    await expect(page.getByRole("dialog")).toBeHidden();
    await page.getByRole("combobox", { name: "Lead", exact: true }).fill(person.split(" ")[0]);
    await page.getByRole("option", { name: new RegExp(person) }).click();
    await expect(page.getByRole("complementary").getByText("2", { exact: true })).toBeVisible();
  }

  await page.goto(leadPage);
  await page.getByRole("button", { name: "More actions" }).click();
  await page.getByRole("menuitem", { name: "Convert to deal", exact: true }).click();
  const convert = page.getByRole("dialog", { name: "Convert to deal", exact: true });
  await expect(convert.getByText("Leave empty to create a new organization from the lead.", { exact: true })).toBeVisible();
  await convert.getByRole("combobox", { name: "Link to organization" }).click();
  await expect(page.getByRole("option").first()).toBeVisible();
  await convert.getByRole("heading", { name: "Convert to deal", exact: true }).click();
  await expect(page.getByRole("option")).toHaveCount(0);
  await convert.getByRole("button", { name: "Convert to deal", exact: true }).click();
  await expect(convert).toBeHidden();
  await expect(page).toHaveURL(/\/(crm\.)?deal\/[^/]+$/);
  await expect(page.getByRole("heading", { level: 1, name: company, exact: true })).toBeVisible();
  const dealPage = new URL(page.url()).pathname;
  const organizations = dealPage.replace(/(crm\.)?deal\/[^/]+$/, "$1organization/");
  await page.getByRole("main").getByRole("link", { name: company, exact: true }).last().click();
  await expect(page).toHaveURL((url) => url.pathname.startsWith(organizations));
  await page.getByRole("main").getByRole("link", { name: company, exact: true }).first().click();
  await expect(page).toHaveURL((url) => url.pathname === dealPage);
  await page.goto(leadPage);
  await expect(page.getByRole("link", { name: company, exact: true }).first()).toBeVisible();
  await page.getByRole("button", { name: "More actions" }).click();
  await expect(page.getByRole("menuitem", { name: "Convert to deal", exact: true })).toBeDisabled();
  await page.keyboard.press("Escape");

  await go("crm.deal", "Deals");
  await page.getByRole("radio", { name: "Board" }).click();
  const card = page.getByRole("region", { name: "Qualification", exact: true }).getByRole("heading", { level: 4, name: company, exact: true });
  await expect(card).toBeVisible();
  const won = page.getByRole("region", { name: "Won", exact: true });
  const from = (await card.boundingBox())!;
  const to = (await won.boundingBox())!;
  await page.mouse.move(from.x + 2, from.y - 4);
  await page.mouse.down();
  await page.mouse.move(from.x + 40, from.y + 20, { steps: 4 });
  await page.mouse.move(to.x + to.width / 2, to.y + to.height / 2, { steps: 8 });
  await page.mouse.up();
  await page.reload();
  const wonCard = page.getByRole("region", { name: "Won", exact: true }).getByRole("heading", { level: 4, name: company, exact: true });
  await expect(wonCard).toBeVisible();
  const lost = page.getByRole("region", { name: "Lost", exact: true });
  await lost.scrollIntoViewIfNeeded();
  const start = (await wonCard.boundingBox())!;
  const end = (await lost.boundingBox())!;
  await page.mouse.move(start.x + 2, start.y - 4);
  await page.mouse.down();
  await page.mouse.move(start.x + 40, start.y + 20, { steps: 4 });
  await page.mouse.move(end.x + end.width / 2, end.y + end.height / 2, { steps: 8 });
  await page.mouse.up();
  const closing = page.getByRole("dialog", { name: "Edit deal", exact: true });
  await expect(closing.getByRole("combobox")).toHaveCount(1);
  await closing.getByRole("combobox", { name: "Lost reason" }).click();
  await page.getByRole("option", { name: "价格", exact: true }).click();
  await closing.getByRole("button", { name: "Edit deal", exact: true }).click();
  await expect(closing).toBeHidden();
  await expect(lost.getByRole("heading", { level: 4, name: company, exact: true })).toBeVisible();

  await page.goto(dealPage);
  await expect(page.getByRole("spinbutton", { name: "Probability %" })).toHaveValue("0");
  await page.getByRole("combobox", { name: "Status" }).click();
  await page.getByRole("option", { name: "Negotiation", exact: true }).click();
  await page.reload();
  await expect(page.getByRole("spinbutton", { name: "Probability %" })).toHaveValue("70");
  await expect(page.getByRole("combobox", { name: "Lost reason" })).toHaveCount(0);
  await page.getByRole("combobox", { name: "Status" }).click();
  await page.getByRole("option", { name: "Lost", exact: true }).click();
  await expect(page.getByText("Also fill in: Lost reason").first()).toBeVisible();
  await page.getByRole("combobox", { name: "Lost reason" }).click();
  await page.getByRole("option", { name: "竞争对手", exact: true }).click();
  await expect(page.getByText("Also fill in: Lost reason")).toHaveCount(0);
  await page.reload();
  await expect(page.getByRole("spinbutton", { name: "Probability %" })).toHaveValue("0");
  await page.getByRole("combobox", { name: "Status" }).click();
  await page.getByRole("option", { name: "Won", exact: true }).click();
  await page.reload();
  await expect(page.getByRole("spinbutton", { name: "Probability %" })).toHaveValue("100");

  await go("crm.organization", "Organizations");
  const search = page.getByRole("textbox", { name: "Search this list" });
  await search.fill(company.split(" ")[0]);
  await search.press("Enter");
  await page.getByRole("link", { name: company, exact: true }).first().click();
  await expect(page.getByRole("heading", { level: 1, name: company, exact: true })).toBeVisible();
  await page.getByRole("link", { name: "Links", exact: true }).click();
  await expect(page.getByText(person, { exact: true }).first()).toBeVisible();
  await expect(page.getByRole("link", { name: company, exact: true }).first()).toBeVisible();
}

test("crm: a lead with a note and a task converts into an organization, contact and deal, and the deal is won", async ({ page }) => {
  test.setTimeout(120000);
  await signIn(page);
  await crmMainline(page, (type) => page.goto(`/ontology/objects/${type}`), "Mira Holm", "Fjord Analytics");
});

test("crm: the released package installs into an empty tenant whose CRM app runs the same mainline", async ({ page }) => {
  test.setTimeout(120000);
  await page.goto("/");
  await page.getByRole("button", { name: "admin-1 · crm" }).click();
  await page.getByRole("link", { name: "CRM", exact: true }).click();
  const navigation = page.getByRole("navigation", { name: "Primary navigation" });
  for (const entry of ["Leads", "Deals", "Organizations", "CRM Contacts", "Tasks", "Notes"]) await expect(navigation.getByRole("link", { name: entry, exact: true })).toBeVisible();
  await crmMainline(page, async (_, plural) => {
    await navigation.getByRole("link", { name: plural, exact: true }).click();
    await expect(page.getByRole("heading", { level: 1, name: plural, exact: true })).toBeVisible();
  }, "Tove Berg", "Saga Logistics");
});

test("crm: a sales rep works only their own leads while the sales manager sees the whole team's, whose activity reads in names", async ({ page }) => {
  const leads = async (seat: string) => {
    await page.goto("/");
    await page.getByRole("button", { name: `${seat} · acceptance` }).click();
    await expect(page.getByRole("button", { name: "Account", exact: true })).toBeVisible();
    await page.goto("/ontology/objects/crm.lead");
    await expect(page.getByText("沈若溪", { exact: true })).toBeVisible();
  };
  await leads("user-1");
  await expect(page.getByText("林子航", { exact: true })).toHaveCount(0);
  await page.getByRole("button", { name: "Account", exact: true }).click();
  await page.getByRole("menuitem", { name: "Sign out", exact: true }).click();
  await leads("manager-1");
  await expect(page.getByText("林子航", { exact: true })).toBeVisible();

  await page.getByText("沈若溪", { exact: true }).click();
  await page.getByRole("link", { name: "Activity", exact: true }).click();
  const main = page.getByRole("main");
  for (const line of ["Source: 邮件", "Industry: 物流", "Territory: 华东大区", "Lead owner: 田雪"]) await expect(main.getByText(line, { exact: true })).toBeVisible();
  await expect(main.getByRole("img", { name: "王丹", exact: true }).first()).toBeVisible();
  await expect(main.getByText(/SRC-|IND-|TER-|admin-1|Unnamed member/)).toHaveCount(0);
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
  await page.getByRole("listitem").filter({ hasText: "crm · 0.1.0" }).getByRole("button", { name: "Review and install" }).click();
  const review = page.getByRole("dialog");
  const name = review.getByRole("textbox", { name: "Project API name" });
  await expect(name).toHaveValue("crm");
  await review.getByRole("button", { name: "Install package", exact: true }).click();
  await expect(review.getByText("A project named crm already exists; install under another name")).toBeVisible();
  await name.fill("pipeline");
  await review.getByRole("button", { name: "Install package", exact: true }).click();
  await expect(page).toHaveURL(/\/delivery\/installations$/);
  await expect(page.getByRole("listitem").filter({ hasText: "crm · 0.1.0" })).toContainText("pipeline");
  await page.locator('a[href="/pipeline/pipeline.lead"]').click();
  await expect(page).toHaveURL(/\/pipeline\/pipeline\.lead$/);
  await expect(page.getByRole("heading", { level: 1, name: "Leads", exact: true })).toBeVisible();
  await expect(page.getByRole("heading", { level: 2, name: "Nothing here yet", exact: true })).toBeVisible();
  await page.goto("/ontology/objects/crm.lead");
  await expect(page.getByText("林子航", { exact: true })).toBeVisible();
});
