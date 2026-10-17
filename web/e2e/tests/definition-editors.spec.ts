import { expect, fresh, slug, test } from "./kit";

test("definition editors retain accepted revisions through refusal, stale reads and discard", async ({ page, builder }) => {
  test.setTimeout(120_000);
  const model = await builder.object({ title: "Definition editor source", fields: [{ name: "note", title: "Note", type: "text" }] });
  const common = { name: slug("definition"), title: "Saved definition", description: "Definition editor lifecycle" };
  const cases = [
    { view: "query", type: "build.query", title: "Query title", save: "Save query", payload: { object: model.type, domain: [], sort: ["id"], limit: 50 } },
    { view: "link-type", type: "build.linktype", title: "Relationship title", save: "Save relationship", payload: { parent: model.type, child: model.type, forward: "children", reverse: "parent", cardinality: "one-to-many", deletePolicy: "owner", storage: "edge", roles: ["builder", "user"] } },
    { view: "property-type", type: "build.propertytype", title: "Shared property title", save: "Save shared property", payload: { type: "text" } },
    { view: "agent", type: "build.agent", title: "Agent title", save: "Save agent", payload: { instructions: "Review the note through its declared action", tools: [model.type + ".edit"] } },
    { view: "function", type: "build.function", title: "Function title", save: "Save function", payload: { object: model.type, fields: ["note"], instructions: "Summarize the note", output: [{ name: "summary", type: "string", required: true, description: "Summary" }], maxInputBytes: 4096, maxOutputBytes: 1024, maxTokens: 256, roles: ["builder", "user"] } },
    { view: "alert", type: "build.alertrule", title: "Rule title", save: "Save alert rule", payload: { description: undefined, object: model.type, field: "note", operator: "=", value: "ready", message: "Check the note", role: "builder", active: true } },
    { view: "decision-table", type: "build.table", title: "Table title", save: "Save decision table", payload: { inputs: [{ name: "note", type: "text" }], outputs: [{ name: "result", type: "text" }], rows: [{ when: ["*"], then: ["ready"] }], default: [] } },
  ];
  for (const item of cases) {
    const id = fresh("DEFINITION");
    await builder.decide(item.type + ".create", { type: item.type, id }, { ...common, ...item.payload });
    const before = await builder.record(item.type, id);
    let refuse = true, writes = 0, stale = false, expectedRevision = before.revision, installs = 0;
    await page.route("**/v1/submissions", async route => {
      const input = route.request().postDataJSON();
      if (input.schema?.name === item.type + ".edit" && input.target?.id === id) {
        writes++;
        expect(input.expectedRevision).toBe(expectedRevision);
        if (refuse) {
          refuse = false;
          return route.fulfill({ status: 400, contentType: "application/json", body: JSON.stringify({ error: { code: "ERROR_CODE_CONFLICT", message: "Definition edit refused" } }) });
        }
        expectedRevision++;
      } else if (input.schema?.name === item.type + ".publish" && input.target?.id === id) {
        installs++;
        expect(input.expectedRevision).toBe(expectedRevision);
        if (installs === 1) return route.fulfill({ status: 400, contentType: "application/json", body: JSON.stringify({ error: { code: "ERROR_CODE_CONFLICT", message: "Definition install refused" } }) });
        expectedRevision++;
      }
      await route.continue();
    });
    const recordPath = "/v1/records/" + item.type + "/" + id, recordURL = "**" + recordPath;
    // This test controls the record through HTTP. A current live snapshot
    // otherwise overrides the deliberately stale response in EdgeClient.get.
    await page.route("**/v1/changes?watch=**", async route => {
      const url = new URL(route.request().url());
      const paths = JSON.parse(url.searchParams.get("watch")!) as string[];
      url.searchParams.set("watch", JSON.stringify(paths.filter(path => path !== recordPath)));
      await route.continue({ url: url.toString() });
    });
    await page.route(recordURL, async route => stale
      ? route.fulfill({ status: 200, contentType: "application/json", body: JSON.stringify({ record: before }) })
      : route.continue());
    await builder.open(page, "/" + item.view + "?id=" + id);
    const title = page.getByRole("textbox", { name: item.title, exact: true });
    await expect(title).toHaveValue(common.title);
    await title.fill("Accepted definition");
    await page.getByRole("button", { name: item.save, exact: true }).click();
    await expect(page.getByRole("alert").filter({ hasText: "Definition edit refused" })).toBeVisible();
    await expect(title).toHaveValue("Accepted definition");
    expect((await builder.record(item.type, id)).revision).toBe(before.revision);
    stale = true;
    await page.getByRole("button", { name: item.save, exact: true }).click();
    await expect(page.getByRole("alert").filter({ hasText: /could not be reloaded|Reload the saved function/ })).toBeVisible();
    expect(await builder.record(item.type, id)).toMatchObject({ title: "Accepted definition", revision: before.revision + 1 });
    await title.fill("Discard this definition");
    if (["function", "alert", "decision-table"].includes(item.view)) {
      await page.getByRole("button", { name: "Publish options", exact: true }).click();
      await page.getByRole("menuitem", { name: "Discard unsaved changes", exact: true }).click();
    } else await page.getByRole("button", { name: item.view === "agent" ? "Discard" : "Cancel changes", exact: true }).click();
    await expect(title).toHaveValue("Accepted definition");
    stale = false;
    await title.fill("Final definition");
    await page.getByRole("button", { name: item.save, exact: true }).click();
    await expect.poll(async () => (await builder.record(item.type, id)).revision).toBe(before.revision + 2);
    await expect(title).toHaveValue("Final definition");
    expect(writes).toBe(3);
    if (item.view === "property-type") {
      await page.getByRole("button", { name: "Direct install", exact: true }).click();
      await expect(page.getByRole("alert").filter({ hasText: "Definition install refused" })).toBeVisible();
      expect((await builder.record(item.type, id)).revision).toBe(expectedRevision);
      stale = true;
      await page.getByRole("button", { name: "Direct install", exact: true }).click();
      await expect(page.getByRole("alert").filter({ hasText: "installed, but its current record could not be reloaded" })).toBeVisible();
      expect(await builder.record(item.type, id)).toMatchObject({ revision: expectedRevision, version: 1 });
      stale = false;
      await title.fill("After publication");
      await page.getByRole("button", { name: item.save, exact: true }).click();
      await expect.poll(async () => (await builder.record(item.type, id)).title).toBe("After publication");
      expect(installs).toBe(2);
      expect((await builder.record(item.type, id)).revision).toBe(expectedRevision);
    }
    await page.unroute("**/v1/submissions");
    await page.unroute(recordURL);
    await page.unroute("**/v1/changes?watch=**");
  }
});
