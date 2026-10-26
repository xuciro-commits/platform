import { expect, test } from "vitest";
import { elementStatus, live } from "./model";

test("effective periods preserve inactive gaps and override the legacy interval", () => {
  const element = { id: "unit", name: "Unit", stereotype: "ActualOrganization", from: "2026-01-01", periods: [{ from: "2026-01-01", until: "2026-03-01" }, { from: "2026-06-01" }] };
  expect(live(element, "2026-02-28")).toBe(true);
  expect(live(element, "2026-03-01")).toBe(false);
  expect(live(element, "2026-05-31")).toBe(false);
  expect(live(element, "2026-06-01")).toBe(true);
  expect(elementStatus(element, "2026-05-01")).toBe("Future");
  expect(live({ ...element, periods: [] }, "2026-02-01")).toBe(false);
  expect(live({ ...element, deleted: "2026-07-01" }, "2026-02-01")).toBe(false);
});

test("historical elements and relationships without periods retain inclusive starts and exclusive ends", () => {
  const legacy = { from: "2026-01-01", until: "2026-02-01" };
  expect(live(legacy, "2025-12-31")).toBe(false);
  expect(live(legacy, "2026-01-01")).toBe(true);
  expect(live(legacy, "2026-02-01")).toBe(false);
  const element = { ...legacy, id: "unit", name: "Unit", stereotype: "ActualOrganization" };
  expect(elementStatus(element, "2025-12-31")).toBe("Future");
  expect(elementStatus(element, "2026-01-01")).toBe("Active");
  expect(elementStatus(element, "2026-02-01")).toBe("Inactive");
});
