import { expect, shots, slug, test } from "./kit";

test("an application authors related drafts and a template before its first release", async ({ page, builder, operator, editor }, testInfo) => {
  test.setTimeout(180_000);
  const application = await builder.application({ title: "Service visits" });
  const parentName = slug("customer"), childName = slug("visit"), pageName = slug("visitdesk");
  const parentType = `build.${parentName}`, childType = `build.${childName}`;
  const projectSections = page.getByRole("tablist", { name: "Project sections", exact: true });
  const objectSections = page.getByRole("tablist", { name: "Object sections", exact: true });
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
    await objectSections.getByRole("tab", { name: "Properties", exact: true }).click();
    await page.getByRole("button", { name: "Add a property", exact: true }).click();
    await objectInspector.getByRole("textbox", { name: "What people call it", exact: true }).fill(title);
    await objectInspector.getByRole("textbox", { name: "Name", exact: true }).fill(name);
  };
  const allowOperators = async () => {
    await objectSections.getByRole("tab", { name: "Permissions", exact: true }).click();
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
  await objectSections.getByRole("tab", { name: "Lifecycle", exact: true }).click();
  await page.getByRole("button", { name: "Add a state", exact: true }).click();
  await objectInspector.getByRole("textbox", { name: "What people call it", exact: true }).fill("Open");
  await objectInspector.getByRole("textbox", { name: "Name", exact: true }).fill("open");
  await allowOperators();
  await objectSections.getByRole("tab", { name: "Actions", exact: true }).click();
  await page.getByRole("button", { name: "Add an action", exact: true }).click();
  await objectInspector.getByRole("button", { name: "Open action type", exact: true }).click();
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
  await navigation.getByRole("button", { name: "Verify", exact: true }).click();
  const verify = page.getByRole("region", { name: "Verification and delivery", exact: true });
  await verify.getByRole("button", { name: "Add the drafts it depends on", exact: true }).click();
  await verify.getByRole("button", { name: "Check joint candidate", exact: true }).click();
  await expect(verify.getByRole("button", { name: "Save immutable candidate", exact: true })).toBeVisible();
  await verify.getByRole("button", { name: "Save immutable candidate", exact: true }).click();
  await verify.getByRole("button", { name: "Activate release", exact: true }).click();
  await expect(verify.getByText("Release active for operators.", { exact: false })).toBeVisible();
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
  await expect.poll(async () => (await builder.record("build.page", pageID)).sections.find((section: any) => section.widget === "table").model.fields.some((field: any) => field.name === "followup")).toBe(true);
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
});
