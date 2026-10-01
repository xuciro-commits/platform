// Browser smoke: canonical build/use paths. Visual layout is reviewed manually.
import { expect, test, type Page } from "@playwright/test";
import { decide, fresh, open } from "./host";

const value = (page: Page, text: string) => page.getByRole("definition").filter({ hasText: new RegExp(`^${text}$`, "i") });

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

test("route 4: an approval reaches the requester's page", async ({ page, request }) => {
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
  const managerPage = await page.context().newPage();
  await open(managerPage, "manager", "/inbox");
  const task = managerPage.getByRole("listitem").filter({ hasText: leave });
  await expect(task).toBeVisible();
  await task.getByRole("button").first().click();
  await managerPage.getByRole("button", { name: "Open related record" }).click();
  await managerPage.getByRole("region", { name: "Approvals", exact: true }).getByRole("button", { name: "Submit for approval", exact: true }).click();
  const approvalID = new URLSearchParams(new URL(managerPage.url()).hash.split("?")[1]).get("id")!;
  await managerPage.getByRole("region", { name: approvalID, exact: true }).getByRole("button", { name: "Approve", exact: true }).click();
  await managerPage.getByRole("dialog").getByRole("button", { name: "Approve", exact: true }).click();
  await managerPage.getByRole("button", { name: "Back to inbox" }).click();
  await expect(value(page, "Approved")).toBeVisible();
  await managerPage.close();
});

test("route 28: preview cannot submit an action", async ({ page }) => {
  await open(page, "sales", "/definitions");
  await page.getByRole("row").filter({ hasText: "crm/page/opportunities" }).click();
  await page.getByRole("button", { name: "Preview page" }).click();
  await expect(page.getByText("Preview uses sample data. Actions do not run.")).toBeVisible();
  const writes: string[] = [];
  page.on("request", (r) => { if (r.method() !== "GET" && r.url().includes("/v1/")) writes.push(r.url()); });
  await page.getByRole("button", { name: "Plan group stay" }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog.getByText("Preview only")).toBeVisible();
  await expect(dialog.getByRole("button", { name: "Plan group stay" })).toBeDisabled();
  expect(writes).toEqual([]);
});

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

