import { Banner } from "@astryxdesign/core/Banner";
import { DateInput } from "@astryxdesign/core/DateInput";
import { EmptyState } from "@astryxdesign/core/EmptyState";
import { HStack, VStack } from "@astryxdesign/core/Layout";
import { Link } from "@astryxdesign/core/Link";
import { Selector } from "@astryxdesign/core/Selector";
import { Skeleton } from "@astryxdesign/core/Skeleton";
import { Table, TableBody, TableCell, TableFooter, TableHeader, TableHeaderCell, TableRow } from "@astryxdesign/core/Table";
import { Text } from "@astryxdesign/core/Text";
import type { PowerSearchFilter } from "@astryxdesign/core/PowerSearch";
import { type ComponentProps, useMemo } from "react";
import { type Api, useRead } from "../host";
import { currentCurrency, t, useFormatConfig } from "../i18n";
import { formatMoneyValue, formatTenantNumber } from "../semantic";
import { numeric, type Described } from "./describe";
import { useGutter } from "./frame";
import type { Options } from "./options";
import { present } from "./values";
import type { ViewState } from "./view";

type SummaryProps = {
  described: Described;
  listPath: string;
  view: ViewState;
  baseDomain: unknown[][];
  options: Options;
  update: (patch: Partial<ViewState>) => void;
};

type AggRow = Record<string, unknown>;
type Groupable = { name: string; title: string; field: Api.FieldInfo };
type MeasureOption = { value: string; label: string; field?: Api.FieldInfo; fn: "count" | "sum" | "avg" };

const groupableTypes = new Set(["choice", "reference", "member", "boolean"]);

function boundDate(field: Api.FieldInfo, day: string, end: boolean): string {
  if (field.type === "datetime") return end ? `${day}T23:59:59Z` : `${day}T00:00:00Z`;
  return day;
}

