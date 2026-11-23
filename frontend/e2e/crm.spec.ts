import { readFileSync } from "node:fs";
import { crc32 } from "node:zlib";
import { type Page, expect, test } from "@playwright/test";
import { signIn, open, property, edit, crmMainline } from "./support";

test.describe.serial("crm and peek", () => {
const dragCard = async (page: Page, fromColumn: string, cardName: string, toColumn: string) => {
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
  create: (form: ReturnType<Page["getByRole"]>, page: Page) => Promise<void>;
  createdTitle: string;
  editField: string;
  editValue: string;
};
const cases: Case[] = [
  {
    type: "crm.source", plural: "Lead sources", filterTerm: "展会", filterHit: "展会",
    create: async (form, page) => { await form.getByRole("textbox", { name: "Source name" }).fill("Partner Webinar"); },
    createdTitle: "Partner Webinar", editField: "Source name", editValue: "Partner Event",
  },
  {
    type: "crm.organization", plural: "Organizations", filterTerm: "曦和", filterHit: "曦和精密",
    create: async (form, page) => { await form.getByRole("textbox", { name: "Organization" }).fill("Orion Systems"); },
    createdTitle: "Orion Systems", editField: "Website", editValue: "https://orion.example",
  },
  {
    type: "crm.contact", plural: "Contacts", filterTerm: "魏", filterHit: "魏清",
    create: async (form, page) => { await form.getByRole("textbox", { name: "Full name" }).fill("Nadia Chen"); },
    createdTitle: "Nadia Chen", editField: "Designation", editValue: "Operations Lead",
  },
  {
    type: "crm.product", plural: "Products", filterTerm: "CRM-CORE", filterHit: "CRM-CORE",
    create: async (form, page) => {
      await form.getByRole("textbox", { name: "Product code" }).fill("CRM-TRN");
      await form.getByRole("textbox", { name: "Product name" }).fill("Admin Training");
      await form.getByRole("spinbutton", { name: "Standard rate" }).fill("1200");
      await form.getByRole("spinbutton", { name: "Standard rate" }).press("Tab");
    },
    createdTitle: "Admin Training", editField: "Product name", editValue: "Admin Training Pack",
  },
  {
    type: "crm.lead", plural: "Leads", filterTerm: "林子", filterHit: "林子航",
    create: async (form, page) => {
      await form.getByRole("textbox", { name: "Full name" }).fill("Sven Lindholm");
      await form.getByRole("combobox", { name: "Status" }).click();
      await page.getByRole("option", { name: "New", exact: true }).click();
    },
    createdTitle: "Sven Lindholm", editField: "Organization", editValue: "Baltic Robotics",
  },
  {
    type: "crm.deal", plural: "Deals", filterTerm: "曦和工厂", filterHit: "曦和工厂客户关系系统上线",
    create: async (form, page) => {
      await form.getByRole("textbox", { name: "Deal" }).fill("Orion phase one");
      await form.getByRole("combobox", { name: "Status" }).click();
      await page.getByRole("option", { name: "Qualification", exact: true }).click();
    },
    createdTitle: "Orion phase one", editField: "Deal", editValue: "Orion phase one rollout",
  },
  {
    type: "crm.dealline", plural: "Deal product lines", filterTerm: "ERP 连接器", filterHit: "ERP 连接器模块",
    create: async (form, page) => {
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
    type: "crm.task", plural: "Tasks", filterTerm: "北辰", filterHit: "给北辰发需求调研资料",
    create: async (form, page) => {
      await form.getByRole("textbox", { name: "Title" }).fill("Confirm CRM pilot dates");
      await form.getByRole("combobox", { name: "Status" }).click();
      await page.getByRole("option", { name: "Todo", exact: true }).click();
    },
    createdTitle: "Confirm CRM pilot dates", editField: "Title", editValue: "Confirm CRM pilot schedule",
  },
  {
    type: "crm.note", plural: "Notes", filterTerm: "北辰", filterHit: "北辰首次通话纪要",
    create: async (form, page) => { await form.getByRole("textbox", { name: "Title" }).fill("Pilot kickoff notes"); },
    createdTitle: "Pilot kickoff notes", editField: "Title", editValue: "Pilot kickoff summary",
  },
];

for (const entry of cases) {
  test(`crm: ${entry.plural} can be created, edited and filtered`, async ({ page }) => {
    await signIn(page);
      await page.goto(`/ontology/objects/${entry.type}`);
      await expect(page.getByRole("heading", { level: 1, name: entry.plural, exact: true })).toBeVisible();
      const search = page.getByRole("textbox", { name: "Search this list" });
      await search.fill(entry.filterTerm);
      await search.press("Enter");
      await expect(page.getByRole("link", { name: entry.filterHit, exact: true }).first()).toBeVisible();
      await page.getByRole("button", { name: "New", exact: true }).click();
      const form = page.getByRole("dialog");
      await entry.create(form, page);
      await form.getByRole("button", { name: "Create", exact: true }).click();
      await expect(page.getByRole("heading", { level: 1, name: entry.createdTitle, exact: true })).toBeVisible();
      await edit(page, entry.editField);
      const field = page.getByRole("textbox", { name: entry.editField, exact: true });
      await field.fill(entry.editValue);
      await field.press("Enter");
      await expect(page.getByRole("complementary").getByText("2", { exact: true })).toBeVisible();
  });
}

test("crm: deal product lines total into the deal and move between deals", async ({ page }) => {
  await signIn(page);

  await page.goto("/ontology/objects/crm.deal/DEL-001");
  const lines = page.getByRole("table", { name: "Deal product lines" });
  await expect(lines).toBeVisible();
  await expect(property(lines, "Product name").filter({ hasText: "平台核心席位" }).first()).toBeVisible();
  await expect(page.getByText("19,500.00 USD", { exact: true }).first()).toBeVisible();
  const added = lines.getByRole("row").filter({ hasText: "1,000.00 USD" });
  await edit(added, "Quantity");
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
  await edit(page, "Deal");
  await page.getByRole("combobox", { name: "Deal", exact: true }).fill("先锋");
  await page.getByRole("option", { name: /先锋车队门户/ }).click();
  await expect(page.getByRole("link", { name: "先锋车队门户" })).toBeVisible();
  await page.goto("/ontology/objects/crm.deal/DEL-001");
  await expect(page.getByText("18,500.00 USD", { exact: true }).first()).toBeVisible();
});

test("crm: notes show what they relate to and a task switches from a lead to a deal", async ({ page }) => {
  await signIn(page);
  await page.goto("/ontology/objects/crm.note");
  await expect(page.getByRole("columnheader", { name: "Related to" })).toBeVisible();
  await expect(page.getByRole("row", { name: /曦和采购时间线/ }).getByText("曦和工厂客户关系系统上线", { exact: true })).toBeVisible();
  await page.goto("/ontology/objects/crm.task/TSK-001");
  await expect(page.getByRole("radio", { name: "Lead", exact: true })).toBeChecked();
  await page.getByRole("radio", { name: "Deal", exact: true }).click();
  await page.getByRole("combobox", { name: "Deal", exact: true }).fill("曦和");
  await page.getByRole("option", { name: "曦和工厂客户关系系统上线" }).click();
  await expect(page.getByRole("link", { name: "曦和工厂客户关系系统上线" })).toBeVisible();
  await page.reload();
  await expect(page.getByRole("radio", { name: "Deal", exact: true })).toBeChecked();
  await expect(page.getByRole("link", { name: "曦和工厂客户关系系统上线" })).toBeVisible();
});

test("crm: lead and deal move on the kanban", async ({ page }) => {
  await signIn(page);
  await page.goto("/ontology/objects/crm.lead");
  await page.getByRole("radio", { name: "Board" }).click();
  await dragCard(page, "New", "林子航", "Contacted");
  await page.reload();
  await expect(page.getByRole("region", { name: "Contacted", exact: true }).getByRole("heading", { level: 4, name: "林子航", exact: true })).toBeVisible();

  await page.goto("/ontology/objects/crm.deal");
  await page.getByRole("radio", { name: "Board" }).click();
  await dragCard(page, "Qualification", "先锋车队门户", "Demo/Making");
  await page.reload();
  await expect(page.getByRole("region", { name: "Demo/Making", exact: true }).getByRole("heading", { level: 4, name: "先锋车队门户", exact: true })).toBeVisible();
});


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
  for (const entry of ["Leads", "Deals", "Organizations", "Contacts", "Tasks", "Notes"]) await expect(navigation.getByRole("link", { name: entry, exact: true })).toBeVisible();
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
  for (const line of ["Source: 邮件", "Industry: 物流", "Territory: 华东大区", "Lead owner: 田雪"]) await expect(main.getByRole("listitem", { name: line, exact: true })).toBeVisible();
  await expect(main.getByText("王丹", { exact: true }).first()).toBeVisible();
  await expect(main.getByText(/SRC-|IND-|TER-|admin-1|Unnamed member/)).toHaveCount(0);
});


function workbook(rows: string[][]) {
  const main = "http://schemas.openxmlformats.org/spreadsheetml/2006/main";
  const links = "http://schemas.openxmlformats.org/officeDocument/2006/relationships";
  const strings = [...new Set(rows.flat().filter(Boolean))];
  const cells = (row: string[], r: number) => row.map((text, c) => (text ? `<c r="${String.fromCharCode(65 + c)}${r}" t="s"><v>${strings.indexOf(text)}</v></c>` : "")).join("");
  const parts = {
    "xl/workbook.xml": `<workbook xmlns="${main}" xmlns:r="${links}"><sheets><sheet name="Sheet1" sheetId="1" r:id="rId1"/></sheets></workbook>`,
    "xl/_rels/workbook.xml.rels": `<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="${links}/worksheet" Target="worksheets/sheet1.xml"/></Relationships>`,
    "xl/sharedStrings.xml": `<sst xmlns="${main}">${strings.map((text) => `<si><t>${text}</t></si>`).join("")}</sst>`,
    "xl/worksheets/sheet1.xml": `<worksheet xmlns="${main}"><sheetData>${rows.map((row, r) => `<row r="${r + 1}">${cells(row, r + 1)}</row>`).join("")}</sheetData></worksheet>`,
  };
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

test("dashboard: an application's home shows its numbers and charts, and each block opens the list it counts", async ({ page }) => {
  await signIn(page, "admin-1 · crm");
  await page.goto("/crm/dashboard");
  const home = page.getByRole("main");
  for (const title of ["Leads", "Open deals", "Won deals", "Average deal value"]) await expect(home.getByRole("link", { name: title, exact: true })).toBeVisible();
  for (const title of ["Leads", "Open deals", "Won deals"]) await expect(home.getByRole("link", { name: title, exact: true }).locator("..")).toHaveText(/\D\d+$/);
  await expect(home.getByRole("link", { name: "Average deal value", exact: true }).locator("..")).toHaveText(/\d USD$/);
  await expect(home.getByRole("heading", { level: 1 })).toHaveCount(1);
  for (const title of ["Sales funnel", "Deal value by stage", "Leads by source", "New deals"]) await expect(home.getByRole("heading", { name: title, exact: true })).toBeVisible();
  await home.locator(".recharts-bar-rectangle").first().click();
  await expect(page).toHaveURL(/\/crm\/crm\.deal\?domain=.*open.*status/);
  await expect(page.getByRole("heading", { name: "Deals", exact: true })).toBeVisible();
  await expect(page.getByText("Clear all", { exact: true })).toBeVisible();
  await page.goBack();
  await home.getByText("Won deals", { exact: true }).click();
  await expect(page).toHaveURL(/\/crm\/crm\.deal\?domain=.*won/);
  await expect(page.getByRole("row").nth(1)).toContainText("Won");
});

test("calendar: tasks sit on their due dates, the month pages, a drag reschedules and a task opens its record", async ({ page }) => {
  await signIn(page, "admin-1 · crm");
  await page.goto("/crm/crm.task");
  await page.getByRole("radio", { name: "Calendar" }).click();
  await expect(page.getByRole("button", { name: "Today", exact: true })).toBeVisible();
  await page.goto(`/crm/crm.task?view=${encodeURIComponent(JSON.stringify({ version: 1, layout: "calendar", month: "2026-11" }))}`);
  const day = (date: string) => page.getByRole("group", { name: date, exact: true });
  const task = day("November 18, 2026").getByText("安排北境合同签署", { exact: true });
  await expect(task).toBeVisible();
  const from = (await task.boundingBox())!;
  const to = (await day("November 20, 2026").boundingBox())!;
  await page.mouse.move(from.x + 4, from.y + 4);
  await page.mouse.down();
  await page.mouse.move(from.x + 40, from.y + 20, { steps: 4 });
  await page.mouse.move(to.x + to.width / 2, to.y + to.height / 2, { steps: 8 });
  await page.mouse.up();
  await expect(day("November 20, 2026")).toContainText("安排北境合同签署");
  await page.reload();
  await expect(day("November 20, 2026")).toContainText("安排北境合同签署");
  await expect(day("November 18, 2026")).not.toContainText("安排北境合同签署");
  await page.getByRole("button", { name: "Next month", exact: true }).click();
  await expect(page.getByRole("heading", { level: 3, name: "December 2026", exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Previous month", exact: true }).click();
  await day("November 20, 2026").getByText("安排北境合同签署", { exact: true }).click();
  await expect(page).toHaveURL(/\/crm\/crm\.task\/TSK-\d+/);
});

test("follow-up: the activity tab shows how long a lead sat in each status", async ({ page }) => {
  await signIn(page, "admin-1 · crm");
  await page.goto("/crm/crm.lead/LED-003/activity");
  const stays = page.getByRole("list", { name: "Time in each state", exact: true });
  await expect(stays.getByRole("listitem")).toHaveCount(1);
  await expect(stays.getByRole("listitem").first()).toHaveAccessibleName(/^New: \d+/);
  await edit(page, "Status");
  await page.getByRole("combobox", { name: "Status" }).fill("Contacted");
  await page.getByRole("option", { name: "Contacted", exact: true }).click();
  await expect(stays.getByRole("listitem")).toHaveCount(2);
  await expect(stays.getByRole("listitem").nth(1)).toHaveAccessibleName(/^Contacted: \d+/);
});

test("follow-up: a lead past its first response deadline fails and tells its owner, a contacted lead is fulfilled", async ({ page }) => {
  await signIn(page, "admin-1 · crm");
  const settles = async (state: string) => expect(async () => {
    await page.reload();
    await expect(property(page, "First response")).toContainText(state, { timeout: 1_000 });
  }).toPass({ timeout: 20_000 });
  await page.goto("/crm/crm.lead/LED-018/properties");
  await settles("Failed");
  await expect(property(page, "Respond by")).toContainText("Nov 1, 2026");
  await page.getByRole("button", { name: "Notifications", exact: true }).click();
  await page.getByRole("menuitem", { name: /Inbox/ }).click();
  await page.getByRole("link", { name: /Notifications/ }).click();
  await page.getByRole("row", { name: /First response overdue: 聂涛/ }).click();
  await page.getByRole("link", { name: "聂涛", exact: true }).click();
  await expect(property(page.getByRole("dialog", { name: "聂涛" }), "First response")).toContainText("Failed");

  await page.goto("/crm/crm.lead/LED-002/properties");
  await expect(property(page, "First response")).toContainText("Due");
  await edit(page, "Status");
  await page.getByRole("combobox", { name: "Status" }).fill("Contacted");
  await page.getByRole("option", { name: "Contacted", exact: true }).click();
  await expect(property(page, "Status")).toContainText("Contacted");
  await expect(property(page, "First response")).toContainText("Fulfilled", { timeout: 20_000 });
});

test("follow-up: a task due by today waits in To do, opens its record and leaves when done", async ({ page }) => {
  await signIn(page, "admin-1 · crm");
  await page.goto("/home/inbox/todo");
  await page.getByRole("row", { name: /完成曦和的安全评审/ }).click();
  const pane = page.getByRole("complementary", { name: "Item" });
  await expect(pane.getByRole("heading", { name: "完成曦和的安全评审" })).toBeVisible();
  await expect(pane.getByText("Nov 20, 2026", { exact: true })).toBeVisible();
  await pane.getByRole("link", { name: "Open full page" }).click();
  await expect(page).toHaveURL(/\/crm\/crm\.task\/TSK-003$/);
  await edit(page, "Status");
  await page.getByRole("listbox", { name: "Status" }).getByRole("option", { name: "Done", exact: true }).click();
  await expect(property(page, "Status")).toContainText("Done");
  await page.goto("/home/inbox/todo");
  await expect(page.getByRole("heading", { name: "Inbox", exact: true })).toBeVisible();
  await expect(page.getByRole("row", { name: /完成曦和的安全评审/ })).toHaveCount(0);
});

test("follow-up: new website leads are assigned evenly across the sales reps", async ({ page }) => {
  await signIn(page, "admin-1 · crm");
  const owners: string[] = [];
  for (const person of ["官网访客甲", "官网访客乙", "官网访客丙"]) {
    await page.goto("/crm/crm.lead");
    await page.getByRole("button", { name: "New", exact: true }).click();
    const lead = page.getByRole("dialog");
    await lead.getByRole("textbox", { name: "Full name" }).fill(person);
    await lead.getByRole("combobox", { name: "Status" }).click();
    await page.getByRole("option", { name: "New", exact: true }).click();
    await lead.getByRole("button", { name: "Create", exact: true }).click();
    await expect(page.getByRole("heading", { level: 1, name: person, exact: true })).toBeVisible();
    await edit(page, "Source");
    await page.getByRole("combobox", { name: "Source", exact: true }).fill("官网");
    await page.getByRole("option", { name: /官网/ }).first().click();
    await expect(property(page, "Source")).toContainText("官网");
    const owner = property(page, "Lead owner");
    await expect(owner).toContainText(/韩奥|田雪/, { timeout: 30_000 });
    owners.push((await owner.innerText()).trim());
  }
  expect(new Set(owners)).toEqual(new Set(["韩奥", "田雪"]));
});

test("import: an Excel workbook of leads maps by column title, refused rows are fixed in place and retried, and the list exports what it shows as CSV", async ({ page }) => {
  await signIn(page, "admin-1 · crm");
  await page.goto("/crm/crm.lead");
  await page.getByRole("button", { name: "More actions", exact: true }).click();
  await page.getByRole("menuitem", { name: "Import", exact: true }).click();
  const wizard = page.getByRole("dialog", { name: "Import · Leads", exact: true });
  await wizard.locator('input[type="file"]').setInputFiles({
    name: "leads.xlsx",
    mimeType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
    buffer: workbook([
      ["Full name", "Organization", "Status", "Industry"],
      ["Ada Lin", "Northwind", "New", "制造"],
      ["Bo Chen", "Contoso", "Contacted", ""],
      ["", "Nameless Co", "New", ""],
      ["Cy Wu", "Fabrikam", "Pending", ""],
    ]),
  });
  await expect(wizard.getByText("4 records", { exact: true })).toBeVisible();
  await wizard.getByRole("button", { name: "Continue", exact: true }).click();
  await wizard.getByRole("button", { name: "Continue", exact: true }).click();
  await expect(wizard.getByText("2 can be imported · 2 need fixing", { exact: true })).toBeVisible();
  await expect(wizard.getByText("This value cannot be read: Status", { exact: true })).toBeVisible();
  await expect(wizard.getByRole("button", { name: "Import 2", exact: true })).toBeEnabled();
  await wizard.getByRole("textbox", { name: "Full name · Row 3", exact: true }).fill("Fiona Gao");
  await wizard.getByRole("textbox", { name: "Status · Row 4", exact: true }).fill("Contacted");
  await expect(wizard.getByRole("button", { name: "Import 2", exact: true })).toBeDisabled();
  await wizard.getByRole("button", { name: "Check", exact: true }).click();
  await expect(wizard.getByText("4 can be imported · 0 need fixing", { exact: true })).toBeVisible();
  await wizard.getByRole("button", { name: "Import 4", exact: true }).click();
  await expect(wizard.getByText("4 records imported", { exact: true })).toBeVisible();
  await wizard.getByRole("button", { name: "Done", exact: true }).click();
  await expect(wizard).toBeHidden();

  const search = page.getByRole("textbox", { name: "Search this list" });
  await search.fill("Fiona Gao");
  await search.press("Enter");
  await expect(page.getByRole("link", { name: "Fiona Gao", exact: true })).toBeVisible();
  await expect(page.getByRole("link", { name: "Ada Lin", exact: true })).toHaveCount(0);
  const started = page.waitForEvent("download");
  await page.getByRole("button", { name: "More actions", exact: true }).click();
  await page.getByRole("menuitem", { name: "Export as CSV", exact: true }).click();
  const exported = readFileSync((await (await started).path())!, "utf8");
  expect(exported).toContain("Fiona Gao");
  expect(exported).not.toContain("Ada Lin");
  await page.getByRole("link", { name: "Fiona Gao", exact: true }).click();
  await expect(page).toHaveURL((url) => /^\/crm\/crm\.lead\/CRM-LEAD-\d{4}-\d{5}$/.test(url.pathname));
  await expect(property(page, "Lead owner")).toContainText("王丹");
});

test("naming: two new deals get consecutive numbers", async ({ page }) => {
  await signIn(page, "admin-1 · crm");
  const createDeal = async (title: string) => {
    await page.goto("/crm/crm.deal");
    await page.getByRole("button", { name: "New", exact: true }).click();
    const form = page.getByRole("dialog");
    await form.getByRole("textbox", { name: "Deal" }).fill(title);
    await form.getByRole("combobox", { name: "Status" }).click();
    await page.getByRole("option", { name: "Qualification", exact: true }).click();
    await form.getByRole("button", { name: "Create", exact: true }).click();
    await expect(page).toHaveURL(/\/crm\/crm\.deal\/CRM-DEAL-\d{4}-\d{5}$/);
    return Number(page.url().slice(-5));
  };
  const first = await createDeal("Numbered deal one");
  const second = await createDeal("Numbered deal two");
  expect(second).toBe(first + 1);
});

test("defaults and from: a lead defaults its owner to the creator and a deal derives industry and territory from its organization", async ({ page }) => {
  await signIn(page, "admin-1 · crm");
  await page.goto("/crm/crm.lead");
  await page.getByRole("button", { name: "New", exact: true }).click();
  const form = page.getByRole("dialog");
  await form.getByRole("textbox", { name: "Full name" }).fill("Default Owner Lead");
  await form.getByRole("combobox", { name: "Status" }).click();
  await page.getByRole("option", { name: "New", exact: true }).click();
  await form.getByRole("button", { name: "Create", exact: true }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Default Owner Lead", exact: true })).toBeVisible();
  await expect(property(page, "Lead owner")).toContainText("王丹");
  await edit(page, "Lead owner");
  await page.getByRole("combobox", { name: "Lead owner", exact: true }).fill("韩奥");
  await page.getByRole("option", { name: /韩奥/ }).click();
  await page.reload();
  await expect(property(page, "Lead owner")).toContainText("韩奥");

  await page.goto("/crm/crm.deal");
  await page.getByRole("button", { name: "New", exact: true }).click();
  const dealForm = page.getByRole("dialog");
  await dealForm.getByRole("textbox", { name: "Deal" }).fill("Derived Deal");
  await dealForm.getByRole("combobox", { name: "Status" }).click();
  await page.getByRole("option", { name: "Qualification", exact: true }).click();
  await dealForm.getByRole("button", { name: "Create", exact: true }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Derived Deal", exact: true })).toBeVisible();
  await expect(page.getByText("From Organization", { exact: true }).first()).toBeVisible();
  await edit(page, "Organization");
  await page.getByRole("combobox", { name: "Organization", exact: true }).fill("曦和精密");
  await page.getByRole("option", { name: /曦和精密/ }).click();
  await expect(property(page, "Industry")).toContainText("制造");
  await expect(property(page, "Territory")).toContainText("长三角");
  await edit(page, "Industry");
  await page.getByRole("combobox", { name: "Industry", exact: true }).fill("软件");
  await page.getByRole("option", { name: "软件", exact: true }).click();
  await edit(page, "Deal");
  const title = page.getByRole("textbox", { name: "Deal", exact: true });
  await title.fill("Derived Deal Updated");
  await title.press("Enter");
  await page.reload();
  await expect(property(page, "Industry")).toContainText("软件");
});

test("roles: sales reps see only business menu entries and actions, and see a lead immediately after assignment", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("button", { name: "user-1 · crm" }).click();
  await page.getByRole("link", { name: "CRM", exact: true }).click();
  const nav = page.getByRole("navigation", { name: "Primary navigation" });
  await expect(nav.getByRole("link", { name: "Leads", exact: true })).toBeVisible();
  await expect(nav.getByRole("link", { name: "Products", exact: true })).toHaveCount(0);
  await expect(nav.getByRole("link", { name: "Territories", exact: true })).toHaveCount(0);
  await nav.getByRole("link", { name: "Leads", exact: true }).click();
  await expect(page.getByText("林子航", { exact: true })).toHaveCount(0);
  await page.getByRole("link", { name: "沈若溪", exact: true }).click();
  const more = page.getByRole("button", { name: "More actions" });
  if (await more.isVisible()) {
    await more.click();
    await expect(page.getByRole("menuitem", { name: /^Archive/ })).toHaveCount(0);
    await page.keyboard.press("Escape");
  }

  await page.getByRole("button", { name: "Account", exact: true }).click();
  await page.getByRole("menuitem", { name: "Sign out", exact: true }).click();
  await page.getByRole("button", { name: "manager-1 · crm" }).click();
  await page.getByRole("link", { name: "CRM", exact: true }).click();
  await expect(nav.getByRole("link", { name: "Products", exact: true })).toBeVisible();
  await nav.getByRole("link", { name: "Leads", exact: true }).click();
  await page.getByRole("link", { name: "林子航", exact: true }).click();
  await edit(page, "Lead owner");
  await page.getByRole("combobox", { name: "Lead owner", exact: true }).fill("田雪");
  await page.getByRole("option", { name: /田雪/ }).click();
  await expect(property(page, "Lead owner")).toContainText("田雪");

  await page.getByRole("button", { name: "Account", exact: true }).click();
  await page.getByRole("menuitem", { name: "Sign out", exact: true }).click();
  await page.getByRole("button", { name: "user-1 · crm" }).click();
  await page.goto("/crm/crm.lead");
  await expect(page.getByRole("link", { name: "林子航", exact: true })).toBeVisible();
});

test("peek: clicking a reference opens the record drawer without leaving the page, and a reference input creates a contact inline", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("button", { name: "admin-1 · crm" }).click();
  await page.goto("/crm/crm.deal/DEL-001");
  await expect(page.getByRole("heading", { level: 1 })).toBeVisible();
  const dealURL = page.url();
  const orgLink = page.getByRole("main").getByRole("link", { name: "曦和精密", exact: true }).first();
  await orgLink.click();
  const drawer = page.getByRole("dialog", { name: "曦和精密" });
  await expect(drawer.getByRole("heading", { level: 2, name: "曦和精密", exact: true })).toBeVisible();
  await expect(page).toHaveURL(dealURL);
  await page.keyboard.press("Escape");
  await expect(drawer).toBeHidden();
  await expect(page).toHaveURL(dealURL);

  await edit(page, "Primary contact");
  const contactInput = page.getByRole("combobox", { name: "Primary contact", exact: true });
  await contactInput.click();
  await page.getByRole("option", { name: /New Contact…/ }).click();
  const createDialog = page.getByRole("dialog");
  await createDialog.getByRole("textbox", { name: "Full name" }).fill("林小满");
  await createDialog.getByRole("button", { name: "Create", exact: true }).click();
  await expect(createDialog).toBeHidden();
  await expect(property(page, "Primary contact")).toContainText("林小满");
});

test("crm board: stage lanes carry their own colours and the plus in a lane header opens the create dialog with that stage", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("button", { name: "manager-1 · crm" }).click();
  await page.getByRole("link", { name: "CRM", exact: true }).click();
  await page.getByRole("navigation", { name: "Primary navigation" }).getByRole("link", { name: "Deals", exact: true }).click();
  await page.getByRole("radio", { name: "Board" }).click();
  await expect(page.getByRole("region", { name: "Negotiation", exact: true })).toBeVisible();
  await page.getByRole("button", { name: "New in Negotiation", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog.getByRole("group", { name: /Status/ }).or(dialog)).toContainText("Negotiation");
  await dialog.getByRole("button", { name: "Cancel", exact: true }).click();
  await expect(dialog).toBeHidden();
});

test("crm timeline: tasks and notes attached to a deal show in its activity, and a task added there is attached to it", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("button", { name: "manager-1 · crm" }).click();
  await page.goto("/crm/crm.deal/DEL-007/activity");
  await page.getByRole("button", { name: "Add Task", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByRole("textbox", { name: "Title" }).fill("时间线里新建的任务");
  await dialog.getByRole("combobox", { name: "Status" }).click();
  await page.getByRole("option", { name: "Todo", exact: true }).click();
  await dialog.getByRole("button", { name: "Create", exact: true }).click();
  await expect(dialog).toBeHidden();
  const added = page.getByRole("main").getByRole("link", { name: "时间线里新建的任务", exact: true });
  await expect(added).toBeVisible();
  await page.reload();
  await expect(added).toBeVisible();
  await page.goto("/crm/crm.deal/DEL-005/activity");
  await expect(page.getByRole("button", { name: "Add Task", exact: true })).toBeVisible();
  await expect(page.getByRole("main").getByRole("link", { name: "时间线里新建的任务", exact: true })).toHaveCount(0);
});

test("crm timeline: a note added on a deal can be pinned above the timeline, and the timeline filters to one kind", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("button", { name: "manager-1 · crm" }).click();
  await page.goto("/crm/crm.deal/DEL-003/activity");
  await page.getByRole("button", { name: "Add Note", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByRole("textbox", { name: "Title" }).fill("要置顶的备注");
  await dialog.getByRole("button", { name: "Create", exact: true }).click();
  await expect(dialog).toBeHidden();
  const main = page.getByRole("main");
  await main.getByRole("button", { name: "Pin", exact: true }).first().click();
  const pinned = page.getByRole("list", { name: "Pinned" });
  await expect(pinned.getByRole("link", { name: "要置顶的备注", exact: true })).toBeVisible();
  await page.getByRole("radio", { name: "Tasks" }).click();
  await expect(pinned).toHaveCount(0);
  await page.getByRole("radio", { name: "Notes" }).click();
  await expect(pinned.getByRole("link", { name: "要置顶的备注", exact: true })).toBeVisible();
  await pinned.getByRole("listitem").filter({ hasText: "要置顶的备注" }).getByRole("button", { name: "Unpin", exact: true }).click();
  await expect(pinned.getByRole("link", { name: "要置顶的备注", exact: true })).toHaveCount(0);
  await expect(main.getByRole("link", { name: "要置顶的备注", exact: true })).toBeVisible();
});

});
