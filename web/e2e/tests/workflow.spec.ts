import { Builder, Member, expect, fresh, test } from "./kit";

test.use({ actionTimeout: 15_000 });

for (const fixture of [
  { industry: "hospitality", baseURL: "http://127.0.0.1:18496", builder: "manager", operator: "desk", operatorID: "desk-1", answer: "approve", state: "Done" },
  { industry: "manufacturing", baseURL: "http://127.0.0.1:18497", builder: "supervisor", operator: "operator-l1", operatorID: "op-l1", answer: "reject", state: "Rejected" },
]) {
  test.describe(fixture.industry, () => {
    test.use({ baseURL: fixture.baseURL });
    test("a tested native flow binds its release and only its operator answers", async ({ browser, page, request }, testInfo) => {
      test.setTimeout(90_000);
      const builder = new Builder(request, fixture.builder);
      const operator = new Member(request, fixture.operator);
      const { type } = await builder.object({ title: "Review sample", fields: [{ name: "note", title: "Note", type: "text" }],
        states: [{ name: "open", title: "Open" }, { name: "done", title: "Done" }, { name: "rejected", title: "Rejected" }],
        actions: [{ name: "close", title: "Close", from: ["open"], to: "done" }, { name: "reject", title: "Reject", from: ["open"], to: "rejected" }],
      });
      const id = fresh("FLOW"), name = fresh("review").replace(/[^a-z0-9]/gi, "").toLowerCase();
      await builder.decide("build.process.create", { type: "build.process", id }, { name, title: "Native review", object: type, when: "open", steps: [
        { name: "ask", title: "Review sample", kind: "ask", ask: "user", answers: ["approve", "reject"], cases: { approve: "close", reject: "reject" } },
        { name: "close", title: "Close sample", kind: "action", act: "close" },
        { name: "reject", title: "Reject sample", kind: "action", act: "reject" },
      ] });

      // Edit and save through the current flow workbench, then test synthetic data.
      await builder.open(page, `/flow?id=${id}`);
      await page.getByRole("region", { name: "Editor", exact: true }).getByRole("button", { name: "Visual", exact: true }).click();
      const properties = page.getByRole("region", { name: "Workflow properties", exact: true });
      await properties.getByLabel("Default workflow input", { exact: false }).fill("{");
      const structure = page.getByRole("region", { name: "Flow structure", exact: true });
      await structure.getByRole("button", { name: /^Review sample/ }).click();
      await expect(page.getByRole("button", { name: "Validate", exact: true })).toBeDisabled();
      await properties.getByRole("tab",{name:"Settings",exact:true}).click();
      const history=page.locator("header").filter({has:page.getByRole("heading",{name:"Native review",exact:true})});
      const timeout = properties.getByRole("spinbutton", {name:"Timeout (seconds)",exact:true});
      await timeout.fill("-");
      await history.getByRole("button",{name:"Undo",exact:true}).click();
      await expect(timeout).toHaveValue("0");
      await history.getByRole("button",{name:"Redo",exact:true}).click();
      await expect(timeout).toHaveValue("-");
      // The shared frame closes without deselecting the block or its input draft.
      await page.getByRole("button", { name: "Close inspector panel", exact: true }).click();
      await expect(properties).not.toBeVisible();
      await page.getByRole("button", { name: "Toggle inspector panel", exact: true }).click();
      await expect(properties.getByRole("tab", { name: "Settings", exact: true })).toHaveAttribute("aria-selected", "true");
      await expect(timeout).toHaveValue("-");
      await properties.getByRole("tab", { name: "Settings", exact: true }).focus();
      await page.keyboard.press("ArrowRight");
      await expect(properties.getByRole("tab", { name: "Input", exact: true })).toBeFocused();
      await page.keyboard.press("ArrowLeft");
      await expect(timeout).toHaveValue("-");
      await structure.getByRole("button", { name: "Native review", exact: true }).click();
      await expect(properties.getByLabel("Default workflow input", { exact: false })).toHaveValue("{");
      await properties.getByLabel("Default workflow input", { exact: false }).fill("{}");
      await expect(page.getByRole("button", {name:"Validate",exact:true})).toBeDisabled();
      await structure.getByRole("button",{name:/^Review sample/}).click();
      await properties.getByRole("tab",{name:"Settings",exact:true}).click();
      await expect(properties.getByRole("spinbutton",{name:"Timeout (seconds)",exact:true})).toHaveValue("-");
      await properties.getByRole("spinbutton",{name:"Timeout (seconds)",exact:true}).fill("0");
      await structure.getByRole("button",{name:"Native review",exact:true}).click();
      await expect(page.getByRole("button", { name: "Validate", exact: true })).toBeEnabled();
      await properties.getByRole("textbox", { name: "Workflow title", exact: true }).fill("Reviewed native flow");
      await page.keyboard.press("ControlOrMeta+s");
      await expect.poll(async () => (await builder.record("build.process", id)).title).toBe("Reviewed native flow");
      await page.getByRole("tab", { name: "Test", exact: true }).click();
      await page.getByRole("tabpanel", { name: "Test", exact: true }).getByRole("button", { name: "Visual", exact: true }).click();
      await page.getByRole("textbox", { name: "Member ID (empty: you)", exact: true }).fill(fixture.operatorID);
      await page.getByRole("group", { name: "Test step 1", exact: true }).getByRole("textbox", { name: "Note", exact: true }).fill("SYNTHETIC");
      await page.getByRole("group", { name: "Test step 2", exact: true }).getByRole("combobox", { name: "Human answer", exact: true }).selectOption(fixture.answer);
      // Active tabs select; only an explicit close collapses the dock. Neither
      // tab changes nor collapse may destroy the isolated test setup.
      const testTab = page.getByRole("tab", { name: "Test", exact: true });
      const note = page.getByRole("group", { name: "Test step 1", exact: true }).getByRole("textbox", { name: "Note", exact: true });
      await testTab.click();
      await expect(note).toHaveValue("SYNTHETIC");
      await page.getByRole("tab", { name: /^Problems(?: \d+)?$/ }).click();
      await testTab.click();
      await expect(note).toHaveValue("SYNTHETIC");
      await page.getByRole("button", { name: "Close dock", exact: true }).click();
      await expect(note).not.toBeVisible();
      await testTab.focus();
      await page.keyboard.press("ControlOrMeta+j");
      await expect(note).toHaveValue("SYNTHETIC");
      await page.getByRole("button", { name: "Run isolated test", exact: true }).click();
      await expect(page.getByRole("status").filter({ hasText: "All expected outcomes matched." })).toBeVisible();
      await expect(page.getByRole("status").filter({ hasText: "Test state recovered exactly." })).toBeVisible();
      await page.getByRole("button", { name: "Test details", exact: true }).click();
      const candidate = await page.getByText("Tested candidate", { exact: true }).locator("..").locator("code").innerText();
      expect((await request.get(`/v1/records/${type}/TEST-1`, { headers: operator.headers })).status()).toBe(404);

      // Release the tested bytes; actual work must retain that candidate binding.
      await page.getByRole("button", { name: "Publish", exact: true }).click();
      await expect(page.getByRole("button", { name: "Check draft and dependencies", exact: true })).toBeVisible();
      await page.getByRole("button", { name: "Close dock", exact: true }).click();
      await page.getByRole("button", { name: "Publish", exact: true }).click();
      await page.getByRole("button", { name: "Check draft and dependencies", exact: true }).click();
      await page.getByRole("button", { name: "Candidate identity", exact: true }).click();
      await expect(page.getByText("Draft candidate", { exact: true }).locator("..").locator("code")).toHaveText(candidate);
      await page.getByRole("button", { name: "Save immutable candidate", exact: true }).click();
      await page.getByRole("button", { name: "Activate release", exact: true }).click();
      await expect(page.getByRole("status").filter({ hasText: "Release active for operators." })).toBeVisible();
      if (process.env.PLATFORM_SCREENSHOTS) await page.screenshot({ path: testInfo.outputPath("flow-workbench.png"), fullPage: true });
      // The same editor remains usable at a narrow container: no stacked,
      // unreachable inspector beneath a clipped canvas.
      await page.setViewportSize({ width: 390, height: 844 });
      const inspectorToggle = page.getByRole("button", { name: "Toggle inspector panel", exact: true });
      await expect(inspectorToggle).toHaveAttribute("aria-expanded", "false");
      await inspectorToggle.click();
      await expect(properties.getByRole("textbox", { name: "Workflow title", exact: true })).toHaveValue("Reviewed native flow");
      await expect(structure).not.toBeVisible();
      if (process.env.PLATFORM_SCREENSHOTS) await page.screenshot({ path: testInfo.outputPath("flow-inspector-narrow.png"), fullPage: true });
      await page.getByRole("button", { name: "Close inspector panel", exact: true }).click();
      await expect(properties).not.toBeVisible();
      await expect(inspectorToggle).toBeFocused();
      await page.getByRole("button", { name: "Toggle structure panel", exact: true }).click();
      await expect(structure.getByRole("button", { name: /^Review sample/ })).toBeVisible();
      await page.getByRole("button", { name: "Close structure panel", exact: true }).click();
      await page.setViewportSize({ width: 1280, height: 720 });
      const real = fresh("REAL");
      await operator.decide(`${type}.create`, { type, id: real }, { note: "OPERATOR RECORD" });
      const context = await browser.newContext({ baseURL: fixture.baseURL });
      try {
        const inbox = await context.newPage();
        await operator.open(inbox, "/inbox");
        const task = inbox.getByRole("listitem").filter({ hasText: real });
        await expect(task).toBeVisible();
        const binding = await builder.record("flow.instance", `build.${name}:${real}`);
        expect(binding.dependencies).toMatch(/^sha256-v1:/);
        expect(binding.release).toBe(candidate);
        await task.getByRole("button", { name: fixture.answer, exact: true }).click();
        await operator.open(inbox, `/record?type=${type}&id=${real}`);
        await expect(inbox.getByRole("definition").filter({ hasText: new RegExp(`^${fixture.state}$`) })).toBeVisible();
        expect([403, 404]).toContain((await request.get(`/v1/records/build.process/${id}`, { headers: operator.headers })).status());
      } finally { await context.close(); }
    });
  });
}

