import { expect, test } from "@playwright/test";
import { decide, fresh, open } from "./host";

test("model resources keep native definitions read-only and bind incoming references into a released page", async ({ page, request }, testInfo) => {
  const suffix = fresh("model").replace(/[^a-z0-9]/gi, "").toLowerCase(), pageName = `model${suffix}`;
  const a = fresh("ACC"), b = fresh("ACC"), oa = fresh("OPP"), ob = fresh("OPP");
  for (const [account, opportunity, title] of [[a, oa, "MODEL-A"], [b, ob, "MODEL-B"]]) {
    await decide(request, "sales", "crm", "crm.account.create", { type: "crm.account", id: account }, { name: title, kind: "company" });
    await decide(request, "sales", "crm", "crm.opportunity.open", { type: "crm.opportunity", id: opportunity }, { account, title: `OPPORTUNITY-${title}` });
  }
  await decide(request, "manager", "platform", "platform.member.grant", { type: "platform.member", id: "sales-1" }, { app: "build", role: "user" });
  try {
    await open(page, "manager", "/model?object=crm.account");
    const inspector = page.getByRole("region", { name: "Semantic inspector", exact: true });
    const workspace = page.getByRole("region", { name: "Model workspace", exact: true });
    await page.getByLabel("Search model resources", { exact: true }).fill("crm.");
    await page.getByRole("button", { name: "Relationship graph", exact: true }).click();
    await workspace.getByRole("button", { name: "Account", exact: true }).dblclick();
    await expect(workspace.getByRole("heading", { name: "Account", exact: true })).toBeVisible();
    await expect(inspector.getByText("Native code", { exact: true })).toBeVisible();
    await expect(inspector.getByRole("button", { name: "Edit object", exact: true })).toHaveCount(0);
    await workspace.getByRole("tab", { name: "Properties", exact: true }).click();
    await workspace.getByRole("row").filter({ has: page.getByRole("cell", { name: "name", exact: true }) }).click();
    await expect(inspector.getByRole("button", { name: "Use property in page", exact: true })).toBeEnabled();
    await expect(inspector.getByRole("button", { name: "Edit property", exact: true })).toHaveCount(0);
    await workspace.getByRole("tab", { name: "Relationships", exact: true }).click();
    await workspace.getByRole("button").filter({ hasText: "crm.opportunity.account" }).click();
    await expect(inspector.getByRole("button", { name: "Use related records in page", exact: true })).toBeEnabled();
    if (process.env.PLATFORM_SCREENSHOTS) await page.screenshot({ path: testInfo.outputPath("model-reference-inspector.png"), fullPage: true });
    await inspector.getByRole("button", { name: "Use related records in page", exact: true }).click();
    const create = workspace.getByRole("region", { name: "Create page from model", exact: true });
    await create.getByLabel("Page name", { exact: true }).fill(pageName);
    await create.getByLabel("Page title", { exact: true }).fill("Model relationship desk");
    await create.getByRole("button", { name: "Create page draft", exact: true }).click();
    await expect(page.getByRole("heading", { name: "Model relationship desk", exact: true })).toBeVisible();
    const candidates = await (await request.get("/v1/records/build.page?limit=500", { headers: { Authorization: "Bearer manager" } })).json();
    const draft = candidates.records.find((record: { name: string }) => record.name === pageName);
    expect(draft.object).toBe("crm.account");
    expect(draft.sections[1]).toMatchObject({ object: "crm.opportunity", relation: "opportunities", parentSelection: "parent", selection: "related" });
    expect(draft.document.formatVersion).toBe(2);
    await page.getByRole("button", { name: "Review release", exact: true }).click();
    await page.getByRole("button", { name: "Check draft and dependencies", exact: true }).click();
    await page.getByRole("button", { name: "Save immutable candidate", exact: true }).click();
    await expect(page.getByRole("button", { name: "Activate release", exact: true })).toBeEnabled();
    await page.getByRole("button", { name: "Activate release", exact: true }).click();
    const operation = await page.context().newPage();
    await open(operation, "sales", `/page?app=build&kind=page&name=${pageName}`);
    await operation.getByRole("row").filter({ hasText: "MODEL-A" }).filter({ hasNotText: "OPPORTUNITY" }).click();
    await expect(operation.getByRole("row").filter({ hasText: "OPPORTUNITY-MODEL-A" })).toBeVisible();
    await expect(operation.getByRole("row").filter({ hasText: "OPPORTUNITY-MODEL-B" })).toHaveCount(0);
    await operation.getByRole("row").filter({ hasText: "OPPORTUNITY-MODEL-A" }).click();
    await expect(operation.getByRole("heading", { name: "OPPORTUNITY-MODEL-A", exact: true })).toBeVisible();
    await operation.reload();
    await operation.getByRole("row").filter({ hasText: "MODEL-B" }).filter({ hasNotText: "OPPORTUNITY" }).click();
    await expect(operation.getByRole("row").filter({ hasText: "OPPORTUNITY-MODEL-B" })).toBeVisible();
    await expect(operation.getByRole("row").filter({ hasText: "OPPORTUNITY-MODEL-A" })).toHaveCount(0);
  } finally {
    await decide(request, "manager", "platform", "platform.member.revoke", { type: "platform.member", id: "sales-1" }, { app: "build" });
  }
});

