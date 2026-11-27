import { Banner } from "@astryxdesign/core/Banner";
import { EmptyState } from "@astryxdesign/core/EmptyState";
import { HStack, VStack } from "@astryxdesign/core/Layout";
import { Link } from "@astryxdesign/core/Link";
import { Skeleton } from "@astryxdesign/core/Skeleton";
import { Table, TableBody, TableCell, TableFooter, TableHeader, TableHeaderCell, TableRow } from "@astryxdesign/core/Table";
import { Text } from "@astryxdesign/core/Text";
import type { PowerSearchFilter } from "@astryxdesign/core/PowerSearch";
import * as stylex from "@stylexjs/stylex";
import { type ReactNode, useMemo } from "react";
import { type Api, useRead } from "../host";
import { currentCurrency, t } from "../i18n";
import { useRecordHref } from "../recordRoutes";
import { MoneyValue, NumberValue } from "../semantic";
import type { Described } from "./describe";
import type { ViewState } from "./view";

type AggRow = Record<string, unknown>;

type Props = {
  described: Described;
  balanceType: string;
  factType: string;
  listPath: string;
  view: ViewState;
  baseDomain: unknown[][];
  groups: string[];
  measureField: string;
  renderGroupCell: (field: Api.FieldInfo | undefined, raw: unknown) => ReactNode;
  labelOf: (fieldName: string, raw: unknown) => string;
};

const styles = stylex.create({ tableWrap: { width: "100%", overflowX: "auto" } });

function metricNumber(raw: unknown): number {
  if (typeof raw === "number") return raw;
  if (raw && typeof raw === "object" && "amount" in raw) return Number((raw as { amount: unknown }).amount ?? 0);
  return Number(raw ?? 0);
}

