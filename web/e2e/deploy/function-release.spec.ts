import process from "node:process";
import type { Page } from "@playwright/test";
import { expect, test, Member } from "../tests/kit";

const phase = process.env.PLATFORM_DEPLOY_PHASE;
if (phase !== "before" && phase !== "after") throw new Error("PLATFORM_DEPLOY_PHASE must be before or after");

const fixtures = [
  { industry: "hospitality", baseURL: "http://localhost:58495", email: "sales@hotel.test", builder: "manager@hotel.test", suffix: "hotel", type: "build.rehearsalhotel", page: "Recovery advice", flow: "build.reviewhotel" },
  { industry: "manufacturing", baseURL: "http://localhost:58490", email: "op1@plant.test", builder: "sup@plant.test", suffix: "plant", type: "build.rehearsalplant", page: "Recovery advice", flow: "build.reviewplant" },
] as const;

async function signIn(page: Page, email: string) {
  const password = process.env.PLATFORM_DEPLOY_PASSWORD;
  if (!password) throw new Error("PLATFORM_DEPLOY_PASSWORD is required");
  await page.goto("/");
  await page.locator('input[type="email"]').fill(email);
  await page.locator('button[type="submit"]').click();
  await page.locator('input[type="password"]').fill(password);
  await page.locator('button[type="submit"]').click();
  await expect(page.getByRole("button", { name: "Applications", exact: true })).toBeVisible();
  const token = await page.evaluate(() => (JSON.parse(sessionStorage.getItem("oidc:session") ?? "null") as { accessToken?: string } | null)?.accessToken);
  if (!token) throw new Error("OIDC sign-in returned no browser session");
  return token;
}

