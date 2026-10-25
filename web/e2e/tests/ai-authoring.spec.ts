import { expect, fresh, shots, slug, test } from "./kit";

test("an application authors AI functions, agents and evaluation sets over saved models", async ({ page, builder, operator }, info) => {
  test.setTimeout(120_000);
  const objectName = slug("aimodel"), objectID = fresh("MODEL"), functionName = slug("aisummary"), agentName = slug("aireviewer"), evaluationName = slug("aievaluation");
  const type = `build.${objectName}`;
  await builder.decide("build.object.create", { type: "build.object", id: objectID }, {
    name: objectName, title: "AI source model", fields: [{ name: "note", title: "Readable note", type: "text" }, { name: "secret", title: "Excluded note", type: "text", aiInput: false }],
    states: [{ name: "open", title: "Open" }], actions: [{ name: "review", title: "Review note", from: ["open"] }],
  });
  const application = await builder.application({ title: "AI drafting application", resources: [{ app: "build", kind: "object", name: type }] });
  const original = await operator.definitions();
  await builder.open(page, `/project?id=${application.id}&area=logic`);
  const create = async (name: string) => {
    await page.getByRole("button", { name: "New", exact: true }).click();
    await page.getByRole("menuitem", { name, exact: true }).click();
    await expect(page.getByRole("dialog")).toHaveCount(0);
    await page.getByRole("group", { name: "Editing mode", exact: true }).getByRole("button", { name: "Visual", exact: true }).click();
  };
  await create("AI function");
  const stages = page.getByRole("region", { name: "Function stages", exact: true });
  await page.getByRole("textbox", { name: "Function name", exact: true }).fill(functionName);
  await page.getByRole("textbox", { name: "Function title", exact: true }).fill("Summarize saved notes");
  await page.getByRole("textbox", { name: "Function description", exact: true }).fill("Summarize the declared note field");
  await stages.getByRole("button", { name: "Record inputs", exact: true }).click();
  await page.getByRole("combobox", { name: "Source object", exact: true }).selectOption(type);
  await page.getByRole("checkbox", { name: "Readable note · text", exact: true }).check();
  await expect(page.getByRole("checkbox", { name: "Excluded note · text", exact: true })).toHaveCount(0);
  await stages.getByRole("button", { name: "Model inference", exact: true }).click();
  await page.getByRole("textbox", { name: "Model instructions", exact: true }).fill("Return a concise summary of the note");
  await stages.getByRole("button", { name: "Strict output", exact: true }).click();
  await page.getByRole("textbox", { name: "Output description", exact: true }).fill("The note summary");

  // A failed membership write must keep the already saved resource. Retry
  // adds that same declaration rather than creating or editing it again.
  let refuseJoin = true;
  await page.route("**/v1/submissions", async route => {
    const input = route.request().postDataJSON();
    if (refuseJoin && input.schema?.name === "build.app.edit" && input.target?.id === application.id) {
      refuseJoin = false;
      return route.fulfill({ status: 400, contentType: "application/json", body: JSON.stringify({ error: { code: "ERROR_CODE_CONFLICT", message: "Application join refused" } }) });
    }
    await route.continue();
  });
  await page.getByRole("button", { name: "Save function", exact: true }).click();
  await expect(page.getByRole("alert").filter({ hasText: "Application join refused" })).toBeVisible();
  const functions = await builder.read<{ records: { id: string; name: string; revision: number }[] }>("records/build.function?limit=1000");
  const fn = functions.records.find(record => record.name === functionName)!;
  expect(fn).toBeTruthy();
  await page.getByRole("button", { name: "Save function", exact: true }).click();
  await expect.poll(async () => (await builder.record("build.app", application.id)).resources).toContainEqual({ app: "build", kind: "function", name: functionName });
  await expect(page).toHaveURL(new RegExp(`[?&]id=${fn.id}(?:&|$)`));
  await expect(page.getByRole("tab", { name: "AI functions · Summarize saved notes", exact: true })).toHaveCount(1);
  expect((await builder.record("build.function", fn.id)).revision).toBe(fn.revision);
  await page.unroute("**/v1/submissions");

  await page.getByRole("navigation", { name: "Application context", exact: true }).getByRole("button", { name: "AI drafting application", exact: true }).click();
  await page.getByRole("navigation", { name: "Main", exact: true }).getByRole("button", { name: "Behavior", exact: true }).click();
  await create("Agent");
  await page.getByRole("textbox", { name: "Agent name", exact: true }).fill(agentName);
  await page.getByRole("textbox", { name: "Agent title", exact: true }).fill("Review saved notes");
  await page.getByRole("textbox", { name: "What it does for others", exact: true }).fill("Review the saved model through its declared action");
  await page.getByRole("textbox", { name: "Instructions", exact: true }).fill("Review the note and ask a person before writing");
  await page.getByRole("button", { name: "AI source model · Review note", exact: true }).click();
  let release!: () => void;
  const held = new Promise<void>(resolve => { release = resolve; });
  let started!: () => void;
  const pending = new Promise<void>(resolve => { started = resolve; });
  await page.route(`**/v1/records/build.app/${application.id}`, async route => {
    started(); await held;
    await route.continue();
  });
  await page.getByRole("button", { name: "Save agent", exact: true }).click();
  await pending;
  await page.getByRole("tab", { name: "Application Studio · AI drafting application", exact: true }).click();
  release();
  await expect.poll(async () => (await builder.record("build.app", application.id)).resources).toContainEqual({ app: "build", kind: "agent", name: agentName });
  await expect(page).toHaveURL(/#\/project\?/);
  await expect(page.getByRole("tab", { name: "Agents · Review saved notes", exact: true })).toHaveCount(0);
  await page.unroute(`**/v1/records/build.app/${application.id}`);

  await builder.open(page, `/project?id=${application.id}&area=release`);
  await create("Evaluation set");
  await expect(page.getByRole("combobox", { name: "Candidate kind", exact: true })).toHaveValue("function");
  await page.getByRole("combobox", { name: "Saved function draft", exact: true }).selectOption(fn.id);
  await expect(page.getByRole("checkbox", { name: "Require a measured release evaluation", exact: true })).toBeChecked();
  await page.getByRole("textbox", { name: "Test plan name", exact: true }).fill("Saved note summary cases");
  await page.getByRole("textbox", { name: "Evaluation set name", exact: true }).fill(evaluationName);
  await page.getByRole("textbox", { name: "Synthetic input (JSON object)", exact: true }).fill(JSON.stringify({ note: "Synthetic note" }));
  await page.getByRole("textbox", { name: "Expected typed answer (JSON object)", exact: true }).fill(JSON.stringify({ summary: "Synthetic summary" }));
  await page.getByRole("button", { name: "Save test plan", exact: true }).click();
  await expect(page.getByRole("status").filter({ hasText: "Test plan saved." })).toBeVisible();
  await expect.poll(async () => (await builder.record("build.app", application.id)).resources).toContainEqual({ app: "build", kind: "evaluation-set", name: evaluationName });
  await shots(page, page.locator("body"), "application-ai-drafts", info);
  await page.getByRole("navigation", { name: "Application context", exact: true }).getByRole("button", { name: "AI drafting application", exact: true }).click();
  await page.getByRole("navigation", { name: "Main", exact: true }).getByRole("button", { name: "Release", exact: true }).click();
  const verify = page.getByRole("region", { name: "Verification and delivery", exact: true });
  await verify.getByRole("button", { name: "Add the drafts of AI drafting application", exact: true }).click();
  await verify.getByRole("button", { name: "Check joint candidate", exact: true }).click();
  await expect(verify.getByRole("button", { name: "Save immutable candidate", exact: true })).toBeVisible();
  expect((await builder.record("build.object", objectID)).published ?? "").toBe("");
  expect((await builder.record("build.function", fn.id)).published ?? "").toBe("");
  expect(await operator.definitions()).toEqual(original);
});
