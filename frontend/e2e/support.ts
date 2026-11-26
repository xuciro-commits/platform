import { type Locator, type Page, expect, test } from "@playwright/test";

const sessions = new Map<string, string>();
export async function signIn(page: Page, seat = "admin-1 · acceptance") {
  const signedIn = page.getByRole("button", { name: /^(Account|Open navigation)$/ });
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


export async function crmMainline(page: Page, go: (type: string, plural: string) => Promise<unknown>, person: string, company: string) {
  await go("crm.lead", "Leads");
  await page.getByRole("button", { name: "New", exact: true }).click();
  const lead = page.getByRole("dialog");
  await lead.getByRole("textbox", { name: "Full name" }).fill(person);
  await lead.getByRole("combobox", { name: "Status" }).click();
  await page.getByRole("option", { name: "New", exact: true }).click();
  await lead.getByRole("button", { name: "Create", exact: true }).click();
  await expect(page.getByRole("heading", { level: 1, name: person, exact: true })).toBeVisible();
  const leadPage = page.url();
  await edit(page, "Organization");
  const organization = page.getByRole("textbox", { name: "Organization", exact: true });
  await organization.fill(company);
  await organization.press("Enter");
  await expectRevision(page, 2);
  await edit(page, "Annual revenue");
  const revenue = page.getByRole("spinbutton", { name: "Annual revenue", exact: true });
  await revenue.fill("250000");
  await revenue.press("Enter");
  await expectRevision(page, 3);
  await page.reload();
  await expect(property(page, "Annual revenue")).toContainText("250,000.00");

  for (const [type, fill] of [
    ["crm.note", async (form: ReturnType<Page["getByRole"]>) => { await form.getByRole("textbox", { name: "Title" }).fill(`${company} discovery call`); }],
    ["crm.task", async (form: ReturnType<Page["getByRole"]>) => {
      await form.getByRole("textbox", { name: "Title" }).fill(`Send ${company} the pilot plan`);
      await form.getByRole("combobox", { name: "Status" }).click();
      await page.getByRole("option", { name: "Todo", exact: true }).click();
    }],
  ] as const) {
    await go(type, type === "crm.note" ? "Notes" : "Tasks");
    await page.getByRole("button", { name: "New", exact: true }).click();
    const form = page.getByRole("dialog");
    await fill(form);
    await form.getByRole("button", { name: "Create", exact: true }).click();
    await expect(page.getByRole("dialog")).toBeHidden();
    await page.getByRole("combobox", { name: "Lead", exact: true }).fill(person.split(" ")[0]);
    await page.getByRole("option", { name: new RegExp(person) }).click();
    await expectRevision(page, 2);
  }

  await page.goto(leadPage);
  await page.getByRole("button", { name: "Convert to deal", exact: true }).click();
  const convert = page.getByRole("dialog", { name: "Convert to deal", exact: true });
  await expect(convert.getByText("Leave empty to create a new organization from the lead.", { exact: true })).toBeVisible();
  await convert.getByRole("combobox", { name: "Link to organization" }).click();
  await expect(page.getByRole("option").first()).toBeVisible();
  await convert.getByRole("heading", { name: "Convert to deal", exact: true }).click();
  await expect(page.getByRole("option")).toHaveCount(0);
  await convert.getByRole("button", { name: "Convert to deal", exact: true }).click();
  await expect(convert).toBeHidden();
  await expect(page).toHaveURL(/\/(crm\.)?deal\/CRM-DEAL-\d{4}-\d{5}$/);
  await expect(page.getByRole("heading", { level: 1, name: company, exact: true })).toBeVisible();
  const dealPage = new URL(page.url()).pathname;
  const organizations = dealPage.replace(/(crm\.)?deal\/[^/]+$/, "$1organization/");
  await page.getByRole("main").getByRole("link", { name: company, exact: true }).last().click();
  await page.getByRole("dialog", { name: company }).getByRole("link", { name: "Open full page", exact: true }).click();
  await expect(page).toHaveURL((url) => url.pathname.startsWith(organizations) && /CRM-ORG-\d{4}-\d{5}$/.test(url.pathname));
  const organizationId = new URL(page.url()).pathname.split("/").pop();
  await page.getByRole("main").getByRole("link", { name: company, exact: true }).first().click();
  await expect(page).toHaveURL((url) => url.pathname === dealPage);
  await page.goto(leadPage);
  await expect(page.getByRole("link", { name: company, exact: true }).first()).toBeVisible();
  await expect(page.getByRole("button", { name: "Convert to deal", exact: true })).toHaveCount(0);

  await go("crm.deal", "Deals");
  const closeFilter = page.getByRole("combobox", { name: "Expected close filter", exact: true }).filter({ visible: true });
  if (!(await closeFilter.count())) await page.getByRole("button", { name: /more filters?$/ }).click();
  await closeFilter.first().click();
  await page.getByRole("radio", { name: "Next 30 days", exact: true }).click();
  await expect(page).toHaveURL(/expectedclose.*after.*expectedclose.*before/);
  await page.getByText("Clear all", { exact: true }).click();
  await expect.poll(() => JSON.parse(new URL(page.url()).searchParams.get("view") ?? "{}").filters).toEqual([]);
  await page.getByRole("radio", { name: "Board" }).click();
  const card = page.getByRole("region", { name: "Qualification", exact: true }).getByRole("heading", { level: 4, name: company, exact: true });
  await expect(card).toBeVisible();
  const won = page.getByRole("region", { name: "Won", exact: true });
  const from = (await card.boundingBox())!;
  const to = (await won.boundingBox())!;
  await page.mouse.move(from.x + 2, from.y - 4);
  await page.mouse.down();
  await page.mouse.move(from.x + 40, from.y + 20, { steps: 4 });
  await page.mouse.move(to.x + to.width / 2, to.y + to.height / 2, { steps: 8 });
  await page.mouse.up();
  await page.reload();
  const wonCard = page.getByRole("region", { name: "Won", exact: true }).getByRole("heading", { level: 4, name: company, exact: true });
  await expect(wonCard).toBeVisible();
  await page.goto(dealPage);
  await expect(property(page, "Probability %")).toHaveText("100");
  await edit(page, "Status");
  await page.getByRole("combobox", { name: "Status" }).fill("Negotiation");
  const refusedEdit = page.waitForResponse((response) => response.url().endsWith("/v1/submissions") && response.status() >= 400);
  await page.getByRole("option", { name: "Negotiation", exact: true }).click();
  await refusedEdit;
  await page.getByRole("combobox", { name: "Status" }).press("Escape");
  await expect(page.getByRole("combobox", { name: "Status" })).toHaveCount(0);
  await page.getByRole("button", { name: "Reopen", exact: true }).click();
  await expect(property(page, "Probability %")).toHaveText("10");

  await page.setViewportSize({ width: 2600, height: 900 });
  await go("crm.deal", "Deals");
  await page.getByRole("radio", { name: "Board" }).click();
  const reopened = (await page.getByRole("region", { name: "Qualification", exact: true }).getByRole("heading", { level: 4, name: company, exact: true }).boundingBox())!;
  const lost = page.getByRole("region", { name: "Lost", exact: true });
  const lostBox = (await lost.boundingBox())!;
  await page.mouse.move(reopened.x + 2, reopened.y - 4);
  await page.mouse.down();
  await page.mouse.move(reopened.x + 40, reopened.y + 20, { steps: 4 });
  await page.mouse.move(lostBox.x + lostBox.width / 2, lostBox.y + lostBox.height / 2, { steps: 8 });
  await page.mouse.up();
  const closing = page.getByRole("dialog", { name: "Mark lost", exact: true });
  await expect(closing.getByRole("button", { name: "Mark lost", exact: true })).toBeDisabled();
  await closing.getByRole("combobox", { name: "Lost reason" }).click();
  await page.getByRole("option", { name: "价格", exact: true }).click();
  await closing.getByRole("button", { name: "Mark lost", exact: true }).click();
  await expect(closing).toBeHidden();
  await expect(lost.getByRole("heading", { level: 4, name: company, exact: true })).toBeVisible();

  await page.goto(dealPage);
  await expect(property(page, "Probability %")).toHaveText("0");
  await expect(page.getByRole("button", { name: "About Probability %", exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Reopen", exact: true }).click();
  await page.getByRole("alertdialog").getByRole("button", { name: "Reopen", exact: true }).click();
  await expect(property(page, "Probability %")).toHaveText("10");
  await edit(page, "Status");
  await page.getByRole("combobox", { name: "Status" }).fill("Negotiation");
  await page.getByRole("option", { name: "Negotiation", exact: true }).click();
  await page.reload();
  await expect(property(page, "Probability %")).toHaveText("70");
  await expect(property(page, "Lost reason")).toHaveCount(0);

  await go("crm.organization", "Organizations");
  const search = page.getByRole("textbox", { name: "Search this list" });
  await search.fill(company.split(" ")[0]);
  await search.press("Enter");
  await page.getByRole("link", { name: company, exact: true }).first().click();
  await expect(page.getByRole("heading", { level: 1, name: company, exact: true })).toBeVisible();
  await page.getByRole("link", { name: "Links", exact: true }).click();
  await expect(page.getByText(person, { exact: true }).first()).toBeVisible();
  await expect(page.getByRole("link", { name: company, exact: true }).first()).toBeVisible();

  await go("crm.lead", "Leads");
  await page.getByRole("button", { name: "New", exact: true }).click();
  const repeat = page.getByRole("dialog");
  await repeat.getByRole("textbox", { name: "Full name" }).fill(`${person} II`);
  await repeat.getByRole("combobox", { name: "Status" }).click();
  await page.getByRole("option", { name: "New", exact: true }).click();
  await repeat.getByRole("button", { name: "Create", exact: true }).click();
  await expect(page.getByRole("heading", { level: 1, name: `${person} II`, exact: true })).toBeVisible();
  await edit(page, "Organization");
  const same = page.getByRole("textbox", { name: "Organization", exact: true });
  await same.fill(` ${company.toUpperCase()}  `);
  await same.press("Enter");
  await expectRevision(page, 2);
  await page.getByRole("button", { name: "Convert to deal", exact: true }).click();
  const relink = page.getByRole("dialog", { name: "Convert to deal", exact: true });
  await expect(relink.getByRole("button", { name: `Link to organization ${company}`, exact: true })).toBeVisible();
  await relink.getByRole("heading", { name: "Convert to deal", exact: true }).click();
  await relink.getByRole("button", { name: "Convert to deal", exact: true }).click();
  await expect(relink).toBeHidden();
  await page.getByRole("main").getByRole("link", { name: company, exact: true }).last().click();
  await page.getByRole("dialog", { name: company }).getByRole("link", { name: "Open full page", exact: true }).click();
  await expect(page).toHaveURL((url) => url.pathname.split("/").pop() === organizationId);
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
