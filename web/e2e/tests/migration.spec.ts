import { expect, fresh, shots, slug, test } from "./kit";

test("a builder reviews a rename, resumes its data batches and activates the sealed target", async ({ page, builder, operator }, info) => {
  test.setTimeout(90_000);
  const object = await builder.object({ name: slug("rename"), title: "Migration records", fields: [{ name: "name", title: "Original name", type: "text", sourceType: "user-input" }] });
  const from = await builder.candidate(object.id);
  await builder.activate(from);
  const recordID = slug("row");
  await operator.decide(`${object.type}.create`, { type: object.type, id: recordID }, { name: "Original value" });
  await builder.records(object.type, [{ name: "Second value" }]);

  // The original field is retained as a read alias; the target is immutable.
  await builder.decide("build.object.edit", { type: "build.object", id: object.id }, { fields: [
    { name: "name", title: "Original name", type: "text", sourceType: "user-input", readOnly: true, aliasOf: "title" },
    { name: "title", title: "New name", type: "text", sourceType: "user-input" },
  ] });
  const to = await builder.candidate(object.id);
  await builder.open(page, "/release-review");
  await page.getByRole("combobox", { name: "Saved candidate", exact: true }).selectOption(to);
  const migration = page.getByRole("region", { name: "Data migration", exact: true });
  await migration.getByRole("combobox", { name: "Migration object", exact: true }).selectOption(object.type);
  const steps = JSON.stringify([
    { kind: "rename", types: [object.type], batch: 1, fields: [{ from: "name", to: "title" }] },
  ]);
  await migration.getByRole("textbox", { name: "Migration steps", exact: true }).fill(steps);
  await migration.getByRole("button", { name: "Review migration", exact: true }).click();
  await expect(migration.getByRole("region", { name: "Migration review", exact: true })).toContainText("2 existing records");
  await migration.getByRole("textbox", { name: "Migration steps", exact: true }).fill("null");
  await expect(migration.getByRole("button", { name: "Review migration", exact: true })).toBeDisabled();
  await expect(migration.getByRole("button", { name: "Start reviewed migration", exact: true })).toHaveCount(0);
  await migration.getByRole("textbox", { name: "Migration steps", exact: true }).fill(steps);
  await migration.getByRole("button", { name: "Review migration", exact: true }).click();
  await migration.getByRole("button", { name: "Start reviewed migration", exact: true }).click();

  // Accept the first batch but lose its reply. Retry keeps the original K5 key.
  const keys: string[] = [];
  let lost = false;
  await page.route("**/v1/submissions", async route => {
    const body = route.request().postDataJSON();
    if (body.schema?.name !== "build.migration.advance") { await route.continue(); return; }
    keys.push(body.idempotencyKey);
    if (lost) { await route.continue(); return; }
    lost = true;
    await route.fetch();
    await route.abort("connectionreset");
  });
  await migration.getByRole("button", { name: "Run next migration batch", exact: true }).click();
  await migration.getByRole("button", { name: "Retry original migration decision", exact: true }).click();
  await expect(migration.getByRole("region", { name: "Migration progress", exact: true })).toContainText("Migrated: 1");
  expect(keys.length).toBeGreaterThanOrEqual(2);
  expect(new Set(keys).size).toBe(1);
  await page.unroute("**/v1/submissions");
  await expect(page.getByRole("button", { name: "Activate release", exact: true })).toBeDisabled();

  // Reopening the task reads accepted progress instead of creating another run.
  await page.reload();
  await page.getByRole("combobox", { name: "Saved candidate", exact: true }).selectOption(to);
  await expect(migration.getByRole("region", { name: "Migration progress", exact: true })).toContainText("Migrated: 1");
  await migration.getByRole("button", { name: "Run next migration batch", exact: true }).click();
  await expect(migration.getByRole("region", { name: "Migration progress", exact: true })).toContainText("Migrated: 2");
  await shots(page, migration, "migration-progress", info);
  await page.getByRole("button", { name: "Activate release", exact: true }).click();
  await expect(page.getByRole("status").filter({ hasText: "Release active for operators." })).toContainText(to);

  // New writes use the new name. The retained old name reads the same value.
  await operator.decide(`${object.type}.edit`, { type: object.type, id: recordID }, { title: "Changed after migration" });
  const record = await operator.record(object.type, recordID);
  expect(record.name).toBe("Changed after migration");
  expect(record.title).toBe("Changed after migration");
});

