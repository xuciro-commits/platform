// Browser smoke: canonical build/use paths. Visual layout is reviewed manually.
import { expect, test } from "./kit";
import type { Page } from "@playwright/test";
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
  await page.getByRole("button", { name: "Plan group stay" }).click();
  const plan = page.getByRole("dialog");
  await plan.getByLabel("Rooms, 1 to 20", { exact: false }).fill("1.5");
  await plan.getByLabel("The provider's room type", { exact: false }).fill("10");
  await expect(plan.getByText("Enter a whole number.")).toBeVisible();
  await plan.getByLabel("Rooms, 1 to 20", { exact: false }).fill("1");
  await plan.getByLabel("First night", { exact: false }).fill("2030-01-10");
  await plan.getByLabel("Departure", { exact: false }).fill("2030-01-11");
  await plan.getByLabel("The last day the rooms are held", { exact: false }).fill("2030-01-11");
  const keys: string[] = [];
  await page.route("**/v1/submissions", async route => {
    const submission = route.request().postDataJSON();
    if (submission.schema?.name !== "crm.opportunity.plan") return route.continue();
    keys.push(submission.idempotencyKey);
    if (keys.length === 1) return route.abort();
    await route.continue();
  });
  await plan.getByRole("button", { name: "Plan group stay" }).click();
  await expect(plan.getByRole("button", { name: "Retry confirmation" })).toBeVisible();
  await expect(plan.getByLabel("The provider's room type", { exact: false })).toHaveValue("10");
  await plan.getByRole("button", { name: "Retry confirmation" }).click();
  await expect(plan.getByRole("alert")).toContainText("before arrival");
  expect(keys).toHaveLength(2);
  expect(keys[1]).toBe(keys[0]);
  await expect(plan.getByLabel("First night", { exact: false })).toHaveValue("2030-01-10");
  await expect(plan.getByLabel("The provider's room type", { exact: false })).toHaveValue("10");
  await page.unroute("**/v1/submissions");
  await plan.getByRole("button", { name: "Cancel" }).click();
  await page.getByRole("button", { name: "Close opportunity" }).click();
  await page.getByRole("dialog").getByRole("combobox").first().selectOption("won");
  await page.getByRole("dialog").getByRole("button", { name: "Close opportunity" }).click();
  await expect(value(page, "won")).toBeVisible();
});

test("route 4: an approval reaches the requester's page", async ({ page, request }) => {
  const leave = fresh("LEA");
  const day = (d: number) => new Date(Date.now() + d * 86_400_000).toISOString().slice(0, 10);
  await open(page, "sales", "/definitions");
  await page.getByRole("textbox", { name: "Filter rows", exact: true }).fill("hcm/page/leaves");
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
  await managerPage.getByRole("region", { name: approvalID, exact: true }).getByRole("button", { name: "Back to inbox", exact: true }).click();
  await expect(value(page, "Approved")).toBeVisible();
  await managerPage.close();
});

test("route 28: preview cannot submit an action", async ({ page }) => {
  await open(page, "sales", "/definitions");
  await page.getByRole("textbox", { name: "Filter rows", exact: true }).fill("crm/page/opportunities");
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