export function SummaryView({ described, listPath, view, baseDomain, options, update }: SummaryProps) {
  useFormatConfig();
  const gutter = useGutter();
  const directory = useRead<Api.MemberOption[]>("/v1/member-directory");
  const membersById = useMemo(
    () => new Map((directory.status === "ready" ? directory.data : []).map((m) => [m.id, m.name])),
    [directory],
  );

  const groupables = useMemo<Groupable[]>(() => {
    const out: Groupable[] = [];
    for (const field of described.entity.fields) {
      if (groupableTypes.has(field.type) || field.name === described.lifecycle?.field) {
        out.push({ name: field.name, title: field.title, field });
      }
    }
    return out;
  }, [described]);

  const dateFields = useMemo(
    () => described.entity.fields.filter((field) => field.type === "date" || field.type === "datetime"),
    [described],
  );

  const measureOptions = useMemo<MeasureOption[]>(() => {
    const list: MeasureOption[] = [{ value: "count", label: t("Count"), fn: "count" }];
    for (const field of described.entity.fields) {
      if (!numeric.has(field.type)) continue;
      list.push({ value: `sum:${field.name}`, label: `${t("Sum")} · ${t(field.title)}`, field, fn: "sum" });
      list.push({ value: `avg:${field.name}`, label: `${t("Average")} · ${t(field.title)}`, field, fn: "avg" });
    }
    return list;
  }, [described]);

  const group1 = groupables.find((g) => g.name === view.group)?.name ?? groupables[0]?.name ?? "none";
  const group2 = view.subGroup && view.subGroup !== "none" && view.subGroup !== group1 && groupables.some((g) => g.name === view.subGroup)
    ? view.subGroup
    : "none";
  const activeMeasure = measureOptions.find((m) => m.value === view.measure) ?? measureOptions[0];
  const activeDate = dateFields.find((f) => f.name === view.dates) ?? dateFields[0];

  const periodDomain = useMemo(() => {
    const conds: unknown[][] = [];
    if (activeDate && view.from) conds.push([activeDate.name, ">=", boundDate(activeDate, view.from, false)]);
    if (activeDate && view.to) conds.push([activeDate.name, "<=", boundDate(activeDate, view.to, true)]);
    return conds;
  }, [activeDate, view.from, view.to]);

  const fullDomain = useMemo(() => [...baseDomain, ...periodDomain], [baseDomain, periodDomain]);
  const groups = useMemo(
    () => [group1, group2].filter((g) => g !== "none"),
    [group1, group2],
  );
  const measures = useMemo(
    () => (activeMeasure.value === "count" ? ["count"] : ["count", activeMeasure.value]),
    [activeMeasure.value],
  );

  const read = useRead<Api.RecordsAggregate>("/v1/object-sets/aggregate", {
    set: fullDomain.length ? { object: described.type, domain: fullDomain } : { object: described.type },
    groups,
    measures,
  });

  const labelOf = (fieldName: string, raw: unknown): string => {
    if (raw == null || raw === "") return t("Not set");
    const field = described.field(fieldName);
    if (!field) return String(raw);
    if (field.name === described.lifecycle?.field) {
      const state = described.lifecycle.states.find((s) => s.name === String(raw));
      return state ? t(state.title) : String(raw);
    }
    if (field.type === "member") {
      return membersById.get(String(raw)) ?? options[field.name]?.find((o) => o.value === String(raw))?.label ?? String(raw);
    }
    if (field.type === "choice") {
      const idx = field.choices?.indexOf(String(raw)) ?? -1;
      return idx >= 0 && field.choiceTitles?.[idx] ? t(field.choiceTitles[idx]) : String(raw);
    }
    return options[field.name]?.find((o) => o.value === String(raw))?.label ?? (present(field, raw) || String(raw));
  };

  const drillHref = (criteria: { field: string; value: unknown }[]) => {
    const nextFilters: PowerSearchFilter[] = [...view.filters];
    const extraDomain: unknown[][] = [...periodDomain];
    for (const { field, value } of criteria) {
      if (value == null || value === "") {
        extraDomain.push([field, "=", ""]);
        continue;
      }
      const info = described.field(field);
      if (info?.type === "boolean") {
        nextFilters.push({ field, operator: value === true ? "is_true" : "is_false", value: { type: "empty" } });
      } else if (info?.type === "choice" || info?.type === "reference" || info?.type === "member" || field === described.lifecycle?.field) {
        nextFilters.push({ field, operator: "is", value: { type: "enum", value: String(value) } });
      } else {
        nextFilters.push({ field, operator: "is", value: { type: "string", value: String(value) } });
      }
    }
    const nextView: ViewState = {
      ...view,
      layout: "table",
      group: "none",
      subGroup: "none",
      filters: nextFilters,
    };
    const params = new URLSearchParams({ view: JSON.stringify(nextView) });
    if (extraDomain.length > 0) params.set("domain", JSON.stringify(extraDomain));
    return `${listPath}?${params}`;
  };

  const currencyKey = read.status === "ready" ? read.data.columns.find((c) => c.name.endsWith(".currency"))?.name : undefined;
  const isMoney = read.status === "ready" && Boolean(read.data.columns.find((c) => c.name === activeMeasure.value)?.money);
  const currency = (read.status === "ready" && currencyKey ? String(read.data.rows.find((r) => r[currencyKey])?.[currencyKey] ?? "") : "") || currentCurrency();

  const formatMetric = (amount: number): string => {
    if (activeMeasure.fn === "count") return formatTenantNumber(amount, { maximumFractionDigits: 0 });
    if (isMoney) return formatMoneyValue({ amount, currency })?.text ?? "0";
    return formatTenantNumber(amount, { maximumFractionDigits: 2 });
  };

  const rows = read.status === "ready" ? (read.data.rows as AggRow[]) : [];
  const totalCount = rows.reduce((sum, row) => sum + Number(row.count ?? 0), 0);
  const totalMetric = activeMeasure.fn === "avg"
    ? (totalCount > 0 ? rows.reduce((sum, row) => sum + Number(row[activeMeasure.value] ?? 0) * Number(row.count ?? 0), 0) / totalCount : 0)
    : rows.reduce((sum, row) => sum + Number(row[activeMeasure.value] ?? 0), 0);

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
    return Array.from(byFirst.entries()).map(([key, { raw, items }]) => {
      const subCount = items.reduce((sum, item) => sum + Number(item.count ?? 0), 0);
      const subMetric = activeMeasure.fn === "avg"
        ? (subCount > 0 ? items.reduce((sum, item) => sum + Number(item[activeMeasure.value] ?? 0) * Number(item.count ?? 0), 0) / subCount : 0)
        : items.reduce((sum, item) => sum + Number(item[activeMeasure.value] ?? 0), 0);
      return { key, raw, items, subCount, subMetric };
    });
  }, [rows, groups.length, group1, activeMeasure]);

  const g1Field = described.field(group1);
  const g2Field = described.field(group2);

  return (
    <VStack gap={4} paddingInline={gutter} paddingBlockEnd={6}>
      <HStack gap={3} vAlign="end" wrap="wrap">
        <Selector
          label={t("Group by")}
          size="sm"
          value={group1}
          options={[{ value: "none", label: t("None") }, ...groupables.map((g) => ({ value: g.name, label: t(g.title) }))]}
          onChange={(next) => update({ group: next, subGroup: next === "none" || next === group2 ? "none" : group2 })}
        />
        {group1 !== "none" && groupables.length > 1 && (
          <Selector
            label={t("Then by")}
            size="sm"
            value={group2}
            options={[{ value: "none", label: t("None") }, ...groupables.filter((g) => g.name !== group1).map((g) => ({ value: g.name, label: t(g.title) }))]}
            onChange={(next) => update({ subGroup: next })}
          />
        )}
        <Selector
          label={t("Measure")}
          size="sm"
          value={activeMeasure.value}
          options={measureOptions.map((m) => ({ value: m.value, label: m.label }))}
          onChange={(next) => update({ measure: next })}
        />
        {activeDate && (
          <>
            {dateFields.length > 1 && (
              <Selector
                label={t("Date field")}
                size="sm"
                value={activeDate.name}
                options={dateFields.map((f) => ({ value: f.name, label: t(f.title) }))}
                onChange={(next) => update({ dates: next })}
              />
            )}
            <DateInput
              label={t("From")}
              size="sm"
              format="date"
              value={view.from ? (view.from as ComponentProps<typeof DateInput>["value"]) : undefined}
              onChange={(next) => update({ from: next ?? "" })}
            />
            <DateInput
              label={t("To")}
              size="sm"
              format="date"
              value={view.to ? (view.to as ComponentProps<typeof DateInput>["value"]) : undefined}
              onChange={(next) => update({ to: next ?? "" })}
            />
          </>
        )}
      </HStack>

      {read.status === "loading" && <Skeleton height={180} />}
      {read.status === "error" && <Banner status="error" title={t("Summary could not be loaded")} description={read.reason} />}
      {read.status === "refused" && <Banner status="warning" title={t("You do not have access to this data")} description={read.reason} />}
      {read.status === "ready" && rows.length === 0 && (
        <EmptyState headingLevel={3} title={t("No summary data")} />
      )}
      {read.status === "ready" && rows.length > 0 && (
        <Table aria-label={t("Summary")} density="compact">
          <TableHeader>
            <TableRow>
              {g1Field && <TableHeaderCell>{t(g1Field.title)}</TableHeaderCell>}
              {g2Field && <TableHeaderCell>{t(g2Field.title)}</TableHeaderCell>}
              <TableHeaderCell>{t("Count")}</TableHeaderCell>
              {activeMeasure.fn !== "count" && <TableHeaderCell>{activeMeasure.label}</TableHeaderCell>}
            </TableRow>
          </TableHeader>
          <TableBody>
            {groups.length === 0 && (
              <TableRow>
                <TableCell>
                  <Link href={drillHref([])}>{formatTenantNumber(totalCount, { maximumFractionDigits: 0 })}</Link>
                </TableCell>
                {activeMeasure.fn !== "count" && (
                  <TableCell>
                    <Link href={drillHref([])}>{formatMetric(totalMetric)}</Link>
                  </TableCell>
                )}
              </TableRow>
            )}
            {groups.length === 1 && rows.map((row, index) => {
              const v1 = row[group1];
              const href = drillHref([{ field: group1, value: v1 }]);
              return (
                <TableRow key={`${String(v1 ?? "")}:${index}`}>
                  <TableCell>{labelOf(group1, v1)}</TableCell>
                  <TableCell>
                    <Link href={href}>{formatTenantNumber(Number(row.count ?? 0), { maximumFractionDigits: 0 })}</Link>
                  </TableCell>
                  {activeMeasure.fn !== "count" && (
                    <TableCell>
                      <Link href={href}>{formatMetric(Number(row[activeMeasure.value] ?? 0))}</Link>
                    </TableCell>
                  )}
                </TableRow>
              );
            })}
            {groups.length === 2 && groupedSections.flatMap((section) => [
              ...section.items.map((row, idx) => {
                const v2 = row[group2];
                const href = drillHref([{ field: group1, value: section.raw }, { field: group2, value: v2 }]);
                return (
                  <TableRow key={`${section.key}:${String(v2 ?? "")}:${idx}`}>
                    <TableCell>{idx === 0 ? labelOf(group1, section.raw) : ""}</TableCell>
                    <TableCell>{labelOf(group2, v2)}</TableCell>
                    <TableCell>
                      <Link href={href}>{formatTenantNumber(Number(row.count ?? 0), { maximumFractionDigits: 0 })}</Link>
                    </TableCell>
                    {activeMeasure.fn !== "count" && (
                      <TableCell>
                        <Link href={href}>{formatMetric(Number(row[activeMeasure.value] ?? 0))}</Link>
                      </TableCell>
                    )}
                  </TableRow>
                );
              }),
              <TableRow key={`${section.key}:subtotal`}>
                <TableCell>
                  <Text type="supporting" weight="medium">{`${labelOf(group1, section.raw)} · ${t("Subtotal")}`}</Text>
                </TableCell>
                <TableCell />
                <TableCell>
                  <Link href={drillHref([{ field: group1, value: section.raw }])}>
                    {formatTenantNumber(section.subCount, { maximumFractionDigits: 0 })}
                  </Link>
                </TableCell>
                {activeMeasure.fn !== "count" && (
                  <TableCell>
                    <Link href={drillHref([{ field: group1, value: section.raw }])}>
                      {formatMetric(section.subMetric)}
                    </Link>
                  </TableCell>
                )}
              </TableRow>,
            ])}
          </TableBody>
          <TableFooter>
            <TableRow>
              {g1Field && <TableCell><Text type="supporting" weight="medium">{t("Totals")}</Text></TableCell>}
              {g2Field && <TableCell />}
              <TableCell>
                <Link href={drillHref([])}>{formatTenantNumber(totalCount, { maximumFractionDigits: 0 })}</Link>
              </TableCell>
              {activeMeasure.fn !== "count" && (
                <TableCell>
                  <Link href={drillHref([])}>{formatMetric(totalMetric)}</Link>
                </TableCell>
              )}
            </TableRow>
          </TableFooter>
        </Table>
      )}
    </VStack>
  );
}
