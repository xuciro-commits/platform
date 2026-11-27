import { readFileSync } from "node:fs";
import { crc32 } from "node:zlib";
import { type Page, expect, test } from "@playwright/test";
import { signIn, property, edit, expectRevision, submit } from "./support";

const choose = async (form: ReturnType<Page["getByRole"]> | Page, page: Page, name: string, label: string) => {
  await form.getByRole("combobox", { name: new RegExp("^" + name + "(?: Required)?$") }).fill(label);
  await page.getByRole("option", { name: label, exact: true }).click();
};

test.describe.serial("crm and peek", () => {
test("crm: deal product lines total into the deal and move between deals", async ({ page }) => {
  await signIn(page);
  const line = await submit(page, "crm.dealline.create", "crm.dealline", "acceptance-line", { deal: "demo-deal-1", product: "demo-product-1", quantity: 2, price: { amount: 50000, currency: "USD" }, discount: 10 }, 0);
  const id = line.record.submission.target.id;
  await page.goto("/ontology/objects/crm.deal/demo-deal-1");
  const rows = page.getByRole("table", { name: "Deal products" });
  await expect(rows).toBeVisible();
  const row = rows.getByRole("row").filter({ hasText: "$900.00" });
  await expect(row).toBeVisible();
  await edit(row, "Quantity");
  await row.getByRole("spinbutton", { name: "Quantity" }).fill("3");
  await row.getByRole("spinbutton", { name: "Quantity" }).press("Enter");
  await expect(rows.getByText("$1,350.00 USD", { exact: true })).toBeVisible();
  await page.goto(`/ontology/objects/crm.dealline/${id}`);
  await edit(page, "Deal");
  await choose(page, page, "Deal", "Harborline Multi-Branch Pipeline Rollout");
  await page.reload();
  await expect(property(page, "Deal")).toContainText("Harborline Multi-Branch Pipeline Rollout");
  await page.goto("/ontology/objects/crm.deal/demo-deal-1");
  await expect(rows.getByText("$1,350.00 USD", { exact: true })).toHaveCount(0);
});

test("crm: a note and activity link to actual CRM leads and deals", async ({ page }) => {
  await signIn(page);
  const note = await submit(page, "crm.note.create", "crm.note", "acceptance-note", { title: "Linked note", deal: "demo-deal-1" }, 0);
  await page.goto(`/ontology/objects/crm.note/${note.record.submission.target.id}`);
  await expect(property(page, "Deal")).toContainText("Northwind Regional CRM Expansion");
  const activity = await submit(page, "crm.activity.create", "crm.activity", "acceptance-activity", { subject: "Linked follow-up", lead: "demo-lead-1" }, 0);
  await page.goto(`/ontology/objects/crm.activity/${activity.record.submission.target.id}`);
  await expect(property(page, "Lead")).toContainText("Zhao Ming");
  await edit(page, "Deal");
  await choose(page, page, "Deal", "Northwind Regional CRM Expansion");
  await page.reload();
  await expect(property(page, "Deal")).toContainText("Northwind Regional CRM Expansion");
});

test("crm: date filters stay available while reference choices load", async ({ page }) => {
  await signIn(page);
  await page.setViewportSize({ width: 1600, height: 900 });
  let release!: () => void;
  const held = new Promise<void>((resolve) => { release = resolve; });
  let requested = false;
  await page.route(/\/v1\/records\/crm\.account\?/, async (route) => { requested = true; await held; await route.continue(); });
  try {
    await page.goto("/ontology/objects/crm.deal");
    await expect.poll(() => requested).toBe(true);
    await expect(page.getByRole("combobox", { name: "Account filter", exact: true })).toBeVisible();
    const date = page.getByRole("combobox", { name: "Expected close filter", exact: true });
    if (!(await date.count())) await page.getByRole("button", { name: /more filters?$/ }).click();
    await date.click();
    const next = page.getByRole("radio", { name: "Next 30 days", exact: true });
    await expect(next).toBeVisible();
    const response = page.waitForResponse((r) => r.url().includes("/v1/records/crm.account?") && r.ok());
    release();
    await response;
    await expect(next).toBeVisible();
    await next.click();
    await expect(page).toHaveURL(/expectedclose.*after.*expectedclose.*before/);
  } finally {
    release();
    await page.unrouteAll({ behavior: "wait" });
  }
});

test("crm: a sales rep works only their own leads while the sales manager sees the whole team", async ({ page }) => {
  await signIn(page, "user-1 · acceptance");
  await submit(page, "crm.lead.create", "crm.lead", "acceptance-own-lead", { fullname: "Own lead", source: "website" }, 0);
  await signIn(page, "builder-1 · acceptance");
  await submit(page, "crm.lead.create", "crm.lead", "acceptance-peer-lead", { fullname: "Peer lead" }, 0);
  await signIn(page, "user-1 · acceptance");
  await page.goto("/ontology/objects/crm.lead");
  await page.getByRole("radio", { name: "Table" }).click();
  await expect(page.getByText("Own lead", { exact: true })).toBeVisible();
  await expect(page.getByText("Peer lead", { exact: true })).toHaveCount(0);
  await signIn(page, "manager-1 · acceptance");
  await page.goto("/ontology/objects/crm.lead");
  await page.getByRole("radio", { name: "Table" }).click();
  await expect(page.getByText("Own lead", { exact: true })).toBeVisible();
  await expect(page.getByText("Peer lead", { exact: true })).toBeVisible();
});

function workbookSheets(sheets: { name: string; rows: string[][] }[]) {
  const main = "http://schemas.openxmlformats.org/spreadsheetml/2006/main";
  const links = "http://schemas.openxmlformats.org/officeDocument/2006/relationships";
  const strings = [...new Set(sheets.flatMap((sheet) => sheet.rows.flat()).filter(Boolean))];
  const cells = (row: string[], r: number) => row.map((text, c) => (text ? `<c r="${String.fromCharCode(65 + c)}${r}" t="s"><v>${strings.indexOf(text)}</v></c>` : "")).join("");
  const parts: Record<string, string> = {
    "xl/workbook.xml": `<workbook xmlns="${main}" xmlns:r="${links}"><sheets>${sheets.map((sheet, idx) => `<sheet name="${sheet.name}" sheetId="${idx + 1}" r:id="rId${idx + 1}"/>`).join("")}</sheets></workbook>`,
    "xl/_rels/workbook.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">${sheets.map((_, idx) => `<Relationship Id="rId${idx + 1}" Type="${links}/worksheet" Target="worksheets/sheet${idx + 1}.xml"/>`).join("")}</Relationships>`,
    "xl/sharedStrings.xml": `<sst xmlns="${main}">${strings.map((text) => `<si><t>${text}</t></si>`).join("")}</sst>`,
  };
  sheets.forEach((sheet, idx) => {
    parts[`xl/worksheets/sheet${idx + 1}.xml`] = `<worksheet xmlns="${main}"><sheetData>${sheet.rows.map((row, r) => `<row r="${r + 1}">${cells(row, r + 1)}</row>`).join("")}</sheetData></worksheet>`;
  });
  const stored: Buffer[] = [];
  const directory: Buffer[] = [];
  let offset = 0;
  for (const [name, text] of Object.entries(parts)) {
    const data = Buffer.from(text);
    const path = Buffer.from(name);
    const local = Buffer.alloc(30);
    local.writeUInt32LE(0x04034b50, 0);
    local.writeUInt32LE(crc32(data), 14);
    local.writeUInt32LE(data.length, 18);
    local.writeUInt32LE(data.length, 22);
    local.writeUInt16LE(path.length, 26);
    const entry = Buffer.alloc(46);
    entry.writeUInt32LE(0x02014b50, 0);
    entry.writeUInt32LE(crc32(data), 16);
    entry.writeUInt32LE(data.length, 20);
    entry.writeUInt32LE(data.length, 24);
    entry.writeUInt16LE(path.length, 28);
    entry.writeUInt32LE(offset, 42);
    stored.push(local, path, data);
    directory.push(entry, path);
    offset += local.length + path.length + data.length;
  }
  const end = Buffer.alloc(22);
  end.writeUInt32LE(0x06054b50, 0);
  end.writeUInt16LE(directory.length / 2, 8);
  end.writeUInt16LE(directory.length / 2, 10);
  end.writeUInt32LE(directory.reduce((size, part) => size + part.length, 0), 12);
  end.writeUInt32LE(offset, 16);
  return Buffer.concat([...stored, ...directory, end]);
}

function workbook(rows: string[][]) {
  return workbookSheets([{ name: "Sheet1", rows }]);
}

const unique = () => Date.now().toString(36) + Math.random().toString(36).slice(2, 6);
const today = () => new Date().toISOString().slice(0, 10);

async function as(page: Page, seat: string) {
  await signIn(page, `${seat} · crm`);
}

async function lead(page: Page, values: Record<string, unknown>) {
  const id = `e2e-lead-${unique()}`;
  await submit(page, "crm.lead.create", "crm.lead", id, values);
  return id;
}

async function activity(page: Page, values: Record<string, unknown>) {
  const id = `e2e-act-${unique()}`;
  await submit(page, "crm.activity.create", "crm.activity", id, values);
  return id;
}

async function deal(page: Page, values: Record<string, unknown>) {
  const id = `e2e-deal-${unique()}`;
  await submit(page, "crm.deal.create", "crm.deal", id, { account: "demo-account-1", amount: { amount: 1000000, currency: "USD" }, expectedclose: "2026-12-31",
    nextstep: "Call back", nextstepdue: "2026-12-01", ...values });
  return id;
}

test("dashboard: the CRM home shows its pipeline numbers and charts, and each block opens the list it counts", async ({ page }) => {
  await as(page, "manager-1");
  await page.goto("/crm/dashboard");
  const home = page.getByRole("main");
  for (const title of ["Open pipeline", "Weighted forecast", "Won revenue", "Open leads"]) await expect(home.getByRole("link", { name: title, exact: true })).toBeVisible();
  await expect(home.getByRole("link", { name: "Weighted forecast", exact: true }).locator("..")).toContainText(/\$[1-9][\d,.]*[KMB]? USD/);
  await expect(home.getByRole("link", { name: "Open leads", exact: true }).locator("..")).toHaveText(/\D\d+$/);
  await expect(home.getByRole("heading", { name: "Dashboard", exact: true })).toBeVisible();
  for (const title of ["Pipeline by stage", "Pipeline by owner", "Forecast", "Leads by source", "Win rate"]) await expect(home.getByRole("heading", { name: title, exact: true })).toBeVisible();
  await expect(home.getByText("You do not have access to this data")).toHaveCount(0);
  await expect(home.getByRole("link", { name: "Won", exact: true })).toBeVisible();
  await home.locator(".recharts-bar-rectangle").first().click();
  await expect(page).toHaveURL(/\/crm\/crm\.deal\?domain=.*open/);
  await expect(page.getByText("Clear all", { exact: true })).toBeVisible();
  await page.goBack();
  await expect(home.getByRole("link", { name: "Won revenue", exact: true }).locator("..")).toContainText(/\$[1-9][\d,.]*[KMB]? USD/);
  await home.getByText("Won revenue", { exact: true }).click();
  await expect(page).toHaveURL(/\/crm\/crm\.deal\?domain=.*won/);
  await expect(page.getByRole("link", { name: "Northwind Service Desk Add-on", exact: true })).toBeVisible();
});

test("calendar: activities sit on their due dates, the month pages, a drag reschedules and an activity opens its record", async ({ page }) => {
  await as(page, "manager-1");
  await page.goto(`/crm/crm.activity?view=${encodeURIComponent(JSON.stringify({ version: 1, layout: "calendar", month: "2027-06" }))}`);
  await expect(page.getByRole("heading", { level: 3, name: "June 2027", exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "New", exact: true })).toBeVisible();
  const subject = `安排北境合同签署 ${unique()}`;
  await activity(page, { subject, due: "2026-11-18", deal: "demo-deal-1" });
  await page.goto(`/crm/crm.activity?view=${encodeURIComponent(JSON.stringify({ version: 1, layout: "calendar", month: "2026-11" }))}`);
  const day = (date: string) => page.getByRole("group", { name: date, exact: true });
  const card = day("November 18, 2026").getByText(subject, { exact: true });
  await expect(card).toBeVisible();
  const from = (await card.boundingBox())!;
  const to = (await day("November 20, 2026").boundingBox())!;
  await page.mouse.move(from.x + 4, from.y + 4);
  await page.mouse.down();
  await page.mouse.move(from.x + 40, from.y + 20, { steps: 4 });
  await page.mouse.move(to.x + to.width / 2, to.y + to.height / 2, { steps: 8 });
  await page.mouse.up();
  await expect(day("November 20, 2026")).toContainText(subject);
  await page.reload();
  await expect(day("November 20, 2026")).toContainText(subject);
  await expect(day("November 18, 2026")).not.toContainText(subject);
  await page.getByRole("button", { name: "Next month", exact: true }).click();
  await expect(page.getByRole("heading", { level: 3, name: "December 2026", exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Previous month", exact: true }).click();
  await day("November 20, 2026").getByText(subject, { exact: true }).click();
  await expect(page).toHaveURL(/\/crm\/crm\.activity\/e2e-act-/);
});

test("follow-up: the activity tab shows how long a lead sat in each follow-up stage", async ({ page }) => {
  await as(page, "manager-1");
  const id = await lead(page, { fullname: `停留 ${unique()}`, owner: "manager-1" });
  await page.goto(`/crm/crm.lead/${id}/activity`);
  const stays = page.getByRole("list", { name: "Time in each state", exact: true });
  await expect(stays.getByRole("listitem")).toHaveCount(1);
  await expect(stays.getByRole("listitem").first()).toHaveAccessibleName(/^New: \d+/);
  await edit(page, "Follow-up stage");
  await page.getByRole("option", { name: "Connected", exact: true }).click();
  await expect(stays.getByRole("listitem")).toHaveCount(2);
  await expect(stays.getByRole("listitem").nth(1)).toHaveAccessibleName(/^Connected: \d+/);
});

test("follow-up: a lead past its response time is marked missed and tells its owner, a connected lead meets it", async ({ page }) => {
  await as(page, "manager-1");
  const late = `聂涛 ${unique()}`;
  const missed = await lead(page, { fullname: late, owner: "manager-1", responseby: "2026-11-01T00:00:00Z" });
  await expect(async () => {
    await page.goto(`/crm/crm.lead/${missed}/properties`);
    await expect(property(page, "Response time")).toContainText("Missed", { timeout: 1_000 });
  }).toPass({ timeout: 20_000 });
  await expect(property(page, "Respond by")).toHaveText("Nov 1, 2026, 12:00 AM");
  await page.goto("/home/inbox/notifications");
  await page.getByRole("row", { name: new RegExp(`First response missed: ${late}`) }).click();
  await page.getByRole("link", { name: late, exact: true }).click();
  await expect(property(page.getByRole("dialog", { name: late }), "Response time")).toContainText("Missed");

  const answered = await lead(page, { fullname: `及时 ${unique()}`, owner: "manager-1" });
  await page.goto(`/crm/crm.lead/${answered}/properties`);
  await expect(property(page, "Response time")).toContainText("Due");
  await edit(page, "Follow-up stage");
  await page.getByRole("option", { name: "Connected", exact: true }).click();
  await expect(property(page, "Follow-up stage")).toContainText("Connected");
  await expect(property(page, "Response time")).toContainText("Met", { timeout: 20_000 });
});

test("follow-up: an activity due today waits in To do, opens its record and leaves when completed", async ({ page }) => {
  await as(page, "manager-1");
  const subject = `完成曦和的安全评审 ${unique()}`;
  const id = await activity(page, { subject, due: today(), assignee: "manager-1", deal: "demo-deal-2" });
  let applicationsHeld = false;
  let releaseApplications!: () => void;
  const applicationGate = new Promise<void>((resolve) => { releaseApplications = resolve; });
  await page.route("**/v1/applications", async (route) => {
    if (route.request().headers()["referer"]?.includes("/home/inbox/todo")) {
      applicationsHeld = true;
      await applicationGate;
    }
    await route.continue();
  });
  const pane = page.getByRole("complementary", { name: "Item" });
  const fullPage = pane.getByRole("link", { name: "Open full page" });
  try {
    await page.goto("/home/inbox/todo");
    await page.getByRole("row", { name: new RegExp(subject) }).click();
    await expect(pane.getByRole("heading", { name: subject })).toBeVisible();
    await expect.poll(() => applicationsHeld).toBe(true);
    await expect(fullPage).toHaveCount(0);
  } finally {
    releaseApplications();
    await page.unrouteAll({ behavior: "wait" });
  }
  await expect(fullPage).toHaveAttribute("href", `/crm/crm.activity/${id}`);
  await fullPage.click();
  await expect(page).toHaveURL(new RegExp(`/crm/crm\\.activity/${id}$`));
  await page.getByRole("main").getByRole("button", { name: "Complete", exact: true }).click();
  const done = page.getByRole("dialog");
  await done.getByRole("textbox", { name: "Outcome" }).fill("评审通过");
  await done.getByRole("button", { name: "Complete", exact: true }).click();
  await expect(done).toBeHidden();
  await page.goto("/home/inbox/todo");
  await expect(page.getByRole("heading", { name: "Inbox", exact: true })).toBeVisible();
  await expect(page.getByRole("row", { name: new RegExp(subject) })).toHaveCount(0);
});

test("follow-up: new leads without an owner are assigned evenly across the sales reps", async ({ page }) => {
  await as(page, "manager-1");
  const owners: string[] = [];
  for (const person of ["官网访客甲", "官网访客乙", "官网访客丙"]) {
    await page.goto("/crm/crm.lead");
    await page.getByRole("button", { name: "New", exact: true }).click();
    const form = page.getByRole("dialog");
    await form.getByRole("textbox", { name: "Full name" }).fill(`${person} ${unique()}`);
    await form.getByRole("button", { name: "Create", exact: true }).click();
    await expect(page.getByRole("heading", { name: new RegExp(person) }).first()).toBeVisible();
    const owner = property(page, "Lead owner");
    await expect(owner).toContainText(/韩奥|田雪/, { timeout: 30_000 });
    owners.push((await owner.innerText()).trim());
  }
  expect(new Set(owners)).toEqual(new Set(["韩奥", "田雪"]));
});

test("import: an Excel workbook of leads, accounts and contacts maps by column title, resolves contact-to-account references, fixes refused rows without affecting others while another tab views and edits, and exports CSV", async ({ page, context }) => {
  test.setTimeout(120_000);
  await as(page, "manager-1");
  const tag = unique();
  const peerLeadId = await lead(page, { fullname: `Concurrent Lead ${tag}`, company: "Before Import", owner: "manager-1" });
  const peerPage = await context.newPage();
  await as(peerPage, "manager-1");
  await peerPage.goto(`/crm/crm.lead/${peerLeadId}`);
  await expect(peerPage.getByRole("heading", { level: 1, name: `Concurrent Lead ${tag}`, exact: true })).toBeVisible();

  await page.goto("/crm/crm.lead");
  await page.getByRole("button", { name: "More actions", exact: true }).click();
  await page.getByRole("menuitem", { name: "Import", exact: true }).click();
  const opener = page.getByRole("dialog", { name: "Import · Leads", exact: true });
  await opener.locator('input[type="file"]').setInputFiles({
    name: "onboarding.xlsx",
    mimeType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
    buffer: workbookSheets([
      {
        name: "Leads",
        rows: [
          ["Full name", "Company", "Follow-up stage", "Source", "Industry"],
          [`Ada Lin ${tag}`, `Nebula Robotics ${tag}`, "New", "网站", "制造"],
          [`Bo Chen ${tag}`, `Helios Solar ${tag}`, "已联系", "Website", "Manufacturing"],
          ["", "Nameless Co", "New", "", ""],
          [`Cy Wu ${tag}`, "Fabrikam", "Pending", "", ""],
        ],
      },
      {
        name: "Contacts",
        rows: [
          ["Full name", "Email", "Job title", "Account"],
          [`Clara Zhou ${tag}`, `clara.${tag}@nebula.example`, "VP Engineering", `Nebula Robotics ${tag}`],
          [`Derek Sun ${tag}`, `derek.${tag}@helios.example`, "Procurement Lead", `Helios Solar ${tag}`],
          [`Evan Li ${tag}`, `evan.${tag}@ghost.example`, "Buyer", `Ghost Corp ${tag}`],
        ],
      },
      {
        name: "Accounts",
        rows: [
          ["Account", "Website", "Relationship"],
          [`Nebula Robotics ${tag}`, "https://nebula.example", "Customer"],
          [`Helios Solar ${tag}`, "https://helios.example", "Prospect"],
        ],
      },
    ]),
  });

  const wizard = page.getByRole("dialog", { name: "Import · Workbook", exact: true });
  await expect(wizard.getByText("9 records", { exact: true })).toBeVisible();
  await wizard.getByRole("button", { name: "Continue", exact: true }).click();
  await expect(wizard.getByRole("radio", { name: "Leads (4)", exact: true })).toBeVisible();
  await wizard.getByRole("radio", { name: "Contacts (3)", exact: true }).click();
  await expect(wizard.getByRole("combobox", { name: "Fields: Account", exact: true })).toBeVisible();
  await wizard.getByRole("radio", { name: "Accounts (2)", exact: true }).click();
  await expect(wizard.getByRole("combobox", { name: "Fields: Account", exact: true })).toBeVisible();

  await wizard.getByRole("button", { name: "Continue", exact: true }).click();
  await expect(wizard.getByText("6 can be imported · 3 need fixing", { exact: true })).toBeVisible();
  await expect(wizard.getByRole("row", { name: /Pending.*Follow-up stage: Choose a listed value\./ })).toBeVisible();
  await expect(wizard.getByRole("row", { name: /Ghost Corp.*This value cannot be read: Account/ })).toBeVisible();

  const importValid = wizard.getByRole("button", { name: "Import 6", exact: true }).click();
  await edit(peerPage, "Company");
  const peerCompany = peerPage.getByRole("textbox", { name: "Company", exact: true });
  await peerCompany.fill(`Edited During Import ${tag}`);
  await peerCompany.press("Enter");
  await expectRevision(peerPage, 2);
  await importValid;
  await peerPage.close();

  await expect(wizard.getByText("6 records imported", { exact: true })).toBeVisible();
  await expect(wizard.getByText("0 can be imported · 3 need fixing", { exact: true })).toBeVisible();

  await wizard.getByRole("textbox", { name: "Full name · Leads · Row 3", exact: true }).fill(`Fiona Gao ${tag}`);
  await wizard.getByRole("textbox", { name: "Follow-up stage · Leads · Row 4", exact: true }).fill("Connected");
  await wizard.getByRole("textbox", { name: "Account · Contacts · Row 3", exact: true }).fill(`Nebula Robotics ${tag}`);
  await expect(wizard.getByRole("button", { name: "Import 0", exact: true })).toBeDisabled();
  await wizard.getByRole("button", { name: "Check", exact: true }).click();
  await expect(wizard.getByText("3 can be imported · 0 need fixing", { exact: true })).toBeVisible();
  await wizard.getByRole("button", { name: "Import 3", exact: true }).click();
  await expect(wizard.getByText("9 records imported", { exact: true })).toBeVisible();
  await wizard.getByRole("button", { name: "Done", exact: true }).click();
  await expect(wizard).toBeHidden();

  const search = page.getByRole("textbox", { name: "Search this list" });
  await search.fill(`Ada Lin ${tag}`);
  await search.press("Enter");
  await expect(page.getByRole("link", { name: `Ada Lin ${tag}`, exact: true })).toHaveCount(1);
  await page.getByRole("link", { name: `Ada Lin ${tag}`, exact: true }).click();
  await expect(property(page, "Source")).toContainText("Website");
  await expect(property(page, "Industry")).toContainText("Manufacturing");

  await page.goto("/crm/crm.lead");
  const nextSearch = page.getByRole("textbox", { name: "Search this list" });
  await nextSearch.fill(`Fiona Gao ${tag}`);
  await nextSearch.press("Enter");
  await expect(page.getByRole("link", { name: `Fiona Gao ${tag}`, exact: true })).toBeVisible();
  await expect(page.getByRole("link", { name: `Ada Lin ${tag}`, exact: true })).toHaveCount(0);
  const started = page.waitForEvent("download");
  await page.getByRole("button", { name: "More actions", exact: true }).click();
  await page.getByRole("menuitem", { name: "Export as CSV", exact: true }).click();
  const exported = readFileSync((await (await started).path())!, "utf8");
  expect(exported).toContain(`Fiona Gao ${tag}`);
  expect(exported).not.toContain(`Ada Lin ${tag}`);
  await page.getByRole("link", { name: `Fiona Gao ${tag}`, exact: true }).click();
  await expect(page).toHaveURL((url) => /^\/crm\/crm\.lead\/LEAD-\d{4}-\d{5}$/.test(url.pathname));
  await expect(property(page, "Lead owner")).toContainText(/韩奥|田雪/, { timeout: 30_000 });

  await page.goto("/crm/crm.contact");
  const contactSearch = page.getByRole("textbox", { name: "Search this list" });
  await contactSearch.fill(`Clara Zhou ${tag}`);
  await contactSearch.press("Enter");
  await page.getByRole("link", { name: `Clara Zhou ${tag}`, exact: true }).click();
  await expect(property(page, "Account")).toContainText(`Nebula Robotics ${tag}`);

  await page.goto("/crm/crm.contact");
  await contactSearch.fill(`Evan Li ${tag}`);
  await contactSearch.press("Enter");
  await page.getByRole("link", { name: `Evan Li ${tag}`, exact: true }).click();
  await expect(property(page, "Account")).toContainText(`Nebula Robotics ${tag}`);
  await page.getByRole("main").getByRole("link", { name: `Nebula Robotics ${tag}`, exact: true }).first().click();
  const drawer = page.getByRole("dialog", { name: `Nebula Robotics ${tag}` });
  await expect(drawer.getByRole("heading", { level: 2, name: `Nebula Robotics ${tag}`, exact: true })).toBeVisible();
  await expect(drawer).toContainText("Customer");
});

test("invite: an administrator invites colleagues by customer account and grants editable or read-only CRM roles", async ({ page }) => {
  test.setTimeout(120_000);
  await as(page, "manager-1");
  const tag = unique();
  const importedLeadName = `Invited Lead ${tag}`;
  await page.goto("/crm/crm.lead");
  await page.getByRole("button", { name: "More actions", exact: true }).click();
  await page.getByRole("menuitem", { name: "Import", exact: true }).click();
  const importDialog = page.getByRole("dialog", { name: "Import · Leads", exact: true });
  await importDialog.locator('input[type="file"]').setInputFiles({
    name: "invite-leads.xlsx",
    mimeType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
    buffer: workbook([
      ["Full name", "Company", "Follow-up stage"],
      [importedLeadName, `Initial Account ${tag}`, "New"],
    ]),
  });
  await expect(importDialog.getByText("1 records", { exact: true })).toBeVisible();
  await importDialog.getByRole("button", { name: "Continue", exact: true }).click();
  await importDialog.getByRole("button", { name: "Continue", exact: true }).click();
  await expect(importDialog.getByText("1 can be imported · 0 need fixing", { exact: true })).toBeVisible();
  await importDialog.getByRole("button", { name: "Import 1", exact: true }).click();
  await expect(importDialog.getByText("1 records imported", { exact: true })).toBeVisible();
  await importDialog.getByRole("button", { name: "Done", exact: true }).click();

  await as(page, "admin-1");
  await page.goto("/admin/members");
  await page.getByRole("button", { name: "Invite colleague", exact: true }).click();
  const inviteEditor = page.getByRole("dialog", { name: "Invite colleague", exact: true });
  const editorSlug = `lin-${tag}`;
  await inviteEditor.getByRole("textbox", { name: /^Customer account/ }).fill(`${editorSlug}@northwind.example`);
  await inviteEditor.getByRole("textbox", { name: "Display name", exact: true }).fill(`Lin Lan ${tag}`);
  await inviteEditor.getByRole("combobox", { name: "Application", exact: true }).click();
  await page.getByRole("option", { name: "CRM", exact: true }).click();
  await inviteEditor.getByRole("combobox", { name: "Role", exact: true }).click();
  await page.getByRole("option", { name: "salesmanager", exact: true }).click();
  await inviteEditor.getByRole("button", { name: "Invite", exact: true }).click();
  await expect(inviteEditor).toBeHidden({ timeout: 30_000 });
  await expect(page.getByRole("row", { name: new RegExp(`Lin Lan ${tag}`) })).toContainText("crm: salesmanager");

  await page.getByRole("button", { name: "Invite colleague", exact: true }).click();
  const inviteViewer = page.getByRole("dialog", { name: "Invite colleague", exact: true });
  const viewerSlug = `zhou-${tag}`;
  await inviteViewer.getByRole("textbox", { name: /^Customer account/ }).fill(`${viewerSlug}@northwind.example`);
  await inviteViewer.getByRole("textbox", { name: "Display name", exact: true }).fill(`Zhou Yue ${tag}`);
  await inviteViewer.getByRole("combobox", { name: "Application", exact: true }).click();
  await page.getByRole("option", { name: "CRM", exact: true }).click();
  await inviteViewer.getByRole("combobox", { name: "Role", exact: true }).click();
  await page.getByRole("option", { name: "salesmanager", exact: true }).click();
  await inviteViewer.getByRole("radio", { name: "Read-only", exact: true }).click();
  await inviteViewer.getByRole("button", { name: "Invite", exact: true }).click();
  await expect(inviteViewer).toBeHidden({ timeout: 30_000 });
  await expect(page.getByRole("row", { name: new RegExp(`Zhou Yue ${tag}`) })).toContainText("crm: salesmanager");

  await as(page, editorSlug);
  await page.goto("/crm/crm.lead");
  const editorSearch = page.getByRole("textbox", { name: "Search this list" });
  await editorSearch.fill(importedLeadName);
  await editorSearch.press("Enter");
  await page.getByRole("link", { name: importedLeadName, exact: true }).click();
  await edit(page, "Company");
  const companyInput = page.getByRole("textbox", { name: "Company", exact: true });
  await companyInput.fill(`Updated By Colleague ${tag}`);
  await companyInput.press("Enter");
  await expect(property(page, "Company")).toContainText(`Updated By Colleague ${tag}`);

  await as(page, viewerSlug);
  await page.goto("/crm/crm.lead");
  await expect(page.getByRole("button", { name: "New", exact: true })).toHaveCount(0);
  const viewerSearch = page.getByRole("textbox", { name: "Search this list" });
  await viewerSearch.fill(importedLeadName);
  await viewerSearch.press("Enter");
  await page.getByRole("link", { name: importedLeadName, exact: true }).click();
  await expect(property(page, "Company")).toContainText(`Updated By Colleague ${tag}`);
  await expect(page.getByRole("button", { name: /^Edit / })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Convert to deal", exact: true })).toHaveCount(0);
});

test("naming: two new activities get consecutive numbers", async ({ page }) => {
  await as(page, "manager-1");
  const create = async (subject: string) => {
    const answer = await submit(page, "crm.activity.create", "crm.activity", "", { subject }, 0);
    const id = answer.record.submission.target.id as string;
    expect(id).toMatch(/^ACT-\d{4}-\d{5}$/);
    return Number(id.slice(-5));
  };
  const first = await create(`Numbered one ${unique()}`);
  const second = await create(`Numbered two ${unique()}`);
  expect(second).toBe(first + 1);
});

test("defaults and from: an activity is assigned to its creator, and a deal takes its probability from its stage until someone overrides it", async ({ page }) => {
  await as(page, "manager-1");
  await page.goto("/crm/crm.activity");
  await page.getByRole("button", { name: "New", exact: true }).click();
  const form = page.getByRole("dialog");
  await form.getByRole("textbox", { name: "Subject" }).fill(`Default owner ${unique()}`);
  await form.getByRole("button", { name: "Create", exact: true }).click();
  await expect(property(page, "Assigned to")).toContainText("赵敏");

  const id = await deal(page, { title: `Derived ${unique()}` });
  await page.goto(`/crm/crm.deal/${id}`);
  await expect(property(page, "Probability %")).toHaveText("10%");
  await expect(page.getByText("From Stage", { exact: true }).first()).toBeVisible();
  await edit(page, "Stage");
  await page.getByRole("combobox", { name: "Stage", exact: true }).fill("Proposal");
  await page.getByRole("option", { name: /Proposal/ }).click();
  await expect(property(page, "Probability %")).toHaveText("50%");
  await edit(page, "Probability %");
  const probability = page.getByRole("spinbutton", { name: "Probability %", exact: true });
  await probability.fill("35");
  await probability.press("Enter");
  await expect(property(page, "Probability %")).toHaveText("35%");
  await edit(page, "Next step");
  const next = page.getByRole("textbox", { name: "Next step", exact: true });
  await next.fill("Send the revised proposal");
  await next.press("Enter");
  await page.reload();
  await expect(property(page, "Probability %")).toHaveText("35%");
});

test("roles: sales reps see only business menu entries and actions, and see a lead immediately after it is assigned to them", async ({ page }) => {
  await as(page, "manager-1");
  const mine = `我的线索 ${unique()}`;
  const handed = `移交线索 ${unique()}`;
  const mineId = await lead(page, { fullname: mine, owner: "user-1", stage: "attempting" });
  const handedId = await lead(page, { fullname: handed, owner: "manager-1", stage: "attempting" });

  await page.goto("/");
  await page.getByRole("button", { name: "Account", exact: true }).click();
  await page.getByRole("menuitem", { name: "Sign out", exact: true }).click();
  await as(page, "user-1");
  await page.getByRole("link", { name: "CRM", exact: true }).first().click();
  const nav = page.getByRole("navigation", { name: "Primary navigation" });
  await expect(nav.getByRole("link", { name: "Leads", exact: true })).toBeVisible();
  await expect(nav.getByRole("link", { name: "Settings", exact: true })).toHaveCount(0);
  await page.goto("/crm/crm.lead");
  await expect(page.getByRole("link", { name: mine, exact: true })).toBeVisible();
  await expect(page.getByRole("link", { name: handed, exact: true })).toHaveCount(0);
  await page.goto(`/crm/crm.lead/${mineId}`);
  const more = page.getByRole("button", { name: "More actions" });
  if (await more.isVisible()) {
    await more.click();
    await expect(page.getByRole("menuitem", { name: /^Archive/ })).toHaveCount(0);
    await page.keyboard.press("Escape");
  }

  await page.getByRole("button", { name: "Account", exact: true }).click();
  await page.getByRole("menuitem", { name: "Sign out", exact: true }).click();
  await as(page, "manager-1");
  await page.goto(`/crm/crm.lead/${handedId}`);
  await edit(page, "Lead owner");
  await page.getByRole("combobox", { name: "Lead owner", exact: true }).fill("田雪");
  await page.getByRole("option", { name: /田雪/ }).click();
  await expect(property(page, "Lead owner")).toContainText("田雪");

  await page.getByRole("button", { name: "Account", exact: true }).click();
  await page.getByRole("menuitem", { name: "Sign out", exact: true }).click();
  await as(page, "user-1");
  await page.goto("/crm/crm.lead");
  await expect(page.getByRole("link", { name: handed, exact: true })).toBeVisible();
});

test("peek: clicking a reference opens the record drawer without leaving the page, and a reference input creates a competitor inline", async ({ page }) => {
  await as(page, "manager-1");
  const id = await deal(page, { title: `Peek ${unique()}` });
  await page.goto(`/crm/crm.deal/${id}`);
  const dealURL = page.url();
  await page.getByRole("main").getByRole("link", { name: "Northwind Precision Manufacturing", exact: true }).first().click();
  const drawer = page.getByRole("dialog", { name: "Northwind Precision Manufacturing" });
  await expect(drawer.getByRole("heading", { level: 2, name: "Northwind Precision Manufacturing", exact: true })).toBeVisible();
  await expect(page).toHaveURL(dealURL);
  await page.keyboard.press("Escape");
  await expect(drawer).toBeHidden();

  await edit(page, "Main competitor");
  await page.getByRole("combobox", { name: "Main competitor", exact: true }).click();
  await page.getByRole("option", { name: /New Competitor…/ }).click();
  const create = page.getByRole("dialog");
  const rival = `Rival ${unique()}`;
  await create.getByRole("textbox", { name: "Competitor" }).fill(rival);
  await create.getByRole("button", { name: "Create", exact: true }).click();
  await expect(create).toBeHidden();
  await expect(property(page, "Main competitor")).toContainText(rival);
});

test("crm board: stage lanes carry their own colours and the plus in a lane header opens the create dialog with that stage", async ({ page }) => {
  await as(page, "manager-1");
  await page.goto("/crm/crm.deal");
  await page.getByRole("radio", { name: "Table" }).click();
  const dealRow = page.getByRole("row", { name: /Northwind Regional CRM Expansion/ });
  await expect(dealRow).toContainText(/2026-12-20|Dec 20, 2026/);
  await expect(dealRow).not.toContainText(/:\d{2}/);
  await page.getByRole("radio", { name: "Board" }).click();
  await expect(page.getByRole("region", { name: "Negotiation", exact: true })).toBeVisible();
  await page.getByRole("button", { name: "New in Negotiation", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog).toContainText("Negotiation");
  await dialog.getByRole("button", { name: "Cancel", exact: true }).click();
  await expect(dialog).toBeHidden();
});

test("crm timeline: activities attached to a deal show in its activity tab, and an activity added there is attached to it", async ({ page }) => {
  await as(page, "manager-1");
  const here = await deal(page, { title: `Timeline ${unique()}` });
  const other = await deal(page, { title: `Elsewhere ${unique()}` });
  const subject = `时间线里新建的活动 ${unique()}`;
  await page.goto(`/crm/crm.deal/${here}/activity`);
  await page.getByRole("button", { name: "Add Activity", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByRole("textbox", { name: "Subject" }).fill(subject);
  await dialog.getByRole("button", { name: "Create", exact: true }).click();
  await expect(dialog).toBeHidden();
  const added = page.getByRole("main").getByRole("link", { name: subject, exact: true });
  await expect(added).toBeVisible();
  await page.reload();
  await expect(added).toBeVisible();
  await page.goto(`/crm/crm.deal/${other}/activity`);
  await expect(page.getByRole("button", { name: "Add Activity", exact: true })).toBeVisible();
  await expect(page.getByRole("main").getByRole("link", { name: subject, exact: true })).toHaveCount(0);
});

test("crm timeline: a note added on a deal can be pinned above the timeline, and the timeline filters to one kind", async ({ page }) => {
  await as(page, "manager-1");
  const id = await deal(page, { title: `Pinned ${unique()}` });
  const title = `要置顶的备注 ${unique()}`;
  await page.goto(`/crm/crm.deal/${id}/activity`);
  await page.getByRole("button", { name: "Add Note", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByRole("textbox", { name: "Title" }).fill(title);
  await dialog.getByRole("button", { name: "Create", exact: true }).click();
  await expect(dialog).toBeHidden();
  const main = page.getByRole("main");
  await main.getByRole("button", { name: "Pin", exact: true }).first().click();
  const pinned = page.getByRole("list", { name: "Pinned" });
  await expect(pinned.getByRole("link", { name: title, exact: true })).toBeVisible();
  await page.getByRole("radio", { name: "Activities" }).click();
  await expect(pinned).toHaveCount(0);
  await page.getByRole("radio", { name: "Notes" }).click();
  await expect(pinned.getByRole("link", { name: title, exact: true })).toBeVisible();
  await pinned.getByRole("listitem").filter({ hasText: title }).getByRole("button", { name: "Unpin", exact: true }).click();
  await expect(pinned.getByRole("link", { name: title, exact: true })).toHaveCount(0);
  await expect(main.getByRole("link", { name: title, exact: true })).toBeVisible();
});

test("semantic: currency settings, minor units, compact KPIs, person cards, reference cards, percent and website links work across surfaces", async ({ page }) => {
  await as(page, "manager-1");
  const tag = unique();

  const idOf = (res: { record?: { submission?: { target?: { id?: string } } } }, fallback: string) =>
    res.record?.submission?.target?.id || fallback;

  const orgRes = await submit(page, "party.organization.create", "party.organization", `org-sem-${tag}`, {
    name: `Semantic Org ${tag}`,
  });
  const baseId = idOf(orgRes, `org-sem-${tag}`);

  const accRes = await submit(page, "crm.account.create", "crm.account", `acc-sem-${tag}`, {
    base: baseId,
    relationship: "customer",
    revenue: { amount: 2500000, currency: "JPY" },
  });
  const accId = idOf(accRes, `acc-sem-${tag}`);

  const dealRes = await submit(page, "crm.deal.create", "crm.deal", `deal-sem-${tag}`, {
    title: `Semantic Deal ${tag}`,
    account: accId,
    amount: { amount: 12500000, currency: "CNY" },
    probability: 75,
    expectedclose: "2026-12-31",
    nextstep: "Review contract",
    nextstepdue: "2026-12-15",
    owner: "user-1",
  });
  const dealId = idOf(dealRes, `deal-sem-${tag}`);

  const leadRes = await submit(page, "crm.lead.create", "crm.lead", `lead-sem-${tag}`, {
    fullname: `Semantic Lead ${tag}`,
    company: `Semantic Org ${tag}`,
    email: "hello@nebula-robotics.example",
    phone: "+1 415 555 0199",
    website: "https://nebula-robotics.example/portal",
  });
  const leadId = idOf(leadRes, `lead-sem-${tag}`);

  await page.goto(`/crm/crm.account/${accId}`);
  await expect(property(page, "Annual revenue")).toContainText("¥25,000 JPY");

  await page.goto(`/crm/crm.deal/${dealId}`);
  await expect(property(page, "Amount")).toContainText("¥125,000.00 CNY");
  await expect(property(page, "Weighted amount")).toContainText("¥93,750.00 CNY");
  await expect(property(page, "Probability %")).toHaveText("75%");

  const ownerProp = property(page, "Deal owner");
  await expect(ownerProp).toContainText("田雪");
  await ownerProp.getByText("田雪").first().hover();
  await expect(page.getByRole("heading", { level: 4, name: "田雪" })).toBeVisible();

  await page.mouse.move(0, 0);
  const accountLink = property(page, "Account").getByRole("link", { name: `Semantic Org ${tag}`, exact: true });
  await expect(accountLink).toBeVisible();
  await accountLink.hover();
  await expect(page.getByText("¥25,000 JPY").first()).toBeVisible();

  await page.goto(`/crm/crm.lead/${leadId}`);
  const webLink = property(page, "Website").getByRole("link", { name: "nebula-robotics.example/portal", exact: true });
  await expect(webLink).toHaveAttribute("href", "https://nebula-robotics.example/portal");
  await expect(property(page, "Email").getByRole("button", { name: "Send email", exact: true })).toBeVisible();
  await expect(property(page, "Phone").getByRole("button", { name: "Call", exact: true })).toBeVisible();

  const boardUrl = "/crm/crm.deal?view=" + encodeURIComponent(JSON.stringify({
    version: 1, layout: "board", columns: ["title", "account", "amount", "probability", "owner"],
    sort: [], filters: [], search: tag, group: "none", subGroup: "none",
    lanes: "stage", dates: "expectedclose", month: "", scale: "month",
    density: "balanced", stickyStart: "none", stickyEnd: "none",
  }));
  await page.goto(boardUrl);
  await expect(page.getByText("¥125K CNY")).toBeVisible();

  await page.getByRole("button", { name: "Account", exact: true }).click();
  await page.getByRole("menuitem", { name: "Settings", exact: true }).click();
  const settingsDialog = page.getByRole("dialog");
  await expect(settingsDialog).toBeVisible();
  await settingsDialog.getByRole("combobox", { name: "Language", exact: true }).click();
  await page.getByRole("option", { name: /中文/ }).click();
  await settingsDialog.getByRole("button", { name: /^(Close|关闭)$/ }).first().click();
  await expect(page.getByText("¥12.5万 CNY")).toBeVisible();

  await page.getByRole("button", { name: "账户", exact: true }).click();
  await page.getByRole("menuitem", { name: "设置", exact: true }).click();
  const zhDialog = page.getByRole("dialog");
  await expect(zhDialog).toBeVisible();
  await zhDialog.getByRole("combobox", { name: "语言", exact: true }).click();
  await page.getByRole("option", { name: /English/ }).click();
  await zhDialog.getByRole("button", { name: /^(Close|关闭)$/ }).first().click();
  await expect(page.getByText("¥125K CNY")).toBeVisible();
});
});
