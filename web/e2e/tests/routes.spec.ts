// docs/Testing.md's routes 4, 5, 17 and 18, walked in a browser.
import { readFileSync } from "node:fs";
import { expect, test, type Page } from "@playwright/test";
import { decide, fresh, open } from "./host";

/** A field's value on the record page, not the status bar's steps or the history. */
const value = (page: Page, text: string) => page.getByRole("definition").filter({ hasText: new RegExp(`^${text}$`, "i") });

// Route 17 (#118): a record page offers the type's declared actions, asks a
// transition's payload in a form, and follows the change without a reload.
test("route 17: every action has an entry, pages follow changes", async ({ page, request }) => {
  const account = fresh("ACC"), opp = fresh("OPP");
  await decide(request, "sales", "crm", "crm.account.create", { type: "crm.account", id: account }, { name: "Acme " + account, kind: "company" });
  await decide(request, "sales", "crm", "crm.opportunity.open", { type: "crm.opportunity", id: opp }, { account, title: "Board offsite" });
  await open(page, "sales", `/record?type=crm.opportunity&id=${opp}`);
  for (const action of ["Close opportunity", "Plan group stay", "Book stay"]) {
    await expect(page.getByRole("button", { name: action })).toBeVisible();
  }
  await page.getByRole("button", { name: "Close opportunity" }).click();
  await page.getByRole("dialog").getByRole("combobox").first().selectOption("won");
  await page.getByRole("dialog").getByRole("button", { name: "Close opportunity" }).click();
  await expect(value(page, "won")).toBeVisible();
});

// Route 5 (ADR-0026): a group's rooms are held at the hotel until a cutoff; won, they are confirmed.
test("route 5: a group block is held, then confirmed", async ({ page, request }) => {
  const account = fresh("ACC"), opp = fresh("OPP");
  await decide(request, "sales", "crm", "crm.account.create", { type: "crm.account", id: account }, { name: "Group " + account, kind: "company" });
  await decide(request, "sales", "crm", "crm.opportunity.open", { type: "crm.opportunity", id: opp }, { account, title: "Retreat" });
  const day = (d: number) => new Date(Date.now() + d * 86_400_000).toISOString().slice(0, 10);
  await decide(request, "sales", "crm", "crm.opportunity.plan", { type: "crm.opportunity", id: opp },
    { rooms: 1, roomType: "standard", arrive: day(200 + Math.floor(Math.random() * 100)), depart: day(310), cutoff: day(150) });
  await open(page, "sales", `/record?type=crm.opportunity&id=${opp}`);
  await expect(value(page, "held")).toBeVisible();
  // The hotel's reservation is on the opportunity's page as a linked record, the platform's, not the CRM's (#129).
  await expect(page.getByText(/\(1, linked\)/)).toBeVisible();
  await decide(request, "sales", "crm", "crm.opportunity.close", { type: "crm.opportunity", id: opp }, { outcome: "won" });
  await expect(value(page, "confirmed")).toBeVisible(); // the page follows without a reload
});

