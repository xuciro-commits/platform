import { Banner } from "@astryxdesign/core/Banner";
import { BottomSheet } from "@astryxdesign/core/BottomSheet";
import { Button } from "@astryxdesign/core/Button";
import { Divider } from "@astryxdesign/core/Divider";
import { EmptyState } from "@astryxdesign/core/EmptyState";
import { Icon } from "@astryxdesign/core/Icon";
import { IconButton } from "@astryxdesign/core/IconButton";
import { HStack, Layout, LayoutContent, LayoutFooter, LayoutHeader, LayoutPanel, StackItem, VStack } from "@astryxdesign/core/Layout";
import { type FieldDefinition, PowerSearch, type PowerSearchFilter, usePowerSearchConfig } from "@astryxdesign/core/PowerSearch";
import { ProgressBar } from "@astryxdesign/core/ProgressBar";
import { ResizeHandle, useResizable } from "@astryxdesign/core/Resizable";
import { ScrollableArea } from "@astryxdesign/core/ScrollableArea";
import { Section } from "@astryxdesign/core/Section";
import { SegmentedControl, SegmentedControlItem } from "@astryxdesign/core/SegmentedControl";
import { Skeleton } from "@astryxdesign/core/Skeleton";
import { Table, pixel, useTableGroupedRows, useTableSelection, useTableSelectionState, useTableSortable, useTableStickyColumns } from "@astryxdesign/core/Table";
import type { TableColumn, TablePlugin } from "@astryxdesign/core/Table";
import { Heading } from "@astryxdesign/core/Text";
import { ToggleButton } from "@astryxdesign/core/ToggleButton";
import { useMediaQuery } from "@astryxdesign/core/hooks";
import { colorVars } from "@astryxdesign/core/theme/tokens.stylex";
import { AdjustmentsHorizontalIcon, ArrowPathIcon, MagnifyingGlassIcon, TableCellsIcon, ViewColumnsIcon } from "@heroicons/react/24/outline";
import { AdjustmentsHorizontalIcon as AdjustmentsHorizontalSolidIcon } from "@heroicons/react/24/solid";
import * as stylex from "@stylexjs/stylex";
import { type ReactNode, useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { type Read, reread } from "../host";
import { t } from "../i18n";
import { navigate, usePath, useSearch } from "../router";
import { BackLink } from "./BackLink";
import { BoardView } from "./BoardView";
import { type Chip, FilterBar } from "./ListFilters";
import { ListOptions } from "./ListOptions";
import { type Cells, dotOf, idKey, ListCell, ListDetail, targetOf, typeOf } from "./ListDetail";
import { useSavedViews } from "./SavedViews";
import type { Dot } from "./Transitions";
import { type Density, newView, readView, hasFilters, type StickyEdge, type ViewSort, type ViewState } from "./view";

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
  chip?: boolean;
  reference?: (row: Row) => { type: string; id: string } | undefined;
};
type Done = Promise<unknown>;
export type ListGroup<Row> = { key: string; label: string; of: (row: Row) => string; order?: string[] };
export type Move = (id: string, to: string) => Done;
type ListBoard<Row> = {
  key: string;
  lanes: { key: string; label: string }[];
  of: (row: Row) => string;
  accepts?: (id: string, to: string) => boolean;
  move?: Move;
  total?: { label: string; value: (row: Row) => { amount: number; currency?: string } | undefined };
};
export type ListShape<Answer, Row> = {
  title: string;
  about: string;
  rows: (answer: Answer) => Row[];
  id: (row: Row) => string;
  link?: (row: Row) => string;
  columns: ListColumn<Row>[];
  groups?: ListGroup<Row>[];
  board?: ListBoard<Row>;
  bulk?: (ids: string[], clear: () => void) => ReactNode;
  detail?: (id: string) => ReactNode;
  defaultView?: unknown;
  carryView?: boolean;
  page?: { total: number; loaded: number; hasMore: boolean; loading: boolean; loadMore: () => void };
  unfiltered?: number;
  savedViews?: string;
  remote?: "all" | "search";
};

type Raw = Record<string, unknown>;
const compare = (left: unknown, right: unknown) =>
  left instanceof Date && right instanceof Date ? left.getTime() - right.getTime()
  : typeof left === "number" && typeof right === "number" ? left - right
  : String(left ?? "").localeCompare(String(right ?? ""));
