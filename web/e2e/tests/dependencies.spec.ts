import { expect, pageUIProfile, shots, slug, test } from "./kit";

test("one dependency graph and capability screen follow the original member authorization", async ({ page, builder, operator }, info) => {
  test.setTimeout(120_000);
  const name = slug("dependencies"), type = `build.${name}`, action = `${type}.review`;
  const object = await builder.object({
    name, title: "Dependency source", fields: [{ name: "title", title: "Title", type: "text" }],
    states: [{ name: "open", title: "Open" }, { name: "ready", title: "Ready" }],
    actions: [{ name: "review", title: "Builder review", from: ["open"], to: "ready", roles: ["builder"], inputs: [], sets: [], conditions: [] }],
    access: [{ role: "user", read: "all", create: true, edit: true }],
  });
  const pageDraft = await builder.page({
    title: "Dependency consumer", object: object.type,
    sections: [
      { id: "records", widget: "table", configVersion: 1, title: "Original records", fields: ["title"] },
      { id: "review", widget: "actions", configVersion: 1, title: "Original action", actions: [action] },
    ],
    document: { formatVersion: 2, uiProfile: pageUIProfile, root: "root", nodes: {
      root: { kind: "rows", children: ["records", "review"] }, records: { kind: "widget", section: "records" }, review: { kind: "widget", section: "review" },
    } },
  });
  await builder.decide("build.page.publish", { type: "build.page", id: pageDraft.id }, {});
  const objectRef = { app: "build", kind: "object", name: object.type };
  const pageRef = { app: "build", kind: "page", name: pageDraft.name };
  const graph = await builder.read<any>(`dependencies?app=build&kind=object&name=${object.type}`);
  expect(graph.usedBy).toContainEqual(pageRef);
  const consumer = graph.descriptors.find((descriptor: any) => descriptor.ref.kind === "page" && descriptor.ref.name === pageDraft.name);
  expect(consumer.requires).toEqual(expect.arrayContaining([objectRef, { app: "build", kind: "action", name: action }]));
  const reverse = await builder.read<any>(`dependencies?app=build&kind=page&name=${pageDraft.name}`);
  expect(reverse.requires).toContainEqual(objectRef);

  await builder.open(page, `/lineage?ref=${encodeURIComponent(`build/object/${type}`)}`);
  await expect(page.getByRole("heading", { name: "Declared dependencies", exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: pageDraft.name, exact: true })).toBeVisible();
  await expect(page.getByRole("heading", { name: "Runtime evidence", exact: true })).toBeVisible();
  const diagram = page.getByRole("group", { name: "Dependency diagram", exact: true });
  const prepared = () => expect(diagram).toHaveAttribute("aria-busy", "false");
  await prepared();
  await shots(page, page.locator("body"), "declared-dependencies", info, prepared);

  const builderCapabilities = await builder.read<any[]>("provides?kind=action&side=authoritative");
  const operatorCapabilities = await operator.read<any[]>("provides?kind=action&side=authoritative");
  expect(builderCapabilities.some(item => item.capability.name === action)).toBe(true);
  expect(operatorCapabilities.some(item => item.capability.name === action)).toBe(false);
  expect(operatorCapabilities.some(item => item.capability.name === `${type}.edit`)).toBe(true);

  await builder.open(page, "/lineage");
  await page.getByRole("combobox", { name: "App", exact: true }).selectOption("build");
  await page.getByRole("combobox", { name: "Capability kind", exact: true }).selectOption("action");
  await page.getByRole("combobox", { name: "Invocation type", exact: true }).selectOption("authoritative");
  await page.getByRole("textbox", { name: "Filter rows", exact: true }).fill(type);
  await expect(page.getByRole("row").filter({ hasText: action })).toBeVisible();
  await shots(page, page.locator("body"), "builder-capability-discovery", info, prepared);

  await operator.open(page, "/lineage");
  await page.getByRole("combobox", { name: "App", exact: true }).selectOption("build");
  await page.getByRole("combobox", { name: "Capability kind", exact: true }).selectOption("action");
  await page.getByRole("combobox", { name: "Invocation type", exact: true }).selectOption("authoritative");
  await page.getByRole("textbox", { name: "Filter rows", exact: true }).fill(type);
  await expect(page.getByRole("row").filter({ hasText: `${type}.edit` })).toBeVisible();
  await expect(page.getByRole("row").filter({ hasText: action })).toHaveCount(0);
  await shots(page, page.locator("body"), "operator-capability-discovery", info, prepared);
});