for (const fixture of fixtures) {
  test.describe(fixture.industry, () => {
    test.use({ baseURL: fixture.baseURL });
    if (phase === "before") {
      test("builder evaluates and activates the shared candidate in the deployed workspace", async ({ page, request }) => {
        const token = await signIn(page, fixture.builder);
        const headers = { Authorization: `Bearer ${token}` };
        const preview = await (await request.post("/v1/releases/preview", { headers, data: { kind: "object", id: "WF-O" } })).json() as { candidateId: string };
        expect(preview.candidateId).toMatch(/^sha256-v1:/);
        await page.goto("/#/release-review");
        await page.getByRole("combobox", { name: "Saved draft" }).selectOption("WF-O");
        await page.getByRole("button", { name: "Check draft and dependencies" }).click();
        for (const ref of [`build/function/advice${fixture.suffix}`, `build/page/advicepage${fixture.suffix}`, `build/flow/${fixture.flow}`]) {
          await expect(page.getByText(ref, { exact: true })).toBeVisible();
        }
        await page.getByRole("button", { name: "Save immutable candidate" }).click();
        await expect(page.getByRole("button", { name: "Activate release" })).toBeDisabled();
        await page.getByRole("combobox", { name: "Evaluation plan" }).selectOption({ label: `Recovery evaluation ${fixture.suffix}` });
        await page.getByRole("button", { name: "Run measured evaluation" }).click();
        await expect.poll(async () => {
          const reports = await (await request.get("/v1/records/build.evaluation?limit=500", { headers })).json() as {
            records: { candidate: string; state: string }[];
          };
          return reports.records.find((report) => report.candidate === preview.candidateId)?.state;
        }).toBe("passed");
        await page.getByRole("button", { name: "Refresh evaluation reports" }).click();
        await expect(page.getByRole("status").filter({ hasText: "Report state: passed" })).toBeVisible();
        await page.getByRole("button", { name: "Activate release" }).click();
        await expect(page.getByRole("status").filter({ hasText: "Release active for operators." })).toBeVisible();
        const active = await (await request.get("/v1/releases/active", { headers })).json() as { id: string };
        expect(active.id).toBe(preview.candidateId);
      });
    }
	if (phase === "after") {
	  test("builder reads the original versioned telemetry output in Workshop after recovery", async ({ page, request }, testInfo) => {
	    const token = await signIn(page, fixture.builder);
	    const headers = { Authorization: `Bearer ${token}` };
	    const instanceID = `build.nativestream${fixture.suffix}:continuous-v1`;
	    const instanceResponse = await request.get(`/v1/records/flow.instance/${instanceID}`, { headers });
	    expect(instanceResponse.ok()).toBeTruthy();
	    const instance = await instanceResponse.json() as { record: { version: number; release: string; state: string; batch: { cursor: string } } };
	    expect(instance.record.version).toBe(1);
	    expect(instance.record.release).toMatch(/^sha256-v1:/);
	    expect(instance.record.state).toBe("canceled");
	    const outputsResponse = await request.get("/v1/records/flow.output?limit=50", { headers });
	    expect(outputsResponse.ok()).toBeTruthy();
	    const outputs = await outputsResponse.json() as { records: { id: string; app: string; instance: string; version: number; name: string; batch: string; value: { mean: number }[] }[] };
	    const original = outputs.records.find(output => output.instance === instanceID && output.name === "liveTelemetryStats");
	    expect(original).toBeDefined();
	    expect(original!.app).toBe("build");
	    expect(original!.version).toBe(instance.record.version);
	    expect(original!.batch).toBe(instance.record.batch.cursor);
	    expect(original!.value.length).toBeGreaterThan(0);
	    expect(original!.value.every(row => row.mean === 13)).toBeTruthy();
	    const writes: string[] = [];
	    page.on("request", req => {
	      if (req.method() !== "GET" && new URL(req.url()).pathname.startsWith("/v1/")) writes.push(`${req.method()} ${new URL(req.url()).pathname}`);
	    });
	    await page.goto(`/#/page?app=build&kind=page&name=nativestats${fixture.suffix}`);
	    await expect(page.getByRole("heading", { name: "Retained telemetry output", exact: true })).toBeVisible();
	    const outputsPanel = page.getByRole("heading", { name: "Original output assets", exact: true }).locator("..");
	    const outputRow = outputsPanel.getByRole("row").filter({ hasText: instanceID });
	    await expect(outputRow).toBeVisible();
	    await expect(outputRow).toContainText("liveTelemetryStats");
	    await expect(outputRow.getByRole("cell", { name: String(original!.version), exact: true })).toBeVisible();
	    await expect(outputRow).toContainText(original!.batch);
	    await outputRow.click();
	    const detail = page.getByRole("heading", { name: "Accepted window statistics", exact: true }).locator("..");
	    await expect(detail).toContainText(instanceID);
	    await expect(detail).toContainText(/"mean":\s*13/);
	    const versionsPanel = page.getByRole("heading", { name: "Original Flow versions", exact: true }).locator("..");
	    const retainedRow = versionsPanel.getByRole("row").filter({ hasText: `build.nativestream${fixture.suffix}` });
	    await expect(retainedRow).toContainText(instance.record.release);
	    await expect(retainedRow).toContainText("canceled");
	    await expect(page.getByRole("button", { name: /^(Edit|Save|Create|Delete|Archive)$/, exact: true })).toHaveCount(0);
	    await expect(detail.getByRole("textbox")).toHaveCount(0);
	    expect(writes).toEqual([]);
	    const image = testInfo.outputPath("retained-telemetry-output.png");
	    await page.screenshot({ path: image, fullPage: true });
	    await testInfo.attach("Original read-only telemetry output", { path: image, contentType: "image/png" });
	  });
	  test("builder retains the controlled source and its continuous intake after recovery", async ({ page }) => {
	    await signIn(page, fixture.builder);
	    await page.goto("/#/data-source?id=STREAM-SOURCE");
	    await expect(page.getByRole("combobox", { name: "Rows go to", exact: true })).toHaveValue("stream");
	    await expect(page.getByRole("combobox", { name: /^Connection\b/ })).toHaveValue("STREAM-DB");
	    await expect(page.getByRole("textbox", { name: "Incremental column", exact: false })).toHaveValue("seq");
	    await expect(page.getByRole("button", { name: "Pull now", exact: true })).toHaveCount(0);
	    await page.goto("/#/flow?id=STREAM-PROCESS");
	    const properties = page.getByRole("region", { name: "Workflow properties", exact: true });
	    await expect(properties.getByLabel("Continuous source intake", { exact: false })).toHaveValue(/"sourceRecord":\s*"STREAM-SOURCE"/);
	  });
	}
    test(`shared function page and flow ${phase} database recovery`, async ({ page, request }) => {
      const token = await signIn(page, fixture.email);
      const headers = { Authorization: `Bearer ${token}` };
      const active = await (await request.get("/v1/releases/active", { headers })).json() as { id: string };
      expect(active.id).toMatch(/^sha256-v1:/);
      const source = "WF-BROWSER";
      if (phase === "before") {
        await new Member(request, token).decide(`${fixture.type}.create`, { type: fixture.type, id: source }, { note: "Browser recovery sample" });
      }
      const instanceURL = `/v1/records/flow.instance/${fixture.flow}:${source}`;
      const retained = phase === "after" ? await (await request.get(instanceURL, { headers })).json() as { record: { release: string } } : undefined;
      const release = retained?.record.release ?? active.id;
      expect(release).toMatch(/^sha256-v1:/);
      await page.goto(`/#/page?app=build&kind=page&name=advicepage${fixture.suffix}`);
      const sourceRow = page.getByRole("table").first().getByRole("row").filter({ hasText: source });
      const refreshAdvice = page.getByRole("button", { name: "Refresh advice", exact: true });
      const adviceHeading = page.getByText(`advice${fixture.suffix} · Version 1`, { exact: true });
      await expect(sourceRow).toBeVisible();
      await expect(adviceHeading).toBeVisible();
      if (!(await refreshAdvice.isVisible())) await sourceRow.click();
      await expect(refreshAdvice).toBeVisible();
      if (phase === "before") await page.getByRole("button", { name: "Request advice" }).click();
      await expect.poll(async () => {
        const calls = await (await request.get("/v1/records/build.function-call?limit=500", { headers })).json() as {
          records: { source: string; release: string; version: number; state: string; costReported: boolean }[];
        };
        return calls.records.filter((call) => call.source === `${fixture.type}/${source}` && call.release === release &&
          call.version === 1 && call.state === "ready" && call.costReported).length;
      }).toBe(2);
      await page.reload();
      // The original page session restores selection on reload. Clicking the
      // same row again toggles it off; select only when it was not restored.
      await expect(sourceRow).toBeVisible();
      await expect(adviceHeading).toBeVisible();
      if (!(await refreshAdvice.isVisible())) await sourceRow.click();
      await expect(refreshAdvice).toBeVisible();
      await refreshAdvice.click();
      const advicePanel = adviceHeading.locator("..");
      await advicePanel.getByRole("table").first().getByRole("row").filter({ hasText: `${fixture.type}/${source}` }).last().click();
      await expect(page.getByText("Measured model call")).toBeVisible();
      await expect(page.getByText("$0.010000")).toBeVisible();
      const instance = await (await request.get(instanceURL, { headers })).json() as { record: { release: string; version: number; state: string } };
      expect(instance.record.release).toBe(release);
      expect(instance.record.version).toBe(3);
      expect(instance.record.state).toBe("waiting");
      // A measured reply is accepted before the Flow owner's next tick
      // consumes it. Wait for that original owner to create the review task.
      await expect.poll(async () => {
        const current = await (await request.get(instanceURL, { headers })).json() as { record: { tokens: { waits?: string }[] } };
        return current.record.tokens.some(token => token.waits === "ask");
      }, { timeout: 20_000 }).toBe(true);
      await page.goto("/#/inbox");
      const review = page.getByRole("listitem").filter({ hasText: source });
      await expect(review).toBeVisible();
      if (phase === "after") {
        await review.getByRole("button", { name: "approve", exact: true }).click();
        await expect.poll(async () => {
          const answer = await (await request.get(`/v1/records/${fixture.type}/${source}`, { headers })).json() as { record: { state: string } };
          return answer.record.state;
        }).toBe("rejected");
        const finished = await (await request.get(instanceURL, { headers })).json() as { record: { state: string; release: string } };
        expect(finished.record.state).toBe("done");
        expect(finished.record.release).toBe(release);
      }
    });
  });
}
