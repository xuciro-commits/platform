import { expect, test } from "@playwright/test";
import { decide, fresh, open } from "./host";

test("two selections of one object keep their details and actions independent after release", async ({ page, request }, testInfo) => {
  const suffix = fresh("probe").replace(/[^a-z0-9]/gi, "").toLowerCase();
  const name = `selection${suffix}`, type = `build.${name}`, pageName = `compare${suffix}`;
  const objectID = fresh("OBJ"), pageID = fresh("PAGE"), a = fresh("REC"), b = fresh("REC");
  const childName = `line${suffix}`, child = `build.${childName}`, childID = fresh("OBJ");
  const taskName = `task${suffix}`, taskType = `build.${taskName}`, taskID = fresh("OBJ");
  const builder = { Authorization: "Bearer manager" }, operator = { Authorization: "Bearer desk" };
  await decide(request, "manager", "build", "build.object.create", { type: "build.object", id: objectID }, {
    name, title: "Selection probe", plural: "Selection probes",
    fields: [{ name: "number", title: "Number", type: "text", required: true, search: true }, { name: "packsize", title: "Pack size", type: "integer", required: true }],
    states: [{ name: "open", title: "Open" }, { name: "done", title: "Done" }],
    actions: [{ name: "finish", title: "Complete item", from: ["open"], to: "done" }],
  });
  await decide(request, "manager", "build", "build.object.publish", { type: "build.object", id: objectID }, {});
  await decide(request, "manager", "build", "build.object.create", { type: "build.object", id: childID }, {
    name: childName, title: "Related item", plural: "Related items",
    fields: [{ name: "parent", title: "Parent", type: "reference", ref: type, inverse: "lines", required: true },
      { name: "number", title: "Number", type: "text", required: true, search: true }],
  });
  await decide(request, "manager", "build", "build.object.publish", { type: "build.object", id: childID }, {});
  await decide(request, "manager", "build", "build.object.create", { type: "build.object", id: taskID }, {
    name: taskName, title: "Task", fields: [{ name: "line", title: "Line", type: "reference", ref: child, inverse: "tasks", required: true },
      { name: "number", title: "Number", type: "text", required: true, search: true }, { name: "size", title: "Size", type: "integer", required: true }],
  });
  await decide(request, "manager", "build", "build.object.publish", { type: "build.object", id: taskID }, {});
  for (const [id, number] of [[a, "ITEM-A"], [b, "ITEM-B"]]) {
    await decide(request, "desk", "build", `${type}.create`, { type, id }, { number, packsize: id === a ? 5 : 10 });
    await decide(request, "desk", "build", `${child}.create`, { type: child, id: fresh("LINE") }, { parent: id, number: `LINE-${number}` });
  }
  await decide(request, "manager", "build", "build.page.create", { type: "build.page", id: pageID }, {
    name: pageName, title: "Compare two records", object: type,
    sections: [
      { widget: "table", title: "Left list", width: "half", fields: ["number", "state"] },
      { widget: "table", title: "Right list", width: "half", fields: ["number", "state"] },
      { widget: "detail", title: "Left detail", width: "half", fields: ["number", "state"] },
      { widget: "detail", title: "Right detail", width: "half", fields: ["number", "state"] },
      { widget: "actions", title: "Left actions", width: "half", actions: [`${type}.finish`] },
      { widget: "actions", title: "Right actions", width: "half", actions: [`${type}.finish`] },
      { widget: "table", title: "Related list", object: child, relation: "lines", fields: ["number"] },
      { widget: "detail", title: "Related detail", object: child, fields: ["number"] },
      { widget: "table", title: "Task list", object: taskType, relation: "tasks", fields: ["number", "size"] },
      { widget: "detail", title: "Task detail", object: taskType, fields: ["number", "size"] },
      { widget: "form", title: "Create task", object: taskType, relation: "tasks", fields: ["number", "size"] },
    ],
  });
  await open(page, "manager", `/compose?id=${pageID}`);
  const layout = page.getByRole("region", { name: "Widgets and layout", exact: true });
  const inspector = page.getByRole("region", { name: "The widget in hand", exact: true });
  // Lift a V1 page into stable V2 instances; title edits, copy and grouping
  // remain reversible without changing the original business bindings.
  await layout.getByRole("button", { name: "Left list", exact: true }).click();
  await inspector.getByLabel("Title", { exact: true }).fill("Temporary title");
  await page.getByRole("button", { name: "Undo", exact: true }).click();
  await expect(inspector.getByLabel("Title", { exact: true })).toHaveValue("Left list");
  await page.getByRole("button", { name: "Redo", exact: true }).click();
  await expect(inspector.getByLabel("Title", { exact: true })).toHaveValue("Temporary title");
  await page.getByRole("button", { name: "Undo", exact: true }).click();
  await page.getByRole("button", { name: "Duplicate widget", exact: true }).click();
  await expect(layout.getByRole("button", { name: "Left list", exact: true })).toHaveCount(2);
  await page.getByRole("button", { name: "Undo", exact: true }).click();
  await expect(layout.getByRole("button", { name: "Left list", exact: true })).toHaveCount(1);
  await page.getByRole("button", { name: "Redo", exact: true }).click();
  await expect(layout.getByRole("button", { name: "Left list", exact: true })).toHaveCount(2);
  await page.getByRole("button", { name: "Undo", exact: true }).click();
  await layout.getByRole("button", { name: "Left detail", exact: true }).scrollIntoViewIfNeeded();
  await layout.getByRole("button", { name: "Right detail", exact: true }).scrollIntoViewIfNeeded();
  await layout.getByRole("button", { name: "Left detail", exact: true }).dragTo(layout.getByRole("button", { name: "Right detail", exact: true }));
  await layout.getByRole("button", { name: "Text", exact: true }).dragTo(page.getByRole("region", { name: "The page", exact: true }).getByRole("heading", { name: "Left list", exact: true }));
  await expect(layout.getByRole("button", { name: "Text", exact: true })).toHaveCount(2);
  await page.getByRole("button", { name: "Undo", exact: true }).click();
  await expect(layout.getByRole("button", { name: "Text", exact: true })).toHaveCount(1);
  await layout.getByRole("button", { name: "Left list", exact: true }).click();
  await layout.getByRole("button", { name: "Group with next in rows", exact: true }).click();
  await expect(inspector.getByRole("combobox", { name: "Layout", exact: true })).toHaveValue("rows");
  await inspector.getByRole("combobox", { name: "Layout", exact: true }).selectOption("columns");
  await page.getByRole("button", { name: "Undo", exact: true }).click();
  await expect(inspector.getByRole("combobox", { name: "Layout", exact: true })).toHaveValue("rows");
  await page.getByRole("button", { name: "Redo", exact: true }).click();
  await expect(inspector.getByRole("combobox", { name: "Layout", exact: true })).toHaveValue("columns");
  await layout.getByRole("button", { name: "Page settings", exact: true }).click();
  for (const [i, name] of ["left", "right"].entries()) {
    await inspector.getByRole("button", { name: "Add record selection", exact: true }).click();
    await inspector.getByLabel("Selection name", { exact: true }).nth(i).fill(name);
    await expect(inspector.getByRole("combobox", { name: "Selection object", exact: true }).nth(i)).toHaveValue(type);
  }
  await inspector.getByRole("button", { name: "Add record selection", exact: true }).click();
  await inspector.getByLabel("Selection name", { exact: true }).nth(2).fill("line");
  await inspector.getByRole("combobox", { name: "Selection object", exact: true }).nth(2).selectOption(child);
  await inspector.getByRole("button", { name: "Add record selection", exact: true }).click();
  await inspector.getByLabel("Selection name", { exact: true }).nth(3).fill("task");
  await inspector.getByRole("combobox", { name: "Selection object", exact: true }).nth(3).selectOption(taskType);
  for (const [side, selection] of [["Left", "left"], ["Right", "right"]]) {
    for (const part of ["list", "detail", "actions"]) {
      await layout.getByRole("button", { name: new RegExp(`^${side} ${part}`) }).click();
      await inspector.getByRole("combobox", { name: part === "list" ? "Writes selection" : "Reads selection", exact: true }).selectOption(selection);
    }
  }
  await layout.getByRole("button", { name: /^Related list/ }).click();
  await inspector.getByRole("combobox", { name: "Writes selection", exact: true }).selectOption("line");
  await inspector.getByRole("combobox", { name: "Parent selection", exact: true }).selectOption("right");
  await layout.getByRole("button", { name: /^Related detail/ }).click();
  await inspector.getByRole("combobox", { name: "Reads selection", exact: true }).selectOption("line");
  await layout.getByRole("button", { name: /^Task list/ }).click();
  await inspector.getByRole("combobox", { name: "Writes selection", exact: true }).selectOption("task");
  await inspector.getByRole("combobox", { name: "Parent selection", exact: true }).selectOption("line");
  await inspector.getByRole("combobox", { name: "Through", exact: true }).selectOption("tasks");
  await layout.getByRole("button", { name: /^Task detail/ }).click();
  await inspector.getByRole("combobox", { name: "Reads selection", exact: true }).selectOption("task");
  await layout.getByRole("button", { name: /^Create task/ }).click();
  await inspector.getByRole("combobox", { name: "Parent selection", exact: true }).selectOption("line");
  await inspector.getByRole("combobox", { name: "Through", exact: true }).selectOption("tasks");
  await inspector.getByRole("combobox", { name: "Input source for Size", exact: true }).selectOption("path:parent.packsize");
  await page.getByRole("button", { name: "Save", exact: true }).click();
  await expect(page.getByRole("button", { name: "Save", exact: true })).toBeDisabled();
  await expect(page.getByRole("button", { name: "Undo", exact: true })).toBeEnabled();
  await page.getByRole("button", { name: "Undo", exact: true }).click();
  await page.getByRole("button", { name: "Redo", exact: true }).click();
  await expect(page.getByRole("button", { name: "Save", exact: true })).toBeDisabled();
  await expect(page.getByRole("region", { name: "The page", exact: true }).getByRole("row").filter({ hasText: "ITEM-A" }).first()).toBeVisible();
  if (process.env.PLATFORM_SCREENSHOTS) await page.screenshot({ path: testInfo.outputPath("studio-v2-editor.png"), fullPage: true });
  await page.getByRole("button", { name: "Review release", exact: true }).click();
  const savedDraft = (await (await request.get(`/v1/records/build.page/${pageID}`, { headers: builder })).json()).record;
  expect(savedDraft.document.formatVersion).toBe(2);
  expect(savedDraft.document.uiProfile).toBe("platform.page.v2.5");
  expect(new Set(savedDraft.sections.map((s: { id: string }) => s.id)).size).toBe(11);
  expect(savedDraft.sections.every((s: { configVersion: number }) => s.configVersion === 1)).toBe(true);
  expect(savedDraft.document.nodes.v1columns2.children).toEqual(["v1node3", "v1node2"]);
  expect(Object.values(savedDraft.document.nodes).filter((n) => (n as { kind: string }).kind === "columns").length).toBeGreaterThan(3);
  await page.getByRole("button", { name: "Check draft and dependencies", exact: true }).click();
  await page.getByRole("button", { name: "Save immutable candidate", exact: true }).click();
  await expect(page.getByRole("button", { name: "Activate release", exact: true })).toBeEnabled();
  await page.getByRole("button", { name: "Activate release", exact: true }).click();
  await expect(page.getByRole("status").filter({ hasText: "Release active for operators." })).toBeVisible();
  const definitions = await (await request.get("/v1/definitions", { headers: builder })).json();
  const installed = definitions.find((d: { ref: { kind: string; name: string } }) => d.ref.kind === "page" && d.ref.name === pageName).page;
  expect(installed.document).toEqual(savedDraft.document);
  expect(installed.selections).toEqual([
    ...["left", "right"].map((name) => ({ name, object: { app: "build", kind: "object", name: type } })),
    { name: "line", object: { app: "build", kind: "object", name: child } },
    { name: "task", object: { app: "build", kind: "object", name: taskType } },
  ]);
  expect(installed.sections.map((section: { selection: string }) => section.selection)).toEqual(["left", "right", "left", "right", "left", "right", "line", "line", "task", "task", undefined]);
  expect(installed.sections[6].parentSelection).toBe("right");

  const operation = await page.context().newPage();
  await open(operation, "desk", `/page?app=build&kind=page&name=${pageName}`);
  const section = (name: string) => operation.getByRole("heading", { name, exact: true }).locator("..");
  const left = section("Left list"), right = section("Right list");
  await left.getByRole("row").filter({ hasText: "ITEM-A" }).click();
  await expect(section("Left detail").getByRole("heading", { name: "ITEM-A", exact: true })).toBeVisible();
  await expect(section("Right actions").getByRole("button", { name: "Complete item", exact: true })).toHaveCount(0);
  await right.getByRole("row").filter({ hasText: "ITEM-B" }).click();
  await expect(section("Right detail").getByRole("heading", { name: "ITEM-B", exact: true })).toBeVisible();
  await expect(section("Left detail").getByRole("heading", { name: "ITEM-A", exact: true })).toBeVisible();
  const related = section("Related list");
  await expect(related.getByRole("row").filter({ hasText: "LINE-ITEM-B" })).toBeVisible();
  await expect(related.getByRole("row").filter({ hasText: "LINE-ITEM-A" })).toHaveCount(0);
  await related.getByRole("row").filter({ hasText: "LINE-ITEM-B" }).click();
  await expect(section("Related detail").getByRole("heading", { name: "LINE-ITEM-B", exact: true })).toBeVisible();
  const taskForm = section("Create task");
  await expect(taskForm.getByText("10", { exact: true })).toBeVisible();
  await expect(taskForm.getByLabel("Size", { exact: true })).toHaveCount(0);
  await taskForm.getByLabel("Number *", { exact: true }).fill("TASK-B");
  await taskForm.getByRole("button", { name: "Create", exact: true }).click();
  await section("Task list").getByRole("row").filter({ hasText: "TASK-B" }).click();
  await expect(section("Task detail").getByRole("heading", { name: "TASK-B", exact: true })).toBeVisible();
  await section("Right actions").getByRole("button", { name: "Complete item", exact: true }).click();
  await expect.poll(async () => (await (await request.get(`/v1/records/${type}/${b}`, { headers: operator })).json()).record.state).toBe("done");
  const original = await (await request.get(`/v1/records/${type}/${a}`, { headers: operator })).json();
  expect(original.record.state).toBe("open");
  await expect(section("Left actions").getByRole("button", { name: "Complete item", exact: true })).toBeVisible();
  await expect(section("Right actions").getByRole("button", { name: "Complete item", exact: true })).toHaveCount(0);
  await left.getByRole("row").filter({ hasText: "ITEM-A" }).click();
  await expect(section("Related detail").getByRole("heading", { name: "LINE-ITEM-B", exact: true })).toBeVisible();
  await left.getByRole("row").filter({ hasText: "ITEM-A" }).click();
  await expect(section("Left detail").getByRole("heading", { name: "ITEM-A", exact: true })).toBeVisible();
  for (const list of [left, right]) await expect(list.getByRole("row").filter({ hasText: "ITEM-B" }).getByText("Done", { exact: true })).toBeVisible();
  if (process.env.PLATFORM_SCREENSHOTS) await operation.screenshot({ path: testInfo.outputPath("two-record-selections.png"), fullPage: true });
  await right.getByRole("row").filter({ hasText: "ITEM-B" }).click();
  await expect(section("Right detail").getByText("Select a record to see it here.", { exact: true })).toBeVisible();
  await expect(section("Related detail").getByText("Select a record to see it here.", { exact: true })).toBeVisible();
  await expect(section("Left detail").getByRole("heading", { name: "ITEM-A", exact: true })).toBeVisible();
  await expect(section("Task detail").getByText("Select a record to see it here.", { exact: true })).toBeVisible();
  await expect(taskForm.getByLabel("Number *", { exact: true })).toHaveCount(0);
  // A fresh operator session still reads the activated layout and persisted
  // action result; transient record selections correctly start empty.
  await operation.reload();
  await expect(section("Left detail").getByText("Select a record to see it here.", { exact: true })).toBeVisible();
  await section("Left list").getByRole("row").filter({ hasText: "ITEM-A" }).click();
  await section("Right list").getByRole("row").filter({ hasText: "ITEM-B" }).click();
  await expect(section("Left detail").getByRole("heading", { name: "ITEM-A", exact: true })).toBeVisible();
  await expect(section("Right detail").getByRole("heading", { name: "ITEM-B", exact: true })).toBeVisible();
  await expect(section("Right actions").getByRole("button", { name: "Complete item", exact: true })).toHaveCount(0);
});

