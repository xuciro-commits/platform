import { Banner } from "@astryxdesign/core/Banner";
import { BottomSheet } from "@astryxdesign/core/BottomSheet";
import { Button } from "@astryxdesign/core/Button";
import { Divider } from "@astryxdesign/core/Divider";
import { EmptyState } from "@astryxdesign/core/EmptyState";
import { Icon } from "@astryxdesign/core/Icon";
import { IconButton } from "@astryxdesign/core/IconButton";
import { HStack, Layout, LayoutContent, LayoutHeader, LayoutPanel, StackItem, VStack } from "@astryxdesign/core/Layout";
import { Link } from "@astryxdesign/core/Link";
import { MetadataList, MetadataListItem } from "@astryxdesign/core/MetadataList";
import { type FieldDefinition, PowerSearch, type PowerSearchFilter, usePowerSearchConfig } from "@astryxdesign/core/PowerSearch";
import { ProgressBar } from "@astryxdesign/core/ProgressBar";
import { ResizeHandle, useResizable } from "@astryxdesign/core/Resizable";
import { ScrollableArea } from "@astryxdesign/core/ScrollableArea";
import { Section } from "@astryxdesign/core/Section";
import { SegmentedControl, SegmentedControlItem } from "@astryxdesign/core/SegmentedControl";
import { Skeleton } from "@astryxdesign/core/Skeleton";
import { StatusDot } from "@astryxdesign/core/StatusDot";
import { Table, pixel, proportional, useTableGroupedRows, useTableSelection, useTableSelectionState, useTableSortable, useTableStickyColumns } from "@astryxdesign/core/Table";
import type { TableColumn, TablePlugin } from "@astryxdesign/core/Table";
import { Heading, Text } from "@astryxdesign/core/Text";
import { Timestamp } from "@astryxdesign/core/Timestamp";
import { ToggleButton } from "@astryxdesign/core/ToggleButton";
import { useMediaQuery } from "@astryxdesign/core/hooks";
import { colorVars, durationVars, easeVars, radiusVars, spacingVars } from "@astryxdesign/core/theme/tokens.stylex";
import { AdjustmentsHorizontalIcon, ArrowPathIcon, MagnifyingGlassIcon, TableCellsIcon, ViewColumnsIcon, XMarkIcon } from "@heroicons/react/24/outline";
import { AdjustmentsHorizontalIcon as AdjustmentsHorizontalSolidIcon } from "@heroicons/react/24/solid";
import * as stylex from "@stylexjs/stylex";
import { type ReactNode, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { type Read, reread, useRead } from "../host";
import { t } from "../i18n";
import { navigate, usePath, useSearch } from "../router";
import { BackLink } from "./BackLink";
import { BoardView } from "./BoardView";
import { type Chip, FilterBar } from "./ListFilters";
import { ListOptions } from "./ListOptions";
import { type Cells, dotOf, idKey, ListCell, ListDetail, targetOf, typeOf } from "./ListDetail";
import { useSavedViews } from "./SavedViews";
import type { Dot } from "./Transitions";
import { type Density, newView, readView, type StickyEdge, type ViewSort, type ViewState } from "./view";

export type ListColumn<Row> = {
  key: string;
  header: string;
  width?: number;
  text: (row: Row) => string;
  kind?: "string" | "enum" | "number" | "time" | "code" | "member";
  choices?: string[];
  options?: { value: string; label: string }[];
  boolean?: boolean;
  dot?: (row: Row) => Dot | undefined;
  value?: (row: Row) => unknown;
  hiddenByDefault?: boolean;
  reference?: (row: Row) => { type: string; id: string } | undefined;
};
type Done = Promise<unknown>;
export type ListGroup<Row> = { key: string; label: string; of: (row: Row) => string; order?: string[]; accepts?: (id: string, to: string) => boolean };
export type Move = (id: string, group: string, to: string) => Done;
export type ListShape<Answer, Row> = {
  title: string;
  about: string;
  rows: (answer: Answer) => Row[];
  id: (row: Row) => string;
  link?: (row: Row) => string;
  columns: ListColumn<Row>[];
  groups?: ListGroup<Row>[];
  move?: Move;
  bulk?: (ids: string[], clear: () => void) => ReactNode;
  detail?: (id: string) => ReactNode;
  defaultView?: unknown;
  carryView?: boolean;
  page?: { total: number; loaded: number; hasMore: boolean; loading: boolean; loadMore: () => void };
  savedViews?: string;
};
export type ListDefinition<Answer, Row> = ListShape<Answer, Row> & { read: string };

type Raw = Record<string, unknown>;
const compare = (left: unknown, right: unknown) =>
  left instanceof Date && right instanceof Date ? left.getTime() - right.getTime()
  : typeof left === "number" && typeof right === "number" ? left - right
  : String(left ?? "").localeCompare(String(right ?? ""));
const textKey = "__text";
const groupPrefix = "__group_";
const ungrouped = "none";
const overscan = 12;
const heights: Record<Density, number> = { compact: 36, balanced: 48, spacious: 60 };
const compactQuery = "(max-width: 768px), (max-width: 1024px) and (pointer: coarse) and (hover: none)";
const operators: Record<string, string> = { is: "is", is_any_of: "is any of", greater_than_or_equal: "≥", less_than_or_equal: "≤", after: "after", before: "before", contains: "contains", is_true: "is yes", is_false: "is no" };
const stuck = (edge: StickyEdge, keys: string[], fromEnd: boolean) => {
  const count = edge === "one" ? 1 : edge === "two" ? 2 : 0;
  return count === 0 ? [] : fromEnd ? keys.slice(-count) : keys.slice(0, count);
};

const styles = stylex.create({
  headerWrap: { position: "relative" },
  progress: { position: "absolute", insetBlockStart: 0, insetInline: 0 },
  toolbarContainer: { containerName: "toolbar", containerType: "inline-size" },
  toolbarPrimary: { flexBasis: { default: 0, "@container toolbar (max-width: 860px)": "100%" } },
  toolbarEnd: { marginInlineStart: "auto" },
  band: { borderRadius: radiusVars["--radius-element"], backgroundColor: colorVars["--color-background-muted"] },
  bandEnter: {
    opacity: 1,
    transform: "translateY(0)",
    transitionProperty: "opacity, transform",
    transitionDuration: { default: durationVars["--duration-medium"], "@media (prefers-reduced-motion: reduce)": "0s" },
    transitionTimingFunction: easeVars["--ease-standard"],
    "@starting-style": { opacity: 0, transform: `translateY(${spacingVars["--spacing-3"]})` },
  },
  clickable: { cursor: "pointer" },
  activeRow: {
    backgroundColor: colorVars["--color-overlay-pressed"],
    "--table-row-overlay": colorVars["--color-overlay-pressed"],
    "--table-sticky-background": `color-mix(in srgb, ${colorVars["--color-background-surface"]} 90%, ${colorVars["--color-text-primary"]})`,
  },
  surface: { backgroundColor: colorVars["--color-background-surface"], "--table-sticky-background": colorVars["--color-background-surface"] },
});

export function listPage<Answer, Row>(definition: ListDefinition<Answer, Row>) {
  return function ListEntry() {
    return <ListView key={definition.read} definition={{ ...definition, carryView: true }} read={useRead<Answer>(definition.read)} />;
  };
}

type ListViewProps<Answer, Row> = {
  definition: ListShape<Answer, Row>;
  read: Read<Answer>;
  primary?: ReactNode;
  back?: { href: string; label: string };
};

export function ListView<Answer, Row>({ definition, read, primary, back }: ListViewProps<Answer, Row>) {
  const currentPath = usePath();
  const currentSearch = useSearch();
  const compactSurface = useMediaQuery(compactQuery);
  const scrollRef = useRef<HTMLDivElement>(null);
  const [scrollTop, setScrollTop] = useState(0);
  const [viewportHeight, setViewportHeight] = useState(600);
  const [power, setPower] = useState(false);
  const [viewsOpen, setViewsOpen] = useState(false);
  const [active, setActive] = useState<string | null>(null);
  const detailWidth = useResizable({ defaultSize: 380, minSize: 320, maxSize: 560 });
  const fallback = useMemo(() => {
    const base = newView(definition.columns.filter((column) => !column.hiddenByDefault).map((column) => column.key));
    if (definition.defaultView == null) return base;
    return readView(typeof definition.defaultView === "string" ? definition.defaultView : JSON.stringify(definition.defaultView), base);
  }, [definition.columns, definition.defaultView]);
  const view = useMemo(() => readView(new URLSearchParams(currentSearch).get("view"), fallback), [currentSearch, fallback]);
  const [rowHeight, setRowHeight] = useState(heights[view.density]);
  const scrollKey = `list-scroll:${currentPath}:${JSON.stringify(view)}:${new URLSearchParams(currentSearch).get("domain") ?? ""}`;
  const update = (next: Partial<ViewState>) => {
    const changed: ViewState = { ...view, ...next, version: 1 };
    if (JSON.stringify(changed) === JSON.stringify(view)) return;
    const params = new URLSearchParams(currentSearch);
    params.set("view", JSON.stringify(changed));
    navigate(currentPath + "?" + params, history.state);
  };
  const [selected, setSelected] = useState<Set<string>>(() => new Set());
  const [collapsed, setCollapsed] = useState<Set<string>>(() => new Set());
  const [moved, setMoved] = useState<Record<string, string>>({});
  const [failure, setFailure] = useState("");
  const group = definition.groups?.find((candidate) => candidate.key === view.group);
  const lanes = definition.groups?.find((candidate) => candidate.key === view.lanes && candidate.order) ?? (group?.order ? group : definition.groups?.find((candidate) => candidate.order));
  const board = view.layout === "board" && lanes?.order ? lanes : undefined;
  const orderedColumns = useMemo(() => {
    const ordered = view.columns.flatMap((key) => definition.columns.find((column) => column.key === key) ?? []);
    return ordered.length ? ordered : definition.columns.slice(0, 1);
  }, [definition.columns, view.columns]);
  useEffect(() => setMoved({}), [read]);

  const chips = useMemo<Chip[]>(
    () =>
      definition.columns.flatMap((column): Chip[] => {
        const base = { key: column.key, label: column.header };
        if (column.options) return [{ ...base, kind: "many", options: column.options }];
        if (column.boolean) return [{ ...base, kind: "boolean", options: [] }];
        if (column.kind === "enum" && column.choices?.length) return [{ ...base, kind: "many", options: column.choices.map((choice) => ({ value: choice, label: t(choice) })) }];
        if (column.kind === "number") return [{ ...base, kind: "range", options: [] }];
        if (column.kind === "time") return [{ ...base, kind: "date", options: [] }];
        return [];
      }),
    [definition.columns],
  );
  const fields = useMemo(
    () =>
      definition.columns
        .filter((column) => column.kind !== "member" || column.options)
        .map((column) => ({
          key: column.key,
          label: t(column.header),
          type: column.options ? "enum" : column.boolean ? "boolean" : column.kind === "time" ? "date" : column.kind === "code" || column.kind === "member" ? "string" : column.kind ?? "string",
          enumValues: column.options ?? column.choices?.map((choice) => ({ value: choice, label: t(choice) })),
        }))
        .concat({ key: textKey, label: t("Any text"), type: "string", enumValues: undefined }),
    [definition],
  );
  const { config: fieldConfig, applyFilters } = usePowerSearchConfig(fields as ReadonlyArray<FieldDefinition>);
  const config = useMemo(() => ({ ...fieldConfig, contentSearchFieldKey: textKey }), [fieldConfig]);
  const textField = config.fields.find((field) => field.key === textKey);
  const textOperator = textField?.defaultOperator ?? textField?.operators[0]?.key ?? "";
  const tokens = view.search ? [...view.filters, { field: textKey, operator: textOperator, value: { type: "string" as const, value: view.search } }] : view.filters;
  const filterLabel = (filter: PowerSearchFilter) => {
    const column = definition.columns.find((candidate) => candidate.key === filter.field);
    const raw = filter.value as { value?: unknown; unixSeconds?: number };
    const name = (value: unknown) => column?.options?.find((option) => option.value === String(value))?.label ?? t(String(value ?? ""));
    const value =
      typeof raw.unixSeconds === "number" ? new Date(raw.unixSeconds * 1000).toLocaleDateString()
      : Array.isArray(raw.value) ? raw.value.map(name).join(", ")
      : raw.value == null ? ""
      : typeof raw.value === "number" ? raw.value.toLocaleString()
      : name(raw.value);
    return `${t(column?.header ?? filter.field)} ${t(operators[filter.operator] ?? filter.operator)} ${value}`.trim();
  };

  const all = useMemo(() => {
    if (read.status !== "ready") return [];
    return definition.rows(read.data).map((row) => {
      const href = definition.link?.(row) ?? "";
      const params = new URLSearchParams(currentSearch);
      if (definition.carryView) params.set("view", JSON.stringify(view));
      const cells: Cells = { [idKey]: definition.id(row), __group: group ? group.of(row) : "", __lane: lanes ? lanes.of(row) : "", __href: href && definition.carryView ? href + "?" + params : href };
      const raw: Raw = { [idKey]: cells[idKey] };
      for (const column of definition.columns) {
        cells[column.key] = column.text(row);
        const dot = column.dot?.(row);
        if (dot) cells[dotOf(column.key)] = dot;
        const reference = cells[column.key] ? column.reference?.(row) : undefined;
        if (reference) [cells[typeOf(column.key)], cells[targetOf(column.key)]] = [reference.type, reference.id];
        raw[column.key] = column.value ? column.value(row) : column.boolean ? cells[column.key] === t("Yes") : column.kind === "time" && cells[column.key] ? new Date(cells[column.key]) : cells[column.key];
      }
      return { cells, raw };
    });
  }, [read, definition, group, lanes, currentSearch, view]);

  const shown = useMemo(() => {
    if (definition.page) return all.map((entry) => entry.cells);
    const query = view.search.trim().toLowerCase();
    const searched = query ? all.filter((entry) => Object.values(entry.raw).some((value) => String(value ?? "").toLowerCase().includes(query))) : all;
    const passing = new Set((applyFilters(view.filters, searched.map((entry) => entry.raw) as never) as Raw[]).map((raw) => raw[idKey]));
    const kept = searched.filter((entry) => passing.has(entry.cells[idKey]));
    const order = group?.order;
    if (!order && view.sort.length === 0) return kept.map((entry) => entry.cells);
    return [...kept]
      .sort((a, b) => {
        if (order && a.cells.__group !== b.cells.__group) return order.indexOf(a.cells.__group) - order.indexOf(b.cells.__group);
        for (const { sortKey, direction } of view.sort) {
          const difference = compare(a.raw[sortKey], b.raw[sortKey]);
          if (difference !== 0) return direction === "ascending" ? difference : -difference;
        }
        return 0;
      })
      .map((entry) => entry.cells);
  }, [all, view.filters, view.search, view.sort, group, applyFilters]);
  const grouped = useTableGroupedRows<Cells>({
    data: shown,
    groupBy: (cells) => cells.__group,
    collapsedGroups: collapsed,
    onToggleGroup: (key) =>
      setCollapsed((previous) => {
        const next = new Set(previous);
        if (!next.delete(key)) next.add(key);
        return next;
      }),
    getRowKey: (cells) => cells[idKey],
    groupOrder: group?.order,
  });
  const isHeader = (cells: Cells) => grouped.idKey(cells).startsWith(groupPrefix);

  const { selectionConfig } = useTableSelectionState<Cells>({ data: shown, idKey, selectedKeys: selected, setSelectedKeys: setSelected });
  const selection = useTableSelection<Cells>({ ...selectionConfig, getRowLabel: (cells) => cells[orderedColumns[0]?.key] || cells[idKey], hasRowHighlight: false });
  const sortable = useTableSortable<Cells>({ sort: view.sort, onSortChange: (sort: ViewSort[]) => update({ sort }), allowUnsortedState: true, isMultiSortEnabled: true });
  const keys = orderedColumns.map((column) => column.key);
  const sticky = useTableStickyColumns<Cells>({ startKeys: stuck(compactSurface ? "none" : view.stickyStart, keys, false), endKeys: stuck(compactSurface ? "none" : view.stickyEnd, keys, true) });
  const chosen = shown.filter((cells) => selected.has(cells[idKey])).map((cells) => cells[idKey]);
  const activation: TablePlugin<Cells> = {
    transformBodyRow: (props, cells) => {
      if (group && isHeader(cells)) return props;
      const id = cells[idKey];
      const open = id === active;
      return {
        ...props,
        htmlProps: {
          ...props.htmlProps,
          tabIndex: 0,
          "aria-current": open ? true : undefined,
          onClick: (event) => {
            if ((event.target as HTMLElement).closest("input, button, a, select, textarea")) return;
            setActive(id);
          },
          onKeyDown: (event) => {
            if (event.target !== event.currentTarget) return;
            if (event.key === "Enter" || event.key === " ") {
              event.preventDefault();
              setActive(id);
            }
          },
        },
        xstyle: open ? [...props.xstyle, styles.clickable, styles.activeRow] : [...props.xstyle, styles.clickable],
      };
    },
  };

  const columns: TableColumn<Cells>[] = orderedColumns.map((column, position) => ({
    key: column.key,
    header: t(column.header),
    width: column.width ? pixel(column.width) : position === 0 ? proportional(2, { minWidth: 220 }) : proportional(1, { minWidth: 140 }),
    sortable: true,
    align: column.kind === "number" ? ("end" as const) : undefined,
    renderCell: (cells) => (group && isHeader(cells) ? null : <ListCell column={column} cells={cells} first={position === 0} />),
  }));
  const windowed = Boolean(definition.page && !group);
  const first = windowed ? Math.max(0, Math.min(shown.length, Math.floor(scrollTop / rowHeight) - overscan)) : 0;
  const last = windowed ? Math.min(shown.length, first + Math.ceil(viewportHeight / rowHeight) + overscan * 2) : shown.length;
  const tableRows = group ? grouped.data : shown.slice(first, last);
  useLayoutEffect(() => {
    const row = scrollRef.current?.querySelector<HTMLElement>("tbody tr[aria-rowindex], tbody tr");
    const measured = row?.getBoundingClientRect().height;
    if (measured && Math.abs(measured - rowHeight) > 0.5) setRowHeight(measured);
  });
  useEffect(() => {
    const node = scrollRef.current;
    if (!node) return;
    let restoring = Number(sessionStorage.getItem(scrollKey)) || 0;
    let frame = 0;
    const onScroll = () => {
      setScrollTop(node.scrollTop);
      setViewportHeight(node.clientHeight);
      const atEnd = node.scrollHeight - node.scrollTop - node.clientHeight < rowHeight;
      if (restoring > node.scrollTop + rowHeight) {
        if (atEnd && definition.page?.hasMore && !definition.page.loading) definition.page.loadMore();
        return;
      }
      restoring = 0;
      sessionStorage.setItem(scrollKey, String(node.scrollTop));
      if (atEnd && definition.page?.hasMore && !definition.page.loading) definition.page.loadMore();
    };
    node.addEventListener("scroll", onScroll, { passive: true });
    const restore = () => {
      setViewportHeight(node.clientHeight);
      const wanted = Math.min(restoring, node.scrollHeight - node.clientHeight);
      node.scrollTop = wanted;
      setScrollTop(wanted);
      if (restoring > wanted + rowHeight && definition.page?.hasMore && !definition.page.loading) definition.page.loadMore();
      else if (Math.abs(wanted - restoring) < rowHeight) restoring = 0;
    };
    frame = requestAnimationFrame(restore);
    return () => {
      cancelAnimationFrame(frame);
      node.removeEventListener("scroll", onScroll);
    };
  }, [scrollKey, read.status, shown.length, rowHeight, definition.page?.hasMore, definition.page?.loading, definition.page?.loadMore]);

  const saved = useSavedViews({ entity: definition.savedViews ?? "", view, fallback, columnCount: definition.columns.length, filterLabel, compact: compactSurface });
  const count = definition.page ? `${all.length} / ${definition.page.total}` : `${shown.length} / ${all.length}`;
  const dirty = view.filters.length > 0 || view.search !== "";
  const clearAll = () => update({ filters: [], search: "" });
  const powerLabel = power ? t("Switch to filter chips") : t("Advanced search");
  const powerToggle = (
    <ToggleButton
      label={powerLabel}
      tooltip={powerLabel}
      isIconOnly
      size="sm"
      isPressed={power}
      icon={<Icon icon={AdjustmentsHorizontalIcon} size="sm" />}
      pressedIcon={<Icon icon={AdjustmentsHorizontalSolidIcon} size="sm" />}
      onPressedChange={setPower}
    />
  );
  const filterBar = (
    <FilterBar
      chips={chips}
      filters={view.filters}
      onFilters={(filters) => update({ filters })}
      search={view.search}
      onSearch={(search) => update({ search })}
      count={count}
      toggle={powerToggle}
      compact={compactSurface}
    />
  );
  const onGroup = (key: string) => {
    update({ group: key, lanes: definition.groups?.find((candidate) => candidate.key === key && candidate.order)?.key ?? view.lanes });
    setCollapsed(new Set());
  };
  const bulk = definition.bulk?.(chosen, () => setSelected(new Set()));
  const ready = read.status === "ready";

  const activeCells = active ? shown.find((cells) => cells[idKey] === active) : undefined;
  useEffect(() => {
    if (active && ready && !activeCells) setActive(null);
  }, [active, ready, activeCells]);
  const detailBody = activeCells && orderedColumns[0] && (
    <ListDetail columns={definition.columns} title={orderedColumns[0]} cells={activeCells} actions={definition.detail?.(activeCells[idKey])} compact={compactSurface} onClose={() => setActive(null)} />
  );
  const detailPanel = detailBody ? (
    <>
      <ResizeHandle resizable={detailWidth.props} isReversed isAlwaysVisible={false} label={t("Resize details")} />
      <LayoutPanel resizable={detailWidth.props} hasDivider padding={0} role="complementary" label={t("Details")}>
        {detailBody}
      </LayoutPanel>
    </>
  ) : undefined;

  return (
    <>
      <Layout
        height="fill"
        padding={0}
        xstyle={styles.surface}
        end={compactSurface ? undefined : detailPanel}
        header={
          <LayoutHeader hasDivider label={t("List filters and actions")}>
            <VStack gap={0} xstyle={styles.headerWrap}>
              {definition.page?.loading && <ProgressBar label={t("Loading…")} isLabelHidden isIndeterminate xstyle={styles.progress} />}
              <Section variant="transparent" padding={4} xstyle={styles.toolbarContainer}>
                <VStack gap={4}>
                  <HStack gap={3} vAlign="center" wrap="wrap">
                    <StackItem size="fill">
                      <VStack gap={2}>
                        {back && <BackLink href={back.href} label={t(back.label)} />}
                        <Heading level={1}>{t(definition.title)}</Heading>
                      </VStack>
                    </StackItem>
                    {lanes && (
                      <SegmentedControl size="sm" label={t("Layout")} value={view.layout} onChange={(layout) => update({ layout: layout as ViewState["layout"], lanes: lanes?.key ?? "" })}>
                        <SegmentedControlItem value="table" label={t("Table")} icon={<Icon icon={TableCellsIcon} size="sm" />} />
                        <SegmentedControlItem value="board" label={t("Board")} icon={<Icon icon={ViewColumnsIcon} size="sm" />} />
                      </SegmentedControl>
                    )}
                    <IconButton label={t("Refresh")} tooltip={t("Refresh")} variant="ghost" size="sm" icon={<Icon icon={ArrowPathIcon} size="sm" />} onClick={reread} />
                    {primary}
                  </HStack>
                  {ready && (
                    <VStack gap={0} paddingInline={chosen.length ? 3 : 0} paddingBlock={chosen.length ? 1.5 : 0} xstyle={chosen.length ? [styles.band, styles.bandEnter] : undefined}>
                      <HStack gap={3} vAlign="center" wrap="wrap" minHeight={32}>
                        <StackItem size="fill" xstyle={styles.toolbarPrimary}>
                          <VStack gap={2}>
                            {bulk}
                            {chosen.length === 0 && (power ? (
                              <PowerSearch
                                config={config}
                                filters={tokens}
                                onChange={(next) => {
                                  const text = next.filter((filter) => filter.field === textKey).at(-1)?.value;
                                  update({ filters: next.filter((filter) => filter.field !== textKey), search: text?.type === "string" ? text.value : "" });
                                }}
                                label={t("Filter")}
                                placeholder={t("Search or filter by any column")}
                                resultCount={count}
                              />
                            ) : viewsOpen && definition.savedViews ? saved.bar : filterBar)}
                          </VStack>
                        </StackItem>
                        {chosen.length > 0 ? (
                          <HStack gap={3} vAlign="center">
                            <Text type="body" hasTabularNumbers>
                              {String(chosen.length)} {t("selected")}
                            </Text>
                            <Text type="supporting" color="secondary">
                              •
                            </Text>
                            <Button label={t("Clear selection")} variant="ghost" size="sm" onClick={() => setSelected(new Set())} />
                          </HStack>
                        ) : (
                          <HStack gap={3} vAlign="center" xstyle={styles.toolbarEnd}>
                            {!viewsOpen && (
                              <>
                                {power && powerToggle}
                                <ListOptions
                                  view={view}
                                  update={update}
                                  columns={definition.columns}
                                  locked={orderedColumns[0]?.key ?? ""}
                                  defaults={fallback.columns}
                                  groups={definition.groups ?? []}
                                  onGroup={onGroup}
                                  compact={compactSurface}
                                />
                                {definition.savedViews && saved.createButton}
                              </>
                            )}
                            {definition.savedViews && (
                              <ToggleButton
                                label={t("Saved views")}
                                tooltip={t("Saved views")}
                                isIconOnly
                                size="sm"
                                isPressed={viewsOpen}
                                icon={<Icon icon={saved.toggleIcons.off} size="sm" />}
                                pressedIcon={<Icon icon={saved.toggleIcons.on} size="sm" />}
                                onPressedChange={(next) => {
                                  setViewsOpen(next);
                                  if (next) setPower(false);
                                }}
                              />
                            )}
                          </HStack>
                        )}
                      </HStack>
                    </VStack>
                  )}
                </VStack>
              </Section>
            </VStack>
          </LayoutHeader>
        }
        content={
          <LayoutContent role="main" padding={0} label={t(definition.title)}>
            {read.status === "loading" && (
              <VStack gap={2} padding={4}>
                {[0, 1, 2, 3, 4, 5].map((index) => (
                  <Skeleton key={index} height={36} index={index} />
                ))}
              </VStack>
            )}
            {read.status === "error" && (
              <VStack padding={4}>
                <Banner status="error" title={t("This list could not be loaded")} description={read.reason || t("Reload the page to try again.")} />
              </VStack>
            )}
            {read.status === "refused" && <EmptyState headingLevel={2} title={t("You cannot see this list")} description={read.reason || t("Ask an administrator of this workspace for access.")} />}
            {ready && shown.length === 0 && !dirty && <EmptyState headingLevel={2} title={t("Nothing here yet")} description={t(definition.about)} />}
            {ready && shown.length === 0 && dirty && (
              <EmptyState
                headingLevel={2}
                icon={<Icon icon={MagnifyingGlassIcon} size="lg" />}
                title={t("No matches")}
                description={t("Try clearing a filter, or switch to advanced search to build a broader query.")}
                actions={
                  <>
                    <Button label={t("Clear all")} onClick={clearAll} />
                    <Button label={t("Advanced search")} variant="secondary" onClick={() => setPower(true)} />
                  </>
                }
              />
            )}
            {failure && (
              <VStack padding={4}>
                <Banner status="error" title={t("Not saved")} description={failure} />
              </VStack>
            )}
            {ready && shown.length > 0 && board && (
              <BoardView
                columns={board.order ?? []}
                page={definition.page}
                cards={shown.map((cells) => ({
                  id: cells[idKey],
                  column: moved[cells[idKey]] ?? cells.__lane,
                  title: cells[orderedColumns[0].key],
                  href: cells.__href,
                  lines: orderedColumns.slice(1, 5).filter((column) => column.key !== board.key && column.kind !== "time" && cells[column.key]).map((column) => cells[column.key]),
                }))}
                accepts={board.accepts}
                onMove={
                  definition.move &&
                  ((id, to) => {
                    setFailure("");
                    setMoved((previous) => ({ ...previous, [id]: to }));
                    definition.move?.(id, board.key, to).catch((refused: Error) => {
                      setMoved(({ [id]: _, ...rest }) => rest);
                      setFailure(refused.message);
                    });
                  })
                }
              />
            )}
            {ready && shown.length > 0 && !board && (
              <ScrollableArea ref={scrollRef} axis="block" role="region" label={t("Records")} height="100%">
                {windowed && first > 0 && <Section variant="transparent" height={first * rowHeight} padding={0} />}
                <Table
                  data={tableRows}
                  columns={columns}
                  idKey={group ? grouped.idKey : idKey}
                  rowIndexStart={windowed ? first + 1 : undefined}
                  rowCount={definition.page?.total}
                  density={view.density}
                  dividers="rows"
                  textOverflow="truncate"
                  hasHover
                  plugins={{ ...(definition.bulk ? { selection } : {}), sortable, sticky, activation, ...(group ? { grouped: grouped.plugin } : {}) }}
                />
                {windowed && last < shown.length && <Section variant="transparent" height={(shown.length - last) * rowHeight} padding={0} />}
              </ScrollableArea>
            )}
          </LayoutContent>
        }
      />
      <BottomSheet isOpen={compactSurface && Boolean(detailBody)} onOpenChange={(open) => !open && setActive(null)} label={t("Details")} height="tall" padding={0}>
        {detailBody}
      </BottomSheet>
      {saved.dialogs}
    </>
  );
}
