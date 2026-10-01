import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, expect, test, vi } from "vitest";
import { Input, VirtualStack } from "./index";

afterEach(() => { cleanup(); vi.restoreAllMocks(); });

test("virtual items are bounded and focused input follows identity through reorder", () => {
  vi.spyOn(HTMLElement.prototype, "offsetHeight", "get").mockReturnValue(560);
  vi.spyOn(HTMLElement.prototype, "offsetWidth", "get").mockReturnValue(600);
  const rows = Array.from({ length: 100 }, (_, id) => ({ id: `item-${id}` }));
  const props = { label: "Items", itemKey: (item: {id:string}) => item.id, renderItem: (item: {id:string}) => <Input aria-label={item.id} defaultValue="" /> };
  const { rerender } = render(<VirtualStack {...props} items={rows} />);
  const original = screen.getByLabelText("item-0"); act(() => original.focus()); fireEvent.change(original, { target: { value: "local draft" } });
  expect(screen.getAllByRole("listitem").length).toBeLessThan(rows.length);
  rerender(<VirtualStack {...props} items={[...rows].reverse()} />);
  expect(screen.getByLabelText("item-0")).toBe(original);
  expect((screen.getByLabelText("item-0") as HTMLInputElement).value).toBe("local draft");
  expect(screen.getAllByRole("listitem").length).toBeLessThan(rows.length);
});
