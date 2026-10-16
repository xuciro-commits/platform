import { expect, pageUIProfile, runtime, shots, slug, test } from "./kit";

test("studio preview keeps its application context and shared values out of live sessions", async ({ page, builder }, info) => {
  test.setTimeout(120_000);
  const object = await builder.object({ title: "Context record", fields: [{ name: "name", title: "Name", type: "text" }] });
  const firstName = slug("contextfirst"), secondName = slug("contextsecond");
  const shared = { scope: "application", type: "string", mode: "shared", writable: true, source: { kind: "application", variable: "message" } };
  const first = await builder.page({
    name: firstName, title: "First context page", object: object.type,
    sections: [
      { id: "message", widget: "input", configVersion: 1, title: "Shared message" },
      { id: "create", widget: "form", configVersion: 1, title: "Create context record", fields: ["name"] },
      { id: "next", widget: "button", configVersion: 1, title: "Open second page" },
    ],
    document: {
      formatVersion: 2, uiProfile: pageUIProfile, root: "root", variables: { message: shared },
      nodes: { root: { kind: "rows", children: ["message", "create", "next"] }, message: { kind: "widget", section: "message", valueVariable: "message" }, create: { kind: "widget", section: "create" }, next: { kind: "widget", section: "next" } },
      interface: { version: 1 },
      events: [{ id: "next", source: "next", event: "click", effects: [{ kind: "navigate", navigate: { page: { app: "build", kind: "page", name: secondName }, interfaceVersion: 1 } }] }],
    },
  });
  const second = await builder.page({
    name: secondName, title: "Second context page", object: object.type,
    sections: [
      { id: "message", widget: "input", configVersion: 1, title: "Shared message" },
      { id: "back", widget: "button", configVersion: 1, title: "Return to first page" },
    ],
    document: {
      formatVersion: 2, uiProfile: pageUIProfile, root: "root", variables: { message: shared },
      nodes: { root: { kind: "rows", children: ["message", "back"] }, message: { kind: "widget", section: "message", valueVariable: "message" }, back: { kind: "widget", section: "back" } },
      interface: { version: 1 },
      events: [{ id: "back", source: "back", event: "click", effects: [{ kind: "return" }] }],
    },
  });
  const application = await builder.application({ title: "Shared context application", pages: [first.name, second.name] });
  await builder.decide("build.app.edit", { type: "build.app", id: application.id }, {
    uiProfile: pageUIProfile,
    variables: { message: { scope: "application", type: "string", mode: "state", initial: "Original value" } },
    header: { title: "Shared context application", variant: "horizontal", items: [{ kind: "title" }, { kind: "tabs", pages: [first.name, second.name] }] },
  });
  await builder.activate(await builder.candidate([{ kind: "page", id: first.id }, { kind: "page", id: second.id }, { kind: "app", id: application.id }]));
  const originalDefinitions = await builder.definitions();
  const originalApplication = await builder.record("build.app", application.id);
  const liveRoute = `/page?app=build&kind=page&name=${first.name}&application=build:${application.name}&instance=main`;
  // Keep the live page attached while previewing. Switching its whole app
  // would intentionally retire the last page's transient session state.
  const live = await runtime(page, builder, first.name);
  await builder.open(live, liveRoute);
  const formalMessage = live.getByRole("textbox", { name: "Shared message", exact: true });
  await expect(formalMessage).toHaveValue("Original value");
  await formalMessage.fill("Formal value");
  const message = page.getByRole("textbox", { name: "Shared message", exact: true });

  const studioRoute = `/module?id=${application.id}&application=${application.id}&page=${first.id}`;
  await builder.open(page, studioRoute);
  await page.getByRole("button", { name: "Preview", exact: true }).click();
  await expect(page.getByRole("combobox", { name: "Page application", exact: true })).toHaveValue(`build:${application.name}`);
  await expect(page.getByRole("region", { name: "Editor", exact: true }).getByRole("button", { name: "Create", exact: true })).toBeDisabled();
  await expect(message).toHaveValue("Original value");
  await message.fill("Preview value");
  await page.getByRole("region", { name: "Editor", exact: true }).getByRole("button", { name: "Open second page", exact: true }).click();
  await expect(page).toHaveURL(/#\/page-preview\?/);
  const child = new URLSearchParams(new URL(page.url()).hash.split("?")[1]);
  expect(child.get("application")).toBe(application.id);
  expect(child.get("previewApplication")).toBe(`build:${application.name}`);
  expect(child.get("previewInstance")).toMatch(/^preview:/);
  await expect(message).toHaveValue("Preview value");
  await message.fill("Returned preview value");
  await page.getByRole("button", { name: "Return to first page", exact: true }).click();
  await expect(message).toHaveValue("Returned preview value");
  await expect(page).toHaveURL(new RegExp(`application=${application.id}`));

  await page.getByRole("navigation", { name: "Application pages", exact: true }).getByRole("button", { name: "Second context page", exact: true }).click();
  await expect(page).toHaveURL(/#\/page-preview\?/);
  await expect(message).toHaveValue("Returned preview value");
  await shots(page, page.getByRole("navigation", { name: "Application pages", exact: true }), "preview-application-pages", info);
  await page.getByRole("button", { name: "New application instance", exact: true }).click();
  await expect(message).toHaveValue("Original value");
  const independent = new URLSearchParams(new URL(page.url()).hash.split("?")[1]);
  expect(independent.get("application")).toBe(application.id);
  expect(independent.get("previewInstance")).not.toBe(child.get("previewInstance"));
  await message.fill("Independent preview");
  await page.getByRole("button", { name: "Close application instance", exact: true }).click();
  await expect(formalMessage).toHaveValue("Formal value");
  expect(await builder.record("build.app", application.id)).toEqual(originalApplication);
  expect(await builder.definitions()).toEqual(originalDefinitions);
  expect((await builder.read<{ records: unknown[] }>(`records/${object.type}`)).records).toEqual([]);
  await live.close();
});
