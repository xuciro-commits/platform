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
  await graph.getByRole("button", { name: "Expand canvas" }).click();
  await expect.poll(async () => (await graph.boundingBox())?.width ?? 0).toBeGreaterThan(900);
  await graph.getByRole("button", { name: "Restore canvas" }).click();
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
  expect(definitions.every((d) => Array.isArray(d.requires))).toBe(true);

  await open(page, "sales", "/definitions");
  await expect(page.getByRole("heading", { name: "Definitions" })).toBeVisible();
  await page.getByRole("cell", { name: "crm/action/crm.opportunity.open", exact: true }).click();
  await expect(page.getByText("crm/action/crm.opportunity.open")).toBeVisible();
  await expect(page.getByRole("columnheader", { name: "Field" })).toBeVisible();
  await open(page, "manager", "/definitions");
  await expect(page.getByRole("heading", { name: "Definitions" })).toBeVisible();
  await expect(page.getByRole("columnheader", { name: "Depends on" })).toBeVisible();
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
  await expect(page.getByText(/^Host started /)).toBeVisible();
});

test("route 18: quarantined tenant is explicit", async ({ page }) => {
  await page.route("**/v1/health", async (route) => {
    await route.fulfill({ json: {
      status: "quarantined", recoveryError: "entry 4: invalid accepted result", started: "2026-09-28T14:00:00Z",
      apps: 4, queues: [], failed: 0, deferred: [], breakers: [], openBreakers: 0,
      connectorsFailing: 0, endpointsFailing: 0,
    } });
  });
  await open(page, "manager", "/automation");
  await expect(page.getByText("Quarantined", { exact: true })).toBeVisible();
  const notice = page.getByRole("region", { name: "Health" }).getByRole("alert");
  await expect(notice).toContainText("Inputs and work are stopped");
  await expect(notice).toContainText("entry 4: invalid accepted result");
});