test("a waiting flow finishes its retired action under the starting release after migration", async ({ page, builder, operator, browser }) => {
  test.setTimeout(90_000);
  const object = await builder.object({ name: slug("evolve"), title: "Evolving visits", fields: [{ name: "note", title: "Note", type: "text", sourceType: "user-input" }], states: [
    { name: "open", title: "Open" }, { name: "done", title: "Done" }, { name: "rejected", title: "Rejected" },
  ], actions: [
    { name: "close", title: "Close", from: ["open"], to: "done" },
    { name: "reject", title: "Reject", from: ["open"], to: "rejected" },
  ] });
  const flowID = fresh("FLOW"), flowName = slug("review");
  const steps = [
    { name: "ask", title: "Review visit", kind: "ask", ask: "user", answers: ["yes"], next: "edit" },
    { name: "edit", kind: "action", act: "edit", inputs: { note: { source: "literal", value: "Old flow finished" } }, next: "close" },
    { name: "close", kind: "action", act: "close" },
  ];
  await builder.decide("build.process.create", { type: "build.process", id: flowID }, { name: flowName, title: "Migration review", object: object.type, when: "open", steps });
  await builder.decide("build.process.publish", { type: "build.process", id: flowID }, {});
  const drafts = [{ kind: "object", id: object.id }, { kind: "flow", id: flowID }];
  const from = await builder.candidate(drafts);
  await builder.activate(from);
  const oldID = slug("oldvisit");
  await operator.decide(`${object.type}.create`, { type: object.type, id: oldID }, { note: "Original note" });
  const context = await browser.newContext();
  try {
    const inbox = await context.newPage();
    await operator.open(inbox, "/inbox");
    await expect(inbox.getByRole("listitem").filter({ hasText: oldID })).toBeVisible();
    const starting = await builder.record("flow.instance", `build.${flowName}:${oldID}`);
    expect(starting.release).toBe(from);

    await builder.decide("build.object.edit", { type: "build.object", id: object.id }, { fields: [
      { name: "note", title: "Old note", type: "text", sourceType: "user-input", readOnly: true, aliasOf: "label" },
      { name: "label", title: "Label", type: "text", sourceType: "user-input" },
      { name: "enabled", title: "Enabled", type: "boolean", sourceType: "user-input", required: true },
    ], actions: [{ name: "reject", title: "Reject", from: ["open"], to: "rejected" }] });
    await builder.decide("build.process.edit", { type: "build.process", id: flowID }, { steps: [
      steps[0],
      { name: "edit", kind: "action", act: "edit", inputs: { label: { source: "literal", value: "New flow finished" } }, next: "close" },
      { name: "close", kind: "action", act: "reject" },
    ] });
    const to = await builder.candidate(drafts);
    await builder.open(page, "/release-review");
    await page.getByRole("combobox", { name: "Saved candidate", exact: true }).selectOption(to);
    const migration = page.getByRole("region", { name: "Data migration", exact: true });
    await migration.getByRole("combobox", { name: "Migration object", exact: true }).selectOption(object.type);
    await migration.getByRole("textbox", { name: "Migration steps", exact: true }).fill(JSON.stringify([
      { kind: "rename", types: [object.type], batch: 1, fields: [{ from: "note", to: "label" }] },
      { kind: "default", types: [object.type], batch: 1, fields: [{ to: "enabled", expr: { op: "", kind: "boolean", lit: { value: true } } }] },
      { kind: "retire-action", types: [object.type], action: `${object.type}.close`, batch: 1 },
    ]));
    await migration.getByRole("button", { name: "Review migration", exact: true }).click();
    await migration.getByRole("button", { name: "Start reviewed migration", exact: true }).click();
    for (let batch = 0; batch < 3; batch++) {
      await migration.getByRole("button", { name: "Run next migration batch", exact: true }).click();
      if (batch < 2) await expect(migration.getByRole("region", { name: "Migration progress", exact: true })).toContainText(`Next step: ${batch + 2} / 3`);
    }
    await expect(migration.getByRole("region", { name: "Migration progress", exact: true })).toContainText("complete");
    await page.getByRole("button", { name: "Activate release", exact: true }).click();
    await expect(page.getByRole("status").filter({ hasText: "Release active for operators." })).toContainText(to);

    await inbox.reload();
    await inbox.getByRole("listitem").filter({ hasText: oldID }).getByRole("button", { name: "yes", exact: true }).click();
    await expect.poll(async () => (await operator.record(object.type, oldID)).state).toBe("done");
    const old = await operator.record(object.type, oldID);
    expect(old.label).toBe("Old flow finished");
    expect(old.enabled).toBe(true);
    const ended = await builder.record("flow.instance", `build.${flowName}:${oldID}`);
    expect(ended.release).toBe(starting.release);
    expect(ended.dependencies).toBe(starting.dependencies);
    expect(ended.state).toBe("done");
    expect((await operator.definitions()).some((d: any) => d.ref.kind === "action" && d.ref.name === `${object.type}.close`)).toBe(false);

    const newID = slug("newvisit");
    await operator.decide(`${object.type}.create`, { type: object.type, id: newID }, { label: "New note", enabled: true });
    await inbox.reload();
    await inbox.getByRole("listitem").filter({ hasText: newID }).getByRole("button", { name: "yes", exact: true }).click();
    await expect.poll(async () => (await operator.record(object.type, newID)).state).toBe("rejected");
    expect((await operator.record(object.type, newID)).label).toBe("New flow finished");
  } finally { await context.close(); }
});

