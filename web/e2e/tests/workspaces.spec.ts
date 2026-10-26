import { readFile } from "node:fs/promises";
import { expect, fresh, pageUIProfile, selectObjectSection, shots, test } from "./kit";
import { switchWorkspace } from "./host";

test.use({ actionTimeout: 10_000 });

test("Builder keeps its complete menus, original object and action editors, and application workspaces", async ({ page, builder }, testInfo) => {
  test.setTimeout(60_000);
  const suffix = fresh("navigation");
  const objectTitle = `Navigation item ${suffix}`, actionTitle = `Complete navigation item ${suffix}`;
  const object = await builder.object({
    title: objectTitle, fields: [{ name: "name", title: "Item name", type: "text" }],
    states: [{ name: "open", title: "Open" }, { name: "done", title: "Done" }],
    actions: [{ name: "complete", title: actionTitle, from: ["open"], to: "done" }],
  });
  const application = await builder.application({ title: `Navigation application ${suffix}`, resources: [{ app: "build", kind: "object", name: object.type }] });
  await builder.open(page, "/projects");
  const toggle = page.getByRole("button", { name: "Toggle navigation", exact: true });
  const showNavigation = async () => { if (await toggle.getAttribute("aria-expanded") !== "true") await toggle.click(); };
  const navigation = page.getByRole("navigation", { name: "Main", exact: true });
  await expect(navigation).toBeVisible();
  for (const group of ["Applications", "Model", "Behavior", "Interface", "Connections", "Release"]) {
    await expect(navigation.getByText(group, { exact: true }).first()).toBeVisible();
  }
  for (const entry of ["All applications", "Object types", "Link types", "Shared properties", "Queries", "Pages", "Templates", "Object behavior", "Logic flows", "Automations", "AI functions", "Code functions", "Decision tables", "Agents", "Alert rules", "Connections", "Observation sources", "Data sources", "Datasets", "Pipelines", "Matching rules", "Writebacks", "Integration health", "Sample sets", "Business test plans", "Evaluation sets", "Changes", "Release review", "Release history"]) {
    await expect(navigation.getByRole("button", { name: entry, exact: true })).toBeVisible();
  }
  await shots(page, navigation, "builder-menus", testInfo, showNavigation);
  await showNavigation();

  await navigation.getByRole("button", { name: "Object types", exact: true }).click();
  await page.getByRole("textbox", { name: "Search model resources", exact: true }).fill(objectTitle);
  await page.getByRole("row").filter({ hasText: object.type }).click();
  await page.getByRole("button", { name: "Edit object", exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`[?&]id=${object.id}(?:&|$)`));
  await selectObjectSection(page, "Properties");
  await page.getByRole("row").filter({ hasText: "Item name" }).click();
  const propertyTitle = page.getByRole("region", { name: "Object inspector", exact: true }).getByRole("textbox", { name: "What people call it", exact: true });
  await expect(propertyTitle).toBeEnabled();
  await propertyTitle.fill("Menu-edited item name");
  await page.getByRole("button", { name: "Save", exact: true }).click();
  await expect.poll(async () => (await builder.record("build.object", object.id)).fields.find((field: any) => field.name === "name")?.title).toBe("Menu-edited item name");

  await navigation.getByRole("button", { name: "Object behavior", exact: true }).click();
  await page.getByRole("treeitem", { name: new RegExp(`^${actionTitle}\\b`) }).click();
  await expect(page).toHaveURL(new RegExp(`[?&]id=${object.id}(?:&|$)`));
  await expect(page).toHaveURL(/[?&]action=complete(?:&|$)/);
  await page.getByRole("region", { name: "Action type", exact: true }).getByRole("group", { name: "Editing mode", exact: true }).getByRole("button", { name: "Visual", exact: true }).click();
  await expect(page.getByRole("region", { name: "Action structure", exact: true }).getByRole("button", { name: "Overview", exact: true })).toBeVisible();
  const actionName = page.getByRole("textbox", { name: "What people call it", exact: true });
  await expect(actionName).toBeEnabled();
  await actionName.fill(`${actionTitle} edited`);
  await page.getByRole("button", { name: "Save", exact: true }).click();
  await expect.poll(async () => (await builder.record("build.object", object.id)).actions.find((action: any) => action.name === "complete")?.title).toBe(`${actionTitle} edited`);
  expect((await builder.record("build.object", object.id)).fields.find((field: any) => field.name === "name")?.title).toBe("Menu-edited item name");

  await builder.open(page, `/project?id=${application.id}`);
  const workspaces = navigation.getByRole("navigation", { name: "Application workspaces", exact: true });
  for (const area of ["Model", "Behavior", "Interface", "Connections", "Release"]) {
    await expect(workspaces.getByRole("button", { name: area, exact: true })).toBeVisible();
  }
  await workspaces.getByRole("button", { name: "Behavior", exact: true }).click();
  await expect(workspaces.getByRole("button", { name: "Behavior", exact: true })).toHaveAttribute("aria-current", "page");
});

