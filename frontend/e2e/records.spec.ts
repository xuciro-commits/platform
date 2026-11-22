import { type Page, expect, test } from "@playwright/test";
import { signIn, open, property, metadata, edit } from "./support";

test.describe.serial("opportunities and records", () => {
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
  await expect(property(page, "Stage")).toContainText("Negotiation");
  await expect(page.getByRole("heading", { level: 1, name: "冲压线改造" })).toBeVisible();

  await expect(page.getByRole("heading", { level: 3, name: "Commercial" })).toBeVisible();
  await expect(page.getByRole("heading", { level: 3, name: "Follow-up" })).toBeVisible();
  await expect(page.getByRole("textbox", { name: "Loss reason" })).toHaveCount(0);
  await expect(page.getByRole("switch", { name: "Budget approved" })).toBeChecked();
  await expect(property(page, "Proposed deposit")).toContainText("1,250.00");
  await expect(property(page, "Owner")).toContainText("王丹");

  await expect(property(page, "Next step")).toHaveText("发送修订后的条款");
  await edit(page, "Next step");
  const next = page.getByRole("textbox", { name: "Next step" });
  await next.fill("Call the plant manager");
  await next.press("Enter");
  await expect(page.getByRole("complementary").getByText("2", { exact: true })).toBeVisible();

  await page.reload();
  await expect(property(page, "Next step")).toHaveText("Call the plant manager");

  await edit(page, "Owner");
  await page.getByRole("combobox", { name: "Owner", exact: true }).fill("韩奥");
  await page.getByRole("option", { name: /韩奥/ }).click();
  await expect(page.getByRole("complementary").getByText("3", { exact: true })).toBeVisible();
  await page.reload();
  await expect(property(page, "Owner")).toContainText("韩奥");
});


test("cells: a list cell edits in place with the input its type asks for", async ({ page }) => {
  await signIn(page);
  await open(page, "Ontology", "Objects");
  await page.getByRole("link", { name: "Opportunities" }).click();
  const row = page.getByRole("row", { name: /会员数据集成/ });
  await row.getByText("Lost", { exact: true }).click();
  await expect(page.getByRole("dialog", { name: "Details" })).toBeVisible();
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
  const reason = property(page, "Loss reason").or(page.getByRole("textbox", { name: "Loss reason" }));
  await expect(reason).toHaveCount(0);

  await edit(page, "Stage");
  await page.getByRole("option", { name: "Lost", exact: true }).click();
  await expect(reason.first()).toBeVisible();
  await expect(property(page, "Stage")).toHaveText("Lost");

  await edit(page, "Stage");
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
  await edit(page, "Next step");
  const next = page.getByRole("textbox", { name: "Next step" });
  await next.fill("Book the site visit");
  await next.press("Enter");
  await expect(page.getByRole("complementary").getByText("2", { exact: true })).toBeVisible();
  expect(dropped).toBe(true);

  await page.unroute("**/v1/submissions");
  await page.reload();
  await expect(property(page, "Next step")).toHaveText("Book the site visit");
  await expect(page.getByRole("complementary").getByText("2", { exact: true })).toBeVisible();
});


test("actions: archive from the record menu, then undo from the notice", async ({ page }) => {
  await signIn(page);
  await open(page, "Ontology", "Objects");
  await page.getByRole("link", { name: "Opportunities" }).click();
  await page.getByRole("link", { name: "标签打印线" }).click();

  await page.getByRole("button", { name: "More actions" }).click();
  await page.getByRole("menuitem", { name: /^Archive/ }).click();
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
    await expect(property(page, "Next step")).toHaveText("商定试点门店");
  }
  const [first, second] = pages;

  await edit(first, "Next step");
  await first.getByRole("textbox", { name: "Next step" }).fill("Pilot in four stores");
  await first.getByRole("textbox", { name: "Next step" }).press("Enter");
  await expect(first.getByRole("complementary").getByText("2", { exact: true })).toBeVisible();

  await edit(second, "Owner");
  await second.getByRole("combobox", { name: "Owner", exact: true }).fill("韩奥");
  await second.getByRole("option", { name: /韩奥/ }).click();
  await expect(second.getByRole("complementary").getByText("3", { exact: true })).toBeVisible();
  await expect(second.getByRole("dialog")).toHaveCount(0);

  await edit(second, "Next step");
  await second.getByRole("textbox", { name: "Next step" }).fill("Pilot in two stores");
  await edit(first, "Next step");
  await first.getByRole("textbox", { name: "Next step" }).fill("Pilot in six stores");
  await first.getByRole("textbox", { name: "Next step" }).press("Enter");
  await expect(first.getByRole("complementary").getByText("4", { exact: true })).toBeVisible();
  await expect(second.getByText("This record has new changes", { exact: true })).toBeVisible();
  await expect(second.getByRole("textbox", { name: "Next step" })).toHaveValue("Pilot in two stores");
  await second.getByRole("textbox", { name: "Next step" }).press("Enter");
  const choice = second.getByRole("dialog");
  await expect(choice).toContainText("Pilot in six stores");
  await expect(choice).toContainText("Pilot in two stores");
  await choice.getByRole("button", { name: "Use mine" }).click();
  await expect(second.getByRole("complementary").getByText("5", { exact: true })).toBeVisible();

  await expect(property(first, "Next step")).toHaveText("Pilot in two stores");
  await Promise.all(pages.map((page) => page.close()));
});


