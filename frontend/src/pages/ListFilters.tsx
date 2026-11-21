import { BottomSheet } from "@astryxdesign/core/BottomSheet";
import { Button } from "@astryxdesign/core/Button";
import { Calendar, type DateRange, type ISODateString } from "@astryxdesign/core/Calendar";
import { ComplexSelector } from "@astryxdesign/core/ComplexSelector";
import { Divider } from "@astryxdesign/core/Divider";
import { HStack, StackItem, VStack } from "@astryxdesign/core/Layout";
import { Link } from "@astryxdesign/core/Link";
import { MultiSelector } from "@astryxdesign/core/MultiSelector";
import { NumberInput } from "@astryxdesign/core/NumberInput";
import { OverflowList, type OverflowItem } from "@astryxdesign/core/OverflowList";
import { Popover } from "@astryxdesign/core/Popover";
import type { PowerSearchFilter } from "@astryxdesign/core/PowerSearch";
import { RadioList, RadioListItem } from "@astryxdesign/core/RadioList";
import { Selector } from "@astryxdesign/core/Selector";
import { Text } from "@astryxdesign/core/Text";
import { TextInput } from "@astryxdesign/core/TextInput";
import { borderVars, colorVars, durationVars, easeVars, fontWeightVars, radiusVars } from "@astryxdesign/core/theme/tokens.stylex";
import { MagnifyingGlassIcon } from "@heroicons/react/24/outline";
import * as stylex from "@stylexjs/stylex";
import { type ReactNode, useEffect, useState } from "react";
import { t } from "../i18n";

export type Chip = { key: string; label: string; kind: "many" | "range" | "date" | "boolean"; options: { value: string; label: string }[] };
type Range = [number | null, number | null];

const readoutReserve = 128;
const day = 86_400_000;
const presets = [
  { key: "past7", label: "Past 7 days", from: -7, to: 0 },
  { key: "past30", label: "Past 30 days", from: -30, to: 0 },
  { key: "next7", label: "Next 7 days", from: 0, to: 7 },
  { key: "next30", label: "Next 30 days", from: 0, to: 30 },
  { key: "before", label: "Before today", from: null, to: 0 },
] as const;

const styles = stylex.create({
  filterRow: { rowGap: 6 },
  searchSlot: {
    flexShrink: 1,
    minWidth: 120,
    width: { default: 160, ":focus-within": 240 },
    transitionProperty: "width",
    transitionDuration: { default: durationVars["--duration-fast"], "@media (prefers-reduced-motion: reduce)": "0s" },
    transitionTimingFunction: easeVars["--ease-standard"],
  },
  overflowSlot: { display: "flex", alignItems: "center", flexGrow: 1, paddingInlineEnd: readoutReserve, flexShrink: 100, minWidth: 44 + readoutReserve },
  overflowItem: { display: "flex", flexDirection: "column" },
  meta: { flexShrink: 0, marginInlineStart: 8, marginInlineEnd: -readoutReserve },
  chrome: {
    borderWidth: borderVars["--border-width"],
    borderStyle: "solid",
    borderColor: colorVars["--color-border-emphasized"],
    borderRadius: radiusVars["--radius-element"],
  },
  surface: { backgroundColor: colorVars["--color-background-surface"] },
  fill: { backgroundColor: colorVars["--color-overlay-pressed"] },
  popover: { width: 300 },
  empty: { fontWeight: fontWeightVars["--font-weight-normal"], color: colorVars["--color-text-secondary"] },
});

