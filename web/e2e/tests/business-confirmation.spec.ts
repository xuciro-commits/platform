import { once } from "node:events";
import { createServer } from "node:http";
import { expect, fresh, shots, slug, test } from "./kit";

test("a builder declares ERP and WMS confirmation and source evidence converges an unknown delivery", async ({ page, builder, operator }, info) => {
  test.setTimeout(90_000);
  let sourceState = "pending";
  let recordID = "";
  const sent: string[] = [];
  const receiver = createServer(async (request, response) => {
    for await (const _chunk of request) { /* Consume the declared request. */ }
    if (request.method !== "POST") {
      response.setHeader("Content-Type", "application/json");
      response.end(JSON.stringify(recordID ? [{ id: recordID, state: sourceState }] : []));
      return;
    }
    sent.push(String(request.headers["idempotency-key"]));
    if (request.url === "/wms") {
      request.socket.destroy();
      return;
    }
    response.setHeader("Content-Type", "application/json");
    response.end(JSON.stringify({ state: "confirmed" }));
  });
  receiver.listen(0, "127.0.0.1");
  await once(receiver, "listening");
  try {
    const address = receiver.address();
    if (!address || typeof address === "string") throw Error("confirmation fixture did not listen");
    const name = slug("receipt"), type = `build.${name}`;
    const object = await builder.object({
      name, title: "Receiving confirmation", fields: [{ name: "status", title: "Status", type: "text" }],
      states: [{ name: "open", title: "Open" }],
      access: [{ role: "user", read: "all", create: true, edit: true }],
      actions: [
        { name: "start", title: "Start receiving", from: ["open"], roles: ["user", "builder"], transaction: { subject: type, steps: [{ name: "mark", kind: "update", target: { type, id: "$subject" }, changes: [{ field: "status", from: "=waiting" }] }] } },
        { name: "complete", title: "Complete receiving", from: ["open"], roles: ["user", "builder"], sets: [{ field: "status", from: "=complete" }] },
      ],
    });
    const connectionID = fresh("CONNECTION");
    await builder.decide("build.connection.create", { type: "build.connection", id: connectionID }, { name: slug("receiver"), title: "Confirmation receiver", kind: "http", address: `http://127.0.0.1:${address.port}/`, allowPrivate: true });
    await builder.decide("build.connection.check", { type: "build.connection", id: connectionID }, {});
    await expect.poll(async () => (await builder.record("build.connection", connectionID)).state, { timeout: 20_000 }).toBe("ready");
    const writes = { erp: fresh("WRITEBACK"), wms: fresh("WRITEBACK") };
    for (const party of ["erp", "wms"] as const) {
      await builder.decide("build.writeback.create", { type: "build.writeback", id: writes[party] }, { name: slug(party), title: party.toUpperCase(), connection: connectionID, object: type, on: "confirmation", path: party });
      await builder.decide("build.writeback.publish", { type: "build.writeback", id: writes[party] }, {});
    }
    const sourceID = fresh("SOURCE");
    await builder.decide("build.source.create", { type: "build.source", id: sourceID }, { name: slug("status"), title: "WMS source status", connection: connectionID, profile: "json", url: "rows", object: type, key: "id", mapping: [{ from: "state", to: "status" }] });
    await builder.decide("build.source.publish", { type: "build.source", id: sourceID }, {});
    await builder.open(page, `/action-type?id=${object.id}&action=start`);
    await page.getByRole("region", { name: "Action structure", exact: true }).getByRole("button", { name: /^Transaction(?: \d+)?$/ }).click();
    await page.getByRole("checkbox", { name: "Wait for declared parties", exact: true }).check();
    await page.getByLabel("When all parties confirm", { exact: true }).selectOption("complete");
    for (const party of ["erp", "wms"] as const) {
      await page.getByRole("button", { name: "Add confirmation party", exact: true }).click();
      await page.getByLabel("Party name", { exact: true }).last().fill(party);
      await page.getByLabel("Party title", { exact: true }).last().fill(party.toUpperCase());
      await page.getByLabel("Confirmation writeback", { exact: true }).last().selectOption(writes[party]);
    }
    await page.getByRole("checkbox", { name: "Reconcile by reading the source", exact: true }).last().check();
    await page.getByLabel("Reconciliation source", { exact: true }).selectOption(sourceID);
    await page.getByLabel("Reconciliation period (seconds)", { exact: true }).fill("1");
    await page.getByRole("button", { name: "Save", exact: true }).click();
    await expect.poll(async () => (await builder.record("build.object", object.id)).actions[0].transaction?.confirmation?.parties.length).toBe(2);
    await builder.decide("build.object.publish", { type: "build.object", id: object.id }, {});
    recordID = fresh("RECEIPT");
    await operator.decide(`${type}.create`, { type, id: recordID }, { status: "new" });
    await operator.decide(`${type}.start`, { type, id: recordID }, {});
    await operator.open(page, `/record?type=${type}&id=${recordID}`);
    const progress = page.getByRole("region", { name: "Business confirmation progress", exact: true });
    await expect(progress).toBeVisible();
    await expect(progress.getByLabel("WMS", { exact: true }).getByText("unknown", { exact: true })).toBeVisible({ timeout: 20_000 });
    await expect(progress.getByText("Business deadline", { exact: false })).toBeVisible();
    await expect(progress.getByText(/Waiting \d+ minutes/)).toBeVisible();
    await shots(page, progress, "confirmation-unknown", info);
    sourceState = "confirmed";
    await expect.poll(async () => (await operator.record(type, recordID)).status, { timeout: 20_000 }).toBe("complete");
    await expect(progress.getByText("Matched", { exact: false }).last()).toBeVisible({ timeout: 10_000 });
    expect(new Set(sent).size).toBe(2);
    expect(sent.length).toBe(2);
    await shots(page, progress, "confirmation-converged", info);
  } finally {
    receiver.closeAllConnections();
    await new Promise<void>((resolve, reject) => receiver.close(error => error ? reject(error) : resolve()));
  }
});
