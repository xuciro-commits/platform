import { test, expect, slug, shots } from "./kit";
import type * as Api from "../../packages/kernel/src/gen/host";

test.use({ actionTimeout: 15_000 });

test("a package can be discarded, recreated, activated and initialized after a lost reply", async ({ page, builder }, info) => {
  test.setTimeout(180_000);
  const id = slug("package"), name = slug("entries"), app = slug("desk"), type = `build.${name}`;
  const first = slug("first"), second = slug("second"), third = slug("third");
  const route = `/package-preparation?id=${id}&version=1.0.0`;
  const descriptor = { id, version: "1.0.0", namespace: id, title: "Package journey", blueprint: {
    roots: [{ app: "build", kind: "app", name: app }],
    assets: [
      { ref: { app: "build", kind: "object", name: type }, contractVersion: 1, sourceVersion: "1", body: { type, name, title: "Journey entry", plural: "Journey entries", fields: [{ name: "note", title: "Note", type: "text", required: true }], states: [{ name: "open", title: "Open" }] } },
      { ref: { app: "build", kind: "app", name: app }, contractVersion: 1, sourceVersion: "1", body: { name: app, title: "Journey desk", pages: [name] } },
    ],
    setup: { roles: [{ app: "build", role: "user" }], steps: [first, second, third].map((record, index) => ({ name: `Initial entry ${index + 1}`, app: "build", schema: `${type}.create`, target: { type, id: record }, payload: JSON.stringify({ note: `Initial entry ${index + 1}` }) })) },
  } };
  await builder.open(page, "/apps");
  await page.getByRole("button", { name: "Add package", exact: true }).click();
  await page.getByLabel("Package source", { exact: true }).selectOption("file");
  await page.getByLabel("Package file", { exact: true }).setInputFiles({ name: "package.json", mimeType: "application/json", buffer: Buffer.from(JSON.stringify(descriptor)) });
  await page.getByRole("button", { name: "Review package", exact: true }).click();
  await page.getByRole("button", { name: "Add reviewed version to library", exact: true }).click();
  await page.getByRole("region", { name: "Package library", exact: true }).getByRole("button").filter({ hasText: id }).click();
  await page.getByRole("combobox", { name: "Exact package version", exact: true }).selectOption("1.0.0");
  await expect(page.getByText("No external connection is required.", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Prepare applications", exact: true }).click();
  await page.getByRole("button", { name: "Install package", exact: true }).click();
  await page.getByRole("button", { name: "Create application drafts", exact: true }).click();
  await page.getByRole("button", { name: "Edit application", exact: true }).click();
  await page.getByRole("button", { name: "Delete application draft", exact: true }).click();
  await page.getByRole("dialog").getByRole("button", { name: "Archive application", exact: true }).click();
  await expect.poll(async () => (await builder.record("build.app", app)).archived).toBe(true);
  await builder.open(page, route);
  await page.getByRole("button", { name: "Create application drafts", exact: true }).click();
  await page.getByRole("button", { name: "Publish application", exact: true }).waitFor();
  const preparation = await builder.read<Api.PackagePreparation>(`packages/${id}/preparation?version=1.0.0`);
  const recreated = preparation.applications[0]!.draftId!;
  expect(recreated).not.toBe(app);
  expect((await builder.record("build.object", name)).archived).not.toBe(true);
  await page.getByRole("button", { name: "Publish application", exact: true }).click();
  await page.getByRole("button", { name: "Check joint candidate", exact: true }).click();
  await page.getByRole("button", { name: "Save immutable candidate", exact: true }).click();
  await page.getByRole("button", { name: "Activate release", exact: true }).click();
  await expect(page.getByText("Release active for operators.", { exact: false })).toBeVisible();

  await builder.open(page, route);
  await page.getByRole("button", { name: "Open application", exact: true }).waitFor();
  const grant = page.getByRole("button", { name: "Grant this role to me", exact: true });
  if (await grant.count()) { await grant.click(); await expect(grant).toHaveCount(0); }
  await page.getByRole("checkbox", { name: "I reviewed the initial data requests.", exact: true }).check();
  // The second record is accepted, but its answer is lost. Continuing must
  // confirm that original submission rather than create it under a new key.
  let interrupted = false;
  const secondKeys = new Set<string>();
  await page.route("**/v1/submissions", async route => {
    if (route.request().postDataJSON().target?.id === second) secondKeys.add(route.request().postDataJSON().idempotencyKey);
    if (!interrupted && route.request().postDataJSON().target?.id === second) {
      interrupted = true;
      const accepted = await route.fetch();
      expect(accepted.ok()).toBe(true);
      await route.fulfill({ status: 503, json: { error: "Interrupted response" } });
    } else await route.continue();
  });
  await page.getByRole("button", { name: "Create initial data", exact: true }).click();
  await expect.poll(async () => interrupted).toBe(true);
  await expect(page.getByRole("alert").filter({ hasText: "Initial entry 2" })).toBeVisible();
  await page.reload();
  await page.getByRole("checkbox", { name: "I reviewed the initial data requests.", exact: true }).check();
  await page.getByRole("button", { name: "Continue initialization", exact: true }).click();
  await expect(page.getByText("Initial data is ready.", { exact: true })).toBeVisible();
  expect(await builder.record(type, first)).toMatchObject({ revision: 1, note: "Initial entry 1" });
  expect(await builder.record(type, second)).toMatchObject({ revision: 1, note: "Initial entry 2" });
  expect(await builder.record(type, third)).toMatchObject({ revision: 1, note: "Initial entry 3" });
  expect(secondKeys.size).toBe(1);
  await shots(page, page.locator("body"), "package-applications-ready", info);
  await page.getByRole("button", { name: "Open application", exact: true }).click();
  await expect(page.getByText("Initial entry 1", { exact: true })).toBeVisible();
  await builder.open(page, `/project?id=${recreated}`);
  await page.getByRole("button", { name: "Withdraw application entry", exact: true }).click();
  await expect(page.getByRole("button", { name: "Restore application entry", exact: true })).toBeVisible();
  await builder.open(page, `/page?app=build&kind=page&name=${name}&application=build:${app}`);
  await expect(page.getByRole("alert").filter({ hasText: "This application is unavailable. Reopen it from the application list." })).toBeVisible();
  await builder.open(page, `/project?id=${recreated}`);
  await page.getByRole("button", { name: "Restore application entry", exact: true }).click();
  expect(await builder.record(type, first)).toMatchObject({ revision: 1, note: "Initial entry 1" });
});
