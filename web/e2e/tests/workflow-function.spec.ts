import { expect, test } from "@playwright/test";
import { decide, fresh, open } from "./host";
import { addBlock, chooseBlock } from "./workflow-helpers";

for (const fixture of [
  { industry: "hospitality", baseURL: "http://127.0.0.1:18496", builder: "manager", member: "desk-1", title: "Guest advice review" },
  { industry: "manufacturing", baseURL: "http://127.0.0.1:18497", builder: "supervisor", member: "op-l1", title: "Inspection advice review" },
]) {
  test.describe(fixture.industry, () => {
    test.use({ baseURL: fixture.baseURL });
    test("save and reload a workflow with a pinned function", async ({ page, request }) => {
      const name = fresh("fnflow").replace(/[^a-z0-9]/gi, "").toLowerCase(), type = `build.${name}`;
      const objectID = fresh("OBJ"), functionID = fresh("FN");
      await decide(request, fixture.builder, "build", "build.object.create", { type: "build.object", id: objectID }, {
        name, title: fixture.title, fields: [{ name: "note", title: "Note", type: "text" }],
        states: [{ name: "open", title: "Open" }, { name: "done", title: "Done" }],
        actions: [{ name: "close", title: "Close", from: ["open"], to: "done" }],
      });
      await decide(request, fixture.builder, "build", "build.object.publish", { type: "build.object", id: objectID }, {});
      await decide(request, fixture.builder, "build", "build.function.create", { type: "build.function", id: functionID }, {
        name, title: fixture.title, description: "Advice for review", object: type, fields: ["note"], roles: ["builder", "user"],
        instructions: "Summarise the note", maxInputBytes: 2048, maxTokens: 128, maxOutputBytes: 2048,
        output: [{ name: "summary", type: "string", required: true, description: "Summary" }],
      });
      await decide(request, fixture.builder, "build", "build.function.publish", { type: "build.function", id: functionID }, {});
      await decide(request, fixture.builder, "build", "build.function.edit", { type: "build.function", id: functionID }, { instructions: "New instructions" });
      await decide(request, fixture.builder, "build", "build.function.publish", { type: "build.function", id: functionID }, {});
      await open(page, fixture.builder, "/workflow");
      await page.getByRole("button", { name: "New workflow", exact: true }).click();
      const properties = page.getByRole("region", { name: "Workflow properties" });
      await properties.getByRole("textbox", { name: "Workflow name", exact: true }).fill(name);
      await properties.getByRole("textbox", { name: "Workflow title" }).fill(fixture.title);
      await properties.getByRole("combobox", { name: "Source object" }).selectOption(type);
      await properties.getByRole("checkbox", { name: "Manual or API start" }).uncheck();
      await addBlock(page, `build/function/${name}`);
      await properties.getByRole("spinbutton", { name: "Retained AI version" }).fill("1");
      await addBlock(page, "flow/control/ask");
      await addBlock(page, `build/action/${type}.close`);
      await chooseBlock(page, "ask");
      await properties.getByRole("combobox", { name: "After approve", exact: true }).selectOption("close");
      await chooseBlock(page, name, true);
      await properties.getByText("Control paths", { exact: true }).click();
      await properties.getByRole("combobox", { name: "Next path", exact: true }).selectOption("ask");
      await page.getByRole("button", { name: "Save workflow", exact: true }).click();
      await expect(page.getByRole("button", { name: "Save workflow", exact: true })).toBeDisabled();
      const inventory = await (await request.get("/v1/records/build.process?limit=500", { headers: { Authorization: `Bearer ${fixture.builder}` } })).json();
      const workflow = inventory.records.find((p: { name: string }) => p.name === name);
      expect(workflow.steps[0].kind).toBe("ai");
      expect(workflow.steps[0].function).toEqual({ app: "build", name, version: 1 });
      await page.getByRole("button", { name: "Direct install", exact: true }).click();
      await expect.poll(async () => (await (await request.get(`/v1/records/build.process/${workflow.id}`, { headers: { Authorization: `Bearer ${fixture.builder}` } })).json()).record.version).toBe(1);
      await page.reload();
      await chooseBlock(page, name);
      await expect(properties.getByRole("spinbutton", { name: "Retained AI version" })).toHaveValue("1");
    });
  });
}