test("a selected model property becomes a typed V2 page field binding", async ({ page, request }) => {
  const name = fresh("fieldpage").replace(/[^a-z0-9]/gi, "").toLowerCase();
  await open(page, "manager", "/model?object=crm.account&tab=properties");
  const workspace = page.getByRole("region", { name: "Model workspace", exact: true });
  const inspector = page.getByRole("region", { name: "Semantic inspector", exact: true });
  await workspace.getByRole("row").filter({ has: page.getByRole("cell", { name: "name", exact: true }) }).click();
  await inspector.getByRole("button", { name: "Use property in page", exact: true }).click();
  const create = workspace.getByRole("region", { name: "Create page from model", exact: true });
  await create.getByLabel("Page name", { exact: true }).fill(name);
  await create.getByLabel("Page title", { exact: true }).fill("Selected property page");
  await create.getByRole("button", { name: "Create page draft", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Selected property page", exact: true })).toBeVisible();
  const records = (await (await request.get("/v1/records/build.page?limit=500", { headers: { Authorization: "Bearer manager" } })).json()).records;
  const draft = records.find((record: { name: string }) => record.name === name);
  expect(draft.sections.map((section: { fields: string[] }) => section.fields)).toEqual([["name"], ["name"]]);
  expect(draft.document.formatVersion).toBe(2);
});

test("tenant properties open the original object editor at the selected field", async ({ page, request }) => {
  const name = fresh("tenantmodel").replace(/[^a-z0-9]/gi, "").toLowerCase(), id = fresh("OBJ");
  await decide(request, "manager", "build", "build.object.create", { type: "build.object", id }, {
    name, title: "Tenant model probe", fields: [{ name: "note", title: "Original note", type: "text" }],
    access: [{ role: "user", read: "all", create: true, edit: true }],
  });
  await decide(request, "manager", "build", "build.object.publish", { type: "build.object", id }, {});
  await open(page, "manager", `/model?object=build.${name}&tab=properties`);
  const workspace = page.getByRole("region", { name: "Model workspace", exact: true });
  const inspector = page.getByRole("region", { name: "Semantic inspector", exact: true });
  await workspace.getByRole("row").filter({ hasText: "Original note" }).click();
  await inspector.getByRole("button", { name: "Edit property", exact: true }).click();
  const field = page.getByRole("region", { name: "The piece in hand", exact: true });
  await expect(field.getByLabel("Name", { exact: true })).toHaveValue("note");
  await field.getByLabel("What people call it", { exact: true }).fill("Updated note");
  await page.getByRole("button", { name: "Save", exact: true }).click();
  const record = (await (await request.get(`/v1/records/build.object/${id}`, { headers: { Authorization: "Bearer manager" } })).json()).record;
  expect(record.fields[0]).toMatchObject({ name: "note", title: "Updated note" });
  // Saving acknowledges the draft without removing the editing history.
  await page.getByRole("button", { name: "Undo", exact: true }).click();
  const outline = page.getByRole("region", { name: "States and actions", exact: true });
  await outline.getByRole("button").filter({ hasText: "Original note" }).click();
  await expect(field.getByLabel("What people call it", { exact: true })).toHaveValue("Original note");
  await page.getByRole("button", { name: "Redo", exact: true }).click();
  await outline.getByRole("button").filter({ hasText: "Updated note" }).click();
  await expect(field.getByLabel("What people call it", { exact: true })).toHaveValue("Updated note");
  await expect(page.getByRole("button", { name: "Save", exact: true })).toBeDisabled();
  const operator = await page.context().newPage();
  await open(operator, "desk", `/model?object=build.${name}`);
  await expect(operator.getByRole("region", { name: "Semantic inspector", exact: true })).toHaveCount(0);
  await expect(operator.getByRole("button", { name: "Application Studio", exact: true })).toHaveCount(0);
  await expect(decide(request, "desk", "build", "build.object.edit", { type: "build.object", id }, { title: "Unauthorized" })).rejects.toThrow(/POLICY_DENIED/);
});

test("object editor retains its base revision and local fields after a concurrent save", async ({ page, request }) => {
  const name = fresh("modelconflict").replace(/[^a-z0-9]/gi, "").toLowerCase(), id = fresh("OBJ");
  await decide(request, "manager", "build", "build.object.create", { type: "build.object", id }, {
    name, title: "Concurrent model", fields: [{ name: "note", title: "Saved note", type: "text" }],
  });
  await open(page, "manager", `/process?id=${id}&field=note`);
  const field = page.getByRole("region", { name: "The piece in hand", exact: true });
  await field.getByLabel("What people call it", { exact: true }).fill("Local note");
  await decide(request, "manager", "build", "build.object.edit", { type: "build.object", id }, {
    fields: [{ name: "note", title: "Remote note", type: "text" }],
  });
  for (let attempt = 0; attempt < 2; attempt++) {
    await page.getByRole("button", { name: "Save", exact: true }).click();
    await expect(page.getByRole("alert").filter({ hasText: /CONFLICT|changed|revision/i })).toBeVisible();
    await expect(field.getByLabel("What people call it", { exact: true })).toHaveValue("Local note");
  }
  const record = (await (await request.get(`/v1/records/build.object/${id}`, { headers: { Authorization: "Bearer manager" } })).json()).record;
  expect(record.fields[0].title).toBe("Remote note");
  await page.getByRole("button", { name: "Cancel changes", exact: true }).click();
  const outline = page.getByRole("region", { name: "States and actions", exact: true });
  await outline.getByRole("button").filter({ hasText: "Remote note" }).click();
  await expect(field.getByLabel("What people call it", { exact: true })).toHaveValue("Remote note");
});
