import { expect, fresh, shots, slug, test } from "./kit";

test("shared published resources retain their owners through application discovery and delivery", async ({ page, builder, operator }, info) => {
  test.setTimeout(120_000);
  const before = await builder.definitions<any>();
  const operatorKeys = new Set((await operator.definitions<any>()).map(definition => JSON.stringify(definition.ref)));
  const action = before.find(definition => definition.ref.app !== "build" && definition.action && !operatorKeys.has(JSON.stringify(definition.ref)));
  const fn = before.find(definition => definition.ref.app !== "build" && definition.function);
  const flow = before.find(definition => definition.ref.app !== "build" && definition.flow);
  expect(action).toBeTruthy(); expect(fn).toBeTruthy(); expect(flow).toBeTruthy();
  const resources = [{ definition: action, title: action.action.title }, { definition: fn, title: fn.function.title }, { definition: flow, title: flow.flow.title }];
  const missing = { app: "build", kind: "action", name: slug("unavailable") };
  const application = await builder.application({ title: "Shared service resources", resources: [missing] });
  await builder.open(page, `/project?id=${application.id}&area=behavior&asset=${encodeURIComponent(`${missing.app}/${missing.kind}/${missing.name}`)}`);
  await expect(page.getByText(`${missing.name} is listed, but no readable draft or published definition is available. Check its owner and access, or remove the reference.`, { exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "Create it", exact: true })).toHaveCount(0);
  await page.getByRole("button", { name: "Remove from project", exact: true }).click();
  await expect.poll(async () => (await builder.record("build.app", application.id)).resources ?? []).toEqual([]);

  // Membership uses the existing application action. References have no
  // tenant draft to edit, and adding them must not change their original owner.
  for (const resource of resources) {
    await page.getByRole("combobox", { name: "Add existing resource", exact: true }).selectOption(JSON.stringify({ kind: "shared", ref: resource.definition.ref }));
    await expect.poll(async () => (await builder.record("build.app", application.id)).resources).toContainEqual(resource.definition.ref);
    const navigation = page.getByRole("navigation", { name: "Main", exact: true });
    const toggle = page.getByRole("button", { name: "Toggle navigation", exact: true });
    if (await toggle.getAttribute("aria-expanded") !== "true") await toggle.click();
    await navigation.getByRole("region", { name: "Application assets", exact: true }).getByRole("button", { name: new RegExp(`^${resource.title.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}`) }).click();
    await expect(page.getByRole("heading", { name: resource.title, exact: true })).toBeVisible();
    await expect(page.getByRole("region", { name: "Published resource", exact: true })).toBeVisible();
    const diagram = page.getByRole("group", { name: "Dependency diagram", exact: true });
    await expect(diagram).toHaveAttribute("aria-busy", "false");
    await shots(page, page.locator("body"), `shared-${resource.definition.ref.kind}`, info);
    await page.getByRole("navigation", { name: "Application context", exact: true }).getByRole("button", { name: "Shared service resources", exact: true }).click();
  }

  await page.getByRole("navigation", { name: "Main", exact: true }).getByRole("button", { name: "Verify", exact: true }).click();
  const review = page.getByRole("region", { name: "Verification and delivery", exact: true });
  await review.getByRole("button", { name: "Check joint candidate", exact: true }).click();
  await review.getByRole("button", { name: "Save immutable candidate", exact: true }).click();
  await review.getByRole("button", { name: "Activate release", exact: true }).click();
  await expect(review.getByText("Release active for operators.", { exact: false })).toBeVisible();
  const delivered = (await builder.definitions<any>()).find(definition => definition.ref.kind === "app" && definition.ref.name === application.name);
  expect(delivered.application.resources).toEqual(expect.arrayContaining(resources.map(resource => resource.definition.ref)));
  const after = await builder.definitions<any>();
  for (const resource of resources) expect(after.find(definition => JSON.stringify(definition.ref) === JSON.stringify(resource.definition.ref))).toEqual(resource.definition);
  expect((await operator.definitions<any>()).some(definition => JSON.stringify(definition.ref) === JSON.stringify(action.ref))).toBe(false);
});