test("action retirement rejects the pending request through its owner before activation", async ({ page, builder, operator }) => {
  test.setTimeout(90_000);
  const object = await builder.object({ name: slug("retire"), title: "Retirement records", fields: [{ name: "name", title: "Name", type: "text", sourceType: "user-input" }], states: [
    { name: "open", title: "Open" }, { name: "pending", title: "Pending" }, { name: "done", title: "Done" }, { name: "rejected", title: "Rejected" },
  ], actions: [{ name: "finish", title: "Finish", from: ["open"], to: "done", roles: ["user"], approval: { pending: "pending", rejected: "rejected", levels: [{ title: "Builder", role: "builder" }] } }] });
  const from = await builder.candidate(object.id);
  await builder.activate(from);
  const recordID = slug("waiting");
  await operator.decide(`${object.type}.create`, { type: object.type, id: recordID }, { name: "Waiting for approval" });
  await operator.decide(`${object.type}.finish`, { type: object.type, id: recordID }, {});
  expect((await operator.record(object.type, recordID)).state).toBe("pending");

  // The target removes the action; a reviewed retirement releases old work.
  await builder.decide("build.object.edit", { type: "build.object", id: object.id }, { actions: [] });
  const to = await builder.candidate(object.id);
  await builder.open(page, "/release-review");
  await page.getByRole("combobox", { name: "Saved candidate", exact: true }).selectOption(to);
  const migration = page.getByRole("region", { name: "Data migration", exact: true });
  await migration.getByRole("combobox", { name: "Migration object", exact: true }).selectOption(object.type);
  await migration.getByRole("textbox", { name: "Migration steps", exact: true }).fill(JSON.stringify([
    { kind: "retire-action", types: [object.type], action: `${object.type}.finish`, batch: 1 },
  ]));
  await migration.getByRole("button", { name: "Review migration", exact: true }).click();
  await migration.getByRole("button", { name: "Start reviewed migration", exact: true }).click();
  await migration.getByRole("button", { name: "Run next migration batch", exact: true }).click();
  await expect(migration.getByRole("region", { name: "Migration progress", exact: true })).toContainText("complete");
  expect((await operator.record(object.type, recordID)).state).toBe("rejected");
  await page.getByRole("button", { name: "Activate release", exact: true }).click();
  await expect(page.getByRole("status").filter({ hasText: "Release active for operators." })).toContainText(to);
  expect((await operator.definitions()).some((definition: any) => definition.ref.kind === "action" && definition.ref.name === `${object.type}.finish`)).toBe(false);
});
