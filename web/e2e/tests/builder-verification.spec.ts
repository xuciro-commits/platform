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
