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

  await test.step("enable and stop a temporary debug window without leaving shared test state behind", async () => {
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
