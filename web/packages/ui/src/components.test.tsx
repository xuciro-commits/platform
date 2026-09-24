import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";
import { z } from "zod";
import { DataTable, EntityForm, StatusTag, submissionStatuses, type ColumnDef } from "./index";

afterEach(cleanup);

// jsdom has no layout: give every element a 800×280 box so the virtualizer has a viewport.
Element.prototype.getBoundingClientRect = () => ({ width: 800, height: 280, top: 0, left: 0, right: 800, bottom: 280, x: 0, y: 0, toJSON: () => ({}) });
for (const [key, value] of [["offsetWidth", 800], ["offsetHeight", 280]] as const) {
  Object.defineProperty(HTMLElement.prototype, key, { configurable: true, get: () => value });
}

type Row = { id: string; name: string; qty: number };
const rows: Row[] = Array.from({ length: 100_000 }, (_, i) => ({ id: `r${i}`, name: `Item ${i}`, qty: i % 97 }));
const columns: ColumnDef<Row, any>[] = [
  { accessorKey: "name", header: "Name" },
  { accessorKey: "qty", header: "Qty", meta: { align: "right" } },
];

test("DataTable renders only the visible window of 100,000 rows", () => {
  render(<DataTable data={rows} columns={columns} getRowId={(r) => r.id} height={280} />);
  const rendered = screen.getAllByRole("row").length - 1; // minus header
  expect(rendered).toBeGreaterThan(0);
  expect(rendered).toBeLessThan(40);
  expect(screen.getByText("100,000 rows")).toBeTruthy();
});

test("DataTable filters and sorts", () => {
  render(<DataTable data={rows.slice(0, 50)} columns={columns} getRowId={(r) => r.id} />);
  fireEvent.change(screen.getByLabelText("Filter rows"), { target: { value: "Item 4" } });
  expect(screen.getByText("11 rows")).toBeTruthy(); // Item 4, 40–49
  fireEvent.click(screen.getByRole("button", { name: /Qty/ })); // numbers sort descending first
  expect(screen.getAllByRole("cell")[0]!.textContent).toBe("Item 49");
  fireEvent.click(screen.getByRole("button", { name: /Qty/ }));
  expect(screen.getAllByRole("cell")[0]!.textContent).toBe("Item 4");
});

test("StatusTag maps states to tones and falls back to neutral", () => {
  render(<>
    <StatusTag status="SUBMISSION_STATE_CONFLICT" registry={submissionStatuses} />
    <StatusTag status="SOMETHING_NEW" registry={submissionStatuses} />
  </>);
  expect(screen.getByText("Conflict").closest("[data-tone]")!.getAttribute("data-tone")).toBe("danger");
  expect(screen.getByText("SOMETHING_NEW").closest("[data-tone]")!.getAttribute("data-tone")).toBe("neutral");
});

test("EntityForm validates with the schema before submitting", async () => {
  const submit = vi.fn();
  const schema = z.object({ guest: z.string().min(1, "Required"), nights: z.number().min(1, "At least one night") });
  render(<EntityForm schema={schema} onSubmit={submit} defaultValues={{ guest: "", nights: 0 }}
    fields={[{ name: "guest", label: "Guest" }, { name: "nights", label: "Nights", kind: "number" }]} />);
  await act(async () => { fireEvent.click(screen.getByRole("button", { name: "Save" })); });
  expect(submit).not.toHaveBeenCalled();
  expect(screen.getByText("Required")).toBeTruthy();
  expect(screen.getByLabelText("Guest").getAttribute("aria-invalid")).toBe("true");
  fireEvent.change(screen.getByLabelText("Guest"), { target: { value: "Ada" } });
  fireEvent.change(screen.getByLabelText("Nights"), { target: { value: "2" } });
  await act(async () => { fireEvent.click(screen.getByRole("button", { name: "Save" })); });
  expect(submit).toHaveBeenCalledWith({ guest: "Ada", nights: 2 }, expect.anything());
});