test("saved application changes include published drafts and use the original joint release", async ({ page, builder }, info) => {
  const object = await builder.object({ title: "Change discovery object", fields: [{ name: "note", title: "Note", type: "text" }] });
  const application = await builder.application({ title: "Change discovery app", resources: [{ app: "build", kind: "object", name: object.type }] });
  await builder.activate(await builder.candidate([{ kind: "app", id: application.id }]));
  const original = (await builder.record("build.object", object.id)).published;
  await builder.decide("build.object.edit", { type: "build.object", id: object.id }, { title: "Change discovery object updated" });
  await builder.decide("build.app.edit", { type: "build.app", id: application.id }, { description: "Saved application update" });
  expect((await builder.record("build.object", object.id)).state).toBe("published");
  await builder.open(page, `/project?id=${application.id}`);
  await page.getByRole("button", { name: "Publish", exact: true }).click();
  const jointSelection = page.getByRole("list", { name: "Joint selection", exact: true });
  await expect(jointSelection).toContainText(object.id);
  await expect(jointSelection).toContainText(application.id);
  await page.getByRole("navigation", { name: "Application context", exact: true }).getByRole("button", { name: "Change discovery app", exact: true }).click();
  await page.getByRole("button", { name: /^Changes/ }).click();
  await expect(page.getByRole("navigation", { name: "Main", exact: true }).getByRole("button", { name: "Verify", exact: true })).toHaveAttribute("aria-current", "page");
  await expect(page.getByRole("cell", { name: "Change discovery app", exact: true })).toBeVisible();
  await page.getByRole("cell", { name: "Change discovery object updated", exact: true }).click();
  const differences = page.getByRole("region", { name: "Saved definition differences", exact: true });
  await differences.getByText("title", { exact: true }).click();
  await expect(differences.getByText('"Change discovery object"', { exact: true })).toBeVisible();
  await expect(differences.getByText('"Change discovery object updated"', { exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "Review selected changes", exact: true })).toBeDisabled();
  expect((await builder.record("build.object", object.id)).published).toBe(original);
  await shots(page, differences, "saved-definition-differences", info);
  await page.getByRole("checkbox", { name: "Select Change discovery app", exact: true }).check();
  await page.getByRole("checkbox", { name: "Select Change discovery object updated", exact: true }).check();
  await shots(page, page.locator("body"), "saved-changes", info);
  await page.getByRole("button", { name: "Review selected changes", exact: true }).click();
  await page.getByRole("button", { name: "Check joint candidate", exact: true }).click();
  await page.getByRole("button", { name: "Save immutable candidate", exact: true }).click();
  const confirmation = page.getByRole("checkbox", { name: "Confirm this optional field upgrade plan", exact: true });
  if (await confirmation.count()) await confirmation.check();
  await page.getByRole("button", { name: "Activate release", exact: true }).click();
  await expect(page.getByText("Release active for operators.", { exact: false })).toBeVisible();
  await expect.poll(async () => (await builder.read<{ changes: unknown[] }>(`releases/drafts/changes?application=${application.id}`)).changes.length).toBe(0);
});

