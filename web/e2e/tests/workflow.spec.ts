import { Builder, Member, expect, fresh, test } from "./kit";

for (const fixture of [
  { industry: "hospitality", baseURL: "http://127.0.0.1:18496", builder: "manager", operator: "desk", operatorID: "desk-1", answer: "approve", state: "Done" },
  { industry: "manufacturing", baseURL: "http://127.0.0.1:18497", builder: "supervisor", operator: "operator-l1", operatorID: "op-l1", answer: "reject", state: "Rejected" },
]) {
  test.describe(fixture.industry, () => {
    test.use({ baseURL: fixture.baseURL });
    test("a tested native flow binds its release and only its operator answers", async ({ browser, page, request }) => {
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
      const properties = page.getByRole("region", { name: "Workflow properties", exact: true });
      await properties.getByRole("textbox", { name: "Workflow title", exact: true }).fill("Reviewed native flow");
      await page.keyboard.press("ControlOrMeta+s");
      await expect.poll(async () => (await builder.record("build.process", id)).title).toBe("Reviewed native flow");
      await page.getByRole("tab", { name: "Test", exact: true }).click();
      await page.getByRole("textbox", { name: "Member ID (empty: you)", exact: true }).fill(fixture.operatorID);
      await page.getByRole("group", { name: "Test step 1", exact: true }).getByRole("textbox", { name: "Note", exact: true }).fill("SYNTHETIC");
      await page.getByRole("group", { name: "Test step 2", exact: true }).getByRole("combobox", { name: "Human answer", exact: true }).selectOption(fixture.answer);
      await page.getByRole("button", { name: "Run isolated test", exact: true }).click();
      await expect(page.getByRole("status").filter({ hasText: "All expected outcomes matched." })).toBeVisible();
      await expect(page.getByRole("status").filter({ hasText: "Test state recovered exactly." })).toBeVisible();
      const candidate = await page.getByText("Tested candidate:").locator("code").innerText();
      expect((await request.get(`/v1/records/${type}/TEST-1`, { headers: operator.headers })).status()).toBe(404);

      // Release the tested bytes; actual work must retain that candidate binding.
      await page.getByRole("button", { name: "Publish", exact: true }).click();
      await page.getByRole("button", { name: "Check draft and dependencies", exact: true }).click();
      await expect(page.getByText("Draft candidate:").locator("code")).toHaveText(candidate);
      await page.getByRole("button", { name: "Save immutable candidate", exact: true }).click();
      await page.getByRole("button", { name: "Activate release", exact: true }).click();
      await expect(page.getByRole("status").filter({ hasText: "Release active for operators." })).toBeVisible();
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
