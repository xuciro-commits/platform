import { expect, fresh, slug, test } from "./kit";

test("integration editors retain one saved draft through refusal, membership retry and discard", async ({ page, builder }) => {
  test.setTimeout(120_000);
  const application = await builder.application({ title: "Integration editor checks" });
  const cases = [
    { view: "connection", type: "build.connection", kind: "connection", name: "Connection name", title: "Connection title", save: "Save connection" },
    { view: "data-source", type: "build.source", kind: "source", name: "Source name", title: "Source title", save: "Save data source" },
    { view: "dataset", type: "build.dataset", kind: "dataset", name: "Dataset name", title: "Dataset title", save: "Save dataset" },
    { view: "pipeline", type: "build.pipeline", kind: "pipeline", name: "Pipeline name", title: "Pipeline title", save: "Save pipeline" },
    { view: "match", type: "build.match", kind: "matching", name: "Rule name", title: "Rule title", save: "Save matching rule" },
    { view: "writeback", type: "build.writeback", kind: "writeback", name: "Writeback name", title: "Writeback title", save: "Save writeback" },
  ];
  const writes: { type: string; id: string }[] = [];
  let rejectJoin = true;
  await page.route(url => url.pathname.endsWith("/submissions"), async route => {
    const submission = route.request().postDataJSON();
    if (cases.some(item => submission.schema?.name === `${item.type}.create`)) writes.push(submission.target);
    if (rejectJoin && submission.schema?.name === "build.app.edit" && submission.target?.id === application.id) {
      rejectJoin = false;
      return route.fulfill({ status: 400, contentType: "application/json", body: JSON.stringify({ error: { code: "ERROR_CODE_CONFLICT", message: "Application join refused" } }) });
    }
    await route.continue();
  });
  for (const [index, item] of cases.entries()) {
    const name = slug("integration"), title = `Integration ${item.kind}`;
    await builder.open(page, `/${item.view}?id=new&application=${application.id}`);
    await page.getByRole("textbox", { name: item.name, exact: true }).fill(name);
    await page.getByRole("textbox", { name: item.title, exact: true }).fill(title);
    if (item.type === "build.source") await page.getByRole("textbox", { name: "URL", exact: true }).fill("https://example.com/rows");
    if (item.type === "build.pipeline") {
      const dataset = fresh("INPUT");
      await builder.decide("build.dataset.create", { type: "build.dataset", id: dataset }, { name: slug("input"), title: "Pipeline input" });
      await page.getByRole("combobox", { name: "Input dataset", exact: true }).selectOption(dataset);
    }
    if (item.type === "build.match" || item.type === "build.writeback") await page.getByRole("combobox", { name: "Object", exact: true }).selectOption("work.task");
    if (item.type === "build.writeback") {
      const connections = await builder.read<{ records: { id: string; name: string }[] }>("records/build.connection?limit=100");
      await page.getByRole("combobox", { name: /^Connection/ }).selectOption(connections.records.find(record => writes.some(write => write.type === "build.connection" && write.id === record.id))!.id);
    }
    await page.getByRole("button", { name: item.save, exact: true }).click();
    if (index === 0) {
      await expect(page.getByRole("alert").filter({ hasText: "Application join refused" })).toBeVisible();
      await page.getByRole("button", { name: item.save, exact: true }).click();
    }
    await expect(page).toHaveURL(url => {
      const route = new URL(url.hash.slice(1), url.origin);
      return route.pathname === `/${item.view}` && !!route.searchParams.get("id") && route.searchParams.get("id") !== "new";
    });
    const created = writes.filter(write => write.type === item.type);
    expect(created).toHaveLength(1);
    const saved = await builder.record(item.type, created[0]!.id);
    expect(saved).toMatchObject({ name, title, revision: 1 });
    await expect.poll(async () => (await builder.record("build.app", application.id)).resources).toEqual(expect.arrayContaining([{ app: "build", kind: item.kind, name }]));
    await page.getByRole("textbox", { name: item.title, exact: true }).fill(`${title} changed`);
    await page.getByRole("button", { name: "Discard", exact: true }).click();
    await expect(page.getByRole("textbox", { name: item.title, exact: true })).toHaveValue(title);
    expect(await builder.record(item.type, saved.id)).toEqual(saved);
  }
});
