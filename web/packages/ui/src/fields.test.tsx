import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";
import { DataTable, RecordForm, applyFilters, columnsFor, defineEntity, field, recordSchema } from "./index";

afterEach(cleanup);
Element.prototype.getBoundingClientRect = () => ({ width: 800, height: 280, top: 0, left: 0, right: 800, bottom: 280, x: 0, y: 0, toJSON: () => ({}) });
for (const [key, value] of [["offsetWidth", 800], ["offsetHeight", 280]] as const) {
  Object.defineProperty(HTMLElement.prototype, key, { configurable: true, get: () => value });
}

type Material = { id: string; name: string; kind: string; tags: string[]; price: number; stock: number; supplier?: string; ean?: string };
const material = defineEntity<Material>({
  name: "Material", primary: "id",
  fields: {
    id: field.barcode({ label: "Code", required: true, readOnly: true }),
    name: field.text({ label: "Name", required: true }),
    kind: field.singleSelect({ label: "Kind", options: [{ value: "raw", label: "Raw", tone: "info" }, { value: "part", label: "Part", tone: "success" }] }),
    tags: field.multiSelect({ label: "Tags", options: [{ value: "rohs", label: "RoHS" }, { value: "fragile", label: "Fragile" }] }),
    price: field.currency({ label: "Price", currency: "CNY" }),
    stock: field.number({ label: "Stock", unit: "pcs" }),
    supplier: field.email({ label: "Supplier" }),
    value: field.formula({ label: "Stock value", compute: (m: Material) => m.price * m.stock, as: field.currency({ label: "", currency: "CNY" }) }),
  },
});
const rows: Material[] = [
  { id: "M-1", name: "Aluminium ingot", kind: "raw", tags: ["rohs"], price: 20, stock: 100 },
  { id: "M-2", name: "Valve seat", kind: "part", tags: ["rohs", "fragile"], price: 3.5, stock: 40 },
  { id: "M-3", name: "Gasket", kind: "part", tags: [], price: 0.2, stock: 1000 },
];

test("field types render cells: select tags, currency, units and formulas", () => {
  render(<DataTable data={rows} columns={columnsFor(material)} getRowId={(r) => r.id} height={280} />);
  expect(screen.getAllByText("Part")[0]!.closest("[data-tone]")!.getAttribute("data-tone")).toBe("success");
  expect(screen.getByText("Fragile")).toBeTruthy();
  expect(screen.getByText(/100 pcs/)).toBeTruthy();
  expect(screen.getAllByText(/2,000\.00/).length).toBe(1); // 20 × 100 as stock value
});

test("each field type brings its own filter operators", () => {
  const ids = (filters: Parameters<typeof applyFilters<Material>>[2]) => applyFilters(material, rows, filters).map((r) => r.id);
  expect(ids([{ field: "kind", operator: "is", arg: "part" }])).toEqual(["M-2", "M-3"]);
  expect(ids([{ field: "tags", operator: "has", arg: ["rohs", "fragile"] }])).toEqual(["M-2"]);
  expect(ids([{ field: "stock", operator: "gt", arg: 50 }, { field: "name", operator: "contains", arg: "ingot" }])).toEqual(["M-1"]);
  expect(ids([{ field: "value", operator: "lt", arg: 150 }])).toEqual(["M-2"]);
  expect(ids([{ field: "tags", operator: "empty" }])).toEqual(["M-3"]);
  expect(ids([{ field: "stock", operator: "gt" }])).toEqual(["M-1", "M-2", "M-3"]); // incomplete filters are ignored
});

test("a record validates by its field types", () => {
  const schema = recordSchema(material);
  expect(schema.safeParse({ name: "Bolt", kind: "raw", price: 1, stock: 2 }).success).toBe(true);
  expect(schema.safeParse({ id: "M-9", name: "", kind: "raw" }).success).toBe(false);
  expect(schema.safeParse({ id: "M-9", name: "Bolt", kind: "metal" }).success).toBe(false);
  expect(schema.safeParse({ id: "M-9", name: "Bolt", supplier: "not-an-email" }).success).toBe(false);
  expect(schema.safeParse({ id: "M-9", name: "Bolt", supplier: "" }).success).toBe(true);
});

test("cells edit in place with their field type's editor", () => {
  const edit = vi.fn();
  render(<DataTable data={rows} columns={columnsFor(material)} getRowId={(r) => r.id} height={280} onCellEdit={edit} />);
  const select = vi.spyOn(HTMLInputElement.prototype, "select");
  const stock = screen.getByText(/100 pcs/).closest("[role=cell]")!;
  fireEvent.doubleClick(stock);
  const input = stock.querySelector("input")!;
  expect(select).toHaveBeenCalledTimes(1); // the value is selected: typing replaces it
  fireEvent.change(input, { target: { value: "120" } });
  fireEvent.keyDown(input, { key: "Enter" });
  expect(edit).toHaveBeenCalledWith(rows[0], "stock", 120);
  const value = screen.getAllByText(/2,000\.00/)[0]!.closest("[role=cell]")!;
  fireEvent.doubleClick(value);
  expect(value.querySelector("input")).toBeNull(); // formulas are read-only
  const code = screen.getByText("M-1").closest("[role=cell]")!;
  fireEvent.doubleClick(code);
  expect(code.querySelector("input")).toBeNull(); // so are fields the domain marks read-only
});

test("a record form uses field editors and reports field errors", async () => {
  const submit = vi.fn();
  render(<RecordForm entity={material} keys={["name", "kind", "supplier"]} onSubmit={submit} defaultValues={{ name: "", supplier: "x" }} />);
  await act(async () => { fireEvent.click(screen.getByRole("button", { name: "Save" })); });
  expect(submit).not.toHaveBeenCalled();
  expect(screen.getByText("Email address")).toBeTruthy();
  fireEvent.change(screen.getByLabelText(/Name/), { target: { value: "Bolt" } });
  fireEvent.change(screen.getByLabelText("Supplier"), { target: { value: "buyer@example.com" } });
  fireEvent.change(screen.getByLabelText("Kind"), { target: { value: "raw" } });
  await act(async () => { fireEvent.click(screen.getByRole("button", { name: "Save" })); });
  expect(submit).toHaveBeenCalledWith({ name: "Bolt", kind: "raw", supplier: "buyer@example.com" }, expect.anything());
});
