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
});

test("decision tables and alerts remain application members through editing and release", async ({ page, builder }) => {
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
  await expect.poll(async () => (await builder.record("build.app", application.id)).resources?.length).toBe(3);
  const rules = await builder.read<{ records: { id: string; name: string }[] }>("records/build.alertrule?limit=1000");
  const alertID = rules.records.find(rule => rule.name === alertName)!.id;

  const navigation = page.getByRole("navigation", { name: "Main", exact: true });
  const toggle = page.getByRole("button", { name: "Toggle navigation", exact: true });
  if (await toggle.getAttribute("aria-expanded") !== "true") await toggle.click();
  const assets = navigation.getByRole("region", { name: "Application assets", exact: true });
  await assets.getByRole("button", { name: /^Heating decision/ }).click();
  await expect(page.getByRole("heading", { name: "Heating decision", exact: true }).first()).toBeVisible();
  await expect(navigation.getByRole("button", { name: "Behavior", exact: true })).toHaveAttribute("aria-current", "page");
  await assets.getByRole("button", { name: /^Low temperature/ }).click();
  await expect(page.getByRole("heading", { name: "Low temperature", exact: true }).first()).toBeVisible();
  await expect(page.getByRole("navigation", { name: "Application context", exact: true }).getByRole("button", { name: "Heating control", exact: true })).toBeVisible();

  await navigation.getByRole("button", { name: "Verify", exact: true }).click();
  const verify = page.getByRole("region", { name: "Verification and delivery", exact: true });
  await verify.getByRole("button", { name: "Add the drafts it depends on", exact: true }).click();
  await verify.getByRole("button", { name: "Check joint candidate", exact: true }).click();
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
