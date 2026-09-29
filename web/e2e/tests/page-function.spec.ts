import { createServer } from "node:http";
import { once } from "node:events";
import { expect, test } from "@playwright/test";
import { decide, fresh, open } from "./host";

for (const fixture of [
  { industry: "hospitality", baseURL: "http://127.0.0.1:18496", builder: "manager", operator: "desk", title: "Guest advice" },
  { industry: "manufacturing", baseURL: "http://127.0.0.1:18497", builder: "supervisor", operator: "operator-l1", title: "Inspection advice" },
]) {
  test.describe(fixture.industry, () => {
    test.use({ baseURL: fixture.baseURL });
    test("compose a page, request a pinned typed answer and read the saved call", async ({ browser, page, request }, testInfo) => {
      const server = createServer((_request, response) => {
        response.setHeader("Content-Type", "application/json");
        response.end(JSON.stringify({ choices: [{ message: { content: JSON.stringify({ summary: "Human review requested" }) } }],
          usage: { prompt_tokens: 4, completion_tokens: 8 } }));
      });
      server.listen(0, "127.0.0.1");
      await once(server, "listening");
      try {
        const address = server.address();
        if (!address || typeof address === "string") throw new Error("model stub did not listen");
        const stamp = fresh("fnp").replace(/[^a-z0-9]/gi, "").toLowerCase(), type = `build.${stamp}`;
        const objectID = fresh("OBJ"), functionID = fresh("FN"), pageID = fresh("PAGE"), recordID = fresh("REC");
        const provider = fresh("provider").replace(/[^a-z0-9]/gi, "").toLowerCase();
        await decide(request, fixture.builder, "build", "build.object.create", { type: "build.object", id: objectID }, {
          name: stamp, title: fixture.title, fields: [{ name: "note", title: "Note", type: "text" }],
        });
        await decide(request, fixture.builder, "build", "build.object.publish", { type: "build.object", id: objectID }, {});
        await decide(request, fixture.builder, "build", "build.function.create", { type: "build.function", id: functionID }, {
          name: stamp, title: fixture.title, description: "For a person to review", object: type, fields: ["note"],
          roles: ["builder", "user"], instructions: "Summarise the note", model: `${provider}/probe`,
          maxInputBytes: 2048, maxOutputBytes: 2048, maxTokens: 128,
          output: [{ name: "summary", type: "string", required: true, description: "Summary" }],
        });
        await decide(request, fixture.builder, "build", "build.function.publish", { type: "build.function", id: functionID }, {});
        await decide(request, fixture.builder, "build", "build.page.create", { type: "build.page", id: pageID },
          { name: stamp, title: fixture.title, object: type });
        await open(page, fixture.builder, `/compose?id=${pageID}`);
        await page.getByRole("button", { name: "Table", exact: true }).click();
        await page.getByRole("button", { name: "AI function", exact: true }).click();
        await page.getByRole("combobox", { name: "Published function version" }).selectOption(`${stamp}:1`);
        await page.getByRole("button", { name: "Save", exact: true }).click();
        await page.getByRole("button", { name: "Publish", exact: true }).click();
        await expect(page.getByText("The page is in the workspace.")).toBeVisible();
        await decide(request, fixture.operator, "build", `${type}.create`, { type, id: recordID }, { note: "Needs advice" });
        const operatorContext = await browser.newContext({ baseURL: fixture.baseURL, locale: "en-US" });
        const operatorPage = await operatorContext.newPage();
        await open(operatorPage, fixture.operator, "/home");
        await operatorPage.getByRole("button", { name: "Application Studio" }).first().click();
        await operatorPage.getByRole("button", { name: fixture.title, exact: true }).click();
        await operatorPage.getByRole("textbox", { name: "Search" }).first().fill(recordID);
        await operatorPage.getByRole("row").filter({ hasText: recordID }).click();
        await expect(operatorPage.getByRole("button", { name: "Request advice" })).toBeEnabled();
        await operatorPage.getByRole("button", { name: "Request advice" }).click();
        const headers = { Authorization: `Bearer ${fixture.operator}` };
        const stateOf = async (state: string) => {
          const data = await (await request.get("/v1/records/build.function-call?limit=100", { headers })).json();
          return data.records.some((r: { source: string; function: string; state: string }) =>
            r.source === `${type}/${recordID}` && r.function === stamp && r.state === state);
        };
        await expect.poll(() => stateOf("rejected")).toBe(true);
        await operatorPage.getByRole("button", { name: "Refresh advice" }).click();
        await expect(operatorPage.getByText("rejected", { exact: true }).first()).toBeVisible();
        await decide(request, fixture.builder, "ai", "ai.provider.add", { type: "ai.provider", id: provider },
          { kind: "local", baseUrl: `http://127.0.0.1:${address.port}/v1` });
        await decide(request, fixture.builder, "ai", "ai.model.enable", { type: "ai.model", id: `${provider}/probe` }, { access: "users" });
        await operatorPage.getByRole("button", { name: "Request advice" }).click();
        await expect.poll(() => stateOf("ready")).toBe(true);
        await operatorPage.getByRole("button", { name: "Refresh advice" }).click();
        const savedAnswer = operatorPage.getByText('{"summary":"Human review requested"}', { exact: true }).first();
        await expect(savedAnswer).toBeVisible();
        await expect(operatorPage.getByText("Measured model call")).toBeVisible();
        await expect(operatorPage.getByText("Not reported", { exact: true }).first()).toBeVisible();
        await savedAnswer.scrollIntoViewIfNeeded();
        await operatorPage.screenshot({ path: testInfo.outputPath("function-page.png"), fullPage: true, animations: "disabled" });
        await operatorPage.setViewportSize({ width: 390, height: 844 });
        await savedAnswer.scrollIntoViewIfNeeded();
        await operatorPage.screenshot({ path: testInfo.outputPath("function-page-narrow.png"), animations: "disabled" });
        await operatorContext.close();
      } finally { server.close(); }
    });
  });
}
