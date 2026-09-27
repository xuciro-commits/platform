import { cleanup, render } from "@testing-library/react";
import { afterEach, expect, test } from "vitest";
import { LinesChange, type FieldInfo } from "./records/Records";

afterEach(cleanup);

const rooms = { name: "stays", title: "Stays", type: "lines", fields: [
  { name: "booking", title: "Booking", type: "text" }, { name: "status", title: "Status", type: "text" }, { name: "rate", title: "Rate", type: "money" },
] } as unknown as FieldInfo;

test("a lines change reads row by row: changed cells, lines added and removed", () => {
  const { container } = render(<LinesChange field={rooms}
    before={[{ booking: "B1", status: "asked", rate: { amount: 12000, currency: "CNY" } }, { booking: "B3", status: "held" }]}
    after={[{ booking: "B1", status: "held", rate: { amount: 12000, currency: "CNY" } }, { booking: "B2", status: "asked" }]} />);
  const text = container.textContent ?? "";
  expect(text).toContain("B1: Status asked → held");
  expect(text).not.toContain("Rate 120.00 CNY →"); // an unchanged cell is not repeated
  expect(text).toContain("+ Booking B2 · Status asked");
  expect(text).toContain("− Booking B3 · Status held");
  expect(text).not.toContain("{");
});