const listOf = (filter: PowerSearchFilter | undefined): string[] => {
  const raw = filter ? (filter.value as { value?: unknown }).value : undefined;
  return Array.isArray(raw) ? raw.map(String) : raw == null ? [] : [String(raw)];
};
const boundOf = (filters: PowerSearchFilter[], key: string, operators: string[]) => filters.find((filter) => filter.field === key && operators.includes(filter.operator));
const numberOf = (filter: PowerSearchFilter | undefined) => (filter ? Number((filter.value as { value: number }).value) : null);
const dateOf = (filter: PowerSearchFilter | undefined) => {
  const value = filter?.value as { unixSeconds?: number } | undefined;
  return value?.unixSeconds == null ? null : new Date(value.unixSeconds * 1000);
};
const shortDate = (date: Date) => date.toLocaleDateString(undefined, { month: "short", day: "numeric" });
const startOfToday = () => {
  const now = new Date();
  return new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime();
};
const isoOf = (time: number) => {
  const date = new Date(time);
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, "0")}-${String(date.getDate()).padStart(2, "0")}` as ISODateString;
};
const localDay = (iso: string) => {
  const [year, month, date] = iso.split("-").map(Number);
  return new Date(year, month - 1, date).getTime();
};
const numeric = (value: number): PowerSearchFilter["value"] => (Number.isInteger(value) ? { type: "integer", value } : { type: "float", value });

export function chipFilters(chip: Chip, filters: PowerSearchFilter[]) {
  return filters.filter((filter) => filter.field === chip.key);
}

type FilterBarProps = {
  chips: Chip[];
  filters: PowerSearchFilter[];
  onFilters: (next: PowerSearchFilter[]) => void;
  search: string;
  onSearch: (next: string) => void;
  count: string;
  onClear?: () => void;
  compact: boolean;
};

export function FilterBar({ chips, filters, onFilters, search, onSearch, count, onClear, compact }: FilterBarProps) {
  const [draft, setDraft] = useState(search);
  useEffect(() => setDraft(search), [search]);
  const replace = (key: string, next: PowerSearchFilter[]) => onFilters([...filters.filter((filter) => filter.field !== key), ...next]);

  const controls = chips.map((chip) => {
    const mine = chipFilters(chip, filters);
    if (chip.kind === "many") {
      const values = listOf(mine[0]);
      return (
        <MultiSelector
          key={chip.key}
          label={`${t(chip.label)} ${t("filter")}`}
          isLabelHidden
          placeholder={t(chip.label)}
          size="sm"
          hasClear
          triggerDisplay="labels"
          formatValue={(items) => (items.length > 1 ? `${items[0].label}, +${items.length - 1}` : items[0].label)}
          options={chip.options}
          value={values}
          controlXstyle={values.length > 0 ? styles.fill : undefined}
          onChange={(next) => replace(chip.key, next.length ? [{ field: chip.key, operator: "is_any_of", value: { type: "enum_list", value: next } }] : [])}
        />
      );
    }
    if (chip.kind === "boolean") {
      const value = mine[0]?.operator === "is_true" ? "yes" : mine[0]?.operator === "is_false" ? "no" : null;
      return (
        <Selector
          key={chip.key}
          label={`${t(chip.label)} ${t("filter")}`}
          isLabelHidden
          placeholder={t(chip.label)}
          size="sm"
          hasClear
          options={[{ value: "yes", label: t("Yes") }, { value: "no", label: t("No") }]}
          value={value}
          renderValue={(option) => `${t(chip.label)}: ${option.label ?? option.value}`}
          controlXstyle={value ? styles.fill : undefined}
          onChange={(next) => replace(chip.key, next ? [{ field: chip.key, operator: next === "yes" ? "is_true" : "is_false", value: { type: "empty" } }] : [])}
        />
      );
    }
    if (chip.kind === "range") {
      const low = numberOf(boundOf(mine, chip.key, ["greater_than_or_equal"]));
      const high = numberOf(boundOf(mine, chip.key, ["less_than_or_equal"]));
      const label =
        low != null && high != null ? `${t(chip.label)} ${low.toLocaleString()} – ${high.toLocaleString()}`
        : low != null ? `${t(chip.label)} ≥ ${low.toLocaleString()}`
        : high != null ? `${t(chip.label)} ≤ ${high.toLocaleString()}`
        : undefined;
      const commit = ([from, to]: Range) =>
        replace(chip.key, [
          ...(from == null ? [] : [{ field: chip.key, operator: "greater_than_or_equal", value: numeric(from) }]),
          ...(to == null ? [] : [{ field: chip.key, operator: "less_than_or_equal", value: numeric(to) }]),
        ]);
      return (
        <ComplexSelector<Range>
          key={chip.key}
          label={`${t(chip.label)} ${t("filter")}`}
          isLabelHidden
          placeholder={t(chip.label)}
          triggerLabel={label}
          size="sm"
          value={[low, high]}
          onChange={commit}
          contentXstyle={styles.popover}
          controlXstyle={label ? styles.fill : undefined}>
          {(range, onChange, close) => (
            <RangeEditor label={t(chip.label)} range={range} onChange={onChange} close={close} />
          )}
        </ComplexSelector>
      );
    }
    const after = dateOf(boundOf(mine, chip.key, ["after"]));
    const before = dateOf(boundOf(mine, chip.key, ["before"]));
    const label =
      after && before ? `${t(chip.label)} ${shortDate(after)} – ${shortDate(before)}`
      : after ? `${t(chip.label)} ${t("after")} ${shortDate(after)}`
      : before ? `${t(chip.label)} ${t("before")} ${shortDate(before)}`
      : undefined;
    const at = (time: number) => ({ type: "date_absolute" as const, unixSeconds: Math.floor(time / 1000) });
    const picked: DateRange | null = after && before ? { start: isoOf(after.getTime()), end: isoOf(before.getTime() - day) } : null;
    const choose = (key: string) => {
      const [start, end] = key.split("~");
      if (start && end) return replace(chip.key, [{ field: chip.key, operator: "after", value: at(localDay(start)) }, { field: chip.key, operator: "before", value: at(localDay(end) + day) }]);
      const preset = presets.find((candidate) => candidate.key === key);
      if (!preset) return replace(chip.key, []);
      const today = startOfToday();
      replace(chip.key, [
        ...(preset.from == null ? [] : [{ field: chip.key, operator: "after", value: at(today + preset.from * day) }]),
        { field: chip.key, operator: "before", value: at(today + (preset.to === 0 && preset.from != null && preset.from < 0 ? 1 : preset.to) * day) },
      ]);
    };
    return (
      <ComplexSelector<string>
        key={chip.key}
        label={`${t(chip.label)} ${t("filter")}`}
        isLabelHidden
        placeholder={t(chip.label)}
        triggerLabel={label}
        size="sm"
        value=""
        onChange={choose}
        contentXstyle={styles.popover}
        controlXstyle={label ? styles.fill : undefined}>
        {(_value, onChange, close) => (
          <VStack gap={3}>
            <RadioList label={t(chip.label)} value="" onChange={(key) => { onChange(String(key)); close(); }}>
              {presets.map((preset) => (
                <RadioListItem key={preset.key} value={preset.key} label={t(preset.label)} />
              ))}
            </RadioList>
            <Divider />
            <Calendar mode="range" value={picked} hasVariableRowCount
              onChange={(next: DateRange | null) => { if (next?.start && next.end) { onChange(`${next.start}~${next.end}`); close(); } }} />
            <Divider />
            <HStack gap={2} hAlign="between" vAlign="center">
              <Button label={t("Clear")} variant="ghost" size="sm" isDisabled={!label} onClick={() => { onChange(""); close(); }} />
              <Button label={t("Done")} variant="secondary" size="sm" onClick={close} />
            </HStack>
          </VStack>
        )}
      </ComplexSelector>
    );
  });

  return (
    <HStack gap={2} vAlign="center" wrap="nowrap" minHeight={32} role="group" aria-label={t("Filter")} xstyle={styles.filterRow}>
      <StackItem xstyle={styles.searchSlot}>
        <TextInput
          label={t("Search this list")}
          isLabelHidden
          placeholder={t("Search")}
          size="sm"
          width="100%"
          value={draft}
          onChange={setDraft}
          onKeyDown={(event) => event.key === "Enter" && onSearch(draft.trim())}
          onBlur={() => draft.trim() !== search && onSearch(draft.trim())}
          startIcon={MagnifyingGlassIcon}
        />
      </StackItem>
      <StackItem xstyle={styles.overflowSlot}>
        <OverflowList behavior="observeParent" gap={2} minVisibleItems={compact ? 0 : 1} overflowRenderer={(items) => <FilterOverflow items={items} compact={compact} />}>
          {controls}
        </OverflowList>
        <HStack gap={2} vAlign="center" xstyle={styles.meta}>
          <Text type="supporting" color="secondary" hasTabularNumbers>
            {count}
          </Text>
          {onClear && (
            <Link type="supporting" onClick={() => { setDraft(""); onClear(); }}>
              {t("Clear all")}
            </Link>
          )}
        </HStack>
      </StackItem>
    </HStack>
  );
}

function RangeEditor({ label, range, onChange, close }: { label: string; range: Range; onChange: (next: Range) => void; close: () => void }) {
  const [from, setFrom] = useState(range[0]);
  const [to, setTo] = useState(range[1]);
  return (
    <VStack gap={4}>
      <Text type="label">{label}</Text>
      <HStack gap={2}>
        <NumberInput label={t("At least")} size="sm" value={from} onChange={(next) => setFrom(next ?? null)} formatValue={(value) => value.toLocaleString()} />
        <NumberInput label={t("At most")} size="sm" value={to} onChange={(next) => setTo(next ?? null)} formatValue={(value) => value.toLocaleString()} />
      </HStack>
      <Divider />
      <HStack gap={2} hAlign="between" vAlign="center">
        <Button label={t("Clear")} variant="ghost" size="sm" isDisabled={range[0] == null && range[1] == null} onClick={() => { onChange([null, null]); close(); }} />
        <Button label={t("Apply")} variant="secondary" size="sm" onClick={() => { onChange([from, to]); close(); }} />
      </HStack>
    </VStack>
  );
}

function FilterOverflow({ items, compact }: { items: OverflowItem[]; compact: boolean }) {
  const [open, setOpen] = useState(false);
  const label = `${items.length} ${t(items.length === 1 ? "more filter" : "more filters")}`;
  const body = (
    <VStack gap={2} padding={compact ? 4 : 0} paddingBlockStart={0}>
      {items.map(({ child, index }) => (
        <StackItem key={index} xstyle={styles.overflowItem}>
          {child}
        </StackItem>
      ))}
    </VStack>
  );
  const trigger = (
    <Button label={`+${items.length}`} tooltip={label} aria-label={label} variant="ghost" size="sm" xstyle={[styles.chrome, styles.surface]} onClick={compact ? () => setOpen(true) : undefined} />
  );
  if (compact) {
    return (
      <>
        {trigger}
        <BottomSheet isOpen={open} onOpenChange={setOpen} label={label} height="hug" padding={0}>
          {body}
        </BottomSheet>
      </>
    );
  }
  return (
    <Popover placement="below" alignment="end" width={280} label={label} content={body}>
      {trigger}
    </Popover>
  );
}

export const chipChrome = styles;