// Route 4 (#118, F-38): a leave request waits as pending, is approved by the
// manager, and the requester's open page follows; a rejection shows its reason.
test("route 4: an approval reaches the requester's page", async ({ page, request }, testInfo) => {
  const leave = fresh("LEA");
  const day = (d: number) => new Date(Date.now() + d * 86_400_000).toISOString().slice(0, 10);
  await open(page, "sales", "/definitions");
  await page.getByRole("row").filter({ hasText: "hcm/page/leaves" }).click();
  await page.getByRole("button", { name: "Open page" }).click();
  await page.getByRole("button", { name: /Draft leave request/ }).click();
  const draft = page.getByRole("dialog");
  await draft.getByRole("textbox").first().fill(leave);
  await draft.getByRole("combobox").first().selectOption("vacation");
  await draft.locator('input[type="date"]').first().fill(day(30));
  await draft.locator('input[type="date"]').last().fill(day(32));
  await draft.getByRole("button", { name: "Create" }).click();
  await page.getByRole("textbox", { name: "Search" }).fill(leave);
  const leaveRow = page.getByRole("row").filter({ hasText: leave });
  await expect(leaveRow).toBeVisible();
  await leaveRow.click();
  await page.getByRole("button", { name: "Submit for approval" }).click();
  await expect(value(page, "Pending approval")).toBeVisible();
  await expect(page.getByText(/^waiting for Manager:/)).toBeVisible();
  const chain = page.getByRole("figure", { name: "Approvals" }); // the chain drawn (#122)
  await expect(chain.getByText("Manager", { exact: true })).toBeVisible();
  await expect(chain.getByText("sales-1", { exact: true })).toBeVisible();
  await page.screenshot({ path: testInfo.outputPath("hcm-page-pending-desktop.png"), fullPage: true });
  const requests = await (await request.get("/v1/requests", { headers: { Authorization: "Bearer sales" } })).json() as { id: string; target: string }[];
  const approval = requests.find((r) => r.target === `hcm.leave/${leave}`)?.id;
  expect(approval).toBeTruthy();
  const managerPage = await page.context().newPage();
  await open(managerPage, "manager", "/inbox");
  const task = managerPage.getByRole("listitem").filter({ hasText: leave });
  await expect(task).toBeVisible();
  await task.getByRole("button", { name: "Approve", exact: true }).click();
  await expect(value(page, "Approved")).toBeVisible();
  await page.screenshot({ path: testInfo.outputPath("hcm-page-approved-desktop.png"), fullPage: true });
  await managerPage.close();
  const opened = await request.get(`/v1/records/work.approval/${encodeURIComponent(approval!)}`, { headers: { Authorization: "Bearer sales" } });
  expect(opened.status()).toBe(200);

  // Rejected with a reason (F-38): the leave says so and may be submitted again.
  const second = fresh("LEA");
  await decide(request, "sales", "hcm", "hcm.leave.create", { type: "hcm.leave", id: second }, { kind: "vacation", from: day(40), until: day(41) });
  const asked = await decide(request, "sales", "hcm", "hcm.leave.submit", { type: "hcm.leave", id: second }, {});
  await open(page, "sales", `/record?type=hcm.leave&id=${second}`);
  await decide(request, "manager", "work", "work.approval.reject", { type: "work.approval", id: `hcm.${asked.key}` }, { note: "busy week" });
  await expect(value(page, "Rejected")).toBeVisible();
  await expect(page.getByText("rejected by manager-1: busy week")).toBeVisible();
  await decide(request, "sales", "hcm", "hcm.leave.submit", { type: "hcm.leave", id: second }, {});
  await expect(value(page, "Pending approval")).toBeVisible();
});

// Route 24 (#122): a flow's definition opens as a graph in Settings.
test("route 24: graphs", async ({ page }) => {
  await open(page, "manager", "/flows");
  await page.getByRole("cell", { name: "csm.service-level" }).click();
  const graph = page.getByRole("figure", { name: "Steps" });
  await expect(graph.getByText("Until it is due")).toBeVisible();
  await expect(graph.getByText("as it decides").first()).toBeVisible();
});

// Route 25 (ADR-0029 D4): every agent in one overview; suspending one shows it and resuming undoes it.
test("route 25: the agents overview and its switch", async ({ page }) => {
  await open(page, "manager", "/agents");
  const row = page.getByRole("row").filter({ hasText: "agent:csm.triage" });
  await expect(row).toBeVisible();
  await row.getByRole("button", { name: "Suspend" }).click();
  await expect(row.getByText("Suspended")).toBeVisible();
  await row.getByRole("button", { name: "Resume" }).click();
  await expect(row.getByText("Working")).toBeVisible();
});

// Route 26 (ADR-0029 D6): an evaluation of the agent's declared cases starts from Settings and reports.
test("route 26: an evaluation suite", async ({ page, request }) => {
  const provider = fresh("lm").toLowerCase();
  await decide(request, "manager", "ai", "ai.provider.add", { type: "ai.provider", id: provider }, { kind: "local", baseUrl: "http://127.0.0.1:9/v1" });
  await decide(request, "manager", "ai", "ai.model.enable", { type: "ai.model", id: `${provider}/candidate` }, { access: "everyone" });
  await open(page, "manager", "/evaluations");
  await page.getByLabel("Agent", { exact: true }).selectOption("csm.triage");
  await page.getByLabel("Candidate model").selectOption(`${provider}/candidate`); // picked from the enabled models, never typed
  await page.getByRole("checkbox", { name: "Declared cases" }).check();
  await page.getByRole("button", { name: "Evaluate" }).click();
  await expect(page.getByRole("cell", { name: `${provider}/candidate` }).first()).toBeVisible();
});

