import { expect, pageUIProfile, runtime, selectObjectSection, shots, slug, test } from "./kit";

test("an application authors related drafts and a template before its first release", async ({ page, builder, operator, editor }, testInfo) => {
  test.setTimeout(180_000);
  const application = await builder.application({ title: "Service visits" });
  const parentName = slug("customer"), childName = slug("visit"), pageName = slug("visitdesk");
  const parentType = `build.${parentName}`, childType = `build.${childName}`;
  const projectSections = page.getByRole("tablist", { name: "Project sections", exact: true });
  const objectInspector = page.getByRole("region", { name: "Object inspector", exact: true });
  const context = page.getByRole("navigation", { name: "Application context", exact: true });

  // Only the empty application is set up over the API. Definitions below are
  // authored through the same forms and editors a builder uses.
  const createObject = async (name: string, title: string) => {
    await builder.open(page, `/project?id=${application.id}`);
    await projectSections.getByRole("tab", { name: "Guided start", exact: true }).click();
    const guide = page.getByRole("region", { name: "Guided start", exact: true });
    await guide.getByRole("textbox", { name: "Business object title", exact: true }).fill(title);
    await guide.getByRole("textbox", { name: "Business object name", exact: true }).fill(name);
    await guide.getByRole("button", { name: "Review suggested object draft", exact: true }).click();
    await page.getByRole("dialog").getByRole("button", { name: "Create", exact: true }).click();
    await expect(page.getByRole("heading", { name: title, exact: true }).first()).toBeVisible();
    const objects = await builder.read<{ records: { id: string; name: string }[] }>("records/build.object?limit=1000");
    return objects.records.find(record => record.name === name)!.id;
  };
  const addTextProperty = async (name: string, title: string) => {
    await selectObjectSection(page, "Properties");
    await page.getByRole("button", { name: "Add a property", exact: true }).click();
    await objectInspector.getByRole("textbox", { name: "What people call it", exact: true }).fill(title);
    await objectInspector.getByRole("textbox", { name: "Name", exact: true }).fill(name);
  };
  const allowOperators = async () => {
    await selectObjectSection(page, "Permissions");
    await page.getByRole("button", { name: "Add a role", exact: true }).click();
    await objectInspector.getByRole("combobox", { name: "Role", exact: true }).selectOption("user");
    await page.getByRole("combobox", { name: "user · Read", exact: true }).selectOption("all");
  };

  const parentID = await createObject(parentName, "Customer");
  await addTextProperty("name", "Customer name");
  await allowOperators();
  await page.getByRole("button", { name: "Save", exact: true }).click();
  await expect.poll(async () => (await builder.record("build.object", parentID)).fields).toEqual([
    expect.objectContaining({ name: "name", title: "Customer name", type: "text" }),
  ]);

  const childID = await createObject(childName, "Visit");
  await addTextProperty("note", "Visit note");
  await addTextProperty("customer", "Customer");
  await objectInspector.getByRole("combobox", { name: "Type", exact: true }).selectOption("reference");
  await objectInspector.getByRole("combobox", { name: "Reference object", exact: true }).selectOption(parentType);
  await objectInspector.getByRole("textbox", { name: "Seen from there as", exact: true }).fill("visits");
  await selectObjectSection(page, "Lifecycle");
  await page.getByRole("button", { name: "Add a state", exact: true }).click();
  await objectInspector.getByRole("textbox", { name: "What people call it", exact: true }).fill("Open");
  await objectInspector.getByRole("textbox", { name: "Name", exact: true }).fill("open");
  await allowOperators();
  await selectObjectSection(page, "Actions");
  await page.getByRole("button", { name: "Add an action", exact: true }).click();
  await objectInspector.getByRole("button", { name: "Open action type", exact: true }).click();
  await page.getByRole("region", { name: "Action type", exact: true }).getByRole("group", { name: "Editing mode", exact: true }).getByRole("button", { name: "Visual", exact: true }).click();
  await page.getByRole("textbox", { name: "What people call it", exact: true }).fill("Mark reviewed");
  await page.getByRole("textbox", { name: "Name", exact: true }).fill("reviewed");
  const actionStructure = page.getByRole("region", { name: "Action structure", exact: true });
  await actionStructure.getByRole("button", { name: "Parameters", exact: false }).click();
  await page.getByRole("button", { name: "Add a parameter", exact: true }).click();
  const actionInspector = page.getByRole("region", { name: "Action inspector", exact: true });
  await actionInspector.getByRole("textbox", { name: "Label", exact: true }).fill("Review customer");
  await actionInspector.getByRole("textbox", { name: "Name", exact: true }).fill("customer");
  await actionInspector.getByRole("combobox", { name: "Type", exact: true }).selectOption("reference");
  await actionInspector.getByRole("combobox", { name: "Reference object", exact: true }).selectOption(parentType);
  await page.getByRole("button", { name: "Save", exact: true }).click();
  await expect.poll(async () => (await builder.record("build.object", childID)).actions?.[0]).toMatchObject({
    name: "reviewed", title: "Mark reviewed", from: ["open"], inputs: [{ name: "customer", type: "reference", ref: parentType }],
  });
  const unpublished = await operator.definitions<any>();
  expect(unpublished.some(definition => definition.ref.kind === "object" && [parentType, childType].includes(definition.ref.name))).toBe(false);

  await context.getByRole("button", { name: "Service visits", exact: true }).click();
  await projectSections.getByRole("tab", { name: "Guided start", exact: true }).click();
  await page.getByRole("region", { name: "Guided start", exact: true }).getByRole("button", { name: "Design pages from templates", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Studio templates", exact: true })).toBeVisible();
  await page.getByRole("button", { name: /^Record handling workspace/ }).click();
  await page.getByRole("combobox", { name: "Object", exact: true }).selectOption(childType);
  await page.getByRole("checkbox", { name: "Visit note", exact: true }).check();
  await page.getByRole("checkbox", { name: "Customer", exact: true }).check();
  await page.getByRole("checkbox", { name: "Mark reviewed · Saved draft", exact: true }).check();
  await page.getByRole("textbox", { name: "Page name", exact: true }).fill(pageName);
  await page.getByRole("textbox", { name: "Page title", exact: true }).fill("Visit desk");
  await shots(page, page.locator("body"), "unpublished-template-bindings", testInfo);
  await page.getByRole("button", { name: "Create draft in Studio", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Visit desk", exact: true }).first()).toBeVisible();
  const pages = await builder.read<{ records: { id: string; name: string }[] }>("records/build.page?limit=1000");
  const pageID = pages.records.find(record => record.name === pageName)!.id;
  await expect.poll(async () => (await builder.record("build.app", application.id)).pages).toContain(pageName);
  expect((await builder.record("build.page", pageID)).actions).toContain(`${childType}.reviewed`);

  const navigation = page.getByRole("navigation", { name: "Main", exact: true });
  const toggle = page.getByRole("button", { name: "Toggle navigation", exact: true });
  if (await toggle.getAttribute("aria-expanded") !== "true") await toggle.click();
  await navigation.getByRole("button", { name: "Release", exact: true }).click();
  const verify = page.getByRole("region", { name: "Verification and delivery", exact: true });
  await verify.getByRole("button", { name: "Add the drafts it depends on", exact: true }).click();
  await verify.getByRole("button", { name: "Check joint candidate", exact: true }).click();
  await expect(verify.getByRole("button", { name: "Save immutable candidate", exact: true })).toBeVisible();
  await verify.getByRole("button", { name: "Save immutable candidate", exact: true }).click();
  await verify.getByRole("button", { name: "Activate release", exact: true }).click();
  await expect(verify.getByRole("status").filter({ hasText: /^Release active for operators\./ })).toBeVisible();
  const delivered = await operator.definitions<any>();
  const deliveredObject = delivered.find(definition => definition.ref.kind === "object" && definition.ref.name === childType);
  const deliveredPage = delivered.find(definition => definition.ref.kind === "page" && definition.ref.name === pageName);
  expect(deliveredObject.entity.fields).toContainEqual(expect.objectContaining({ name: "customer", type: "reference", ref: parentType, inverse: "visits" }));
  expect(deliveredObject.entity.lifecycle.states).toContainEqual(expect.objectContaining({ name: "open" }));
  expect(delivered.some(definition => definition.ref.kind === "action" && definition.ref.name === `${childType}.reviewed`)).toBe(true);
  expect(deliveredPage.page.object.name).toBe(childType);

  // Editing a published object must expose its new saved fields to page
  // authoring without silently changing the definition operators run.
  await builder.open(page, `/object-type?id=${childID}&application=${application.id}`);
  await addTextProperty("followup", "Follow-up note");
  await page.getByRole("button", { name: "Save", exact: true }).click();
  await expect.poll(async () => (await builder.record("build.object", childID)).fields.some((field: any) => field.name === "followup")).toBe(true);
  await builder.open(page, `/module?id=${application.id}&application=${application.id}&page=${pageID}&surface=studio`);
  await editor.select("Table");
  await editor.inspector.getByRole("tab", { name: "Data", exact: true }).click();
  const model = editor.inspector.getByLabel("Model binding", { exact: true });
  await model.getByRole("button", { name: "Add model field", exact: true }).click();
  await model.getByLabel("Declared model path", { exact: true }).last().getByRole("combobox", { name: "Model field", exact: true }).selectOption("followup");
  await expect(model.getByRole("status")).toHaveText("This binding includes saved model changes. Publish the model and page together before reading these fields in preview.");
  await editor.save();
  await expect.poll(async () => (await builder.record("build.page", pageID)).sections.find((section: any) => section.widget === "table")?.model?.fields?.some((field: any) => field.name === "followup")).toBe(true);
  await editor.tree.getByRole("button", { name: "Page settings", exact: true }).click();
  await editor.inspector.getByRole("tab", { name: "Variables", exact: false }).click();
  await editor.inspector.getByRole("button", { name: "Add variable", exact: true }).click();
  await editor.inspector.getByRole("textbox", { name: "Variable label", exact: true }).fill("Selected visit");
  await editor.inspector.getByRole("combobox", { name: "Variable mode", exact: true }).selectOption("resource");
  await editor.inspector.getByRole("combobox", { name: "Resource output kind", exact: true }).selectOption("record");
  await editor.inspector.getByRole("button", { name: "Add variable", exact: true }).click();
  await editor.inspector.getByRole("textbox", { name: "Variable label", exact: true }).fill("Follow-up text");
  await editor.inspector.getByRole("combobox", { name: "Variable mode", exact: true }).selectOption("property");
  await editor.inspector.getByRole("combobox", { name: "Record property field", exact: true }).selectOption("followup");
  await editor.save();
  await expect.poll(async () => Object.values((await builder.record("build.page", pageID)).document.variables)).toContainEqual(
    expect.objectContaining({ title: "Follow-up text", type: "string", source: expect.objectContaining({ field: "followup" }) }),
  );
  const stillPublished = await operator.definitions<any>();
  expect(stillPublished.find(definition => definition.ref.kind === "object" && definition.ref.name === childType)).toEqual(deliveredObject);
  expect(stillPublished.find(definition => definition.ref.kind === "page" && definition.ref.name === pageName)).toEqual(deliveredPage);
  await shots(page, page.locator("body"), "saved-draft-page-field", testInfo);

  const variablesTab = page.getByRole("tablist", { name: "Page dock", exact: true })
    .getByRole("tab", { name: /^Variables/ });
  await variablesTab.click();
  const variablesPanelID = await variablesTab.getAttribute("aria-controls");
  expect(variablesPanelID).toBeTruthy();
  const variablesPanel = page.getByRole("tabpanel").filter({ has: page.getByRole("columnheader", { name: "Current value", exact: true }) });
  await expect(variablesPanel).toHaveAttribute("id", variablesPanelID!);
  await expect(variablesPanel.getByRole("columnheader")).toHaveText(["Variable", "Type", "Current value"]);
  await expect(variablesPanel.getByRole("row").filter({ hasText: "Selected visit" }).getByRole("cell"))
    .toHaveText(["Selected visit", "record", JSON.stringify({ status: "empty" })]);
  await expect(variablesPanel.getByRole("row").filter({ hasText: "Follow-up text" }).getByRole("cell"))
    .toHaveText(["Follow-up text", "string", JSON.stringify({ status: "empty" })]);
  await shots(page, variablesPanel, "page-variable-values", testInfo);
});

test("a narrow workspace can search applications and reopen the empty page widget library", async ({ page, builder, editor }, testInfo) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await builder.open(page, "/projects");
  await expect(page.getByRole("searchbox", { name: "Search projects", exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "New application", exact: true })).toBeEnabled();
  await shots(page, page.locator("body"), "applications-narrow", testInfo);
  await page.setViewportSize({ width: 390, height: 844 });
  await page.getByRole("button", { name: "Workspace menu", exact: true }).click();
  await expect(page.getByRole("menuitem", { name: "Dark", exact: true })).toBeVisible();
  await shots(page, page.locator("body"), "workspace-menu-narrow", testInfo);
  await page.keyboard.press("Escape");
  await page.setViewportSize({ width: 390, height: 844 });

  const { type } = await builder.object({ title: "Empty page records", fields: [{ name: "title", title: "Title", type: "text" }] });
  const { id } = await builder.page({ title: "Empty page", object: type, sections: [],
    document: { formatVersion: 2, uiProfile: pageUIProfile, root: "root", nodes: { root: { kind: "rows", children: [] } } },
  });
  await editor.open(builder, id);
  if (await editor.inspector.isVisible()) await page.getByRole("button", { name: "Close inspector panel", exact: true }).click();
  if (await editor.tree.isVisible()) await page.getByRole("button", { name: "Close structure panel", exact: true }).click();

  for (let attempt = 0; attempt < 2; attempt++) {
    await page.getByRole("button", { name: "Add widget", exact: true }).click();
    await expect(editor.tree).toBeVisible();
    await expect(editor.tree.getByRole("tab", { name: "Widgets", exact: true })).toHaveAttribute("aria-selected", "true");
    await expect(editor.tree.getByRole("searchbox", { name: "Search widgets", exact: true })).toBeVisible();
    await page.getByRole("button", { name: "Close structure panel", exact: true }).click();
  }
  await shots(page, page.locator("body"), "empty-page-narrow", testInfo);
});

test("authoring previews stay read-only with and without a published application or an embedded page", async ({ page, builder, editor }) => {
  test.setTimeout(150_000);
  const object = await builder.object({ title: "Preview entry", fields: [{ name: "note", title: "Note", type: "text" }] });
  await builder.records(object.type, [{ note: "Existing authorized entry" }]);
  const sections = [
    { id: "records", widget: "table", configVersion: 1, title: "Preview records", fields: ["note"] },
    { id: "create", widget: "form", configVersion: 1, title: "Create preview entry", fields: ["note"] },
  ];
  const document = { formatVersion: 2, uiProfile: pageUIProfile, root: "root", nodes: {
    root: { kind: "rows", children: ["records", "create"] }, records: { kind: "widget", section: "records" }, create: { kind: "widget", section: "create" },
  } };
  const original = await builder.page({ title: "Preview boundaries", object: object.type, sections, document });
  const writes: string[] = [];
  page.on("request", request => {
    if (request.method() !== "POST") return;
    const path = new URL(request.url()).pathname;
    if (path === "/v1/submissions") {
      const schema = request.postDataJSON()?.schema?.name;
      if (typeof schema === "string" && schema.startsWith(`${object.type}.`)) writes.push(schema);
    } else if (path.startsWith("/v1/capabilities/calls/") || path === "/v1/files" || path.startsWith("/v1/import/")) writes.push(path);
  });
  const preview = async (id: string, application?: string) => {
    if (application) { await builder.open(page, `/module?id=${application}&application=${application}&page=${id}&surface=studio`); await editor.visual(); }
    else await editor.open(builder, id);
    const previewButton = page.getByRole("button", { name: "Preview", exact: true });
    if (await previewButton.getAttribute("aria-pressed") !== "true") await previewButton.click();
    const canvas = page.locator(`[data-structured-editor^="build.page:${id}:"]:visible [data-canvas-scroll]`);
    await expect(canvas).toHaveCount(1);
    await expect(canvas.getByText("The form does not submit while you compose.", { exact: true })).toBeVisible();
    await expect(canvas.getByRole("textbox", { name: "Note", exact: true })).toBeDisabled();
    await expect(canvas.getByRole("button", { name: "Create", exact: true })).toBeDisabled();
    await expect(canvas.getByText("Existing authorized entry", { exact: true })).toBeVisible();
    expect(writes).toEqual([]);
  };
  await preview(original.id);
  const application = await builder.application({ title: "Preview boundary application", pages: [original.name] });
  await preview(original.id, application.id);
  await builder.activate(await builder.candidate([{ kind: "page", id: original.id }, { kind: "app", id: application.id }]));
  await preview(original.id, application.id);
  const added = await builder.page({ title: "New page of published application", object: object.type, sections, document });
  await builder.decide("build.app.edit", { type: "build.app", id: application.id }, { pages: [original.name, added.name] });
  await preview(added.id, application.id);
  const published = (await builder.definitions<any>()).find(definition => definition.ref.kind === "page" && definition.ref.name === original.name);
  const embedded = await builder.page({ title: "Embedded preview", object: object.type,
    sections: [{ id: "child", widget: "embedded-page", configVersion: 1, title: "Embedded entry workspace", embedding: {
      kind: "module", page: { ref: published.ref, sourceVersion: published.version }, contentVersion: published.contentVersion, interfaceVersion: 0,
    } }], document: { formatVersion: 2, uiProfile: pageUIProfile, root: "root", nodes: { root: { kind: "rows", children: ["child"] }, child: { kind: "widget", section: "child" } } },
  });
  await preview(embedded.id);
  expect((await builder.read<{ records: unknown[] }>(`records/${object.type}`)).records).toHaveLength(1);

  // The same published form remains usable in the actual application.
  const live = await runtime(page, builder, original.name);
  await live.getByRole("textbox", { name: "Note", exact: true }).fill("Created in runtime");
  await live.getByRole("button", { name: "Create", exact: true }).last().click();
  await expect.poll(async () => (await builder.read<{ records: unknown[] }>(`records/${object.type}`)).records.length).toBe(2);
  await live.close();
});
