import { createServer } from "node:http";
import { once } from "node:events";
import { expect, test } from "@playwright/test";
import { decide, fresh, open } from "./host";

for (const fixture of [
  { industry: "hospitality", baseURL: "http://127.0.0.1:18496", admin: "manager", member: "sales", app: "crm", type: "crm.opportunity", list: "opportunities" },
  { industry: "manufacturing", baseURL: "http://127.0.0.1:18497", admin: "supervisor", member: "supervisor", app: "mes", type: "mes.order", list: "orders" },
]) {
  test.describe(fixture.industry, () => {
    test.use({ baseURL: fixture.baseURL });
    test("business record opens its AI work and returns to the inbox", async ({ page, request }, testInfo) => {
      const model = createServer((_request, response) => {
        response.setHeader("Content-Type", "application/json");
        response.end(JSON.stringify({ choices: [{ message: { content: JSON.stringify({ summary: "Review this source before acting", category: "review", review: true }) } }],
          usage: { prompt_tokens: 4, completion_tokens: 8 } }));
      });
      model.listen(0, "127.0.0.1");
      await once(model, "listening");
      try {
        const address = model.address();
        if (!address || typeof address === "string") throw new Error("model stub did not listen");
        const provider = fresh("source").toLowerCase();
        await decide(request, fixture.admin, "ai", "ai.provider.add", { type: "ai.provider", id: provider }, { kind: "local", baseUrl: `http://127.0.0.1:${address.port}/v1` });
        await decide(request, fixture.admin, "ai", "ai.model.enable", { type: "ai.model", id: `${provider}/review` }, { access: "everyone" });
        await decide(request, fixture.admin, "platform", "platform.setting.set", { type: "platform.setting", id: "ai/app-model" }, { value: `${provider}/review` });
        const id = fresh("RECORD");
        if (fixture.app === "crm") {
          const account = fresh("ACC");
          await decide(request, fixture.member, "crm", "crm.account.create", { type: "crm.account", id: account }, { name: "Task customer", kind: "company" });
          await decide(request, fixture.member, "crm", "crm.opportunity.open", { type: fixture.type, id }, { account, title: id });
        } else {
          const master = await (await request.get("/v1/master", { headers: { Authorization: `Bearer ${fixture.member}` } })).json();
          await decide(request, fixture.member, "mes", "mes.order.release", { type: fixture.type, id }, { product: master.products[0].id, quantity: 1, sfcs: 1 });
        }
        await open(page, fixture.member, `/${fixture.list}`);
        await page.getByRole("textbox", { name: "Search", exact: true }).fill(id);
        await page.getByRole("row").filter({ hasText: id }).click();
        const advice = page.getByRole("region", { name: "AI review advice" });
        await advice.getByRole("button", { name: "Request review advice" }).click();
        await page.getByRole("dialog").getByRole("button", { name: "Request review advice", exact: true }).click();
        await expect(advice.getByText("Review this source before acting", { exact: true })).toBeVisible();
        if (process.env.PLATFORM_SCREENSHOTS) {
          await page.screenshot({ path: testInfo.outputPath("record-advice-desktop.png"), fullPage: true });
          await page.setViewportSize({ width: 390, height: 780 });
          // Let the docking workspace settle after the viewport change before taking a visual sample.
          await page.waitForTimeout(300);
          await page.screenshot({ path: testInfo.outputPath("record-advice-narrow.png"), fullPage: true });
        }
        await page.getByRole("button", { name: "Back to inbox" }).click();
        await expect(page.getByRole("heading", { name: "Inbox", exact: true })).toBeVisible();
      } finally { model.close(); }
    });
  });
}