// Route 27 (ADR-0032 13a): code and the read-only workspace catalog use the
// same qualified object/action references and the caller's existing grants.
test("route 27: installed definitions", async ({ page, request }) => {
  const response = await request.get("/v1/definitions", { headers: { Authorization: "Bearer sales" } });
  expect(response.status()).toBe(200);
  const definitions = await response.json() as { ref: { app: string; kind: string; name: string }; requires: { app: string; kind: string; name: string }[] }[];
  const crm = (kind: string, name: string) => definitions.find((d) => d.ref.app === "crm" && d.ref.kind === kind && d.ref.name === name);
  expect(crm("object", "crm.opportunity")).toBeTruthy();
  expect(crm("action", "crm.opportunity.open")?.requires).toContainEqual({ app: "crm", kind: "object", name: "crm.opportunity" });
  expect(definitions.some((d) => d.ref.app === "erp")).toBe(false);

  await open(page, "sales", "/definitions");
  await expect(page.getByRole("heading", { name: "Definitions" })).toBeVisible();
  await page.getByRole("cell", { name: "crm/action/crm.opportunity.open", exact: true }).click();
  await expect(page.getByText("crm/action/crm.opportunity.open")).toBeVisible();
  await expect(page.getByRole("columnheader", { name: "Field" })).toBeVisible();
});

// Route 28 (ADR-0032 13b): one installed page descriptor drives the live
// list/detail task and a local preview whose action form cannot submit.
test("route 28: page descriptor, preview and keyboard task", async ({ page, request }, testInfo) => {
  const account = fresh("ACC"), opp = fresh("OPP");
  await decide(request, "sales", "crm", "crm.account.create", { type: "crm.account", id: account }, { name: "Page test " + account, kind: "company" });
  await decide(request, "sales", "crm", "crm.opportunity.open", { type: "crm.opportunity", id: opp }, { account, title: "Page task " + opp });
  await open(page, "sales", "/definitions");
  await page.getByRole("row").filter({ hasText: "crm/page/opportunities" }).click();
  await page.getByRole("button", { name: "Open page" }).click();
  await expect(page.getByRole("heading", { name: "Opportunities" })).toBeVisible();
  const search = page.getByRole("textbox", { name: "Search" });
  await search.fill(opp);
  const row = page.getByRole("row").filter({ hasText: opp });
  await expect(row).toBeVisible();
  await row.focus();
  await page.keyboard.press("Enter");
  await expect(page.getByRole("region", { name: "Selected record" }).getByText("Page task " + opp).first()).toBeVisible();
  await expect(page.getByRole("button", { name: "Plan group stay" })).toBeVisible();
  await page.screenshot({ path: testInfo.outputPath("page-desktop.png"), fullPage: true });

  await page.setViewportSize({ width: 390, height: 780 });
  await expect(page.getByRole("button", { name: "Back to list" })).toBeVisible();
  await expect.poll(async () => (await page.getByRole("region", { name: "Selected record" }).boundingBox())?.width ?? 0).toBeGreaterThan(300);
  await expect.poll(async () => page.evaluate(() => document.documentElement.scrollWidth)).toBeLessThanOrEqual(390);
  await page.screenshot({ path: testInfo.outputPath("page-mobile.png"), fullPage: true });
  await expect(page.getByRole("navigation", { name: "Main" })).toHaveCount(0);
  await page.getByRole("button", { name: "Toggle navigation" }).click();
  await expect(page.getByRole("navigation", { name: "Main" })).toBeVisible();
  await page.getByRole("button", { name: "Close navigation" }).click({ position: { x: 380, y: 400 } });
  await expect(page.getByRole("navigation", { name: "Main" })).toHaveCount(0);
  await page.getByRole("button", { name: "Back to list" }).click();
  await expect(page.getByRole("region", { name: "Records in this page" })).toBeVisible();

  await open(page, "sales", "/definitions");
  await page.getByRole("row").filter({ hasText: "crm/page/opportunities" }).click();
  await page.getByRole("button", { name: "Preview page" }).click();
  await expect(page.getByText("Preview uses sample data. Actions do not run.")).toBeVisible();
  await expect(page.getByRole("region", { name: "Selected record" }).getByText("SAMPLE-001")).toBeVisible();
  await page.screenshot({ path: testInfo.outputPath("page-preview-mobile.png"), fullPage: true });
  await page.getByRole("button", { name: "Back to list" }).click();
  await expect(page.getByRole("row").filter({ hasText: "SAMPLE-001" })).toBeVisible();
  const writes: string[] = [];
  page.on("request", (r) => { if (r.method() !== "GET" && r.url().includes("/v1/")) writes.push(r.url()); });
  await page.getByRole("button", { name: "Plan group stay" }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog.getByText("Preview only")).toBeVisible();
  await expect(dialog.getByRole("button", { name: "Plan group stay" })).toBeDisabled();
  expect(writes).toEqual([]);

  const chinese = await page.context().newPage();
  await chinese.addInitScript(() => localStorage.setItem("platform.language", "zh-CN"));
  await chinese.setViewportSize({ width: 390, height: 780 });
  await open(chinese, "sales", "/definitions");
  await expect(chinese.getByRole("heading", { name: "定义" })).toBeVisible();
  await chinese.getByRole("row").filter({ hasText: "crm/page/opportunities" }).click();
  await chinese.getByRole("button", { name: "打开页面" }).click();
  await expect(chinese.getByRole("heading", { name: "商机" })).toBeVisible();
  await chinese.getByRole("textbox", { name: "搜索" }).fill(opp);
  const chineseRow = chinese.getByRole("row").filter({ hasText: opp });
  await expect(chineseRow).toBeVisible();
  await chineseRow.click();
  await expect(chinese.getByRole("button", { name: "返回列表" })).toBeVisible();
  await expect(chinese.getByRole("heading", { name: "Page task " + opp })).toBeVisible();
  await chinese.screenshot({ path: testInfo.outputPath("page-chinese-mobile.png"), fullPage: true });
  await chinese.close();
});

