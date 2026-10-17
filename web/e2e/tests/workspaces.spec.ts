import { readFile } from "node:fs/promises";
import { expect, fresh, pageUIProfile, shots, test } from "./kit";
import { switchWorkspace } from "./host";

test("task workspaces preserve native PMS and hand off a controlled receiving application", async ({ browser, page, builder, operator, editor }, testInfo) => {
  test.setTimeout(120_000);
  const suffix = fresh("workspace").replace(/[^a-z0-9]/gi, "").toLowerCase();
  const appID = fresh("APP"), flowID = fresh("FLOW");
  const objectName = `receipt${suffix}`, pageName = `receiving${suffix}`, appName = `wms${suffix}`, flowName = `routing${suffix}`;
  const object = await builder.object({
    name: objectName, title: "Receipt", plural: "Receipts",
    fields: [{ name: "number", title: "Receipt number", type: "text", required: true, search: true }],
    access: [{ role: "user", read: "all", create: true, edit: true }],
  });
  const receiving = await builder.page({
    name: pageName, title: "Receiving workspace", object: object.type,
    sections: [{ id: "receipts", widget: "table", configVersion: 1, fields: ["number"] }],
    document: { formatVersion: 2, uiProfile: pageUIProfile, root: "root", nodes: {
      root: { kind: "rows", children: ["receipts"] }, receipts: { kind: "widget", section: "receipts" },
    } },
  });
  await builder.decide("build.page.publish", { type: "build.page", id: receiving.id }, {});
  await builder.decide("build.process.create", { type: "build.process", id: flowID }, {
    name: flowName, title: "Receiving process", manual: true, steps: [{ name: "done", kind: "end" }],
  });
  await builder.decide("build.process.publish", { type: "build.process", id: flowID }, {});
  await builder.decide("build.app.create", { type: "build.app", id: appID }, {
    uiProfile: pageUIProfile, header: { variant: "horizontal", title: "WMS", logo: "wms", items: [{ kind: "logo" }, { kind: "title" }] },
    name: appName, title: "WMS entry probe", icon: "clipboard", pages: [pageName],
    resources: [{ app: "build", kind: "flow", name: `build.${flowName}` }],
  });

  // Header edits retain the existing descriptor: removing a logo clears its
  // URL; disabling the header sends an explicit null to its original owner.
  await builder.open(page, `/module?id=${appID}`);
  await page.getByRole("tab", { name: "Header", exact: true }).click();
  const header = page.getByRole("group", { name: "Application header", exact: true });
  await header.locator("div").filter({ has: page.locator("strong").filter({ hasText: /^logo$/ }) }).first().getByRole("button", { name: "Remove", exact: true }).click();
  const savedHeader = async () => (await builder.record("build.app", appID)).header;
  await expect.poll(async () => (await savedHeader())?.logo ?? "").toBe("");
  await header.getByRole("checkbox", { name: "Show application header", exact: true }).uncheck();
  await expect.poll(savedHeader).toBeUndefined();

  // Rejection returns to the original navigation, preserving each diagnostic.
  await builder.decide("build.app.edit", { type: "build.app", id: appID }, {
    groups: [{ title: "Empty receiving group", pages: [] }, { title: "Receiving", pages: [pageName] }, { title: "Receiving", pages: [pageName] }],
  });
  await page.getByRole("button", { name: "Publish", exact: true }).click();
  await page.getByRole("button", { name: "Check draft and dependencies", exact: true }).click();
  await expect(page.getByRole("alert").filter({ hasText: "holds no page" })).toBeVisible();
  await page.getByRole("button", { name: "Open the module's navigation", exact: true }).click();
  await expect(page.getByRole("tab", { name: "Navigation", exact: true })).toHaveAttribute("aria-selected", "true");
  await page.getByRole("button", { name: "Toggle dock", exact: true }).click();
  await expect(page.getByRole("button", { name: "The group “Empty receiving group” holds no page. Put a page under it or remove the group.", exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: `The page “${pageName}” is under two groups: “Receiving” and “Receiving”.`, exact: true })).toBeVisible();
  for (let i = 0; i < 3; i++) await page.getByRole("button", { name: "Remove group", exact: true }).first().click();
  await expect.poll(async () => (await builder.record("build.app", appID)).groups ?? []).toEqual([]);

  // Native PMS and Knowledge remain task-owned, not builder-only aliases.
  await builder.open(page, "/home");
  await expect(page.getByRole("button", { name: /^PMS\b/ }).first()).toBeVisible();
  await expect(page.getByRole("button", { name: "Platform Catalog", exact: true })).toHaveCount(0);
  await page.getByRole("button", { name: /^PMS\b/ }).first().click();
  await expect(page.getByRole("heading", { name: "Reservations", exact: true })).toBeVisible();
  await switchWorkspace(page, "Knowledge");
  await expect(page.getByRole("heading", { name: "Documents", exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "Create document", exact: true })).toHaveCount(1);
  await expect(page.getByRole("button", { name: "New document", exact: true })).toHaveCount(0);
  await page.getByRole("button", { name: "Create document", exact: true }).click();
  const document = page.getByRole("dialog", { name: "Create document", exact: true });
  const documentID = fresh("DOCUMENT"), documentTitle = `Receiving instructions ${suffix}`;
  await document.getByRole("textbox", { name: "ID *", exact: true }).fill(documentID);
  await document.getByRole("textbox", { name: "Title *", exact: true }).fill(documentTitle);
  await document.getByRole("textbox", { name: "Text *", exact: true }).fill("Check the receipt number before accepting delivery.");
  await document.getByRole("textbox", { name: "Where it comes from", exact: true }).fill("Receiving desk");
  await document.getByRole("textbox", { name: "Read by members of", exact: true }).fill("pms");
  await document.getByRole("textbox", { name: "Read by members of", exact: true }).press("Enter");

  // Losing an accepted reply retains this document's original request.
  const documentRequests: unknown[] = [];
  await page.route("**/v1/submissions", async route => {
    const body = route.request().postDataJSON();
    if (body.schema?.name !== "knowledge.document.create") { await route.continue(); return; }
    documentRequests.push(body);
    if (documentRequests.length === 1) {
      const response = await route.fetch();
      expect(response.ok()).toBeTruthy();
      await route.abort("connectionreset");
      return;
    }
    await route.continue();
  });
  await document.getByRole("button", { name: "Create", exact: true }).click();
  await expect(document.getByRole("button", { name: "Retry confirmation", exact: true })).toBeVisible();
  expect(await builder.record("knowledge.document", documentID)).toMatchObject({ title: documentTitle, text: "Check the receipt number before accepting delivery.", source: "Receiving desk", apps: ["pms"] });
  await expect(document.getByRole("textbox", { name: "Title *", exact: true })).toHaveValue(documentTitle);
  await expect(document.getByRole("textbox", { name: "Title *", exact: true })).toBeDisabled();
  await document.getByRole("button", { name: "Retry confirmation", exact: true }).click();
  await expect(document).toHaveCount(0);
  expect(documentRequests).toHaveLength(2);
  expect(documentRequests[1]).toEqual(documentRequests[0]);
  expect((await builder.record("knowledge.document", documentID)).revision).toBe(1);
  await page.unroute("**/v1/submissions");
  await expect(page.getByRole("row").filter({ hasText: documentTitle })).toBeVisible();

  const knowledgeToggle = page.getByRole("button", { name: "Toggle navigation", exact: true });
  if (await knowledgeToggle.getAttribute("aria-expanded") !== "true") await knowledgeToggle.click();
  await page.getByRole("navigation", { name: "Main", exact: true }).getByRole("button", { name: "Glossary", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Terms", exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "Create term", exact: true })).toHaveCount(1);
  await expect(page.getByRole("button", { name: "New term", exact: true })).toHaveCount(0);
  await page.getByRole("button", { name: "Create term", exact: true }).click();
  const term = page.getByRole("dialog", { name: "Create term", exact: true });
  const termID = fresh("TERM"), termName = `Receiving ${suffix}`, missingDeclaration = `build.missing${suffix}`;
  await term.getByRole("textbox", { name: "ID *", exact: true }).fill(termID);
  await term.getByRole("textbox", { name: "Term *", exact: true }).fill(termName);
  await term.getByRole("textbox", { name: "Meaning *", exact: true }).fill("Accepting an incoming delivery");
  await term.getByRole("textbox", { name: "Synonyms", exact: true }).fill("Goods receipt, delivery acceptance");
  await term.getByRole("textbox", { name: "Refers to", exact: true }).fill(missingDeclaration);
  await term.getByRole("textbox", { name: "Read by members of", exact: true }).fill("pms");
  await term.getByRole("textbox", { name: "Read by members of", exact: true }).press("Enter");
  const termRequests: {
    body: { idempotencyKey: string; target: { type: string; id: string } };
    correction?: string;
  }[] = [];
  page.on("request", request => {
    if (request.method() !== "POST" || !request.url().endsWith("/v1/submissions")) return;
    const body = request.postDataJSON();
    if (body.schema?.name === "knowledge.term.create" && body.target?.id === termID) {
      termRequests.push({ body, correction: request.headers()["platform-correction-of"] });
    }
  });
  await term.getByRole("button", { name: "Create", exact: true }).click();
  await expect(term.getByText("ERROR_CODE_INVALID_ARGUMENT", { exact: true })).toBeVisible();
  await expect(term.getByRole("textbox", { name: "ID *", exact: true })).toHaveValue(termID);
  await expect(term.getByRole("textbox", { name: "Term *", exact: true })).toHaveValue(termName);
  await expect(term.getByRole("textbox", { name: "Meaning *", exact: true })).toHaveValue("Accepting an incoming delivery");
  await expect(term.getByRole("textbox", { name: "Synonyms", exact: true })).toHaveValue("Goods receipt, delivery acceptance");
  await expect(term.getByRole("textbox", { name: "Refers to", exact: true })).toHaveValue(missingDeclaration);
  await expect(term.getByRole("button", { name: "Remove pms", exact: true })).toBeVisible();
  expect(await builder.record("knowledge.term", termID)).toBeUndefined();
  await term.getByRole("textbox", { name: "Refers to", exact: true }).fill(object.type);
  await term.getByRole("button", { name: "Submit corrected inputs", exact: true }).click();
  await expect(term).toHaveCount(0);
  expect(termRequests).toHaveLength(2);
  expect(termRequests[1]!.body.target).toEqual(termRequests[0]!.body.target);
  expect(termRequests[1]!.body.idempotencyKey).not.toBe(termRequests[0]!.body.idempotencyKey);
  expect(termRequests[1]!.correction).toBe(termRequests[0]!.body.idempotencyKey);
  expect(await builder.record("knowledge.term", termID)).toMatchObject({ term: termName, meaning: "Accepting an incoming delivery", synonyms: "Goods receipt, delivery acceptance", refersTo: object.type, apps: ["pms"] });
  await expect(page.getByRole("row").filter({ hasText: termName })).toBeVisible();

  await switchWorkspace(page, "Builder");
  await expect(page.getByRole("button", { name: "Switch application", exact: true })).toContainText("WMS entry probe");
  await page.goto(`/#/project?id=${appID}`); // preserve the supported old deep link
  await editor.release();
  await expect(page.getByText("Release active for operators.", { exact: false })).toBeVisible();

  // Resource controls share one archive operation and keep discarded edits local.
  await builder.open(page, `/project?id=${appID}&area=behavior`);
  await page.getByRole("button", { name: "New", exact: true }).click();
  await page.getByRole("menuitem", { name: "Alert rule", exact: true }).click();
  const cancelledRuleName = `cancelled${suffix}`;
  await page.getByRole("textbox", { name: "Rule name", exact: true }).fill(cancelledRuleName);
  await page.getByRole("textbox", { name: "Rule title", exact: true }).fill("Cancelled receiving alert");
  await page.getByRole("button", { name: "Cancel", exact: true }).click();
  const unsaved = page.getByRole("dialog", { name: "Unsaved changes", exact: true });
  if (await unsaved.isVisible()) await unsaved.getByRole("button", { name: "Discard changes", exact: true }).click();
  await expect.poll(() => new URL(page.url()).hash.startsWith("#/project?")).toBe(true);
  await expect(page).toHaveURL(new RegExp(`[?&]id=${appID}(?:&|$)`));
  const cancelledRules = await builder.read<{ records: { name: string }[] }>("records/build.alertrule?limit=1000");
  expect(cancelledRules.records.some(rule => rule.name === cancelledRuleName)).toBe(false);

  await page.getByRole("navigation", { name: "Main", exact: true }).getByRole("button", { name: "Behavior", exact: true }).click();
  await page.getByRole("button", { name: "New", exact: true }).click();
  await page.getByRole("menuitem", { name: "Alert rule", exact: true }).click();
  const ruleName = `receivingalert${suffix}`;
  await page.getByRole("textbox", { name: "Rule name", exact: true }).fill(ruleName);
  await page.getByRole("textbox", { name: "Rule title", exact: true }).fill("Receiving alert");
  await page.getByRole("combobox", { name: "Object", exact: true }).selectOption(object.type);
  await page.getByRole("combobox", { name: "Field", exact: true }).selectOption("number");
  await page.getByRole("textbox", { name: "Value", exact: true }).fill("FOLLOWUP");
  await page.getByRole("textbox", { name: "Message", exact: true }).fill("Check the receiving documents.");
  await page.getByRole("button", { name: "Save alert rule", exact: true }).click();
  await expect.poll(async () => (await builder.record("build.app", appID)).resources)
    .toContainEqual({ app: "build", kind: "alert", name: ruleName });
  const rules = await builder.read<{ records: { id: string; name: string }[] }>("records/build.alertrule?limit=1000");
  const ruleID = rules.records.find(rule => rule.name === ruleName)!.id;
  await expect(page).toHaveURL(new RegExp(`[?&]id=${ruleID}(?:&|$)`));
  const savedRule = await builder.record("build.alertrule", ruleID);
  await page.getByRole("textbox", { name: "Rule title", exact: true }).fill("Unsaved receiving alert");
  await page.getByRole("button", { name: "Publish options", exact: true }).click();
  await expect(page.getByRole("menuitem", { name: "Archive…", exact: true })).toBeDisabled();
  await page.getByRole("menuitem", { name: "Discard unsaved changes", exact: true }).click();
  await expect(page.getByRole("textbox", { name: "Rule title", exact: true })).toHaveValue("Receiving alert");
  await expect(page).toHaveURL(new RegExp(`[?&]id=${ruleID}(?:&|$)`));
  await expect(page.getByRole("button", { name: "Archive", exact: true })).toHaveCount(0);
  await shots(page, page.locator("body"), "resource-controls", testInfo);
  await page.getByRole("button", { name: "Publish options", exact: true }).click();
  await expect(page.getByRole("menuitem", { name: "Archive…", exact: true })).toHaveCount(1);
  await expect(page.getByRole("menuitem", { name: "Archive…", exact: true })).toBeEnabled();
  await page.getByRole("menuitem", { name: "Archive…", exact: true }).click();
  const archive = page.getByRole("dialog");
  await expect(archive).toHaveCount(1);
  await expect(archive).toContainText("Archive this saved record?");
  await archive.getByRole("button", { name: "Cancel", exact: true }).click();
  await expect(archive).toHaveCount(0);
  expect(await builder.record("build.alertrule", ruleID)).toEqual(savedRule);
  await page.getByRole("navigation", { name: "Application context", exact: true }).getByRole("button", { name: "WMS entry probe", exact: true }).click();

  // Navigation is retained state, not assumed collapsed. Open it through the
  // original accessible toggle only when needed; retain all owner assertions.
  await switchWorkspace(page, "Control Panel");
  const toggle = page.getByRole("button", { name: "Toggle navigation", exact: true });
  if (await toggle.getAttribute("aria-expanded") !== "true") await toggle.click();
  const nav = page.getByRole("navigation", { name: "Main", exact: true });
  await expect(nav.getByRole("button", { name: "Members", exact: true })).toBeVisible();
  await expect(nav.getByRole("button", { name: "Documents", exact: true })).toHaveCount(0);
  await switchWorkspace(page, "Runs");
  await expect(nav.getByRole("button", { name: "Workflow runs", exact: true })).toBeVisible();
  await switchWorkspace(page, "Builder");
  await nav.getByRole("button", { name: "Verify", exact: true }).click();
  await expect(page.getByRole("region", { name: "Verification and delivery", exact: true })).toBeVisible();
  if (process.env.PLATFORM_SCREENSHOTS) await page.screenshot({ path: testInfo.outputPath("tenant-operations.png"), fullPage: true });

  // The ordinary operator receives the released application, sees native
  // writes after refresh, and remains unable to edit the builder's definitions.
  const operatorContext = await browser.newContext({ baseURL: "http://127.0.0.1:18496", locale: "en-US" });
  try {
    const business = await operatorContext.newPage();
    await operator.open(business, "/home");
    await expect(business.getByRole("button", { name: "Projects", exact: true })).toHaveCount(0);
    await expect(business.getByRole("button", { name: /^Asset Library\b/ })).toHaveCount(0);
    await operator.open(business, "/records");
    const entityTypes = business.getByRole("combobox", { name: "Entity type", exact: true });
    await expect(entityTypes).toBeVisible();
    await expect(entityTypes.locator('option[value="knowledge.document"]')).toHaveCount(0);
    await expect(entityTypes.locator('option[value="knowledge.term"]')).toHaveCount(0);
    await expect(business.getByRole("button", { name: "Create document", exact: true })).toHaveCount(0);
    await expect(business.getByRole("button", { name: "Create term", exact: true })).toHaveCount(0);
    await expect(operator.decide("knowledge.document.create", { type: "knowledge.document", id: fresh("DENIED") }, { title: "Not an editor", text: "Not allowed" }, "knowledge")).rejects.toThrow(/POLICY_DENIED/);
    await switchWorkspace(business, "Control Panel");
    await expect(business.getByRole("heading", { name: "Overview", exact: true })).toBeVisible();
    await business.getByRole("button", { name: "My account", exact: true }).last().click();
    await expect(business.getByRole("heading", { name: "My account", exact: true })).toBeVisible();
    await operator.open(business, "/home");
    await business.getByRole("button", { name: "WMS entry probe", exact: true }).click();
    await expect(business.getByRole("heading", { name: "Receiving workspace", exact: true })).toBeVisible();
    await expect(business.getByRole("button", { name: "New application instance", exact: true })).toHaveCount(0);
    await expect(business.getByRole("combobox", { name: "Page application", exact: true })).toHaveCount(0);
    const recordID = fresh("RECEIPT");
    await operator.decide(`${object.type}.create`, { type: object.type, id: recordID }, { number: "IN-M1" });
    await expect(business.getByRole("row").filter({ hasText: "IN-M1" })).toBeVisible();
    await business.getByRole("textbox", { name: "Search", exact: true }).fill("IN-M1");
    const exported = business.waitForEvent("download", { timeout: 10_000 });
    await business.getByRole("button", { name: "Export CSV", exact: true }).click();
    const csv = await readFile((await (await exported).path())!, "utf8");
    expect(csv).toContain(recordID);
    expect(csv).toContain("IN-M1");
    await business.reload();
    await expect(business.getByRole("heading", { name: "Receiving workspace", exact: true })).toBeVisible();
    await expect(operator.decide("build.object.edit", { type: "build.object", id: object.id }, { title: "Unauthorized" })).rejects.toThrow(/POLICY_DENIED/);
    await operator.open(business, "/records");
    await business.getByRole("combobox", { name: "Entity type", exact: true }).selectOption(object.type);
    const beforeInspection = business.url();
    await business.getByRole("row").filter({ hasText: "IN-M1" }).click();
    const inspection = business.getByRole("dialog", { name: recordID, exact: true });
    await expect(inspection.getByRole("heading", { name: "IN-M1", exact: true })).toBeVisible();
    expect(business.url()).toBe(beforeInspection);
    await expect(business.getByRole("tab", { name: recordID, exact: true })).toHaveCount(0);
    await inspection.getByRole("button", { name: "Open record", exact: true }).click();
    await expect(inspection).not.toBeVisible();
    await expect.poll(() => new URL(business.url()).hash.startsWith("#/record?")).toBe(true);
    await expect(business.getByRole("button", { name: "Edit", exact: true })).toBeVisible();
    await operator.open(business, `/page?app=build&kind=page&name=${pageName}&application=build:${appName}`);
    if (process.env.PLATFORM_SCREENSHOTS) {
      await business.screenshot({ path: testInfo.outputPath("business-receiving.png"), fullPage: true });
      await business.setViewportSize({ width: 390, height: 844 });
      await business.screenshot({ path: testInfo.outputPath("business-receiving-narrow.png"), fullPage: true });
    }
  } finally { await operatorContext.close(); }
});
