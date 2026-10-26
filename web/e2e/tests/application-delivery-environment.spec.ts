import { expect, fresh, pageUIProfile, slug, test } from "./kit";
import type * as Api from "../../packages/kernel/src/gen/host";

test.use({ actionTimeout: 10_000 });

test("an application publishes without configured external services and refuses only the operations that need them", async ({ page, builder }) => {
  test.setTimeout(120_000);
  const id = slug("delivery"), name = slug("entry"), type = `build.${name}`, app = slug("desk"), pageName = slug("notes");
  const connection = slug("external"), source = slug("intake"), fn = slug("summary"), sample = fresh("OPTIONAL");
  const ref = (kind: string, name: string) => ({ app: "build", kind, name });
  const pageDefinition: Api.Page = {
    name: pageName, title: "Local notes", object: ref("object", type), layout: "composed", listFields: [], detailFields: [], actions: [],
    sections: [
      { id: "notes", widget: "table", configVersion: 1, fields: ["note"] },
      { id: "summary", widget: "ai-assistant", configVersion: 1, function: { ref: ref("function", fn), sourceVersion: "1.function-1" }, recordVariable: "record", ai: { kind: "analyst" } },
    ],
    document: { formatVersion: 2, uiProfile: pageUIProfile, root: "root", nodes: { root: { kind: "rows", children: ["notes", "summary"] }, notes: { kind: "widget", section: "notes" }, summary: { kind: "widget", section: "summary" } }, variables: { record: { scope: "page", type: "record", mode: "resource", source: { kind: "record", section: "notes" } } } },
  };
  const descriptor = { id, version: "1.0.0", namespace: id, title: "Independent application", blueprint: {
    roots: [ref("app", app)],
    assets: [
      { ref: ref("object", type), contractVersion: 1, sourceVersion: "1", body: { type, name, title: "Local note", plural: "Local notes", fields: [{ name: "note", title: "Note", type: "text", aiInput: true }], states: [{ name: "open", title: "Open" }] } },
      { ref: ref("connection", connection), contractVersion: 1, sourceVersion: "1.connection-1", body: { name: connection, title: "Unconfigured external service", kind: "http", credential: true } },
      { ref: ref("source", source), contractVersion: 1, sourceVersion: "1.source-1", body: { name: source, title: "External note intake", connection, profile: "json", url: "/rows", path: "rows", object: type, key: "id", mapping: [{ from: "note", to: "note" }] } },
      { ref: ref("function", fn), contractVersion: 1, sourceVersion: "1.function-1", body: { name: fn, title: "Summarize a note", description: "Return a short summary of the supplied note", object: type, fields: ["note"], instructions: "Summarize only the supplied note", model: "", output: [{ name: "summary", type: "string", required: true, description: "Note summary" }], maxInputBytes: 4096, maxOutputBytes: 1024, maxTokens: 256, roles: ["builder", "user"] } },
      { ref: ref("page", pageName), contractVersion: 1, sourceVersion: "1", body: pageDefinition },
      { ref: ref("app", app), contractVersion: 1, sourceVersion: "1", body: { name: app, title: "Independent note desk", pages: [pageName], resources: [ref("object", type), ref("connection", connection), ref("source", source), ref("function", fn)] } },
    ],
    setup: { roles: [{ app: "build", role: "user" }], steps: [{ name: "Optional sample note", app: "build", schema: `${type}.create`, target: { type, id: sample }, payload: JSON.stringify({ note: "Optional sample" }) }] },
  } };
  // This route runs in its own fresh host. Keep its original empty AI
  // configuration, and verify it rather than creating an artificial binding.
  const models = await builder.read<Api.ModelSelection>("ai/model-selection?purpose=generation");
  expect(models.models).toHaveLength(0);
  expect(models.default).toBeFalsy();
  await builder.open(page, "/apps");
  await page.getByRole("button", { name: "Add package", exact: true }).click();
  await page.getByLabel("Package source", { exact: true }).selectOption("file");
  await page.getByLabel("Package file", { exact: true }).setInputFiles({ name: "application.json", mimeType: "application/json", buffer: Buffer.from(JSON.stringify(descriptor)) });
  const inspection = page.waitForResponse(response => response.request().method() === "POST" && response.url().endsWith("/v1/packages/inspect"));
  await page.getByRole("button", { name: "Review package", exact: true }).click();
  const inspected = await inspection;
  expect(inspected.ok(), await inspected.text()).toBe(true);
  const preview = await inspected.json() as Api.PackagePreview;
  expect(preview.precheck.ok, JSON.stringify(preview.precheck)).toBe(true);
  await page.getByRole("button", { name: "Add reviewed version to library", exact: true }).click();
  const route = `/package-preparation?id=${id}&version=1.0.0`;
  await builder.open(page, route);
  await page.getByRole("button", { name: "Install package", exact: true }).click();
  const creation = page.waitForResponse(response => response.request().method() === "POST" && response.url().includes(`/v1/packages/${id}/drafts`));
  await page.getByRole("button", { name: "Create application drafts", exact: true }).click();
  const created = await creation;
  expect(created.ok(), await created.text()).toBe(true);
  expect(((await created.json()) as Api.PackageDraftResult).drafts.length).toBeGreaterThan(0);
  await expect(page.getByText("Needs configuration", { exact: true })).toBeVisible();
  await expect(page.getByText("Needs a model", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Publish application", exact: true }).click();
  const candidate = page.waitForResponse(response => response.request().method() === "POST" && response.url().endsWith("/v1/releases/preview"));
  await page.getByRole("button", { name: "Check joint candidate", exact: true }).click();
  const checked = await candidate;
  expect(checked.ok(), await checked.text()).toBe(true);
  const reviewed = await checked.json() as Api.ReleasePreview;
  expect(reviewed.diagnostic, JSON.stringify(reviewed)).toBeFalsy();
  expect(reviewed.candidateId).toBeTruthy();
  await page.getByRole("button", { name: "Save immutable candidate", exact: true }).click();
  await expect(page.getByText("This candidate is not fully evaluated. Configure models and run evaluations when needed; no quality result is claimed.", { exact: true })).toBeVisible();
  const activation = page.waitForResponse(response => response.request().method() === "POST" && response.url().endsWith("/v1/releases/active"));
  await page.getByRole("button", { name: "Activate release", exact: true }).click();
  const activated = await activation;
  expect(activated.ok(), await activated.text()).toBe(true);
  await expect(page.getByText("Release active for operators.", { exact: false })).toBeVisible();
  const preparation = await builder.read<Api.PackagePreparation>(`packages/${id}/preparation?version=1.0.0`);
  expect(preparation.applications.every(application => application.published)).toBe(true);
  expect(preparation.connections[0]).toMatchObject({ configured: false, ready: false });
  expect(preparation.ai[0]?.ready).toBe(false);
  const intake = preparation.connections[0]!;
  const sources = await builder.read<{ records: { id: string; name: string; requested?: string; last?: unknown }[] }>("records/build.source?limit=1000");
  const sourceDraft = sources.records.find(record => record.name === source)!;
  expect(sourceDraft.requested).toBeFalsy(); expect(sourceDraft.last).toBeFalsy();
  await builder.open(page, `/data-source?id=${sourceDraft.id}`);
  await page.getByRole("group", { name: "Editing mode", exact: true }).getByRole("button", { name: "Visual", exact: true }).click();
  const pull = page.waitForResponse(response => response.request().method() === "POST" && response.url().endsWith("/v1/submissions") && response.request().postDataJSON().schema?.name === "build.source.pull");
  await page.getByRole("button", { name: "Pull now", exact: true }).click();
  expect((await (await pull).json()).error).toBeTruthy();
  await expect(page.getByRole("alert").filter({ hasText: /connection|address|configur|endpoint/i })).toBeVisible();
  expect((await builder.record("build.source", sourceDraft.id)).requested).toBeFalsy();
  expect((await builder.record("build.connection", intake.draftId!)).state).not.toBe("ready");
  await builder.records(type, [{ note: "A local record works before external setup" }]);
  await builder.open(page, route);
  await expect(page.getByText("Optional initial data", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Open application", exact: true }).click();
  const note = page.getByRole("cell", { name: "A local record works before external setup", exact: true });
  await expect(note).toBeVisible(); await note.click();
  const analysis = page.waitForResponse(response => response.request().method() === "POST" && response.url().endsWith("/v1/submissions") && response.request().postDataJSON().schema?.name === "build.function-call.start");
  await page.getByRole("button", { name: "Request original analysis", exact: true }).click();
  expect((await (await analysis).json()).error).toBeTruthy();
  await expect(page.getByRole("alert").filter({ hasText: /model|AI configuration/i })).toBeVisible();
  expect(await builder.record(type, sample)).toBeUndefined();
});
