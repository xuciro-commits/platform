import { type Page, expect, test } from "@playwright/test";
import { signIn, property, edit, submit } from "./support";

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
  await expect(page.getByRole("link", { name: "Northwind Regional CRM Expansion", exact: true })).toBeVisible();
  const activity = await submit(page, "crm.activity.create", "crm.activity", "acceptance-activity", { subject: "Linked follow-up", lead: "demo-lead-1" }, 0);
  await page.goto(`/ontology/objects/crm.activity/${activity.record.submission.target.id}`);
  await expect(page.getByRole("link", { name: "Zhao Ming", exact: true })).toBeVisible();
  await page.getByRole("radio", { name: "Deal", exact: true }).click();
  await page.getByRole("combobox", { name: "Deal", exact: true }).fill("Northwind Regional CRM Expansion");
  await Promise.all([
    page.waitForResponse((response) => response.url().endsWith("/v1/submissions") && response.ok()),
    page.getByRole("option", { name: "Northwind Regional CRM Expansion", exact: true }).click(),
  ]);
  await page.reload();
  await expect(page.getByRole("link", { name: "Northwind Regional CRM Expansion", exact: true })).toBeVisible();
  await expect(page.getByRole("link", { name: "Zhao Ming", exact: true })).toHaveCount(0);
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
});
