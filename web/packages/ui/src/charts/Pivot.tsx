// A pivot over an entity type (ADR-0019): row and column groups, one measure,
// totals, and drilling into the records behind a cell, like Odoo's pivot view.
import { useEffect, useRef, useState } from "react";
import { formatter } from "./echarts";
import type { AggregateData, AggregateQuery } from "./spec";
import type { ChartSource } from "./Chart";
import { t } from "../i18n";

const text = (v: unknown) => (v === undefined || v === null || v === "" ? "—" : String(v));

/** The domain that selects a group's records: a value, or a date bucket as a range. */
export function groupDomain(group: string, value: unknown): unknown[] {
  const [field, unit] = group.split(":") as [string, string | undefined];
  if (!unit) return [[field, "=", value ?? ""]];
  const v = String(value ?? "");
  if (!v) return [[field, "=", ""]];
  if (unit === "week") return []; // ISO weeks are not ranges the domain can name simply; drilling shows the whole filter
  const start = unit === "year" ? `${v}-01-01` : unit === "month" ? `${v}-01` : v;
  const d = new Date(`${start}T00:00:00Z`);
  if (unit === "year") d.setUTCFullYear(d.getUTCFullYear() + 1);
  else if (unit === "month") d.setUTCMonth(d.getUTCMonth() + 1);
  else d.setUTCDate(d.getUTCDate() + 1);
  return [[field, ">=", start], [field, "<", d.toISOString().slice(0, 10)]];
}

export function Pivot({ source, type, query, rows, columns, measure, onDrill }: {
  source: ChartSource; type: string; query: Omit<AggregateQuery, "groups" | "measures">;
  rows: string; columns?: string; measure: string; onDrill?: (domain: unknown[]) => void;
}) {
  const [data, setData] = useState<AggregateData>();
  const [error, setError] = useState<string>();
  const key = JSON.stringify([type, query, rows, columns, measure, source.revision ?? 0]);
  const from = useRef(source);
  from.current = source;
  useEffect(() => {
    let live = true;
    from.current.aggregate(type, { ...query, groups: columns ? [rows, columns] : [rows], measures: [measure] })
      .then((d) => { if (live) { setData(d); setError(undefined); } }, (e) => live && setError(String(e)));
    return () => { live = false; };
  }, [key]); // eslint-disable-line react-hooks/exhaustive-deps
  if (error) return <p className="text-sm text-[var(--tone-danger)]">{error}</p>;
  if (!data) return <p className="text-sm text-muted">{t("Loading…")}</p>;
  const fmt = formatter({ type: "quantitative", aggregate: measure === "count" ? "count" : measure.split(":")[0] as never, field: measure.split(":")[1] }, data.columns);
  const money = data.columns.find((c) => c.kind === "measure" && c.money);
  const currency = money ? `${money.field}.currency` : undefined;
  const rowKeys = [...new Set(data.rows.map((r) => text(r[rows])))];
  const colKeys = columns ? [...new Set(data.rows.map((r) => text(r[columns])))].sort() : [""];
  const additive = measure === "count" || measure.startsWith("sum:");
  const cell = (rk: string, ck: string) => data.rows.filter((r) => text(r[rows]) === rk && text(r[columns!]) === ck);
  const total = (list: Record<string, unknown>[]) => list.reduce((n, r) => n + Number(r[measure] ?? 0), 0);
  const show = (list: Record<string, unknown>[]) => {
    if (!list.length) return "";
    if (!additive && list.length > 1) return "…"; // averages and extremes do not add up across groups
    const byCurrency = new Map<string, Record<string, unknown>[]>();
    for (const r of list) {
      const c = currency ? String(r[currency] ?? "") : "";
      byCurrency.set(c, [...(byCurrency.get(c) ?? []), r]);
    }
    return [...byCurrency.values()].map((l) => fmt(total(l), l[0])).join(" · ");
  };
  const drill = (rk: string, ck?: string) => onDrill?.([
    ...groupDomain(rows, data.rows.find((r) => text(r[rows]) === rk)?.[rows]),
    ...(columns && ck !== undefined ? groupDomain(columns, data.rows.find((r) => text(r[columns]) === ck)?.[columns]) : []),
  ]);
  const title = (name: string) => data.columns.find((c) => c.name === name)?.title ?? name;
  return (
    <div className="overflow-auto rounded-md border border-border">
      <table className="w-full border-collapse text-sm tabular-nums">
        <thead className="bg-surface text-xs text-muted">
          <tr>
            <th className="border-b border-border px-2 py-1 text-left font-medium">{title(rows)}{columns ? ` by ${title(columns)}` : ""}</th>
            {columns && colKeys.map((ck) => <th key={ck} className="border-b border-border px-2 py-1 text-right font-medium">{ck}</th>)}
            <th className="border-b border-border px-2 py-1 text-right font-medium">{title(measure)}{columns ? " · total" : ""}</th>
          </tr>
        </thead>
        <tbody>
          {rowKeys.map((rk) => (
            <tr key={rk} className="hover:bg-row-hover">
              <th className="border-b border-border px-2 py-1 text-left font-normal">{rk}</th>
              {columns && colKeys.map((ck) => (
                <td key={ck} className="cursor-pointer border-b border-border px-2 py-1 text-right" onClick={() => drill(rk, ck)}>{show(cell(rk, ck))}</td>
              ))}
              <td className="cursor-pointer border-b border-border px-2 py-1 text-right font-medium" onClick={() => drill(rk)}>{show(data.rows.filter((r) => text(r[rows]) === rk))}</td>
            </tr>
          ))}
        </tbody>
        {additive && (
          <tfoot className="bg-surface font-medium">
            <tr>
              <th className="px-2 py-1 text-left">{t("Total")}</th>
              {columns && colKeys.map((ck) => <td key={ck} className="px-2 py-1 text-right">{show(data.rows.filter((r) => text(r[columns]) === ck))}</td>)}
              <td className="px-2 py-1 text-right">{show(data.rows)}</td>
            </tr>
          </tfoot>
        )}
      </table>
    </div>
  );
}
