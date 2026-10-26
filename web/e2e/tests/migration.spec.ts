import { expect, fresh, retainActionCatalog, shots, slug, test } from "./kit";
import type { Locator } from "@playwright/test";

async function setMigrationSteps(migration: Locator, steps: unknown[]) {
  const mode = migration.getByRole("group", { name: "Editing mode", exact: true });
  await mode.getByRole("button", { name: "Source editor", exact: true }).click();
  const input = migration.getByRole("textbox", { name: "Configuration source", exact: true });
  const source = JSON.parse(await input.inputValue());
  expect(source).toMatchObject({ name: expect.any(String), title: expect.any(String), plan: { steps: expect.any(Array) } });
  source.plan.steps = steps;
  await input.fill(JSON.stringify(source));
  await migration.getByRole("button", { name: "Apply source", exact: true }).click();
  await mode.getByRole("button", { name: "Visual", exact: true }).click();
}

function migrationProgress(migration: Locator, label: "Migrated" | "Next step" | "Migration state") {
  return migration.getByRole("region", { name: "Migration progress", exact: true })
    .getByRole("term").filter({ hasText: label }).locator("..").getByRole("definition");
}

test("an old client keeps its schema through compatible publication and lost replies, then refreshes across families", async ({ page, builder, operator }) => {
  test.setTimeout(90_000);
  const fields = [{ name: "note", title: "Note", type: "text" }];
  const object = await builder.object({ title: "Schema record", fields,
    states: [{ name: "open", title: "Open" }, { name: "reviewed", title: "Reviewed" }],
    actions: [{ name: "review", title: "Review", from: ["open"], to: "reviewed" }],
  });
  const catalog = await operator.read<any[]>("actions");
  const original = catalog.find(action => action.schema === `${object.type}.create`).schemaVersion;
  expect(original.family).toBe(1);

  // This tab retains the definitions it originally read, as an offline client would.
  const retained = await retainActionCatalog(page, catalog);
  await operator.open(page, `/page?app=build&kind=page&name=${object.name}`);
  await page.getByRole("button", { name: "Create schema record", exact: true }).click();
  const form = page.getByRole("dialog");
  const acceptedID = fresh("SCHEMA");
  await form.getByRole("textbox", { name: "ID *", exact: true }).fill(acceptedID);
  await form.getByRole("textbox", { name: "Note", exact: true }).fill("Keep the original contract");
  await builder.decide("build.object.edit", { type: "build.object", id: object.id }, { fields: [...fields, { name: "description", title: "Description", type: "text" }] });
  await builder.decide("build.object.publish", { type: "build.object", id: object.id }, {});
  const compatible = (await operator.read<any[]>("actions")).find(action => action.schema === `${object.type}.create`).schemaVersion;
  expect(compatible.family).toBe(original.family);
  expect(compatible.version).not.toBe(original.version);

  // The host accepts the old content version. Losing its reply retains the exact outbox declaration.
  const requests: { key: string; family: number; content: string }[] = [];
  await page.route("**/v1/submissions", async route => {
    const body = route.request().postDataJSON();
    if (body.schema?.name !== `${object.type}.create`) { await route.continue(); return; }
    requests.push({ key: body.idempotencyKey, family: body.schema.version, content: route.request().headers()["platform-schema-version"]! });
    if (requests.length === 1) {
      const response = await route.fetch();
      expect(response.ok()).toBeTruthy();
      await route.abort("connectionreset");
      return;
    }
    if (requests.length === 3) await retained.release();
    await route.continue();
  });
  await form.getByRole("button", { name: "Create", exact: true }).click();
  await expect(form.getByRole("button", { name: "Retry confirmation", exact: true })).toBeVisible();
  expect(await operator.record(object.type, acceptedID)).toMatchObject({ note: "Keep the original contract" });
  const stored = await page.evaluate(key => {
    const entries = Object.keys(localStorage).filter(name => name.startsWith("outbox:")).flatMap(name => JSON.parse(localStorage.getItem(name)!));
    return entries.find(entry => entry.submission.idempotencyKey === key);
  }, requests[0]!.key);
  expect(stored.schemaVersion).toEqual(original);

  await builder.decide("build.object.edit", { type: "build.object", id: object.id }, { actions: [{ name: "review", title: "Review", from: ["open"], to: "open" }] });
  await builder.decide("build.object.publish", { type: "build.object", id: object.id }, {});
  await form.getByRole("button", { name: "Retry confirmation", exact: true }).click();
  await expect(form).toHaveCount(0);
  expect(requests[1]).toEqual(requests[0]);
  expect((await operator.record(object.type, acceptedID)).revision).toBe(1);

  // A new request from that stale tab cannot execute current rules. Refresh keeps its entered values.
  const newID = fresh("SCHEMA");
  await page.getByRole("button", { name: "Create schema record", exact: true }).click();
  await form.getByRole("textbox", { name: "ID *", exact: true }).fill(newID);
  await form.getByRole("textbox", { name: "Note", exact: true }).fill("Keep my draft after refresh");
  await form.getByRole("button", { name: "Create", exact: true }).click();
  await expect(form.getByRole("alert")).toContainText("refresh definitions");
  await expect.poll(() => retained.currentFamily(`${object.type}.create`)).toBe(2);
  expect(await operator.record(object.type, newID)).toBeUndefined();
  await expect(form.getByRole("textbox", { name: "Note", exact: true })).toHaveValue("Keep my draft after refresh");
  await expect(form.getByRole("button", { name: "Retry original inputs", exact: true })).toBeEnabled();
  await form.getByRole("button", { name: "Submit with refreshed definition", exact: true }).click();
  await expect(form).toHaveCount(0);
  expect(await operator.record(object.type, newID)).toMatchObject({ note: "Keep my draft after refresh", revision: 1 });
  expect(requests[2]!.family).toBe(1);
  expect(requests[3]!.family).toBe(2);
  expect(requests[3]!.key).not.toBe(requests[2]!.key);
});