test("page save conflicts retain the local draft until the builder discards it", async ({ page, request }) => {
  const suffix = fresh("conflict").replace(/[^a-z0-9]/gi, "").toLowerCase();
  const objectID = fresh("OBJ"), pageID = fresh("PAGE"), name = `notes${suffix}`;
  await decide(request, "manager", "build", "build.object.create", { type: "build.object", id: objectID }, {
    name, title: "Note", fields: [{ name: "note", title: "Note", type: "text" }],
  });
  await decide(request, "manager", "build", "build.object.publish", { type: "build.object", id: objectID }, {});
  await decide(request, "manager", "build", "build.page.create", { type: "build.page", id: pageID }, {
    name, title: "Saved page", object: `build.${name}`, sections: [{ widget: "text", title: "Notes", text: "Saved words" }],
  });
  await open(page, "manager", `/compose?id=${pageID}`);
  const layout = page.getByRole("region", { name: "Widgets and layout", exact: true });
  const inspector = page.getByRole("region", { name: "The widget in hand", exact: true });
  await layout.getByRole("button", { name: "Page settings", exact: true }).click();
  await inspector.getByLabel("What people call it", { exact: true }).fill("Local unsaved title");
  await decide(request, "manager", "build", "build.page.edit", { type: "build.page", id: pageID }, { title: "Remote title" });
  await page.getByRole("button", { name: "Save", exact: true }).click();
  await expect(page.getByRole("alert").filter({ hasText: /CONFLICT|changed|revision/i })).toBeVisible();
  await expect(inspector.getByLabel("What people call it", { exact: true })).toHaveValue("Local unsaved title");
  // Refetching the newer record must not silently advance the draft's base
  // revision and turn the next click into an overwrite.
  await page.getByRole("button", { name: "Save", exact: true }).click();
  await expect(page.getByRole("alert").filter({ hasText: /CONFLICT|changed|revision/i })).toBeVisible();
  const saved = (await (await request.get(`/v1/records/build.page/${pageID}`, { headers: { Authorization: "Bearer manager" } })).json()).record;
  expect(saved.title).toBe("Remote title");
  await page.getByRole("button", { name: "Cancel changes", exact: true }).click();
  await expect(inspector.getByLabel("What people call it", { exact: true })).toHaveValue("Remote title");
});
