import { once } from "node:events";
import { createServer } from "node:http";
import { expect, fresh, shots, slug, test } from "./kit";

test.use({ actionTimeout: 10_000 });

test("an application assembles integration drafts over a saved model before joint delivery", async ({ page, builder, operator }, info) => {
  test.setTimeout(150_000);
  const name = slug("integration"), objectID = fresh("MODEL"), type = `build.${name}`;
  let pulls = 0, deliveries = 0;
  const external = createServer(async (request, response) => {
    response.setHeader("Content-Type", "application/json");
    if (request.method === "POST") {
      for await (const _chunk of request) { /* consume the original writeback body */ }
      deliveries++;
      response.writeHead(201).end(JSON.stringify({ receipt: "EXTERNAL-RECEIPT" }));
    } else if (request.url === "/rows") {
      pulls++;
      response.end(JSON.stringify([{ id: "FROM-SOURCE", note: "Imported note" }]));
    } else response.end("{}");
  });
  external.listen(0, "127.0.0.1");
  await once(external, "listening");
  try {
    const address = external.address();
    if (!address || typeof address === "string") throw Error("External system did not listen");
    await builder.decide("build.object.create", { type: "build.object", id: objectID }, {
      name, title: "Saved receiving model", fields: [
        { name: "note", title: "Imported note", type: "text" },
        { name: "receipt", title: "External receipt", type: "text" },
      ], states: [{ name: "open", title: "Open" }],
    });
    const application = await builder.application({ title: "Integration drafting application", resources: [{ app: "build", kind: "object", name: type }] });
    const original = await operator.definitions();
    await builder.open(page, `/project?id=${application.id}&area=assemble`);
    const create = async (label: string) => {
      await page.getByRole("button", { name: "New", exact: true }).click();
      await page.getByRole("menuitem", { name: label, exact: true }).click();
      await expect(page.getByRole("dialog")).toHaveCount(0);
    };
    const back = async () => {
      await page.getByRole("navigation", { name: "Application context", exact: true }).getByRole("button", { name: "Integration drafting application", exact: true }).click();
      await page.getByRole("navigation", { name: "Main", exact: true }).getByRole("button", { name: "Assemble", exact: true }).click();
    };
    const savedID = async (save: string) => {
      await page.getByRole("button", { name: save, exact: true }).click();
      await expect(page).not.toHaveURL(/[?&]id=new(?:&|$)/);
      return new URLSearchParams(new URL(page.url()).hash.split("?")[1]).get("id")!;
    };

    await create("Dataset");
    await page.getByRole("textbox", { name: "Dataset name", exact: true }).fill(`${name}raw`);
    await page.getByRole("textbox", { name: "Dataset title", exact: true }).fill("Saved input rows");
    // Saving the definition and joining an application are separate writes.
    // A refused join must retry that same definition rather than create it again.
    let refuseJoin = true;
    await page.route("**/v1/submissions", async route => {
      const input = route.request().postDataJSON();
      if (refuseJoin && input.schema?.name === "build.app.edit" && input.target?.id === application.id) {
        refuseJoin = false;
        return route.fulfill({ status: 400, contentType: "application/json", body: JSON.stringify({ error: { code: "ERROR_CODE_CONFLICT", message: "Application join refused" } }) });
      }
      await route.continue();
    });
    await page.getByRole("button", { name: "Save dataset", exact: true }).click();
    await expect(page.getByRole("alert").filter({ hasText: "Application join refused" })).toBeVisible();
    const datasets = await builder.read<{ records: { id: string; name: string; revision: number }[] }>("records/build.dataset?limit=1000");
    const dataset = datasets.records.find(record => record.name === `${name}raw`)!;
    expect(dataset).toBeTruthy();
    expect(await savedID("Save dataset")).toBe(dataset.id);
    expect((await builder.record("build.dataset", dataset.id)).revision).toBe(dataset.revision);
    await page.unroute("**/v1/submissions");

    await back();
    await create("Connection");
    await page.getByRole("textbox", { name: "Connection name", exact: true }).fill(`${name}connection`);
    await page.getByRole("textbox", { name: "Connection title", exact: true }).fill("Saved external connection");
    await page.getByRole("textbox", { name: "Address", exact: true }).fill(`http://127.0.0.1:${address.port}/`);
    await page.getByRole("checkbox", { name: "Allow http and private addresses (on-premise systems)", exact: true }).check();
    const connection = await savedID("Save connection");
    expect((await builder.record("build.connection", connection)).state).toBe("draft");

    await back();
    await create("Data source");
    await page.getByRole("textbox", { name: "Source name", exact: true }).fill(`${name}source`);
    await page.getByRole("textbox", { name: "Source title", exact: true }).fill("Saved receiving feed");
    await page.getByRole("combobox", { name: /^Connection/ }).selectOption(connection);
    await expect(page.getByRole("status").filter({ hasText: "This saved connection is not ready." })).toBeVisible();
    await page.getByRole("textbox", { name: /^URL/ }).fill("rows");
    await page.getByRole("combobox", { name: "Target object", exact: true }).selectOption(type);
    await page.getByRole("button", { name: "Add mapping", exact: true }).click();
    await page.getByRole("textbox", { name: "Row field", exact: true }).fill("note");
    await page.getByRole("combobox", { name: "Object field", exact: true }).selectOption("note");
    await page.getByRole("button", { name: "Publish", exact: true }).click();
    await expect(page.getByRole("alert").filter({ hasText: "The connection is not ready" })).toBeVisible();
    await expect(page).toHaveURL(/[?&]id=new(?:&|$)/);
    const sources = await builder.read<{ records: { id: string; name: string; revision: number }[] }>("records/build.source?limit=1000");
    const sourceDraft = sources.records.find(record => record.name === `${name}source`)!;
    const source = await savedID("Save data source");
    expect(source).toBe(sourceDraft.id);
    expect((await builder.record("build.source", source)).revision).toBe(sourceDraft.revision);

    await back();
    await create("Pipeline");
    await page.getByRole("textbox", { name: "Pipeline name", exact: true }).fill(`${name}pipeline`);
    await page.getByRole("textbox", { name: "Pipeline title", exact: true }).fill("Saved receiving pipeline");
    await page.getByRole("combobox", { name: /^Input dataset/ }).selectOption(dataset.id);
    await page.getByRole("combobox", { name: "Write to", exact: true }).selectOption("object");
    await page.getByRole("combobox", { name: /^Output object/ }).selectOption(type);
    await page.getByRole("textbox", { name: /^Record id column/ }).fill("id");
    await savedID("Save pipeline");

    await back();
    await create("Writeback");
    await page.getByRole("textbox", { name: "Writeback name", exact: true }).fill(`${name}writeback`);
    await page.getByRole("textbox", { name: "Writeback title", exact: true }).fill("Saved receipt delivery");
    await page.getByRole("combobox", { name: "Object", exact: true }).selectOption(type);
    await page.getByRole("combobox", { name: /^Connection/ }).selectOption(connection);
    await page.getByRole("textbox", { name: /^Path/ }).fill("receipts");
    await page.getByRole("button", { name: "Add body field", exact: true }).click();
    await page.getByRole("combobox", { name: "Payload field", exact: true }).selectOption("note");
    await page.getByRole("textbox", { name: "Body key", exact: true }).fill("note");
    await page.getByRole("button", { name: "Add answer field", exact: true }).click();
    await page.getByRole("textbox", { name: "Answer key", exact: true }).fill("receipt");
    await page.getByRole("combobox", { name: "Record field", exact: true }).selectOption("receipt");
    await savedID("Save writeback");

    await back();
    await create("Matching rule");
    await page.getByRole("textbox", { name: "Rule name", exact: true }).fill(`${name}matching`);
    await page.getByRole("textbox", { name: "Rule title", exact: true }).fill("Saved receiving identity");
    await page.getByRole("combobox", { name: "Object", exact: true }).selectOption(type);
    await page.getByRole("button", { name: "Add key", exact: true }).click();
    await page.getByLabel("Fields", { exact: true }).fill("note");
    await savedID("Save matching rule");
    await shots(page, page.locator("body"), "application-integration-drafts", info);

    const members = (await builder.record("build.app", application.id)).resources;
    for (const kind of ["object", "dataset", "connection", "source", "pipeline", "writeback", "matching"]) {
      expect(members.some((member: { kind: string }) => member.kind === kind)).toBe(true);
    }
    expect(pulls).toBe(0);
    expect(deliveries).toBe(0);
    expect(await operator.definitions()).toEqual(original);
    await builder.open(page, `/connection?id=${connection}&application=${application.id}`);
    await page.getByRole("button", { name: "Check", exact: true }).click();
    await expect.poll(async () => (await builder.record("build.connection", connection)).state, { timeout: 15_000 }).toBe("ready");
    await builder.open(page, `/project?id=${application.id}&area=verify`);
    const verify = page.getByRole("region", { name: "Verification and delivery", exact: true });
    await verify.getByRole("button", { name: "Add the drafts of Integration drafting application", exact: true }).click();
    await verify.getByRole("button", { name: "Check joint candidate", exact: true }).click();
    await verify.getByRole("button", { name: "Save immutable candidate", exact: true }).click();
    expect((await builder.record("build.object", objectID)).published ?? "").toBe("");
    expect((await builder.record("build.source", source)).state).toBe("draft");
    expect(await operator.definitions()).toEqual(original);
    await verify.getByRole("button", { name: "Activate release", exact: true }).click();
    await expect(verify.getByRole("status").filter({ hasText: "Release active for operators." })).toBeVisible();
    await expect.poll(async () => (await builder.record(type, "FROM-SOURCE"))?.receipt, { timeout: 20_000 }).toBe("EXTERNAL-RECEIPT");
    expect(pulls).toBeGreaterThan(0);
    expect(deliveries).toBe(1);
    expect((await builder.record(type, "FROM-SOURCE")).note).toBe("Imported note");
    await expect(operator.decide("build.source.edit", { type: "build.source", id: source }, { title: "Unauthorized" })).rejects.toThrow(/DENIED/);
  } finally {
    await new Promise<void>((resolve, reject) => external.close(error => error ? reject(error) : resolve()));
  }
});
