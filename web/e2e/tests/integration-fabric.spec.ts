import { createServer } from "node:http";
import { once } from "node:events";
import { expect, test } from "@playwright/test";
import { decide, fresh, open } from "./host";

for (const fixture of [
  { industry: "hospitality", baseURL: "http://127.0.0.1:18496", builder: "manager", member: "desk" },
  { industry: "manufacturing", baseURL: "http://127.0.0.1:18497", builder: "supervisor", member: "operator-l1" },
]) {
  test.describe(fixture.industry, () => {
    test.use({ baseURL: fixture.baseURL, actionTimeout: 10_000 });
    test("source, dataset, pipeline and writeback survive an external outage without losing the accepted answer", async ({ browser, page, request }, info) => {
      test.setTimeout(90_000);
      const state = { down: true, rows: [{ sku: "A", qty: "3" }, { sku: "", qty: "1" }, { sku: "B", qty: "50" }] };
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
        const headers = { Authorization: `Bearer ${fixture.builder}` };
        const record = async (type: string, id: string) => (await (await request.get(`/v1/records/${type}/${id}`, { headers })).json()).record;
        const submit = (schema: string, type: string, id: string, payload: unknown) => decide(request, fixture.builder, "build", schema, { type, id }, payload);
        await page.addInitScript(() => localStorage.setItem("platform.language", "en"));
        await open(page, fixture.builder, "/dataset?id=new");
        await page.getByLabel("Dataset name", { exact: true }).fill(name);
        await page.getByLabel("Dataset title", { exact: true }).fill("Raw integration rows");
        await page.getByRole("button", { name: "Save dataset", exact: true }).click();
        await expect.poll(() => page.url().includes("id=new")).toBe(false);
        const raw = new URLSearchParams(new URL(page.url()).hash.split("?")[1]).get("id")!;
        const clean = fresh("CLEAN"), conn = fresh("CONN");
        await submit("build.dataset.create", "build.dataset", clean, { name: `${name}clean`, title: "Clean integration rows" });
        await submit("build.connection.create", "build.connection", conn, { name: `${name}conn`, title: "External fixture", kind: "http", address: `http://127.0.0.1:${address.port}/`, allowPrivate: true });
        await submit("build.connection.check", "build.connection", conn, {});
        await expect.poll(async () => (await record("build.connection", conn))?.state, { timeout: 15_000 }).toBe("ready");
        await open(page, fixture.builder, "/data-source?id=new");
        await page.getByLabel("Source name", { exact: true }).fill(`${name}source`);
        await page.getByLabel("Source title", { exact: true }).fill("External rows");
        await page.getByRole("combobox", { name: /^Connection/ }).selectOption(conn);
        await page.getByRole("combobox", { name: "Rows go to", exact: true }).selectOption("dataset");
        await page.getByRole("combobox", { name: /^Target dataset/ }).selectOption(raw);
        await page.getByRole("button", { name: "Publish", exact: true }).click();
        await expect.poll(async () => (await record("build.dataset", raw))?.version, { timeout: 15_000 }).toBe(1);
        await open(page, fixture.builder, "/pipeline?id=new");
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
        expect((await record("build.datasetversion", `${clean}@1`)).data).toEqual([{ sku: "A", qty: 3 }]);
        await expect(page.getByText(/3 rows · 1 written · 2 quarantined/)).toBeVisible();
        if (process.env.PLATFORM_SCREENSHOTS) await page.screenshot({ path: info.outputPath("pipeline.png"), fullPage: true });
        await open(page, fixture.builder, `/dataset?id=${clean}`);
        await expect(page.getByText("A", { exact: true }).last()).toBeVisible();
        await page.getByRole("button", { name: "Pipeline Clean external rows", exact: true }).click();
        await expect(page.getByLabel("Pipeline name", { exact: true })).toHaveValue(`${name}pipe`);

        const object = fresh("OBJECT"), type = `build.${name}receipt`, wb = fresh("WB"), receipt = fresh("RECEIPT");
        await submit("build.object.create", "build.object", object, { name: `${name}receipt`, title: "Integration receipts", fields: [{ name: "item", title: "Item", type: "text" }, { name: "docno", title: "Document", type: "text" }] });
        await submit("build.object.publish", "build.object", object, {});
        await submit("build.writeback.create", "build.writeback", wb, { name: `${name}wb`, title: "Receipt delivery", object: type, on: "create", connection: conn, path: "receipts", result: [{ from: "MaterialDocument", to: "docno" }] });
        await open(page, fixture.builder, `/writeback?id=${wb}`);
        await page.getByRole("button", { name: "Publish", exact: true }).click();
        await expect.poll(async () => (await record("build.writeback", wb))?.state).toBe("published");
        await submit(`${type}.create`, type, receipt, { item: "A" });
        await expect.poll(async () => (await (await request.get("/v1/integration-effects", { headers })).json()).find((e: { event: string }) => e.event === `writeback/${name}wb`)?.state, { timeout: 15_000 }).toBe("retrying");
        await expect(page.getByText("1 queued", { exact: true })).toBeVisible();
        const first = attempts[0];
        if (!first) throw new Error("writeback did not reach the external fixture");
        expect(first.status).toBe(503);
        expect(JSON.parse(first.body)).toMatchObject({ id: receipt, item: "A" });
        state.down = false;
        await expect.poll(async () => (await record(type, receipt))?.docno, { timeout: 20_000 }).toBe("DOC-1");
        await expect(page.getByText("1 delivered", { exact: true })).toBeVisible();
        expect(attempts.filter((a) => a.status === 201)).toHaveLength(1);
        for (const attempt of attempts) expect(attempt).toMatchObject({ key: first.key, body: first.body });
        const health = await (await request.get("/v1/health", { headers })).json();
        expect(health.status).not.toBe("quarantined");
        await open(page, fixture.builder, "/integration-health");
        await expect(page.getByRole("button", { name: /Writeback · Receipt delivery/ })).toBeVisible();
        if (process.env.PLATFORM_SCREENSHOTS) await page.screenshot({ path: info.outputPath("integration-health.png"), fullPage: true });

        const memberHeaders = { Authorization: `Bearer ${fixture.member}` };
        const member = await (await request.get("/v1/me", { headers: memberHeaders })).json();
        const context = await browser.newContext({ baseURL: fixture.baseURL, locale: "en-US" });
        try {
          await decide(request, fixture.builder, "platform", "platform.member.grant", { type: "platform.member", id: member.principalId }, { app: "build", role: "integrator" });
          const integration = await context.newPage();
          await open(integration, fixture.member, "/integration-health");
          await expect(integration.getByRole("button", { name: /Writeback · Receipt delivery/ })).toBeVisible();
		  if (!await integration.getByRole("button", { name: "Connections", exact: true }).isVisible()) await integration.getByRole("button", { name: "Toggle navigation", exact: true }).click();
		  await expect(integration.getByRole("button", { name: "Connections", exact: true })).toBeVisible();
          await expect(integration.getByRole("button", { name: "Object types", exact: true })).toHaveCount(0);
          await expect(integration.getByRole("button", { name: "All projects", exact: true })).toHaveCount(0);
          const delivery = await (await request.get("/v1/integration-effects", { headers: memberHeaders })).json();
          expect(delivery.find((e: { event: string }) => e.event === `writeback/${name}wb`)).toMatchObject({ state: "delivered" });
          expect(delivery.some((e: object) => "body" in e || "target" in e || "answer" in e)).toBe(false);
          expect((await request.get("/v1/effects", { headers: memberHeaders })).status()).toBe(403);
          if (process.env.PLATFORM_SCREENSHOTS) await integration.screenshot({ path: info.outputPath("integrator.png"), fullPage: true });
        } finally {
          await context.close();
          await decide(request, fixture.builder, "platform", "platform.member.grant", { type: "platform.member", id: member.principalId }, { app: "build", role: member.profile.roles.build });
        }
      } finally { sink.closeAllConnections(); sink.close(); }
    });
  });
}
