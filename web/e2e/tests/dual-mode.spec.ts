import { expect, fresh, pageUIProfile, shots, slug, test } from "./kit";

test.use({ actionTimeout: 15_000 });

test("new application visual, JSON and YAML edit one payload without implicit writes", async ({ page, builder }, info) => {
  test.setTimeout(90_000);
  const resource = await builder.object({ title: "Application reference model", fields: [{ name: "name", title: "Name", type: "text" }] });
  const submissions: any[] = [];
  page.on("request", request => {
    if (request.method() === "POST" && new URL(request.url()).pathname === "/v1/submissions") submissions.push(request.postDataJSON());
  });
  await builder.open(page, "/projects");
  await page.getByRole("button", { name: "New application", exact: true }).click();
  const dialog = page.getByRole("dialog");
  const id = await dialog.getByRole("textbox", { name: "ID *", exact: true }).inputValue();
  const name = slug("sourcemodes"), title = `Source modes ${fresh("app")}`;
  await shots(page, dialog, "dual-application-basic-en", info);
  await dialog.getByText("Advanced configuration", { exact: true }).click();
  for (const label of ["Application profile", "Application header", "UI profile", "Application queries", "Extends", "Entry", "Replacements", "Application variables", "Application resources", "Business concepts"]) {
    await expect(dialog.locator("label").filter({ hasText: new RegExp(`^${label}$`) })).toBeVisible();
  }
  await dialog.getByRole("textbox", { name: /^Name \*$/ }).fill(name);
  await dialog.getByRole("textbox", { name: /^What people call it \*$/ }).fill(title);
  await dialog.getByRole("group", { name: "Editing mode", exact: true }).getByRole("button", { name: "Source editor", exact: true }).click();
  const source = dialog.getByRole("textbox", { name: "Configuration source", exact: true });
  await dialog.getByText("Fields and examples", { exact: true }).click();
  const fieldHelp=dialog.getByRole("button",{name:"About Fields and examples",exact:true});
  await fieldHelp.click();
  await expect(page.getByRole("dialog",{name:"Fields and examples",exact:true})).toContainText("Optional fields may be omitted.");
  await page.keyboard.press("Escape");
  await expect(fieldHelp).toBeFocused();
  await expect(source).toBeVisible();
  const original = JSON.parse(await source.inputValue());
  expect(original).toMatchObject({ name, title });
  expect(original).not.toHaveProperty("id");
  const declaration = {
    ...original, description: "", pages: [], groups: [], profile: null, uiProfile: pageUIProfile,
    header: { title: "Application field parity", variant: "horizontal", items: [{ kind: "title" }] },
    queries: {}, variables: { message: { scope: "application", type: "string", mode: "state", initial: "Shared initial value" } },
    resources: [{ app: "build", kind: "object", name: resource.name }],
  };
  await source.fill(JSON.stringify({ ...declaration, unknownInput: true, state: "published" }, null, 2));
  await dialog.getByRole("button", { name: "Check inputs", exact: true }).click();
  await expect(dialog.getByRole("alert").filter({ hasText: "unknownInput" })).toBeVisible();
  await expect(dialog.getByRole("alert").filter({ hasText: "state" })).toBeVisible();
  await source.fill("{ broken source");
  await dialog.getByRole("group", { name: "Editing mode", exact: true }).getByRole("button", { name: "Visual", exact: true }).click();
  await expect(source).toHaveValue("{ broken source");
  await expect(dialog.getByRole("button", { name: "Create", exact: true })).toBeDisabled();
  await source.fill(JSON.stringify(declaration, null, 2));
  await dialog.getByRole("button", { name: "Apply source", exact: true }).click();
  await dialog.getByRole("combobox", { name: "Source format", exact: true }).selectOption("yaml");
  await expect(source).toHaveValue(new RegExp(`name: ${name}`));
  await expect(source).toHaveValue(/profile: null/);
  await dialog.getByRole("combobox", { name: "Source format", exact: true }).selectOption("json");
  expect(JSON.parse(await source.inputValue())).toMatchObject(declaration);
  await shots(page, dialog, "dual-application-source-en", info);
  await dialog.getByRole("group", { name: "Editing mode", exact: true }).getByRole("button", { name: "Visual", exact: true }).focus();
  await page.keyboard.press("Enter");
  await expect(dialog.getByRole("textbox", { name: /^What people call it \*$/ })).toHaveValue(title);
  await dialog.getByRole("group", { name: "Application header", exact: true }).getByRole("textbox", { name: "Application header.title", exact: true }).fill("Visual application header");
  declaration.header.title = "Visual application header";
  await dialog.getByRole("button", { name: "Source editor", exact: true }).click();
  expect(JSON.parse(await source.inputValue()).header).toEqual(declaration.header);
  await dialog.getByRole("button", { name: "Visual", exact: true }).click();
  expect(submissions).toHaveLength(0);
  await shots(page, dialog, "dual-application-visual-en", info);
  await dialog.getByRole("button", { name: "Create", exact: true }).click();
  await expect(dialog).toHaveCount(0);
  expect(submissions.filter(input => input.schema?.name === "build.app.create")).toHaveLength(1);
  expect(await builder.record("build.app", id)).toMatchObject({ name, title, pages: [], header: declaration.header, uiProfile: pageUIProfile, variables: declaration.variables, resources: declaration.resources });

  await builder.open(page, `/record?type=build.app&id=${id}`);
  await page.getByRole("button", { name: "Edit", exact: true }).click();
  for (const label of ["Application profile", "Application header", "UI profile", "Application queries", "Extends", "Entry", "Replacements", "Application variables", "Application resources", "Business concepts"]) {
    await expect(dialog.locator("label").filter({ hasText: new RegExp(`^${label}$`) })).toBeVisible();
  }
  await dialog.getByRole("textbox", { name: "UI profile", exact: true }).fill(pageUIProfile);
  await shots(page, dialog, "dual-application-edit-en", info);
  await dialog.getByRole("textbox", { name: /^What people call it/ }).fill(`${title} edited`);
  await dialog.getByRole("button", { name: "Source editor", exact: true }).click();
  const editDeclaration = JSON.parse(await source.inputValue());
  expect(editDeclaration).toMatchObject({ name, title: `${title} edited`, header: declaration.header, uiProfile: pageUIProfile, variables: declaration.variables, resources: declaration.resources });
  await dialog.getByRole("button", { name: "Visual", exact: true }).click();
  await dialog.getByRole("button", { name: "Save", exact: true }).click();
  await expect(dialog).toHaveCount(0);
  expect(await builder.record("build.app", id)).toMatchObject({ title: `${title} edited`, header: declaration.header, variables: declaration.variables, resources: declaration.resources });
  const me = await builder.read<any>("me");
  await builder.decide("platform.profile.update", { type: "platform.profile", id: me.principalId }, { language: "zh-CN" }, "platform");
  try {
    await builder.open(page, "/projects");
    await page.getByRole("button", { name: "新建应用", exact: true }).click();
    await shots(page, dialog, "dual-application-basic-zh", info);
    await dialog.getByText("高级配置", { exact: true }).click();
    await shots(page, dialog, "dual-application-advanced-zh", info);
    await dialog.getByRole("group", { name: "编辑模式", exact: true }).getByRole("button", { name: "源码", exact: true }).click();
    await expect(dialog.getByRole("textbox", { name: "配置源码", exact: true })).toBeVisible();
    await shots(page, dialog, "dual-application-source-zh", info);
    await dialog.getByRole("button", { name: "取消", exact: true }).click();
  } finally {
    await builder.decide("platform.profile.update", { type: "platform.profile", id: me.principalId }, { language: "en" }, "platform");
    await page.evaluate(() => localStorage.setItem("platform.language", "en"));
  }
});

