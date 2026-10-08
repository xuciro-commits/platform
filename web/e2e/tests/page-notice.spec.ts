import { expect, runtime, sampleModule, shots, slug, test } from "./kit";

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
  await builder.open(page, `/compose?id=${id}`);
  await editor.importModule(m);

  // The inspector shows the imported notice; an over-long text blocks saving.
  await editor.select("Operator instructions");
  await expect(editor.inspector.getByRole("textbox", { name: "Notice title", exact: true })).toHaveValue("Before starting");
  await expect(editor.inspector.getByRole("combobox", { name: "Notice tone", exact: true })).toHaveValue("info");
  const text = editor.inspector.getByRole("textbox", { name: "Notice text", exact: true });
  await text.fill("中".repeat(1366));
  await expect(editor.save).toBeDisabled();
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
