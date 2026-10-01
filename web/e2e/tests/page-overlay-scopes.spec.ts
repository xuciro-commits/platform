import { expect, test } from "@playwright/test";
import { decide, fresh, open } from "./host";

test("declared overlay inputs isolate roots and discard old returns after close through a frozen release", async ({ page, request }, testInfo) => {
  test.setTimeout(60_000);
  const name = fresh("scopes").replace(/[^a-z0-9]/gi, "").toLowerCase(), id = fresh("PAGE"), handlerID = fresh("PAGE"), objectID = fresh("OBJ"), type = `build.${name}`;
  await decide(request, "manager", "build", "build.object.create", { type: "build.object", id: objectID }, { name, title: "Scope notes", fields: [{ name: "note", title: "Note", type: "text" }] });
  await decide(request, "manager", "build", "build.object.publish", { type: "build.object", id: objectID }, {});
  await decide(request, "manager", "build", "build.page.create", { type: "build.page", id: handlerID }, {
    name: `${name}handler`, title: "Scope handler", object: type, sections: [{ id: "return", widget: "button", configVersion: 1, title: "Return overlay value" }],
    document: { formatVersion: 2, uiProfile: "platform.page.v2.7", root: "root", nodes: { root: { kind: "rows", children: ["return"] }, return: { kind: "widget", section: "return" } }, variables: { result: { scope: "page", type: "string", mode: "constant", initial: "Returned value" } }, interface: { version: 1, outputs: { result: { variable: "result", type: "string", required: true } } }, events: [{ source: "return", event: "click", target: "", return: true }] },
  });
  await decide(request, "manager", "build", "build.page.publish", { type: "build.page", id: handlerID }, {});
  await decide(request, "manager", "build", "build.page.create", { type: "build.page", id }, {
    name, title: "Overlay scope desk", object: type,
    sections: [
      { id: "shared", widget: "input", configVersion: 1, title: "Page draft" },
      { id: "openA", widget: "button", configVersion: 1, title: "Open first" }, { id: "openB", widget: "button", configVersion: 1, title: "Open second" },
      { id: "inputA", widget: "input", configVersion: 1, title: "First draft" }, { id: "inputB", widget: "input", configVersion: 1, title: "Second draft" },
      { id: "switch", widget: "button", configVersion: 1, title: "Switch to second" },
      { id: "call", widget: "button", configVersion: 1, title: "Call from first" },
    ],
    document: {
      formatVersion: 2, uiProfile: "platform.page.v2.7", root: "root",
      nodes: { root: { kind: "rows", children: ["shared", "openA", "openB"] }, shared: { kind: "widget", section: "shared", valueVariable: "shared" }, openA: { kind: "widget", section: "openA" }, openB: { kind: "widget", section: "openB" }, first: { kind: "rows", children: ["inputA", "switch", "call"] }, second: { kind: "rows", children: ["inputB"] }, inputA: { kind: "widget", section: "inputA", valueVariable: "draftA" }, inputB: { kind: "widget", section: "inputB", valueVariable: "draftB" }, switch: { kind: "widget", section: "switch" }, call: { kind: "widget", section: "call" } },
      variables: { shared: { title: "Page draft", scope: "page", type: "string", mode: "state", initial: "" }, openA: { scope: "page", type: "boolean", mode: "state", initial: false }, openB: { scope: "page", type: "boolean", mode: "state", initial: false }, draftA: { title: "First local", scope: "overlay", owner: "A", type: "string", mode: "state", initial: "A reset" }, draftB: { title: "Second local", scope: "page", type: "string", mode: "state", initial: "B reset" } },
      overlays: { A: { root: "first", kind: "modal", title: "First panel", openVariable: "openA" }, B: { root: "second", kind: "drawer", title: "Second panel", openVariable: "openB" } },
      events: [{ source: "switch", event: "click", target: "openB", value: true }, { source: "openA", event: "click", target: "openA", value: true }, { source: "openB", event: "click", target: "openB", value: true }, { source: "call", event: "click", target: "", navigate: { page: { app: "build", kind: "page", name: `${name}handler` }, interfaceVersion: 1, results: { result: "draftA" } } }],
    },
  });
  await open(page, "manager", `/compose?id=${id}`);
  const tree = page.getByRole("region", { name: "Widgets and layout", exact: true }), inspector = page.getByRole("region", { name: "The widget in hand", exact: true });
  await tree.getByRole("button", { name: "Page variables", exact: true }).click();
  await inspector.getByRole("combobox", { name: "Choose page variable", exact: true }).selectOption("draftB");
  await inspector.getByRole("combobox", { name: "Variable scope", exact: true }).selectOption({ label: "Overlay: Second panel" });
  await tree.getByRole("button", { name: "First draft", exact: true }).click();
  const binding = inspector.getByRole("combobox", { name: "Input state variable", exact: true });
  await expect(binding.locator("option", { hasText: "Second local" })).toHaveCount(0);
  await binding.selectOption("shared"); await binding.selectOption("draftA");
  await page.getByRole("button", { name: "Save", exact: true }).click();
  await expect(page.getByRole("button", { name: "Save", exact: true })).toBeDisabled();
  const saved = (await (await request.get(`/v1/records/build.page/${id}`, { headers: { Authorization: "Bearer manager" } })).json()).record;
  expect(saved.document.variables.draftB).toMatchObject({ scope: "overlay", owner: "B" });
  await page.getByRole("button", { name: "Review release", exact: true }).click();
  await page.getByRole("button", { name: "Check draft and dependencies", exact: true }).click();
  await page.getByRole("button", { name: "Save immutable candidate", exact: true }).click();
  await page.getByRole("button", { name: "Activate release", exact: true }).click();
  const operation = await page.context().newPage(), route = `/page?app=build&kind=page&name=${name}`;
  await open(operation, "desk", route);
  await operation.getByRole("textbox", { name: "Page draft", exact: true }).fill("Keep page draft");
  await operation.getByRole("button", { name: "Open first", exact: true }).click();
  const first = operation.getByRole("dialog", { name: "First panel", exact: true });
  await first.getByRole("textbox", { name: "First draft", exact: true }).fill("Only first");
  if (process.env.PLATFORM_SCREENSHOTS) await operation.screenshot({ path: testInfo.outputPath("overlay-local-state.png"), fullPage: true });
  // A sibling opening uses the same declared event while the first is active.
  await first.getByRole("button", { name: "Switch to second", exact: true }).click();
  const second = operation.getByRole("dialog", { name: "Second panel", exact: true });
  await expect(second.getByRole("textbox", { name: "Second draft", exact: true })).toHaveValue("B reset");
  await second.getByRole("textbox", { name: "Second draft", exact: true }).fill("Only second");
  await operation.keyboard.press("Escape");
  await operation.getByRole("button", { name: "Open first", exact: true }).click();
  await expect(first.getByRole("textbox", { name: "First draft", exact: true })).toHaveValue("A reset");
  await first.getByRole("button", { name: "Call from first", exact: true }).click();
  await expect(operation.getByRole("button", { name: "Return overlay value", exact: true })).toBeVisible();
  await operation.getByRole("button", { name: "Return overlay value", exact: true }).click();
  await expect(first.getByRole("textbox", { name: "First draft", exact: true })).toHaveValue("Returned value");
  await first.getByRole("button", { name: "Call from first", exact: true }).click();
  await expect(operation.getByRole("button", { name: "Return overlay value", exact: true })).toBeVisible();
  const callRoute = new URL(operation.url()).hash.slice(1);
  await operation.evaluate((route) => { location.hash = route; }, route);
  await expect(first).toBeVisible(); await operation.keyboard.press("Escape");
  await operation.getByRole("button", { name: "Open first", exact: true }).click();
  await operation.evaluate((route) => { location.hash = route; }, callRoute);
  await operation.getByRole("button", { name: "Return overlay value", exact: true }).click();
  await expect(first.getByRole("textbox", { name: "First draft", exact: true })).toHaveValue("A reset");
  await operation.keyboard.press("Escape");
  await expect(operation.getByRole("textbox", { name: "Page draft", exact: true })).toHaveValue("Keep page draft");
  await operation.getByRole("button", { name: "Open second", exact: true }).click();
  await expect(second.getByRole("textbox", { name: "Second draft", exact: true })).toHaveValue("B reset");
  if (process.env.PLATFORM_SCREENSHOTS) { await operation.setViewportSize({ width: 390, height: 844 }); await operation.screenshot({ path: testInfo.outputPath("overlay-local-state-narrow.png"), fullPage: true }); }
  await operation.reload();
  await expect(second).toHaveCount(0); await expect(operation.getByRole("textbox", { name: "Page draft", exact: true })).toHaveValue("");
});
