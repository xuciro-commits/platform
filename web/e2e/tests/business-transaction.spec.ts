import { once } from "node:events";
import { createServer } from "node:http";
import { expect, fresh, shots, slug, test } from "./kit";

test.use({ actionTimeout: 15_000 });

test("a builder edits one transaction and approval commits its records, balance, external request and work together", async ({ page, builder, operator }, info) => {
  test.setTimeout(120_000);
  const requests: { key: string; body: unknown }[] = [];
  const receiver = createServer(async (request, response) => {
    let body = "";
    for await (const part of request) body += part;
    if (request.method === "POST") requests.push({ key: String(request.headers["idempotency-key"]), body: JSON.parse(body) });
    response.setHeader("Content-Type", "application/json");
    response.end("{}");
  });
  receiver.listen(0, "127.0.0.1");
  await once(receiver, "listening");
  try {
    const address = receiver.address();
    if (!address || typeof address === "string") throw Error("Transaction receiver did not start");
    const text = (value: string) => ({ op: "", kind: "text", lit: { value } });
    const balance = await builder.object({
      title: "Transaction balance", plural: "Transaction balances", fields: [{ name: "item", title: "Item", type: "text", required: true }, { name: "quantity", title: "Quantity", type: "decimal" }],
      access: [{ role: "user", read: "all", create: true, edit: true }],
    });
    const name = slug("receiving"), type = `build.${name}`;
    const object = await builder.object({
      name, title: "Receiving request", fields: [{ name: "number", title: "Number", type: "text" }, { name: "note", title: "Note", type: "text" }],
      states: [{ name: "draft", title: "Draft" }, { name: "received", title: "Received" }],
      access: [{ role: "user", read: "all", create: true, edit: true }],
      actions: [{ name: "receive", title: "Receive transaction", from: ["draft"], roles: ["user", "builder"], inputs: [{ name: "amount", title: "Received quantity", type: "decimal", required: true }],
        approval: { pending: "", levels: [{ title: "Receiving supervisor", role: "builder" }] },
        transaction: { subject: type, steps: [
          { name: "number", kind: "sequence", sequence: { name: `${name}.documents`, pattern: "R-{n:4}" } },
          { name: "receive", kind: "update", target: { type, id: "$subject" }, changes: [{ field: "number", from: "step.number" }, { field: "state", from: "=received" }] },
          { name: "stock", kind: "post", target: { type: balance.type }, post: { match: [{ field: "item", expr: text("material-A") }], field: "quantity", amount: { op: "", kind: "decimal", ref: "input.amount" } } },
          { name: "review", kind: "work", work: { title: "Check the received goods", role: "user", body: text("The receiving transaction was accepted.") } },
        ] },
      }],
    });
    const connection = fresh("CONNECTION"), writeback = fresh("WRITEBACK");
    await builder.decide("build.connection.create", { type: "build.connection", id: connection }, { name: slug("warehouse"), title: "External warehouse", kind: "http", address: `http://127.0.0.1:${address.port}/`, allowPrivate: true });
    await builder.decide("build.connection.check", { type: "build.connection", id: connection }, {});
    await expect.poll(async () => (await builder.record("build.connection", connection)).state, { timeout: 20_000 }).toBe("ready");
    await builder.decide("build.writeback.create", { type: "build.writeback", id: writeback }, { name: slug("receipt"), title: "Explicit transaction request", connection, object: type, on: "transaction", path: "receipt" });
    await builder.decide("build.writeback.publish", { type: "build.writeback", id: writeback }, {});
    const draft = await builder.record("build.object", object.id);
    draft.actions[0].transaction.steps.push({ name: "send", kind: "effect", effect: { writeback, payload: [{ field: "quantity", expr: { op: "", kind: "decimal", ref: "input.amount" } }] } });
    await builder.decide("build.object.edit", { type: "build.object", id: object.id }, { actions: draft.actions });
    const related = slug("relateddraft");
    await builder.decide("build.object.create", { type: "build.object", id: fresh("DRAFT") }, {
      name: related, title: "Related draft item", fields: [{ name: "quantity", title: "Draft quantity", type: "decimal" }],
    });
    await builder.open(page, `/action-type?id=${object.id}&action=receive`);
    await page.getByRole("button", { name: "Visual", exact: true }).click();
    await page.getByRole("region", { name: "Action structure", exact: true }).getByRole("button", { name: /^Transaction(?: \d+)?$/ }).click();
    await expect(page.getByRole("combobox", { name: "Transaction writeback", exact: true })).toHaveValue(writeback);
    // Related drafts are editable before any separate publication.
    await page.getByRole("combobox", { name: "New transaction step kind", exact: true }).selectOption("create");
    await page.getByRole("button", { name: "Add transaction step", exact: true }).click();
    const creation = page.getByRole("group", { name: /^Transaction step \d+$/ }).last();
    await creation.getByRole("combobox", { name: "Step object", exact: true }).selectOption(`build.${related}`);
    await creation.getByRole("button", { name: "Add field assignment", exact: true }).click();
    await expect(creation.getByRole("combobox", { name: "Record field", exact: true })).toHaveValue("quantity");
    await creation.getByRole("button", { name: "Remove transaction step", exact: true }).click();
    const steps = page.getByRole("group", { name: /^Transaction step \d+$/ });
    // Renaming the producer edits its typed consumers, while a move keeps
    // invalid references visible until their original order is restored.
    await steps.nth(0).getByRole("textbox", { name: "Step name", exact: true }).fill("document");
    await steps.nth(0).getByRole("button", { name: "Move step down", exact: true }).click();
    await expect(page.getByText('Step "receive" uses "step.document" before that result is available in this scope.', { exact: true }).first()).toBeVisible();
    await expect(page.getByRole("button", { name: "Save", exact: true })).toBeDisabled();
    await steps.nth(1).getByRole("button", { name: "Move step up", exact: true }).click();
    // Unapplied expression text belongs to the object draft, including when
    // its section is unmounted and its step is subsequently reordered.
    const postingAmount = () => page.getByText("Posting amount", { exact: true }).locator("..");
    await postingAmount().getByRole("button", { name: "Expression source", exact: true }).click();
    const invalidExpression = '{ "op":';
    await postingAmount().getByRole("textbox", { name: "Expression source", exact: true }).fill(invalidExpression);
    await postingAmount().getByRole("button", { name: "Apply checked expression", exact: true }).click();
    await expect(page.getByRole("button", { name: "Save", exact: true })).toBeDisabled();
    const actionStructure = page.getByRole("region", { name: "Action structure", exact: true });
    await actionStructure.getByRole("button", { name: "Overview", exact: true }).click();
    await actionStructure.getByRole("button", { name: /^Transaction(?: \d+)?$/ }).click();
    await expect(postingAmount().getByRole("textbox", { name: "Expression source", exact: true })).toHaveValue(invalidExpression);
    await steps.nth(2).getByRole("button", { name: "Move step down", exact: true }).click();
    await expect(postingAmount().getByRole("textbox", { name: "Expression source", exact: true })).toHaveValue(invalidExpression);
    await expect(page.getByRole("button", { name: "Save", exact: true })).toBeDisabled();
    await postingAmount().getByRole("button", { name: "Discard", exact: true }).click();
    await steps.nth(3).getByRole("button", { name: "Move step up", exact: true }).click();
    // The typed editor below is the same expression used for publication.
    await page.getByRole("textbox", { name: "Step name", exact: true }).last().fill("warehouse");
    await page.getByRole("button", { name: "Save", exact: true }).click();
    await expect.poll(async () => (await builder.record("build.object", object.id)).actions[0].transaction.steps.at(-1).name).toBe("warehouse");
    const saved = (await builder.record("build.object", object.id)).actions[0].transaction;
    expect(saved.steps[0].name).toBe("document");
    expect(saved.steps[1].changes[0].field).toBe("number");
    expect(saved.steps[1].changes[0].expr?.ref ?? saved.steps[1].changes[0].from).toBe("step.document");
    await expect(page.getByText("Saved", { exact: true }).first()).toBeVisible();
    await shots(page, page.locator("body"), "business-transaction-editor", info);
    await builder.activate(await builder.candidate(object.id));

    const [record] = await builder.records(type, [{ note: "Receiving one delivery" }]);
    await operator.open(page, `/record?type=${type}&id=${record}`);
    await page.getByRole("button", { name: "Receive transaction", exact: true }).click();
    const form = page.getByRole("dialog");
    await form.getByRole("spinbutton", { name: "Received quantity *", exact: true }).fill("0.1");
    await form.getByRole("button", { name: "Receive transaction", exact: true }).click();
    await expect(form).not.toBeVisible();
    expect((await operator.record(type, record!)).state).toBe("draft");
    expect(requests).toHaveLength(0);
    await builder.open(page, "/inbox");
    const approval = page.getByRole("listitem").filter({ hasText: record! });
    await approval.getByRole("button", { name: "Approve", exact: true }).click();
    await expect.poll(async () => (await operator.record(type, record!)).state).toBe("received");
    expect((await operator.record(type, record!)).number).toBe("R-0001");
    await expect.poll(() => requests.length, { timeout: 20_000 }).toBe(1);
    expect(requests[0]!.key).not.toBe("undefined");
    expect(requests[0]!.body).toMatchObject({ quantity: 0.1 });
    await operator.open(page, "/inbox");
    await expect(page.getByRole("listitem").filter({ hasText: "Check the received goods" })).toBeVisible();
    await operator.open(page, `/explorer?type=${balance.type}`);
    await expect(page.getByRole("cell", { name: "0.10", exact: true })).toBeVisible();
    await shots(page, page.locator("body"), "business-transaction-balance", info);
  } finally {
    receiver.closeAllConnections();
    await new Promise<void>((resolve, reject) => receiver.close(error => error ? reject(error) : resolve()));
  }
});
