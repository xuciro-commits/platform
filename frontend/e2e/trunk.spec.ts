import { type Page, expect, test } from "@playwright/test";

async function signIn(page: Page) {
  await page.goto("/");
  await page.getByRole("button", { name: "admin-1 · acceptance" }).click();
}

async function open(page: Page, app: string, entry: string) {
  await page.getByRole("link", { name: app }).click();
  await page.getByRole("navigation", { name: "Primary navigation" }).getByRole("link", { name: entry }).click();
}

test("records: open a list, open a record, change a field in place", async ({ page }) => {
  await signIn(page);
  await open(page, "Ontology", "Objects");
  await page.getByRole("link", { name: "Opportunities" }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Opportunities" })).toBeVisible();
  const retrofit = page.getByRole("row", { name: /Press line retrofit/ });
  await expect(retrofit.getByText("Negotiation", { exact: true })).toBeVisible();
  await expect(retrofit.getByText("184,000", { exact: true })).toBeVisible();

  await page.getByRole("link", { name: "Press line retrofit" }).click();
  await expect(page.getByRole("combobox", { name: "Stage" })).toContainText("Negotiation");
  await expect(page.getByRole("heading", { level: 1, name: "Press line retrofit" })).toBeVisible();

  await expect(page.getByRole("heading", { level: 3, name: "Commercial" })).toBeVisible();
  await expect(page.getByRole("heading", { level: 3, name: "Follow-up" })).toBeVisible();
  await expect(page.getByRole("textbox", { name: "Loss reason" })).toHaveCount(0);
  const owner = page.getByRole("complementary").getByRole("textbox", { name: "Owner" });
  await expect(owner).toHaveValue("Dana Whitfield");

  const next = page.getByRole("textbox", { name: "Next step" });
  await expect(next).toHaveValue("Send revised terms");
  await next.fill("Call the plant manager");
  await next.press("Enter");
  await expect(page.getByRole("complementary").getByText("2", { exact: true })).toBeVisible();

  await page.reload();
  await expect(page.getByRole("textbox", { name: "Next step" })).toHaveValue("Call the plant manager");

  await page.getByRole("complementary").getByRole("textbox", { name: "Owner" }).fill("Omar Haddad");
  await page.getByRole("complementary").getByRole("textbox", { name: "Owner" }).press("Enter");
  await expect(page.getByRole("complementary").getByText("3", { exact: true })).toBeVisible();
  await page.reload();
  await expect(page.getByRole("complementary").getByRole("textbox", { name: "Owner" })).toHaveValue("Omar Haddad");
});

test("sections: a field shows only when its condition holds", async ({ page }) => {
  await signIn(page);
  await open(page, "Ontology", "Objects");
  await page.getByRole("link", { name: "Opportunities" }).click();
  await page.getByRole("link", { name: "Fleet tracking rollout" }).click();
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
  await page.getByRole("link", { name: "Warehouse scanners" }).click();
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

test("publish: a draft type goes live from its record page and takes records", async ({ page }) => {
  await signIn(page);
  await open(page, "Ontology", "Object types");
  await page.getByRole("link", { name: "Contact", exact: true }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Contact" })).toBeVisible();
  await expect(page.getByText("Draft", { exact: true })).toBeVisible();

  await page.getByRole("button", { name: "Publish" }).click();
  const confirm = page.getByRole("alertdialog");
  await expect(confirm).toContainText("Contact");
  await confirm.getByRole("button", { name: "Publish" }).click();
  await expect(page.getByText("Published", { exact: true })).toBeVisible();

  await page.getByRole("link", { name: "Open records" }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Contacts" })).toBeVisible();

  await page.getByRole("button", { name: "New" }).click();
  const form = page.getByRole("dialog");
  await expect(form.getByRole("textbox")).toHaveCount(1);
  await form.getByRole("textbox", { name: "Full name" }).fill("Ines Brandt");
  await form.getByRole("button", { name: "Create" }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Ines Brandt" })).toBeVisible();
  await expect(page.getByRole("textbox", { name: "Customer" })).toBeFocused();
});