test.describe("flow condition editing", () => {
  test.use({ baseURL: "http://127.0.0.1:18496" });
  test("nested condition connections and problem locations survive source edits and undo", async ({ page, request }) => {
    const builder = new Builder(request, "manager");
    const id = fresh("FLOW"), name = fresh("conditions").replace(/[^a-z0-9]/gi, "").toLowerCase();
    const condition = { op: "all", terms: [
      { op: "eq", left: { source: "step", step: "calculate", path: ["score"] }, right: { source: "literal", value: 3 } },
      { op: "not", terms: [{ op: "exists", left: { source: "step", step: "calculate", path: ["blocked"] } }] },
    ] };
    await builder.decide("build.process.create", { type: "build.process", id }, {
      name, title: "Condition references", manual: true,
      inputSchema: { type: "object", properties: { score: { type: "integer" }, blocked: { type: "boolean" } }, required: ["score", "blocked"] },
      input: { score: 3, blocked: false },
      steps: [
        { name: "calculate", title: "Calculate score", kind: "transform", next: "eligible", inputs: { score: { source: "input", path: ["score"] }, blocked: { source: "input", path: ["blocked"] } } },
        { name: "eligible", title: "Eligible score", kind: "branch", condition, cases: { true: "done", false: "done" } },
        { name: "done", title: "Review complete", kind: "end", value: { source: "literal", value: "done" } },
      ],
    });
    await builder.open(page, `/flow?id=${id}`);
    const editor = page.getByRole("region", { name: "Editor", exact: true });
    await editor.getByRole("button", { name: "Source editor", exact: true }).click();
    const source = editor.getByRole("textbox", { name: "Configuration source", exact: true });
    const original = JSON.parse(await source.inputValue());
    await editor.getByRole("button", { name: "Visual", exact: true }).click();
    const canvas = page.getByRole("region", { name: "Flow map", exact: true });
    const firstEdge = canvas.getByTestId("rf__edge-eligible:binding:condition.terms.0.left");
    const nestedEdge = canvas.getByTestId("rf__edge-eligible:binding:condition.terms.1.terms.0.left");
    const expectNestedEdgeDrawn = async () => {
      // A horizontal SVG edge has zero geometric height, so Playwright calls
      // its group hidden even though its nonzero-width stroke is rendered.
      await expect.poll(() => nestedEdge.locator("path.react-flow__edge-path").evaluate((node) => {
        const path = node as SVGPathElement, box = path.getBoundingClientRect(), style = getComputedStyle(path);
        return path.getTotalLength() > 0 && Math.max(box.width, box.height) > 0 && style.visibility === "visible" &&
          style.stroke !== "none" && Number.parseFloat(style.strokeWidth) > 0;
      })).toBe(true);
    };
    await expect(firstEdge).toBeVisible();
    await expectNestedEdgeDrawn();
    await expect(canvas.getByLabel("Input port: Condition 1 · Left value, integer", { exact: true })).toBeVisible();
    await expect(canvas.getByLabel("Input port: Condition 2.1 · Left value, boolean", { exact: true })).toBeVisible();
    await expect(canvas.getByLabel("Output port: score, integer", { exact: true })).toBeVisible();
    await expect(canvas.getByLabel("Output port: blocked, boolean", { exact: true })).toBeVisible();

    // Keep the original nested references while introducing a control error in this draft.
    await editor.getByRole("button", { name: "Source editor", exact: true }).click();
    const changed = structuredClone(original);
    changed.steps[1].cases.false = "missing";
    await source.fill(JSON.stringify(changed, null, 2));
    await editor.getByRole("button", { name: "Visual", exact: true }).click();
    const structure = page.getByRole("region", { name: "Flow structure", exact: true });
    await structure.getByRole("button", { name: "Review complete", exact: true }).click();
    await page.getByRole("button", { name: "Close inspector panel", exact: true }).click();
    await page.getByRole("tab", { name: /^Problems(?: \d+)?$/ }).click();
    const problems = page.getByRole("tabpanel", { name: /^Problems(?: \d+)?$/ });
    await problems.getByRole("button").filter({ hasText: "A connected step is missing." }).click();
    const properties = page.getByRole("region", { name: "Workflow properties", exact: true });
    await properties.getByRole("tab", { name: "Settings", exact: true }).click();
    await expect(properties.getByRole("textbox", { name: "Step name", exact: true })).toHaveValue("eligible");

    // The native compiler's node-qualified error locates the same original step.
    await page.getByRole("button", { name: "Close inspector panel", exact: true }).click();
    await page.getByRole("button", { name: "Validate", exact: true }).click();
    await problems.getByRole("button").filter({ hasText: "Node eligible:" }).click();
    await properties.getByRole("tab", { name: "Settings", exact: true }).click();
    await expect(properties.getByRole("textbox", { name: "Step name", exact: true })).toHaveValue("eligible");

    const history = page.locator("header").filter({ has: page.getByRole("heading", { name: "Condition references", exact: true }) });
    await history.getByRole("button", { name: "Undo", exact: true }).click();
    await expect(problems.getByRole("button").filter({ hasText: "A connected step is missing." })).toHaveCount(0);
    await expect(firstEdge).toBeVisible();
    await expectNestedEdgeDrawn();
    await editor.getByRole("button", { name: "Source editor", exact: true }).click();
    expect(JSON.parse(await source.inputValue()).steps[1]).toEqual(original.steps[1]);
    expect(JSON.parse(await source.inputValue()).steps[1].condition).toEqual(condition);
    await editor.getByRole("button", { name: "Visual", exact: true }).click();
    await page.getByRole("button", { name: "Validate", exact: true }).click();
    await expect(problems.getByText("The native compiler accepted this draft.", { exact: true })).toBeVisible();
    expect((await builder.record("build.process", id)).steps[1].condition).toEqual(condition);
  });
});
