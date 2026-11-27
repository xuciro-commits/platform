import { type Locator, type Page, expect, test } from "@playwright/test";

const sessions = new Map<string, string>();
export async function signIn(page: Page, seat = "admin-1 · acceptance") {
  const signedIn = page.getByRole("button", { name: /^(Account|Open navigation|账户)$/ });
  const cached = sessions.get(seat);
  if (cached) {
    if (!page.url().startsWith("http")) await page.goto("/");
    await page.evaluate((raw) => {
      sessionStorage.clear();
      sessionStorage.setItem("platform:session", raw);
    }, cached);
    await page.goto("/home/overview");
    await expect(signedIn).toBeVisible();
    return;
  }
  const [member, tenant] = seat.split(" · ");
  if (page.url().startsWith("http")) await page.evaluate(() => sessionStorage.clear());
  await page.goto("/");
  await page.getByRole("button", { name: member, exact: true }).click();
  await expect(signedIn).toBeVisible();
  if (tenant) {
    const switched = await page.evaluate((target) => {
      const raw = sessionStorage.getItem("platform:session");
      if (!raw) return false;
      const current = JSON.parse(raw) as { token: string; tenant: string };
      if (current.tenant === target) return false;
      sessionStorage.setItem("platform:session", JSON.stringify({ ...current, tenant: target }));
      return true;
    }, tenant);
    if (switched) {
      await page.goto("/home/overview");
      await expect(signedIn).toBeVisible();
    }
  }
  const raw = await page.evaluate(() => sessionStorage.getItem("platform:session"));
  if (raw) sessions.set(seat, raw);
}

export async function open(page: Page, app: string, entry: string) {
  await page.getByRole("link", { name: app, exact: true }).first().click();
  await page.getByRole("navigation", { name: "Primary navigation" }).getByRole("link", { name: entry, exact: true }).click();
}

export const property = (scope: Page | Locator, name: string) => scope.getByRole("group", { name, exact: true });
export const metadata = (scope: Page | Locator, name: string) => scope.getByRole("term").filter({ hasText: new RegExp(`^${name}$`) }).locator("xpath=following-sibling::*[1]");
export async function expectRevision(page: Page, expected: number) {
  await expect.poll(async () => page.evaluate(async () => {
    const raw = sessionStorage.getItem("platform:session");
    if (!raw) return -1;
    const session = JSON.parse(raw) as { token: string; tenant: string };
    const parts = location.pathname.split("/").filter(Boolean);
    const [type, id] = parts[0] === "ontology" && parts[1] === "objects" ? [parts[2], parts[3]] : [parts[1], parts[2]];
    if (!type || !id) return -1;
    const response = await fetch(`/v1/records/${encodeURIComponent(decodeURIComponent(type))}/${encodeURIComponent(decodeURIComponent(id))}`, {
      headers: { Authorization: `Bearer ${session.token}`, "Platform-Tenant": session.tenant },
    });
    if (!response.ok) return -1;
    const data = await response.json() as { record?: { revision?: number } };
    return data.record?.revision ?? -1;
  })).toBe(expected);
}

export async function edit(scope: Page | Locator, name: string) {
  const button = scope.getByRole("button", { name: `Edit ${name}`, exact: true });
  const more = scope.getByRole("button", { name: /^Show empty fields/ });
  const control = scope.getByRole("textbox", { name, exact: true }).or(scope.getByRole("combobox", { name, exact: true })).or(scope.getByRole("spinbutton", { name, exact: true }));
  await expect(button.or(more).or(control).first()).toBeVisible();
  if (await control.count()) return control.first().click();
  if (!(await button.count())) await more.click();
  await button.press("Enter");
}