test("route 29: define an object, publish it, use it", async ({ page }) => {
  const name = `visit${Date.now().toString(36).slice(-5)}`;
  await open(page, "manager", "/home");
  await page.getByRole("button", { name: "Application Studio" }).first().click(); // the app, from the launcher
  await page.getByRole("region", { name: "Application Studio", exact: true }).getByRole("button", { name: "Objects and relationships", exact: true }).click();
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
  await expect(page.getByRole("heading", { name: "Design Visit", exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Direct install", exact: true }).click();
  await expect(page.getByText("The object is installed with its states and actions.")).toBeVisible();

  // Publication does not dump an automatic CRUD page into Studio's menu.
  const nav = page.getByRole("navigation", { name: "Main", exact: true });
  await expect(nav.getByRole("button", { name: "Objects", exact: true })).toHaveCount(1);
  await expect(nav.getByRole("button", { name: "Process and access", exact: true })).toHaveCount(0);
  await expect(nav.getByRole("button", { name: "Visits", exact: true })).toHaveCount(0);
  await page.getByRole("button", { name: "Open records", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Visits" })).toBeVisible();
  await page.getByRole("button", { name: "Create Visit" }).click();
  const create = page.getByRole("dialog");
  await create.getByRole("textbox", { name: "Guest" }).fill("Ada Lovelace");
  await create.getByRole("button", { name: "Create" }).click();
  await expect(page.getByRole("row").filter({ hasText: "Ada Lovelace" })).toBeVisible();
});

test("route 30: compose a page of widgets and use it", async ({ page, request }) => {
  const account = fresh("ACC"), opp = fresh("OPP"), name = `offsites${Date.now().toString(36).slice(-5)}`;
  await decide(request, "sales", "crm", "crm.account.create", { type: "crm.account", id: account }, { name: "Composed " + account, kind: "company" });
  await decide(request, "sales", "crm", "crm.opportunity.open", { type: "crm.opportunity", id: opp }, { account, title: "Composed offsite " + opp });
  await open(page, "manager", "/home");
  await page.getByRole("button", { name: "Application Studio" }).first().click();
  await page.getByRole("navigation", { name: "Main", exact: true }).getByRole("button", { name: "Pages", exact: true }).click();
  await page.getByRole("button", { name: "Create page" }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByRole("textbox", { name: "Name" }).first().fill(name);
  await dialog.getByRole("textbox", { name: "What people call it" }).fill("Group offsites");
  await dialog.getByRole("textbox", { name: "Object it shows" }).fill("crm.opportunity");
  await dialog.getByRole("button", { name: "Create" }).click();

  // The composer: a layout panel, a canvas over real records, a widget panel.
  await page.getByRole("row").filter({ hasText: name }).click();
  await expect(page.getByText("Actions do not run while you compose.")).toBeVisible();
  // Neither installation nor release review accepts an empty page.
  await expect(page.getByRole("button", { name: "Direct install", exact: true })).toBeDisabled();
  await expect(page.getByRole("button", { name: "Review release", exact: true })).toBeDisabled();
  await expect(page.getByText("Add at least one widget before installing or reviewing a release.")).toBeVisible();
  await page.getByRole("button", { name: "Table", exact: true }).click();
  await page.getByRole("group", { name: "Fields it shows" }).getByRole("button", { name: "Stage" }).click();
  await page.getByRole("button", { name: "Detail", exact: true }).click();
  await page.getByRole("button", { name: "Actions", exact: true }).click();
  await page.getByRole("group", { name: "Actions it offers" }).getByRole("button", { name: "Close opportunity" }).click();
  // Clicking a widget on the canvas takes it in hand.
  await page.locator("div").filter({ hasText: /^Detail/ }).last().click();
  await expect(page.getByRole("group", { name: "Fields it shows" })).toBeVisible();
  // Release review saves the edited page and keeps its kind/id, without installing it.
  const pages = await (await request.get("/v1/records/build.page?limit=500", { headers: { Authorization: "Bearer manager" } })).json();
  const draft = pages.records.find((record: { name: string }) => record.name === name);
  await page.getByRole("button", { name: "Review release", exact: true }).click();
  await expect(page.getByRole("combobox", { name: "Definition kind" })).toHaveValue("page");
  await expect(page.getByRole("combobox", { name: "Saved draft" })).toHaveValue(draft.id);
  const saved = await (await request.get(`/v1/records/build.page/${draft.id}`, { headers: { Authorization: "Bearer manager" } })).json();
  expect(saved.record.sections.map((section: { widget: string }) => section.widget)).toEqual(["table", "detail", "actions"]);
  expect(saved.record.state).toBe("draft");
  await open(page, "manager", `/compose?id=${draft.id}`);
  await page.getByRole("button", { name: "Direct install", exact: true }).click();
  await expect(page.getByText("The page is in the workspace.")).toBeVisible(); // what the host answered
  await expect(page.getByRole("region", { name: "Compose a page", exact: true }).getByText("Published", { exact: true })).toBeVisible(); // and the composer shows where the page stands

  // What was composed is what people open: the table fills the detail beside it.
  await page.getByRole("button", { name: "Open published page", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Group offsites" })).toBeVisible();
  await page.getByRole("textbox", { name: "Search" }).first().fill(opp);
  await page.getByRole("row").filter({ hasText: opp }).click();
  await expect(page.getByRole("heading", { name: "Composed offsite " + opp, exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "Close opportunity" })).toBeVisible(); // the CRM's own action, on a page someone composed
});

test("route 31: hand an application to the people who use it", async ({ page, request }) => {
  const account = fresh("ACC"), opp = fresh("OPP");
  const name = `desk${Date.now().toString(36).slice(-5)}`, pageName = `handed${Date.now().toString(36).slice(-5)}`;
  await decide(request, "sales", "crm", "crm.account.create", { type: "crm.account", id: account }, { name: "Handed " + account, kind: "company" });
  await decide(request, "sales", "crm", "crm.opportunity.open", { type: "crm.opportunity", id: opp }, { account, title: "Handed offsite " + opp });
  // A page to hand over, composed as in route 30.
  await open(page, "manager", "/home");
  await page.getByRole("button", { name: "Application Studio" }).first().click();
  await page.getByRole("navigation", { name: "Main", exact: true }).getByRole("button", { name: "Pages", exact: true }).click();
  await page.getByRole("button", { name: "Create page" }).click();
  let dialog = page.getByRole("dialog");
  await dialog.getByRole("textbox", { name: "Name" }).first().fill(pageName);
  await dialog.getByRole("textbox", { name: "What people call it" }).fill("Handed offsites");
  await dialog.getByRole("textbox", { name: "Object it shows" }).fill("crm.opportunity");
  await dialog.getByRole("button", { name: "Create" }).click();
  await page.getByRole("row").filter({ hasText: pageName }).click();
  await page.getByRole("button", { name: "Table", exact: true }).click();
  await page.getByRole("button", { name: "Direct install", exact: true }).click();
  await expect(page.getByText("The page is in the workspace.")).toBeVisible();

  // The application editor writes typed membership and reviews the whole app.
  const flowID = fresh("FLOW"), flowName = `routing${Date.now().toString(36).slice(-5)}`;
  await decide(request, "manager", "build", "build.process.create", { type: "build.process", id: flowID }, { name: flowName, title: "Shared routing", manual: true, steps: [{ name: "end", kind: "end" }] });
  await decide(request, "manager", "build", "build.process.publish", { type: "build.process", id: flowID }, {});
  await page.getByRole("button", { name: "Applications", exact: true }).click();
  await page.getByRole("button", { name: "Create application", exact: true }).click();
  await page.getByRole("textbox", { name: "Name", exact: true }).fill(name);
  await page.getByRole("textbox", { name: "What people call it", exact: true }).fill("Front desk");
  await page.getByRole("combobox", { name: "Icon", exact: true }).selectOption("clipboard");
  await page.getByRole("checkbox", { name: new RegExp(`Handed offsites.*${pageName}`) }).check();
  await page.getByRole("checkbox", { name: /Shared routing/ }).check();
  await page.getByRole("button", { name: "Create application", exact: true }).click();
  await expect(page.getByRole("button", { name: "Review application release", exact: true })).toBeEnabled();
  await page.reload();
  await expect(page.getByRole("checkbox", { name: /Shared routing/ })).toBeChecked();
  await page.getByRole("button", { name: "Review application release", exact: true }).click();
  await page.getByRole("button", { name: "Check draft and dependencies", exact: true }).click();
  await expect(page.getByText(`build/flow/build.${flowName}`, { exact: true }).first()).toBeVisible();
  await page.getByRole("button", { name: "Save immutable candidate", exact: true }).click();
  await page.getByRole("button", { name: "Activate release", exact: true }).click();
  await expect(page.getByText("Release active for operators.", { exact: false })).toBeVisible();

  // It is in the launcher, and its page opens from its own navigation.
  await page.getByRole("navigation", { name: "Main" }).getByRole("button", { name: "Application launcher", exact: true }).click();
  await expect(page.getByRole("heading", { name: /Welcome/ })).toBeVisible(); // the launcher itself
  await expect(page.getByRole("button", { name: "Front desk" }).first()).toBeVisible();
  await page.getByRole("button", { name: "Front desk" }).first().click();
  await expect(page.getByRole("heading", { name: "Handed offsites" })).toBeVisible();
  // A shared page view retains its application's shell after a fresh load.
  await page.reload();
  const nav = page.getByRole("navigation", { name: "Main", exact: true });
  await expect(nav.getByRole("button", { name: "Handed offsites", exact: true })).toHaveCount(1);
  await expect(nav.getByRole("button", { name: "Objects", exact: true })).toHaveCount(0);
  await expect(nav.getByRole("button", { name: "Definitions", exact: true })).toHaveCount(0);
  // Old unambiguous page links also resolve membership, even from Studio.
  await page.getByRole("button", { name: "Apps", exact: true }).click();
  await page.getByRole("menuitemradio", { name: "Application Studio", exact: true }).click();
  await page.goto(`/#/page?app=build&kind=page&name=${pageName}`);
  await expect(nav.getByRole("button", { name: "Handed offsites", exact: true })).toHaveCount(1);
});

test("route 34: states and actions a tenant defines", async ({ page, request }) => {
  const stamp = Date.now().toString(36).slice(-5), name = `lost${stamp}`, id = fresh("O");
  await decide(request, "manager", "build", "build.object.create", { type: "build.object", id }, {
    name, title: "Lost item", plural: "Lost items " + stamp,
    fields: [{ name: "item", title: "Item", type: "text", required: true, search: true }, { name: "value", title: "Value", type: "integer" },
      { name: "claimant", title: "Handed to", type: "text" }],
  });
  await open(page, "manager", `/process?id=${id}`);
  await expect(page.getByRole("button", { name: "Back to objects" })).toBeVisible();
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
  await expect(inHand.getByRole("group", { name: "Taken from" }).getByRole("button", { name: "Found" })).toHaveAttribute("aria-pressed", "true");
  await page.getByRole("button", { name: "Direct install", exact: true }).click();
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
