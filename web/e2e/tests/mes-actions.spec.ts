import { Builder, Member, expect, fresh, shots, test } from "./kit";
import type * as Api from "../../packages/kernel/src/gen/host";

test.use({ baseURL: "http://127.0.0.1:18497" });

test("MES shop orders move to a published editable page while retaining the original actions", async ({ page, request }, info) => {
  test.setTimeout(120_000);
  const supervisor = new Builder(request, "supervisor");
  const master = await supervisor.read<{ products: { id: string }[] }>("master");
  await supervisor.open(page, "/apps?surface=tenant");
  const card = page.getByRole("region", { name: "Package industry.mes-workspace", exact: true });
  await card.getByRole("combobox", { name: "Exact package version", exact: true }).selectOption("0.1.0");
  await card.getByRole("button", { name: "Install", exact: true }).click();
  await expect(card.getByText("active", { exact: true })).toBeVisible();
  const creating = page.waitForResponse(response => new URL(response.url()).pathname === "/v1/packages/industry.mes-workspace/drafts" && response.request().method() === "POST");
  await card.getByRole("button", { name: "Create application drafts", exact: true }).click();
  const created = await creating;
  expect(created.ok()).toBe(true);
  const drafts = await created.json() as Api.PackageDraftResult;
  const pageDraft = drafts.drafts.find(draft => draft.ref.kind === "page")!;
  expect((await supervisor.definitions<any>()).some(item => item.ref.app === "build" && item.ref.name === "mesworkspace")).toBe(false);
  await supervisor.activate(await supervisor.candidate([{ kind: "page", id: pageDraft.id }, { kind: "app", id: drafts.applicationID }]));
  const installed = await supervisor.definitions<any>();
  expect(installed.find(item => item.ref.app === "build" && item.ref.kind === "app" && item.ref.name === "mesworkspace")?.application.extends).toEqual({ app: "mes", kind: "app", name: "mes" });
  expect(installed.find(item => item.ref.app === "build" && item.ref.kind === "page" && item.ref.name === "mesorders")?.page.object.name).toBe("mes.order");

  const optionReads: string[] = [];
  page.on("request", request => { if (request.url().includes("/v1/apps/mes/reads/")) optionReads.push(new URL(request.url()).pathname); });
  await supervisor.open(page, "/orders");
  await expect(page).toHaveURL(/name=mesorders/);
  await page.getByRole("button", { name: "Release shop order", exact: true }).click();
  const form = page.getByRole("dialog", { name: "Release shop order", exact: true });
  const identity = form.getByRole("textbox", { name: "ID *", exact: true });
  await expect(identity).toHaveValue(/^SO/);
  const product = form.getByRole("combobox", { name: "Product ID *", exact: true });
  const quantity = form.getByRole("spinbutton", { name: "Units to make *", exact: true });
  const lots = form.getByRole("spinbutton", { name: "Number of SFCs (lots), at most the quantity *", exact: true });
  await expect(product).toHaveValue(master.products[0]!.id);
  await expect(quantity).toHaveValue("1");
  await expect(lots).toHaveValue("1");
  await quantity.fill("");
  await lots.fill("3");
  await expect(quantity).toHaveValue("");
  const id = fresh("SO");
  await identity.fill(id);
  await quantity.fill("10");
  await expect(form.getByRole("combobox", { name: "ERP planned order ID", exact: true })).toBeVisible();
  await lots.fill("11");
  await lots.press("Tab");
  await expect(lots).toHaveAttribute("aria-invalid", "true");
  await lots.fill("3");
  const place = form.getByRole("combobox", { name: /^The place in the enterprise model/ });
  await expect(place.locator('option[value="L1"]')).toHaveCount(1);
  await expect(place.locator('option[value="plant-sz"]')).toHaveCount(1);
  await place.selectOption("plant-sz");
  await expect(place).toHaveValue("plant-sz");
  await place.selectOption("L1");
  await shots(page, form, "mes-editable-order-form", info);
  await form.getByRole("button", { name: "Release shop order", exact: true }).click();
  await expect(form).toHaveCount(0);
  const order = await supervisor.record<any>("mes.order", id);
  expect(order).toMatchObject({ product: master.products[0]!.id, quantity: 10, place: "L1" });
  const lotsMade = await Promise.all(order.sfcs.map((sfc: string) => supervisor.record<any>("mes.sfc", sfc)));
  expect(lotsMade.map(sfc => sfc.quantity)).toEqual([4, 3, 3]);
  expect(optionReads).toEqual(expect.arrayContaining(["/v1/apps/mes/reads/products", "/v1/apps/mes/reads/available-planned-orders"]));

  await page.getByRole("button", { name: "Release shop order", exact: true }).click();
  await identity.fill(id);
  await quantity.fill("7");
  await form.getByRole("button", { name: "Release shop order", exact: true }).click();
  await expect(form.getByRole("alert").first()).toBeVisible();
  await expect(identity).toHaveValue(id);
  await expect(quantity).toHaveValue("7");
  await form.getByRole("button", { name: "Cancel", exact: true }).click();

  // The plan stays owned by ERP and is chosen through the MES protocol read.
  const planID = fresh("MO"), draftPlanID = fresh("MO");
  for (const plan of [planID, draftPlanID]) {
    await supervisor.decide("erp.production.create", { type: "erp.production", id: plan }, { product: master.products[0]!.id, quantity: 6 }, "erp");
  }
  await supervisor.decide("erp.production.release", { type: "erp.production", id: planID }, {}, "erp");
  await page.getByRole("button", { name: "Release shop order", exact: true }).click();
  const plans = form.getByRole("combobox", { name: "ERP planned order ID", exact: true });
  await expect(plans.locator(`option[value="${planID}"]`)).toHaveCount(1);
  await expect(plans.locator(`option[value="${draftPlanID}"]`)).toHaveCount(0);
  const associatedID = fresh("SO");
  await identity.fill(associatedID);
  await quantity.fill("6");
  await lots.fill("2");
  await plans.selectOption(planID);
  await form.getByRole("button", { name: "Release shop order", exact: true }).click();
  await expect(form).toHaveCount(0);
  const associated = await supervisor.record<any>("mes.order", associatedID);
  expect(associated).toMatchObject({ planned: planID, quantity: 6, product: master.products[0]!.id });
  expect(associated.sfcs).toHaveLength(2);
  await page.getByRole("button", { name: "Release shop order", exact: true }).click();
  await expect(plans.locator(`option[value="${planID}"]`)).toHaveCount(0);
  await form.getByRole("button", { name: "Cancel", exact: true }).click();
  await expect(supervisor.decide("mes.order.release", { type: "mes.order", id: fresh("SO") }, { product: master.products[0]!.id, quantity: 1, sfcs: 1, planned: planID }, "mes")).rejects.toThrow("ERROR_CODE_INVALID_ARGUMENT");

  await page.getByRole("button", { name: "Release shop order", exact: true }).click();
  await identity.focus();
  await identity.press("Tab");
  await expect(product).toBeFocused();
  await product.press("Tab");
  await expect(quantity).toBeFocused();
  await form.getByRole("button", { name: "Cancel", exact: true }).click();
  await supervisor.decide("platform.profile.update", { type: "platform.profile", id: "sup-1" }, { language: "zh-CN" }, "platform");
  await page.reload();
  await page.getByRole("button", { name: "下达车间订单", exact: true }).click();
  const chinese = page.getByRole("dialog", { name: "下达车间订单", exact: true });
  await expect(chinese.getByRole("combobox", { name: "产品 ID *", exact: true })).toHaveValue(master.products[0]!.id);
  await expect(chinese.getByRole("spinbutton", { name: "生产数量 *", exact: true })).toHaveValue("1");
  await expect(chinese.getByRole("spinbutton", { name: "SFC（批）数量，不超过订单数量 *", exact: true })).toHaveValue("1");
  await shots(page, chinese, "mes-editable-order-form-zh", info);
  await chinese.getByRole("button", { name: "取消", exact: true }).click();
  await supervisor.decide("platform.profile.update", { type: "platform.profile", id: "sup-1" }, { language: "en" }, "platform");
  await page.evaluate(() => localStorage.setItem("platform.language", "en"));

  const operator = new Member(request, "operator-l1");
  await operator.open(page, "/orders");
  await expect(page).toHaveURL(/name=mesorders/);
  await expect(page.getByRole("button", { name: "Release shop order", exact: true })).toHaveCount(0);
  await expect(page.getByRole("table")).toContainText(id);
  await expect(operator.decide("mes.order.release", { type: "mes.order", id: fresh("SO") }, { product: master.products[0]!.id, quantity: 1, sfcs: 1 }, "mes")).rejects.toThrow("ERROR_CODE_POLICY_DENIED");
});

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