test("leave: a typed value is kept when the user walks away without Enter", async ({ page }) => {
  await signIn(page);
  await open(page, "Ontology", "Objects");
  await page.getByRole("link", { name: "Opportunities" }).click();
  await page.getByRole("link", { name: "熔炉监测" }).click();
  await edit(page, "Next step");
  await page.getByRole("textbox", { name: "Next step" }).fill("Book the site walk");
  await page.getByRole("link", { name: "Opportunities" }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Opportunities" })).toBeVisible();
  await page.reload();
  await page.getByRole("link", { name: "熔炉监测" }).click();
  await expect(property(page, "Next step")).toHaveText("Book the site walk");
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
  const earlier = page.getByRole("button", { name: /earlier changes$/ });
  if (await earlier.isVisible()) await earlier.click();
  await expect(page.getByRole("listitem", { name: "Subject: 冲压线改造" })).toBeVisible();
  await visit("Ontology", "Objects", "Customers", "北风工具");
  await visit("Ontology", "Object types", null, "Opportunity");
});


test("batch: retries a stale row without submitting a successful row again", async ({ page, context }) => {
  await page.route("**/v1/changes", (route) => route.abort());
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
  await edit(editing, "Next step");
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
  await edit(page, "Subject");
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
  await expect(property(page, "Subject")).toHaveText("车队追踪上线");
});


test("reference: search by name, save the selected record and follow its link", async ({ page }) => {
  await signIn(page);
  await open(page, "Ontology", "Objects");
  await page.getByRole("link", { name: "Opportunities" }).click();
  await page.getByRole("link", { name: "冲压线改造" }).click();
  await edit(page, "Customer");
  await page.getByRole("combobox", { name: "Customer", exact: true }).fill("哈特");
  await page.getByRole("option", { name: /哈特曼/ }).click();
  await expect(page.getByRole("link", { name: "哈特曼制造", exact: true })).toBeVisible();
  await page.reload();
  const customer = page.getByRole("link", { name: "哈特曼制造", exact: true });
  await expect(customer).toBeVisible();
  await customer.hover();
  await expect(page.getByRole("heading", { name: "哈特曼制造", exact: true })).toBeVisible();
  await customer.click();
  const peek = page.getByRole("dialog", { name: "哈特曼制造" });
  await expect(peek.getByRole("heading", { level: 2, name: "哈特曼制造", exact: true })).toBeVisible();
  await peek.getByRole("link", { name: "Open full page", exact: true }).click();
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
  await expect(property(table, "Product")).toHaveText("Press 200t");
  await page.reload();
  await expect(property(table, "Quantity")).toHaveText("2");
  await expect(table.getByRole("row", { name: /Totals/ })).toContainText("1,000");
  await edit(table, "Quantity");
  const quantity = table.getByRole("spinbutton", { name: "Quantity" });
  await quantity.fill("3");
  await quantity.press("Enter");
  await expect(table.getByRole("row", { name: /Totals/ })).toContainText("3");
  await page.reload();
  await expect(property(table, "Quantity")).toHaveText("3");
  await table.getByRole("button", { name: "Archive row" }).click();
  await expect(property(table, "Product")).toHaveCount(0);
});


test("activity: a comment and an inline change appear in one timeline", async ({ page }) => {
  await signIn(page);
  await open(page, "Ontology", "Objects");
  await page.getByRole("link", { name: "Opportunities" }).click();
  await page.getByRole("link", { name: "仓库扫码枪" }).click();
  await edit(page, "Next step");
  const next = page.getByRole("textbox", { name: "Next step" });
  const before = await next.inputValue();
  await next.fill("Confirm scanner quantities");
  await next.press("Enter");
  await page.getByRole("link", { name: "Activity", exact: true }).click();
  await page.getByRole("textbox", { name: "Write a comment" }).fill("I asked the warehouse lead.");
  await page.getByRole("button", { name: "Post comment" }).click();
  const comment = page.getByText("I asked the warehouse lead.", { exact: true });
  const change = page.getByRole("listitem", { name: `Next step: ${before} → Confirm scanner quantities`, exact: true });
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
  await page.getByRole("button", { name: "Preview", exact: true }).click();
  await expect(page.getByRole("dialog", { name: "schedule.txt" }).getByText("Lead time: 6 weeks")).toBeVisible();
  await page.keyboard.press("Escape");
  const pdf = "%PDF-1.4\n1 0 obj<</Type/Catalog/Pages 2 0 R>>endobj\n2 0 obj<</Type/Pages/Kids[3 0 R]/Count 1>>endobj\n3 0 obj<</Type/Page/Parent 2 0 R/MediaBox[0 0 200 200]>>endobj\ntrailer<</Root 1 0 R>>\n%%EOF\n";
  await page.locator('input[type="file"]').setInputFiles({ name: "brief.pdf", mimeType: "application/pdf", buffer: Buffer.from(pdf) });
  await page.getByRole("listitem").filter({ hasText: "brief.pdf" }).getByRole("button", { name: "Preview", exact: true }).click();
  await expect(page.getByRole("dialog", { name: "brief.pdf" }).getByRole("document", { name: "brief.pdf" }).locator("canvas")).toHaveCount(1);
  await page.keyboard.press("Escape");
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
  await tabTo(page.getByRole("button", { name: "Edit Next step", exact: true }));
  await page.keyboard.press("Enter");
  await expect(page.getByRole("textbox", { name: "Next step" })).toBeFocused();
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
  await expect(property(page, "Next step")).toHaveText("Confirm renewal scope");
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
  await page.getByRole("button", { name: /^Show empty fields/ }).click();
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
  const peek = page.getByRole("dialog", { name });
  await expect(peek.getByRole("heading", { level: 2, name, exact: true })).toBeVisible();
  await peek.getByRole("link", { name: "Open full page", exact: true }).click();
  await expect(page.getByRole("heading", { level: 1, name, exact: true })).toBeVisible();
});


test("record fields: values read as values and become inputs when focused", async ({ page }) => {
  await signIn(page);
  await page.goto("/ontology/objects/sales.opportunity/OPP-001");
  await expect(property(page, "Amount")).toHaveText("184,000");
  await edit(page, "Amount");
  const amount = page.getByRole("spinbutton", { name: "Amount", exact: true });
  await expect(amount).toBeFocused();
  await expect(amount).toHaveValue("184000");
  await amount.press("Escape");
  await expect(property(page, "Amount")).toHaveText("184,000");
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
  await page.getByRole("button", { name: /^Show empty fields/ }).click();
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
  await expect(ownerCell.getByText("王丹", { exact: true })).toBeVisible();

  await row.getByRole("link", { name: "熔炉监测" }).click();
  const properties = page.getByRole("complementary");
  await expect(property(page, "Owner")).toContainText("王丹");
  await edit(page, "Owner");
  await page.getByRole("combobox", { name: "Owner", exact: true }).fill("韩奥");
  await page.getByRole("option", { name: /韩奥/ }).click();
  await expect(properties.getByText("2", { exact: true })).toBeVisible();
  await page.reload();
  await expect(property(page, "Owner")).toContainText("韩奥");
});


test("record fields: an attachment preserves the horizontal label alignment", async ({ page }) => {
  await signIn(page);
  await page.goto("/ontology/objects/sales.opportunity/OPP-001");
  await page.getByRole("button", { name: /^Show empty fields/ }).click();
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
  await edit(page, "Expected close");
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
  const details = page.getByRole("dialog", { name: "Details" });
  await expect(details.getByRole("heading", { level: 2, name: "机器人工作站集成" })).toBeVisible();
  await expect(details.getByText("红隼机器人").first()).toBeVisible();
  await expect(page).toHaveURL(/sales\.opportunity\?/);
  await page.keyboard.press("Escape");
  await expect(details).toBeHidden();

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
  await expect(page).toHaveURL(/\/ontology\/object-types\/sales\.opportunity$/);
  await expect(page.getByRole("heading", { name: "Opportunity", exact: true })).toBeVisible();

  await page.keyboard.press("ControlOrMeta+K");
  await search.fill("Inbox");
  await expect(palette.getByRole("option", { name: "Inbox", exact: true })).toBeVisible();
  await search.press("Enter");
  await expect(page).toHaveURL(/\/home\/inbox$/);
  await expect(page.getByRole("heading", { name: "Inbox", exact: true })).toBeVisible();
});

});