test("customer extension fields and actions survive an upgrade and resolve a declared field conflict", async ({ page, builder, operator }, info) => {
  test.setTimeout(90_000);
  const baseFields = [{ name: "name", title: "Material name", type: "text" }];
  const base = await builder.object({ title: "Shared material", fields: baseFields });
  const fields = [
    { name: "base", title: "Material", type: "reference", ref: base.type, required: true, inverse: "extension" },
    { name: "customergrade", title: "Customer grade", type: "text" },
    { name: "inspectiondue", title: "Inspection due", type: "date" },
  ];
  const extension = await builder.object({ title: "Customer material extension", extends: base.type, fields,
    states: [{ name: "open", title: "Open" }, { name: "reviewed", title: "Reviewed" }],
    actions: [{ name: "review", title: "Review", from: ["open"], to: "reviewed" }],
  });
  await builder.activate(await builder.candidate(extension.id));
  const materials = await builder.records(base.type, [{ name: "One" }, { name: "Two" }, { name: "Three" }]);
  const rows = await builder.records(extension.type, materials.map((base, index) => ({ base, customergrade: `Grade ${index}`, inspectiondue: "2027-01-01" })));
  const before = await Promise.all(rows.map(id => operator.record(extension.type, id)));

  const compatibleFields = [...baseFields, { name: "description", title: "Description", type: "text" }];
  await builder.decide("build.object.edit", { type: "build.object", id: base.id }, { fields: compatibleFields });
  const compatible = await builder.candidate(base.id);
  await builder.open(page, "/release-review");
  await page.getByRole("combobox", { name: "Saved candidate", exact: true }).selectOption(compatible);
  const extensions = page.getByRole("region", { name: "Customer extensions", exact: true });
  await expect(extensions).toContainText(extension.type);
  await expect(extensions).toContainText("customergrade");
  await page.getByRole("checkbox", { name: "Confirm this optional field upgrade plan", exact: true }).check();
  await page.getByRole("button", { name: "Activate release", exact: true }).click();
  await expect(page.getByRole("status").filter({ hasText: "Release active for operators." })).toContainText(compatible);
  expect(await Promise.all(rows.map(id => operator.record(extension.type, id)))).toEqual(before);
  await operator.decide(`${extension.type}.review`, { type: extension.type, id: rows[0]! }, {});

  // A new base field cannot silently shadow the customer field.
  await builder.decide("build.object.edit", { type: "build.object", id: base.id }, { fields: [...compatibleFields, { name: "customergrade", title: "Package grade", type: "text" }] });
  const conflict = await builder.request.post("/v1/releases/preview", { headers: builder.headers, data: { kind: "object", id: base.id } });
  expect(conflict.ok()).toBeTruthy();
  expect((await conflict.json()).diagnostic).toContain("customergrade");
  expect(await operator.record(extension.type, rows[1]!)).toEqual(before[1]);
  await builder.decide("build.object.edit", { type: "build.object", id: extension.id }, { fields: [
    fields[0], { ...fields[1], readOnly: true, aliasOf: "localgrade" }, fields[2], { name: "localgrade", title: "Local customer grade", type: "text" },
  ] });
  const target = await builder.candidate([{ kind: "object", id: base.id }, { kind: "object", id: extension.id }]);
  await page.getByRole("combobox", { name: "Saved candidate", exact: true }).selectOption(target);
  const migration = page.getByRole("region", { name: "Data migration", exact: true });
  await migration.getByRole("combobox", { name: "Migration object", exact: true }).selectOption(extension.type);
  await setMigrationSteps(migration, [
    { kind: "rename", types: [extension.type], batch: 2, fields: [{ from: "customergrade", to: "localgrade" }] },
  ]);
  await migration.getByRole("button", { name: "Save migration plan", exact: true }).click();
  await migration.getByRole("button", { name: "Publish migration plan", exact: true }).click();
  await migration.getByRole("button", { name: "Review migration", exact: true }).click();
  await migration.getByRole("button", { name: "Start reviewed migration", exact: true }).click();
  await migration.getByRole("button", { name: "Run next migration batch", exact: true }).click();
  await expect(migrationProgress(migration, "Migrated")).toHaveText("2");
  await page.reload();
  await page.getByRole("combobox", { name: "Saved candidate", exact: true }).selectOption(target);
  await migration.getByRole("button", { name: "Run next migration batch", exact: true }).click();
  await expect(migrationProgress(migration, "Migrated")).toHaveText("3");
  await page.getByRole("button", { name: "Activate release", exact: true }).click();
  await expect(page.getByRole("status").filter({ hasText: "Release active for operators." })).toContainText(target);
  await shots(page, extensions, "customer-extension-upgrade", info);
  for (const [index, id] of rows.entries()) {
    expect(await operator.record(extension.type, id)).toMatchObject({ localgrade: `Grade ${index}`, customergrade: `Grade ${index}`, inspectiondue: "2027-01-01" });
  }
  await operator.decide(`${base.type}.edit`, { type: base.type, id: materials[1]! }, { customergrade: "Package grade" });
  await operator.decide(`${extension.type}.edit`, { type: extension.type, id: rows[1]! }, { localgrade: "Customer grade" });
  expect(await operator.record(base.type, materials[1]!)).toMatchObject({ customergrade: "Package grade" });
  expect(await operator.record(extension.type, rows[1]!)).toMatchObject({ localgrade: "Customer grade" });
  await operator.decide(`${extension.type}.review`, { type: extension.type, id: rows[1]! }, {});
});

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
  const steps = [
    { kind: "rename", types: [object.type], batch: 1, fields: [{ from: "name", to: "title" }] },
  ];
  await migration.getByRole("button", { name: "Add migration step", exact: true }).click();
  const step = migration.getByRole("group", { name: "Migration step 1", exact: true });
  await step.getByRole("spinbutton", { name: "Records per batch", exact: true }).fill("1");
  await step.getByRole("combobox", { name: "Source field", exact: true }).selectOption("name");
  await step.getByRole("combobox", { name: "Target field", exact: true }).selectOption("title");
  await shots(page, migration, "migration-step-editor", info);

  await migration.getByRole("button", { name: "Save migration plan", exact: true }).click();
  await migration.getByRole("button", { name: "Publish migration plan", exact: true }).click();
  await migration.getByRole("button", { name: "Review migration", exact: true }).click();
  await expect(migration.getByRole("region", { name: "Migration review", exact: true })).toContainText("2 existing records");
  const mode = migration.getByRole("group", { name: "Editing mode", exact: true });
  await mode.getByRole("button", { name: "Source editor", exact: true }).click();
  const source = migration.getByRole("textbox", { name: "Configuration source", exact: true });
  const validSource = await source.inputValue();
  expect(JSON.parse(validSource).plan.steps).toEqual(steps);
  const invalidWrites: string[] = [];
  await page.route("**/v1/submissions", async route => {
    const schema = route.request().postDataJSON().schema?.name;
    if (schema?.startsWith("build.migration")) invalidWrites.push(schema);
    await route.continue();
  });
  await source.fill("null");
  await migration.getByRole("button", { name: "Apply source", exact: true }).click();
  await mode.getByRole("button", { name: "Visual", exact: true }).click();
  await expect(mode.getByRole("button", { name: "Source editor", exact: true })).toHaveAttribute("aria-pressed", "true");
  await expect(source).toHaveValue("null");
  await expect(migration.getByRole("alert")).toBeVisible();
  await expect(migration.getByRole("button", { name: "Save migration plan", exact: true })).toHaveCount(0);
  await expect(migration.getByRole("button", { name: "Start reviewed migration", exact: true })).toHaveCount(0);
  expect(invalidWrites).toEqual([]);
  await page.unroute("**/v1/submissions");
  await source.fill(validSource);
  await migration.getByRole("button", { name: "Apply source", exact: true }).click();
  await mode.getByRole("button", { name: "Visual", exact: true }).click();
  await migration.getByRole("button", { name: "Save migration plan", exact: true }).click();
  await migration.getByRole("button", { name: "Publish migration plan", exact: true }).click();
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
  await expect(migrationProgress(migration, "Migrated")).toHaveText("1");
  expect(keys.length).toBeGreaterThanOrEqual(2);
  expect(new Set(keys).size).toBe(1);
  await page.unroute("**/v1/submissions");
  await expect(page.getByRole("button", { name: "Activate release", exact: true })).toBeDisabled();

  // Reopening the task reads accepted progress instead of creating another run.
  await page.reload();
  await page.getByRole("combobox", { name: "Saved candidate", exact: true }).selectOption(to);
  await expect(migrationProgress(migration, "Migrated")).toHaveText("1");
  await migration.getByRole("button", { name: "Run next migration batch", exact: true }).click();
  await expect(migrationProgress(migration, "Migrated")).toHaveText("2");
  await shots(page, migration, "migration-progress", info);
  await page.getByRole("button", { name: "Activate release", exact: true }).click();
  await expect(page.getByRole("status").filter({ hasText: "Release active for operators." })).toContainText(to);

  // New writes use the new name. The retained old name reads the same value.
  await operator.decide(`${object.type}.edit`, { type: object.type, id: recordID }, { title: "Changed after migration" });
  const record = await operator.record(object.type, recordID);
  expect(record.name).toBe("Changed after migration");
  expect(record.title).toBe("Changed after migration");
});