test("route 18: operator retries in-place recovery after repairing the journal", async ({ page }) => {
  let repaired = false;
  let attempts = 0;
  await page.route("**/v1/me", async (route) => {
    if (!repaired) await route.fulfill({ status: 503, json: { error: { code: "TENANT_QUARANTINED" } } });
    else await route.continue();
  });
  await page.route("**/v1/health", async (route) => {
    await route.fulfill({ json: {
      status: "quarantined", recoveryError: "entry 4: digest mismatch", started: "2026-09-28T14:00:00Z",
      apps: 4, queues: [], failed: 0, deferred: [], breakers: [], openBreakers: 0,
      connectorsFailing: 0, endpointsFailing: 0,
    } });
  });
  await page.route("**/v1/recovery/retry", async (route) => {
    attempts++;
    if (attempts === 1) await route.fulfill({ status: 409, json: { error: "entry 4: digest mismatch" } });
    else { repaired = true; await route.fulfill({ json: { status: "ok" } }); }
  });
  await open(page, "manager", "/automation");
  await expect(page.getByRole("heading", { name: "Tenant recovery" })).toBeVisible();
  await expect(page.getByRole("alert")).toContainText("entry 4: digest mismatch");
  await page.getByRole("button", { name: "Retry recovery" }).click();
  await expect(page.getByRole("alert").last()).toContainText("entry 4: digest mismatch");
  await page.getByRole("button", { name: "Retry recovery" }).click();
  await expect(page.getByRole("heading", { name: "Tenant recovery" })).toHaveCount(0);
  await expect(page.getByText("Automation", { exact: true }).first()).toBeVisible();
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
  await page.getByRole("button", { name: "Application Studio" }).first().click(); // the app, from the launcher
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
  await page.getByRole("button", { name: "Application Studio" }).first().click();
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
  await page.setViewportSize({ width: 390, height: 780 });
  const [mobileLayout, mobilePage, mobileProperties] = await Promise.all([pane("Widgets and layout"), pane("The page"), pane("The widget in hand")]);
  expect(mobileLayout.y + mobileLayout.height).toBeLessThanOrEqual(mobilePage.y + 1);
  expect(mobilePage.y + mobilePage.height).toBeLessThanOrEqual(mobileProperties.y + 1);
  expect(mobilePage.height).toBeGreaterThan(200);
  expect(mobileProperties.height).toBeGreaterThan(150);
  await page.screenshot({ path: testInfo.outputPath("mobile-composer.png"), fullPage: true });
  await page.setViewportSize({ width: 1280, height: 720 });
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

// The integrated editor's object picker and the runtime's reference filter
// must agree: a related table follows the selected master, not every record.
test("route 30: a composed related table follows the selected account", async ({ page, request }) => {
  const first = fresh("ACC"), second = fresh("ACC");
  const firstOpp = fresh("OPP"), secondOpp = fresh("OPP");
  const name = `related${Date.now().toString(36).slice(-5)}`, id = fresh("P");
  await decide(request, "sales", "crm", "crm.account.create", { type: "crm.account", id: first }, { name: "First " + first, kind: "company" });
  await decide(request, "sales", "crm", "crm.account.create", { type: "crm.account", id: second }, { name: "Second " + second, kind: "company" });
  await decide(request, "sales", "crm", "crm.opportunity.open", { type: "crm.opportunity", id: firstOpp }, { account: first, title: "First deal " + firstOpp });
  await decide(request, "sales", "crm", "crm.opportunity.open", { type: "crm.opportunity", id: secondOpp }, { account: second, title: "Second deal " + secondOpp });
  await decide(request, "manager", "build", "build.page.create", { type: "build.page", id },
    { name, title: "Related deals", object: "crm.account", list: ["name"], detail: ["name"] });
  await open(page, "manager", `/compose?id=${id}`);
  await page.getByRole("button", { name: "Table", exact: true }).click();
  await page.getByRole("button", { name: "Table", exact: true }).click();
  const inspector = page.getByRole("region", { name: "The widget in hand" });
  await inspector.getByRole("combobox", { name: "Object" }).selectOption("crm.opportunity");
  await inspector.getByRole("textbox", { name: "Title" }).fill("Opportunities for account");
  await inspector.getByRole("group", { name: "Fields it shows" }).getByRole("button", { name: "Title" }).click();
  await page.getByRole("button", { name: "Publish" }).click();
  await expect(page.getByText("The page is in the workspace.")).toBeVisible();

  await open(page, "manager", `/page?app=build&kind=page&name=${name}`);
  await expect(page.getByText("Select a record to see related opportunities.")).toBeVisible();
  await page.getByRole("row").filter({ hasText: "First " + first }).click();
  await expect(page.getByRole("row").filter({ hasText: "First deal " + firstOpp })).toBeVisible();
  await expect(page.getByRole("row").filter({ hasText: "Second deal " + secondOpp })).toHaveCount(0);
  await page.getByRole("row").filter({ hasText: "Second " + second }).click();
  await expect(page.getByRole("row").filter({ hasText: "Second deal " + secondOpp })).toBeVisible();
  await expect(page.getByRole("row").filter({ hasText: "First deal " + firstOpp })).toHaveCount(0);
});

// Route 37 (ADR-0039 20a): the builder reviews the saved draft's semantic
// closure and failure reason without installing it or disclosing it to others.
test("route 37: review a saved draft and its dependencies", async ({ page, request }, testInfo) => {
  const id = fresh("OBJ"), name = `review${Date.now().toString(36).slice(-5)}`;
  await decide(request, "manager", "build", "build.object.create", { type: "build.object", id },
    { name, title: "Review visit", fields: [{ name: "guest", title: "Guest", type: "text" }] });
  const noAccess = await request.post("/v1/releases/preview", {
    headers: { Authorization: "Bearer desk" }, data: { kind: "object", id },
  });
  expect(noAccess.status()).toBe(403);
  expect(await noAccess.text()).toBe("");
  await open(page, "manager", "/home");
  await page.getByRole("button", { name: "Application Studio" }).first().click();
  await page.getByRole("button", { name: "Release review" }).click();
  await page.getByRole("combobox", { name: "Saved draft" }).selectOption(id);
  await page.getByRole("button", { name: "Check draft and dependencies" }).focus();
  await page.keyboard.press("Enter");
  await expect(page.getByText("Candidate ready for review")).toBeVisible();
  await expect(page.getByText(`build/page/${name}`)).toBeVisible();
  await expect(page.getByText(`build/object/build.${name}`)).toBeVisible();
  await expect(page.getByText("Installed candidate:", { exact: false })).toHaveCount(0);
  await page.screenshot({ path: testInfo.outputPath("release-review.png"), fullPage: true });
  await page.setViewportSize({ width: 390, height: 780 });
  await expect(page.getByText(`build/page/${name}`)).toBeVisible();
  await page.screenshot({ path: testInfo.outputPath("release-review-narrow.png"), fullPage: true });
  await page.setViewportSize({ width: 1280, height: 720 });
  // Preview reads only: publication is a separate accepted action.
  await decide(request, "manager", "build", "build.object.publish", { type: "build.object", id }, {});
  await page.getByRole("button", { name: "Check draft and dependencies" }).click();
  await expect(page.getByText("Installed candidate:", { exact: false })).toBeVisible();
  await expect(page.getByText("Changed · 0")).toBeVisible();

  const bad = fresh("PAGE");
  await decide(request, "manager", "build", "build.page.create", { type: "build.page", id: bad },
    { name: `bad${Date.now().toString(36).slice(-5)}`, title: "Unbound", object: "crm.opportunity" });
  await page.getByRole("combobox", { name: "Definition kind" }).selectOption("page");
  await page.getByRole("combobox", { name: "Saved draft" }).selectOption(bad);
  await page.getByRole("button", { name: "Check draft and dependencies" }).click();
  await expect(page.getByText("Candidate rejected")).toBeVisible();
  await expect(page.getByRole("alert").filter({ hasText: "a page needs fields" })).toBeVisible();
});

// Route 31 (ADR-0036): what someone builds is handed to the people it was
// built for — a name and an icon in their launcher, holding the page composed
// in route 30, and nobody gains access they did not already have.
test("route 31: hand an application to the people who use it", async ({ page, request }, testInfo) => {
  const account = fresh("ACC"), opp = fresh("OPP");
  const name = `desk${Date.now().toString(36).slice(-5)}`, pageName = `handed${Date.now().toString(36).slice(-5)}`;
  await decide(request, "sales", "crm", "crm.account.create", { type: "crm.account", id: account }, { name: "Handed " + account, kind: "company" });
  await decide(request, "sales", "crm", "crm.opportunity.open", { type: "crm.opportunity", id: opp }, { account, title: "Handed offsite " + opp });
  // A page to hand over, composed as in route 30.
  await open(page, "manager", "/home");
  await page.getByRole("button", { name: "Application Studio" }).first().click();
  await page.getByRole("button", { name: "Pages", exact: true }).click();
  await page.getByRole("button", { name: "Create page" }).click();
  let dialog = page.getByRole("dialog");
  await dialog.getByRole("textbox", { name: "Name" }).first().fill(pageName);
  await dialog.getByRole("textbox", { name: "What people call it" }).fill("Handed offsites");
  await dialog.getByRole("textbox", { name: "Object it shows" }).fill("crm.opportunity");
  await dialog.getByRole("button", { name: "Create" }).click();
  await page.getByRole("row").filter({ hasText: pageName }).click();
  await page.getByRole("button", { name: "Table", exact: true }).click();
  await page.getByRole("button", { name: "Publish" }).click();
  await expect(page.getByText("The page is in the workspace.")).toBeVisible();

  // The application: a name, an icon, the page it holds.
  await page.getByRole("button", { name: "Applications", exact: true }).click();
  await page.getByRole("button", { name: "Create application" }).click();
  dialog = page.getByRole("dialog");
  await dialog.getByRole("textbox", { name: "Name" }).first().fill(name);
  await dialog.getByRole("textbox", { name: "What people call it" }).fill("Front desk");
  await dialog.getByRole("combobox", { name: "Icon" }).selectOption("clipboard");
  await dialog.getByLabel("Pages", { exact: true }).fill(pageName);
  await dialog.getByLabel("Pages", { exact: true }).press("Enter");
  await dialog.getByRole("button", { name: "Create" }).click();
  await page.getByRole("row").filter({ hasText: name }).click();
  await expect(page.getByRole("definition").filter({ hasText: pageName })).toBeVisible(); // the page it holds
  await page.getByRole("button", { name: "Hand it over" }).click();
  await expect(page.getByRole("definition").filter({ hasText: "Published" })).toBeVisible(); // the host took it

  // The registry offers it, with the page it holds.
  const offered = await (await request.get("/v1/definitions", { headers: { Authorization: "Bearer manager" } })).json() as { ref: { kind: string; name: string }; application?: { title: string; pages: string[] } }[];
  const application = offered.find((d) => d.ref.kind === "app" && d.ref.name === name);
  expect(application?.application, `the registry's applications: ${JSON.stringify(offered.filter((d) => d.ref.kind === "app"))}`).toBeTruthy();
  expect(application!.application!.pages).toContain(pageName);

  // It is in the launcher, and its page opens from its own navigation.
  await page.getByRole("navigation", { name: "Main" }).getByRole("button", { name: "Apps", exact: true }).click();
  await expect(page.getByRole("heading", { name: /Welcome/ })).toBeVisible(); // the launcher itself
  await expect(page.getByRole("button", { name: "Front desk" }).first()).toBeVisible();
  await page.getByRole("button", { name: "Front desk" }).first().click();
  await expect(page.getByRole("heading", { name: "Handed offsites" })).toBeVisible();
  await page.screenshot({ path: testInfo.outputPath("handed-application.png"), fullPage: true });
});

// Route 32 (ADR-0036 17b): a page renamed in the editor's page settings, an
// application whose navigation has a heading, and what a member sees of it —
// only the pages theirs to open, and a plain answer at a page that is not.
test("route 32: an application's headings, a page's settings, a page not yours", async ({ page, request }) => {
  const stamp = Date.now().toString(36).slice(-5);
  const tickets = `tickets${stamp}`, offsites = `offsites${stamp}`, app = `service${stamp}`;
  const make = async (id: string, name: string, title: string, object: string, field: string) => {
    await decide(request, "manager", "build", "build.page.create", { type: "build.page", id }, { name, title, object, list: [field], detail: [field] });
    await decide(request, "manager", "build", "build.page.publish", { type: "build.page", id }, {});
  };
  await make(fresh("P"), tickets, "Tickets " + stamp, "csm.ticket", "subject");
  const offsitesID = fresh("P");
  await make(offsitesID, offsites, "Offsites " + stamp, "crm.opportunity", "title");

  // The page's own settings, beside its widgets: what people call it.
  await open(page, "manager", `/compose?id=${offsitesID}`);
  await page.getByRole("button", { name: "Page settings", exact: true }).click();
  await page.getByRole("textbox", { name: "What people call it" }).fill("Renamed deals " + stamp);
  await page.getByRole("button", { name: "Table", exact: true }).click();
  await page.getByRole("button", { name: "Publish" }).click();
  await expect(page.getByText("The page is in the workspace.")).toBeVisible();

  const id = fresh("A");
  await decide(request, "manager", "build", "build.app.create", { type: "build.app", id },
    { name: app, title: "Service " + stamp, icon: "people", pages: [tickets, offsites], groups: [{ title: "Sales", pages: [offsites] }] });
  await decide(request, "manager", "build", "build.app.publish", { type: "build.app", id }, {});

  // The builder sees both pages: the ungrouped one under the application's
  // name, the other under its heading, with the title set in the editor.
  await open(page, "manager", "/home");
  await page.getByRole("button", { name: "Service " + stamp }).first().click();
  const nav = page.getByRole("navigation", { name: "Main" });
  await expect(nav.getByText("Sales", { exact: true })).toBeVisible();
  await expect(nav.getByRole("button", { name: "Renamed deals " + stamp })).toBeVisible();
  await expect(nav.getByRole("button", { name: "Tickets " + stamp })).toBeVisible();

  // The front desk reads tickets, not the CRM: the heading over only offsites is not theirs.
  const desk = await page.context().newPage();
  await open(desk, "desk", "/home");
  await desk.getByRole("button", { name: "Service " + stamp }).first().click();
  const deskNav = desk.getByRole("navigation", { name: "Main" });
  await expect(deskNav.getByRole("button", { name: "Tickets " + stamp })).toBeVisible();
  await expect(deskNav.getByRole("button", { name: "Renamed deals " + stamp })).toHaveCount(0);
  await expect(deskNav.getByText("Sales", { exact: true })).toHaveCount(0);
  // A link to the page that is not theirs answers plainly, without saying what it holds.
  await desk.goto(`/#/page?app=build&kind=page&name=${offsites}`);
  await expect(desk.getByText("This page is not open to you.")).toBeVisible();
  await desk.getByRole("button", { name: "Back to your apps" }).click();
  await expect(desk.getByRole("heading", { name: /Welcome/ })).toBeVisible();
  await desk.close();
});

// Route 33 (ADR-0035 16b): a filter the table and metric read, a form that
// makes a record through the object's own create action, and the selected
// record's timeline and tasks — composed over the CRM's accounts and used.
test("route 33: filter, form, timeline and tasks on a composed page", async ({ page, request }, testInfo) => {
  const stamp = Date.now().toString(36).slice(-5), name = `accounts${stamp}`, id = fresh("P");
  await decide(request, "manager", "build", "build.page.create", { type: "build.page", id },
    { name, title: "Accounts " + stamp, object: "crm.account", list: ["name"], detail: ["name"] });
  await open(page, "manager", `/compose?id=${id}`);
  for (const widget of ["Filter", "Table", "Form", "Timeline", "Tasks"]) await page.getByRole("button", { name: widget, exact: true }).click();
  // The form started with what an account needs; the filter with its kind.
  await page.getByRole("region", { name: "Widgets and layout" }).getByRole("button", { name: "Form 1", exact: true }).click(); // the section, in the layout
  await expect(page.getByRole("group", { name: "Fields it asks for" }).getByRole("button", { name: "Name *" })).toHaveAttribute("aria-pressed", "true");
  await page.getByRole("button", { name: "Publish" }).click();
  await expect(page.getByText("The page is in the workspace.")).toBeVisible();

  // Used: a person makes an account from the form, narrows the table to people, selects it.
  await open(page, "manager", `/page?app=build&kind=page&name=${name}`);
  await expect(page.getByRole("heading", { name: "Accounts " + stamp })).toBeVisible();
  const person = "Filtered person " + stamp;
  await page.getByRole("textbox", { name: "Name" }).fill(person);
  await page.getByRole("combobox", { name: "Kind" }).last().selectOption("person");
  await page.getByRole("button", { name: "Create", exact: true }).click();
  await page.getByRole("search").getByRole("combobox", { name: "Kind" }).selectOption("company");
  await page.getByRole("textbox", { name: "Search" }).first().fill(person);
  await expect(page.getByRole("row").filter({ hasText: person })).toHaveCount(0); // a person is not a company
  await page.getByRole("search").getByRole("combobox", { name: "Kind" }).selectOption("person");
  await page.getByRole("row").filter({ hasText: person }).click();
  await expect(page.getByRole("region", { name: "History" }).getByText("crm.account.create")).toBeVisible();
  await expect(page.getByText("Nothing waits on it.")).toBeVisible();
  await page.screenshot({ path: testInfo.outputPath("page-16b.png"), fullPage: true });
});

// Route 34 (ADR-0037 18a): a builder gives an object states and an action with
// an input, a condition and a field it sets, in the object editor; a person
// takes the action on a record and reads the builder's words when it may not.
test("route 34: states and actions a tenant defines", async ({ page, request }, testInfo) => {
  const stamp = Date.now().toString(36).slice(-5), name = `lost${stamp}`, id = fresh("O");
  await decide(request, "manager", "build", "build.object.create", { type: "build.object", id }, {
    name, title: "Lost item", plural: "Lost items " + stamp,
    fields: [{ name: "item", title: "Item", type: "text", required: true, search: true }, { name: "value", title: "Value", type: "integer" },
      { name: "claimant", title: "Handed to", type: "text" }],
  });
  await open(page, "manager", `/process?id=${id}`);
  await expect(page.getByRole("button", { name: "Process and access" })).toBeVisible();
  const outline = page.getByRole("region", { name: "States and actions" });
  const inHand = page.getByRole("region", { name: "The piece in hand" });
  // Two states: found, then returned.
  await outline.getByRole("button", { name: "Add a state" }).click();
  await inHand.getByRole("textbox", { name: "What people call it" }).fill("Found");
  await inHand.getByRole("textbox", { name: "Name" }).fill("found");
  await outline.getByRole("button", { name: "Add a state" }).click();
  await inHand.getByRole("textbox", { name: "What people call it" }).fill("Returned");
  await inHand.getByRole("textbox", { name: "Name" }).fill("returned");
  // An action from found to returned, asking who took it, refused for valuables.
  await outline.getByRole("button", { name: "Add an action" }).click();
  await inHand.getByRole("textbox", { name: "What people call it" }).fill("Hand it back");
  await inHand.getByRole("textbox", { name: "Name" }).fill("handback");
  await expect(inHand.getByRole("combobox", { name: "Leaves it in" })).toHaveValue("returned");
  await inHand.getByRole("button", { name: "Inputs and rules" }).click();
  await inHand.getByRole("button", { name: "Add an input" }).click();
  await inHand.getByRole("textbox", { name: "Label" }).fill("Handed to");
  await inHand.getByRole("textbox", { name: "Name" }).last().fill("to");
  await inHand.getByRole("checkbox", { name: "Required" }).check();
  await inHand.getByRole("button", { name: "Set a field" }).click();
  await inHand.getByRole("combobox", { name: "Field" }).first().selectOption("claimant");
  await inHand.getByRole("combobox", { name: "From" }).selectOption("to");
  await inHand.getByRole("button", { name: "Add a condition" }).click();
  await inHand.getByRole("combobox", { name: "Field" }).last().selectOption("value");
  await inHand.getByRole("combobox", { name: "Operator" }).selectOption("<");
  await inHand.getByRole("textbox", { name: "Value" }).fill("500");
  await inHand.getByRole("textbox", { name: "Message when it does not hold" }).fill("Valuables go back through the manager.");
  // The visual edge edits the same declaration as the inspector.
  await inHand.getByRole("group", { name: "Taken from" }).getByRole("button", { name: "Found" }).click();
  await expect(inHand.getByRole("group", { name: "Taken from" }).getByRole("button", { name: "Found" })).toHaveAttribute("aria-pressed", "false");
  const graph = page.getByRole("region", { name: "Process map" });
  await graph.getByRole("button", { name: "Expand canvas" }).click();
  await page.waitForTimeout(250); // let the shared viewport fit after its container is measured
  const hit = async (selector: string, handle: string) => {
    const box = await graph.locator(selector).boundingBox();
    if (!box) return false;
    return page.evaluate(({ x, y, handle }) => document.elementFromPoint(x, y)?.closest(".react-flow__handle")?.getAttribute("data-handleid") === handle,
      { x: box.x + box.width / 2, y: box.y + box.height / 2, handle });
  };
  await expect.poll(() => hit('.react-flow__handle[data-nodeid="state:found"][data-handleid="take"]', "take")).toBe(true);
  await expect.poll(() => hit('.react-flow__handle[data-nodeid="action:handback"][data-handleid="from"]', "from")).toBe(true);
  const sourcePort = await graph.locator('.react-flow__handle[data-nodeid="state:found"][data-handleid="take"]').boundingBox();
  const targetPort = await graph.locator('.react-flow__handle[data-nodeid="action:handback"][data-handleid="from"]').boundingBox();
  if (!sourcePort || !targetPort) throw new Error("The lifecycle ports were not visible");
  await page.mouse.move(sourcePort.x + sourcePort.width / 2, sourcePort.y + sourcePort.height / 2);
  await page.mouse.down();
  await page.mouse.move(targetPort.x + targetPort.width / 2, targetPort.y + targetPort.height / 2, { steps: 8 });
  await page.mouse.up();
  await expect(inHand.getByRole("group", { name: "Taken from" }).getByRole("button", { name: "Found" })).toHaveAttribute("aria-pressed", "true");
  await page.mouse.move(sourcePort.x + sourcePort.width / 2, sourcePort.y + sourcePort.height / 2);
  await page.mouse.down();
  await page.mouse.move(targetPort.x + targetPort.width / 2, targetPort.y + targetPort.height / 2, { steps: 8 });
  await page.mouse.up();
  await expect(graph.getByRole("alert")).toHaveText("This connection already exists.");
  const actionNode = graph.locator('.react-flow__node[data-id="action:handback"]');
  const beforeDrag = await actionNode.boundingBox();
  const line = graph.locator('.react-flow__edge[data-id="from:handback:found"] .react-flow__edge-path');
  const beforeLine = await line.getAttribute("d");
  if (!beforeDrag || !beforeLine) throw new Error("The connected action was not laid out");
  await page.mouse.move(beforeDrag.x + beforeDrag.width / 2, beforeDrag.y + 20);
  await page.mouse.down();
  await page.mouse.move(beforeDrag.x + beforeDrag.width / 2 + 42, beforeDrag.y + 85, { steps: 12 });
  await page.mouse.up();
  await expect.poll(async () => (await actionNode.boundingBox())?.y ?? 0).toBeGreaterThan(beforeDrag.y + 35);
  await expect.poll(() => line.getAttribute("d")).not.toBe(beforeLine);
  await graph.getByRole("button", { name: "Restore canvas" }).click();
  const beforeResize = await graph.boundingBox();
  if (!beforeResize) throw new Error("The graph could not be resized");
  await page.mouse.move(beforeResize.x + beforeResize.width - 3, beforeResize.y + beforeResize.height - 3);
  await page.mouse.down();
  await page.mouse.move(beforeResize.x + beforeResize.width - 3, beforeResize.y + beforeResize.height + 72, { steps: 8 });
  await page.mouse.up();
  await expect.poll(async () => (await graph.boundingBox())?.height ?? 0).toBeGreaterThan(beforeResize.height + 35);
  // What people will see, while it is composed: the status bar and the action's form.
  const preview = page.getByRole("region", { name: "What people see" });
  await expect(graph.getByText("Found", { exact: true })).toBeVisible();
  await page.screenshot({ path: testInfo.outputPath("process-map.png"), fullPage: true });
  await preview.getByRole("tab", { name: "Record preview" }).click();
  await expect(preview.locator("ol").getByText("Found", { exact: true })).toBeVisible();
  await expect(preview.getByText("Handed to *")).toBeVisible();
  await page.screenshot({ path: testInfo.outputPath("object-process.png"), fullPage: true });
  await page.setViewportSize({ width: 390, height: 780 });
  const box = async (name: string) => (await page.getByRole("region", { name }).boundingBox())!;
  const [mobileOutline, mobileCanvas, mobileInspector] = await Promise.all([box("States and actions"), box("What people see"), box("The piece in hand")]);
  expect(mobileOutline.y + mobileOutline.height).toBeLessThanOrEqual(mobileCanvas.y + 1);
  expect(mobileCanvas.y + mobileCanvas.height).toBeLessThanOrEqual(mobileInspector.y + 1);
  expect(mobileCanvas.height).toBeGreaterThan(200);
  expect(mobileInspector.height).toBeGreaterThan(200);
  await page.screenshot({ path: testInfo.outputPath("mobile-process.png"), fullPage: true });
  await page.setViewportSize({ width: 1280, height: 720 });
  await page.getByRole("button", { name: "Publish" }).click();
  await expect(page.getByText("The object is installed with its states and actions.")).toBeVisible();

  // A person takes it on a record, from the object's own page.
  await decide(request, "manager", "build", `build.${name}.create`, { type: `build.${name}`, id: "L-UMB" + stamp }, { item: "Umbrella " + stamp, value: 20 });
  await decide(request, "manager", "build", `build.${name}.create`, { type: `build.${name}`, id: "L-WAT" + stamp }, { item: "Watch " + stamp, value: 900 });
  await open(page, "manager", `/page?app=build&kind=page&name=${name}`);
  await page.getByRole("row").filter({ hasText: "Umbrella " + stamp }).click();
  await page.getByRole("button", { name: "Hand it back" }).click();
  let dialog = page.getByRole("dialog");
  await dialog.getByRole("textbox", { name: "Handed to *" }).fill("Ada");
  await dialog.getByRole("button", { name: "Hand it back" }).click();
  await expect(page.getByRole("definition").filter({ hasText: /^Ada$/ })).toBeVisible();
  await expect(page.getByRole("button", { name: "Hand it back" })).toHaveCount(0); // it is returned: the step is behind it
  // Refused with the builder's own words.
  await page.getByRole("row").filter({ hasText: "Watch " + stamp }).click();
  await page.getByRole("button", { name: "Hand it back" }).click();
  dialog = page.getByRole("dialog");
  await dialog.getByRole("textbox", { name: "Handed to *" }).fill("Bob");
  await dialog.getByRole("button", { name: "Hand it back" }).click();
  await expect(page.getByText("Valuables go back through the manager.").first()).toBeVisible();
});

// Route 35 (ADR-0037 18b): a builder says who may do what with an object in
// the editor; two members with different roles in the builder app then see
// and do different things on the same object.
test("route 35: who may do what with an object a tenant defines", async ({ page, request }) => {
  const stamp = Date.now().toString(36).slice(-5), name = `claims${stamp}`, id = fresh("O");
  await decide(request, "manager", "build", "build.object.create", { type: "build.object", id }, {
    name, title: "Claim", plural: "Claims " + stamp,
    fields: [{ name: "item", title: "Item", type: "text", required: true, search: true }, { name: "value", title: "Value", type: "integer" }],
  });
  await open(page, "manager", `/process?id=${id}`);
  const outline = page.getByRole("region", { name: "States and actions" });
  const inHand = page.getByRole("region", { name: "The piece in hand" });
  // Desk ("user" in the builder app) sees only what it created and may not set the value.
  await outline.getByRole("button", { name: "Add a role" }).click();
  await inHand.getByRole("textbox", { name: "Role in the builder app" }).fill("user");
  await expect(inHand.getByRole("combobox", { name: "Which records it reads" })).toHaveValue("own");
  await inHand.getByRole("checkbox", { name: "Edit records" }).uncheck();
  // A second role that reads everything and alone reads and sets the value.
  await outline.getByRole("button", { name: "Add a role" }).click();
  await inHand.getByRole("textbox", { name: "Role in the builder app" }).fill("auditor" + stamp);
  await inHand.getByRole("combobox", { name: "Which records it reads" }).selectOption("all");
  await inHand.getByRole("checkbox", { name: "Create records" }).uncheck();
  const valueRow = inHand.locator("div").filter({ hasText: /^Value/ }).last();
  await valueRow.getByRole("checkbox", { name: "only its readers" }).check();
  await valueRow.getByRole("checkbox", { name: "only its setters" }).check();
  await page.getByRole("button", { name: "Publish" }).click();
  await expect(page.getByText("The object is installed with its states and actions.")).toBeVisible();

  // The new role is the builder app's: the Console grants it.
  await decide(request, "manager", "platform", "platform.member.grant", { type: "platform.member", id: "sales-1" }, { app: "build", role: "auditor" + stamp });
  const type = `build.${name}`;
  await decide(request, "desk", "build", `${type}.create`, { type, id: "C-D" + stamp }, { item: "Desk's scarf " + stamp });
  await decide(request, "manager", "build", `${type}.create`, { type, id: "C-M" + stamp }, { item: "Manager's pen " + stamp, value: 40 });

  // desk sees its own claim only, without the value; it may not edit.
  const desk = await page.context().newPage();
  await open(desk, "desk", `/page?app=build&kind=page&name=${name}`);
  await expect(desk.getByRole("row").filter({ hasText: "Desk's scarf " + stamp })).toBeVisible();
  await expect(desk.getByRole("row").filter({ hasText: "Manager's pen " + stamp })).toHaveCount(0);
  await desk.getByRole("row").filter({ hasText: "Desk's scarf " + stamp }).click();
  await expect(desk.getByRole("button", { name: "Edit" })).toHaveCount(0);
  await expect(desk.getByRole("term").filter({ hasText: /^Value$/ })).toHaveCount(0);
  // A link to the other's claim answers as if it were not there.
  const other = await (await request.get(`/v1/records/${type}/C-M${stamp}`, { headers: { Authorization: "Bearer desk" } })).status();
  expect(other).toBe(404);
  await desk.close();

  // The auditor reads both, with the value; it may not create.
  const auditor = await page.context().newPage();
  await open(auditor, "sales", `/page?app=build&kind=page&name=${name}`);
  await expect(auditor.getByRole("row").filter({ hasText: "Desk's scarf " + stamp })).toBeVisible();
  await expect(auditor.getByRole("row").filter({ hasText: "Manager's pen " + stamp })).toBeVisible();
  await expect(auditor.getByRole("button", { name: "Create Claim" })).toHaveCount(0);
  await auditor.getByRole("row").filter({ hasText: "Manager's pen " + stamp }).click();
  await expect(auditor.getByRole("definition").filter({ hasText: /^40$/ })).toBeVisible();
  await auditor.close();
  // Leave sales as the shared host had it.
  await decide(request, "manager", "platform", "platform.member.revoke", { type: "platform.member", id: "sales-1" }, { app: "build" });
});

// Route 36 (ADR-0037 18c): a tenant action uses the work app's approval,
// edited in the process builder and decided in the same inbox as coded apps.
test("route 36: approve a tenant-defined action", async ({ page, request }, testInfo) => {
  const stamp = Date.now().toString(36).slice(-5), name = `found${stamp}`, id = fresh("O");
  await decide(request, "manager", "build", "build.object.create", { type: "build.object", id }, {
    name, title: "Found item", fields: [{ name: "item", title: "Item", type: "text", required: true, search: true }],
    states: [{ name: "found", title: "Found" }, { name: "pending", title: "Pending review" }, { name: "returned", title: "Returned" }],
    actions: [{ name: "handback", title: "Hand it back", from: ["found"], to: "returned" }],
  });
  await open(page, "manager", `/process?id=${id}`);
  const outline = page.getByRole("region", { name: "States and actions" });
  const inHand = page.getByRole("region", { name: "The piece in hand" });
  await outline.getByRole("button", { name: "Hand it back", exact: true }).click();
  await inHand.getByRole("button", { name: "Approval settings" }).click();
  await inHand.getByRole("checkbox", { name: "Wait for approval" }).check();
  await inHand.getByRole("combobox", { name: "While it waits" }).selectOption("pending");
  await page.getByRole("tab", { name: "Record preview" }).click();
  await expect(page.getByRole("region", { name: "What people see" }).getByText("Waits in Pending review for Approver (builder)")).toBeVisible();
  await page.screenshot({ path: testInfo.outputPath("defined-approval-editor.png"), fullPage: true });
  await page.getByRole("button", { name: "Publish" }).click();
  await expect(page.getByText("The object is installed with its states and actions.")).toBeVisible();

  const type = `build.${name}`, record = `F-${stamp}`;
  await decide(request, "desk", "build", `${type}.create`, { type, id: record }, { item: `Scarf ${stamp}` });
  const desk = await page.context().newPage();
  await open(desk, "desk", `/page?app=build&kind=page&name=${name}`);
  await desk.getByRole("row").filter({ hasText: `Scarf ${stamp}` }).click();
  await desk.getByRole("button", { name: "Hand it back" }).click();
  await expect(desk.getByText(/^waiting for Approver:/)).toBeVisible();
  await expect(desk.getByText("Pending review", { exact: true }).first()).toBeVisible();
  await open(page, "manager", "/inbox");
  const task = page.getByRole("listitem").filter({ hasText: record });
  await expect(task).toBeVisible();
  await task.getByRole("button", { name: "Approve", exact: true }).click();
  await expect(desk.getByText("Returned", { exact: true }).first()).toBeVisible();
  await desk.close();
});
