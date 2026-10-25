import { afterEach, expect, test, vi } from "vitest";
import { formatDate, formatDateTime, formatDateTimeSmart, formatRelative } from "./datetime-format";

afterEach(() => vi.useRealTimers());

test("timestamps representing the same instant retain the same local display and relative day", () => {
  vi.useFakeTimers().setSystemTime(new Date("2026-10-24T12:00:00Z"));
  const instant = "2026-10-24T23:30:00Z", offset = "2026-10-25T07:30:00+08:00";
  expect(formatDateTime(instant)).toBe(formatDateTime(offset));
  expect(formatDateTimeSmart(instant)).toBe(formatDateTimeSmart(offset));
  expect(formatRelative(instant)).toBe(formatRelative(offset));
});

test("near timestamps retain their time as well as relative context", () => {
  vi.useFakeTimers().setSystemTime(new Date("2026-10-24T12:00:00Z"));
  const morning = "2026-10-24T09:15:00", afternoon = "2026-10-24T15:45:00";
  expect(formatDateTimeSmart(morning)).toContain(formatDateTime(morning));
  expect(formatDateTimeSmart(morning)).not.toBe(formatDateTimeSmart(afternoon));
  expect(formatDateTimeSmart(morning)).toContain("Today");
});

test("civil dates do not acquire a timezone and malformed values stay visible", () => {
  expect(formatDateTime("2026-10-24")).toBe(formatDate("2026-10-24"));
  expect(formatDate("2026-02-30")).toBe("2026-02-30");
  expect(formatDateTimeSmart("invalid timestamp")).toBe("invalid timestamp");
  expect(formatRelative("2026-10-24")).toBeUndefined();
});
