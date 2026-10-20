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
      await page.getByRole("tab", { name: "Problems", exact: true }).click();
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
        await expect(inbox.getByText(fixture.state, { exact: true }).first()).toBeVisible();
        expect([403, 404]).toContain((await request.get(`/v1/records/build.process/${id}`, { headers: operator.headers })).status());
      } finally { await context.close(); }
    });
  });
}