test("business source trial stays private and formal refusal retries the original intent", async ({ page, builder, operator }, info) => {
  test.setTimeout(90_000);
  const object = await builder.object({
    name: slug("sourcebusiness"), title: "Source operation item", plural: "Source operation items",
    fields: [{ name: "name", title: "Item name", type: "text", required: true }, { name: "quantity", title: "Quantity", type: "integer" }, { name: "enabled", title: "Enabled", type: "boolean" }],
    access: [{ role: "user", read: "all", create: true, edit: true }],
  });
  await operator.open(page, `/definition?app=build&kind=object&name=${object.type}`);
  await page.getByRole("button", { name: /^Create [Ss]ource operation item$/, exact: true }).click();
  const dialog = page.getByRole("dialog");
  const id = fresh("DUAL");
  await dialog.getByRole("textbox", { name: "ID *", exact: true }).fill(id);
  await dialog.getByRole("group", { name: "Editing mode", exact: true }).getByRole("button", { name: "Source editor", exact: true }).click();
  const source = dialog.getByRole("textbox", { name: "Configuration source", exact: true });
  await source.fill(JSON.stringify({ name: "Trial item", quantity: 0, enabled: false }));
  await dialog.getByRole("button", { name: "Apply source", exact: true }).click();
  await dialog.getByRole("button", { name: "Try operation", exact: true }).click();
  await expect(dialog.getByRole("status").filter({ hasText: "Business trial passed. Nothing was saved; submission checks again." })).toBeVisible();
  expect((await operator.read<any>(`records/${object.type}?search=${id}`)).records).toEqual([]);

  // Another confirmed create wins after trial; formal submit must re-check.
  await operator.decide(`${object.type}.create`, { type: object.type, id }, { name: "Existing record", quantity: 7, enabled: true });
  const requests: any[] = [];
  page.on("request", request => {
    if (request.method() !== "POST" || new URL(request.url()).pathname !== "/v1/submissions") return;
    const input = request.postDataJSON();
    if (input.schema?.name === `${object.type}.create`) requests.push(input);
  });
  await dialog.getByRole("button", { name: "Create", exact: true }).click();
  await expect(dialog.getByRole("button", { name: "Retry original inputs", exact: true })).toBeVisible();
  expect(JSON.parse(await source.inputValue())).toMatchObject({ name: "Trial item", quantity: 0, enabled: false });
  await dialog.getByRole("button", { name: "Retry original inputs", exact: true }).click();
  await expect.poll(() => requests.length).toBe(2);
  expect(requests[1]).toEqual(requests[0]);
  expect(await operator.record(object.type, id)).toMatchObject({ name: "Existing record", quantity: 7, enabled: true, revision: 1 });
  await shots(page, dialog, "dual-business-refusal-en", info);
  await dialog.getByRole("button", { name: "Cancel", exact: true }).click();
});

