import { createServer } from "node:http";
import { once } from "node:events";
import { expect, test } from "@playwright/test";
import { decide, fresh, open } from "./host";

for (const fixture of [
  { industry: "hospitality", baseURL: "http://127.0.0.1:18496", builder: "manager", operator: "desk", title: "Guest review" },
  { industry: "manufacturing", baseURL: "http://127.0.0.1:18497", builder: "supervisor", operator: "operator-l1", title: "Inspection review" },
]) {
  test.describe(fixture.industry, () => {
    test.use({ baseURL: fixture.baseURL });
    test("activate one candidate for a page and workflow using the same typed function", async ({ browser, page, request }) => {
      const model = createServer((_request, response) => {
        response.setHeader("Content-Type", "application/json");
        response.end(JSON.stringify({ choices: [{ message: { content: JSON.stringify({ summary: "Review the source" }) } }],
          usage: { prompt_tokens: 4, completion_tokens: 8, cost: 0.0125 } }));
      });
      model.listen(0, "127.0.0.1");
      await once(model, "listening");
      try {
        const address = model.address();
        if (!address || typeof address === "string") throw new Error("model stub did not listen");
        const name = fresh("joint").replace(/[^a-z0-9]/gi, "").toLowerCase(), type = `build.${name}`;
        const objectID = fresh("OBJ"), functionID = fresh("FN"), pageID = fresh("PAGE"), flowID = fresh("FLOW"), source = fresh("REC");
        const provider = fresh("provider").replace(/[^a-z0-9]/gi, "").toLowerCase();
        const headers = { Authorization: `Bearer ${fixture.builder}` };
        await decide(request, fixture.builder, "build", "build.object.create", { type: "build.object", id: objectID }, {
          name, title: fixture.title, fields: [{ name: "note", title: "Note", type: "text" }],
          states: [{ name: "open", title: "Open" }, { name: "done", title: "Done" }],
          actions: [{ name: "close", title: "Close", from: ["open"], to: "done" }],
        });
        await decide(request, fixture.builder, "build", "build.object.publish", { type: "build.object", id: objectID }, {});
        await decide(request, fixture.builder, "build", "build.function.create", { type: "build.function", id: functionID }, {
          name, title: fixture.title, description: "Review a readable note", object: type, fields: ["note"], roles: ["builder", "user"],
          instructions: "Summarise only the note", model: `${provider}/probe`, maxInputBytes: 2048, maxOutputBytes: 2048, maxTokens: 128,
          output: [{ name: "summary", type: "string", required: true, description: "Factual summary" }],
        });
        await decide(request, fixture.builder, "build", "build.function.publish", { type: "build.function", id: functionID }, {});
        await decide(request, fixture.builder, "build", "build.page.create", { type: "build.page", id: pageID }, {
          name: `${name}page`, title: fixture.title, object: type,
          sections: [{ widget: "table", fields: ["note"] }, { widget: "function", function: { name, version: 1 } }],
        });
        await decide(request, fixture.builder, "build", "build.page.publish", { type: "build.page", id: pageID }, {});
        await decide(request, fixture.builder, "build", "build.process.create", { type: "build.process", id: flowID }, {
          name: `${name}flow`, title: fixture.title, object: type, when: "open",
          steps: [{ name: "infer", function: { name, version: 1 }, next: "review" },
            { name: "review", ask: "user", answers: ["approve"], branches: { approve: "close" } },
            { name: "close", act: "close" }],
        });
        await decide(request, fixture.builder, "build", "build.process.publish", { type: "build.process", id: flowID }, {});
        await decide(request, fixture.builder, "ai", "ai.provider.add", { type: "ai.provider", id: provider },
          { kind: "local", baseUrl: `http://127.0.0.1:${address.port}/v1` });
        await decide(request, fixture.builder, "ai", "ai.model.enable", { type: "ai.model", id: `${provider}/probe` }, { access: "users" });

        await open(page, fixture.builder, "/candidate-test");
        await page.getByRole("combobox", { name: "Candidate kind" }).selectOption("function");
        await page.getByRole("combobox", { name: "Saved function draft" }).selectOption(functionID);
        await page.getByRole("textbox", { name: "Test plan name" }).fill(`${fixture.title} evaluation`);
        await page.getByRole("textbox", { name: "Model identifier" }).fill(`${provider}/probe`);
        await page.getByRole("checkbox", { name: "Require a measured release evaluation" }).check();
        await page.getByRole("textbox", { name: "Synthetic input (JSON object)" }).fill('{"note":"Synthetic review"}');
        await page.getByRole("textbox", { name: "Expected typed answer (JSON object)" }).fill('{"summary":"Review the source"}');
        await page.getByRole("button", { name: "Save test plan" }).click();
        await expect(page.getByRole("status").filter({ hasText: "Test plan saved." })).toBeVisible();

        await open(page, fixture.builder, "/release-review");
        await page.getByRole("combobox", { name: "Saved draft" }).selectOption(objectID);
        await page.getByRole("button", { name: "Check draft and dependencies" }).click();
        await expect(page.getByText("Candidate ready for review")).toBeVisible();
        await expect(page.getByText(`build/function/${name}`, { exact: true })).toBeVisible();
        await expect(page.getByText(`build/page/${name}page`, { exact: true })).toBeVisible();
        await expect(page.getByText(`build/flow/build.${name}flow`, { exact: true })).toBeVisible();
        const preview = await (await request.post("/v1/releases/preview", { headers, data: { kind: "object", id: objectID } })).json();
        expect(preview.candidateId).toMatch(/^sha256-v1:/);
        await page.getByRole("button", { name: "Save immutable candidate" }).click();
        await expect(page.getByRole("button", { name: "Activate release" })).toBeDisabled();
        await page.getByRole("combobox", { name: "Evaluation plan" }).selectOption({ label: `${fixture.title} evaluation` });
        await page.getByRole("button", { name: "Run measured evaluation" }).click();
        await expect.poll(async () => {
          const reports = await (await request.get("/v1/records/build.evaluation?limit=500", { headers })).json();
          return reports.records.find((report: { candidate: string; state: string }) => report.candidate === preview.candidateId)?.state;
        }).toBe("passed");
        await page.getByRole("button", { name: "Refresh evaluation reports" }).click();
        await expect(page.getByRole("status").filter({ hasText: "Report state: passed" })).toBeVisible();
        await page.getByRole("button", { name: "Activate release" }).click();
        await expect(page.getByRole("status").filter({ hasText: "Release active for operators." })).toBeVisible();
        const active = await (await request.get("/v1/releases/active", { headers })).json();
        expect(active.id).toBe(preview.candidateId);

        await decide(request, fixture.operator, "build", `${type}.create`, { type, id: source }, { note: "Needs human review" });
        const operator = await browser.newContext({ baseURL: fixture.baseURL, locale: "en-US" });
        const task = await operator.newPage();
        await open(task, fixture.operator, "/home");
        await task.getByRole("button", { name: "Application Studio" }).first().click();
        await task.getByRole("button", { name: fixture.title, exact: true }).click();
        await task.getByRole("textbox", { name: "Search" }).first().fill(source);
        await task.getByRole("row").filter({ hasText: source }).click();
        await task.getByRole("button", { name: "Request advice" }).click();
        const operatorHeaders = { Authorization: `Bearer ${fixture.operator}` };
        await expect.poll(async () => {
          const calls = await (await request.get("/v1/records/build.function-call?limit=500", { headers: operatorHeaders })).json();
          return calls.records.filter((r: { source: string; function: string; state: string; version: number; release: string }) =>
            r.source === `${type}/${source}` && r.function === name && r.state === "ready" && r.version === 1 &&
            r.release === active.id).length;
        }).toBe(2);
        await task.getByRole("button", { name: "Refresh advice" }).click();
        const answer = task.getByText('{"summary":"Review the source"}', { exact: true }).first();
        await expect(answer).toBeVisible();
        await expect(task.getByText("Measured model call")).toBeVisible();
        await expect(task.getByText("$0.012500")).toBeVisible();
        const instance = await (await request.get(`/v1/records/flow.instance/build.${name}flow:${source}`, { headers })).json();
        expect(instance.record.release).toBe(active.id);
        await open(task, fixture.operator, "/inbox");
        const review = task.getByRole("listitem").filter({ hasText: source });
        await expect(review).toBeVisible();
        await review.getByRole("button", { name: "approve", exact: true }).click();
        await expect.poll(async () => {
          const record = await (await request.get(`/v1/records/${type}/${source}`, { headers: operatorHeaders })).json();
          return record.record.state;
        }).toBe("done");
        await operator.close();
      } finally { model.close(); }
    });
  });
}
