import process from "node:process";
import { readFileSync } from "node:fs";
import { expect, test, signInWithOIDC } from "../tests/kit";

const password = process.env.PLATFORM_DEPLOY_PASSWORD;
if (!password) throw new Error("PLATFORM_DEPLOY_PASSWORD is required");
const fixtures = JSON.parse(readFileSync(new URL("../../../deploy/local/rehearsal-fixtures.json", import.meta.url), "utf8")) as {suffix:string;port:number;builder:string}[];
for (const fixture of fixtures) {
 test.describe(fixture.suffix, () => {
  test.use({baseURL:`http://localhost:${fixture.port}`});
	  test("builder reads the original versioned telemetry output in Workshop after recovery", async ({ page, request }, testInfo) => {
	    const token = await signInWithOIDC(page, fixture.builder, password);
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
	    await signInWithOIDC(page, fixture.builder, password);
	    await page.goto("/#/data-source?id=NATIVE-SOURCE");
	    await page.locator('[data-structured-editor^="build.source:NATIVE-SOURCE:"]').getByRole("button", { name: "Visual", exact: true }).click();
	    await expect(page.getByRole("combobox", { name: "Rows go to", exact: true })).toHaveValue("stream");
	    await expect(page.getByRole("combobox", { name: /^Connection\b/ })).toHaveValue("STREAM-DB");
	    await expect(page.getByRole("textbox", { name: "Incremental column", exact: false })).toHaveValue("seq");
	    await expect(page.getByRole("button", { name: "Pull now", exact: true })).toHaveCount(0);
	    await page.goto("/#/flow?id=NATIVE-P");
	    await page.getByRole("region", { name: "Editor", exact: true }).getByRole("button", { name: "Visual", exact: true }).click();
	    const properties = page.getByRole("region", { name: "Workflow properties", exact: true });
	    await expect(properties.getByLabel("Continuous source intake", { exact: false })).toHaveValue(/"sourceRecord":\s*"NATIVE-SOURCE"/);
	  });
 });
}
