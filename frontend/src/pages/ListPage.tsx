import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { Divider } from "@astryxdesign/core/Divider";
import { EmptyState } from "@astryxdesign/core/EmptyState";
import { Icon } from "@astryxdesign/core/Icon";
import { IconButton } from "@astryxdesign/core/IconButton";
import { HStack, Layout, LayoutContent, LayoutFooter, LayoutHeader, StackItem, VStack } from "@astryxdesign/core/Layout";
import { type FieldDefinition, PowerSearch, type PowerSearchFilter, usePowerSearchConfig } from "@astryxdesign/core/PowerSearch";
import { ProgressBar } from "@astryxdesign/core/ProgressBar";
import { Section } from "@astryxdesign/core/Section";
import { SegmentedControl, SegmentedControlItem } from "@astryxdesign/core/SegmentedControl";
import { Skeleton } from "@astryxdesign/core/Skeleton";
import { Table, proportional, useTableSelection, useTableSelectionState, useTableSortable, useTableStickyColumns } from "@astryxdesign/core/Table";
import type { TableColumn, TablePlugin } from "@astryxdesign/core/Table";
import { ToggleButton } from "@astryxdesign/core/ToggleButton";
import { useMediaQuery } from "@astryxdesign/core/hooks";
import { colorVars } from "@astryxdesign/core/theme/tokens.stylex";
import { AdjustmentsHorizontalIcon, ArrowPathIcon, ArrowsPointingInIcon, ArrowsPointingOutIcon, Bars3BottomLeftIcon, CalendarDaysIcon, ChartBarIcon, MagnifyingGlassIcon, TableCellsIcon, ViewColumnsIcon } from "@heroicons/react/24/outline";
import { AdjustmentsHorizontalIcon as AdjustmentsHorizontalSolidIcon } from "@heroicons/react/24/solid";
import * as stylex from "@stylexjs/stylex";
import { lazy, type ReactNode, Suspense, useEffect, useMemo, useState } from "react";
import { HostRefusal, type Read, reread } from "../host";
import { dayText, t } from "../i18n";
import { navigate, usePath, useSearch } from "../router";
import { PageTitle, useGutter } from "./frame";
import { motionStyles } from "./motion";
import { useListWindow } from "./ListWindow";
import { CellProperty } from "./PropertyField";
import { FilterBar } from "./ListFilters";
import { ListOptions } from "./ListOptions";
import { type Cells, dotOf, idKey, ListCell, ListDetail, rawOf, targetOf, typeOf } from "./ListDetail";
import { buildFilterChips, columnWidthSpec, formatFilterLabel, prepareRows, type RawRow, sortPreparedRows, stuck } from "./listModel";
import { useSavedViews } from "./SavedViews";
import type { Dot } from "./Transitions";
import { useNestedGroupedRows } from "./useNestedGroupedRows";
import { type GanttScale, newView, readView, hasFilters, shownMonth, type ViewSort, type ViewState } from "./view";

import type { ListShape } from "./listShape";
export type { ListColumn, ListGroup, ListShape, Move } from "./listShape";

const BoardView = lazy(() => import("./BoardView").then((module) => ({ default: module.BoardView })));
const CalendarView = lazy(() => import("./CalendarView").then((module) => ({ default: module.CalendarView })));
const GanttView = lazy(() => import("./GanttView").then((module) => ({ default: module.GanttView })));
const SummaryView = lazy(() => import("./SummaryView").then((module) => ({ default: module.SummaryView })));

const textKey = "__text";
const fillKey = "__fill";
const compactQuery = "(max-width: 768px), (max-width: 1024px) and (pointer: coarse) and (hover: none)";

const styles = stylex.create({
  headerWrap: { position: "relative" },
  progress: { position: "absolute", insetBlockStart: 0, insetInline: 0 },
  toolbarContainer: { containerName: "toolbar", containerType: "inline-size" },
  toolbarPrimary: { flexBasis: { default: 0, "@container toolbar (max-width: 860px)": "100%" } },
  toolbarEnd: { marginInlineStart: "auto" },
  clickable: { cursor: "pointer" },
  activeRow: {
    backgroundColor: colorVars["--color-overlay-pressed"],
    "--table-row-overlay": colorVars["--color-overlay-pressed"],
    "--table-sticky-background": colorVars["--color-background-muted"],
  },
  editable: { cursor: "text" },
  surface: { backgroundColor: colorVars["--color-background-surface"], "--table-sticky-background": colorVars["--color-background-surface"] },
});