export function LedgerPeriodSummary({
  described,
  balanceType,
  factType,
  listPath,
  view,
  baseDomain,
  groups,
  measureField,
  renderGroupCell,
  labelOf,
}: Props) {
  const recordHref = useRecordHref();
  const targetPath = factType === described.type ? listPath : recordHref(factType);
  const measureSpec = `sum:${measureField}`;
  const read = useRead<Api.RecordsAggregate>("/v1/object-sets/aggregate", {
    set: baseDomain.length ? { object: balanceType, domain: baseDomain } : { object: balanceType },
    groups,
    measures: [measureSpec],
    from: view.from || undefined,
    to: view.to || undefined,
  });

  const keys = {
    opening: `opening:${measureField}`,
    debit: `debit:${measureField}`,
    credit: `credit:${measureField}`,
    closing: `closing:${measureField}`,
  };

  const isMoney = described.field(measureField)?.type === "money";
  const currency = useMemo(() => {
    if (read.status !== "ready") return currentCurrency();
    const currencyCol = read.data.columns.find((c) => c.name.endsWith(".currency"))?.name;
    if (currencyCol) {
      const found = read.data.rows.find((r) => r[currencyCol])?.[currencyCol];
      if (found) return String(found);
    }
    return currentCurrency();
  }, [read]);

  const drillHref = (criteria: { field: string; value: unknown }[], bucket: "opening" | "debit" | "credit" | "closing") => {
    const nextFilters: PowerSearchFilter[] = [...view.filters];
    const extraDomain: unknown[][] = [...baseDomain];
    for (const { field, value } of criteria) {
      if (value == null || value === "") {
        extraDomain.push([field, "=", ""]);
        continue;
      }
      const info = described.field(field);
      if (info?.type === "boolean") {
        nextFilters.push({ field, operator: value === true ? "is_true" : "is_false", value: { type: "empty" } });
      } else if (info?.type === "choice" || info?.type === "reference" || info?.type === "member") {
        nextFilters.push({ field, operator: "is", value: { type: "enum", value: String(value) } });
      } else {
        nextFilters.push({ field, operator: "is", value: { type: "string", value: String(value) } });
      }
    }
    if (bucket === "opening") {
      if (view.from) extraDomain.push(["on", "<", view.from]);
    } else if (bucket === "debit") {
      if (view.from) extraDomain.push(["on", ">=", view.from]);
      if (view.to) extraDomain.push(["on", "<=", view.to]);
      extraDomain.push([measureField, ">", 0]);
    } else if (bucket === "credit") {
      if (view.from) extraDomain.push(["on", ">=", view.from]);
      if (view.to) extraDomain.push(["on", "<=", view.to]);
      extraDomain.push([measureField, "<", 0]);
    } else if (view.to) {
      extraDomain.push(["on", "<=", view.to]);
    }
    const nextView: ViewState = { ...view, layout: "table", group: "none", subGroup: "none", filters: nextFilters };
    const params = new URLSearchParams({ view: JSON.stringify(nextView) });
    if (extraDomain.length > 0) params.set("domain", JSON.stringify(extraDomain));
    return `${targetPath}?${params}`;
  };

  const renderCell = (amount: number, href: string) => (
    <HStack hAlign="end">
      <Link href={href}>
        {isMoney
          ? <MoneyValue value={{ amount, currency }} size="sm" align="end" />
          : <NumberValue value={amount} size="sm" align="end" />}
      </Link>
    </HStack>
  );

  const rows = read.status === "ready" ? (read.data.rows as AggRow[]) : [];
  const sumBucket = (items: AggRow[], k: string) => items.reduce((acc, row) => acc + metricNumber(row[k]), 0);
  const group1 = groups[0] ?? "none";
  const group2 = groups[1] ?? "none";
  const g1Field = described.field(group1);
  const g2Field = described.field(group2);

  const groupedSections = useMemo(() => {
    if (groups.length < 2) return [];
    const byFirst = new Map<string, { raw: unknown; items: AggRow[] }>();
    for (const row of rows) {
      const raw = row[group1];
      const key = String(raw ?? "");
      const bucket = byFirst.get(key);
      if (bucket) bucket.items.push(row);
      else byFirst.set(key, { raw, items: [row] });
    }
    return Array.from(byFirst.entries()).map(([key, { raw, items }]) => ({ key, raw, items }));
  }, [rows, groups.length, group1]);

  if (read.status === "loading") return <Skeleton height={180} />;
  if (read.status === "error") return <Banner status="error" title={t("Summary could not be loaded")} description={read.reason} />;
  if (read.status === "refused") return <Banner status="warning" title={t("You do not have access to this data")} description={read.reason} />;
  if (rows.length === 0) return <EmptyState headingLevel={3} title={t("No summary data")} />;

  const renderBuckets = (criteria: { field: string; value: unknown }[], opening: number, debit: number, credit: number, closing: number) => (
    <>
      <TableCell>{renderCell(opening, drillHref(criteria, "opening"))}</TableCell>
      <TableCell>{renderCell(debit, drillHref(criteria, "debit"))}</TableCell>
      <TableCell>{renderCell(credit, drillHref(criteria, "credit"))}</TableCell>
      <TableCell>{renderCell(closing, drillHref(criteria, "closing"))}</TableCell>
    </>
  );

  return (
    <VStack xstyle={styles.tableWrap}>
      <Table aria-label={t("Summary")} density="compact">
        <TableHeader>
          <TableRow>
            {g1Field && <TableHeaderCell>{t(g1Field.title)}</TableHeaderCell>}
            {g2Field && <TableHeaderCell>{t(g2Field.title)}</TableHeaderCell>}
            <TableHeaderCell><HStack hAlign="end">{t("Opening")}</HStack></TableHeaderCell>
            <TableHeaderCell><HStack hAlign="end">{t("Increase")}</HStack></TableHeaderCell>
            <TableHeaderCell><HStack hAlign="end">{t("Decrease")}</HStack></TableHeaderCell>
            <TableHeaderCell><HStack hAlign="end">{t("Closing")}</HStack></TableHeaderCell>
          </TableRow>
        </TableHeader>
        <TableBody>
          {groups.length === 0 && (
            <TableRow>
              {renderBuckets([], sumBucket(rows, keys.opening), sumBucket(rows, keys.debit), sumBucket(rows, keys.credit), sumBucket(rows, keys.closing))}
            </TableRow>
          )}
          {groups.length === 1 && rows.map((row, index) => {
            const v1 = row[group1];
            const crit = [{ field: group1, value: v1 }];
            return (
              <TableRow key={`${String(v1 ?? "")}:${index}`}>
                <TableCell>{renderGroupCell(g1Field, v1)}</TableCell>
                {renderBuckets(crit, metricNumber(row[keys.opening]), metricNumber(row[keys.debit]), metricNumber(row[keys.credit]), metricNumber(row[keys.closing]))}
              </TableRow>
            );
          })}
          {groups.length === 2 && groupedSections.flatMap((section) => [
            ...section.items.map((row, idx) => {
              const v2 = row[group2];
              const crit = [{ field: group1, value: section.raw }, { field: group2, value: v2 }];
              return (
                <TableRow key={`${section.key}:${String(v2 ?? "")}:${idx}`}>
                  <TableCell>{idx === 0 ? renderGroupCell(g1Field, section.raw) : null}</TableCell>
                  <TableCell>{renderGroupCell(g2Field, v2)}</TableCell>
                  {renderBuckets(crit, metricNumber(row[keys.opening]), metricNumber(row[keys.debit]), metricNumber(row[keys.credit]), metricNumber(row[keys.closing]))}
                </TableRow>
              );
            }),
            <TableRow key={`${section.key}:subtotal`}>
              <TableCell><Text type="supporting" weight="medium">{`${labelOf(group1, section.raw)} · ${t("Subtotal")}`}</Text></TableCell>
              <TableCell />
              {renderBuckets(
                [{ field: group1, value: section.raw }],
                sumBucket(section.items, keys.opening),
                sumBucket(section.items, keys.debit),
                sumBucket(section.items, keys.credit),
                sumBucket(section.items, keys.closing),
              )}
            </TableRow>,
          ])}
        </TableBody>
        <TableFooter>
          <TableRow>
            {g1Field && <TableCell><Text type="supporting" weight="medium">{t("Totals")}</Text></TableCell>}
            {g2Field && <TableCell />}
            {renderBuckets([], sumBucket(rows, keys.opening), sumBucket(rows, keys.debit), sumBucket(rows, keys.credit), sumBucket(rows, keys.closing))}
          </TableRow>
        </TableFooter>
      </Table>
    </VStack>
  );
}
