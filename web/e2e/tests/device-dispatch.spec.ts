import { once } from "node:events";
import { createServer } from "node:http";
import { expect, fresh, shots, slug, test } from "./kit";

test.use({ actionTimeout: 15_000 });

test("a declared device command is read back without blind resend and a stuck command is resolved through work", async ({ page, builder, operator }, info) => {
  test.setTimeout(120_000);
  const states = new Map<string, { state: string; reason: string }>();
  const sent: string[] = [];
  const controller = createServer(async (request, response) => {
    for await (const _part of request) { /* Consume the frozen command request. */ }
    if (request.method === "POST") {
      sent.push(String(request.headers["idempotency-key"]));
      request.socket.destroy();
      return;
    }
    response.setHeader("Content-Type", "application/json");
    response.end(JSON.stringify([...states].map(([id, value]) => ({ id, ...value }))));
  });
  controller.listen(0, "127.0.0.1");
  await once(controller, "listening");
  try {
    const address = controller.address();
    if (!address || typeof address === "string") throw Error("Device controller did not start");
    const name = slug("dispatch"), type = `build.${name}`;
    const object = await builder.object({
      name, title: "Device dispatch requests",
      fields: [{ name: "status", title: "Status", type: "text" }],
      states: [{ name: "open", title: "Open" }],
      access: [{ role: "user", read: "all", create: true, edit: true }],
      actions: [{
        name: "start", title: "Send device command", from: ["open"], roles: ["user", "builder"],
        transaction: { subject: type, steps: [{ name: "mark", kind: "update", target: { type, id: "$subject" }, changes: [{ field: "status", from: "=waiting" }] }] },
      }],
    });
    const connection = fresh("CONNECTION"), writeback = fresh("WRITEBACK"), source = fresh("SOURCE");
    await builder.decide("build.connection.create", { type: "build.connection", id: connection }, { name: slug("controller"), title: "External device controller", kind: "http", address: `http://127.0.0.1:${address.port}/`, allowPrivate: true });
    await builder.decide("build.connection.check", { type: "build.connection", id: connection }, {});
    await expect.poll(async () => (await builder.record("build.connection", connection)).state, { timeout: 20_000 }).toBe("ready");
    await builder.decide("build.writeback.create", { type: "build.writeback", id: writeback }, { name: slug("command"), title: "Declared device command", connection, object: type, on: "dispatch", path: "commands" });
    await builder.decide("build.writeback.publish", { type: "build.writeback", id: writeback }, {});
    await builder.decide("build.source.create", { type: "build.source", id: source }, { name: slug("readback"), title: "Device status readback", connection, profile: "json", url: "rows", object: type, key: "id", mapping: [{ from: "state", to: "status" }] });
    await builder.decide("build.source.publish", { type: "build.source", id: source }, {});

    await builder.open(page, `/action-type?id=${object.id}&action=start`);
    await page.getByRole("region", { name: "Action structure", exact: true }).getByRole("button", { name: /^Transaction(?: \d+)?$/ }).click();
    await page.getByRole("checkbox", { name: "Dispatch a declared command after this transaction", exact: true }).check();
    await page.getByRole("button", { name: "Add command declaration", exact: true }).click();
    await page.getByLabel("Command name", { exact: true }).fill("move");
    await page.getByLabel("Command title", { exact: true }).fill("Move to target");
    await page.getByRole("combobox", { name: "Dispatch writeback", exact: true }).selectOption(writeback);
    await page.getByRole("combobox", { name: "Declared command", exact: true }).selectOption("move");
    await page.getByRole("combobox", { name: "Dispatch target", exact: true }).selectOption(connection);
    await page.getByRole("combobox", { name: "Published data source", exact: true }).selectOption(source);
    await page.getByLabel("Readback period (seconds)", { exact: true }).fill("1");
    await page.getByLabel("Reason field (optional)", { exact: true }).fill("reason");
    await page.getByRole("combobox", { name: "Dispatch reviewer role", exact: true }).selectOption("builder");
    await page.getByRole("button", { name: "Save", exact: true }).click();
    await expect.poll(async () => (await builder.record("build.object", object.id)).actions[0].transaction?.dispatch?.command).toBe("move");
    await shots(page, page.locator("body"), "device-dispatch-editor", info);
    const application = await builder.application({ title: "Device dispatch operations", resources: [{ app: "build", kind: "object", name: type }] });
    const candidate = await builder.candidate([{ kind: "object", id: object.id }, { kind: "app", id: application.id }]);
    await builder.activate(candidate);

    const first = fresh("DEVICE");
    await operator.decide(`${type}.create`, { type, id: first }, { status: "new" });
    await operator.decide(`${type}.start`, { type, id: first }, {});
    await operator.open(page, `/record?type=${type}&id=${first}`);
    const progress = page.getByRole("region", { name: "Device dispatch progress", exact: true });
    await expect(progress).toBeVisible();
    await expect(progress.locator('[data-ui="tag"]').filter({ hasText: /^Outcome unknown$/ })).toBeVisible({ timeout: 20_000 });
    await shots(page, progress, "device-dispatch-unknown", info);
    states.set(first, { state: "running", reason: "Controller reports movement in progress" });
    await expect(progress.locator('[data-ui="tag"]').filter({ hasText: /^In transit$/ })).toBeVisible({ timeout: 20_000 });
    states.set(first, { state: "completed", reason: "Controller confirms arrival" });
    await expect(progress.locator('[data-ui="tag"]').filter({ hasText: /^Completed$/ })).toBeVisible({ timeout: 20_000 });
    const runs = await builder.read<{ runs: { kind: string; subject: string; state: string }[] }>(`applications/build/${application.name}/runs?kind=dispatch`);
    expect(runs.runs).toEqual(expect.arrayContaining([expect.objectContaining({ kind: "dispatch", subject: `${type}/${first}`, state: "completed" })]));
    expect(sent).toHaveLength(1);

    const second = fresh("DEVICE");
    states.set(second, { state: "stuck", reason: "Controller requires an operator" });
    await operator.decide(`${type}.create`, { type, id: second }, { status: "new" });
    await operator.decide(`${type}.start`, { type, id: second }, {});
    await builder.open(page, `/record?type=${type}&id=${second}`);
    await expect(progress.locator('[data-ui="tag"]').filter({ hasText: /^Stuck$/ })).toBeVisible({ timeout: 20_000 });
    await expect(progress.getByRole("paragraph").filter({ hasText: /^Controller requires an operator$/ })).toBeVisible();
    await shots(page, progress, "device-dispatch-stuck", info);
    await builder.open(page, "/inbox");
    const task = page.getByRole("listitem").filter({ hasText: "Move to target" });
    await expect(task).toBeVisible({ timeout: 20_000 });
    await task.getByRole("button", { name: "Review dispatch", exact: true }).click();
    await page.getByRole("combobox", { name: "Dispatch decision", exact: true }).selectOption("complete");
    await page.getByRole("textbox", { name: "Decision evidence", exact: true }).fill("The on-site operator checked the destination and confirmed completion.");
    await page.getByRole("button", { name: "Record dispatch decision", exact: true }).click();
    await expect(progress.locator('[data-ui="tag"]').filter({ hasText: /^Completed$/ })).toBeVisible();
    await shots(page, page.locator("body"), "device-dispatch-work-completed", info);
    expect(sent).toHaveLength(2);
    expect(new Set(sent).size).toBe(2);
  } finally {
    controller.closeAllConnections();
    await new Promise<void>((resolve, reject) => controller.close(error => error ? reject(error) : resolve()));
  }
});
