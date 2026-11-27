import { expect, test, type Page } from "@playwright/test";
import { signIn, open, property, metadata, submit } from "./support";

test.describe.serial("package lifecycle", () => {
test("delivery: install a contributed app and run its state transition", async ({ page }) => {
  await signIn(page);
  await open(page, "Delivery", "Marketplace");
  await expect(page.getByRole("listitem").filter({ hasText: "application.production-provider · 0.1.5" })).toBeVisible();
  const provider = page.getByRole("listitem").filter({ hasText: "application.production-provider · 0.1.5" });
  await provider.getByRole("button", { name: "Install", exact: true }).click();
  const review = page.getByRole("dialog", { name: /Review and install/ });
  await expect(review).toContainText("application.production-provider");
  const install = review.getByRole("button", { name: "Install package", exact: true });
  await expect(install).toBeEnabled({ timeout: 30_000 });
  await install.click();
  await expect(page).toHaveURL(/\/delivery\/installations$/, { timeout: 30_000 });
  const installed = page.getByRole("listitem").filter({ hasText: "Production batch provider" });
  await expect(installed).toContainText("0.1.5");

  await page.reload();
  await page.getByRole("link", { name: "Production order provider", exact: true }).click();
  await page.getByRole("navigation", { name: "Primary navigation" }).getByRole("link", { name: "Production orders", exact: true }).click();
  await expect(page.getByRole("heading", { level: 1, name: "Production orders", exact: true })).toBeVisible();
  await page.getByRole("button", { name: "New", exact: true }).click();
  const order = page.getByRole("dialog");
  await order.getByRole("textbox", { name: /^Order number/ }).fill("GATE-1");
  await order.getByRole("textbox", { name: /^Product/ }).fill("Gate valve");
  await order.getByRole("spinbutton", { name: /^Ordered quantity/ }).fill("12");
  await order.getByRole("spinbutton", { name: /^Unit production cost/ }).fill("40");
  await page.keyboard.press("Tab");
  await order.getByRole("button", { name: "Create", exact: true }).click();
  await expect(page.getByRole("heading", { level: 1, name: "GATE-1", exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Start upstream production", exact: true }).click();
  const contributed = page.getByRole("dialog", { name: "Start upstream production", exact: true });
  await contributed.getByRole("textbox", { name: /^Shop order/ }).fill("WO-GATE-1");
  await contributed.getByRole("button", { name: "Start upstream production", exact: true }).click();
  await expect(contributed).toBeHidden();
  await expect(page.getByText("Running", { exact: true }).first()).toBeVisible();
});

test("delivery: shared records respect roles, survive retirement and reinstall", async ({ page, browser }) => {
  await signIn(page);
  await submit(page, "build.batchproviderorder.create", "build.batchproviderorder", "PO-1042", { number: "PO-1042", product: "Precision gearbox", quantity: 25, unitcost: 180 }, 0);
  await submit(page, "build.batchproviderorder.setshoporder", "build.batchproviderorder", "PO-1042", { shoporder: "WO-1042" }, 1);
  await page.goto("/productionprovider/batchproviderorder");
  const entry = page.getByRole("link", { name: "PO-1042", exact: true });
  const appRecord = new URL((await entry.getAttribute("href"))!, page.url());
  const recordURL = `/ontology/objects/build.batchproviderorder/${appRecord.pathname.split("/").at(-1)}`;
  const schema = "build.batchproviderorder.setshoporder";
  const member = await browser.newPage();
  await signIn(member, "builder-1 · acceptance");
  await member.goto(appRecord.href);
  await expect(member.getByRole("heading", { name: "PO-1042", exact: true })).toBeVisible();
  await expect(property(member, "First shop order")).toHaveText("WO-1042");
  await member.getByRole("button", { name: "More actions" }).click();
  await member.getByRole("menuitem", { name: "Set shop order", exact: true }).click();
  const action = member.getByRole("dialog", { name: "Set shop order", exact: true });
  await action.getByRole("textbox", { name: /^Shop order/ }).fill("WO-1043");
  const submitted = member.waitForRequest((request) => request.url().endsWith("/v1/submissions") && request.postDataJSON().schema.name === schema);
  await action.getByRole("button", { name: "Set shop order", exact: true }).click();
  const sent = await submitted;
  await expect(action).toBeHidden();
  await expect(property(member, "First shop order")).toHaveText("WO-1043");
  const historyURL = await member.getByRole("link", { name: "Activity", exact: true }).getAttribute("href");
  expect(new URL(historyURL!, member.url()).pathname).toBe(`${appRecord.pathname}/activity`);

  const direct = (actor: Page) => actor.evaluate(async ({ submission, version }) => {
    const session = JSON.parse(sessionStorage.getItem("platform:session")!);
    const headers = { authorization: `Bearer ${session.token}`, "platform-tenant": session.tenant };
    const identity = await (await fetch("/v1/me", { headers })).json();
    const response = await fetch("/v1/submissions", {
      method: "POST",
      headers: { ...headers, "content-type": "application/json", "platform-schema-version": version },
      body: JSON.stringify({
        tenantId: identity.tenantId,
        principalId: identity.principalId,
        authority: submission.authority,
        target: submission.target,
        schema: submission.schema,
        idempotencyKey: crypto.randomUUID(),
        payload: btoa(JSON.stringify({ shoporder: "WO-DENIED" })),
      }),
    });
    return (await response.json()).error as { code: string; message: string };
  }, { submission: sent.postDataJSON(), version: sent.headers()["platform-schema-version"] });

  await member.getByRole("button", { name: "Account", exact: true }).click();
  await member.getByRole("menuitem", { name: "Sign out", exact: true }).click();
  await signIn(member, "user-1 · acceptance");
  await member.goto(recordURL);
  await expect(member.getByRole("heading", { name: "PO-1042", exact: true })).toBeVisible();
  await expect(member.getByText("WO-1043", { exact: true })).toBeVisible();
  await expect(member.getByRole("button", { name: "Start upstream production", exact: true })).toBeEnabled();
  const menu = member.getByRole("button", { name: "More actions" });
  if (await menu.isVisible()) await menu.click();
  await expect(member.getByRole("menuitem", { name: "Set shop order", exact: true })).toHaveCount(0);
  expect((await direct(member)).code).toBe("ERROR_CODE_POLICY_DENIED");

  await open(page, "Delivery", "Installations");
  const installation = page.getByRole("listitem").filter({ hasText: "Production batch provider" });
  const manage = installation.getByRole("button", { name: "Manage installation", exact: true });
  await manage.click();
  const review = page.getByRole("dialog", { name: /Manage installation/ });
  const retained = metadata(review, "Business records retained");
  await expect(retained).toHaveText(/^[1-9]\d*$/);
  const kept = await retained.innerText();
  const markUninstall = review.getByRole("button", { name: "Mark for uninstall", exact: true });
  await expect(markUninstall).toBeEnabled({ timeout: 30_000 });
  await markUninstall.click();
  await expect(review).toBeHidden({ timeout: 30_000 });
  await expect(installation).toContainText("draining");
  await manage.click();
  const retireBtn = review.getByRole("button", { name: "Retire package", exact: true });
  await expect(retireBtn).toBeEnabled({ timeout: 30_000 });
  await retireBtn.click();
  await expect(review).toBeHidden({ timeout: 30_000 });
  await expect(installation).toContainText("retired");
  await expect(page.getByRole("link", { name: "Production order provider", exact: true })).toHaveCount(0);
  expect(await direct(page)).toMatchObject({ code: "ERROR_CODE_POLICY_DENIED", message: expect.stringContaining("The package contribution is retired") });
  await manage.click();
  await expect(retained).toHaveText(kept);
  await page.goto(appRecord.href);
  await expect(property(page, "First shop order")).toHaveText("WO-1043");
  const retiredMenu = page.getByRole("button", { name: "More actions" });
  if (await retiredMenu.isVisible()) await retiredMenu.click();
  await expect(page.getByRole("menuitem", { name: "Set shop order", exact: true })).toHaveCount(0);

  await member.goto(appRecord.href);
  await expect(member).toHaveURL((url) => url.pathname === recordURL);
  await expect(member.getByText("WO-1043", { exact: true })).toBeVisible();
  await expect(member.getByRole("button", { name: "Start upstream production", exact: true })).toHaveCount(0);
  if (await menu.isVisible()) await menu.click();
  await expect(member.getByRole("menuitem", { name: "Start upstream production", exact: true })).toHaveCount(0);
  await expect(member.getByRole("link", { name: "Production order provider", exact: true })).toHaveCount(0);
  await member.goto(historyURL!);
  await expect(member).toHaveURL((url) => url.pathname === `${recordURL}/activity`);
  await expect(member.getByRole("listitem", { name: /First shop order: .*WO-1043/ })).toBeVisible();
  await member.close();

  await page.goto("/delivery/installations");
  await installation.getByRole("button", { name: "Reinstall", exact: true }).click();
  const reinstall = page.getByRole("dialog", { name: /Review and install/ });
  await expect(reinstall.getByText("Installation scope", { exact: true })).toBeVisible();
  const reinstallBtn = reinstall.getByRole("button", { name: "Reinstall package", exact: true });
  await expect(reinstallBtn).toBeEnabled({ timeout: 30_000 });
  await reinstallBtn.click();
  await expect(reinstall).toBeHidden({ timeout: 60_000 });
  await expect(installation).toContainText("0.1.5");
  await expect(page.getByRole("link", { name: "Production order provider", exact: true })).toBeVisible();
  await page.goto(appRecord.href);
  await expect(property(page, "First shop order")).toHaveText("WO-1043");
  await page.getByRole("button", { name: "More actions" }).click();
  await page.getByRole("menuitem", { name: "Set shop order", exact: true }).click();
  const after = page.getByRole("dialog", { name: "Set shop order", exact: true });
  await after.getByRole("textbox", { name: /^Shop order/ }).fill("WO-1044");
  await after.getByRole("button", { name: "Set shop order", exact: true }).click();
  await expect(after).toBeHidden();
  await page.reload();
  await expect(property(page, "First shop order")).toHaveText("WO-1044");
});

test("delivery: a builder publishes a version and installs beside its source project under another name", async ({ page }) => {
  await signIn(page);
  await submit(page, "build.app.create", "build.app", "releaseexample", { name: "releaseexample", title: "Release example" }, 0);
  await submit(page, "build.object.create", "build.object", "releaseexample.item", { name: "item", project: "releaseexample", title: "Release item", plural: "Release items", display: "title", fields: [{ name: "title", title: "Title", type: "text", required: true, search: true }] }, 0);
  await submit(page, "build.object.publish", "build.object", "releaseexample.item", {}, 1);
  await submit(page, "build.app.edit", "build.app", "releaseexample", { pages: ["releaseexample.item"], groups: [{ title: "Workspace", pages: ["releaseexample.item"] }] }, 1);
  await submit(page, "build.app.publish", "build.app", "releaseexample", {}, 2);
  await submit(page, "releaseexample.item.create", "releaseexample.item", "source-item", { title: "Source only" }, 0);
  await page.goto("/delivery/versions");
  await page.getByRole("button", { name: "New", exact: true }).first().click();
  const release = page.getByRole("dialog", { name: "Publish product version", exact: true });
  await release.getByRole("combobox", { name: "Product" }).click();
  await page.getByRole("option", { name: "Release example · Create product", exact: true }).click();
  await expect(release.getByRole("textbox", { name: "Version" })).toHaveValue("0.1.0");
  await release.getByRole("button", { name: "Publish product version", exact: true }).click();
  await expect(page).toHaveURL(/releaseexample%400\.1\.0$/);
  await page.goto("/delivery/marketplace");
  await page.getByRole("listitem").filter({ hasText: "releaseexample · 0.1.0" }).getByRole("button", { name: "Install", exact: true }).click();
  const review = page.getByRole("dialog", { name: /Review and install/ });
  const name = review.getByRole("textbox", { name: "Project API name" });
  await expect(name).toHaveValue("releaseexample");
  const install = review.getByRole("button", { name: "Install package", exact: true });
  await expect(install).toBeEnabled({ timeout: 30_000 });
  await install.click();
  await expect(review.getByText("A project named releaseexample already exists; install under another name")).toBeVisible();
  await name.fill("releasecopy");
  await expect(install).toBeEnabled({ timeout: 30_000 });
  await install.click();
  await expect(review).toBeHidden({ timeout: 60_000 });
  const access = page.getByRole("dialog", { name: "Who can use it" });
  await expect(access.getByRole("combobox", { name: "Role in Release example", exact: true })).toBeVisible();
  await access.getByRole("button", { name: "Grant access", exact: true }).click();
  await expect(access).toBeHidden();
  await page.goto("/ontology/objects/releasecopy.item");
  await expect(page.getByRole("heading", { name: "Release items", exact: true })).toBeVisible();
  await expect(page.getByText("Source only", { exact: true })).toHaveCount(0);
  await page.goto("/ontology/objects/releaseexample.item");
  await expect(page.getByText("Source only", { exact: true })).toBeVisible();
});
});
