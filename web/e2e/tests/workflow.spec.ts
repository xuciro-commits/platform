import { expect, test } from "@playwright/test";
import { decide, fresh, open } from "./host";

for (const fixture of [
  { industry: "hospitality", baseURL: "http://127.0.0.1:18496", builder: "manager", builderID: "manager-1", operator: "desk", operatorID: "desk-1", title: "Guest service review", answer: "approve", state: "Done" },
  { industry: "manufacturing", baseURL: "http://127.0.0.1:18497", builder: "supervisor", builderID: "sup-1", operator: "operator-l1", operatorID: "op-l1", title: "Shop inspection review", answer: "reject", state: "Rejected" },
]) {
  test.describe(fixture.industry, () => {
    test.use({ baseURL: fixture.baseURL });
    test("route 43: compose, test, publish and answer a native workflow", async ({ page, request }) => {
      test.setTimeout(90_000);
      const objectID = fresh("OBJ"), stamp = fresh("wf").replace(/[^a-z0-9]/gi, "").toLowerCase(), name = `item${stamp}`, flowName = `review${stamp}`, type = `build.${name}`;
      await decide(request, fixture.builder, "build", "build.object.create", { type: "build.object", id: objectID }, {
        name, title: fixture.title, fields: [{ name: "note", title: "Note", type: "text" }],
        states: [{ name: "open", title: "Open" }, { name: "done", title: "Done" }, { name: "rejected", title: "Rejected" }],
        actions: [{ name: "close", title: "Close", from: ["open"], to: "done" }, { name: "reject", title: "Reject", from: ["open"], to: "rejected" }],
      });
      await decide(request, fixture.builder, "build", "build.object.publish", { type: "build.object", id: objectID }, {});
      await open(page, fixture.builder, "/studio");
      const studio = page.getByRole("region", { name: "Application map" });
      await expect(studio.getByText(fixture.title, { exact: true })).toBeVisible();
      await studio.getByText(fixture.title, { exact: true }).click();
      await expect(page.getByRole("region", { name: "Asset inspector" }).getByText(`build.${name}`)).toBeVisible();
      await page.getByRole("button", { name: "Open selected asset" }).click();
      await expect(page.getByRole("heading", { name: `Design ${fixture.title}` })).toBeVisible();
      await open(page, fixture.builder, "/workflow");
      await page.getByRole("button", { name: "New workflow", exact: true }).click();
      const properties = page.getByRole("region", { name: "Workflow properties" });
      await properties.getByRole("textbox", { name: "Workflow name", exact: true }).fill(flowName);
      await properties.getByRole("textbox", { name: "Workflow title" }).fill(fixture.title);
      await properties.getByRole("combobox", { name: "Source object" }).selectOption(type);
      await page.getByRole("button", { name: "Add human task" }).click();
      await properties.getByRole("textbox", { name: "Step title" }).fill("Review sample");
      await page.getByRole("button", { name: "Add object action" }).click();
      await properties.getByRole("textbox", { name: "Step name" }).fill("close");
      await properties.getByRole("textbox", { name: "Step title" }).fill("Close sample");
      await page.getByRole("button", { name: "Add object action" }).click();
      await properties.getByRole("textbox", { name: "Step name" }).fill("reject");
      await properties.getByRole("textbox", { name: "Step title" }).fill("Reject sample");
      await properties.getByRole("combobox", { name: "Object action", exact: true }).selectOption("reject");
      await page.getByRole("button", { name: "1. Review sample", exact: true }).click();
      await properties.getByRole("combobox", { name: "After answer reject" }).selectOption("reject");
      await properties.getByRole("combobox", { name: "After answer approve" }).selectOption("close");
      await page.getByRole("button", { name: "Save workflow", exact: true }).focus();
      await page.keyboard.press("Enter");
      await expect(page.getByRole("button", { name: "Test workflow", exact: true })).toBeEnabled();
      const records = await (await request.get("/v1/records/build.process?limit=500", { headers: { Authorization: `Bearer ${fixture.builder}` } })).json();
      const workflow = records.records.find((p: { name: string }) => p.name === flowName);
      expect(workflow.steps[0].branches).toEqual({ approve: "close", reject: "reject" });
      // Publication errors remain editable. Save is allowed to retain an
      // unfinished draft; publish checks the owner graph again.
      await page.getByRole("button", { name: "1. Review sample", exact: true }).click();
      await properties.getByRole("textbox", { name: "Step name" }).fill("InvalidName");
      await page.getByRole("button", { name: "Publish workflow", exact: true }).click();
      await expect(page.getByRole("alert").filter({ hasText: "unique lower-case name" })).toBeVisible();
      await properties.getByRole("textbox", { name: "Step name" }).fill("review");
      await page.getByRole("button", { name: "Save workflow", exact: true }).click();
      await expect(page.getByRole("button", { name: "Save workflow", exact: true })).toBeDisabled();
      // Concurrent editing refuses a stale revision and retains local input.
      await page.getByRole("button", { name: "Workflow settings", exact: true }).click();
      await properties.getByRole("textbox", { name: "Workflow title" }).fill("Local edit");
      await decide(request, fixture.builder, "build", "build.process.edit", { type: "build.process", id: workflow.id }, { title: "Remote edit" });
      await page.getByRole("button", { name: "Save workflow", exact: true }).click();
      await expect(page.getByRole("alert").filter({ hasText: "changed since it was read" })).toBeVisible();
      await expect(properties.getByRole("textbox", { name: "Workflow title" })).toHaveValue("Local edit");
      await page.getByRole("button", { name: "Reload saved workflow", exact: true }).click();
      await expect(properties.getByRole("textbox", { name: "Workflow title" })).toHaveValue("Remote edit");
      await properties.getByRole("textbox", { name: "Workflow title" }).fill(fixture.title);
      await page.getByRole("button", { name: "Save workflow", exact: true }).click();
      await expect(page.getByRole("button", { name: "Test workflow", exact: true })).toBeEnabled();
      await page.getByRole("button", { name: "Test workflow", exact: true }).click();
      await expect(page.getByRole("combobox", { name: "Saved workflow draft" })).toHaveValue(workflow.id);
      await page.getByRole("textbox", { name: "Member ID (empty: you)" }).fill(fixture.operatorID);
      await page.getByRole("textbox", { name: "Test plan name" }).fill(`Fixed ${flowName}`);
      const first = page.getByRole("group", { name: "Test step 1", exact: true });
      const answer = page.getByRole("group", { name: "Test step 2", exact: true });
      await first.getByRole("textbox", { name: "Test inputs (JSON)" }).fill('{"note":"FIXED SAMPLE"}');
      await answer.getByRole("combobox", { name: "Human answer", exact: true }).selectOption(fixture.answer);
      await page.getByRole("button", { name: "Save test plan", exact: true }).click();
      await expect(page.getByRole("status").filter({ hasText: "Test plan saved." })).toBeVisible();
      await page.getByRole("button", { name: "Run isolated test" }).click();
      await expect(page.getByRole("status").filter({ hasText: "All expected outcomes matched." })).toBeVisible();
      await expect(page.getByRole("status").filter({ hasText: "Test state recovered exactly." })).toBeVisible();
      await expect(page.getByText("Why it moved", { exact: true }).first()).toBeVisible();
      const candidate = await page.getByText("Tested candidate:").locator("code").innerText();
      await page.getByText("Why it moved", { exact: true }).last().scrollIntoViewIfNeeded();
      await page.getByText("Release binding", { exact: true }).last().click();
      await expect(page.getByText("Development run", { exact: true }).last()).toBeVisible();
      await answer.getByRole("textbox", { name: "Step member ID (empty: plan member)" }).fill(fixture.builderID);
      await answer.getByRole("combobox", { name: "Expected outcome" }).selectOption("refused");
      await expect(page.getByRole("status").filter({ hasText: "Test state recovered exactly." })).toHaveCount(0);
      await page.getByRole("button", { name: "Run isolated test" }).click();
      await expect(page.getByText("Test step 2 · Refused", { exact: true })).toBeVisible();
      await expect(page.getByRole("status").filter({ hasText: "All expected outcomes matched." })).toBeVisible();
      await page.getByRole("button", { name: "Reload saved plan" }).click();
      await expect(answer.getByRole("textbox", { name: "Step member ID (empty: plan member)" })).toHaveValue("");
      await page.reload();
      await page.getByRole("button", { name: "Test workflow", exact: true }).click();
      await page.getByRole("combobox", { name: "Saved test plan" }).selectOption({ label: `Fixed ${flowName}` });
      await expect(page.getByRole("combobox", { name: "Saved workflow draft" })).toHaveValue(workflow.id);
      await page.getByRole("button", { name: "Run isolated test" }).focus();
      await page.keyboard.press("Enter");
      await expect(page.getByRole("status").filter({ hasText: "All expected outcomes matched." })).toBeVisible();
      await expect(page.getByText("Tested candidate:").locator("code")).toHaveText(candidate);
      const missing = await request.get(`/v1/records/${type}/TEST-1`, { headers: { Authorization: `Bearer ${fixture.operator}` } });
      expect(missing.status()).toBe(404);
      await open(page, fixture.builder, `/workflow?id=${workflow.id}`);
      await page.getByRole("button", { name: "Publish workflow", exact: true }).click();
      await expect(page.getByRole("status").filter({ hasText: "Installed workflow version 1." })).toBeVisible();
      await page.getByRole("button", { name: "Release", exact: true }).click();
      await page.getByRole("button", { name: "Check draft and dependencies" }).click();
      await expect(page.getByText("Draft candidate:").locator("code")).toHaveText(candidate);
      await page.getByRole("button", { name: "Save immutable candidate" }).click();
      await page.getByRole("button", { name: "Test", exact: true }).click();
      await expect(page.getByRole("combobox", { name: "Saved workflow draft" })).toHaveValue(workflow.id);
      await page.getByRole("button", { name: "Release", exact: true }).click();
      await page.getByRole("button", { name: "Activate release" }).click();
      await expect(page.getByRole("status").filter({ hasText: "Release active for operators." })).toBeVisible();
      const real = fresh("REAL");
      await decide(request, fixture.operator, "build", `${type}.create`, { type, id: real }, { note: "OPERATOR RECORD" });
      const operator = await page.context().newPage();
      await open(operator, fixture.operator, "/inbox");
      const task = operator.getByRole("listitem").filter({ hasText: real });
      await expect(task).toBeVisible();
      const bound = await request.get(`/v1/records/flow.instance/build.${flowName}:${real}`, { headers: { Authorization: `Bearer ${fixture.builder}` } });
      expect(bound.ok()).toBeTruthy();
      const binding = (await bound.json()).record;
      expect(binding.dependencies).toMatch(/^sha256-v1:/);
      expect(binding.release).toBe(candidate);
      await task.getByRole("button", { name: fixture.answer, exact: true }).click();
      await open(operator, fixture.operator, `/record?type=${type}&id=${real}`);
      await expect(operator.getByText(fixture.state, { exact: true }).first()).toBeVisible();
      await expect(operator.getByRole("button", { name: "Workflows", exact: true })).toHaveCount(0);
      const privateWorkflow = await request.get(`/v1/records/build.process/${workflow.id}`, { headers: { Authorization: `Bearer ${fixture.operator}` } });
      expect([403, 404]).toContain(privateWorkflow.status());
      await operator.close();
      await open(page, fixture.builder, `/workflow?id=${workflow.id}`);
      await page.getByRole("button", { name: "Runs", exact: true }).click();
      await expect(page.getByRole("heading", { name: "Run history" })).toBeVisible();
      await expect(page.getByRole("table").getByText(fixture.title).first()).toBeVisible();
      await page.getByRole("button", { name: "Review sample", exact: true }).first().click();
      await expect(page.getByRole("region", { name: "Workflow properties" }).getByRole("textbox", { name: "Step title" })).toHaveValue("Review sample");
    });
  });
}
