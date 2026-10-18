import { expect, slug, test, shots } from "./kit";

test("one application retains drafts across five workspaces and delivers grouped business assets", async ({ page, builder, operator }, testInfo) => {
  test.setTimeout(120_000);
  const application = await builder.application({ title: "Service application" });
  const objectName = slug("service"), intent = "Track service cases and complete their work.";
  await builder.open(page, `/project?id=${application.id}`);
  const sections = page.getByRole("tablist", { name: "Project sections", exact: true });
  await sections.getByRole("tab", { name: "Guided start", exact: true }).click();
  const guide = page.getByRole("region", { name: "Guided start", exact: true });
  await guide.getByRole("textbox", { name: "Business intent", exact: true }).fill(intent);
  await expect.poll(async () => (await builder.record("build.app", application.id)).description).toBe(intent);
  await guide.getByRole("textbox", { name: "Business object title", exact: true }).fill("Service case");
  await guide.getByRole("textbox", { name: "Business object name", exact: true }).fill(objectName);
  await guide.getByRole("button", { name: "Review suggested object draft", exact: true }).click();
  const create = page.getByRole("dialog");
  await expect(create.getByRole("textbox", { name: "What people call it *", exact: true })).toHaveValue("Service case");
  await create.getByRole("button", { name: "Create", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Service case", exact: true }).first()).toBeVisible();
  const objects = await builder.read<{ records: { id: string; name: string }[] }>("records/build.object?limit=1000");
  const objectID = objects.records.find(record => record.name === objectName)!.id;
  const context = page.getByRole("navigation", { name: "Application context", exact: true });
  await expect(context.getByRole("button", { name: "Service application", exact: true })).toBeVisible();

  // Guidance submits the original create payload. No generated definition or
  // assistant marker is added to the resulting business definition.
  const guided = await builder.record("build.object", objectID);
  const manualID = `${objectID}-MANUAL`, manualName = slug("manual");
  await builder.decide("build.object.create", { type: "build.object", id: manualID }, {
    name: manualName, title: guided.title, plural: guided.plural, description: guided.description, fields: guided.fields,
  });
  const manual = await builder.record("build.object", manualID);
  const definition = (record: any) => Object.fromEntries(Object.entries(record).filter(([key]) => !["id", "name", "revision", "created", "changed"].includes(key)));
  expect(definition(guided)).toEqual(definition(manual));

  const objectSections = page.getByRole("tablist", { name: "Object sections", exact: true });
  await objectSections.getByRole("tab", { name: "Properties", exact: true }).click();
  await page.getByRole("button", { name: "Add a property", exact: true }).click();
  const inspector = page.getByRole("region", { name: "Object inspector", exact: true });
  await inspector.getByRole("textbox", { name: "What people call it", exact: true }).fill("Service note");
  await inspector.getByRole("textbox", { name: "Name", exact: true }).fill("note");
  const navigation = page.getByRole("navigation", { name: "Main", exact: true });
  const toggle = page.getByRole("button", { name: "Toggle navigation", exact: true });
  if (await toggle.getAttribute("aria-expanded") !== "true") await toggle.click();
  await navigation.getByRole("button", { name: "Interface", exact: true }).click();
  await expect(navigation.getByRole("button", { name: "Interface", exact: true })).toHaveAttribute("aria-current", "page");
  await navigation.getByRole("button", { name: "Model", exact: true }).click();
  await navigation.getByRole("region", { name: "Application assets", exact: true }).getByRole("button", { name: /^Service case/ }).click();
  await expect(inspector.getByRole("textbox", { name: "What people call it", exact: true })).toHaveValue("Service note");
  await expect(page.getByRole("button", { name: "Save", exact: true })).toBeEnabled();
  await objectSections.getByRole("tab", { name: "Properties", exact: true }).press("ArrowRight");
  await expect(objectSections.getByRole("tab", { name: "Links", exact: true })).toHaveAttribute("aria-selected", "true");
  await expect(inspector.getByRole("textbox", { name: "What people call it", exact: true })).toHaveValue("Service note");
  await objectSections.getByRole("tab", { name: "Lifecycle", exact: true }).click();
  await page.getByRole("button", { name: "Add a state", exact: true }).click();
  await inspector.getByRole("textbox", { name: "What people call it", exact: true }).fill("Open");
  await inspector.getByRole("textbox", { name: "Name", exact: true }).fill("open");
  await objectSections.getByRole("tab", { name: "Actions", exact: true }).click();
  await page.getByRole("button", { name: "Add an action", exact: true }).click();
  await page.getByRole("button", { name: "Save", exact: true }).click();
  await expect.poll(async () => (await builder.record("build.object", objectID)).actions?.length).toBe(1);
  await shots(page, page.locator("body"), "object-editor", testInfo);

  // Object properties and permissions use the shared table; inline edits
  // still save the same object draft and the application matrix reads it.
  await objectSections.getByRole("tab", { name: "Properties", exact: true }).click();
  await page.getByRole("row").filter({ hasText: "Service note" }).click();
  await expect(inspector.getByRole("textbox", { name: "What people call it", exact: true })).toHaveValue("Service note");
  await shots(page, page.locator("body"), "object-properties", testInfo);
  await objectSections.getByRole("tab", { name: "Permissions", exact: true }).click();
  await page.getByRole("button", { name: "Add a role", exact: true }).click();
  await inspector.getByRole("combobox", { name: "Role", exact: true }).selectOption("user");
  await page.getByRole("combobox", { name: "user · Read", exact: true }).selectOption("all");
  await page.getByRole("checkbox", { name: "user · archive", exact: true }).check();
  await page.getByRole("button", { name: "Save", exact: true }).click();
  await expect.poll(async () => (await builder.record("build.object", objectID)).access?.[0]).toMatchObject({ role: "user", read: "all", archive: true });
  await shots(page, page.locator("body"), "object-permissions", testInfo);
  await context.getByRole("button", { name: "Service application", exact: true }).click();
  await sections.getByRole("tab", { name: "Roles", exact: true }).click();
  const applicationRead = page.getByRole("combobox", { name: "Service case · user · Read", exact: true });
  await applicationRead.selectOption("own");
  await expect(applicationRead).toHaveValue("own");
  await expect.poll(async () => (await builder.record("build.object", objectID)).access?.[0]?.read).toBe("own");
  await applicationRead.selectOption("all");
  await expect(applicationRead).toHaveValue("all");
  await expect.poll(async () => (await builder.record("build.object", objectID)).access?.[0]?.read).toBe("all");
  await shots(page, page.locator("body"), "application-roles", testInfo);

  // Creating another kind does not require learning its workspace first.
  await navigation.getByRole("button", { name: "Model", exact: true }).click();
  await page.getByTitle("New resource", { exact: true }).click();
  const newMenu = page.getByRole("menu", { name: "New", exact: true });
  await expect(newMenu.getByRole("menuitem", { name: "Page", exact: true })).toBeVisible();
  await expect(newMenu.getByRole("menuitem", { name: "Object type", exact: true })).toHaveCount(1);
  await shots(page, page.locator("body"), "application-new-resource", testInfo);
  await page.keyboard.press("Escape");

  const pageDraft = await builder.page({ title: "Service desk", object: `build.${objectName}`, sections: [{ id: "initial", widget: "text", configVersion: 1, text: "Service desk" }] });
  // Add existing uses the same membership decision as creating a page here.
  await navigation.getByRole("button", { name: "Interface", exact: true }).click();
  await page.getByRole("combobox", { name: "Add existing resource", exact: true }).selectOption(JSON.stringify({ kind: "page", id: pageDraft.id }));
  await expect.poll(async () => (await builder.record("build.app", application.id)).pages).toContain(pageDraft.name);
  await sections.getByRole("tab", { name: "Business concepts", exact: true }).click();
  const concepts = page.getByRole("region", { name: "Business concepts", exact: true });
  await concepts.getByRole("button", { name: "Add business concept", exact: true }).click();
  await expect(concepts.getByRole("textbox", { name: "Business concept title", exact: true })).toBeVisible();
  await concepts.getByRole("textbox", { name: "Business concept title", exact: true }).fill("Customer service");
  await concepts.getByRole("textbox", { name: "Business concept title", exact: true }).press("Tab");
  await expect.poll(async () => (await builder.record("build.app", application.id)).concepts?.[0]?.title).toBe("Customer service");
  await concepts.getByRole("button", { name: /^Service case/ }).click();
  const beforeMove = await builder.record("build.object", objectID);
  const savedApp = await builder.record("build.app", application.id);
  await concepts.getByRole("combobox", { name: "Business concept for selected asset", exact: true }).selectOption(savedApp.concepts[0].id);
  await expect.poll(async () => (await builder.record("build.app", application.id)).concepts?.[0]?.assets.length).toBe(1);
  expect(await builder.record("build.object", objectID)).toEqual(beforeMove);
  await concepts.getByRole("button", { name: /^Service desk/ }).click();
  await concepts.getByRole("combobox", { name: "Business concept for selected asset", exact: true }).selectOption(savedApp.concepts[0].id);
  await expect.poll(async () => (await builder.record("build.app", application.id)).concepts?.[0]?.assets.length).toBe(2);

  // Workspaces offer tasks; the business tree keeps the whole concept together.
  const assets = navigation.getByRole("region", { name: "Application assets", exact: true });
  await navigation.getByRole("button", { name: "Model", exact: true }).click();
  await expect(assets.getByRole("button", { name: /^Service case/ })).toBeVisible();
  await expect(assets.getByRole("button", { name: /^Service desk/ })).toBeVisible();
  await assets.getByRole("button", { name: /^Service desk/ }).click();
  await expect(navigation.getByRole("button", { name: "Interface", exact: true })).toHaveAttribute("aria-current", "page");
  await context.getByRole("button", { name: "Service application", exact: true }).click();
  await expect(assets.getByRole("button", { name: /^Service desk/ })).toHaveAttribute("aria-pressed", "true");

  await navigation.getByRole("button", { name: "Verify", exact: true }).click();
  await expect(page.getByRole("tab", { name: /^Application Studio/ })).toHaveCount(1);
  const verify = page.getByRole("region", { name: "Verification and delivery", exact: true });
  await verify.getByRole("button", { name: "Add the drafts it depends on", exact: true }).click();
  await verify.getByRole("button", { name: "Check joint candidate", exact: true }).click();
  await verify.getByRole("button", { name: "Save immutable candidate", exact: true }).click();
  await verify.getByRole("button", { name: "Activate release", exact: true }).click();
  await expect(verify.getByText("Release active for operators.", { exact: false })).toBeVisible();
  const delivered = await operator.definitions<any>();
  const root = delivered.find(definition => definition.ref.kind === "app" && definition.ref.name === application.name);
  expect(root.application.concepts[0].title).toBe("Customer service");
  expect(delivered.some(definition => definition.ref.name === `build.${objectName}.${beforeMove.actions[0].name}`)).toBe(true);

  await navigation.getByRole("button", { name: "Model", exact: true }).click();
  await navigation.getByRole("region", { name: "Application assets", exact: true }).getByRole("button", { name: /^Service case/ }).click();
  await page.getByRole("combobox", { name: "Related assets", exact: true }).selectOption(`build/action/build.${objectName}.${beforeMove.actions[0].name}`);
  await expect(page.getByRole("heading", { name: beforeMove.actions[0].title, exact: true })).toBeVisible();
  await page.getByRole("combobox", { name: "Related assets", exact: true }).selectOption(`build/object/build.${objectName}`);
  await expect(page.getByRole("heading", { name: "Service case", exact: true }).first()).toBeVisible();
  await page.getByRole("combobox", { name: "Related assets", exact: true }).selectOption(`build/action/build.${objectName}.create`);
  await expect.poll(() => new URL(page.url()).hash.startsWith("#/definition?")).toBe(true);
  await navigation.getByRole("region", { name: "Application assets", exact: true }).getByRole("button", { name: /^Service case/ }).click();
  await page.getByRole("combobox", { name: "Related assets", exact: true }).selectOption(`build/page/${pageDraft.name}`);
  await expect(page.getByRole("navigation", { name: "Application context", exact: true }).getByRole("button", { name: "Service application", exact: true })).toBeVisible();
  await expect(page.getByRole("heading", { name: "Service desk", exact: true }).first()).toBeVisible();
  await shots(page, page.locator("body"), "application-studio", testInfo);

  // Two editor categories share one original record inventory.
  const manualFlowID = slug("manualflow"), automaticID = slug("automaticflow");
  await builder.decide("build.process.create", { type: "build.process", id: manualFlowID }, {
    name: slug("manualflow"), title: "Inventory manual flow", manual: true, steps: [{ name: "done", kind: "end" }],
  });
  await builder.decide("build.process.create", { type: "build.process", id: automaticID }, {
    name: slug("automaticflow"), title: "Inventory automation", kind: "automation", object: `build.${objectName}`, when: "open", steps: [{ name: "done", kind: "end" }],
  });
  await builder.open(page, `/project?id=${application.id}&area=assemble`);
  const existing = page.getByRole("combobox", { name: "Add existing resource", exact: true });
  const manualChoice = JSON.stringify({ kind: "flow", id: manualFlowID }), automaticChoice = JSON.stringify({ kind: "automation", id: automaticID });
  await expect(existing.locator("option").filter({ hasText: "Inventory manual flow" })).toHaveCount(1);
  await expect(existing.locator("option").filter({ hasText: "Inventory automation" })).toHaveCount(1);
  await expect(existing.locator("option").filter({ hasText: "Inventory manual flow" })).toHaveAttribute("value", manualChoice);
  await expect(existing.locator("option").filter({ hasText: "Inventory automation" })).toHaveAttribute("value", automaticChoice);
  await existing.selectOption(manualChoice);
  await expect(existing).toBeEnabled();
  await existing.selectOption(automaticChoice);
  const inventoryAssets = navigation.getByRole("region", { name: "Application assets", exact: true });
  await expect(inventoryAssets.getByRole("button", { name: /^Inventory manual flow/ })).toHaveCount(1);
  await expect(inventoryAssets.getByRole("button", { name: /^Inventory automation/ })).toHaveCount(1);
  await inventoryAssets.getByRole("button", { name: /^Inventory manual flow/ }).click();
  await expect.poll(() => new URL(page.url()).hash.startsWith("#/flow?")).toBe(true);
  await inventoryAssets.getByRole("button", { name: /^Inventory automation/ }).click();
  await expect.poll(() => new URL(page.url()).hash.startsWith("#/automation?")).toBe(true);
  await builder.decide("build.process.edit", { type: "build.process", id: automaticID }, { title: "Inventory automation updated" });
  await expect(inventoryAssets.getByRole("button", { name: /^Inventory automation updated/ })).toHaveCount(1);
  await shots(page, page.locator("body"), "shared-inventory-categories", testInfo);
});

test("decision tables and alerts remain application members through editing and release", async ({ page, builder }, testInfo) => {
  test.setTimeout(120_000);
  const object = await builder.object({ title: "Heating zone", fields: [{ name: "temperature", title: "Temperature", type: "decimal" }] });
  const tableID = slug("table"), tableName = slug("heatdecision");
  await builder.decide("build.table.create", { type: "build.table", id: tableID }, {
    name: tableName, title: "Heating decision",
    inputs: [{ name: "temperature", title: "Temperature", type: "number" }],
    outputs: [{ name: "heat", title: "Heat", type: "boolean" }],
    rows: [{ when: ["< 20"], then: ["true"] }], default: ["false"],
  });
  const alertName = slug("lowtemperature");
  const application = await builder.application({ title: "Heating control", resources: [{ app: "build", kind: "object", name: object.type }] });
  await builder.open(page, `/project?id=${application.id}&area=behavior`);
  const add = page.getByRole("combobox", { name: "Add existing resource", exact: true });
  await add.selectOption(JSON.stringify({ kind: "table", id: tableID }));
  await expect.poll(async () => (await builder.record("build.app", application.id)).resources?.length).toBe(2);
  const declarationWrites: { schema: string; id: string }[] = [], joins = { alert: 0, table: 0 };
  let joining: keyof typeof joins = "alert";
  await page.route("**/v1/submissions", async route => {
    const input = route.request().postDataJSON(), schema = input.schema?.name;
    if (["build.alertrule.create", "build.alertrule.edit", "build.table.create", "build.table.edit"].includes(schema)) declarationWrites.push({ schema, id: input.target.id });
    if (schema === "build.app.edit" && input.target?.id === application.id && ++joins[joining] === 1)
      return route.fulfill({ status: 400, contentType: "application/json", body: JSON.stringify({ error: { code: "ERROR_CODE_CONFLICT", message: "Application membership refused" } }) });
    await route.continue();
  });
  await page.getByRole("button", { name: "New", exact: true }).click();
  await page.getByRole("menuitem", { name: "Alert rule", exact: true }).click();
  await page.getByRole("textbox", { name: "Rule name", exact: true }).fill(alertName);
  await page.getByRole("textbox", { name: "Rule title", exact: true }).fill("Low temperature");
  await page.getByRole("combobox", { name: "Object", exact: true }).selectOption(object.type);
  await page.getByRole("combobox", { name: "Field", exact: true }).selectOption("temperature");
  await page.getByRole("combobox", { name: "Operator", exact: true }).selectOption("<");
  await page.getByRole("textbox", { name: "Value", exact: true }).fill("15");
  await page.getByRole("textbox", { name: "Message", exact: true }).fill("The zone needs attention.");
  await page.getByRole("button", { name: "Save alert rule", exact: true }).click();
  await expect(page.getByRole("alert").filter({ hasText: "Application membership refused" })).toBeVisible();
  await expect(page.getByRole("textbox", { name: "Rule title", exact: true })).toHaveValue("Low temperature");
  await expect(page.getByRole("textbox", { name: "Message", exact: true })).toHaveValue("The zone needs attention.");
  await expect(page.getByRole("textbox", { name: "Rule title", exact: true })).toBeDisabled();
  expect((await builder.record("build.app", application.id)).resources).not.toContainEqual({ app: "build", kind: "alert", name: alertName });
  await page.getByRole("button", { name: "Save alert rule", exact: true }).click();
  await expect.poll(async () => (await builder.record("build.app", application.id)).resources?.length).toBe(3);
  const rules = await builder.read<{ records: { id: string; name: string }[] }>("records/build.alertrule?limit=1000");
  const alertID = rules.records.find(rule => rule.name === alertName)!.id;
  expect(declarationWrites).toEqual([{ schema: "build.alertrule.create", id: alertID }]);
  expect((await builder.record("build.app", application.id)).resources).toContainEqual({ app: "build", kind: "alert", name: alertName });

  joining = "table";
  await page.getByRole("navigation", { name: "Application context", exact: true }).getByRole("button", { name: "Heating control", exact: true }).click();
  await page.getByRole("button", { name: "New", exact: true }).click();
  await page.getByRole("menuitem", { name: "Decision table", exact: true }).click();
  const retryTableName = slug("retrydecision"), retryTitle = page.getByRole("textbox", { name: "Table title", exact: true });
  await page.getByRole("textbox", { name: "Table name", exact: true }).fill(retryTableName);
  await retryTitle.fill("Membership retry decision");
  const retryResult = page.getByRole("row").filter({ has: page.getByRole("button", { name: "Move up", exact: true }) }).getByRole("textbox").nth(1);
  await retryResult.fill("Keep");
  await page.getByRole("button", { name: "Save decision table", exact: true }).click();
  await expect(page.getByRole("alert").filter({ hasText: "Application membership refused" })).toBeVisible();
  await expect(retryTitle).toHaveValue("Membership retry decision");
  await expect(retryTitle).toBeDisabled();
  await expect(retryResult).toHaveValue("Keep");
  await expect(retryResult).toBeDisabled();
  expect((await builder.record("build.app", application.id)).resources).not.toContainEqual({ app: "build", kind: "compute", name: retryTableName });
  await page.getByRole("button", { name: "Save decision table", exact: true }).click();
  await expect.poll(async () => (await builder.record("build.app", application.id)).resources).toContainEqual({ app: "build", kind: "compute", name: retryTableName });
  expect(declarationWrites.map(write => write.schema)).toEqual(["build.alertrule.create", "build.table.create"]);
  expect(await builder.record("build.table", declarationWrites[1]!.id)).toMatchObject({ name: retryTableName, title: "Membership retry decision", revision: 1 });
  expect((await builder.record("build.app", application.id)).resources).not.toContainEqual({ app: "build", kind: "table", name: retryTableName });
  expect(joins).toEqual({ alert: 2, table: 2 });
  await page.unroute("**/v1/submissions");

  const navigation = page.getByRole("navigation", { name: "Main", exact: true });
  const toggle = page.getByRole("button", { name: "Toggle navigation", exact: true });
  if (await toggle.getAttribute("aria-expanded") !== "true") await toggle.click();
  const assets = navigation.getByRole("region", { name: "Application assets", exact: true });
  await assets.getByRole("button", { name: /^Heating decision/ }).click();
  await expect(page.getByRole("heading", { name: "Heating decision", exact: true }).first()).toBeVisible();
  await expect(navigation.getByRole("button", { name: "Behavior", exact: true })).toHaveAttribute("aria-current", "page");
  await expect(assets.getByText("Current application", { exact: true })).toBeVisible();
  await expect(assets.getByRole("paragraph").filter({ hasText: /^Heating control$/ })).toBeVisible();
  await expect(page.getByRole("navigation", { name: "Application context", exact: true })
    .getByRole("button", { name: "Heating control", exact: true })).toBeVisible();
  expect(new URLSearchParams(new URL(page.url()).hash.split("?")[1]).get("application")).toBe(application.id);

  // The tenant-wide inventory opens the same record without claiming it is shared.
  const applicationTableTab = page.getByRole("tab", { name: /^Decision tables/ }).and(page.locator(`[data-tab-panel-id="decision-table?application=${application.id}&id=${tableID}"]`));
  await expect(applicationTableTab).toHaveAttribute("aria-selected", "true");
  await page.getByRole("textbox", { name: "Description", exact: true }).fill("Unsaved application decision note");
  await expect(navigation.getByText("Functions", { exact: true })).toBeVisible();
  await expect(navigation.getByText("Shared rules", { exact: true })).toHaveCount(0);
  await navigation.getByRole("button", { name: "Decision tables", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Decision tables", exact: true })).toBeVisible();
  await expect(page.getByRole("navigation", { name: "Application context", exact: true })).toHaveCount(0);
  await expect(assets).toHaveCount(0);
  const allTables = page.getByRole("region", { name: "Decision tables", exact: true });
  const heatingDecision = allTables.getByRole("treeitem").filter({ hasText: tableName }).last();
  await expect(heatingDecision).toContainText("Heating decision");
  await heatingDecision.click();
  await expect(page).toHaveURL(new RegExp(`[?&]id=${tableID}(?:&|$)`));
  const globalParams = new URLSearchParams(new URL(page.url()).hash.split("?")[1]);
  expect(globalParams.get("id")).toBe(tableID);
  expect(globalParams.has("application")).toBe(false);
  await expect(page.getByRole("textbox", { name: "Description", exact: true })).toHaveValue("");
  await applicationTableTab.click();
  await expect(page.getByRole("textbox", { name: "Description", exact: true })).toHaveValue("Unsaved application decision note");
  expect(new URLSearchParams(new URL(page.url()).hash.split("?")[1]).get("application")).toBe(application.id);
  await shots(page, page.locator("body"), "application-and-readable-rules", testInfo);
  await page.setViewportSize({ width: 390, height: 844 });
  await toggle.click();
  await expect(assets.getByText("Current application", { exact: true })).toBeVisible();
  await expect(navigation.getByText("Releases", { exact: true })).toBeVisible();
  await shots(page, navigation, "application-navigation", testInfo);
  await page.setViewportSize({ width: 1280, height: 720 });

  const rulePanel = page.locator("section").filter({ has: page.getByRole("heading", { name: "Rules", exact: true }) }).last();
  const ruleTable = rulePanel.getByRole("table");
  const ruleRows = ruleTable.getByRole("row").filter({ has: page.getByRole("button", { name: "Move up", exact: true }) });
  const ruleColumns = ruleTable.getByRole("columnheader").filter({ has: page.getByRole("textbox") });
  const defaultRow = ruleTable.getByRole("row").filter({ has: page.getByRole("cell", { name: "else", exact: true }) });
  await expect(ruleTable.getByRole("columnheader").filter({ hasText: /^When/ })).toHaveCount(1);
  await expect(ruleTable.getByRole("columnheader").filter({ hasText: /^Then/ })).toHaveCount(1);
  await rulePanel.getByRole("button", { name: "Add row", exact: true }).click();
  await ruleRows.nth(1).getByRole("textbox").nth(0).pressSequentially("< 25");
  await ruleRows.nth(1).getByRole("textbox").nth(1).fill("false");
  await ruleRows.nth(1).getByRole("button", { name: "Move up", exact: true }).click();
  await expect(ruleRows.nth(0).getByRole("textbox").nth(0)).toHaveValue("< 25");
  await rulePanel.getByRole("button", { name: /^\+ condition$/ }).click();
  await ruleColumns.nth(1).getByRole("textbox").fill("");
  await ruleColumns.nth(1).getByRole("textbox").pressSequentially("season");
  await rulePanel.getByRole("button", { name: /^\+ result$/ }).click();
  await ruleColumns.nth(3).getByRole("textbox").fill("instruction");
  await ruleRows.nth(0).getByRole("textbox").nth(3).fill("Check first");
  await ruleRows.nth(1).getByRole("textbox").nth(3).fill("Start heating");
  await expect(defaultRow.getByRole("textbox").nth(0)).toHaveValue("false");
  await defaultRow.getByRole("textbox").nth(1).fill("Leave unchanged");
  await page.getByRole("button", { name: "Save decision table", exact: true }).click();
  await expect.poll(async () => (await builder.record("build.table", tableID)).inputs.length).toBe(2);
  const expandedTable = await builder.record("build.table", tableID);
  expect(expandedTable.inputs).toMatchObject([{ name: "temperature", type: "number" }, { name: "season", type: "text" }]);
  expect(expandedTable.outputs).toMatchObject([{ name: "heat", type: "boolean" }, { name: "instruction", type: "text" }]);
  expect(expandedTable.rows.map((row: any) => [row.expressions.length, row.then.length])).toEqual([[2, 2], [2, 2]]);
  expect(expandedTable.rows.map((row: any) => row.then)).toEqual([["false", "Check first"], ["true", "Start heating"]]);
  expect(expandedTable.default).toEqual(["false", "Leave unchanged"]);
  await shots(page, rulePanel, "decision-table-columns", testInfo);

  await ruleColumns.nth(1).getByRole("button", { name: "Remove column", exact: true }).click();
  await ruleColumns.nth(2).getByRole("button", { name: "Remove column", exact: true }).click();
  await expect(defaultRow.getByRole("textbox")).toHaveValue("false");
  await page.getByRole("button", { name: "Save decision table", exact: true }).click();
  await expect.poll(async () => (await builder.record("build.table", tableID)).inputs.length).toBe(1);
  const savedTable = await builder.record("build.table", tableID);
  expect(savedTable.inputs).toMatchObject([{ name: "temperature", type: "number" }]);
  expect(savedTable.outputs).toMatchObject([{ name: "heat", type: "boolean" }]);
  expect(savedTable.rows.map((row: any) => row.expressions)).toMatchObject([
    [{ op: "<", args: [{ ref: "input.temperature" }, { lit: { value: "25" } }] }],
    [{ op: "<", args: [{ ref: "input.temperature" }, { lit: { value: "20" } }] }],
  ]);
  expect(savedTable.rows.map((row: any) => row.then)).toEqual([["false"], ["true"]]);
  expect(savedTable.default).toEqual(["false"]);
  await assets.getByRole("button", { name: /^Low temperature/ }).click();
  await expect(page.getByRole("heading", { name: "Low temperature", exact: true }).first()).toBeVisible();
  await expect(page.getByRole("navigation", { name: "Application context", exact: true }).getByRole("button", { name: "Heating control", exact: true })).toBeVisible();

  await navigation.getByRole("button", { name: "Verify", exact: true }).click();
  const verify = page.getByRole("region", { name: "Verification and delivery", exact: true });
  await verify.getByRole("button", { name: "Add the drafts it depends on", exact: true }).click();
  await expect(verify.getByRole("list", { name: "Joint selection", exact: true }).getByText(tableID, { exact: true })).toHaveCount(1);
  await verify.getByRole("button", { name: "Check joint candidate", exact: true }).click();
  await expect(verify.getByRole("button", { name: "Save immutable candidate", exact: true })).toBeVisible();
  await verify.getByRole("button", { name: "Save immutable candidate", exact: true }).click();
  await verify.getByRole("button", { name: "Activate release", exact: true }).click();
  await expect(verify.getByText("Release active for operators.", { exact: false })).toBeVisible();
  const delivered = (await builder.definitions<any>()).find(definition => definition.ref.kind === "app" && definition.ref.name === application.name);
  expect(delivered.application.resources).toEqual(expect.arrayContaining([
    { app: "build", kind: "compute", name: tableName }, { app: "build", kind: "alert", name: alertName },
  ]));
  expect((await builder.record("build.table", tableID)).state).toBe("published");
  expect((await builder.record("build.alertrule", alertID)).state).toBe("published");
});