test("system definitions are opt-in across lists and keep native references and tenant drafts separate", async ({ page, builder }, testInfo) => {
  test.setTimeout(60_000);
  const suffix = fresh("systemlists").replace(/[^a-z0-9]/gi, "").toLowerCase();
  const object = await builder.object({ title: `System list source ${suffix}`, fields: [{ name: "note", title: "Note", type: "text" }] });
  const queryID = fresh("QUERY"), queryTitle = `Tenant query ${suffix}`;
  await builder.decide("build.query.create", { type: "build.query", id: queryID }, {
    name: `query${suffix}`, title: queryTitle, description: "Retain this tenant query when system references are hidden", object: object.type, limit: 20, sort: ["id"], domain: [],
  });
  const functionID = fresh("FUNCTION"), functionTitle = `Tenant advice ${suffix}`;
  await builder.decide("build.function.create", { type: "build.function", id: functionID }, {
    name: `advice${suffix}`, title: functionTitle, description: "Retain this tenant AI draft", object: object.type, fields: ["note"],
    instructions: "Summarise the supplied note", output: [{ name: "summary", type: "string", required: true }],
    maxInputBytes: 4096, maxOutputBytes: 1024, maxTokens: 256, roles: ["builder", "user"],
  });
  const flowID = fresh("FLOW"), flowTitle = `Tenant flow ${suffix}`;
  await builder.decide("build.process.create", { type: "build.process", id: flowID }, {
    name: `flow${suffix}`, title: flowTitle, manual: true, steps: [{ name: "done", kind: "end" }],
  });
  const definitions = await builder.definitions();
  expect(definitions.find(definition => definition.ref.app === "core" && definition.ref.name === "core.material")?.entity.fields).toEqual(expect.arrayContaining([expect.objectContaining({ name: "baseUom", type: "reference", ref: "core.uom" })]));
  expect(definitions.find(definition => definition.ref.app === "core" && definition.ref.name === "core.location")?.entity.fields).toEqual(expect.arrayContaining([expect.objectContaining({ name: "site", type: "reference", ref: "core.site" })]));
  // The retired applications supplied the former code queries, AI and flows.
  // Their absence is intentional; tenant drafts still remain in every list.
  expect(definitions.filter(definition => definition.source === "code" && ["query", "function", "flow"].includes(definition.ref.kind))).toEqual([]);
  const nativeAction = definitions.find(definition => definition.source === "code" && definition.ref.kind === "action" && !!definition.action);
  expect(nativeAction).toBeTruthy();

  await builder.open(page, "/link-type");
  const navigation = page.getByRole("navigation", { name: "Main", exact: true });
  const relationships = page.getByRole("region", { name: "Relationships", exact: true });
  const checkbox = relationships.getByRole("checkbox", { name: "Show system definitions", exact: true });
  const references = relationships.getByRole("region", { name: "System definitions", exact: true });
  await expect(checkbox).not.toBeChecked();
  await expect(references).toHaveCount(0);
  await checkbox.check();
  const materialReference = references.getByRole("treeitem", { name: /^Material · Base unit/ });
  const locationReference = references.getByRole("treeitem", { name: /^Location · Site/ });
  await expect(materialReference).toContainText("baseUom → Unit of measure");
  await expect(locationReference).toContainText("site → Site");
  await shots(page, page.locator("body"), "system-relationship-references", testInfo);
  await materialReference.click();
  await expect.poll(() => ({ route: new URL(page.url()).hash.split("?")[0], ...Object.fromEntries(new URLSearchParams(new URL(page.url()).hash.split("?")[1])) })).toMatchObject({ route: "#/object-type", object: "core.material", tab: "links" });
  await expect(page.getByRole("tablist", { name: "Object resource views", exact: true }).getByRole("tab", { name: "Relationships", exact: true })).toHaveAttribute("aria-selected", "true");
  await expect(page.getByText("Native definitions are maintained by their code owner. This workbench provides read-only inspection.", { exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "Edit object", exact: true })).toHaveCount(0);

  await navigation.getByRole("button", { name: "Link types", exact: true }).click();
  await expect(checkbox).toBeChecked();
  await locationReference.click();
  await expect.poll(() => ({ route: new URL(page.url()).hash.split("?")[0], ...Object.fromEntries(new URLSearchParams(new URL(page.url()).hash.split("?")[1])) })).toMatchObject({ route: "#/object-type", object: "core.location", tab: "links" });
  await expect(page.getByRole("tablist", { name: "Object resource views", exact: true }).getByRole("tab", { name: "Relationships", exact: true })).toHaveAttribute("aria-selected", "true");
  await expect(page.getByRole("button", { name: "Edit object", exact: true })).toHaveCount(0);

  await navigation.getByRole("button", { name: "Queries", exact: true }).click();
  const queries = page.getByRole("region", { name: "Queries", exact: true });
  await expect(queries.getByRole("checkbox", { name: "Show system definitions", exact: true })).toBeChecked();
  const queryReferences = queries.getByRole("region", { name: "System definitions", exact: true });
  await expect(queryReferences.getByText("No system definitions are available in this category.", { exact: true })).toBeVisible();
  await expect(queries.getByRole("treeitem").filter({ hasText: queryTitle }).last()).toBeVisible();
  await queries.getByRole("button", { name: "Table", exact: true }).click();
  await expect(queryReferences).toBeVisible();
  await expect(queries.getByRole("checkbox", { name: "Show system definitions", exact: true })).toBeChecked();
  await queries.getByRole("button", { name: "Tree", exact: true }).click();

  await navigation.getByRole("button", { name: "AI functions", exact: true }).click();
  const functions = page.getByRole("region", { name: "AI functions", exact: true });
  const functionCheckbox = functions.getByRole("checkbox", { name: "Show system definitions", exact: true });
  await expect(functionCheckbox).toBeChecked();
  const functionReferences = functions.getByRole("region", { name: "System definitions", exact: true });
  await expect(functionReferences.getByText("No system definitions are available in this category.", { exact: true })).toBeVisible();

  await navigation.getByRole("button", { name: "Logic flows", exact: true }).click();
  const flows = page.getByRole("region", { name: "Flows", exact: true });
  await expect(flows.getByRole("checkbox", { name: "Show system definitions", exact: true })).toBeChecked();
  const flowReferences = flows.getByRole("region", { name: "System definitions", exact: true });
  await expect(flowReferences.getByText("No system definitions are available in this category.", { exact: true })).toBeVisible();
  await expect(flows.getByRole("treeitem").filter({ hasText: flowTitle }).last()).toBeVisible();

  await navigation.getByRole("button", { name: "Object types", exact: true }).click();
  await expect(page.getByRole("checkbox", { name: "Show system definitions", exact: true })).toBeChecked();
  const modelSearch = page.getByRole("textbox", { name: "Search model resources", exact: true });
  await modelSearch.fill("core.material");
  await expect(page.getByRole("row").filter({ hasText: "core.material" })).toBeVisible();
  await page.getByRole("checkbox", { name: "Show system definitions", exact: true }).uncheck();
  await expect(page.getByRole("row").filter({ hasText: "core.material" })).toHaveCount(0);
  await navigation.getByRole("button", { name: "Object behavior", exact: true }).click();
  const publishedActions = page.getByRole("region", { name: "Published action definitions", exact: true });
  const nativeActionKey = [nativeAction.ref.app, nativeAction.ref.kind, nativeAction.ref.name].join("/");
  await publishedActions.getByRole("textbox", { name: "Search", exact: true }).fill(nativeActionKey);
  await expect(publishedActions.getByRole("treeitem").filter({ hasText: nativeActionKey }).last()).toHaveCount(0);
  await page.getByRole("checkbox", { name: "Show system definitions", exact: true }).check();
  await expect(publishedActions.getByRole("treeitem").filter({ hasText: nativeActionKey }).last()).toBeVisible();

  await navigation.getByRole("button", { name: "AI functions", exact: true }).click();
  await functionCheckbox.uncheck();
  await expect(functionReferences).toHaveCount(0);
  await expect(functions.getByRole("treeitem").filter({ hasText: functionTitle }).last()).toBeVisible();
  await navigation.getByRole("button", { name: "Queries", exact: true }).click();
  await expect(queries.getByRole("checkbox", { name: "Show system definitions", exact: true })).not.toBeChecked();
  await expect(queryReferences).toHaveCount(0);
  await expect(queries.getByRole("treeitem").filter({ hasText: queryTitle }).last()).toBeVisible();
  await navigation.getByRole("button", { name: "Link types", exact: true }).click();
  await expect(checkbox).not.toBeChecked();
  await expect(references).toHaveCount(0);
  expect((await builder.record("build.query", queryID)).state).toBe("draft");
  expect((await builder.record("build.function", functionID)).state).toBe("draft");
  expect((await builder.record("build.process", flowID)).state).toBe("draft");
});