const textKey = "__text";
const groupPrefix = "__group_";
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
  tableWidth: (maxWidth: number) => ({ maxWidth }),
  clickable: { cursor: "pointer" },
  activeRow: {
    backgroundColor: colorVars["--color-overlay-pressed"],
    "--table-row-overlay": colorVars["--color-overlay-pressed"],
    "--table-sticky-background": `color-mix(in srgb, ${colorVars["--color-background-surface"]} 90%, ${colorVars["--color-text-primary"]})`,
  },
  surface: { backgroundColor: colorVars["--color-background-surface"], "--table-sticky-background": colorVars["--color-background-surface"] },
});

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
  const [failure, setFailure] = useState("");
  const group = definition.groups?.find((candidate) => candidate.key === view.group);
  const lanes = definition.board;
  const board = view.layout === "board" ? lanes : undefined;
  const orderedColumns = useMemo(() => {
    const ordered = view.columns.flatMap((key) => definition.columns.find((column) => column.key === key) ?? []);
    return ordered.length ? ordered : definition.columns.slice(0, 1);
  }, [definition.columns, view.columns]);

  const chips = useMemo<Chip[]>(
    () =>
      definition.columns.flatMap((column): Chip[] => {
        const base = { key: column.key, label: column.header };
        if (column.chip === false) return [];
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
      const cells: Cells = { [idKey]: definition.id(row), __group: group ? group.of(row) : "", __lane: board ? board.of(row) : "", __href: href && definition.carryView ? href + "?" + params : href };
      const raw: Raw = { [idKey]: cells[idKey] };
      for (const column of definition.columns) {
        cells[column.key] = column.text(row);
        const dot = column.dot?.(row);
        if (dot) cells[dotOf(column.key)] = dot;
        const reference = cells[column.key] ? column.reference?.(row) : undefined;
        if (reference) [cells[typeOf(column.key)], cells[targetOf(column.key)]] = [reference.type, reference.id];
        const value = column.value ? column.value(row) : column.boolean ? cells[column.key] === t("Yes") : cells[column.key];
        raw[column.key] = column.kind === "time" ? (value ? new Date(String(value)) : undefined) : value;
      }
      if (board?.total) {
        const amount = board.total.value(row);
        if (amount) {
          cells.__amount = String(amount.amount);
          cells.__currency = amount.currency ?? "";
        }
      }
      return { cells, raw };
    });
  }, [read, definition, group, board, currentSearch, view]);

  const shown = useMemo(() => {
    if (definition.remote === "all") return all.map((entry) => entry.cells);
    const query = definition.remote === "search" ? "" : view.search.trim().toLowerCase();
    const searched = query ? all.filter((entry) => definition.columns.some((column) => (entry.cells[column.key] ?? "").toLowerCase().includes(query))) : all;
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
  }, [all, view.filters, view.search, view.sort, group, applyFilters, definition.columns, definition.remote]);
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

  const widths = orderedColumns.map((column, position) => column.width ?? (position === 0 ? 300 : column.boolean ? 100
    : column.kind === "time" || column.kind === "number" ? 140 : column.kind === "enum" || column.kind === "code" ? 150 : 200));
  const tableWidth = widths.reduce((sum, width) => sum + width, definition.bulk ? 48 : 0);
  const columns: TableColumn<Cells>[] = orderedColumns.map((column, position) => ({
    key: column.key,
    header: t(column.header),
    width: pixel(widths[position]),
    sortable: true,
    align: column.kind === "number" || column.kind === "time" ? "end" : column.boolean ? "center" : undefined,
    renderCell: (cells) => (group && isHeader(cells) ? null : <ListCell column={column} cells={cells} first={position === 0} />),
  }));
  const sortIndicator: TablePlugin<Cells> = {
    transformHeaderCell: (props) => ({ ...props, xstyle: [...props.xstyle, stylex.defaultMarker()] }),
  };
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
  const params = new URLSearchParams(currentSearch);
  const dirty = hasFilters(view, params.get("domain"));
  const total = definition.page?.total ?? all.length;
  const unfiltered = definition.remote ? definition.unfiltered : definition.page?.hasMore ? undefined : all.length;
  const matched = definition.remote === "all" || !dirty ? total : shown.length;
  const count = dirty && unfiltered !== undefined ? `${matched} / ${unfiltered}`
    : `${matched}${definition.page?.hasMore && definition.remote !== "all" ? "+" : ""}`;
  const clearAll = () => {
    params.delete("domain");
    params.set("view", JSON.stringify({ ...view, filters: [], search: "" }));
    void navigate(currentPath + "?" + params, history.state);
  };
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
      onClear={dirty ? clearAll : undefined}
      compact={compactSurface}
    />
  );
  const onGroup = (key: string) => {
    update({ group: key });
    setCollapsed(new Set());
  };
  const bulk = definition.bulk?.(chosen, () => setSelected(new Set()));
  const ready = read.status === "ready";
  const empty = ready && shown.length === 0;
  const toolbar = definition.columns.length > 0 && read.status !== "error" && read.status !== "refused";

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
                    {(!empty || dirty) && primary}
                  </HStack>
                  {toolbar && (
                    <HStack gap={3} vAlign="center" wrap="wrap" minHeight={32}>
                      <StackItem size="fill" xstyle={styles.toolbarPrimary}>
                        {power ? (
                          <PowerSearch config={config} filters={tokens} label={t("Filter")} placeholder={t("Search or filter by any column")} resultCount={count}
                            onChange={(next) => {
                              const text = next.filter((filter) => filter.field === textKey).at(-1)?.value;
                              update({ filters: next.filter((filter) => filter.field !== textKey), search: text?.type === "string" ? text.value : "" });
                            }} />
                        ) : filterBar}
                      </StackItem>
                      <HStack gap={2} vAlign="center" wrap="wrap" xstyle={styles.toolbarEnd}>
                        <IconButton label={t("Refresh")} tooltip={t("Refresh")} variant="ghost" size="sm" icon={<Icon icon={ArrowPathIcon} size="sm" />} onClick={reread} />
                        {powerToggle}
                        <Divider orientation="vertical" />
                        <HStack gap={1} vAlign="center" role="group" aria-label={t("View options")}>
                          {lanes && (
                            <SegmentedControl size="sm" label={t("Layout")} value={view.layout} onChange={(layout) => update({ layout: layout as ViewState["layout"], lanes: lanes.key })}>
                              <SegmentedControlItem value="table" label={t("Table")} icon={<Icon icon={TableCellsIcon} size="sm" />} />
                              <SegmentedControlItem value="board" label={t("Board")} icon={<Icon icon={ViewColumnsIcon} size="sm" />} />
                            </SegmentedControl>
                          )}
                          <ListOptions view={view} update={update} columns={definition.columns} locked={orderedColumns[0]?.key ?? ""} defaults={fallback.columns}
                            groups={definition.groups ?? []} onGroup={onGroup} compact={compactSurface} />
                          {definition.savedViews && (
                            <>
                              {saved.createButton}
                              <ToggleButton label={t("Saved views")} tooltip={t("Saved views")} isIconOnly size="sm" isPressed={viewsOpen}
                                icon={<Icon icon={saved.toggleIcons.off} size="sm" />} pressedIcon={<Icon icon={saved.toggleIcons.on} size="sm" />}
                                onPressedChange={setViewsOpen} />
                            </>
                          )}
                        </HStack>
                      </HStack>
                    </HStack>
                  )}
                  {viewsOpen && definition.savedViews && saved.bar}
                  {saved.unsaved}
                </VStack>
              </Section>
            </VStack>
          </LayoutHeader>
        }
        footer={bulk && <LayoutFooter padding={0}>{bulk}</LayoutFooter>}
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
            {ready && shown.length === 0 && !dirty && <EmptyState headingLevel={2} title={t("Nothing here yet")} description={t(definition.about)} actions={primary} />}
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
                columns={board.lanes}
                page={definition.page}
                total={board.total?.label}
                cards={shown.map((cells) => ({
                  id: cells[idKey],
                  column: cells.__lane,
                  title: cells[orderedColumns[0].key],
                  href: cells.__href,
                  lines: orderedColumns.slice(1, 5).filter((column) => column.key !== board.key && column.kind !== "time" && cells[column.key]).map((column) => cells[column.key]),
                  amount: cells.__amount === undefined ? undefined : Number(cells.__amount),
                  currency: cells.__currency,
                }))}
                accepts={board.accepts}
                onMove={board.move && ((id, to) => {
                  setFailure("");
                  board.move?.(id, to).catch((refused: Error) => setFailure(refused.message));
                })}
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
                  rowCount={definition.remote === "all" ? definition.page?.total : undefined}
                  density={view.density}
                  dividers="rows"
                  xstyle={styles.tableWidth(tableWidth)}
                  textOverflow="truncate"
                  hasHover
                  plugins={{ ...(definition.bulk ? { selection } : {}), sortable, sortIndicator, sticky, activation, ...(group ? { grouped: grouped.plugin } : {}) }}
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
