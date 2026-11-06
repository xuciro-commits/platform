import { expect, signIn, test } from "./kit";

test("F17 an administrator finds a refused submission in Logs, inspects its request correlation, and changes the log policy", async ({ page }) => {
  const headers = { authorization: "Bearer admin", "Platform-Tenant": "acceptance" };

  await test.step("create a refused submission that must appear as operational evidence", async () => {
    const response = await page.request.post("/v1/submissions", {
      headers: { ...headers, "Content-Type": "application/json" },
      data: '{"schema":',
    });
    expect(response.status()).toBe(400);
  });

  await test.step("open Logs, narrow to warnings from submissions, and inspect the event", async () => {
    await signIn(page, "admin", "/monitoring-logs");
    await expect(page.getByRole("heading", { name: "Logs", level: 1 })).toBeVisible();
    await page.getByLabel("Level", { exact: true }).selectOption("warn");
    await page.getByLabel("Source", { exact: true }).selectOption("submit");
    const row = page.getByRole("row").filter({ hasText: "Submission refused" }).first();
    await expect(row).toBeVisible({ timeout: 10_000 });
    await row.click();
    await expect(page.getByRole("dialog", { name: "Log event" })).toBeVisible();
    await expect(page.getByRole("dialog", { name: "Log event" })).toContainText("POST /v1/submissions");
    await expect(page.getByRole("dialog", { name: "Log event" })).toContainText("INVALID_ARGUMENT");
    await page.getByRole("dialog", { name: "Log event" }).getByRole("button", { name: "Close" }).click();
  });

  await test.step("the monitoring and health entry points all render", async () => {
    const liveFailures: string[] = [];
    const watchLive = (response: import("@playwright/test").Response) => {
      if (response.url().includes("/v1/changes") && response.status() >= 400) {
        liveFailures.push(`${response.status()} ${response.url()}`);
      }
    };
    page.on("response", watchLive);
    try {
      for (const [path, heading] of [
        ["/monitoring-overview", "Overview"],
        ["/monitoring-traces", "Traces"],
        ["/monitoring-metrics", "Metrics"],
        ["/monitoring-alerts", "Alerts"],
        ["/monitoring-views", "Monitoring views"],
        ["/system-health", "Health"],
      ]) {
        await page.goto(`/#${path}`);
        await expect(page.getByRole("heading", { name: heading, level: 1 })).toBeVisible();
        if (path === "/monitoring-overview") {
          await expect(page.getByRole("button", { name: "Data coverage and known limits" })).toBeVisible();
          await expect(page.getByRole("alert")).toHaveCount(0);
        }
        await page.waitForTimeout(1_500);
      }
    } finally {
      page.off("response", watchLive);
    }
    expect(liveFailures).toEqual([]);
    await expect(page.getByLabel("Readiness conditions")).toBeVisible();
  });

  await test.step("enable and stop a temporary debug window without leaving shared test state behind", async () => {
    await page.goto("/#/monitoring-logs");
    await expect(page.getByRole("heading", { name: "Logs", level: 1 })).toBeVisible();
    await page.getByRole("button", { name: "Log policy" }).click();
    const policy = page.getByRole("dialog", { name: "Log policy" });
    await expect(policy).toBeVisible();
    await policy.getByLabel("Temporary debug window").selectOption("15");
    await policy.getByLabel("Temporary debug reason").fill("F17 interaction check");
    await policy.getByRole("button", { name: "Save policy" }).click();
    await expect(policy).toBeHidden({ timeout: 10_000 });
    await page.getByRole("button", { name: "Log policy" }).click();
    await page.getByRole("dialog", { name: "Log policy" }).getByLabel("Temporary debug window").selectOption("0");
    await page.getByRole("dialog", { name: "Log policy" }).getByRole("button", { name: "Save policy" }).click();
    await expect(page.getByRole("dialog", { name: "Log policy" })).toBeHidden({ timeout: 10_000 });
  });

  await test.step("the log read itself is visible in the audit trail", async () => {
    const response = await page.request.get("/v1/audit", { headers });
    expect(response.ok(), await response.text()).toBeTruthy();
    const audit = await response.json() as { entries: { action: string; member: string }[] };
    expect(audit.entries.some((entry) => entry.action === "platform.log.read" && entry.member === "admin-1")).toBeTruthy();
  });

  await test.step("the user entry crosses from Logs to Audit and back", async () => {
    await signIn(page, "admin", "/monitoring-logs");
    await page.getByRole("button", { name: "Audit trail" }).click();
    await expect(page.getByRole("heading", { name: "Audit", level: 1 })).toBeVisible();
    await expect(page.getByRole("button", { name: "Open logs" })).toBeVisible();
    await page.getByRole("button", { name: "Open logs" }).click();
    await expect(page.getByRole("heading", { name: "Logs", level: 1 })).toBeVisible();
  });
});