test("platform apps expose their own primary functions and keep one menu owner", async ({ page, builder }, testInfo) => {
  test.setTimeout(90_000);
  await builder.decide("enterprise.element.add", { type: "enterprise.element", id: fresh("navigationorg") }, { stereotype: "ActualOrganization", name: "Navigation organization", kind: "team" }, "enterprise");
  const navigationPage = await builder.page({ title: "Navigation page", object: "core.material" });
  const navigationApp = await builder.application({ title: "Navigation application", pages: [navigationPage.name] });
  await builder.activate(await builder.candidate([{ kind: "page", id: navigationPage.id }, { kind: "app", id: navigationApp.id }]));
  const savedTitle = `Navigation saved view ${fresh("saved")}`, savedID = fresh("VIEW");
  await builder.decide("work.view.save", { type: "work.view", id: savedID }, { title: savedTitle, entity: "core.material", state: "{}" }, "work");
  await builder.open(page, "/home");
  const switcher = page.getByRole("button", { name: "Switch application", exact: true });
  const menu = page.getByRole("menu");
  const openSwitcher = async () => { if (!await menu.isVisible()) await switcher.click(); };
  await openSwitcher();
  for (const category of ["Business applications", "Knowledge and data", "Build and intelligence", "Run and manage"]) {
    await expect(menu.getByText(category, { exact: true })).toBeVisible();
  }
  for (const title of ["Builder", "Data Connection", "Agents", "Enterprise", "Master data", "My work"]) {
    await expect(menu.getByRole("menuitemradio", { name: title, exact: true })).toHaveCount(1);
  }
  await shots(page, menu, "platform-apps", testInfo, openSwitcher);
  await page.keyboard.press("Escape");

  const toggle = page.getByRole("button", { name: "Toggle navigation", exact: true });
  const navigation = page.getByRole("navigation", { name: "Main", exact: true });
  const choose = async (title: string) => {
    await switchWorkspace(page, title);
    if (await toggle.getAttribute("aria-expanded") !== "true") await toggle.click();
    await expect(switcher).toContainText(title);
  };
  const entries = async (labels: string[]) => {
    for (const label of labels) await expect(navigation.getByRole("button", { name: label, exact: true })).toBeVisible();
  };
  await choose("Builder");
  await entries(["Object types", "Object behavior", "Connections", "Agents", "Sample sets", "Business test plans", "Evaluation sets"]);
  for (const label of ["Object Explorer", "Asset Library"]) {
    await expect(navigation.getByRole("button", { name: label, exact: true })).toHaveCount(0);
  }

  await choose("Data Connection");
  await expect(navigation.getByRole("button", { name: savedTitle, exact: true })).toHaveCount(0);
  await entries(["Connectors", "Endpoints", "Observations", "Lineage", "Outbound effects"]);
  for (const label of ["Connections", "Observation sources", "Data sources", "Datasets", "Pipelines", "Matching rules", "Writebacks", "Integration health"]) {
    await expect(navigation.getByRole("button", { name: label, exact: true })).toHaveCount(0);
  }
  await navigation.getByRole("button", { name: "Endpoints", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Endpoints", exact: true })).toBeVisible();
  await navigation.getByRole("button", { name: "Lineage", exact: true }).click();
  await expect(switcher).toContainText("Data Connection");
  await shots(page, navigation, "data-menus", testInfo, async () => { if (await toggle.getAttribute("aria-expanded") !== "true") await toggle.click(); });

  await choose("Agents");
  await entries(["Agents", "Agent runs", "Agent memories", "Evaluations", "Assistant"]);
  await expect(navigation.getByRole("button", { name: "Agent definitions", exact: true })).toHaveCount(0);
  await navigation.getByRole("button", { name: "Agent memories", exact: true }).click();
  await expect(page).toHaveURL(/#\/agent-memories$/);
  await choose("AI assistance");
  await entries(["Playground", "Usage", "Providers and models"]);
  await choose("Enterprise");
  await entries(["Enterprise", "Organisation management", "Business usage", "Tenant links"]);
  await navigation.getByRole("button", { name: "Organisation management", exact: true }).click();
  await expect(page.getByRole("combobox", { name: "Element status", exact: true })).toBeVisible();
  await choose("Master data");
  await entries(["Object Explorer", savedTitle]);
  await expect(page).toHaveURL(/#\/master-data$/);
  await navigation.getByRole("button", { name: savedTitle, exact: true }).click();
  await expect(page).toHaveURL(new RegExp(`#/saved\\?id=${savedID}$`));
  await expect(switcher).toContainText("Master data");
  await choose("Control Panel");
  await entries(["Members", "Roles and permissions", "Packages", "App settings", "Audit trail"]);
  for (const label of ["Enterprise", "Providers and models", "Outbox"]) {
    await expect(navigation.getByRole("button", { name: label, exact: true })).toHaveCount(0);
  }

  await choose("Runs");
  await entries(["Workflow runs", "Work queue", "Delivery attempts", "Health"]);
  for (const label of ["Work queue", "Delivery attempts", "Health"]) {
    await navigation.getByRole("button", { name: label, exact: true }).click();
    await expect(page.getByRole("heading", { name: label, exact: true })).toBeVisible();
  }
  await choose("Knowledge");
  await entries(["Documents", "Glossary"]);
  await choose("My work");
  await entries(["Inbox", "My requests", "Notifications", "Outbox"]);
  await navigation.getByRole("button", { name: "My requests", exact: true }).click();
  await expect(page.getByRole("heading", { name: "My requests", exact: true })).toBeVisible();
});

test("task workspaces preserve platform applications and hand off a controlled receiving application", async ({ browser, page, builder, operator, editor }, testInfo) => {
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
  await expect(page.getByRole("button", { name: "Reload current module", exact: true })).toBeEnabled();

  // Master data and Knowledge remain task-owned, not builder-only aliases.
  await builder.open(page, "/home");
  await page.getByRole("button", { name: "Operate", exact: true }).click();
  await expect(page.getByRole("button", { name: /^Master data\b/ }).first()).toBeVisible();
  await expect(page.getByRole("button", { name: "Platform Catalog", exact: true })).toHaveCount(0);
  await page.getByRole("button", { name: /^Master data\b/ }).first().click();
  await page.getByRole("navigation", { name: "Main", exact: true }).getByRole("button", { name: "Materials", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Materials", exact: true })).toBeVisible();
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
  await document.getByRole("textbox", { name: "Read by members of", exact: true }).fill("build");
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
  expect(await builder.record("knowledge.document", documentID)).toMatchObject({ title: documentTitle, text: "Check the receipt number before accepting delivery.", source: "Receiving desk", apps: ["build"] });
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
  await term.getByRole("textbox", { name: "Read by members of", exact: true }).fill("build");
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
  await expect(term.getByText("Create term was refused", { exact: false })).toBeVisible();
  const details = term.getByRole("button", { name: "Technical details", exact: true });
  if (await details.count()) await details.click();
  await expect(term.getByText("ERROR_CODE_INVALID_ARGUMENT", { exact: false })).toBeVisible();
  await expect(term.getByRole("textbox", { name: "ID *", exact: true })).toHaveValue(termID);
  await expect(term.getByRole("textbox", { name: "Term *", exact: true })).toHaveValue(termName);
  await expect(term.getByRole("textbox", { name: "Meaning *", exact: true })).toHaveValue("Accepting an incoming delivery");
  await expect(term.getByRole("textbox", { name: "Synonyms", exact: true })).toHaveValue("Goods receipt, delivery acceptance");
  await expect(term.getByRole("textbox", { name: "Refers to", exact: true })).toHaveValue(missingDeclaration);
  await expect(term.getByRole("button", { name: "Remove build", exact: true })).toBeVisible();
  expect(await builder.record("knowledge.term", termID)).toBeUndefined();
  await term.getByRole("textbox", { name: "Refers to", exact: true }).fill(object.type);
  await term.getByRole("button", { name: "Submit corrected inputs", exact: true }).click();
  await expect(term).toHaveCount(0);
  expect(termRequests).toHaveLength(2);
  expect(termRequests[1]!.body.target).toEqual(termRequests[0]!.body.target);
  expect(termRequests[1]!.body.idempotencyKey).not.toBe(termRequests[0]!.body.idempotencyKey);
  expect(termRequests[1]!.correction).toBe(termRequests[0]!.body.idempotencyKey);
  expect(await builder.record("knowledge.term", termID)).toMatchObject({ term: termName, meaning: "Accepting an incoming delivery", synonyms: "Goods receipt, delivery acceptance", refersTo: object.type, apps: ["build"] });
  await expect(page.getByRole("row").filter({ hasText: termName })).toBeVisible();

  await switchWorkspace(page, "Builder");
  await expect(page.getByRole("button", { name: "Switch application", exact: true })).toContainText("Builder");
  await page.goto(`/#/project?id=${appID}`); // preserve the supported old deep link
  await editor.release();
  await expect(page.getByText("Release active for operators.", { exact: false })).toBeVisible();

  // Resource controls share one archive operation and keep discarded edits local.
  await builder.open(page, `/project?id=${appID}&area=logic`);
  await page.getByRole("button", { name: "New", exact: true }).click();
  await page.getByRole("menuitem", { name: "Alert rule", exact: true }).click();
  await page.getByRole("group", { name: "Editing mode", exact: true }).getByRole("button", { name: "Visual", exact: true }).click();
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
  await page.getByRole("group", { name: "Editing mode", exact: true }).getByRole("button", { name: "Visual", exact: true }).click();
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
  await page.getByRole("group", { name: "Editing mode", exact: true }).getByRole("button", { name: "Visual", exact: true }).click();
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
  await nav.getByRole("button", { name: "Release", exact: true }).click();
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
    await expect(business.getByRole("heading", { name: "Control Panel", exact: true })).toBeVisible();
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
    const otherRecordID = fresh("RECEIPT");
    await operator.decide(`${object.type}.create`, { type: object.type, id: otherRecordID }, { number: "OTHER-M1" });
    await expect(business.getByRole("row").filter({ hasText: "OTHER-M1" })).toBeVisible();
    await business.getByRole("textbox", { name: "Search", exact: true }).fill("IN-M1");
    await expect(business.getByRole("row").filter({ hasText: "OTHER-M1" })).toHaveCount(0);
    const exported = business.waitForEvent("download", { timeout: 10_000 });
    await business.getByRole("button", { name: "Export CSV", exact: true }).click();
    const csv = await readFile((await (await exported).path())!, "utf8");
    expect(csv).toContain(recordID);
    expect(csv).toContain("IN-M1");
    expect(csv).not.toContain(otherRecordID);
    expect(csv).not.toContain("OTHER-M1");
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
