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
  await decide(request, "sales", "crm", "crm.opportunity.close", { type: "crm.opportunity", id: opp }, { outcome: "won" });
  await expect(value(page, "confirmed")).toBeVisible(); // the page follows without a reload
});

// Route 4 (#118): a leave request approved by the manager; the requester's open
// page turns approved by itself, and the requester may open the approval.
test("route 4: an approval reaches the requester's page", async ({ page, request }) => {
  const leave = fresh("LEA");
  const day = (d: number) => new Date(Date.now() + d * 86_400_000).toISOString().slice(0, 10);
  await decide(request, "sales", "hcm", "hcm.leave.create", { type: "hcm.leave", id: leave }, { kind: "vacation", from: day(30), until: day(31) });
  const submitted = await decide(request, "sales", "hcm", "hcm.leave.submit", { type: "hcm.leave", id: leave }, {});
  await open(page, "sales", `/record?type=hcm.leave&id=${leave}`);
  await expect(value(page, "Draft")).toBeVisible();
  const approval = `hcm.${submitted.key}`;
  await decide(request, "manager", "work", "work.approval.approve", { type: "work.approval", id: approval }, {});
  await expect(value(page, "Approved")).toBeVisible();
  const opened = await request.get(`/v1/records/work.approval/${encodeURIComponent(approval)}`, { headers: { Authorization: "Bearer sales" } });
  expect(opened.status()).toBe(200);
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
  await dialog.getByRole("combobox").first().selectOption({ label: `${account} Picker ${account}` });
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
