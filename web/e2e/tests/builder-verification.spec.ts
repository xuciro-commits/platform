import { expect, fresh, slug, shots, test } from "./kit";

test("an unpublished application trials synthetic samples across controlled members and saves its scenario", async ({ page, builder, operator }, info) => {
  test.setTimeout(120_000);
  const name = slug("trial"), objectId = fresh("TRIAL"), type = `build.${name}`;
  await builder.decide("build.object.create", { type: "build.object", id: objectId }, {
    name, title: "Trial work order", fields: [
      { name: "qty", title: "Quantity", type: "integer" }, { name: "done", title: "Done", type: "boolean" },
    ], states: [{ name: "draft", title: "Draft" }],
    actions: [{ name: "complete", title: "Complete work order", from: ["draft"], roles: ["builder", "user"],
      transaction: { subject: type, guard: { op: ">", kind: "boolean", args: [{ kind: "integer", ref: "qty" }, { kind: "integer", lit: { value: 0 } }] }, message: "Quantity must be positive",
        steps: [{ name: "complete", kind: "update", target: { type, id: "$subject" }, changes: [{ field: "done", from: "=true" }] }] } }],
  });
  const application = await builder.application({ title: "Trial operations", resources: [{ app: "build", kind: "object", name: type }] });
  const original = await builder.record("build.object", objectId);
  expect(original.published ?? "").toBe("");
  await builder.open(page, `/project?id=${application.id}&area=verify`);
  const workspace = page.getByRole("region", { name: "Verification and delivery", exact: true });
  await expect(workspace.getByRole("heading", { name: "Draft trial", exact: true })).toBeVisible();
  await workspace.getByRole("checkbox", { name: "Include in application releases", exact: true }).uncheck();
  const sample = workspace.getByRole("region", { name: "Application samples", exact: true });
  const sampleName = slug("samples");
  await sample.getByRole("textbox", { name: "Sample set name", exact: true }).fill(sampleName);
  await sample.getByRole("textbox", { name: "Sample set title", exact: true }).fill("Completion examples");
  await sample.getByRole("textbox", { name: "Synthetic sample records (JSON)", exact: true }).fill(JSON.stringify([{ type, records: [{ id: "SAMPLE-1", qty: 2, done: false, state: "draft" }] }]));
  await sample.getByRole("button", { name: "Save application sample", exact: true }).click();
  await expect(sample.getByRole("status")).toHaveText("Application sample saved. Release review retains its version.");
  const fixture = await builder.read<any>("records/build.sample-set");
  const savedSample = fixture.records.find((record: any) => record.name === sampleName);
  expect(savedSample.samples[0].records[0].done).toBe(false);
  const me = await builder.read<any>("me"), user = await operator.read<any>("me");
  await workspace.getByRole("textbox", { name: "Comparison members (up to three, comma separated)", exact: true }).fill(`${me.principalId}, ${user.principalId}, sales-1`);
  await workspace.getByRole("textbox", { name: "Test plan name", exact: true }).fill("Complete work order regression");
  await workspace.getByRole("button", { name: "Add a test step", exact: true }).click();
  const step = workspace.getByRole("group", { name: "Test step 1", exact: true });
  await step.getByRole("textbox", { name: "Test record ID", exact: true }).fill("SAMPLE-1");
  await step.getByRole("combobox", { name: "Action", exact: true }).selectOption(`${type}.complete`);
  await workspace.getByRole("button", { name: "Add a test step", exact: true }).click();
  const read = workspace.getByRole("group", { name: "Test step 2", exact: true });
  await read.getByRole("textbox", { name: "Test record ID", exact: true }).fill("SAMPLE-1");
  await read.getByRole("combobox", { name: "Test step kind", exact: true }).selectOption("read");
  await workspace.getByRole("button", { name: "Save test plan", exact: true }).click();
  await expect(workspace.getByRole("status").filter({ hasText: "Test plan saved." })).toBeVisible();
  const response = page.waitForResponse(response => response.url().endsWith("/v1/simulate/candidate") && response.request().method() === "POST");
  await workspace.getByRole("button", { name: "Run isolated test", exact: true }).click();
  const run = await response;
  const result = await run.json();
  expect(result, JSON.stringify(result)).toHaveProperty("actors");
  expect(run.ok()).toBe(true);
  expect(result.actors.map((actor: any) => actor.steps[0].accepted)).toEqual([true, true, false]);
  expect(result.actors[0].steps[1].read.done).toBe(true);
  expect(result.actors.every((actor: any) => actor.recovered)).toBe(true);
  expect(result.actors[0].steps[0].trace.some((event: any) => event.kind === "rule" && event.reads.refs.qty.value === 2)).toBe(true);
  const comparison = workspace.getByRole("region", { name: "Role comparison", exact: true });
  await expect(comparison.getByRole("region", { name: me.principalId, exact: true })).toBeVisible();
  await expect(comparison.getByRole("region", { name: user.principalId, exact: true })).toBeVisible();
  await expect(comparison.getByRole("region", { name: "sales-1", exact: true }).getByText("Refused", { exact: false }).first()).toBeVisible();
  await shots(page, comparison, "builder-role-comparison", info);
  expect(await builder.record("build.object", objectId)).toEqual(original);
  expect(await builder.record("build.sample-set", savedSample.id)).toEqual(savedSample);
  expect((await builder.request.get(`/v1/records/${type}`, {headers:builder.headers})).status()).toBe(404);
  await workspace.getByRole("button", { name: "Reload saved plan", exact: true }).click();
  await expect(workspace.getByRole("textbox", { name: "Comparison members (up to three, comma separated)", exact: true })).toHaveValue(`${me.principalId}, ${user.principalId}, sales-1`);
});

