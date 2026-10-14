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

  await page.getByRole("button", { name: "Add a property", exact: true }).first().click();
  const inspector = page.getByRole("region", { name: "Object inspector", exact: true });
  await inspector.getByRole("textbox", { name: "What people call it", exact: true }).fill("Service note");
  await inspector.getByRole("textbox", { name: "Name", exact: true }).fill("note");
  const navigation = page.getByRole("navigation", { name: "Main", exact: true });
  const toggle = page.getByRole("button", { name: "Toggle navigation", exact: true });
  if (await toggle.getAttribute("aria-expanded") !== "true") await toggle.click();
  await navigation.getByRole("button", { name: "Interface", exact: true }).click();
  await expect(page.getByRole("tab", { name: "Interface", exact: true })).toHaveAttribute("aria-selected", "true");
  await navigation.getByRole("region", { name: "Application assets", exact: true }).getByRole("button", { name: /^Service case/ }).click();
  await expect(inspector.getByRole("textbox", { name: "What people call it", exact: true })).toHaveValue("Service note");
  await expect(page.getByRole("button", { name: "Save", exact: true })).toBeEnabled();
  await page.getByRole("button", { name: "Add a state", exact: true }).click();
  await inspector.getByRole("textbox", { name: "What people call it", exact: true }).fill("Open");
  await inspector.getByRole("textbox", { name: "Name", exact: true }).fill("open");
  await page.getByRole("button", { name: "Add an action", exact: true }).click();
  await page.getByRole("button", { name: "Save", exact: true }).click();
  await expect.poll(async () => (await builder.record("build.object", objectID)).actions?.length).toBe(1);

  const pageDraft = await builder.page({ title: "Service desk", object: `build.${objectName}`, sections: [{ id: "initial", widget: "text", configVersion: 1, text: "Service desk" }] });
  // Add existing uses the same membership decision as creating a page here.
  await context.getByRole("button", { name: "Interface", exact: true }).click();
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

  await navigation.getByRole("button", { name: "Verify", exact: true }).click();
  const verify = page.getByRole("region", { name: "Verification and delivery", exact: true });
  await verify.getByRole("button", { name: "Add the drafts it depends on", exact: true }).click();
  await verify.getByRole("button", { name: "Check joint candidate", exact: true }).click();
  await verify.getByRole("button", { name: "Save immutable candidate", exact: true }).click();
  await verify.getByRole("button", { name: "Activate release", exact: true }).click();
  await expect(verify.getByText("Current content version", { exact: true })).toBeVisible();
  const delivered = await operator.definitions<any>();
  const root = delivered.find(definition => definition.ref.kind === "app" && definition.ref.name === application.name);
  expect(root.application.concepts[0].title).toBe("Customer service");
  expect(delivered.some(definition => definition.ref.name === `build.${objectName}.${beforeMove.actions[0].name}`)).toBe(true);

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