for (const change of ["rename", "split", "extract"] as const) {
  test(`a waiting flow finishes its retired action after ${change === "extract" ? "an" : "a"} ${change} migration`, async ({ page, builder, operator, browser }) => {
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
    const detailName = slug("detail"), detailID = fresh("DETAIL");
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

      if (change === "extract") {
        await builder.decide("build.object.create", { type: "build.object", id: detailID }, { name: detailName, title: "Extracted details", fields: [{ name: "name", title: "Name", type: "text", sourceType: "user-input", required: true }] });
        drafts.push({ kind: "object", id: detailID });
      }

      await builder.decide("build.object.edit", { type: "build.object", id: object.id }, { fields: [
        { name: "note", title: "Old note", type: "text", sourceType: "user-input", readOnly: true, aliasOf: change === "rename" ? "label" : undefined },
        { name: "label", title: "Label", type: "text", sourceType: "user-input" },
        { name: "enabled", title: "Enabled", type: "boolean", sourceType: "user-input", required: true },
        ...(change === "split" ? [{ name: "detail", title: "Detail", type: "text", sourceType: "user-input" }] : []),
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
      const fields = change === "rename"
        ? [{ from: "note", to: "label" }]
        : change === "split"
          ? [{ from: "note", to: "label", expr: { kind: "text", ref: "note" } }, { from: "note", to: "detail", expr: { kind: "text", ref: "note" } }]
          : [{ from: "note", to: "name", expr: { kind: "text", ref: "note" } }];
      await setMigrationSteps(migration, [
        { kind: change, types: [object.type], batch: 1, fields, ...(change === "extract" ? { target: `build.${detailName}`, identity: { kind: "reference", ref: "id" } } : {}) },
        { kind: "default", types: [object.type], batch: 1, fields: [{ to: "enabled", expr: { op: "", kind: "boolean", lit: { value: true } } }] },
        { kind: "retire-action", types: [object.type], action: `${object.type}.close`, batch: 1 },
      ]);
      await migration.getByRole("button", { name: "Save migration plan", exact: true }).click();
      await migration.getByRole("button", { name: "Publish migration plan", exact: true }).click();
      await migration.getByRole("button", { name: "Review migration", exact: true }).click();
      await migration.getByRole("button", { name: "Start reviewed migration", exact: true }).click();
      for (let batch = 0; batch < 3; batch++) {
        await migration.getByRole("button", { name: "Run next migration batch", exact: true }).click();
        if (batch < 2) await expect(migrationProgress(migration, "Next step")).toHaveText(`${batch + 2} / 3`);
      }
      await expect(migrationProgress(migration, "Migration state")).toHaveText("complete");
      await page.getByRole("button", { name: "Activate release", exact: true }).click();
      await expect(page.getByRole("status").filter({ hasText: "Release active for operators." })).toContainText(to);

      await inbox.reload();
      await inbox.getByRole("listitem").filter({ hasText: oldID }).getByRole("button", { name: "yes", exact: true }).click();
      await expect.poll(async () => (await operator.record(object.type, oldID)).state).toBe("done");
      const old = await operator.record(object.type, oldID);
      if (change === "extract") {
        expect(old.note).toBe("Original note");
        expect((await operator.record(`build.${detailName}`, oldID)).name).toBe("Old flow finished");
      } else {
        expect(old.label).toBe("Old flow finished");
      }
      expect(old.enabled).toBe(true);
      if (change === "split") {
        expect(old.note).toBe("Original note");
        expect(old.detail).toBe("Old flow finished");
      }
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
}

for (const redirect of [false, true]) {
test(redirect ? "a declared redirect opens and executes a new approval after target activation" : "action retirement rejects the pending request through its owner before activation", async ({ page, builder, operator }) => {
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
  const requests = await operator.read<any[]>("requests");
  const original = requests.find(request => request.target === `${object.type}/${recordID}`);
  expect(original.state).toBe("pending");
  const targetActions = redirect ? [{ name: "finishnew", title: "New finish", from: ["open"], to: "done", roles: ["user"], approval: { pending: "pending", rejected: "rejected", levels: [{ title: "Builder", role: "builder" }] } }] : [];
  await builder.decide("build.object.edit", { type: "build.object", id: object.id }, { actions: targetActions });
  const to = await builder.candidate(object.id);
  await builder.open(page, "/release-review");
  await page.getByRole("combobox", { name: "Saved candidate", exact: true }).selectOption(to);
  const migration = page.getByRole("region", { name: "Data migration", exact: true });
  await migration.getByRole("combobox", { name: "Migration object", exact: true }).selectOption(object.type);
  await setMigrationSteps(migration, [
    { kind: "retire-action", types: [object.type], action: `${object.type}.finish`, batch: 1, ...(redirect ? { redirect: `${object.type}.finishnew` } : {}) },
  ]);
  await migration.getByRole("button", { name: "Save migration plan", exact: true }).click();
  await migration.getByRole("button", { name: "Publish migration plan", exact: true }).click();
  await migration.getByRole("button", { name: "Review migration", exact: true }).click();
  await migration.getByRole("button", { name: "Start reviewed migration", exact: true }).click();
  await migration.getByRole("button", { name: "Run next migration batch", exact: true }).click();
  await expect(migrationProgress(migration, "Migration state")).toHaveText("complete");
  expect((await operator.record(object.type, recordID)).state).toBe(redirect ? "open" : "rejected");
  if (redirect) {
    const queued = (await operator.read<any[]>("requests")).find(request => request.id === original.id);
    expect(queued.state).toBe("redirected");
    expect(queued.submission).toBe(original.submission);
    expect(queued.redirectTo ?? "").toBe("");
  }
  await page.getByRole("button", { name: "Activate release", exact: true }).click();
  await expect(page.getByRole("status").filter({ hasText: "Release active for operators." })).toContainText(to);
  expect((await operator.definitions()).some((definition: any) => definition.ref.kind === "action" && definition.ref.name === `${object.type}.finish`)).toBe(false);
  if (redirect) {
    await expect.poll(async () => (await operator.read<any[]>("requests")).find(request => request.id === original.id)?.redirectTo).toBeTruthy();
    const after = await operator.read<any[]>("requests");
    const queued = after.find(request => request.id === original.id);
    const next = after.find(request => request.id === queued.redirectTo);
    expect(next.release).toBe(to);
    expect(next.action).toBe(`${object.type}.finishnew`);
    expect(next.state).toBe("pending");
    await builder.open(page, "/inbox");
    await page.getByRole("listitem").filter({ hasText: recordID }).getByRole("button", { name: "Approve", exact: true }).click();
    await expect.poll(async () => (await operator.record(object.type, recordID)).state).toBe("done");
    const complete = await operator.read<any[]>("requests");
    expect(complete.find(request => request.id === next.id).state).toBe("approved");
    expect(complete.find(request => request.id === original.id).submission).toBe(original.submission);
  }
});
}
