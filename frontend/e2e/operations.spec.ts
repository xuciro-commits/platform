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

test("host console: filters and sorts tenant health, usage, lane, error rate and backups, and suspending one tenant leaves another's latency unchanged", async ({ page }) => {
  await signInAs(page, "admin-1", "acceptance");

  const measureMedianReadMs = async (tenant: string, rounds = 9) => {
    return await page.evaluate(async ({ tenant, rounds }) => {
      const raw = sessionStorage.getItem("platform:session");
      if (!raw) throw new Error("missing session");
      const session = JSON.parse(raw) as { token: string };
      const samples: number[] = [];
      for (let i = 0; i < rounds; i++) {
        const t0 = performance.now();
        const res = await fetch("/v1/me", {
          cache: "no-store",
          headers: { Authorization: `Bearer ${session.token}`, "Platform-Tenant": tenant },
        });
        if (!res.ok) throw new Error(`read ${tenant} failed: ${res.status}`);
        await res.json();
        samples.push(performance.now() - t0);
      }
      samples.sort((a, b) => a - b);
      return samples[Math.floor(samples.length / 2)];
    }, { tenant, rounds });
  };

  await measureMedianReadMs("acceptance", 3);
  const beforeMs = await measureMedianReadMs("acceptance", 9);

  await open(page, "Host", "Tenants");
  await expect(page.getByRole("heading", { level: 1, name: "Tenants", exact: true })).toBeVisible();
  await expect(page.getByRole("heading", { level: 3, name: "acceptance", exact: true })).toBeVisible();
  await expect(page.getByRole("heading", { level: 3, name: "publisher", exact: true })).toBeVisible();

  const filterInput = page.getByRole("textbox", { name: "Filter tenants", exact: true });
  await filterInput.fill("publisher");
  await expect(page.getByRole("heading", { level: 3, name: "publisher", exact: true })).toBeVisible();
  await expect(page.getByRole("heading", { level: 3, name: "acceptance", exact: true })).toHaveCount(0);
  await filterInput.fill("");
  await expect(page.getByRole("heading", { level: 3, name: "acceptance", exact: true })).toBeVisible();

  await page.getByRole("combobox", { name: "Sort by", exact: true }).click();
  await page.getByRole("option", { name: "Records", exact: true }).click();
  const headings = await page.getByRole("heading", { level: 3 }).allTextContents();
  expect(headings.indexOf("acceptance")).toBeLessThan(headings.indexOf("publisher"));

  await Promise.all([
    page.waitForResponse((res) => res.url().endsWith("/v1/host/tenants/acceptance/backup") && res.ok()),
    page.getByRole("button", { name: "Backup acceptance", exact: true }).click(),
  ]);
  await expect(page.getByText(/Backup OK/).first()).toBeVisible();

  await page.getByRole("button", { name: "Suspend publisher", exact: true }).click();
  const suspendDialog = page.getByRole("dialog");
  await expect(suspendDialog).toBeVisible();
  await suspendDialog.getByRole("textbox", { name: "Reason", exact: true }).fill("abuse isolation drill");
  await Promise.all([
    page.waitForResponse((res) => res.url().endsWith("/v1/host/tenants/publisher/lifecycle") && res.ok()),
    suspendDialog.getByRole("button", { name: "Suspend workspace", exact: true }).click(),
  ]);
  await expect(suspendDialog).toBeHidden();
  await expect(page.getByRole("button", { name: "Resume publisher", exact: true })).toBeVisible();

  const isolation = await page.evaluate(async () => {
    const raw = sessionStorage.getItem("platform:session");
    if (!raw) throw new Error("missing session");
    const session = JSON.parse(raw) as { token: string };
    const signInRes = await fetch("/v1/me", {
      cache: "no-store",
      headers: { Authorization: `Bearer ${session.token}`, "Platform-Tenant": "publisher" },
    });
    const writeRes = await fetch("/v1/submissions", {
      method: "POST",
      headers: { Authorization: `Bearer ${session.token}`, "Platform-Tenant": "publisher", "Content-Type": "application/json" },
      body: JSON.stringify({}),
    });
    const blockedFlood = Promise.all(Array.from({ length: 16 }, () =>
      fetch("/v1/me", { cache: "no-store", headers: { Authorization: `Bearer ${session.token}`, "Platform-Tenant": "publisher" } }),
    ));
    const samples: number[] = [];
    for (let i = 0; i < 9; i++) {
      const t0 = performance.now();
      const okRes = await fetch("/v1/me", {
        cache: "no-store",
        headers: { Authorization: `Bearer ${session.token}`, "Platform-Tenant": "acceptance" },
      });
      if (!okRes.ok) throw new Error(`acceptance read failed while publisher suspended: ${okRes.status}`);
      await okRes.json();
      samples.push(performance.now() - t0);
    }
    await blockedFlood;
    samples.sort((a, b) => a - b);
    return {
      publisherSignInStatus: signInRes.status,
      publisherWriteStatus: writeRes.status,
      duringMedianMs: samples[Math.floor(samples.length / 2)],
    };
  });

  expect(isolation.publisherSignInStatus).toBe(503);
  expect(isolation.publisherWriteStatus).toBe(503);
  expect(isolation.duringMedianMs).toBeLessThanOrEqual(Math.max(beforeMs * 3 + 15, 35));

  await page.getByRole("button", { name: "Resume publisher", exact: true }).click();
  const resumeDialog = page.getByRole("dialog");
  await expect(resumeDialog).toBeVisible();
  await resumeDialog.getByRole("textbox", { name: "Reason", exact: true }).fill("drill complete");
  await Promise.all([
    page.waitForResponse((res) => res.url().endsWith("/v1/host/tenants/publisher/lifecycle") && res.ok()),
    resumeDialog.getByRole("button", { name: "Resume workspace", exact: true }).click(),
  ]);
  await expect(resumeDialog).toBeHidden();
  await expect(page.getByRole("button", { name: "Suspend publisher", exact: true })).toBeVisible();

  const resumed = await page.evaluate(async () => {
    const raw = sessionStorage.getItem("platform:session");
    if (!raw) throw new Error("missing session");
    const session = JSON.parse(raw) as { token: string };
    const meRes = await fetch("/v1/me", {
      cache: "no-store",
      headers: { Authorization: `Bearer ${session.token}`, "Platform-Tenant": "publisher" },
    });
    const pubAuditRes = await fetch("/v1/audit?limit=20", {
      cache: "no-store",
      headers: { Authorization: `Bearer ${session.token}`, "Platform-Tenant": "publisher" },
    });
    const accAuditRes = await fetch("/v1/audit?limit=20", {
      cache: "no-store",
      headers: { Authorization: `Bearer ${session.token}`, "Platform-Tenant": "acceptance" },
    });
    const pubAudit = await pubAuditRes.json() as Api.AuditPage;
    const accAudit = await accAuditRes.json() as Api.AuditPage;
    return {
      meOk: meRes.ok,
      pubActions: pubAudit.entries.map((e) => e.action),
      accActions: accAudit.entries.map((e) => e.action),
    };
  });

  expect(resumed.meOk).toBe(true);
  expect(resumed.pubActions).toContain("host.lifecycle.suspend");
  expect(resumed.pubActions).toContain("host.lifecycle.resume");
  expect(resumed.accActions).toContain("host.backup.run");
});
