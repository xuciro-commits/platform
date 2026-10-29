import { expect, pageUIProfile, runtime, shots, slug, test } from "./kit";

test("list and board editors retain independent card fields and the original move action", async ({ page, builder, editor }, info) => {
  const { type } = await builder.object({
    name: slug("cards"), title: "Card records",
    fields: [{ name: "name", title: "Name", type: "text" }, { name: "detail", title: "Detail", type: "text" }, { name: "quantity", title: "Quantity", type: "integer" }],
    states: [{ name: "open", title: "Open" }, { name: "done", title: "Done" }],
    actions: [{ name: "move", title: "Move original record", from: ["open", "done"], toInput: "status", roles: ["builder", "user"], inputs: [{ name: "status", title: "Destination state", type: "choice", choices: "open,done", required: true }] }],
  });
  const { id } = await builder.page({
    title: "Independent card fields", object: type,
    sections: [
      { id: "cards", widget: "record-list", configVersion: 1, title: "Record cards", cardLabel: "id", collectionVariable: "records", recordList: { layout: "grid" } },
      { id: "board", widget: "kanban", configVersion: 1, title: "Record board", cardLabel: "id", collectionVariable: "records" },
    ],
    document: {
      formatVersion: 2, uiProfile: pageUIProfile, root: "root",
      nodes: {
        root: { kind: "rows", children: ["cards", "board"] },
        cards: { kind: "widget", section: "cards" },
        board: { kind: "widget", section: "board" },
      },
      variables: { records: { scope: "page", type: "object-set", mode: "resource", source: { kind: "plan", query: "records" } } },
      queries: { records: { object: { app: "build", kind: "object", name: type }, limit: 50, sort: ["id"] } },
    },
  });
  await editor.open(builder, id);
  await editor.select("Record cards");
  const title = editor.inspector.getByRole("combobox", { name: "Card title field", exact: true });
  const summaries = editor.inspector.getByRole("group", { name: "Card summary fields", exact: true });
  await title.selectOption("name");
  await summaries.getByRole("button", { name: "Detail", exact: true }).click();
  await editor.select("Record board");
  await title.selectOption("detail");
  await summaries.getByRole("button", { name: "Quantity", exact: true }).click();
  const move = editor.inspector.getByRole("group", { name: "Allowed move actions", exact: true }).getByRole("button", { name: /^Move original record →/ });
  await move.click();
  await expect(move).toHaveAttribute("aria-pressed", "true");
  await editor.select("Record cards");
  await expect(title).toHaveValue("name");
  await expect(summaries.getByRole("button", { name: "Detail", exact: true })).toHaveAttribute("aria-pressed", "true");
  await shots(page, page.locator("body"), "list-board-card-fields", info);
  await editor.save();
  await expect.poll(async () => (await builder.sections(id)).find((section: any) => section.widget === "kanban")?.actions?.map((action: any) => action.name)).toEqual([`${type}.move`]);
  const saved = await builder.draft(id);
  expect(saved.sections.find((section: any) => section.widget === "record-list")).toMatchObject({ model: { type, fields: [{ name: "name", slot: "cardLabel" }, { name: "detail" }] }, recordList: { layout: "grid" } });
  expect(saved.sections.find((section: any) => section.widget === "kanban")).toMatchObject({ model: { type, fields: [{ name: "detail", slot: "cardLabel" }, { name: "quantity" }] }, actions: [{ name: `${type}.move` }] });
});

