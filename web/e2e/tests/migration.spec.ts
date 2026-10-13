import { expect, shots, slug, test } from "./kit";

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
