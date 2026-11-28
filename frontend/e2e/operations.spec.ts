import { type Page, expect, test } from "@playwright/test";
import type { Api } from "../src/host";
import { open } from "./support";

async function signInAs(page: Page, member: string, tenant: string) {
  await page.goto("/");
  await page.evaluate(async ({ member, tenant }) => {
    const res = await fetch("/v1/sign-in", { cache: "no-store" });
    if (!res.ok) throw new Error(`sign-in options failed: ${res.status}`);
    const data = await res.json() as Api.SignIn;
    const match = (data.identities ?? []).find((id) => id.member === member && id.tenant === tenant)
      ?? (data.identities ?? []).find((id) => id.member === member);
    if (!match) throw new Error(`no identity for ${member} @ ${tenant}`);
    sessionStorage.clear();
    sessionStorage.setItem("platform:session", JSON.stringify({ token: match.token, tenant }));
  }, { member, tenant });
  await page.goto("/home/overview");
  await expect(page.getByRole("button", { name: /^(Account|Open navigation|账户)$/ })).toBeVisible();
}

test("usage: workspace settings shows progress, monthly trend and clickable sources", async ({ page }) => {
  await signInAs(page, "admin-1", "acceptance");

  const usage = await page.evaluate(async () => {
    const raw = sessionStorage.getItem("platform:session");
    if (!raw) throw new Error("missing session");
    const session = JSON.parse(raw) as { token: string; tenant: string };
    const assign = await fetch(`/v1/host/tenants/${session.tenant}/quota`, {
      method: "POST",
      headers: {
        Authorization: `Bearer ${session.token}`,
        "Content-Type": "application/json",
      },
      body: JSON.stringify({ plan: "starter", reason: "e2e usage check" }),
    });
    if (!assign.ok) throw new Error(`quota assign failed: ${assign.status}`);
    const res = await fetch("/v1/monitoring/usage", {
      headers: {
        Authorization: `Bearer ${session.token}`,
        "Platform-Tenant": session.tenant,
      },
    });
    if (!res.ok) throw new Error(`usage read failed: ${res.status}`);
    return await res.json() as Api.TenantUsage;
  });

  const recordMetric = usage.metrics.find((m) => m.kind === "records");
  expect(recordMetric).toBeTruthy();
  const topSource = recordMetric!.sources[0];
  expect(topSource).toBeTruthy();

  await open(page, "Admin", "Settings");
  await expect(page.getByRole("heading", { level: 1, name: "Settings", exact: true })).toBeVisible();
  await expect(page.getByRole("heading", { level: 2, name: "Usage & quotas", exact: true })).toBeVisible();
  await expect(page.getByText("Starter", { exact: true })).toBeVisible();

  for (const label of ["Active records", "Storage", "Daily writes", "AI tokens", "Installed packages"]) {
    await expect(page.getByRole("progressbar", { name: label, exact: true })).toBeVisible();
  }
  await expect(page.getByText(`Month ${usage.month}`)).toBeVisible();

  await page.getByRole("button", { name: /^Active records/ }).click();
  const sourceLink = page.getByRole("link", { name: topSource.title, exact: true });
  await expect(sourceLink).toBeVisible();
  await sourceLink.click();
  await expect(page).not.toHaveURL(/\/admin\/settings$/);

  await open(page, "Admin", "Settings");
  await page.getByRole("main").getByRole("button", { name: "Agent Studio", exact: true }).click();
  await expect(page.getByRole("heading", { level: 2, name: "Agent Studio", exact: true })).toBeVisible();
});
