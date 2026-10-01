import { expect, test } from "@playwright/test";
import { decide, fresh, open } from "./host";

test("configure related creation visually and execute it through the original record action", async ({ page, request }, testInfo) => {
  const suffix = fresh("create").replace(/[^a-z0-9]/gi, "").toLowerCase();
  const parent = `build.receipt${suffix}`, child = `build.line${suffix}`;
  const parentID = fresh("OBJ"), childID = fresh("OBJ"), pageID = fresh("PAGE"), receiptID = fresh("REC");
  const builder = { Authorization: "Bearer manager" }, operatorHeaders = { Authorization: "Bearer desk" };
  await decide(request, "manager", "build", "build.object.create", { type: "build.object", id: parentID }, {
    name: `receipt${suffix}`, title: "Receipt", fields: [{ name: "number", title: "Receipt number", type: "text", search: true }],
    states: [{ name: "open", title: "Open" }, { name: "received", title: "Received" }],
    actions: [{ name: "receive", title: "Receive goods", from: ["open"], to: "received",
      inputs: [{ name: "item", title: "Item", type: "text", required: true }, { name: "units", title: "Units", type: "integer", required: true }] }],
  });
  await decide(request, "manager", "build", "build.object.publish", { type: "build.object", id: parentID }, {});
  await decide(request, "manager", "build", "build.object.create", { type: "build.object", id: childID }, {
    name: `line${suffix}`, title: "Receipt line", plural: "Receipt lines",
    fields: [{ name: "receipt", title: "Receipt", type: "reference", ref: parent, inverse: "lines", required: true },
      { name: "sku", title: "SKU", type: "text", required: true }, { name: "quantity", title: "Quantity", type: "integer", required: true }],
  });
  await decide(request, "manager", "build", "build.object.publish", { type: "build.object", id: childID }, {});
  await open(page, "manager", `/process?id=${parentID}`);
  await page.getByRole("region", { name: "States and actions", exact: true }).getByRole("button", { name: "Receive goods", exact: true }).click();
  const properties = page.getByRole("region", { name: "The piece in hand", exact: true });
  await properties.getByRole("button", { name: "Create a related record", exact: true }).click();
  const creates = properties.getByRole("group", { name: "What it creates", exact: true });
  await expect(creates.getByRole("combobox", { name: "Related object", exact: true })).toHaveValue(child);
  await expect(creates.getByRole("combobox", { name: "Parent reference", exact: true })).toHaveValue("receipt");
  await expect(page.getByRole("button", { name: "Review release", exact: true })).toBeDisabled();
  const mappings = creates.getByRole("group", { name: "What it sets", exact: true });
  await mappings.getByRole("combobox", { name: "From", exact: true }).nth(0).selectOption("item");
  await mappings.getByRole("combobox", { name: "From", exact: true }).nth(1).selectOption("item");
  await expect(page.getByRole("alert").filter({ hasText: "Quantity needs integer" })).toBeVisible();
  await mappings.getByRole("combobox", { name: "From", exact: true }).nth(1).selectOption("units");
  await expect(page.getByRole("button", { name: "Review release", exact: true })).toBeEnabled();
  if (process.env.PLATFORM_SCREENSHOTS) await page.screenshot({ path: testInfo.outputPath("related-create-inspector.png") });
  await page.getByRole("button", { name: "Save", exact: true }).click();
  await expect(page.getByRole("button", { name: "Save", exact: true })).toBeDisabled();
  await expect.poll(async () => (await (await request.get(`/v1/records/build.object/${parentID}`, { headers: builder })).json()).record.actions[0].creates)
    .toEqual([{ object: child, via: "receipt", sets: [{ field: "sku", from: "item" }, { field: "quantity", from: "units" }] }]);
  await page.getByRole("button", { name: "Review release", exact: true }).click();
  await page.getByRole("button", { name: "Check draft and dependencies", exact: true }).click();
  await page.getByRole("button", { name: "Save immutable candidate", exact: true }).click();
  await expect(page.getByRole("button", { name: "Activate release", exact: true })).toBeEnabled();
  await page.getByRole("button", { name: "Activate release", exact: true }).click();
  await expect(page.getByRole("status").filter({ hasText: "Release active for operators." })).toBeVisible();

  await decide(request, "manager", "build", "build.page.create", { type: "build.page", id: pageID }, {
    name: `receiving${suffix}`, title: "Receiving desk", object: parent,
    sections: [{ widget: "table", title: "Receipts", fields: ["number", "state"] },
      { widget: "actions", title: "Receiving", actions: [`${parent}.receive`] },
      { widget: "table", title: "Receipt lines", object: child, relation: "lines", fields: ["sku", "quantity"] }],
  });
  await decide(request, "manager", "build", "build.page.publish", { type: "build.page", id: pageID }, {});
  await decide(request, "desk", "build", `${parent}.create`, { type: parent, id: receiptID }, { number: "IN-001" });
  const operator = await page.context().newPage();
  await open(operator, "desk", `/page?app=build&kind=page&name=receiving${suffix}`);
  await operator.getByRole("row").filter({ hasText: "IN-001" }).click();
  await operator.getByRole("button", { name: "Receive goods", exact: true }).click();
  const dialog = operator.getByRole("dialog");
  await dialog.getByRole("textbox", { name: "Item *", exact: true }).fill("SKU-A");
  await dialog.getByRole("spinbutton", { name: "Units *", exact: true }).fill("6");
  await dialog.getByRole("button", { name: "Receive goods", exact: true }).click();
  await expect(operator.getByRole("row").filter({ hasText: "SKU-A" })).toBeVisible();
  const lines = await (await request.get(`/v1/records/${child}`, { headers: operatorHeaders })).json();
  expect(lines.records).toHaveLength(1);
  expect(lines.records[0]).toMatchObject({ receipt: receiptID, sku: "SKU-A", quantity: 6 });
  const receipt = await (await request.get(`/v1/records/${parent}/${receiptID}`, { headers: operatorHeaders })).json();
  expect(receipt.record.state).toBe("received");
});
