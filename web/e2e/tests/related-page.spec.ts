import { expect, test } from "@playwright/test";
import { decide, fresh, open } from "./host";

test("compose related records and act on a selected line without losing its receipt", async ({ page, request }, testInfo) => {
  const suffix = fresh("probe").replace(/[^a-z0-9]/gi, "").toLowerCase();
  const receiptName = `inbound${suffix}`, lineName = `line${suffix}`, pageName = `receiving${suffix}`;
  const receiptType = `build.${receiptName}`, lineType = `build.${lineName}`;
  const receiptObject = fresh("OBJ"), lineObject = fresh("OBJ"), pageID = fresh("PAGE");
  const receiptA = fresh("REC"), receiptB = fresh("REC"), receiptOther = fresh("REC");
  const lineA = fresh("LINE"), lineB = fresh("LINE");
  const builder = "manager", operator = "desk";
  const headers = { Authorization: `Bearer ${operator}` };
  const operatorIdentity = await (await request.get("/v1/me", { headers })).json();

  // These are controlled definitions in the existing builder, not a WMS app.
  await decide(request, builder, "build", "build.object.create", { type: "build.object", id: receiptObject }, {
    name: receiptName, title: "Inbound receipt", plural: "Inbound receipts",
    fields: [
      { name: "number", title: "Receipt number", type: "text", required: true, search: true },
      { name: "warehouse", title: "Warehouse", type: "choice", choices: "north,south", required: true },
    ],
  });
  await decide(request, builder, "build", "build.object.publish", { type: "build.object", id: receiptObject }, {});
  await decide(request, builder, "build", "build.object.create", { type: "build.object", id: lineObject }, {
    name: lineName, title: "Inbound line", plural: "Inbound lines",
    fields: [
      { name: "receipt", title: "Receipt", type: "reference", ref: receiptType, inverse: "lines", required: true },
      { name: "sku", title: "SKU", type: "text", required: true, search: true },
      { name: "quantity", title: "Quantity", type: "integer", required: true },
      { name: "checkedby", title: "Checked by", type: "text" },
    ],
    states: [{ name: "waiting", title: "Waiting", tone: "info" }, { name: "checked", title: "Checked", tone: "success" }],
    actions: [{ name: "check", title: "Check line", from: ["waiting"], to: "checked", sets: [{ field: "checkedby", from: "$me" }] }],
  });
  await decide(request, builder, "build", "build.object.publish", { type: "build.object", id: lineObject }, {});
  for (const [id, number, warehouse] of [[receiptA, "IN-001", "north"], [receiptB, "IN-002", "north"], [receiptOther, "IN-003", "south"]]) {
    await decide(request, operator, "build", `${receiptType}.create`, { type: receiptType, id }, { number, warehouse });
  }
  for (const [id, receipt, sku, quantity] of [[lineA, receiptA, "SKU-A", 6], [lineB, receiptB, "SKU-B", 3]] as const) {
    await decide(request, operator, "build", `${lineType}.create`, { type: lineType, id }, { receipt, sku, quantity });
  }
  await decide(request, builder, "build", "build.page.create", { type: "build.page", id: pageID }, {
    name: pageName, title: "Inbound receiving probe", object: receiptType,
    sections: [
      { widget: "table", title: "Inbound receipts", fields: ["number", "warehouse"] },
      { widget: "filter", title: "Warehouse filter", fields: ["warehouse"] },
    ],
  });

  await open(page, builder, `/compose?id=${pageID}`);
  const layout = page.getByRole("region", { name: "Widgets and layout", exact: true });
  const inspector = page.getByRole("region", { name: "The widget in hand", exact: true });
  await layout.getByRole("button", { name: "Detail", exact: true }).click();
  await inspector.getByRole("textbox", { name: "Title", exact: true }).fill("Selected receipt");
  await layout.getByRole("button", { name: "Table", exact: true }).click();
  await inspector.getByRole("combobox", { name: "Object", exact: true }).selectOption(lineType);
  await inspector.getByRole("combobox", { name: "Through", exact: true }).selectOption("lines");
  await inspector.getByRole("textbox", { name: "Title", exact: true }).fill("Receipt lines");
  for (const field of ["SKU", "Quantity", "State"]) {
    await inspector.getByRole("group", { name: "Fields it shows" }).getByRole("button", { name: field, exact: true }).click();
  }
  await layout.getByRole("button", { name: "Detail", exact: true }).click();
  await inspector.getByRole("combobox", { name: "Object", exact: true }).selectOption(lineType);
  await inspector.getByRole("textbox", { name: "Title", exact: true }).fill("Selected line");
  for (const field of ["SKU", "Quantity", "State", "Checked by"]) {
    await inspector.getByRole("group", { name: "Fields it shows" }).getByRole("button", { name: field, exact: true }).click();
  }
  await layout.getByRole("button", { name: "Actions", exact: true }).click();
  await inspector.getByRole("combobox", { name: "Object", exact: true }).selectOption(lineType);
  await inspector.getByRole("textbox", { name: "Title", exact: true }).fill("Line actions");
  await inspector.getByRole("group", { name: "Actions it offers" }).getByRole("button", { name: "Check line", exact: true }).click();
  await layout.getByRole("button", { name: "Form", exact: true }).click();
  await inspector.getByRole("combobox", { name: "Object", exact: true }).selectOption(lineType);
  await inspector.getByRole("combobox", { name: "Through", exact: true }).selectOption("lines");
  await inspector.getByRole("textbox", { name: "Title", exact: true }).fill("Add receipt line");
  for (const field of ["SKU *", "Quantity *"]) {
    await inspector.getByRole("group", { name: "Fields it asks for" }).getByRole("button", { name: field, exact: true }).click();
  }
  await page.getByRole("button", { name: "Direct install", exact: true }).click();
  await expect(page.getByText("The page is in the workspace.", { exact: true })).toBeVisible();
  const saved = await (await request.get(`/v1/records/build.page/${pageID}`, { headers: { Authorization: `Bearer ${builder}` } })).json();
  expect(saved.record.sections).toEqual(expect.arrayContaining([
    expect.objectContaining({ widget: "table", object: lineType, relation: "lines" }),
    expect.objectContaining({ widget: "detail", object: lineType }),
    expect.objectContaining({ widget: "actions", object: lineType, actions: [`${lineType}.check`] }),
    expect.objectContaining({ widget: "form", object: lineType, relation: "lines", fields: ["sku", "quantity"] }),
  ]));

  const operatorPage = await page.context().newPage();
  await open(operatorPage, operator, `/page?app=build&kind=page&name=${pageName}`);
  const section = (title: string) => operatorPage.getByRole("heading", { name: title, exact: true }).locator("..");
  const receipts = section("Inbound receipts"), lines = section("Receipt lines");
  const receiptDetail = section("Selected receipt"), lineDetail = section("Selected line"), lineActions = section("Line actions");
  const lineForm = section("Add receipt line");
  await expect(lineForm.getByText("Select a parent record before creating a related record.", { exact: true })).toBeVisible();
  await expect(lineForm.getByRole("button", { name: "Create", exact: true })).toHaveCount(0);
  await operatorPage.getByRole("search", { name: "Warehouse filter", exact: true }).getByLabel("Warehouse", { exact: true }).selectOption("north");
  await expect(receipts.getByRole("row").filter({ hasText: "IN-003" })).toHaveCount(0);
  await receipts.getByRole("row").filter({ hasText: "IN-001" }).click();
  await expect(lines.getByRole("row").filter({ hasText: "SKU-A" })).toBeVisible();
  await expect(lines.getByRole("row").filter({ hasText: "SKU-B" })).toHaveCount(0);
  await lines.getByRole("row").filter({ hasText: "SKU-A" }).click();
  await expect(lineDetail.getByRole("heading", { name: "SKU-A", exact: true })).toBeVisible();
  await expect(receiptDetail.getByRole("heading", { name: "IN-001", exact: true })).toBeVisible();
  await expect(receiptDetail.getByRole("heading", { name: "History", exact: true })).toHaveCount(0);
  await expect(lineForm.getByRole("definition").filter({ hasText: /^IN-001$/ })).toBeVisible();
  await expect(lineForm.getByRole("combobox", { name: /Receipt/ })).toHaveCount(0);
  await lineForm.getByLabel("SKU *", { exact: true }).fill("STALE-LINE");
  await lineForm.getByLabel("Quantity *", { exact: true }).fill("4");
  await lineActions.getByRole("button", { name: "Check line", exact: true }).click();
  await expect.poll(async () => (await (await request.get(`/v1/records/${lineType}/${lineA}`, { headers })).json()).record).toMatchObject({
    receipt: receiptA, sku: "SKU-A", quantity: 6, state: "checked", checkedby: operatorIdentity.principalId,
  });
  await expect(lines.getByRole("row").filter({ hasText: "SKU-A" }).getByText("Checked", { exact: true })).toBeVisible();
  await expect(lineDetail.getByRole("definition").filter({ hasText: /^Checked$/ })).toBeVisible();
  if (process.env.PLATFORM_SCREENSHOTS) await operatorPage.screenshot({ path: testInfo.outputPath("related-receipt-workspace.png"), fullPage: true });

  // Switching the master must clear child context even when the filter stays unchanged.
  await receipts.getByRole("row").filter({ hasText: "IN-002" }).click();
  await expect(receiptDetail.getByRole("heading", { name: "IN-002", exact: true })).toBeVisible();
  await expect(lineDetail.getByText("SKU-A", { exact: true })).toHaveCount(0);
  await expect(lineDetail.getByText("Select a record to see it here.", { exact: true })).toBeVisible();
  await expect(lineActions.getByRole("button", { name: "Check line", exact: true })).toHaveCount(0);
  await expect(lines.getByRole("row").filter({ hasText: "SKU-A" })).toHaveCount(0);
  await expect(lines.getByRole("row").filter({ hasText: "SKU-B" })).toBeVisible();
  await expect(lineForm.getByLabel("SKU *", { exact: true })).toHaveValue("");
  await expect(lineForm.getByLabel("Quantity *", { exact: true })).toHaveValue("");
  await expect(lineForm.getByRole("definition").filter({ hasText: /^IN-002$/ })).toBeVisible();
  await lineForm.getByLabel("SKU *", { exact: true }).fill("SKU-C");
  await lineForm.getByLabel("Quantity *", { exact: true }).fill("8");
  await lineForm.getByRole("button", { name: "Create", exact: true }).click();
  await expect(lines.getByRole("row").filter({ hasText: "SKU-C" })).toBeVisible();
  const created = await (await request.get(`/v1/records/${lineType}?limit=50`, { headers })).json();
  expect(created.records.filter((record: { sku: string }) => record.sku === "SKU-C")).toEqual([
    expect.objectContaining({ receipt: receiptB, quantity: 8, state: "waiting" }),
  ]);
  await expect(lineForm.getByLabel("SKU *", { exact: true })).toHaveValue("");
  if (process.env.PLATFORM_SCREENSHOTS) await operatorPage.screenshot({ path: testInfo.outputPath("related-form-workspace.png"), fullPage: true });
  const other = await (await request.get(`/v1/records/${lineType}/${lineB}`, { headers })).json();
  expect(other.record).toMatchObject({ receipt: receiptB, state: "waiting" });
});
