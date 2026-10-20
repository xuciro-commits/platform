import { expect, fresh, shots, slug, test } from "./kit";

test.use({ actionTimeout: 15_000 });

test("automation cards retain custom conditions and return values when edited or opened as a flow", async ({ page, builder }, info) => {
  test.setTimeout(90_000);
  const id = fresh("AUTOMATION");
  const steps = [
    { name: "eligible", kind: "branch", condition: { op: "eq", left: { source: "literal", value: true }, right: { source: "literal", value: true } }, cases: { true: "write", false: "skipped" } },
    { name: "write", title: "Create dataset", kind: "action", act: "build.dataset.create", target: { source: "literal", value: "output" }, inputs: { name: { source: "literal", value: "output" }, title: { source: "literal", value: "Output" } }, error: "failed", timeoutSeconds: 10 },
    { name: "skipped", kind: "end", value: { source: "literal", value: { status: "skipped" } } },
    { name: "failed", kind: "end", value: { source: "literal", value: { status: "failed" } } },
  ];
  await builder.decide("build.process.create", { type: "build.process", id }, {
    name: slug("linearedit"), title: "Retained automation", kind: "automation", every: "1h", steps,
  });
  await builder.open(page, `/automation?id=${id}`);
  await page.getByRole("button", { name: "Visual", exact: true }).click();
  await expect(page.getByRole("button", { name: /^If Condition/ })).toBeVisible();
  await page.getByRole("button", { name: /^Then .*Create dataset/ }).click();
  const inspector = page.getByRole("region", { name: "Workflow properties", exact: true });
  await inspector.getByRole("tab", { name: "Settings", exact: true }).click();
  await inspector.getByRole("textbox", { name: "Step title", exact: true }).fill("Create retained dataset");
  await expect.poll(async () => (await builder.record("build.process", id)).steps[1].title).toBe("Create retained dataset");
  const saved = await builder.record("build.process", id);
  expect(saved.steps[0]).toEqual(steps[0]);
  expect(saved.steps[1]).toMatchObject({ ...steps[1], title: "Create retained dataset" });
  expect(saved.steps.slice(2)).toEqual(steps.slice(2));
  await shots(page, page.locator("body"), "automation-custom-condition", info);

  // A legitimate graph that the cards cannot represent stays in the original
  // process and opens the original flow editor without an implicit rewrite.
  await builder.decide("build.process.edit", { type: "build.process", id }, { kind: "flow", manual: true, every: "" });
  await expect(page.getByText("This automation uses settings that need the flow map. Open it there to keep every step and result.", { exact: true })).toBeVisible();
  const graph = await builder.record("build.process", id);
  await page.getByRole("button", { name: "Open on the flow map", exact: true }).click();
  await expect.poll(() => new URL(page.url()).hash.startsWith("#/flow?")).toBe(true);
  const reopened = await builder.record("build.process", id);
  expect(reopened.revision).toBe(graph.revision);
  expect(reopened.steps).toEqual(graph.steps);
});

test("a saved process keeps its accepted revision when the following read fails", async ({ page, builder }) => {
  test.setTimeout(90_000);
  const id = fresh("FLOW");
  await builder.decide("build.process.create", { type: "build.process", id }, {
    name: slug("retainedrevision"), title: "Original flow", kind: "flow", manual: true,
    steps: [{ name: "done", kind: "end" }],
  });
  await builder.open(page, `/flow?id=${id}`);
  const title = page.getByRole("region", { name: "Workflow properties", exact: true }).getByRole("textbox", { name: "Workflow title", exact: true });
  await expect(title).toHaveValue("Original flow");
  const recordPath = `**/v1/records/build.process/${id}`;
  await page.route(recordPath, route => route.request().method() === "GET" ? route.abort("failed") : route.continue());
  const watchPath = "**/v1/changes?*";
  await page.route(watchPath, route => {
    const watched = JSON.parse(new URL(route.request().url()).searchParams.get("watch") ?? "[]") as string[];
    return watched.includes(`/v1/records/build.process/${id}`) ? route.abort("failed") : route.continue();
  });
  await title.fill("Accepted before read failure");
  await page.keyboard.press("ControlOrMeta+s");
  await expect.poll(async () => (await builder.record("build.process", id)).title).toBe("Accepted before read failure");
  const accepted = await builder.record("build.process", id);
  await expect(page.getByText("The resource was saved, but its current record could not be reloaded.", { exact: true })).toBeVisible({ timeout: 15_000 });
  await title.fill("Discard this edit");
  await page.getByRole("button", { name: "Publish options", exact: true }).click();
  await page.getByRole("menuitem", { name: "Discard unsaved changes", exact: true }).click();
  await expect(title).toHaveValue("Accepted before read failure");
  expect((await builder.record("build.process", id)).revision).toBe(accepted.revision);
  await page.unroute(recordPath);
  await page.unroute(watchPath);
  await title.fill("Next accepted edit");
  await page.keyboard.press("ControlOrMeta+s");
  await expect.poll(async () => (await builder.record("build.process", id)).title).toBe("Next accepted edit");
  expect((await builder.record("build.process", id)).revision).toBe(accepted.revision + 1);
});
