import { createServer } from "node:http";
import { once } from "node:events";
import { Builder, Editor, Member, expect, fresh, pageUIProfile, test } from "./kit";

for (const fixture of [
  { industry: "hospitality", baseURL: "http://127.0.0.1:18496", builder: "manager", member: "desk" },
  { industry: "manufacturing", baseURL: "http://127.0.0.1:18497", builder: "supervisor", member: "operator-l1" },
]) {
  test.describe(fixture.industry, () => {
    test.use({ baseURL: fixture.baseURL, actionTimeout: 10_000 });
    test("external rows become a released receiving application whose accepted action survives an outage", async ({ browser, page, request }, info) => {
      test.setTimeout(90_000);
      const builder = new Builder(request, fixture.builder), operator = new Member(request, fixture.member), editor = new Editor(page);
      const state = { down: true, rows: [{ id: "IN-A", sku: "A", qty: "3" }, { id: "IN-EMPTY", sku: "", qty: "1" }, { id: "IN-B", sku: "B", qty: "50" }] };
      const attempts: { key: string; body: string; status: number }[] = [];
      const sink = createServer(async (req, res) => {
        res.setHeader("Content-Type", "application/json");
        if (req.method === "GET") { res.end(JSON.stringify(state.rows)); return; }
        let body = "";
        for await (const chunk of req) body += chunk;
        res.statusCode = state.down ? 503 : 201;
        attempts.push({ key: String(req.headers["idempotency-key"]), body, status: res.statusCode });
        res.end(JSON.stringify(state.down ? { error: "offline" } : { d: { MaterialDocument: "DOC-1" } }));
      });
      sink.listen(0, "127.0.0.1");
      await once(sink, "listening");
      try {
        const address = sink.address();
        if (!address || typeof address === "string") throw new Error("external fixture did not listen");
        const name = fresh("fabric").replace(/[^a-z0-9]/gi, "").toLowerCase();
        const headers = builder.headers;
        const record = builder.record.bind(builder);
        // Connect and clean real external rows through the Data Connection editors.
        await page.addInitScript(() => localStorage.setItem("platform.language", "en"));
        await builder.open(page, "/dataset?id=new");
        await page.getByLabel("Dataset name", { exact: true }).fill(name);
        await page.getByLabel("Dataset title", { exact: true }).fill("Raw integration rows");
        await page.getByRole("button", { name: "Save dataset", exact: true }).click();
        await expect.poll(() => page.url().includes("id=new")).toBe(false);
        const raw = new URLSearchParams(new URL(page.url()).hash.split("?")[1]).get("id")!;
        const clean = fresh("CLEAN"), conn = fresh("CONN");
        await builder.decide("build.dataset.create", { type: "build.dataset", id: clean }, { name: `${name}clean`, title: "Clean integration rows" });
        await builder.decide("build.connection.create", { type: "build.connection", id: conn }, { name: `${name}conn`, title: "External fixture", kind: "http", address: `http://127.0.0.1:${address.port}/`, allowPrivate: true });
        await builder.decide("build.connection.check", { type: "build.connection", id: conn }, {});
        await expect.poll(async () => (await record("build.connection", conn))?.state, { timeout: 15_000 }).toBe("ready");
        await builder.open(page, "/data-source?id=new");
        await page.getByLabel("Source name", { exact: true }).fill(`${name}source`);
        await page.getByLabel("Source title", { exact: true }).fill("External rows");
        await page.getByRole("combobox", { name: /^Connection/ }).selectOption(conn);
        await page.getByRole("combobox", { name: "Rows go to", exact: true }).selectOption("dataset");
        await page.getByRole("combobox", { name: /^Target dataset/ }).selectOption(raw);
        await page.getByRole("button", { name: "Publish", exact: true }).click();
        await expect.poll(async () => (await record("build.dataset", raw))?.version, { timeout: 15_000 }).toBe(1);
        await builder.open(page, "/pipeline?id=new");
        await page.getByLabel("Pipeline name", { exact: true }).fill(`${name}pipe`);
        await page.getByLabel("Pipeline title", { exact: true }).fill("Clean external rows");
        await page.getByRole("combobox", { name: /^Input dataset/ }).selectOption(raw);
        await page.getByRole("combobox", { name: "Output dataset", exact: true }).selectOption(clean);
        await page.getByRole("button", { name: "Add step", exact: true }).click();
        // Select the step by its available grammar, then fill its labelled fields.
        await page.locator("select").filter({ has: page.locator('option[value="cast"]') }).selectOption("cast");
        await page.getByLabel("Column", { exact: true }).fill("qty");
        await page.getByRole("combobox", { name: "Type", exact: true }).selectOption("number");
        await page.getByRole("button", { name: "Add expectation", exact: true }).click();
        await page.getByPlaceholder("column", { exact: true }).fill("sku");
        await page.getByRole("button", { name: "Add expectation", exact: true }).click();
        await page.getByPlaceholder("column", { exact: true }).nth(1).fill("qty");
        await page.locator("select").filter({ has: page.locator('option[value="range"]') }).nth(1).selectOption("range");
        await page.getByPlaceholder("0..100", { exact: true }).fill("0..10");
        await page.getByRole("button", { name: "Publish", exact: true }).click();
        await expect.poll(async () => (await record("build.dataset", clean))?.version, { timeout: 15_000 }).toBe(1);
        const pipe = new URLSearchParams(new URL(page.url()).hash.split("?")[1]).get("id")!;
        expect((await record("build.pipeline", pipe)).last).toMatchObject({ rows: 3, written: 1, quarantined: 2 });
        expect((await record("build.datasetversion", `${clean}@1`)).data).toEqual([{ id: "IN-A", sku: "A", qty: 3 }]);
        await expect(page.getByText(/3 rows · 1 written · 2 quarantined/)).toBeVisible();
        if (process.env.PLATFORM_SCREENSHOTS) await page.screenshot({ path: info.outputPath("pipeline.png"), fullPage: true });
        await builder.open(page, `/dataset?id=${clean}`);
        await expect(page.getByText("A", { exact: true }).last()).toBeVisible();
        await page.getByRole("button", { name: "Pipeline Clean external rows", exact: true }).click();
        await expect(page.getByLabel("Pipeline name", { exact: true })).toHaveValue(`${name}pipe`);

        // Deliver a page and its native flow together; the object is also the pipeline's target.
        const { id: object, type } = await builder.object({ name: `${name}receipt`, title: "Integration receipt", plural: "Integration receipts",
          fields: [{ name: "item", title: "Item", type: "text", search: true }, { name: "qty", title: "Quantity", type: "integer" }, { name: "docno", title: "Document", type: "text" },
            { name: "code", title: "Code", type: "text", search: true }, { name: "name", title: "Name", type: "text", search: true }],
          implements: ["core.coded"],
          states: [{ name: "open", title: "Open" }, { name: "received", title: "Received" }],
          actions: [{ name: "receive", title: "Receive", from: ["open"], to: "received", roles: ["user"],
            inputs: [{ name: "item", title: "Received item", type: "text", required: true }] }],
        });
        const receiving = await builder.page({ name: `${name}receiving`, title: "Receiving workspace", object: type,
          sections: [{ id: "rows", widget: "table", configVersion: 1, fields: ["item", "qty", "state", "docno"] },
            { id: "actions", widget: "actions", configVersion: 1, actions: [`${type}.receive`] }],
          document: { formatVersion: 2, uiProfile: pageUIProfile, root: "root", nodes: { root: { kind: "rows", children: ["rows", "actions"] }, rows: { kind: "widget", section: "rows" }, actions: { kind: "widget", section: "actions" } } },
        });
        const flowID = fresh("FLOW"), flowName = `${name}received`, appID = fresh("APP");
        await builder.decide("build.process.create", { type: "build.process", id: flowID }, { name: flowName, title: "Received delivery", object: type, when: "received", steps: [{ name: "done", kind: "end" }] });
        await builder.decide("build.app.create", { type: "build.app", id: appID }, { name: `${name}app`, title: "Receiving application", pages: [receiving.name],
          resources: [{ app: "build", kind: "flow", name: `build.${flowName}` }], groups: [{ title: "Empty receiving group", pages: [] }],
        });
        await builder.open(page, `/module?id=${appID}`);
        await page.getByRole("button", { name: "Publish", exact: true }).click();
        await page.getByRole("button", { name: "Add the drafts it depends on", exact: true }).click();
        await page.getByRole("button", { name: "Check joint candidate", exact: true }).click();
        await expect(page.getByRole("alert").filter({ hasText: "holds no page" })).toBeVisible();
        await page.getByRole("button", { name: "Open the module's navigation", exact: true }).click();
        await expect(page.getByRole("tab", { name: "Navigation", exact: true })).toHaveAttribute("aria-selected", "true");
        await page.getByRole("button", { name: "Remove group", exact: true }).click();
        await expect.poll(async () => (await record("build.app", appID)).groups ?? []).toEqual([]);
        let candidate = "";
        await editor.release(async () => {
          await expect(page.getByRole("list", { name: "Joint selection", exact: true })).toContainText(receiving.id);
          await expect(page.getByRole("list", { name: "Joint selection", exact: true })).toContainText(flowID);
          candidate = await page.getByText("Saved candidate:", { exact: false }).locator("code").innerText();
        }, true);
        await expect(page.getByRole("status").filter({ hasText: "Release active for operators." })).toBeVisible();

        const land = fresh("PIPE");
        await builder.decide("build.pipeline.create", { type: "build.pipeline", id: land }, { name: `${name}land`, title: "Land receiving records", input: clean, steps: [{ kind: "rename", from: "sku", to: "item" }] });
        await builder.open(page, `/pipeline?id=${land}`);
        await page.getByRole("combobox", { name: "Write to", exact: true }).selectOption("object");
        await page.getByRole("combobox", { name: /^Output object/ }).selectOption(type);
        await page.getByRole("textbox", { name: /^Record id column/ }).fill("id");
        await page.getByRole("button", { name: "Publish", exact: true }).click();
        await expect.poll(async () => (await record("build.pipeline", land)).last?.written, { timeout: 15_000 }).toBe(1);
        const wb = fresh("WB"), receipt = "IN-A";
        expect(await record(type, receipt)).toMatchObject({ item: "A", qty: 3, state: "open" });
        await builder.decide("build.writeback.create", { type: "build.writeback", id: wb }, { name: `${name}wb`, title: "Receipt delivery", object: type, on: "receive", connection: conn, path: "receipts", result: [{ from: "MaterialDocument", to: "docno" }] });
        await builder.open(page, `/writeback?id=${wb}`);
        await page.getByRole("button", { name: "Publish", exact: true }).click();
        await expect.poll(async () => (await record("build.writeback", wb))?.state).toBe("published");

        const context = await browser.newContext({ baseURL: fixture.baseURL, locale: "en-US" });
        const member = await (await request.get("/v1/me", { headers: operator.headers })).json();
        try {
          // An ordinary member receives the imported row in the released application, without builder access.
          const business = await context.newPage();
          await operator.open(business, "/home");
          await business.getByRole("button", { name: "Receiving application", exact: true }).click();
          await expect(business.getByRole("heading", { name: "Receiving workspace", exact: true })).toBeVisible();
          await business.getByRole("row").filter({ hasText: receipt }).click();
          await business.getByRole("button", { name: "Receive", exact: true }).click();
          const confirmation = business.getByRole("dialog");
          await confirmation.getByRole("textbox", { name: /^Received item/ }).fill("A");
          await confirmation.getByRole("button", { name: "Receive", exact: true }).click();
          await expect(confirmation).toHaveCount(0);
          await expect.poll(async () => (await record(type, receipt)).state).toBe("received");
          await expect.poll(async () => (await record("flow.instance", `build.${flowName}:${receipt}`))?.release).toBe(candidate);
          await expect(operator.decide("build.object.edit", { type: "build.object", id: object }, { title: "Unauthorized" })).rejects.toThrow(/POLICY_DENIED/);
          await expect.poll(async () => (await (await request.get("/v1/integration-effects", { headers })).json()).find((e: { event: string }) => e.event === `writeback/${name}wb`)?.state, { timeout: 15_000 }).toBe("retrying");
          await expect(page.getByText("1 queued", { exact: true })).toBeVisible();
          const first = attempts[0];
          if (!first) throw new Error("writeback did not reach the external fixture");
          expect(first.status).toBe(503);
          expect(JSON.parse(first.body)).toMatchObject({ id: receipt, item: "A" });
          state.down = false;
          await expect.poll(async () => (await record(type, receipt))?.docno, { timeout: 20_000 }).toBe("DOC-1");
          await expect(page.getByText("1 delivered", { exact: true })).toBeVisible();
          await expect(business.getByRole("row").filter({ hasText: receipt })).toContainText("DOC-1");
          await business.reload();
          await expect(business.getByRole("row").filter({ hasText: receipt })).toContainText("DOC-1");
          if (process.env.PLATFORM_SCREENSHOTS) await business.screenshot({ path: info.outputPath("receiving-application.png"), fullPage: true });
          expect(attempts.filter((a) => a.status === 201)).toHaveLength(1);
          for (const attempt of attempts) expect(attempt).toMatchObject({ key: first.key, body: first.body });
          const health = await (await request.get("/v1/health", { headers })).json();
          expect(health.status).not.toBe("quarantined");
          await builder.open(page, "/integration-health");
          await expect(page.getByRole("button", { name: /Writeback · Receipt delivery/ })).toBeVisible();
          if (process.env.PLATFORM_SCREENSHOTS) await page.screenshot({ path: info.outputPath("integration-health.png"), fullPage: true });

          // The interface selector must distinguish equal IDs and open the actual object.
          const reference = await builder.object({ name: `${name}reference`, title: "Reference receipt", implements: ["core.coded"],
            fields: [{ name: "code", title: "Code", type: "text", search: true }, { name: "name", title: "Name", type: "text", search: true }],
          });
          await builder.decide(`${reference.type}.create`, { type: reference.type, id: receipt }, { code: "REF", name: "Duplicate identity" });
          await builder.open(page, "/query?id=new");
          await page.getByRole("textbox", { name: "Query name", exact: true }).fill(`${name}lookup`);
          await page.getByRole("textbox", { name: "Query title", exact: true }).fill("Receiving lookup");
          await page.getByRole("textbox", { name: "Query description", exact: true }).fill("Typed interface selection");
          await page.getByRole("combobox", { name: "Query source kind", exact: true }).selectOption("interface");
          await page.getByRole("combobox", { name: "Source interface", exact: true }).selectOption("core.coded");
          await page.getByRole("button", { name: "Save query", exact: true }).click();
          await expect.poll(() => page.url().includes("id=new")).toBe(false);
          const queryID = new URLSearchParams(new URL(page.url()).hash.split("?")[1]).get("id")!;
          await editor.release(undefined,false,"Review release");
          await builder.open(page, `/query?id=${queryID}`);
          await page.getByRole("combobox", { name: "Query result record", exact: true }).fill(receipt);
          await expect(page.getByRole("option").filter({ hasText: `Integration receipt · ${receipt}` })).toBeVisible();
          await page.getByRole("option").filter({ hasText: `Reference receipt · ${receipt}` }).click();
          await page.getByRole("button", { name: "Open selected record", exact: true }).click();
          await expect(page.getByText("Duplicate identity", { exact: true })).toBeVisible();
          if (process.env.PLATFORM_SCREENSHOTS) await page.screenshot({ path: info.outputPath("interface-record.png"), fullPage: true });

          // Grant only the integration role; its delivery view must still redact host effect payloads.
          await builder.decide("platform.member.grant", { type: "platform.member", id: member.principalId }, { app: "build", role: "integrator" }, "platform");
          const integration = await context.newPage();
          await operator.open(integration, "/integration-health");
          await expect(integration.getByRole("button", { name: /Writeback · Receipt delivery/ })).toBeVisible();
          if (!await integration.getByRole("button", { name: "Connections", exact: true }).isVisible()) await integration.getByRole("button", { name: "Toggle navigation", exact: true }).click();
          await expect(integration.getByRole("button", { name: "Connections", exact: true })).toBeVisible();
          await expect(integration.getByRole("button", { name: "Object types", exact: true })).toHaveCount(0);
          await expect(integration.getByRole("button", { name: "All projects", exact: true })).toHaveCount(0);
          const delivery = await (await request.get("/v1/integration-effects", { headers: operator.headers })).json();
          expect(delivery.find((e: { event: string }) => e.event === `writeback/${name}wb`)).toMatchObject({ state: "delivered" });
          expect(delivery.some((e: object) => "body" in e || "target" in e || "answer" in e)).toBe(false);
          expect((await request.get("/v1/effects", { headers: operator.headers })).status()).toBe(403);
          if (process.env.PLATFORM_SCREENSHOTS) await integration.screenshot({ path: info.outputPath("integrator.png"), fullPage: true });
        } finally {
          await context.close();
          await builder.decide("platform.member.grant", { type: "platform.member", id: member.principalId }, { app: "build", role: member.profile.roles.build }, "platform");
        }
      } finally { sink.closeAllConnections(); sink.close(); }
    });
  });
}
