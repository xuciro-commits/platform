import { expect, test } from "vitest";
import { toOption, type Theme } from "./echarts";
import { groupDomain } from "./Pivot";
import { aggregateQuery, aggregateValues, type AggregateColumn, type ChartSpec } from "./spec";

const theme: Theme = { palette: ["#1", "#2", "#3"], foreground: "#f", muted: "#m", border: "#b" };

test("a spec over records asks the host for groups and measures", () => {
  const spec: ChartSpec = {
    data: { entity: "pms.reservation", domain: [["canceled", "=", false]] }, mark: "bar",
    encoding: { x: { field: "checkIn", timeUnit: "month", type: "temporal" }, y: { aggregate: "count", type: "quantitative" }, color: { field: "roomType", type: "nominal" } },
  };
  expect(aggregateQuery(spec)).toEqual({ domain: [["canceled", "=", false]], groups: ["checkIn:month", "roomType"], measures: ["count"], search: undefined, archived: undefined });
  expect(aggregateQuery({ ...spec, data: { values: [] } })).toBeUndefined();
});

test("inline values aggregate into the same columns as the host's", () => {
  const spec: ChartSpec = {
    data: { values: [] }, mark: "line",
    encoding: { x: { field: "at", timeUnit: "month", type: "temporal" }, y: { field: "qty", aggregate: "sum", type: "quantitative" } },
  };
  const rows = aggregateValues(spec, [{ at: "2026-10-03", qty: 2 }, { at: "2026-10-20", qty: 3 }, { at: "2026-11-01", qty: 1 }]);
  expect(rows).toEqual([{ "at:month": "2026-10", "sum:qty": 5 }, { "at:month": "2026-11", "sum:qty": 1 }]);
  const weeks = aggregateValues({ ...spec, encoding: { ...spec.encoding, x: { field: "at", timeUnit: "week", type: "temporal" } } }, [{ at: "2027-01-01", qty: 1 }]);
  expect(weeks[0]!["at:week"]).toBe("2026-W53");
});

test("the renderer draws one series per colour, categories in time order", () => {
  const spec: ChartSpec = {
    data: { entity: "crm.opportunity" }, mark: { type: "bar", stack: true },
    encoding: { x: { field: "created", timeUnit: "month", type: "temporal" }, y: { aggregate: "count", type: "quantitative" }, color: { field: "stage", type: "nominal" } },
  };
  const columns: AggregateColumn[] = [{ name: "created:month", title: "Created (month)", kind: "group", type: "temporal" },
    { name: "stage", title: "Stage", kind: "group", type: "nominal" }, { name: "count", title: "Count", kind: "measure", type: "quantitative" }];
  const option = toOption(spec, [{ "created:month": "2026-10", stage: "won", count: 2 }, { "created:month": "2026-09", stage: "open", count: 1 },
    { "created:month": "2026-10", stage: "open", count: 4 }], columns, theme) as { xAxis: { data: string[] }; series: { name: string; stack?: string; data: (number | null)[] }[] };
  expect(option.xAxis.data).toEqual(["2026-09", "2026-10"]);
  expect(option.series.map((s) => [s.name, s.stack, s.data])).toEqual([["won", "total", [null, 2]], ["open", "total", [1, 4]]]);
});

test("money sums split by currency and are shown in major units", () => {
  const spec: ChartSpec = { data: { entity: "crm.opportunity" }, mark: "arc", encoding: { theta: { field: "amount", aggregate: "sum", type: "quantitative" }, color: { field: "stage", type: "nominal" } } };
  const columns: AggregateColumn[] = [{ name: "stage", title: "Stage", kind: "group", type: "nominal" },
    { name: "amount.currency", title: "Amount currency", kind: "group", type: "nominal", field: "amount" },
    { name: "sum:amount", title: "Amount (sum)", kind: "measure", type: "quantitative", field: "amount", money: true }];
  const option = toOption(spec, [{ stage: "won", "amount.currency": "EUR", "sum:amount": 150000 }], columns, theme) as { series: { type: string; data: unknown[]; tooltip: { valueFormatter: (v: unknown) => string } }[] };
  expect(option.series[0]!.type).toBe("pie");
  expect(option.series[0]!.data).toEqual([{ name: "won", value: 150000 }]);
});

test("drilling into a group selects its records, date buckets as ranges", () => {
  expect(groupDomain("stage", "won")).toEqual([["stage", "=", "won"]]);
  expect(groupDomain("checkIn:month", "2026-12")).toEqual([["checkIn", ">=", "2026-12-01"], ["checkIn", "<", "2027-01-01"]]);
  expect(groupDomain("created:day", "2026-10-31")).toEqual([["created", ">=", "2026-10-31"], ["created", "<", "2026-11-01"]]);
});
