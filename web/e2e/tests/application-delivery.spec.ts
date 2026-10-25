import { expect, fresh, shots, slug, test } from "./kit";

test("shared published resources retain their owners through application discovery and delivery", async ({ page, builder, operator }, info) => {
  test.setTimeout(120_000);
  const before = await builder.definitions<any>();
  const operatorKeys = new Set((await operator.definitions<any>()).map(definition => JSON.stringify(definition.ref)));
  const actions = before.filter(definition => definition.ref.app !== "build" && definition.action && !operatorKeys.has(JSON.stringify(definition.ref)));
  expect(actions.length).toBeGreaterThan(0);
  const action = actions[0];
  const secondAction = actions.find(d => d.ref.app !== action.ref.app) ?? actions[1];
  const resources = [
    { definition: action, title: action.action.title },
    ...(secondAction ? [{ definition: secondAction, title: secondAction.action.title }] : []),
  ];
  const missing = { app: "build", kind: "action", name: slug("unavailable") };
  const application = await builder.application({ title: "Shared service resources", resources: [missing] });
  await builder.open(page, `/project?id=${application.id}&area=logic&asset=${encodeURIComponent(`${missing.app}/${missing.kind}/${missing.name}`)}`);
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
    await navigation.getByRole("button", { name: "Behavior", exact: true }).click();
    await page.getByRole("region", { name: "Workspace resources", exact: true }).getByRole("button", { name: new RegExp(`^${resource.title.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}`) }).click();
    await expect(page.getByRole("heading", { name: resource.title, exact: true })).toBeVisible();
    await expect(page.getByRole("region", { name: "Published resource", exact: true })).toBeVisible();
    const diagram = page.getByRole("group", { name: "Dependency diagram", exact: true });
    await expect(diagram).toHaveAttribute("aria-busy", "false");
    await shots(page, page.locator("body"), `shared-${resource.definition.ref.kind}`, info);
    await page.getByRole("navigation", { name: "Application context", exact: true }).getByRole("button", { name: "Shared service resources", exact: true }).click();
  }

  await page.getByRole("navigation", { name: "Main", exact: true }).getByRole("button", { name: "Release", exact: true }).click();
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
  await expect(page.getByRole("navigation", { name: "Main", exact: true }).getByRole("button", { name: "Release", exact: true })).toHaveAttribute("aria-current", "page");
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
  test.setTimeout(120_000);
  const object = await builder.object({ title: "Runtime case object", fields: [{ name: "note", title: "Note", type: "text" }] });
  const workflow = { id: fresh("FLOW"), name: slug("runtime"), title: "Runtime review" };
  await builder.decide("build.process.create", { type: "build.process", id: workflow.id }, {
    name: workflow.name, title: workflow.title, object: object.type, manual: true,
    steps: [{ name: "hold", title: "Wait for review", kind: "wait", untilSeconds: 3600 }],
  });
  const application = await builder.application({ title: "Runtime case application", resources: [
    { app: "build", kind: "object", name: object.type },
    { app: "build", kind: "flow", name: `build.${workflow.name}` },
  ] });

  // Set the original application profile, then publish the workflow and app
  // together through the existing release owner.
  await builder.open(page, `/project?id=${application.id}`);
  await page.getByRole("tab", { name: "Settings", exact: true }).click();
  const capacity = page.getByRole("spinbutton", { name: "Expected records: Runtime case object", exact: true });
  await capacity.fill("1");
  await expect.poll(async () => (await builder.record("build.app", application.id)).profile?.expectedRecords?.[object.type]).toBe(1);
  await page.getByRole("textbox", { name: "Target recovery time", exact: true }).fill("30m");
  await expect.poll(async () => (await builder.record("build.app", application.id)).profile?.rto).toBe("30m");
  await builder.activate(await builder.candidate([{ kind: "flow", id: workflow.id }, { kind: "app", id: application.id }]));
  const first = `${application.id}-FIRST`, second = `${application.id}-SECOND`;
  await operator.decide(`${object.type}.create`, { type: object.type, id: first }, { note: "First" });
  await operator.decide(`${object.type}.create`, { type: object.type, id: second }, { note: "Second" });
  for (const key of [first, second]) {
    await builder.decide("build.process.run", { type: "build.process", id: workflow.id }, { key, input: "{}" });
  }
  const inventory = await builder.read<{ records: { id: string; flow: string; key: string; subject: string }[] }>("records/flow.instance?limit=1000");
  const firstRun = inventory.records.find(run => run.flow === `build.${workflow.name}` && run.key === first)!;
  const secondRun = inventory.records.find(run => run.flow === `build.${workflow.name}` && run.key === second)!;
  expect(firstRun).toMatchObject({ subject: `${object.type}/${first}` });
  expect(secondRun).toMatchObject({ subject: `${object.type}/${second}` });

  // Project Runs selects and explains a run; handling belongs to its original
  // detail view rather than a second set of controls in the list.
  await page.getByRole("tab", { name: "Runs", exact: true }).click();
  await page.getByRole("combobox", { name: "Run kind", exact: true }).selectOption("flow");
  const firstRunButton = page.getByRole("button", { name: `${workflow.title} ${first}`, exact: true });
  const secondRunButton = page.getByRole("button", { name: `${workflow.title} ${second}`, exact: true });
  await firstRunButton.click();
  const selected = page.getByRole("region", { name: "Selected run", exact: true });
  await expect(selected.getByRole("button", { name: "Cancel", exact: true })).toHaveCount(0);
  await expect(selected.getByRole("button", { name: "Retry", exact: true })).toHaveCount(0);
  await expect(selected.getByRole("button", { name: "Retry original operation", exact: true })).toHaveCount(0);
  await selected.getByRole("button", { name: "Open run", exact: true }).click();
  const runView = page.getByRole("region", { name: firstRun.id, exact: true });

  // The server accepts cancellation before its reply is lost. Retrying the
  // original operation must reuse that decision, without another revision.
  const cancelKeys: string[] = [];
  const submissions = (url: URL) => url.pathname.endsWith("/submissions");
  await page.route(submissions, async route => {
    const body = route.request().postDataJSON();
    if (body.schema?.name !== "flow.instance.cancel" || body.target?.id !== firstRun.id) {
      await route.continue();
      return;
    }
    cancelKeys.push(body.idempotencyKey);
    if (cancelKeys.length === 1) {
      const response = await route.fetch();
      expect(response.ok()).toBe(true);
      await route.abort("connectionreset");
      return;
    }
    await route.continue();
  });
  await runView.getByRole("button", { name: "Cancel", exact: true }).click();
  await expect.poll(async () => (await builder.record("flow.instance", firstRun.id)).state).toBe("canceled");
  const canceled = await builder.record("flow.instance", firstRun.id);
  const retryCancellation = runView.getByRole("button", { name: "Retry original operation", exact: true });
  await retryCancellation.click();
  await expect(retryCancellation).toHaveCount(0);
  expect(cancelKeys.length).toBeGreaterThanOrEqual(2);
  expect(new Set(cancelKeys).size).toBe(1);
  expect((await builder.record("flow.instance", firstRun.id)).revision).toBe(canceled.revision);
  await page.unroute(submissions);
  await runView.getByRole("button", { name: "Back to project", exact: true }).click();
  await expect(page.getByRole("tab", { name: "Runs", exact: true })).toHaveAttribute("aria-selected", "true");

  // Canceled is the native flow state. Related records match exact type/id.
  const state = page.getByRole("combobox", { name: "Run state", exact: true });
  await state.selectOption({ label: "Canceled" });
  await expect(state).toHaveValue("canceled");
  await expect(firstRunButton).toBeVisible();
  await expect(secondRunButton).toHaveCount(0);
  const subject = page.getByRole("textbox", { name: "Related record", exact: true });
  await subject.fill(`${object.type}/${second}`);
  await expect(page.getByText("No related runs you can read.", { exact: true })).toBeVisible();
  await subject.fill(`${object.type}/${first.slice(0, -1)}`);
  await expect(page.getByText("No related runs you can read.", { exact: true })).toBeVisible();
  await subject.fill(`${object.type}/${first}`);
  await firstRunButton.click();

  // Viewing a business record stays read-only and leaves the current task.
  const beforeInspection = page.url();
  await selected.getByRole("button", { name: "Open related record", exact: true }).first().click();
  const inspection = page.getByRole("dialog", { name: first, exact: true });
  await expect(inspection).toContainText("First");
  await expect(inspection.getByRole("button", { name: "Edit", exact: true })).toHaveCount(0);
  await expect(page.getByRole("tab", { name: first, exact: true })).toHaveCount(0);
  expect(page.url()).toBe(beforeInspection);
  await page.keyboard.press("Escape");
  await expect(inspection).not.toBeVisible();

  // The full run opens beside its project and returns to the same Runs
  // selection and filters, including after visiting another project section.
  await selected.getByRole("button", { name: "Open run", exact: true }).click();
  await expect(runView.getByRole("button", { name: "Back to project", exact: true })).toBeVisible();
  await expect(runView.getByRole("button", { name: "Back to application runs", exact: true })).toHaveCount(0);
  await runView.getByRole("button", { name: "Back to project", exact: true }).click();
  await expect(page.getByRole("tab", { name: "Runs", exact: true })).toHaveAttribute("aria-selected", "true");
  await expect(state).toHaveValue("canceled");
  await expect(subject).toHaveValue(`${object.type}/${first}`);
  await expect(selected).toContainText(`${workflow.title} ${first}`);
  await shots(page, page.locator("body"), "project-runtime", testInfo);
  await page.getByRole("tab", { name: "Settings", exact: true }).click();
  await page.getByRole("tab", { name: "Runs", exact: true }).click();
  await expect(state).toHaveValue("canceled");
  await expect(subject).toHaveValue(`${object.type}/${first}`);
  await expect(selected).toContainText(`${workflow.title} ${first}`);

  // The source editor retains its history/input view and opens the same
  // original runtime detail instead of offering another cancellation path.
  await selected.getByRole("button", { name: "Open current source editor", exact: true }).click();
  const source = page.getByRole("region", { name: "Flows", exact: true });
  await expect(source.getByRole("heading", { name: workflow.title, exact: true })).toBeVisible();
  const dock = source.getByRole("button", { name: "Toggle dock", exact: true });
  if (await dock.getAttribute("aria-expanded") !== "true") await dock.click();
  await source.getByRole("tab", { name: "Runs", exact: true }).click();
  await source.getByRole("row").filter({ hasText: `${workflow.title} ${second}` }).click();
  await expect(source.getByText("Workflow input", { exact: true })).toBeVisible();
  await expect(source.getByRole("button", { name: "Cancel run", exact: true })).toHaveCount(0);
  await expect(source.getByRole("button", { name: "Retry run", exact: true })).toHaveCount(0);
  await source.getByRole("button", { name: "Open run", exact: true }).click();
  const secondRunView = page.getByRole("region", { name: secondRun.id, exact: true });
  await expect(secondRunView.getByRole("button", { name: "Cancel", exact: true })).toBeVisible();
  await secondRunView.getByRole("button", { name: "Back to project", exact: true }).click();
  await expect(state).toHaveValue("canceled");
  await expect(subject).toHaveValue(`${object.type}/${first}`);
  await expect(selected).toContainText(`${workflow.title} ${first}`);

  // The installed headless app retains its original runtime/record diagnosis
  // and returns from the full run to application runs, without a project.
  await builder.open(page, "/portal");
  await page.getByRole("region", { name: "Applications", exact: true }).getByRole("button", { name: "Runtime case application", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Application overview", exact: true })).toBeVisible();
  await expect(page.getByText("No business pages are available in this application. You can view its activity and diagnose problems from application runs.", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "View application runs", exact: true }).click();
  const artifact=page.getByLabel("Application artifact",{exact:true});
  await expect(artifact.getByText("Current content version",{exact:true})).toHaveCount(0);
  const explanation=artifact.getByRole("button",{name:"About Application artifact",exact:true});
  await explanation.focus();
  await page.keyboard.press("Enter");
  await expect(page.getByRole("dialog",{name:"Application artifact",exact:true})).toContainText("Counts and diagnostics follow your authorized scope.");
  await page.keyboard.press("Escape");
  await expect(explanation).toBeFocused();
  await shots(page,artifact,"application-runtime-summary",testInfo);
  await artifact.getByRole("button",{name:"Versions and recovery",exact:true}).click();
  await expect(artifact.getByText("Current content version", { exact: true })).toBeVisible();
  await expect(page.getByRole("alert").filter({ hasText: "scale exceeded: 2 / 1" })).toBeVisible();
  await page.getByText("Diagnose a record", { exact: true }).click();
  await page.getByRole("textbox", { name: "Record ID", exact: true }).fill(first);
  const diagnostic = page.waitForResponse(response => new URL(response.url()).pathname.endsWith(`/records/${object.type}/${first}/diagnostic`));
  await page.getByRole("button", { name: "Diagnose", exact: true }).click();
  expect((await diagnostic).ok()).toBe(true);
  await expect(page.getByText(`${object.type}.create`, { exact: true }).first()).toBeVisible();
  await expect(page.getByText(/^Definition content: schema.sha256\./)).toBeVisible();
  await expect(page.getByRole("combobox", { name: "Run kind", exact: true })).toBeVisible();
  await expect(page.getByRole("combobox", { name: "Run state", exact: true })).toBeVisible();
  await firstRunButton.click();
  await selected.getByRole("button", { name: "Open run", exact: true }).click();
  await expect(runView.getByRole("button", { name: "Back to project", exact: true })).toHaveCount(0);
  await runView.getByRole("button", { name: "Back to application runs", exact: true }).click();
  const versions=artifact.getByRole("button",{name:"Versions and recovery",exact:true});
  if(await versions.getAttribute("aria-expanded")!=="true")await versions.click();
  await expect(page.getByText("Current content version", { exact: true })).toBeVisible();
  await expect(selected).toContainText(`${workflow.title} ${first}`);
  await shots(page, page.locator("body"), "application-runtime", testInfo);
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


test("page explanations share one entry across Studio and tenant management",async({page,builder},info)=>{
  for(const [route,title] of [["/projects","Applications"],["/control-panel","Control Panel"]]){
    await builder.open(page,route!);
    const help=page.getByRole("button",{name:"About "+title,exact:true});
    await help.focus();
    await page.keyboard.press("Enter");
    await expect(page.getByRole("dialog",{name:title,exact:true})).toBeVisible();
    await shots(page,page.locator("body"),"context-help-"+title!.toLowerCase().replaceAll(" ","-"),info);
    await page.keyboard.press("Escape");
    await expect(help).toBeFocused();
  }
  const me=await builder.read<any>("me");
  await builder.decide("platform.profile.update",{type:"platform.profile",id:me.principalId},{language:"zh-CN"},"platform");
  try{
    await builder.open(page,"/control-panel");
    await page.getByRole("button",{name:"控制面板说明",exact:true}).click();
    await expect(page.getByRole("dialog",{name:"控制面板",exact:true})).toBeVisible();
    await shots(page,page.locator("body"),"context-help-zh",info);
    await page.keyboard.press("Escape");
  }finally{
    await builder.decide("platform.profile.update",{type:"platform.profile",id:me.principalId},{language:"en"},"platform");
    await page.evaluate(()=>localStorage.setItem("platform.language","en"));
  }
});