// Route 18 (ADR-0027): the process answers /healthz; Settings → Automation shows the tenant's health.
test("route 18: health", async ({ page, request }) => {
  expect((await (await request.get("/healthz")).json()).status).toBe("ok");
  await open(page, "manager", "/automation");
  await expect(page.getByText(/^(Healthy|Needs attention)$/).first()).toBeVisible();
});

// Route 19 (ADR-0028): a file added on a record's page is listed there and
// downloads for whoever may read the record.
test("route 19: a file on a ticket", async ({ page, request }) => {
  const ticket = fresh("T");
  await decide(request, "desk", "csm", "csm.ticket.open", { type: "csm.ticket", id: ticket }, { subject: "Broken lamp", customer: "anna@acme.test" });
  await open(page, "desk", `/record?type=csm.ticket&id=${ticket}`);
  await page.locator('input[type="file"]').setInputFiles({ name: "lamp.txt", mimeType: "text/plain", buffer: Buffer.from("the lamp flickers") });
  await expect(page.getByRole("button", { name: "lamp.txt" })).toBeVisible();
  const view = await (await request.get(`/v1/records/csm.ticket/${ticket}`, { headers: { Authorization: "Bearer desk" } })).json();
  const file = view.files[0];
  const bytes = await request.get(`/v1/files/${file.id}`, { headers: { Authorization: "Bearer desk" } });
  expect(await bytes.text()).toBe("the lamp flickers");
  expect((await request.get(`/v1/files/${file.id}`, { headers: { Authorization: "Bearer sales-only" } })).status()).toBe(404);
});

// Route 20 (ADR-0028): a field only some roles read — the opportunity's
// expected margin is shown to the sales manager, never to the salesperson.
test("route 20: field security", async ({ page, request }) => {
  const account = fresh("ACC"), opp = fresh("OPP");
  await decide(request, "sales", "crm", "crm.account.create", { type: "crm.account", id: account }, { name: "Margin " + account, kind: "company" });
  await decide(request, "sales", "crm", "crm.opportunity.open", { type: "crm.opportunity", id: opp }, { account, title: "Retreat" });
  await decide(request, "manager", "crm", "crm.opportunity.edit", { type: "crm.opportunity", id: opp }, { margin: 31.5 });
  await open(page, "manager", `/record?type=crm.opportunity&id=${opp}`);
  await expect(page.getByRole("term").filter({ hasText: "Expected margin" })).toBeVisible();
  await expect(value(page, "31.50")).toBeVisible();
  const salesPage = await page.context().newPage(); // another member: a page of its own
  await open(salesPage, "sales", `/record?type=crm.opportunity&id=${opp}`);
  await expect(salesPage.getByText("Retreat").first()).toBeVisible();
  await expect(salesPage.getByText("Expected margin")).toHaveCount(0);
});