export async function crmMainline(page: Page, go: (type: string) => Promise<unknown>, person: string, company: string) {
  await go("crm.lead");
  await page.getByRole("button", { name: "New", exact: true }).click();
  const lead = page.getByRole("dialog");
  await lead.getByRole("textbox", { name: "Full name" }).fill(person);
  await lead.getByRole("button", { name: "Create", exact: true }).click();
  await expect(page.getByRole("heading", { name: person, exact: true }).first()).toBeVisible();
  const leadPage = page.url(), id = new URL(leadPage).pathname.split("/").at(-1)!;
  await edit(page, "Company");
  await page.getByRole("textbox", { name: "Company", exact: true }).fill(company);
  await page.getByRole("textbox", { name: "Company", exact: true }).press("Enter");
  await expect(property(page, "Company")).toContainText(company);
  await edit(page, "Budget");
  await page.getByRole("spinbutton", { name: "Budget", exact: true }).fill("250000");
  await page.getByRole("spinbutton", { name: "Budget", exact: true }).press("Enter");
  await expect(property(page, "Budget")).toContainText("250,000.00");
  await submit(page, "crm.note.create", "crm.note", "", { title: company + " discovery", lead: id });
  await submit(page, "crm.activity.create", "crm.activity", "", { subject: company + " follow-up", lead: id });
  await page.goto(leadPage);
  await page.getByRole("button", { name: "Convert to deal", exact: true }).click();
  const convert = page.getByRole("dialog", { name: "Convert to deal", exact: true });
  for (const [name, date] of [["Expected close", "December 31, 2026"], ["Next step due", "November 30, 2026"]]) {
    const field = convert.getByRole("combobox", { name, exact: true });
    await field.fill(date); await field.press("Tab");
  }
  await convert.getByRole("textbox", { name: "Next step" }).fill("Send proposal");
  await convert.getByRole("button", { name: "Convert to deal", exact: true }).click();
  await expect(convert).toBeHidden();
  await expect(page).toHaveURL(/deal\/DEAL-\d{4}-\d{5}$/);
  await expect(page.getByRole("heading", { name: company, exact: true }).first()).toBeVisible();
  await expect(property(page, "Amount")).toContainText("250,000.00");
  await expect(page.getByRole("main").getByRole("link", { name: company, exact: true }).first()).toBeVisible();
  await expect(page.getByRole("main").getByRole("link", { name: person, exact: true }).first()).toBeVisible();
  const dealPage = page.url();
  await page.getByRole("button", { name: "Mark won", exact: true }).click();
  const win = page.getByRole("dialog", { name: "Mark won", exact: true });
  await win.getByRole("textbox", { name: "Why we won" }).fill("Requirements matched");
  await win.getByRole("button", { name: "Mark won", exact: true }).click();
  await expect(win).toBeHidden();
  await page.reload();
  await expect(page.getByRole("main").getByText("Won", { exact: true }).first()).toBeVisible();
  await page.goto(leadPage);
  await expect(page.getByRole("button", { name: "Convert to deal", exact: true })).toHaveCount(0);
  await expect(page.getByRole("link", { name: company, exact: true }).first()).toBeVisible();
  await page.goto(dealPage);
  await expect(property(page, "Amount")).toContainText("250,000.00");
}

export async function submit(page: Page, schema: string, targetType: string, targetId: string, payload: Record<string, unknown>, revision?: number) {
  return page.evaluate(async ({ schema, targetType, targetId, payload, revision }) => {
    const session = JSON.parse(sessionStorage.getItem("platform:session")!);
    const headers = { Authorization: `Bearer ${session.token}`, "Platform-Tenant": session.tenant, "Content-Type": "application/json" };
    const [catalog, identity, declarations] = await Promise.all([
      fetch("/v1/actions", { headers }).then((r) => r.json()),
      fetch("/v1/me", { headers }).then((r) => r.json()),
      fetch("/v1/declarations", { headers }).then((r) => r.json()),
    ]);
    const action = catalog.find((entry: { schema: string }) => entry.schema === schema);
    if (!action) throw new Error(`missing action ${schema}`);
    const bytes = new TextEncoder().encode(JSON.stringify(payload));
    const encoded = btoa(String.fromCharCode(...bytes));
    const body: Record<string, unknown> = {
      tenantId: identity.tenantId,
      principalId: identity.principalId,
      authority: declarations.find((entry: { dataClass: string }) => entry.dataClass === targetType)?.authorityId ?? "build",
      target: { type: targetType, id: targetId },
      schema: { name: schema, version: action.schemaVersion.family },
      idempotencyKey: crypto.randomUUID(),
      payload: encoded,
    };
    if (revision !== undefined) body.expectedRevision = revision;
    const response = await fetch("/v1/submissions", {
      method: "POST",
      headers: { ...headers, "Platform-Schema-Version": action.schemaVersion.version },
      body: JSON.stringify(body),
    });
    const answer = await response.json();
    if (!response.ok || answer.error) throw new Error(JSON.stringify(answer));
    return answer;
  }, { schema, targetType, targetId, payload, revision });
}

export async function restartHost(mode: "snapshot" | "replay") {
  const controlPort = 19521 + Number(process.env.TRUNK_PORT_OFFSET ?? 0);
  const response = await fetch(`http://127.0.0.1:${controlPort}/restart`, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ snapshotEvery: mode === "replay" ? 0 : 10000 }),
  });
  const answer = await response.json() as { log?: string; error?: string };
  if (!response.ok || answer.error) throw new Error(answer.error ?? `restart failed: ${response.status}`);
  return answer.log ?? "";
}

export async function restoreTenantBackup(tenant: string) {
  const controlPort = 19521 + Number(process.env.TRUNK_PORT_OFFSET ?? 0);
  const response = await fetch(`http://127.0.0.1:${controlPort}/restore-backup`, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ tenant }),
  });
  const answer = await response.json() as { log?: string; error?: string };
  if (!response.ok || answer.error) throw new Error(answer.error ?? `restore-backup failed: ${response.status}`);
  return answer.log ?? "";
}
