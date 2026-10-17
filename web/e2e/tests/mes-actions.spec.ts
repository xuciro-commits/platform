import { Member, expect, fresh, test } from "./kit";

test.use({ baseURL: "http://127.0.0.1:18497" });

test("MES downtime reasons retain original choices and retry an unanswered decision with the same key", async ({ page, request }) => {
  const operator = new Member(request, "operator-l1");
  const start = Date.now() - 1000, startedAt = new Date(start).toISOString();
  const delivered = await request.post("/v1/connectors/states", {
    headers: { Authorization: "Bearer gateway-l1" },
    data: { batchId: fresh("STATES"), resource: "CNC-11", samples: [
      { at: new Date(start - 1000).toISOString(), state: "run" },
      { at: startedAt, state: "down" },
      { at: new Date(start + 1000).toISOString(), state: "run" },
    ] },
  });
  expect(delivered.ok()).toBe(true);
  type Downtime = { id: string; resource: string; start: string; end?: string; reason?: string };
  const original = (await operator.read<Downtime[]>("downtime")).find(event => event.resource === "CNC-11" && Date.parse(event.start) === start)!;
  expect(original).toBeDefined();

  await operator.open(page, "/queue");
  await page.getByRole("button", { name: "Downtime", exact: true }).click();
  await expect(page.getByRole("heading", { name: "Equipment downtime", exact: true })).toBeVisible();
  const row = page.getByRole("row").filter({ has: page.getByRole("cell", { name: "CNC-11", exact: true }) });
  await row.getByRole("button", { name: "Set reason", exact: true }).click();
  const dialog = page.getByRole("dialog", { name: "Reason for CNC-11 stop", exact: true });
  const reason = dialog.getByRole("combobox");
  await expect(reason.locator("option")).toHaveText(["—", "Tool change", "Setup", "Material shortage", "Breakdown", "Quality issue"]);
  await expect(reason).toHaveValue("Tool change");
  await page.screenshot({ path: "../../.build/mes-downtime-desktop.png" });
  await page.setViewportSize({ width: 390, height: 844 });
  await expect(reason).toBeVisible();
  await expect(dialog.getByRole("button", { name: "Save", exact: true })).toBeVisible();
  await page.screenshot({ path: "../../.build/mes-downtime-phone.png" });
  await page.setViewportSize({ width: 1280, height: 720 });
  await reason.selectOption("Setup");
  await dialog.getByRole("button", { name: "Save", exact: true }).click();
  await expect(dialog).toHaveCount(0);
  await expect.poll(async () => (await operator.read<Downtime[]>("downtime")).find(event => event.id === original.id)?.reason).toBe("Setup");
  await row.getByRole("button", { name: "Change", exact: true }).click();
  await expect(reason).toHaveValue("Setup");

  const attempts: { idempotencyKey: string; target: { type: string; id: string }; payload: string; expectedRevision?: number }[] = [];
  const submissions = (url: URL) => url.pathname.endsWith("/v1/submissions");
  await page.route(submissions, async route => {
    const body = route.request().postDataJSON();
    if (body.schema?.name !== "mes.downtime.reason" || body.target?.id !== original.id) return route.continue();
    attempts.push(body);
    if (attempts.length === 1) {
      await route.fulfill({ status: 400, contentType: "application/json", body: JSON.stringify({ error: { code: "ERROR_CODE_INVALID_ARGUMENT", message: "Downtime reason refused" } }) });
    } else if (attempts.length === 2) {
      const accepted = await route.fetch();
      expect(accepted.ok()).toBe(true);
      await route.abort("connectionreset");
    } else await route.continue();
  });
  await reason.selectOption("Breakdown");
  await dialog.getByRole("button", { name: "Save", exact: true }).click();
  await expect(dialog.getByRole("alert").filter({ hasText: "Downtime reason refused" })).toBeVisible();
  await expect(reason).toHaveValue("Breakdown");
  expect((await operator.read<Downtime[]>("downtime")).find(event => event.id === original.id)?.reason).toBe("Setup");

  await dialog.getByRole("button", { name: "Cancel", exact: true }).click();
  await row.getByRole("button", { name: "Change", exact: true }).click();
  await expect(reason).toHaveValue("Setup");
  await reason.selectOption("Quality issue");
  await dialog.getByRole("button", { name: "Save", exact: true }).click();
  await expect(dialog.getByRole("button", { name: "Retry confirmation", exact: true })).toBeVisible();
  await expect(reason).toHaveValue("Quality issue");
  await expect(reason).toBeDisabled();
  await expect.poll(async () => (await operator.read<Downtime[]>("downtime")).find(event => event.id === original.id)?.reason).toBe("Quality issue");
  await dialog.getByRole("button", { name: "Retry confirmation", exact: true }).click();
  await expect(dialog).toHaveCount(0);
  await page.unroute(submissions);
  expect(attempts).toHaveLength(3);
  expect(attempts[1]!.idempotencyKey).not.toBe(attempts[0]!.idempotencyKey);
  expect(attempts[2]).toEqual(attempts[1]);
  for (const attempt of attempts) {
    expect(attempt.target).toEqual({ type: "mes.downtime", id: original.id });
    expect(Object.hasOwn(attempt, "expectedRevision")).toBe(false);
  }
  expect(JSON.parse(Buffer.from(attempts[2]!.payload, "base64").toString())).toEqual({ reason: "Quality issue" });
  const updated = (await operator.read<Downtime[]>("downtime")).filter(event => Date.parse(event.start) === start && event.resource === "CNC-11");
  expect(updated).toHaveLength(1);
  expect(updated[0]).toMatchObject({ id: original.id, start: original.start, end: original.end, reason: "Quality issue" });
});
