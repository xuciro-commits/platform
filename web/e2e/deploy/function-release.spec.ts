import process from "node:process";
import { expect, test, type Page } from "@playwright/test";
import { decide } from "../tests/host";

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
  await expect(page.getByRole("button", { name: "Application Studio" }).first()).toBeVisible();
  const token = await page.evaluate(() => (JSON.parse(sessionStorage.getItem("oidc:session") ?? "null") as { accessToken?: string } | null)?.accessToken);
  if (!token) throw new Error("OIDC sign-in returned no browser session");
  return token;
}

for (const fixture of fixtures) {
  test.describe(fixture.industry, () => {
    test.use({ baseURL: fixture.baseURL });
    if (phase === "before") {
      test("builder evaluates and activates the shared candidate in the deployed workspace", async ({ page, request }, testInfo) => {
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
        await page.screenshot({ path: testInfo.outputPath("deployed-release-review.png"), fullPage: true, animations: "disabled" });
      });
    }
    test(`shared function page and flow ${phase} database recovery`, async ({ page, request }, testInfo) => {
      const token = await signIn(page, fixture.email);
      const headers = { Authorization: `Bearer ${token}` };
      const active = await (await request.get("/v1/releases/active", { headers })).json() as { id: string };
      expect(active.id).toMatch(/^sha256-v1:/);
      const source = "WF-BROWSER";
      if (phase === "before") {
        await decide(request, token, "build", `${fixture.type}.create`, { type: fixture.type, id: source }, { note: "Browser recovery sample" });
      }
      await page.goto("/#/home");
      await page.getByRole("button", { name: "Application Studio" }).first().click();
      await page.getByRole("button", { name: fixture.page, exact: true }).click();
      await page.getByRole("textbox", { name: "Search" }).first().fill(source);
      await page.getByRole("row").filter({ hasText: source }).click();
      if (phase === "before") await page.getByRole("button", { name: "Request advice" }).click();
      await expect.poll(async () => {
        const calls = await (await request.get("/v1/records/build.function-call?limit=500", { headers })).json() as {
          records: { source: string; release: string; version: number; state: string; costReported: boolean }[];
        };
        return calls.records.filter((call) => call.source === `${fixture.type}/${source}` && call.release === active.id &&
          call.version === 1 && call.state === "ready" && call.costReported).length;
      }).toBe(2);
      await page.getByRole("button", { name: "Refresh advice" }).click();
      await page.getByRole("table").last().getByRole("row").filter({ hasText: `${fixture.type}/${source}` }).last().click();
      await expect(page.getByText("Measured model call")).toBeVisible();
      await expect(page.getByText("$0.010000")).toBeVisible();
      if (phase === "after") {
        await page.setViewportSize({ width: 390, height: 844 });
        expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBeTruthy();
        await page.screenshot({ path: testInfo.outputPath("deployed-function-narrow.png"), fullPage: true, animations: "disabled" });
        await page.setViewportSize({ width: 1280, height: 720 });
      }
      const instanceURL = `/v1/records/flow.instance/${fixture.flow}:${source}`;
      const instance = await (await request.get(instanceURL, { headers })).json() as { record: { release: string; version: number; state: string } };
      expect(instance.record.release).toBe(active.id);
      expect(instance.record.version).toBe(3);
      expect(instance.record.state).toBe("waiting");
      await page.screenshot({ path: testInfo.outputPath(`deployed-function-${phase}.png`), fullPage: true, animations: "disabled" });
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
        expect(finished.record.release).toBe(active.id);
      }
    });
  });
}