// Route 21 (ADR-0028 D5, D6): a form offers a record picker and a list of
// choices; a comment on a record tells whom it mentions.
test("route 21: pickers, choices and comments", async ({ page, request }) => {
  const account = fresh("ACC"), opp = fresh("OPP");
  await decide(request, "sales", "crm", "crm.account.create", { type: "crm.account", id: account }, { name: "Picker " + account, kind: "company" });
  await open(page, "sales", "/opportunities");
  await page.getByRole("button", { name: /Open opportunity/ }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByRole("textbox").first().fill(opp);
  await dialog.getByRole("combobox").first().fill(account);
  await dialog.getByRole("option", { name: `${account} · Picker ${account}` }).click();
  await dialog.getByRole("textbox").last().fill("Retreat");
  await dialog.getByRole("button", { name: "Open opportunity" }).click();
  await open(page, "sales", `/record?type=crm.opportunity&id=${opp}`);
  await page.getByRole("textbox", { name: "Comment" }).fill("@manager-1 can you look at the price?");
  await page.getByRole("button", { name: "Comment", exact: true }).click();
  await expect(page.getByText("@manager-1 can you look at the price?")).toBeVisible();
  await expect(page.getByRole("button", { name: "Unfollow" })).toBeVisible();
  const told = await (await request.get("/v1/notifications", { headers: { Authorization: "Bearer manager" } })).json();
  expect(told.some((n: { title: string }) => n.title.startsWith("sales-1 mentioned you"))).toBe(true);
});

// Route 22 (ADR-0028 11e): accounts imported from CSV after a preview, and exported.
test("route 23: import and export", async ({ page }) => {
  const a = fresh("IMP"), b = fresh("IMP");
  await open(page, "sales", "/accounts");
  await page.locator('input[type="file"]').setInputFiles({ name: "accounts.csv", mimeType: "text/csv",
    buffer: Buffer.from(`id,name,kind\n${a},Imported one,company\n${b},Imported two,person\nBAD,Nobody,alien\n`) });
  const dialog = page.getByRole("dialog");
  await expect(dialog.getByText("ERROR_CODE_INVALID_ARGUMENT")).toBeVisible();
  await dialog.getByRole("button", { name: "Import", exact: true }).click();
  await expect(dialog.getByRole("heading", { name: "Imported" })).toBeVisible();
  await dialog.getByRole("button", { name: "Close", exact: true }).last().click();
  await expect(page.getByText("Imported one")).toBeVisible();
  const download = page.waitForEvent("download");
  await page.getByRole("button", { name: "Export CSV" }).click();
  const file = await (await download).path();
  expect(readFileSync(file, "utf8")).toContain(`${a},Imported one,company`);
});

// Route 29 (ADR-0034): the hotel's manager defines an object the platform never
// heard of, publishes it, and it is at once an ordinary one — its page, its
// generated form, its records — with no restart and no code.
test("route 29: define an object, publish it, use it", async ({ page }, testInfo) => {
  const name = `visit${Date.now().toString(36).slice(-5)}`;
  await open(page, "manager", "/home");
  await page.getByRole("button", { name: "Builder" }).first().click(); // the app, from the launcher
  await expect(page.getByRole("heading", { name: "Objects" })).toBeVisible();
  await page.getByRole("button", { name: "Create object" }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByRole("textbox", { name: "Name" }).first().fill(name);
  await dialog.getByRole("textbox", { name: "What people call it" }).fill("Visit");
  await dialog.getByRole("textbox", { name: "What people call several" }).fill("Visits");
  await dialog.getByRole("button", { name: "Add line" }).click();
  const row = dialog.getByRole("row").last();
  await row.getByRole("textbox").first().fill("guest");
  await row.getByRole("textbox").nth(1).fill("Guest");
  await row.getByRole("combobox").selectOption("text");
  await row.getByRole("checkbox").last().check(); // searchable, so it names a record
  await dialog.getByRole("button", { name: "Create" }).click();
  await expect(page.getByRole("row").filter({ hasText: name })).toBeVisible();

  await page.getByRole("row").filter({ hasText: name }).click();
  await page.getByRole("button", { name: "Publish", exact: true }).click();
  await expect(page.getByRole("definition").filter({ hasText: `build.${name}` })).toBeVisible();

  // The object someone just defined is now in the navigation and has its own page.
  await page.getByRole("button", { name: "Toggle navigation" }).click({ trial: true }).catch(() => undefined);
  await page.getByRole("button", { name: "Visits" }).click();
  await expect(page.getByRole("heading", { name: "Visits" })).toBeVisible();
  await page.getByRole("button", { name: "Create Visit" }).click();
  const create = page.getByRole("dialog");
  await create.getByRole("textbox", { name: "Guest" }).fill("Ada Lovelace");
  await create.getByRole("button", { name: "Create" }).click();
  await expect(page.getByRole("row").filter({ hasText: "Ada Lovelace" })).toBeVisible();
  await page.screenshot({ path: testInfo.outputPath("defined-object.png"), fullPage: true });
});

// Route 30 (ADR-0034, ADR-0035): someone composes a page over an object the
// platform already has — the CRM's opportunity — laying out a table, a detail
// and the CRM's own action, and publishes it. It then opens real records, and
// selecting one fills the widgets that read the selection.
test("route 30: compose a page of widgets and use it", async ({ page, request }, testInfo) => {
  const account = fresh("ACC"), opp = fresh("OPP"), name = `offsites${Date.now().toString(36).slice(-5)}`;
  await decide(request, "sales", "crm", "crm.account.create", { type: "crm.account", id: account }, { name: "Composed " + account, kind: "company" });
  await decide(request, "sales", "crm", "crm.opportunity.open", { type: "crm.opportunity", id: opp }, { account, title: "Composed offsite " + opp });
  await open(page, "manager", "/home");
  await page.getByRole("button", { name: "Builder" }).first().click();
  await page.getByRole("button", { name: "Pages", exact: true }).click();
  await page.getByRole("button", { name: "Create page" }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByRole("textbox", { name: "Name" }).first().fill(name);
  await dialog.getByRole("textbox", { name: "What people call it" }).fill("Group offsites");
  await dialog.getByRole("textbox", { name: "Object it shows" }).fill("crm.opportunity");
  await dialog.getByRole("button", { name: "Create" }).click();

  // The composer: a layout panel, a canvas over real records, a widget panel.
  await page.getByRole("row").filter({ hasText: name }).click();
  await expect(page.getByText("Actions do not run while you compose.")).toBeVisible();
  // Nothing laid out yet: publishing is not offered, and the composer says why.
  await expect(page.getByRole("button", { name: "Publish" })).toBeDisabled();
  await expect(page.getByText("Add at least one widget before publishing.")).toBeVisible();
  await page.getByRole("button", { name: "Table", exact: true }).click();
  await page.getByRole("group", { name: "Fields it shows" }).getByRole("button", { name: "Stage" }).click();
  // Three panes, side by side: what there is to place, the page, the widget in
  // hand. (They once collapsed into one column when the package's styles were
  // not scanned; the owner saw it before any test did.)
  const pane = async (name: string) => (await page.getByRole("region", { name }).boundingBox())!;
  const [palette, canvas, inspector] = await Promise.all([pane("Widgets and layout"), pane("The page"), pane("The widget in hand")]);
  expect(palette.x + palette.width).toBeLessThanOrEqual(canvas.x + 1);
  expect(canvas.x + canvas.width).toBeLessThanOrEqual(inspector.x + 1);
  await page.getByRole("button", { name: "Detail", exact: true }).click();
  await page.getByRole("button", { name: "Actions", exact: true }).click();
  await page.getByRole("group", { name: "Actions it offers" }).getByRole("button", { name: "Close opportunity" }).click();
  // Clicking a widget on the canvas takes it in hand.
  await page.locator("div").filter({ hasText: /^Detail/ }).last().click();
  await expect(page.getByRole("group", { name: "Fields it shows" })).toBeVisible();
  await page.screenshot({ path: testInfo.outputPath("composer.png"), fullPage: true });
  await page.getByRole("button", { name: "Publish" }).click();
  await expect(page.getByText("The page is in the workspace.")).toBeVisible(); // what the host answered
  await expect(page.getByText("Published", { exact: true })).toBeVisible(); // and the composer shows where the page stands

  // What was composed is what people open: the table fills the detail beside it.
  await page.getByRole("button", { name: "Group offsites" }).click();
  await expect(page.getByRole("heading", { name: "Group offsites" })).toBeVisible();
  await page.getByRole("textbox", { name: "Search" }).first().fill(opp);
  await page.getByRole("row").filter({ hasText: opp }).click();
  await expect(page.getByText("Composed offsite " + opp).first()).toBeVisible();
  await expect(page.getByRole("button", { name: "Close opportunity" })).toBeVisible(); // the CRM's own action, on a page someone composed
  await page.screenshot({ path: testInfo.outputPath("composed-page.png"), fullPage: true });
});
