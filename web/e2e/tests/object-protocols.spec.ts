import { expect, fresh, selectObjectSection, shots, slug, test } from "./kit";

test("an object provides a versioned protocol through its original actions and release", async ({ page, builder, request }, info) => {
  test.setTimeout(150_000);
  const name = slug("provider"), protocol = `${name}.orders`;
  const object = await builder.object({
    name, title: "Protocol provider", fields: [{ name: "note", title: "Order note", type: "text" }],
    states: [{ name: "open", title: "Open" }, { name: "done", title: "Done" }],
    actions: [{ name: "complete", title: "Complete order", from: ["open"], to: "done" }],
  });
  const [record] = await builder.records(object.type, [{ note: "Awaiting completion" }]);
  const original = await builder.record(object.type, record!);
  await builder.open(page, `/object-type?id=${object.id}&tab=protocols`);
  await page.getByRole("button", { name: "Add protocol", exact: true }).click();
  const binding = page.getByRole("group", { name: "Protocol binding 1", exact: true });
  await binding.getByRole("textbox", { name: "Protocol name", exact: true }).fill(protocol);
  await binding.getByRole("spinbutton", { name: "Protocol version", exact: true }).fill("2");
  const actions = binding.getByRole("group", { name: "Protocol action mappings", exact: true });
  await actions.getByRole("textbox", { name: "Protocol action name", exact: true }).fill("release");
  await actions.getByRole("button", { name: "Add action mapping", exact: true }).click();
  await actions.getByRole("combobox", { name: "Object action for release", exact: true }).selectOption("complete");
  await binding.getByRole("button", { name: "Add record read", exact: true }).click();
  const events = binding.getByRole("group", { name: "Protocol event mappings", exact: true });
  await events.getByRole("textbox", { name: "Protocol event name", exact: true }).fill("released");
  await events.getByRole("button", { name: "Add event mapping", exact: true }).click();
  await events.getByRole("combobox", { name: "Object action for released", exact: true }).selectOption("complete");
  await page.getByRole("button", { name: "Save", exact: true }).click();
  const expected = [{ name: protocol, version: 2, actions: { release: "complete" }, reads: ["records"], events: { released: "complete" } }];
  await expect.poll(async () => (await builder.record("build.object", object.id)).protocols).toEqual(expected);
  expect((await builder.read<{ id: string }[]>("protocols")).some(item => item.id === `${protocol}/2`)).toBe(false);
  expect(await builder.record(object.type, record!)).toEqual(original);

  await selectObjectSection(page, "Properties");
  await page.getByRole("row").filter({ hasText: "Order note" }).click();
  await page.getByRole("region", { name: "Object inspector", exact: true }).getByRole("textbox", { name: "What people call it", exact: true }).fill("Work note");
  await page.getByRole("button", { name: "Save", exact: true }).click();
  await expect.poll(async () => (await builder.record("build.object", object.id)).fields[0].title).toBe("Work note");
  expect((await builder.record("build.object", object.id)).protocols).toEqual(expected);
  await selectObjectSection(page, "Protocols");
  await expect(binding.getByRole("combobox", { name: "Object action for release", exact: true })).toHaveValue("complete");
  await shots(page, page.locator("body"), "object-protocol-bindings", info);

  await page.getByRole("button", { name: "Publish", exact: true }).click();
  await page.getByRole("button", { name: "Check draft and dependencies", exact: true }).click();
  await page.getByRole("button", { name: "Save immutable candidate", exact: true }).click();
  const activationReply = page.waitForResponse(response => new URL(response.url()).pathname === "/v1/releases/active" && response.request().method() === "POST");
  await page.getByRole("button", { name: "Activate release", exact: true }).click();
  const activation = await activationReply;
  expect(activation.ok(), await activation.text()).toBe(true);
  const accepted = await activation.json();
  const refreshed = await request.get(`/v1/releases/candidates/${encodeURIComponent(accepted.id)}`, { headers: builder.headers });
  expect(refreshed.ok(), await refreshed.text()).toBe(true);
  const review = await refreshed.json();
  expect(review.preview.candidateId).toBe(accepted.id);
  expect(review.active).toBe(true);
  expect(review.runningMatches).toBe(true);
  await expect(page.getByText("Running definitions match this saved release.", { exact: true })).toBeVisible();
  await expect(page.getByText("Release active for operators.", { exact: false })).toBeVisible();
  const directory = await builder.read<{ id: string; actions: string[]; reads: string[]; events: { name: string }[]; providers: string[] }[]>("protocols");
  expect(directory.find(item => item.id === `${protocol}/2`)).toMatchObject({ actions: ["release"], reads: ["records"], events: [{ name: "released" }], providers: ["build"] });
  const answer = await request.post(`/v1/protocols/${encodeURIComponent(protocol)}/2/release`, { headers: builder.headers, data: { target: record, idempotencyKey: fresh("PROTOCOL"), payload: {} } });
  expect(answer.ok(), await answer.text()).toBe(true);
  expect((await builder.record(object.type, record!)).state).toBe("done");

  await builder.open(page, `/object-type?id=${object.id}&tab=protocols`);
  await page.reload();
  await expect(binding.getByRole("combobox", { name: "Protocol", exact: true })).toContainText(`${protocol}/2`);
  await expect(binding.getByRole("combobox", { name: "Object action for release", exact: true })).toHaveValue("complete");
  await binding.getByRole("combobox", { name: "Protocol", exact: true }).selectOption(`${protocol}/2`);
  await expect(binding).toContainText("Uses the installed protocol contract.");
  await page.getByRole("button", { name: "Save", exact: true }).click();
  await expect.poll(async () => (await builder.record("build.object", object.id)).protocols[0].contract?.Name).toBe(protocol);
  const pinned = (await builder.record("build.object", object.id)).protocols;
  expect(pinned[0].contract.Version).toBe(2);
  expect(pinned[0].actions.release).toBe("complete");
  await selectObjectSection(page, "Properties");
  await page.getByRole("row").filter({ hasText: "Work note" }).click();
  await page.getByRole("region", { name: "Object inspector", exact: true }).getByRole("textbox", { name: "What people call it", exact: true }).fill("Dispatch note");
  await page.getByRole("button", { name: "Save", exact: true }).click();
  await expect.poll(async () => (await builder.record("build.object", object.id)).fields[0].title).toBe("Dispatch note");
  expect((await builder.record("build.object", object.id)).protocols).toEqual(pinned);
});