// Notice text is literal (no markup, no templating), its title is optional,
// and a released page keeps the frozen copy while the draft moves on.
// Overlay notices gate independently.
test("notices freeze literal text and optional titles; page and overlay gates stay independent", async ({ page, builder, operator, editor }, info) => {
  test.setTimeout(90_000);
  const { type } = await builder.object({ name: slug("notes"), title: "Instruction records", fields: [{ name: "name", title: "Name", type: "text" }] });
  const literal = "Review <img src=x> and {value} literally.\n**Markdown stays text.**";

  const { id, name } = await builder.page({
    title: "Operator instructions", object: type,
    sections: [
      { id: "main", widget: "notice", configVersion: 1, title: "Operator instructions", notice: { title: "Before starting", message: literal, tone: "info" } },
      { id: "success", widget: "notice", configVersion: 1, title: "Completion note", notice: { message: "Completed safely.", tone: "success" } },
      { id: "warning", widget: "notice", configVersion: 1, title: "Warning note", notice: { title: "", message: "Review the current record.", tone: "warning" } },
      { id: "danger", widget: "notice", configVersion: 1, title: "Danger note", notice: { title: "Stop", message: "Wait for authorization.", tone: "danger" } },
      { id: "gate", widget: "boolean-input", configVersion: 1, title: "Show instructions", booleanVariable: "show", booleanLabel: "Show instructions" },
      { id: "local", widget: "notice", configVersion: 1, title: "Local instructions", notice: { title: "Local note", message: "Use the original drawer workflow.", tone: "info" } },
      { id: "localGate", widget: "boolean-input", configVersion: 1, title: "Show local instructions", booleanVariable: "localShow", booleanLabel: "Show local instructions" },
      { id: "trigger", widget: "button-group", configVersion: 1, title: "Open instructions", buttons: [{ id: "open", title: "Open instructions", variant: "primary" }] },
      { id: "close", widget: "button-group", configVersion: 1, title: "Close instructions", buttons: [{ id: "close", title: "Close instructions" }] },
    ],
    document: {
      formatVersion: 2, uiProfile: pageUIProfile, root: "root",
      nodes: {
        root: { kind: "rows", children: ["gate", "mainWrapper", "success", "warning", "danger", "trigger"] },
        mainWrapper: { kind: "rows", title: "Conditional instructions", visibleWhen: "show", children: ["main"] },
        panelRoot: { kind: "rows", children: ["localGate", "localWrapper", "close"] },
        localWrapper: { kind: "rows", title: "Conditional local instructions", visibleWhen: "localShow", children: ["local"] },
        main: { kind: "widget", section: "main" },
        success: { kind: "widget", section: "success" },
        warning: { kind: "widget", section: "warning" },
        danger: { kind: "widget", section: "danger" },
        gate: { kind: "widget", section: "gate" },
        local: { kind: "widget", section: "local" },
        localGate: { kind: "widget", section: "localGate" },
        trigger: { kind: "widget", section: "trigger" },
        close: { kind: "widget", section: "close" },
      },
      variables: {
        show: { scope: "page", type: "boolean", mode: "state", initial: true },
        localShow: { scope: "overlay", owner: "panel", type: "boolean", mode: "state", initial: true },
        panelOpen: { scope: "page", type: "boolean", mode: "state", initial: false },
      },
      overlays: { panel: { kind: "drawer", title: "Instruction panel", root: "panelRoot", openVariable: "panelOpen" } },
      events: [
        { source: "trigger", control: "open", event: "click", effects: [{ kind: "set", target: "panelOpen", value: true }] },
        { source: "close", control: "close", event: "click", effects: [{ kind: "set", target: "panelOpen", value: false }] },
      ],
    },
  });
  await editor.open(builder, id);

  // The inspector shows the native notice; an over-long text blocks saving.
  await editor.select("Operator instructions");
  await expect(editor.inspector.getByRole("textbox", { name: "Notice title", exact: true })).toHaveValue("Before starting");
  await expect(editor.inspector.getByRole("combobox", { name: "Notice tone", exact: true })).toHaveValue("info");
  const text = editor.inspector.getByRole("textbox", { name: "Notice text", exact: true });
  // Unparsed inspector text belongs to the page, even when another widget is selected.
  await editor.inspector.getByRole("tab", { name: "Appearance", exact: true }).click();
  const width = editor.inspector.getByRole("textbox", { name: "Width (px)", exact: true });
  await width.fill("-");
  await editor.select("Completion note");
  await expect(editor.publish).toBeDisabled();
  await editor.save();
  await editor.select("Operator instructions");
  await expect(width).toHaveValue("-");
  await width.fill("");
  await editor.inspector.getByRole("tab", { name: "Properties", exact: true }).click();

  const oversized = "中".repeat(1366);
  await text.fill(oversized);
  await expect(editor.publish).toBeDisabled();
  await editor.save();
  const refusedDraft = await builder.draft(id);
  expect(refusedDraft.sections.some((s: any) => s.notice?.message === oversized)).toBe(false);
  await editor.select("Completion note");
  await page.getByRole("tablist", { name: "Page dock", exact: true }).getByRole("tab", { name: /^Problems\b/ }).click();
  await page.getByRole("button", { name: /Choose a supported notice tone and bounded plain title and text before saving\./ }).click();
  await expect(text).toHaveValue(oversized);
  await shots(page, page.locator("body"), "notice-problem-location", info);
  await text.fill(literal);
  const retained = `${literal}\nRetained after a refused save.`;
  await page.route("**/v1/submissions", async route => {
    const submission = route.request().postDataJSON();
    if (submission.schema?.name !== "build.page.edit" || submission.target?.id !== id) return route.continue();
    await route.fulfill({ status: 400, contentType: "application/json", body: JSON.stringify({ error: {
      code: "ERROR_CODE_INVALID_ARGUMENT", message: "Inspector save refused",
    } }) });
  });
  await text.fill(retained);
  await editor.save();
  await expect(page.getByRole("alert").filter({ hasText: "Inspector save refused" })).toBeVisible();
  await editor.select("Completion note");
  await editor.select("Operator instructions");
  await expect(text).toHaveValue(retained);
  await page.unroute("**/v1/submissions");
  await editor.save();
  await expect.poll(async () => (await builder.sections(id))
    .find((section: any) => section.title === "Operator instructions")?.notice?.message).toBe(retained);
  await text.fill(literal);
  await editor.select("Completion note");
  await expect(editor.inspector.getByRole("checkbox", { name: "Show notice title", exact: true })).not.toBeChecked();
  await editor.select("Warning note");
  await expect(editor.inspector.getByRole("checkbox", { name: "Show notice title", exact: true })).toBeChecked();
  await expect(editor.inspector.getByRole("textbox", { name: "Notice title", exact: true })).toHaveValue("");

  const saved = await editor.saveUntil(builder, id, "notice", 5);
  const main = saved.sections.find((s: any) => s.title === "Operator instructions");
  expect(main.notice).toEqual({ title: "Before starting", message: literal, tone: "info" });
  await page.reload();
  await editor.select("Operator instructions");
  await expect(editor.inspector.getByRole("textbox", { name: "Notice title", exact: true })).toHaveValue("Before starting");

  // Release; the draft then changes, which the release must not show.
  await editor.release(async () => {
    const draft = await builder.editable(id);
    draft.sections.find((s: any) => s.title === "Operator instructions").notice = { tone: "danger", title: "Later title", message: "Later instructions" };
    const wrapper = Object.values(draft.document.nodes).find((n: any) => n.title === "Conditional instructions") as any;
    draft.document.variables[wrapper.visibleWhen].initial = false;
    await builder.edit(id, draft);
  });

  const tab = await runtime(page, operator, name);
  const mainNote = tab.getByRole("note", { name: "Operator instructions", exact: true });
  const success = tab.getByRole("note", { name: "Completion note", exact: true });
  const warning = tab.getByRole("note", { name: "Warning note", exact: true });
  const danger = tab.getByRole("note", { name: "Danger note", exact: true });
  await expect(mainNote).toContainText("Before starting");
  await expect(mainNote).toContainText("Review <img src=x> and {value} literally.");
  await expect(mainNote).toContainText("**Markdown stays text.**");
  await expect(mainNote.locator("img")).toHaveCount(0);
  await expect(success.locator("strong")).toHaveCount(0);
  await expect(warning.locator("strong")).toHaveCount(0);
  await expect(danger).toContainText("Stop");
  await expect(tab.getByRole("alert", { name: "Warning note", exact: true })).toHaveCount(0);
  await shots(tab, mainNote, "notice", info);

  // The page gate and the overlay gate are independent and survive reopening.
  const gate = tab.getByRole("switch", { name: "Show instructions", exact: true });
  await gate.click();
  await expect(mainNote).toHaveCount(0);
  await expect(success).toBeVisible();
  const openPanel = () => tab.getByRole("button", { name: "Open instructions", exact: true }).click();
  const panel = tab.getByRole("dialog", { name: "Instruction panel", exact: true });
  const local = panel.getByRole("note", { name: "Local instructions", exact: true });
  const localGate = panel.getByRole("switch", { name: "Show local instructions", exact: true });
  await openPanel();
  await expect(local).toContainText("Local note");
  await localGate.click();
  await expect(local).toHaveCount(0);
  await panel.getByRole("button", { name: "Close instructions", exact: true }).click();
  await expect(mainNote).toHaveCount(0);
  await openPanel();
  await expect(localGate).toBeChecked();
  await expect(local).toBeVisible();
  await panel.getByRole("button", { name: "Close instructions", exact: true }).click();
  await gate.click();
  await expect(mainNote).toContainText("Before starting");
  await tab.reload();
  await expect(mainNote).toContainText("Before starting");
  await expect(mainNote).not.toContainText("Later instructions");
});
