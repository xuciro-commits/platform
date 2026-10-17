import { expect, runtime, sampleModule, shots, slug, test } from "./kit";

test("Workshop list and board imports retain independent card fields and the original move action", async ({ page, builder, editor }, info) => {
  const { type } = await builder.object({
    name: slug("importcards"), title: "Imported card records",
    fields: [{ name: "name", title: "Name", type: "text" }, { name: "detail", title: "Detail", type: "text" }, { name: "quantity", title: "Quantity", type: "integer" }],
    states: [{ name: "open", title: "Open" }, { name: "done", title: "Done" }],
    actions: [{ name: "move", title: "Move original record", from: ["open", "done"], toInput: "status", roles: ["builder", "user"], inputs: [{ name: "status", title: "Destination state", type: "choice", choices: "open,done", required: true }] }],
  });
  const m = sampleModule();
  m.variables = m.variables.filter((variable: any) => variable.id === "notes");
  m.widgets = {
    cards: { id: "cards", name: "Imported cards", type: "ObjectList", config: { objectSetVarId: "notes", layout: "grid" } },
    board: { id: "board", name: "Imported board", type: "KanbanBoard", config: { objectSetVarId: "notes", groupBy: "status", actionId: "updateAssetStatus", actionParam: "status" } },
  };
  m.sections = { root: { id: "root", name: "Imported collections", layout: "rows", children: [{ kind: "widget", id: "cards" }, { kind: "widget", id: "board" }] } };
  const { id } = await builder.page({ title: "Imported card mappings", object: type });
  await editor.open(builder, id);
  await editor.importModule(m, async dialog => {
    await dialog.getByRole("combobox", { name: "Map object Asset", exact: true }).selectOption(type);
    await dialog.getByRole("combobox", { name: "Map field Asset.status", exact: true }).selectOption("state");
    const cards = dialog.getByRole("group", { name: "Map record cards Imported cards", exact: true });
    await cards.getByRole("combobox", { name: "Card title field", exact: true }).selectOption("name");
    await cards.getByRole("group", { name: "Card summary fields", exact: true }).getByRole("button", { name: "Detail", exact: true }).click();
    const board = dialog.getByRole("group", { name: "Map board Imported board", exact: true });
    const move = board.getByRole("group", { name: "Map original board move actions", exact: true }).getByRole("button", { name: /^Move original record →/ });
    await move.click();
    await board.getByRole("combobox", { name: "Card title field", exact: true }).selectOption("detail");
    await board.getByRole("group", { name: "Card summary fields", exact: true }).getByRole("button", { name: "Quantity", exact: true }).click();
    await expect(move).toHaveAttribute("aria-pressed", "true");
    await expect(cards.getByRole("combobox", { name: "Card title field", exact: true })).toHaveValue("name");
    await expect(cards.getByRole("group", { name: "Card summary fields", exact: true }).getByRole("button", { name: "Detail", exact: true })).toHaveAttribute("aria-pressed", "true");
    await shots(page, dialog, "import-list-board-mappings", info);
  });
  const saved = await editor.saveUntil(builder, id, "kanban", 1);
  expect(saved.sections.find((section: any) => section.widget === "record-list")).toMatchObject({ cardLabel: "name", fields: ["detail"], recordList: { layout: "grid" } });
  expect(saved.sections.find((section: any) => section.widget === "kanban")).toMatchObject({ cardLabel: "detail", fields: ["quantity"], actions: [`${type}.move`] });
});

// A Callout imported from Workshop becomes a notice: its text is literal (no
// markup, no templating), its title is optional, and a released page keeps
// the frozen copy while the draft moves on. Overlay notices gate independently.
test("notices freeze literal text and optional titles; page and overlay gates stay independent", async ({ page, builder, operator, editor }, info) => {
  test.setTimeout(90_000);
  const { type } = await builder.object({ name: slug("notes"), title: "Instruction records", fields: [{ name: "name", title: "Name", type: "text" }] });
  const literal = "Review <img src=x> and {value} literally.\n**Markdown stays text.**";

  const m = sampleModule();
  const note = (id: string, name: string, title: string | undefined, text: string, intent: string) => ({ id, name, type: "Callout", config: { ...(title === undefined ? {} : { title }), text, intent } });
  const toggle = (id: string, name: string, variableId: string) => ({ id, name, type: "ToggleSwitch", config: { variableId, label: name } });
  const button = (id: string, label: string, kind: string) => ({ id, name: label, type: "SingleButton", config: { label, eventActions: [{ kind, overlayId: "panel" }] } });
  m.variables = [
    { id: "show", name: "Show instructions", type: "boolean", definitionKind: "static", staticValue: true },
    { id: "localShow", name: "Show local instructions", type: "boolean", definitionKind: "static", staticValue: true },
  ];
  m.widgets = {
    main: note("main", "Operator instructions", "Before starting", literal, "primary"),
    success: note("success", "Completion note", undefined, "Completed safely.", "success"),
    warning: note("warning", "Warning note", "", "Review the current record.", "warning"),
    danger: note("danger", "Danger note", "Stop", "Wait for authorization.", "danger"),
    gate: toggle("gate", "Show instructions", "show"),
    local: note("local", "Local instructions", "Local note", "Use the original drawer workflow.", "primary"),
    localGate: toggle("localGate", "Show local instructions", "localShow"),
    trigger: button("trigger", "Open instructions", "openOverlay"),
    close: button("close", "Close instructions", "closeOverlay"),
  };
  const widgets = (ids: string[]) => ids.map((id) => ({ kind: "widget", id }));
  m.sections = {
    root: { id: "root", name: "Root", layout: "rows", children: [...widgets(["gate"]), { kind: "section", id: "mainWrapper" }, ...widgets(["success", "warning", "danger", "trigger"])] },
    mainWrapper: { id: "mainWrapper", name: "Conditional instructions", layout: "rows", visibleVariableId: "show", children: widgets(["main"]) },
    panel: { id: "panel", name: "Panel", layout: "rows", children: [...widgets(["localGate"]), { kind: "section", id: "localWrapper" }, ...widgets(["close"])] },
    localWrapper: { id: "localWrapper", name: "Conditional local instructions", layout: "rows", visibleVariableId: "localShow", children: widgets(["local"]) },
  };
  m.overlays = [{ id: "panel", name: "Instruction panel", kind: "drawer", rootSectionId: "panel" }];

  const { id, name } = await builder.page({ title: "Imported instructions", object: type });
  await editor.open(builder, id);
  await editor.importModule(m);

  // The inspector shows the imported notice; an over-long text blocks saving.
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
  const refusedDraft = await builder.record("build.page", id);
  expect(refusedDraft.sections.some((s: any) => s.notice?.message === oversized)).toBe(false);
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
  await expect.poll(async () => (await builder.record("build.page", id)).sections
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
    main.notice = { tone: "danger", title: "Later title", message: "Later instructions" };
    const wrapper = Object.values(saved.document.nodes).find((n: any) => n.title === "Conditional instructions") as any;
    saved.document.variables[wrapper.visibleWhen].initial = false;
    await builder.edit(id, { sections: saved.sections, document: saved.document });
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