type ListViewProps<Answer, Row> = {
  definition: ListShape<Answer, Row>;
  read: Read<Answer>;
  primary?: ReactNode;
  back?: { href: string; label: string };
  exportRef?: { current: (() => { headers: string[]; rows: string[][] }) | null };
};

export function ListView<Answer, Row>({ definition, read, primary, back, exportRef }: ListViewProps<Answer, Row>) {
  const currentPath = usePath();
  const currentSearch = useSearch();
  const gutter = useGutter();
  const compactSurface = useMediaQuery(compactQuery);
  const [power, setPower] = useState(false);
  const [viewsOpen, setViewsOpen] = useState(false);
  const [active, setActive] = useState<string | null>(null);
  const [editing, setEditing] = useState<{ id: string; key: string } | null>(null);
  const fallback = useMemo(() => {
    const base = newView(definition.columns.filter((column) => !column.hiddenByDefault).map((column) => column.key));
    if (definition.defaultView == null) return base;
    return readView(typeof definition.defaultView === "string" ? definition.defaultView : JSON.stringify(definition.defaultView), base);
  }, [definition.columns, definition.defaultView]);
  const view = useMemo(() => readView(new URLSearchParams(currentSearch).get("view"), fallback), [currentSearch, fallback]);
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
  const moveFailed = (refused: Error) => {
    if (refused instanceof HostRefusal && refused.reasonCode === "stale_revision") reread();
    setFailure(refused.message);
  };
  const group = definition.groups?.find((candidate) => candidate.key === view.group);
  const subGroup = group && view.subGroup !== "none" && view.subGroup !== group.key ? definition.groups?.find((candidate) => candidate.key === view.subGroup) : undefined;
  const activeGroup = group?.key ?? "none";
  const activeSubGroup = subGroup?.key ?? "none";
  const effectiveView = useMemo(() => ({ ...view, group: activeGroup, subGroup: activeSubGroup }), [view, activeGroup, activeSubGroup]);
  const lanes = definition.board;
  const board = view.layout === "board" ? lanes : undefined;
  const calendar = view.layout === "calendar" && definition.calendar?.fields.length ? definition.calendar : undefined;
  const gantt = view.layout === "gantt" && definition.gantt ? definition.gantt : undefined;
  const summary = view.layout === "summary" && definition.summary ? definition.summary : undefined;
  const dateField = calendar ? (calendar.fields.find((field) => field.key === view.dates) ?? calendar.fields[0]).key : "";
  const orderedColumns = useMemo(() => {
    const ordered = view.columns.flatMap((key) => definition.columns.find((column) => column.key === key) ?? []);
    const shownColumns = ordered.filter((column, position) => position === 0 || (column.key !== activeGroup && column.key !== activeSubGroup));
    return shownColumns.length ? shownColumns : definition.columns.slice(0, 1);
  }, [definition.columns, view.columns, activeGroup, activeSubGroup]);

  const chips = useMemo(() => buildFilterChips(orderedColumns), [orderedColumns]);
  const fields = useMemo(
    () =>
      definition.columns
        .filter((column) => column.kind !== "member" || column.options)
        .map((column) => ({
          key: column.key,
          label: t(column.header),
          type: column.options ? "enum" : column.boolean ? "boolean" : column.kind === "time" || column.kind === "date" ? "date" : column.kind === "enum" ? "enum" : column.kind === "number" || column.kind === "money" || column.kind === "percent" ? "number" : "string",
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
  const filterLabel = (filter: PowerSearchFilter) => formatFilterLabel(definition.columns, filter);

  const all = useMemo(
    () => (read.status === "ready" ? prepareRows({ data: read.data, definition, group, subGroup, board, calendar, gantt, dateField, currentSearch, view }) : []),
    [read, definition, group, subGroup, board, calendar, gantt, dateField, currentSearch, view],
  );
  const rowOf = useMemo(() => new Map(all.map((entry) => [entry.cells[idKey], entry.row])), [all]);

  const shown = useMemo(() => {
    if (definition.remote === "all") return sortPreparedRows(all, view.sort, group?.order, subGroup?.order);
    const query = definition.remote === "search" ? "" : view.search.trim().toLowerCase();
    const searched = query ? all.filter((entry) => definition.columns.some((column) => (entry.cells[column.key] ?? "").toLowerCase().includes(query))) : all;
    const passing = new Set((applyFilters(view.filters, searched.map((entry) => entry.raw) as never) as RawRow[]).map((raw) => raw[idKey]));
    return sortPreparedRows(searched.filter((entry) => passing.has(entry.cells[idKey])), view.sort, group?.order, subGroup?.order);
  }, [all, view.filters, view.search, view.sort, group, subGroup, applyFilters, definition.columns, definition.remote]);
  if (exportRef) exportRef.current = () => ({ headers: orderedColumns.map((column) => t(column.header)), rows: shown.map((cells) => orderedColumns.map((column) => cells[column.key] ?? "")) });
  const grouped = useNestedGroupedRows<Cells>({
    data: shown,
    primary: group ? { label: group.label, of: (cells) => cells.__group, order: group.order } : undefined,
    secondary: subGroup ? { label: subGroup.label, of: (cells) => cells.__subGroup, order: subGroup.order } : undefined,
    collapsedGroups: collapsed,
    onToggleGroup: (key) =>
      setCollapsed((previous) => {
        const next = new Set(previous);
        if (!next.delete(key)) {
          next.add(key);
          for (const childKey of grouped.childrenOf(key)) next.add(childKey);
        }
        return next;
      }),
    getRowKey: (cells) => cells[idKey],
    firstColumnKey: orderedColumns[0]?.key,
    hasSelection: Boolean(definition.bulk),
  });
  const isHeader = grouped.isHeader;

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
        xstyle: open ? [...props.xstyle, styles.clickable, motionStyles.pressableRow, styles.activeRow] : [...props.xstyle, styles.clickable, motionStyles.pressableRow],
      };
    },
  };

  const editableKeys = orderedColumns.slice(1).filter((column) => Boolean(column.edit)).map((column) => column.key);
  const advanceCell = (rowId: string, key: string, shift: boolean) => {
    const colIdx = editableKeys.indexOf(key);
    const rowIds = shown.map((cells) => cells[idKey]);
    const rowIdx = rowIds.indexOf(rowId);
    if (colIdx < 0 || rowIdx < 0) return;
    const step = shift ? -1 : 1;
    const nextCol = colIdx + step;
    if (nextCol >= 0 && nextCol < editableKeys.length) { setActive(rowId); setEditing({ id: rowId, key: editableKeys[nextCol] }); return; }
    const targetId = rowIds[rowIdx + step];
    if (targetId) { setActive(targetId); setEditing({ id: targetId, key: shift ? editableKeys[editableKeys.length - 1] : editableKeys[0] }); }
  };

  const groupDepth = subGroup ? 2 : group ? 1 : 0;
  const sizedColumns = orderedColumns.map((column, position) => ({
    column,
    position,
    sizing: columnWidthSpec(column, position, compactSurface, groupDepth),
  }));
  const hasFlexColumn = sizedColumns.some((entry) => entry.sizing.flex);
  const columns: TableColumn<Cells>[] = [
    ...sizedColumns.map(({ column, position, sizing }): TableColumn<Cells> => ({
      key: column.key,
      header: t(column.header),
      width: sizing.spec,
      sortable: true,
      align: column.kind === "number" || column.kind === "money" || column.kind === "percent" || column.kind === "quantity" || column.kind === "time" || column.kind === "date" ? "end" : undefined,
      renderCell: (cells) => {
        if (group && isHeader(cells)) return null;
        const row = rowOf.get(cells[idKey]);
        return column.edit && row && editing?.id === cells[idKey] && editing.key === column.key
          ? <CellProperty edit={column.edit} row={row} close={() => setEditing(null)} onTab={(shift) => advanceCell(cells[idKey], column.key, shift)} />
          : <ListCell column={column} cells={cells} first={position === 0} />;
      },
    })),
    ...(hasFlexColumn ? [] : [{ key: fillKey, header: "", width: proportional(1, { minWidth: 0 }), renderCell: () => null }]),
  ];
  const sortIndicator: TablePlugin<Cells> = {
    transformHeaderCell: (props) => ({ ...props, xstyle: [...props.xstyle, stylex.defaultMarker()] }),
  };
  const rows = group ? grouped.data : shown;
  const { first, last, plugin: windowing } = useListWindow<Cells>({ count: rows.length, density: view.density, scrollKey, ready: read.status, page: definition.page });
  const frame: TablePlugin<Cells> = {
    transformBodyCell: (props, column, cells) => {
      if (column.key === orderedColumns[0]?.key || !definition.columns.find((candidate) => candidate.key === column.key)?.edit || (group && isHeader(cells))) return props;
      const isActive = active === cells[idKey];
      return {
        ...props,
        htmlProps: {
          ...props.htmlProps,
          onClick: isActive ? (event) => {
            event.stopPropagation();
            setEditing({ id: cells[idKey], key: column.key });
          } : props.htmlProps.onClick,
          onDoubleClick: (event) => {
            event.stopPropagation();
            setActive(cells[idKey]);
            setEditing({ id: cells[idKey], key: column.key });
          },
        },
        xstyle: isActive ? [...props.xstyle, styles.editable] : props.xstyle,
      };
    },
  };

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
  const onGroups = (primary: string, secondary: string) => {
    const nextSub = primary === "none" || secondary === primary ? "none" : secondary;
    update({ group: primary, subGroup: nextSub });
    setCollapsed(new Set());
  };
  const allGroupsCollapsed = grouped.allKeys.length > 0 && grouped.allKeys.every((key) => collapsed.has(key));
  const collapseActions = useMemo(() => ({
    allCollapsed: allGroupsCollapsed,
    hasSubGroups: Boolean(subGroup && grouped.l2Keys.length > 0),
    onExpandAll: () => setCollapsed(new Set()),
    onExpandLevelOne: () => setCollapsed(new Set(grouped.l2Keys)),
    onCollapseAll: () => setCollapsed(new Set(grouped.allKeys)),
  }), [allGroupsCollapsed, subGroup, grouped.l2Keys, grouped.allKeys]);
  const bulk = definition.bulk?.(chosen, () => setSelected(new Set()));
  const ready = read.status === "ready";
  const empty = ready && shown.length === 0;
  const toolbar = definition.columns.length > 0 && read.status !== "error" && read.status !== "refused"
    && (!empty || dirty || definition.remote === "search" || Boolean(lanes) || Boolean(definition.calendar?.fields.length) || Boolean(definition.gantt));

  const activeCells = active ? shown.find((cells) => cells[idKey] === active) : undefined;
  const activeUnscheduled = Boolean(active && gantt?.unscheduled.some((item) => item.id === active));
  useEffect(() => {
    if (active && ready && !activeCells && !activeUnscheduled) setActive(null);
  }, [active, ready, activeCells, activeUnscheduled]);

  return (
    <>
      <Layout
        height="fill"
        padding={0}
        xstyle={styles.surface}
        header={
          <LayoutHeader label={t("List filters and actions")}>
            <VStack gap={0} xstyle={styles.headerWrap}>
              {definition.page?.loading && <ProgressBar label={t("Loading…")} isLabelHidden isIndeterminate xstyle={styles.progress} />}
              <Section variant="transparent" padding={gutter} paddingBlockStart={3} paddingBlockEnd={3} xstyle={styles.toolbarContainer}>
                <VStack gap={4}>
                  <PageTitle title={t(definition.title)} back={back && { href: back.href, label: t(back.label) }} actions={(!empty || dirty || Boolean(board) || Boolean(calendar) || Boolean(gantt)) && primary} />
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
                        <HStack gap={1} vAlign="center" wrap="wrap" role="group" aria-label={t("View options")}>
                          {(lanes || Boolean(definition.calendar?.fields.length) || Boolean(definition.gantt) || Boolean(definition.summary)) && (
                            <SegmentedControl size="sm" label={t("Layout")} value={view.layout} onChange={(layout) => update({ layout: layout as ViewState["layout"], lanes: lanes?.key ?? view.lanes })}>
                              <SegmentedControlItem value="table" label={t("Table")} icon={<Icon icon={TableCellsIcon} size="sm" />} />
                              {lanes && <SegmentedControlItem value="board" label={t("Board")} icon={<Icon icon={ViewColumnsIcon} size="sm" />} />}
                              {Boolean(definition.calendar?.fields.length) && <SegmentedControlItem value="calendar" label={t("Calendar")} icon={<Icon icon={CalendarDaysIcon} size="sm" />} />}
                              {Boolean(definition.gantt) && <SegmentedControlItem value="gantt" label={t("Gantt")} icon={<Icon icon={Bars3BottomLeftIcon} size="sm" />} />}
                              {Boolean(definition.summary) && <SegmentedControlItem value="summary" label={t("Summary")} icon={<Icon icon={ChartBarIcon} size="sm" />} />}
                            </SegmentedControl>
                          )}
                          {group && grouped.l1Keys.length > 0 && view.layout === "table" && (
                            <Button
                              label={allGroupsCollapsed ? t("Expand all") : t("Collapse all")}
                              variant="ghost"
                              size="sm"
                              icon={<Icon icon={allGroupsCollapsed ? ArrowsPointingOutIcon : ArrowsPointingInIcon} size="sm" />}
                              onClick={allGroupsCollapsed ? collapseActions.onExpandAll : collapseActions.onCollapseAll}
                            />
                          )}
                          <ListOptions view={view} update={update} columns={definition.columns} locked={orderedColumns[0]?.key ?? ""} defaults={fallback.columns}
                            groups={definition.groups ?? []} onGroups={onGroups} collapseActions={collapseActions} compact={compactSurface} />
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
                  {definition.savedViews && (
                    <VStack
                      ref={(node) => { if (node) node.inert = !viewsOpen; }}
                      aria-hidden={!viewsOpen || undefined}
                      xstyle={[motionStyles.collapseGrid, viewsOpen && motionStyles.collapseOpen]}
                    >
                      <VStack xstyle={motionStyles.collapseInner}>{saved.bar}</VStack>
                    </VStack>
                  )}
                  {saved.unsaved}
                </VStack>
              </Section>
            </VStack>
          </LayoutHeader>
        }
        footer={bulk && <LayoutFooter padding={0}>{bulk}</LayoutFooter>}
        content={
          <Layout
            height="fill"
            padding={0}
            end={!compactSurface ? (
              <ListDetail
                recordType={definition.peekType}
                activeId={active}
                columns={definition.columns}
                title={orderedColumns[0]}
                cells={activeCells}
                actions={activeCells ? definition.detail?.(activeCells[idKey]) : undefined}
                surface="panel"
                onClose={() => setActive(null)}
              />
            ) : undefined}
            content={
              <LayoutContent role="main" padding={0} label={t(definition.title)}>
            {read.status === "loading" && (
              <VStack gap={2} paddingInline={gutter}>
                {[0, 1, 2, 3, 4, 5].map((index) => (
                  <Skeleton key={index} height={36} index={index} />
                ))}
              </VStack>
            )}
            {read.status === "error" && (
              <VStack paddingInline={gutter}>
                <Banner status="error" title={t("This list could not be loaded")} description={read.reason || t("Reload the page to try again.")} />
              </VStack>
            )}
            {read.status === "refused" && <EmptyState headingLevel={2} title={t("You cannot see this list")} description={read.reason || t("Ask an administrator of this workspace for access.")} />}
            {ready && shown.length === 0 && !dirty && !calendar && !board && !gantt && <EmptyState headingLevel={2} title={t("Nothing here yet")} description={t(definition.about)} actions={primary} />}
            {ready && shown.length === 0 && dirty && !calendar && !board && !gantt && (
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
              <VStack paddingInline={gutter} paddingBlockEnd={4} xstyle={motionStyles.bannerEnter}>
                <Banner status="error" title={t("Not saved")} description={failure} />
              </VStack>
            )}
            {ready && board && (
              <Suspense fallback={<VStack gap={2} paddingInline={gutter}><Skeleton height={240} /></VStack>}>
                <BoardView
                  columns={board.lanes}
                  page={definition.page}
                  total={board.total?.label}
                  cards={shown.map((cells) => {
                    const ownerCol = definition.columns.find((column) => column.kind === "member" && cells[column.key]);
                    const timeCol = definition.columns.find((column) => column.kind === "time" && cells[column.key]);
                    return {
                      id: cells[idKey],
                      column: cells.__lane,
                      title: cells[orderedColumns[0].key],
                      href: cells.__href,
                      lines: orderedColumns.slice(1, 5).filter((column) => column.key !== board.key && column.kind !== "time" && column.key !== ownerCol?.key && cells[column.key]).map((column) => ({
                        label: t(column.header),
                        value: column.kind === "date" ? dayText(cells[column.key]) : cells[column.key],
                        kind: column.kind,
                        raw: cells[rawOf(column.key)],
                        refType: cells[typeOf(column.key)],
                        refId: cells[targetOf(column.key)],
                        dot: cells[dotOf(column.key)] as Dot | undefined,
                      })),
                      amount: cells.__amount === undefined ? undefined : Number(cells.__amount),
                      currency: cells.__currency,
                      owner: ownerCol ? cells[ownerCol.key] : undefined,
                      ownerId: ownerCol ? cells[targetOf(ownerCol.key)] : undefined,
                      updated: timeCol ? cells[timeCol.key] : undefined,
                      related: cells.__related || undefined,
                    };
                  })}
                  accepts={board.accepts}
                  onCreate={board.create}
                  onMove={board.move && ((id, to) => { setFailure(""); return board.move?.(id, to).catch((err: Error) => { moveFailed(err); throw err; }); })}
                />
              </Suspense>
            )}
            {ready && calendar && (
              <Suspense fallback={<VStack gap={2} paddingInline={gutter}><Skeleton height={240} /></VStack>}>
                <CalendarView
                  month={shownMonth(view)}
                  field={dateField}
                  fields={calendar.fields}
                  onMonth={(month) => update({ month })}
                  onField={(dates) => update({ dates })}
                  page={definition.page}
                  items={shown.map((cells) => ({ id: cells[idKey], day: cells.__day, title: cells[orderedColumns[0].key], href: cells.__href }))}
                  accepts={calendar.accepts}
                  onMove={calendar.move && ((id, day) => { setFailure(""); calendar.move?.(id, dateField, day).catch(moveFailed); })}
                />
              </Suspense>
            )}
            {ready && gantt && (
              <Suspense fallback={<VStack gap={2} paddingInline={gutter}><Skeleton height={240} /></VStack>}>
                <GanttView
                  month={shownMonth(view)} scale={view.scale} startField={gantt.startField} endField={gantt.endField} fields={gantt.fields}
                  grouped={Boolean(group)} groupOrder={group?.order} activeId={active} page={definition.page} unscheduled={gantt.unscheduled}
                  onMonth={(month) => update({ month })} onScale={(scale: GanttScale) => update({ scale })} onStartField={(dates) => update({ dates })} onEndField={(until) => update({ until })} onSelect={setActive}
                  items={shown.filter((cells) => Boolean(cells.__start && cells.__end)).map((cells) => ({ id: cells[idKey], title: cells[orderedColumns[0].key], start: cells.__start, end: cells.__end, after: cells.__after || undefined, group: cells.__group || undefined, dot: (cells.__ganttDot || undefined) as Dot | undefined, href: cells.__href }))}
                  accepts={gantt.accepts}
                  onMove={gantt.move && ((id, start, end) => { setFailure(""); return gantt.move?.(id, gantt.startField, gantt.endField, start, end).catch((err: Error) => { moveFailed(err); throw err; }); })}
                />
              </Suspense>
            )}
            {ready && summary && (
              <Suspense fallback={<VStack gap={2} paddingInline={gutter}><Skeleton height={240} /></VStack>}>
                <SummaryView
                  described={summary.described}
                  listPath={summary.listPath}
                  view={view}
                  baseDomain={summary.baseDomain}
                  options={summary.options}
                  update={update}
                />
              </Suspense>
            )}
            {ready && shown.length > 0 && !board && !calendar && !gantt && !summary && (
              <VStack key="table" height="100%" paddingInline={gutter} xstyle={motionStyles.fadeEnter}>
                <Table
                  data={rows.slice(first, last)}
                  columns={columns}
                  idKey={group ? grouped.idKey : idKey}
                  rowIndexStart={first + 1}
                  rowCount={definition.remote === "all" ? definition.page?.total : undefined}
                  density={view.density}
                  dividers="rows"
                  textOverflow="truncate"
                  hasHover
                  plugins={{ ...(definition.bulk ? { selection } : {}), sortable, sortIndicator, sticky, activation, ...(group ? { grouped: grouped.plugin } : {}), frame, windowing }}
                />
              </VStack>
            )}
              </LayoutContent>
            }
          />
        }
      />
      {compactSurface && (
        <ListDetail
          recordType={definition.peekType}
          activeId={active}
          columns={definition.columns}
          title={orderedColumns[0]}
          cells={activeCells}
          actions={activeCells ? definition.detail?.(activeCells[idKey]) : undefined}
          surface="drawer"
          onClose={() => setActive(null)}
        />
      )}
      {saved.dialogs}
    </>
  );
}