test("editing starts from writable values and the source guide selects real references and choices", async ({ page, builder, operator }, info) => {
  test.setTimeout(90_000);
  const access = [{ role: "user", read: "all", create: true, edit: true }];
  const supplier = await builder.object({ title: "Source supplier", fields: [{ name: "name", title: "Supplier name", type: "text" }], access });
  const [firstSupplier, secondSupplier] = await builder.records(supplier.type, [{ name: "Original supplier" }, { name: "Replacement supplier" }], "SUPPLIER");
  const object = await builder.object({ title: "Guided source item", fields: [
    { name: "name", title: "Item name", type: "text", required: true },
    { name: "priority", title: "Priority", type: "choice", choices: "normal,urgent" },
    { name: "supplier", title: "Supplier", type: "reference", ref: supplier.type },
    { name: "quantity", title: "Quantity", type: "integer" },
  ], access });
  const id = fresh("GUIDE");
  const original = { name: "Existing item", priority: "normal", supplier: firstSupplier, quantity: 0 };
  await operator.decide(`${object.type}.create`, { type: object.type, id }, original);
  await operator.open(page, `/record?type=${object.type}&id=${id}`);
  await page.getByRole("button", { name: "Edit", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await dialog.getByRole("button", { name: "Source editor", exact: true }).click();
  const source = dialog.getByRole("textbox", { name: "Configuration source", exact: true });
  expect(JSON.parse(await source.inputValue())).toMatchObject(original);
  expect(JSON.parse(await source.inputValue())).not.toHaveProperty("revision");
  expect(JSON.parse(await source.inputValue())).not.toHaveProperty("id");
  await dialog.getByText("Fields and examples", { exact: true }).click();
  await dialog.getByText("Example document", { exact: true }).click();
  await expect(dialog.locator("pre")).toContainText('"name": "<Item name>"');
  await expect(dialog.locator("pre")).toContainText('"supplier": "<select a permitted record>"');
  expect(JSON.parse(await source.inputValue()).name).toBe("Existing item");
  await dialog.getByRole("combobox", { name: "Field reference", exact: true }).selectOption("priority");
  await dialog.getByRole("combobox", { name: "Priority", exact: true }).selectOption("urgent");
  await dialog.getByRole("combobox", { name: "Field reference", exact: true }).selectOption("supplier");
  await dialog.getByRole("combobox", { name: "Supplier", exact: true }).fill(secondSupplier!);
  await dialog.getByRole("option", { name: new RegExp(secondSupplier!) }).click();
  const changed = { ...original, priority: "urgent", supplier: secondSupplier };
  expect(JSON.parse(await source.inputValue())).toMatchObject(changed);
  expect(await operator.record(object.type, id)).toMatchObject(original);
  await shots(page, dialog, "dual-source-guide-en", info);

  await source.fill(JSON.stringify({ ...changed, priority: "unknown-priority" }));
  await dialog.getByRole("button", { name: "Check inputs", exact: true }).click();
  await expect(dialog.getByRole("alert").filter({ hasText: "priority" })).toBeVisible();
  await expect(dialog.getByRole("button", { name: "Save", exact: true })).toBeDisabled();
  const missing = fresh("MISSING");
  await source.fill(JSON.stringify({ ...changed, supplier: missing }));
  await dialog.getByRole("button", { name: "Apply source", exact: true }).click();
  const response = page.waitForResponse(answer => answer.request().method() === "POST" && new URL(answer.url()).pathname === "/v1/simulate");
  await dialog.getByRole("button", { name: "Try operation", exact: true }).click();
  expect((await (await response).json()).accepted).toBe(false);
  expect(JSON.parse(await source.inputValue()).supplier).toBe(missing);
  expect(await operator.record(object.type, id)).toMatchObject(original);

  await source.fill(JSON.stringify(changed));
  await dialog.getByRole("button", { name: "Apply source", exact: true }).click();
  await dialog.getByRole("button", { name: "Visual", exact: true }).click();
  await expect(dialog.getByRole("textbox", { name: /^Item name/ })).toHaveValue("Existing item");
  await dialog.getByRole("button", { name: "Save", exact: true }).click();
  await expect(dialog).toHaveCount(0);
  expect(await operator.record(object.type, id)).toMatchObject(changed);

  // Original native declarations also use the generic editor for opaque JSON.
  const flowID = fresh("PARITYFLOW"), input = { enabled: false, threshold: 0, tags: [], optional: null };
  await builder.decide("build.process.create", { type: "build.process", id: flowID }, { name: slug("parityflow"), title: "Generic process input", manual: true, steps: [{ name: "done", kind: "end" }], input });
  await builder.open(page, `/record?type=build.process&id=${flowID}`);
  await page.getByRole("button", { name: "Edit", exact: true }).click();
  const settings = dialog.getByRole("group", { name: "Default input", exact: true });
  await settings.getByRole("combobox", { name: "Default input.enabled", exact: true }).selectOption("true");
  await settings.getByRole("spinbutton", { name: "Default input.threshold", exact: true }).fill("2");
  const final = { ...input, enabled: true, threshold: 2 };
  await dialog.getByRole("button", { name: "Source editor", exact: true }).click();
  expect(JSON.parse(await source.inputValue()).input).toEqual(final);
  await dialog.getByRole("button", { name: "Visual", exact: true }).click();
  await shots(page, dialog, "dual-process-json-visual-en", info);
  await dialog.getByRole("button", { name: "Save", exact: true }).click();
  await expect(dialog).toHaveCount(0);
  expect((await builder.record("build.process", flowID)).input).toEqual(final);
});

test("object, relationship, action, flow and page source return to the original visual editors", async ({ page, builder, editor }, info) => {
  test.setTimeout(120_000);
  const model = await builder.object({
    title: "Declaration model", fields: [{ name: "note", title: "Note", type: "text" }],
    states: [{ name: "open", title: "Open" }, { name: "done", title: "Done" }],
    actions: [{ name: "complete", title: "Complete declaration", from: ["open"], to: "done", inputs: [{ name: "quantity", title: "Quantity", type: "integer", required: true }] }],
  });
  let writes = 0;
  page.on("request", request => { if (request.method() === "POST" && new URL(request.url()).pathname === "/v1/submissions") writes++; });
  await builder.open(page, `/object-type?id=${model.id}`);
  const objectRevision = (await builder.record("build.object", model.id)).revision;
  await page.getByRole("group", { name: "Editing mode", exact: true }).getByRole("button", { name: "Source editor", exact: true }).click();
  const source = page.getByRole("textbox", { name: "Configuration source", exact: true });
  const objectDeclaration = JSON.parse(await source.inputValue());
  const originalObjectDeclaration = structuredClone(objectDeclaration);
  objectDeclaration.fields[0].title = "Source-authored note";
  objectDeclaration.actions[0].description = "Original object action";
  await source.fill(JSON.stringify(objectDeclaration, null, 2));
  await page.getByRole("button", { name: "Apply source", exact: true }).click();
  await page.getByRole("button", { name: "Undo", exact: true }).click();
  expect(JSON.parse(await source.inputValue())).toEqual(originalObjectDeclaration);
  await page.getByRole("button", { name: "Redo", exact: true }).click();
  expect(JSON.parse(await source.inputValue())).toEqual(objectDeclaration);
  await page.getByRole("group", { name: "Editing mode", exact: true }).getByRole("button", { name: "Visual", exact: true }).click();
  expect(writes).toBe(0);
  expect((await builder.record("build.object", model.id)).revision).toBe(objectRevision);
  await page.getByRole("tablist", { name: "Object sections", exact: true }).getByRole("tab", { name: "Properties", exact: true }).click();
  await expect(page.getByRole("row").filter({ hasText: "Source-authored note" })).toBeVisible();
  await page.getByRole("button", { name: "Save", exact: true }).click();
  await expect.poll(async () => (await builder.record("build.object", model.id)).fields[0].title).toBe("Source-authored note");

  const relationshipID = fresh("REL");
  await builder.decide("build.linktype.create", { type: "build.linktype", id: relationshipID }, { name: slug("sourcelink"), title: "Source relationship", description: "Editable relationship", parent: model.type, child: model.type, storage: "edge", forward: "children", reverse: "parent", cardinality: "one-to-many", deletePolicy: "owner", roles: ["builder", "user"] });
  await builder.open(page, `/link-type?id=${relationshipID}`);
  await page.getByRole("group", { name: "Editing mode", exact: true }).getByRole("button", { name: "Source editor", exact: true }).click();
  await page.getByRole("combobox", { name: "Source format", exact: true }).selectOption("yaml");
  const relationSource = await source.inputValue();
  await source.fill(relationSource.replace("title: Source relationship", "title: YAML relationship"));
  await page.getByRole("button", { name: "Apply source", exact: true }).click();
  await page.getByRole("group", { name: "Editing mode", exact: true }).getByRole("button", { name: "Visual", exact: true }).click();
  await expect(page.getByRole("textbox", { name: "Relationship title", exact: true })).toHaveValue("YAML relationship");
  await page.getByRole("button", { name: "Save relationship", exact: true }).click();
  await expect.poll(async () => (await builder.record("build.linktype", relationshipID)).title).toBe("YAML relationship");

  await builder.open(page, `/action-type?id=${model.id}&action=complete`);
  await page.getByRole("group", { name: "Editing mode", exact: true }).getByRole("button", { name: "Source editor", exact: true }).click();
  const action = JSON.parse(await source.inputValue());
  expect(action.name).toBe("complete");
  await source.fill(JSON.stringify({ ...action, title: "Source-completed action", inputs: [{ ...action.inputs[0], required: false }] }, null, 2));
  await page.getByRole("button", { name: "Apply source", exact: true }).click();
  await page.getByRole("group", { name: "Editing mode", exact: true }).getByRole("button", { name: "Visual", exact: true }).click();
  await expect(page.getByRole("textbox", { name: "What people call it", exact: true })).toHaveValue("Source-completed action");
  await page.getByRole("button", { name: "Save", exact: true }).click();
  await expect.poll(async () => (await builder.record("build.object", model.id)).actions[0].title).toBe("Source-completed action");
  expect((await builder.record("build.object", model.id)).actions[0].inputs[0].required ?? false).toBe(false);

  const flowID = fresh("FLOW");
  await builder.decide("build.process.create", { type: "build.process", id: flowID }, { name: slug("sourceflow"), title: "Source flow", manual: true, steps: [{ name: "done", kind: "end" }] });
  await builder.open(page, `/flow?id=${flowID}`);
  const flowEditor = page.getByRole("region", { name: "Flows", exact: true });
  await expect(flowEditor.getByRole("region", { name: "Flow map", exact: true })).toBeVisible();
  await flowEditor.getByRole("group", { name: "Editing mode", exact: true }).getByRole("button", { name: "Source editor", exact: true }).click();
  const flow = JSON.parse(await source.inputValue());
  await source.fill(JSON.stringify({ ...flow, title: "Source-edited flow", input: { quantity: 0, enabled: false, tags: [], optional: null } }, null, 2));
  await page.getByRole("button", { name: "Apply source", exact: true }).click();
  await page.getByRole("group", { name: "Editing mode", exact: true }).getByRole("button", { name: "Visual", exact: true }).click();
  await expect(page.getByRole("region", { name: "Flow map", exact: true })).toBeVisible();
  await page.keyboard.press("ControlOrMeta+s");
  await expect.poll(async () => (await builder.record("build.process", flowID)).title).toBe("Source-edited flow");
  expect((await builder.record("build.process", flowID)).input).toMatchObject({ quantity: 0, enabled: false, tags: [], optional: null });
  await shots(page, flowEditor, "dual-declaration-flow-en", info, async () => {
    await expect(flowEditor.getByRole("button", { name: "Source editor", exact: true })).toBeEnabled();
    await flowEditor.getByRole("region", { name: "Flow map", exact: true }).getByRole("button", { name: "done", exact: true }).click();
  });

  const draft = await builder.page({ title: "Source page", object: model.type });
  await editor.open(builder, draft.id);
  await page.getByRole("group", { name: "Editing mode", exact: true }).getByRole("button", { name: "Source editor", exact: true }).click();
  const pageDeclaration = JSON.parse(await source.inputValue());
  pageDeclaration.title = "Source-edited page";
  pageDeclaration.sections[0].text = "Retained source content";
  await source.fill(JSON.stringify(pageDeclaration, null, 2));
  await page.getByRole("button", { name: "Apply source", exact: true }).click();
  await page.getByRole("group", { name: "Editing mode", exact: true }).getByRole("button", { name: "Visual", exact: true }).click();
  await editor.save();
  await expect.poll(async () => (await builder.record("build.page", draft.id)).sections[0].text).toBe("Retained source content");
  await shots(page, page.locator("body"), "dual-declaration-page-en", info, async () => {
    await expect(page.getByRole("region", { name: "Page", exact: true }).getByRole("button", { name: "Source editor", exact: true })).toBeEnabled();
  });
});