test("application capacity, original decisions and headless runtime remain visible", async ({ page, builder, operator }, testInfo) => {
  const object = await builder.object({ title: "Runtime case object", fields: [{ name: "note", title: "Note", type: "text" }] });
  const application = await builder.application({ title: "Runtime case application", resources: [{ app: "build", kind: "object", name: object.type }] });
  await builder.open(page, `/project?id=${application.id}`);
  await page.getByRole("tab", { name: "Settings", exact: true }).click();
  const capacity = page.getByRole("spinbutton", { name: "Expected records: Runtime case object", exact: true });
  await capacity.fill("1");
  await expect.poll(async () => (await builder.record("build.app", application.id)).profile?.expectedRecords?.[object.type]).toBe(1);
  await page.getByRole("textbox", { name: "Target recovery time", exact: true }).fill("30m");
  await expect.poll(async () => (await builder.record("build.app", application.id)).profile?.rto).toBe("30m");
  await builder.decide("build.app.publish", { type: "build.app", id: application.id }, {});
  const first = `${application.id}-FIRST`, second = `${application.id}-SECOND`;
  await operator.decide(`${object.type}.create`, { type: object.type, id: first }, { note: "First" });
  await operator.decide(`${object.type}.create`, { type: object.type, id: second }, { note: "Second" });
  await builder.open(page, "/portal");
  await page.getByRole("button", { name: "Runtime case application", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Application overview", exact: true })).toBeVisible();
  await expect(page.getByText("No business pages are available in this application. You can view its activity and diagnose problems from application runs.", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "View application runs", exact: true }).click();
  await expect(page.getByText("Current content version", { exact: true })).toBeVisible();
  await expect(page.getByRole("alert").filter({ hasText: "scale exceeded: 2 / 1" })).toBeVisible();
  await page.getByText("Diagnose a record", { exact: true }).click();
  await page.getByRole("textbox", { name: "Record ID", exact: true }).fill(first);
  await page.getByRole("button", { name: "Diagnose", exact: true }).click();
  await expect(page.getByText(`${object.type}.create`, { exact: true }).first()).toBeVisible();
  await expect(page.getByText(/^Definition content: schema.sha256\./)).toBeVisible();
  await expect(page.getByRole("combobox", { name: "Run kind", exact: true })).toBeVisible();
  await expect(page.getByRole("combobox", { name: "Run state", exact: true })).toBeVisible();
  if (process.env.PLATFORM_SCREENSHOTS) await page.screenshot({ path: testInfo.outputPath("application-runtime.png"), fullPage: true });
});

test("published evaluation sets open in their original test-plan editor and release review", async ({ page, builder }) => {
  const object = await builder.object({ title: "Evaluation object", fields: [{ name: "note", title: "Note", type: "text" }] });
  const functionID = fresh("FUNCTION"), functionName = slug("advice");
  await builder.decide("build.function.create", { type: "build.function", id: functionID }, {
    name: functionName, title: "Evaluation advice", description: "Summarise a synthetic note", object: object.type,
    fields: ["note"], roles: ["user", "builder"], model: "fixture/probe",
    instructions: "Summarise the provided note", maxInputBytes: 4096, maxOutputBytes: 1024, maxTokens: 256,
    output: [{ name: "summary", type: "string", required: true, description: "A short factual summary" }],
  });
  await builder.decide("build.function.publish", { type: "build.function", id: functionID }, {});
  const planID = fresh("PLAN"), name = slug("quality");
  await builder.decide("build.testplan.create", { type: "build.testplan", id: planID }, {
    name, title: "Versioned evaluation", function: functionID, model: "fixture/probe", at: "2026-10-14T09:00:00Z",
    steps: [{ type: object.type, id: "synthetic", action: `${object.type}.create`, payload: '{"note":"Synthetic"}', expect: "accepted" }],
    evaluation: [{ minQuality: 1, maxCostUsd: 0.1, maxLatencyMillis: 30000, cases: [{ name: "synthetic", input: { note: "Synthetic" }, expected: { summary: "Synthetic" } }] }],
  });
  await builder.decide("build.testplan.publish", { type: "build.testplan", id: planID }, {});
  await builder.open(page, `/evaluation-set?id=${planID}`);
  await expect(page.getByRole("textbox", { name: "Evaluation set name", exact: true })).toHaveValue(name);
  await expect(page.getByRole("button", { name: "Publish evaluation set", exact: true })).toBeEnabled();
  await page.getByRole("button", { name: "Publish evaluation set", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Release review", exact: true })).toBeVisible();
});
