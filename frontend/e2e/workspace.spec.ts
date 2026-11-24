import { readFileSync } from "node:fs";
import { type Locator, expect, test } from "@playwright/test";
import type { Api } from "../src/host";
import { signIn, open, submit } from "./support";

test.describe.serial("workspace and admin", () => {
test("legacy links: open records and mapped views, and explain unavailable views", async ({ page }) => {
  await signIn(page);
  const session = await page.evaluate(() => JSON.parse(sessionStorage.getItem("platform:session")!) as { token: string; tenant: string });
  const response = await page.request.get("/v1/resources", {
    headers: { authorization: `Bearer ${session.token}`, "platform-tenant": session.tenant },
  });
  expect(response.ok()).toBeTruthy();
  const directory: Api.ResourceDirectory = await response.json();
  const resource = directory.resources.find((entry) => entry.type === "build.object" && entry.draft === "sales.opportunity");
  expect(resource?.id).toMatch(/^ri\./);

  await page.goto("/#/objects/sales.opportunity/OPP-001");
  await expect(page).toHaveURL("/ontology/objects/sales.opportunity/OPP-001");
  await expect(page.getByRole("heading", { level: 1, name: "冲压线改造" })).toBeVisible();

  await page.goto(`/#/object-type/${resource!.id}`);
  await expect(page).toHaveURL("/ontology/object-types/sales.opportunity");
  await expect(page.getByRole("heading", { level: 1, name: "Opportunity", exact: true })).toBeVisible();

  await page.goto("/#/dataset");
  await expect(page).toHaveURL("/data/datasets");
  await expect(page.getByRole("heading", { level: 1, name: "Datasets", exact: true })).toBeVisible();

  await page.goto("/#/apps");
  await expect(page).toHaveURL("/delivery/marketplace");
  await expect(page.getByRole("heading", { level: 1, name: "Marketplace", exact: true })).toBeVisible();

  await page.goto("/#/demo");
  await expect(page).toHaveURL("/home/overview");
  const notifications = page.getByRole("region", { name: "Notifications", exact: true });
  await expect(notifications.getByText("This legacy link has no corresponding page in the new frontend.", { exact: true })).toBeVisible();
});


test("mobile: the navigation drawer contains the application controls", async ({ page }) => {
  await page.setViewportSize({ width: 375, height: 812 });
  await signIn(page);
  await page.getByRole("button", { name: "Open navigation" }).click();
  const drawer = page.getByRole("dialog");
  await expect(drawer.getByRole("button", { name: "Search", exact: true })).toBeVisible();
  await expect(drawer.getByRole("button", { name: "Notifications", exact: true })).toBeVisible();
  await drawer.getByRole("button", { name: "Account", exact: true }).click();
  await page.getByRole("menuitem", { name: "Sign out", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Welcome back", exact: true })).toBeVisible();
});


test("restricted fields: a reader sees the field label and no amount", async ({ page }) => {
  await page.goto("/");
  await page.getByRole("button", { name: "user-1 · acceptance" }).click();
  await page.goto("/ontology/objects/sales.opportunity/OPP-001");
  await expect(page.getByRole("heading", { level: 1, name: "冲压线改造" })).toBeVisible();
  await expect(page.getByText("Amount", { exact: true })).toBeVisible();
  await expect(page.getByText("Cannot view", { exact: true })).toBeVisible();
  await expect(page.getByRole("spinbutton", { name: "Amount", exact: true })).toHaveCount(0);
  await expect(page.getByText("184,000", { exact: true })).toHaveCount(0);
});


test("member menu: language and appearance live in settings, sign out returns to sign-in", async ({ page }) => {
  await signIn(page, "builder-1 · acceptance");
  const settings = async (menu: string, item: string) => {
    await page.getByRole("button", { name: menu, exact: true }).click();
    await page.getByRole("menuitem", { name: item, exact: true }).click();
    const dialog = page.getByRole("dialog");
    await expect(dialog).toBeVisible();
    return dialog;
  };
  const close = async (dialog: Locator) => {
    await dialog.getByRole("button", { name: /^(Close|关闭)$/ }).first().click();
    await expect(dialog).toBeHidden();
  };

  let dialog = await settings("Account", "Settings");
  await dialog.getByRole("combobox", { name: "Language", exact: true }).click();
  await page.getByRole("option", { name: /中文/ }).click();
  await close(dialog);
  await expect(page.getByRole("button", { name: "搜索", exact: true }).first()).toBeVisible();
  await page.reload();
  await expect(page.getByRole("button", { name: "账户", exact: true })).toBeVisible();

  dialog = await settings("账户", "偏好");
  await dialog.getByRole("checkbox", { name: "深色", exact: true }).click();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");
  await close(dialog);
  await page.reload();
  await expect(page.locator("html")).toHaveAttribute("data-theme", "dark");

  dialog = await settings("账户", "偏好");
  await dialog.getByRole("checkbox", { name: "跟随系统", exact: true }).click();
  await close(dialog);
  dialog = await settings("账户", "设置");
  await dialog.getByRole("combobox", { name: "语言", exact: true }).click();
  await page.getByRole("option", { name: "租户默认", exact: true }).click();
  await close(dialog);
  await expect(page.getByRole("button", { name: "Account", exact: true })).toBeVisible();

  await page.getByRole("button", { name: "Account", exact: true }).click();
  await page.getByRole("menuitem", { name: "Sign out", exact: true }).click();
  await expect(page.getByRole("button", { name: "admin-1 · acceptance" })).toBeVisible();
});


test("vocabulary: a workspace translation wins over the shipped one and reverts", async ({ page }) => {
  await signIn(page);
  await open(page, "Admin", "Language and vocabulary");
  await page.getByRole("textbox", { name: "Search this list" }).fill("Opportunity");
  await page.getByRole("textbox", { name: "Search this list" }).press("Enter");
  const row = page.getByRole("row").filter({ hasText: "Types and fields" }).filter({ has: page.getByRole("cell", { name: "Opportunity", exact: true }) });
  await expect(row).toContainText("Missing");
  await row.getByRole("cell").nth(1).click();
  await row.getByRole("cell").nth(1).click();
  await row.getByRole("textbox").fill("商机");
  await row.getByRole("textbox").press("Enter");
  await expect(row).toContainText("商机");
  await expect(row).toContainText("Overridden");
  await expect(row).toContainText("This workspace");
  await row.getByRole("cell", { name: "Opportunity", exact: true }).click();
  await page.getByRole("button", { name: "Revert to the shipped translation", exact: true }).click();
  await page.getByRole("alertdialog").getByRole("button", { name: "Revert to the shipped translation", exact: true }).click();
  await expect(row).toContainText("Missing");

  const started = page.waitForEvent("download");
  await page.getByRole("button", { name: "More actions", exact: true }).click();
  await page.getByRole("menuitem", { name: "Export as CSV", exact: true }).click();
  const exported = readFileSync((await (await started).path())!, "utf8");
  expect(exported).toContain("Opportunity");
  expect(exported).not.toContain("Budget approved");

  await page.getByRole("button", { name: "More actions", exact: true }).click();
  await page.getByRole("menuitem", { name: "Import", exact: true }).click();
  const wizard = page.getByRole("dialog", { name: "Import · Language and vocabulary", exact: true });
  await wizard.locator('input[type="file"]').setInputFiles({
    name: "words.csv",
    mimeType: "text/csv",
    buffer: Buffer.from("Text,Translation\nOpportunity,销售商机\n,缺失原文\n", "utf8"),
  });
  await expect(wizard.getByText("2 records", { exact: true })).toBeVisible();
  await wizard.getByRole("button", { name: "Continue", exact: true }).click();
  await wizard.getByRole("button", { name: "Continue", exact: true }).click();
  await expect(wizard.getByText("1 can be imported · 1 need fixing", { exact: true })).toBeVisible();
  await wizard.getByRole("textbox", { name: "Text · Row 2", exact: true }).fill("Customer");
  await wizard.getByRole("button", { name: "Check", exact: true }).click();
  await expect(wizard.getByText("2 can be imported · 0 need fixing", { exact: true })).toBeVisible();
  await wizard.getByRole("button", { name: "Import 2", exact: true }).click();
  await expect(wizard.getByText("2 records imported", { exact: true })).toBeVisible();
  await wizard.getByRole("button", { name: "Done", exact: true }).click();
  await expect(wizard).toBeHidden();
  await expect(row).toContainText("销售商机");
  await expect(row).toContainText("Overridden");
  await row.getByRole("cell", { name: "Opportunity", exact: true }).click();
  await page.getByRole("button", { name: "Revert to the shipped translation", exact: true }).click();
  await page.getByRole("alertdialog").getByRole("button", { name: "Revert to the shipped translation", exact: true }).click();
  await expect(row).toContainText("Missing");
});


test("tenants: switching keeps the entry and replaces what the page shows", async ({ page }) => {
  await signIn(page);
  await open(page, "Ontology", "Object types");
  const opportunity = page.getByRole("row").filter({ hasText: "opportunityline" });
  await expect(opportunity).toBeVisible();
  const tenant = async (name: string) => {
    await page.getByRole("button", { name: "Account", exact: true }).click();
    await page.getByRole("group", { name: "Tenant" }).getByRole("menuitemradio", { name, exact: true }).click();
  };

  await tenant("publisher");
  await expect(page).toHaveURL(/\/ontology\/object-types$/);
  await expect(page.getByRole("heading", { level: 2, name: "Nothing here yet", exact: true })).toBeVisible();
  await expect(opportunity).toHaveCount(0);

  await tenant("acceptance");
  await expect(opportunity).toBeVisible();
});


test("sign-in: a work account signs in by password or single sign-on and lands where it was", async ({ page }) => {
  await page.route("**/v1/sign-in", (route) => route.fulfill({ json: { issuer: "http://localhost:8497/auth/v1/", client: "platform-web" } }));
  await page.route("**/auth/v1/oidc/token", (route) => {
    const form = new URLSearchParams(route.request().postData() ?? "");
    const refused = form.get("grant_type") === "password" ? form.get("password") !== "right" : !form.get("code_verifier") || form.get("code") !== "granted";
    return route.fulfill(refused ? { status: 400, json: { error: "invalid_grant" } } : { json: { access_token: "admin", token_type: "Bearer" } });
  });
  await page.route("**/auth/v1/oidc/authorize?*", (route) => {
    const asked = new URL(route.request().url()).searchParams;
    return route.fulfill({ status: 302, headers: { location: `${asked.get("redirect_uri")}?code=granted&state=${asked.get("state")}` } });
  });
  const objectTypes = page.getByRole("heading", { name: "Object types", exact: true });

  await page.goto("/ontology/object-types");
  await page.getByRole("textbox", { name: "Email" }).fill("admin@platform.test");
  await page.getByLabel("Password").fill("wrong");
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(page.getByText("The email or password is not right.").first()).toBeVisible();
  await page.getByLabel("Password").fill("right");
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  await expect(objectTypes).toBeVisible();
  await expect(page).toHaveURL(/\/ontology\/object-types$/);

  await page.getByRole("button", { name: "Account", exact: true }).click();
  await page.getByRole("menuitem", { name: "Sign out", exact: true }).click();
  await page.goto("/#/object-type");
  await page.getByRole("button", { name: "Continue with single sign-on", exact: true }).click();
  await expect(objectTypes).toBeVisible();
  await expect(page).toHaveURL(/\/ontology\/object-types$/);
});


test("settings: the profile is edited in place and the member menu follows", async ({ page }) => {
  await signIn(page, "user-1 · acceptance");
  const openSettings = async () => {
    await page.getByRole("button", { name: "Account", exact: true }).click();
    await page.getByRole("menuitem", { name: "Settings", exact: true }).click();
    return page.getByRole("dialog", { name: "Settings" });
  };
  let settings = await openSettings();
  const name = settings.getByRole("textbox", { name: "Display name", exact: true });
  const before = await name.inputValue();
  await name.fill("Dana W.");
  await name.press("Enter");
  await settings.getByRole("navigation", { name: "Settings sections" }).getByText("Login & security", { exact: true }).click();
  await expect(settings.getByText("This session", { exact: true })).toBeVisible();
  await expect(settings.getByRole("button", { name: "Sign out others", exact: true })).toBeDisabled();
  await settings.getByRole("button", { name: "Close", exact: true }).click();

  await page.getByRole("button", { name: "Account", exact: true }).click();
  await expect(page.getByRole("menuitem", { name: /Dana W\./ })).toBeVisible();
  await page.keyboard.press("Escape");

  settings = await openSettings();
  await settings.getByRole("textbox", { name: "Display name", exact: true }).fill(before);
  await settings.getByRole("textbox", { name: "Display name", exact: true }).press("Enter");
  await settings.getByRole("button", { name: "Close", exact: true }).click();
  await page.getByRole("button", { name: "Account", exact: true }).click();
  await expect(page.getByRole("menuitem", { name: new RegExp(before || "user-1") })).toBeVisible();
});


test("entries: record types open as lists and dropped entries are gone from the menu", async ({ page }) => {
  await signIn(page);
  for (const [app, entry] of [["Logic", "Automations"], ["Apps", "Object views"], ["AI", "Evaluations"]]) {
    await open(page, app, entry);
    await expect(page.getByRole("heading", { level: 1, name: entry, exact: true })).toBeVisible();
  }
  const menu = page.getByRole("navigation", { name: "Primary navigation" });
  await page.getByRole("link", { name: "Home" }).click();
  await expect(menu.getByRole("link", { name: "Recent", exact: true })).toHaveCount(0);
  await page.getByRole("link", { name: "Apps" }).click();
  await expect(menu.getByRole("link", { name: "Templates", exact: true })).toHaveCount(0);
});


test("reads: each read list opens, the audit loads its next page, and a marking is created and archived", async ({ page }) => {
  await signIn(page);
  const session = await page.evaluate(() => JSON.parse(sessionStorage.getItem("platform:session")!) as { token: string; tenant: string });
  const headers = { authorization: `Bearer ${session.token}`, "platform-tenant": session.tenant };
  for (let index = 0; index < 120; index += 1) expect((await page.request.get("/v1/audit?limit=1", { headers })).ok()).toBeTruthy();

  const main = page.getByRole("main");
  for (const [app, entry] of [
    ["Home", "Proposals"], ["Operations", "Runs"], ["Operations", "Logs"], ["Operations", "Alerts"], ["Operations", "Audit"],
    ["Admin", "Teams"], ["Admin", "Business roles"], ["Admin", "Markings"], ["Admin", "Credentials"], ["Admin", "Models"],
    ["Admin", "Support grants"], ["Admin", "Language and vocabulary"], ["Data", "Lineage"], ["AI", "Knowledge"],
  ]) {
    await open(page, app, entry);
    await expect(page.getByRole("heading", { level: 1, name: entry, exact: true })).toBeVisible();
    await expect(main.getByRole("table").or(main.getByText("Nothing here yet")).first()).toBeVisible();
  }

  await page.getByRole("textbox", { name: "Search this list" }).fill("retrofit");
  await page.getByRole("textbox", { name: "Search this list" }).press("Enter");
  await expect(main.getByRole("table").or(main.getByText("No matches")).first()).toBeVisible();

  await open(page, "Operations", "Audit");
  await expect(page.getByText("100+", { exact: true })).toBeVisible();
  await page.getByRole("region", { name: "Records" }).evaluate((node) => node.scrollTo(0, node.scrollHeight));
  await expect(page.getByText("100+", { exact: true })).toHaveCount(0);
  await expect.poll(async () => Number.parseInt((await page.getByRole("group", { name: "Filter", exact: true }).getByText(/^\d+\+?$/).textContent()) ?? "", 10)).toBeGreaterThan(100);

  await open(page, "Admin", "Markings");
  await page.getByRole("button", { name: "New", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByRole("textbox", { name: "title" }).fill("Trunk marking");
  await dialog.getByRole("button", { name: "Create", exact: true }).click();
  await expect(dialog).toHaveCount(0);
  await main.getByRole("row", { name: /Trunk marking/ }).click();
  await page.getByRole("dialog", { name: "Details" }).getByRole("button", { name: "Archive marking", exact: true }).click();
  await page.getByRole("alertdialog").getByRole("button", { name: "Archive marking", exact: true }).click();
  await expect(main.getByRole("row", { name: /Trunk marking.*Archived/ })).toBeVisible();
});


test("teams: create a team with two members, open it and remove it", async ({ page }) => {
  await signIn(page);
  await open(page, "Admin", "Teams");
  await page.getByRole("button", { name: "New", exact: true }).click();
  const form = page.getByRole("dialog");
  await form.getByRole("textbox", { name: /^Name/ }).fill("Trunk review team");
  const members = form.getByRole("button", { name: /^Members/ });
  await members.click();
  const options = page.getByRole("listbox");
  await options.getByRole("option", { name: "韩奥", exact: true }).click();
  await options.getByRole("option", { name: "田雪", exact: true }).click();
  await page.keyboard.press("Escape");
  await form.getByRole("button", { name: "Create", exact: true }).click();
  await expect(form).toBeHidden();

  const team = page.getByRole("row", { name: /Trunk review team/ });
  await team.click();
  const details = page.getByRole("dialog", { name: "Details" });
  await expect(details.getByRole("heading", { name: "Trunk review team" })).toBeVisible();
  await details.getByRole("button", { name: "Edit", exact: true }).click();
  await expect(members).toContainText("韩奥");
  await expect(members).toContainText("田雪");
  await form.getByRole("button", { name: "Cancel", exact: true }).click();
  await details.getByRole("button", { name: "Remove team", exact: true }).click();
  await page.getByRole("alertdialog").getByRole("button", { name: "Remove team", exact: true }).click();
  await expect(team).toHaveCount(0);
});


test("trees: the organization rolls people up its units and a trace opens to its steps", async ({ page }) => {
  await signIn(page);
  const main = page.getByRole("main");
  await open(page, "Admin", "Organization");
  await expect(main.getByRole("row").filter({ hasText: "Acme Industries" })).toContainText("3");
  const sales = main.getByRole("row").filter({ hasText: /^Sales\s*department/ });
  await expect(sales).toContainText("2");
  await expect(main.getByRole("row").filter({ hasText: "Sales North" })).toHaveCount(0);
  await sales.click();
  await page.keyboard.press("ArrowRight");
  await expect(main.getByRole("row").filter({ hasText: "Sales North" })).toContainText("1");
  await page.getByRole("radio", { name: "Sites" }).click();
  await expect(main.getByRole("row").filter({ hasText: /^Operations/ })).toContainText("1");

  await open(page, "Operations", "Traces");
  const traces = main.getByRole("row").filter({ hasText: /^GET / });
  await expect(traces.first()).toBeVisible();
  const before = await main.getByRole("row").count();
  await traces.first().click();
  await page.keyboard.press("ArrowRight");
  await expect.poll(() => main.getByRole("row").count()).toBeGreaterThan(before);
  await expect(main.getByRole("row").filter({ hasText: "Request completed" }).first()).toBeVisible();
});


test("dashboards: metrics and health show their blocks", async ({ page }) => {
  await signIn(page);
  const main = page.getByRole("main");

  await open(page, "Operations", "Metrics");
  await expect(main.getByRole("heading", { level: 1, name: "Metrics", exact: true })).toBeVisible();
  await expect(main.getByRole("heading", { name: "Requests", exact: true })).toBeVisible();

  await open(page, "Operations", "Health");
  await expect(main.getByRole("heading", { level: 1, name: "Health", exact: true })).toBeVisible();
  await expect(main.getByRole("heading", { name: "Failed work", exact: true })).toBeVisible();
  await expect(main.getByRole("heading", { name: "Readiness", exact: true })).toBeVisible();
});


test("settings: an app's setting changes in place and survives a reload", async ({ page }) => {
  await signIn(page);
  await open(page, "Admin", "Settings");
  await expect(page.getByRole("heading", { level: 1, name: "Settings", exact: true })).toBeVisible();
  await page.getByRole("main").getByRole("button", { name: "Agent Studio", exact: true }).click();
  await expect(page.getByRole("heading", { level: 2, name: "Agent Studio", exact: true })).toBeVisible();
  const days = page.getByRole("textbox", { name: "Days transcripts are kept" });
  const saved = () => page.waitForResponse((response) => response.url().endsWith("/v1/submissions"));
  await expect(days).not.toHaveValue("");
  const before = await days.inputValue();
  await days.fill("45");
  await Promise.all([saved(), days.press("Enter")]);
  await page.reload();
  await page.getByRole("main").getByRole("button", { name: "Agent Studio", exact: true }).click();
  await expect(days).toHaveValue("45");
  await days.fill(before);
  await Promise.all([saved(), days.press("Enter")]);
});


test("menu: every entry in every app opens a page with its own title", async ({ page }) => {
  await signIn(page);
  const menu = page.getByRole("navigation", { name: "Primary navigation" });
  for (const app of ["Home", "Data", "Ontology", "Logic", "Apps", "AI", "Delivery", "Operations", "Admin", "Host"]) {
    await page.getByRole("link", { name: app, exact: true }).click();
    for (const name of await menu.getByRole("link").allInnerTexts()) {
      await menu.getByRole("link", { name: name.trim(), exact: true }).click();
      await expect(page.getByRole("heading", { level: 1, name: name.trim(), exact: true })).toBeVisible();
    }
  }
});

});


test("people: a person gets a sign-in account and shows what an application attaches to them", async ({ page }) => {
  await signIn(page);
  const suffix = Date.now().toString(36);
  const person = `Trunk Person ${suffix}`;
  const object = `profile${suffix}`;
  await submit(page, "enterprise.element.add", "enterprise.element", `person-${suffix}`, { stereotype: "ActualPerson", name: person });
  await submit(page, "build.object.create", "build.object", `PROFILE-${suffix}`, {
    project: "sales", name: object, title: "Staff profile", plural: "Staff profiles", extends: "enterprise.element", display: "skill",
    fields: [
      { name: "base", title: "Person", type: "reference", ref: "enterprise.element", stereotype: "ActualPerson", required: true },
      { name: "skill", title: "Main skill", type: "text", search: true },
    ],
    access: [{ role: "user", read: "all", create: true, edit: true }],
  });
  await submit(page, "build.object.publish", "build.object", `PROFILE-${suffix}`, {});
  await submit(page, `sales.${object}.create`, `sales.${object}`, "P1", { base: `person-${suffix}`, skill: "Welding" });

  const main = page.getByRole("main");
  await open(page, "Admin", "People");
  await main.getByRole("row").filter({ hasText: person }).getByRole("link", { name: person }).click();
  await expect(page.getByRole("heading", { level: 1, name: person })).toBeVisible();
  await expect(main.getByRole("heading", { name: "Staff profile" })).toBeVisible();
  await expect(main.getByText("Welding", { exact: true })).toBeVisible();

  await main.getByRole("combobox", { name: "Account that signs in as this person" }).click();
  await page.getByRole("option", { name: /系统管理员/ }).click();
  await main.getByRole("button", { name: "Link account", exact: true }).click();
  await expect(main.getByRole("button", { name: "Unlink account", exact: true })).toBeVisible();

  await open(page, "Admin", "People");
  await expect(main.getByRole("row").filter({ hasText: person })).toContainText("系统管理员");
});