test("a builder authors a draft state-triggered flow and trials it without publishing", async ({ page, builder, operator }, info) => {
  test.setTimeout(120_000);
  const applicationName = slug("flowtrialapp"), objectName = slug("flowtrialtask"), flowName = slug("flowtrial");
  const type = `build.${objectName}`;
  await builder.open(page, "/projects");
  await page.getByRole("button", { name: "New application", exact: true }).click();
  const createApplication = page.getByRole("dialog");
  await createApplication.getByRole("textbox", { name: "Name *", exact: true }).fill(applicationName);
  await createApplication.getByRole("textbox", { name: "What people call it *", exact: true }).fill("Task trial application");
  await createApplication.getByRole("button", { name: "Create", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Task trial application", exact: true }).first()).toBeVisible();
  const applications = await builder.read<{ records: { id: string; name: string }[] }>("records/build.app?limit=1000");
  const application = applications.records.find(record => record.name === applicationName)!;

  const projectSections = page.getByRole("tablist", { name: "Project sections", exact: true });
  await projectSections.getByRole("tab", { name: "Guided start", exact: true }).click();
  const guide = page.getByRole("region", { name: "Guided start", exact: true });
  await guide.getByRole("textbox", { name: "Business object title", exact: true }).fill("Trial task");
  await guide.getByRole("textbox", { name: "Business object name", exact: true }).fill(objectName);
  await guide.getByRole("button", { name: "Review suggested object draft", exact: true }).click();
  await page.getByRole("dialog").getByRole("button", { name: "Create", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Trial task", exact: true }).first()).toBeVisible();
  const objects = await builder.read<{ records: { id: string; name: string }[] }>("records/build.object?limit=1000");
  const objectID = objects.records.find(record => record.name === objectName)!.id;
  const objectSections = page.getByRole("tablist", { name: "Object sections", exact: true });
  const objectInspector = page.getByRole("region", { name: "Object inspector", exact: true });
  await objectSections.getByRole("tab", { name: "Properties", exact: true }).click();
  await page.getByRole("button", { name: "Add a property", exact: true }).click();
  await objectInspector.getByRole("textbox", { name: "What people call it", exact: true }).fill("Task label");
  await objectInspector.getByRole("textbox", { name: "Name", exact: true }).fill("label");
  await objectSections.getByRole("tab", { name: "Lifecycle", exact: true }).click();
  await page.getByRole("button", { name: "Add a state", exact: true }).click();
  await objectInspector.getByRole("textbox", { name: "What people call it", exact: true }).fill("Open");
  await objectInspector.getByRole("textbox", { name: "Name", exact: true }).fill("open");
  await page.getByRole("button", { name: "Add a state", exact: true }).click();
  await objectInspector.getByRole("textbox", { name: "What people call it", exact: true }).fill("Done");
  await objectInspector.getByRole("textbox", { name: "Name", exact: true }).fill("done");
  await objectSections.getByRole("tab", { name: "Actions", exact: true }).click();
  await page.getByRole("button", { name: "Add an action", exact: true }).click();
  await objectInspector.getByRole("button", { name: "Open action type", exact: true }).click();
  await page.getByRole("textbox", { name: "What people call it", exact: true }).fill("Complete trial task");
  await page.getByRole("textbox", { name: "Name", exact: true }).fill("complete");
  const actionStructure = page.getByRole("region", { name: "Action structure", exact: true });
  await actionStructure.getByRole("button", { name: /^Transaction/ }).click();
  const transaction = page.getByRole("group", { name: "Transaction step 1", exact: true });
  await transaction.getByRole("button", { name: "Add field assignment", exact: true }).click();
  await transaction.getByRole("combobox", { name: "Record field", exact: true }).selectOption("state");
  await transaction.getByRole("combobox", { name: "State or input value", exact: true }).selectOption("done");
  await page.getByRole("button", { name: "Save", exact: true }).click();
  await expect.poll(async () => (await builder.record("build.object", objectID)).actions?.[0]?.transaction?.steps[0].changes).toEqual([
    expect.objectContaining({ field: "state", expr: expect.objectContaining({ lit: { value: "done" } }) }),
  ]);

  // The source object and its action are still unpublished when the native
  // flow editor offers them and validates the saved flow.
  await builder.open(page, `/flow?id=new&application=${application.id}`);
  const flowInspector = page.getByRole("region", { name: "Flow inspector", exact: true });
  await flowInspector.getByRole("textbox", { name: "Workflow name", exact: true }).fill(flowName);
  await flowInspector.getByRole("textbox", { name: "Workflow title", exact: true }).fill("Complete open tasks");
  await flowInspector.getByRole("checkbox", { name: "Manual or API start", exact: true }).uncheck();
  await flowInspector.getByRole("combobox", { name: "Source object", exact: true }).selectOption(type);
  await flowInspector.getByRole("combobox", { name: "Start state", exact: true }).selectOption("open");
  const flowStructure = page.getByRole("region", { name: "Flow structure", exact: true });
  await flowStructure.getByRole("tab", { name: "Blocks", exact: true }).click();
  await flowStructure.getByRole("textbox", { name: "Search capabilities", exact: true }).fill("Complete trial task");
  await flowStructure.getByRole("button", { name: /^Complete trial task/ }).click();
  await page.keyboard.press("ControlOrMeta+s");
  await expect.poll(async () => (await builder.read<any>("records/build.process?limit=1000")).records.some((record: any) => record.name === flowName)).toBe(true);
  const processes = await builder.read<{ records: { id: string; name: string }[] }>("records/build.process?limit=1000");
  const flowID = processes.records.find(record => record.name === flowName)!.id;
  const validation = page.waitForResponse(response => response.url().endsWith("/v1/build/process/check") && response.request().method() === "POST");
  await page.getByRole("button", { name: "Validate", exact: true }).click();
  const validationResponse = await validation;
  expect(await validationResponse.json()).toMatchObject({ valid: true });

  await builder.open(page, `/project?id=${application.id}&area=behavior`);
  await page.getByRole("combobox", { name: "Add existing resource", exact: true }).selectOption(JSON.stringify({ kind: "flow", id: flowID }));
  await expect.poll(async () => (await builder.record("build.app", application.id)).resources).toContainEqual({ app: "build", kind: "flow", name: `build.${flowName}` });
  const originalObject = await builder.record("build.object", objectID);
  const originalFlow = await builder.record("build.process", flowID);
  expect(originalObject.published ?? "").toBe("");
  expect(originalFlow.published ?? "").toBe("");
  const originalDefinitions = await operator.definitions<any>();
  expect(originalDefinitions.some(definition => definition.ref.name === type || definition.ref.name === `build.${flowName}`)).toBe(false);

  await builder.open(page, `/project?id=${application.id}&area=verify`);
  const workspace = page.getByRole("region", { name: "Verification and delivery", exact: true });
  await workspace.getByRole("button", { name: "Add a test step", exact: true }).click();
  const create = workspace.getByRole("group", { name: "Test step 1", exact: true });
  await create.getByRole("combobox", { name: "Object type", exact: true }).selectOption(type);
  await create.getByRole("textbox", { name: "Test record ID", exact: true }).fill("TRIAL-TASK-1");
  await create.getByRole("combobox", { name: "Action", exact: true }).selectOption(`${type}.create`);
  await create.getByRole("textbox", { name: "Task label", exact: true }).fill("Synthetic task");
  await workspace.getByRole("button", { name: "Add a test step", exact: true }).click();
  const advance = workspace.getByRole("group", { name: "Test step 2", exact: true });
  await advance.getByRole("combobox", { name: "Object type", exact: true }).selectOption(type);
  await advance.getByRole("textbox", { name: "Test record ID", exact: true }).fill("TRIAL-TASK-1");
  await advance.getByRole("combobox", { name: "Test step kind", exact: true }).selectOption("clock");
  await advance.getByRole("spinbutton", { name: "Advance clock (seconds)", exact: true }).fill("2");
  await workspace.getByRole("button", { name: "Add a test step", exact: true }).click();
  const read = workspace.getByRole("group", { name: "Test step 3", exact: true });
  await read.getByRole("combobox", { name: "Object type", exact: true }).selectOption(type);
  await read.getByRole("textbox", { name: "Test record ID", exact: true }).fill("TRIAL-TASK-1");
  await read.getByRole("combobox", { name: "Test step kind", exact: true }).selectOption("read");
  await workspace.getByRole("textbox", { name: "Test plan name", exact: true }).fill("Draft flow trial");
  await workspace.getByRole("textbox", { name: "Test plan definition name", exact: true }).fill(slug("flowplan"));
  await workspace.getByRole("button", { name: "Save test plan", exact: true }).click();
  await expect(workspace.getByRole("status").filter({ hasText: "Test plan saved." })).toBeVisible();
  await workspace.getByRole("button", { name: "Reload saved plan", exact: true }).click();
  await expect(advance.getByRole("combobox", { name: "Test step kind", exact: true })).toHaveValue("clock");
  await expect(advance.getByRole("spinbutton", { name: "Advance clock (seconds)", exact: true })).toHaveValue("2");
  const savedApplication = await builder.record("build.app", application.id);
  const simulation = page.waitForResponse(response => response.url().endsWith("/v1/simulate/candidate") && response.request().method() === "POST");
  await workspace.getByRole("button", { name: "Run isolated test", exact: true }).click();
  const response = await simulation;
  const result = await response.json();
  expect(response.ok(), JSON.stringify(result)).toBe(true);
  expect(result.steps.map((step: any) => step.accepted)).toEqual([true, true, true]);
  expect(result.steps[2].read).toMatchObject({ id: "TRIAL-TASK-1", label: "Synthetic task", state: "done" });
  const execution = result.steps.flatMap((step: any) => step.flows ?? []).find((flow: any) => flow.flow === `build.${flowName}` && flow.state === "done");
  expect(execution).toBeTruthy();
  expect(execution.trace.length).toBeGreaterThan(0);
  expect(result.recovered).toBe(true);
  await expect(workspace.getByRole("status").filter({ hasText: "All expected outcomes matched." })).toBeVisible();
  await shots(page, workspace, "unpublished-flow-trial", info);
  expect(await builder.record("build.object", objectID)).toEqual(originalObject);
  expect(await builder.record("build.process", flowID)).toEqual(originalFlow);
  expect(await builder.record("build.app", application.id)).toEqual(savedApplication);
  expect(await operator.definitions<any>()).toEqual(originalDefinitions);
  expect((await builder.request.get(`/v1/records/${type}`, { headers: builder.headers })).status()).toBe(404);
});
