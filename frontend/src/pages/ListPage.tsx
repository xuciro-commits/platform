import { Badge } from "@astryxdesign/core/Badge";
import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { CheckboxInput } from "@astryxdesign/core/CheckboxInput";
import { EmptyState } from "@astryxdesign/core/EmptyState";
import { Icon } from "@astryxdesign/core/Icon";
import { IconButton } from "@astryxdesign/core/IconButton";
import { HStack, Layout, LayoutContent, LayoutHeader, StackItem, VStack } from "@astryxdesign/core/Layout";
import { Link } from "@astryxdesign/core/Link";
import { Popover } from "@astryxdesign/core/Popover";
import { type FieldDefinition, PowerSearch, usePowerSearchConfig } from "@astryxdesign/core/PowerSearch";
import { RadioList, RadioListItem } from "@astryxdesign/core/RadioList";
import { ScrollableArea } from "@astryxdesign/core/ScrollableArea";
import { Section } from "@astryxdesign/core/Section";
import { SegmentedControl, SegmentedControlItem } from "@astryxdesign/core/SegmentedControl";
import { Skeleton } from "@astryxdesign/core/Skeleton";
import { StatusDot } from "@astryxdesign/core/StatusDot";
import { Table, pixel, proportional, useTableGroupedRows, useTableSelection, useTableSelectionState, useTableSortable } from "@astryxdesign/core/Table";
import type { TableColumn } from "@astryxdesign/core/Table";
import { Heading, Text } from "@astryxdesign/core/Text";
import { TextInput } from "@astryxdesign/core/TextInput";
import { Timestamp } from "@astryxdesign/core/Timestamp";
import { ArrowDownIcon, ArrowPathIcon, ArrowUpIcon, TableCellsIcon, ViewColumnsIcon } from "@heroicons/react/24/outline";
import { type ReactNode, useEffect, useMemo, useRef, useState } from "react";
import { type Read, reread, useRead } from "../host";
import { navigate, usePath, useSearch } from "../router";
import { BackLink } from "./BackLink";
import { BoardView } from "./BoardView";
import { t } from "../i18n";
import type { Dot } from "./Transitions";
import { newView, readView, type ViewSort, type ViewState } from "./view";
import { SavedViews } from "./SavedViews";

export type ListColumn<Row> = {
  key: string;
  header: string;
  width?: number;
  text: (row: Row) => string;
  kind?: "string" | "enum" | "number" | "time";
  choices?: string[];
  boolean?: boolean;
  dot?: (row: Row) => Dot | undefined;
  value?: (row: Row) => unknown;
  hiddenByDefault?: boolean;
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
  defaultView?: unknown;
  carryView?: boolean;
  page?: { total: number; loaded: number; hasMore: boolean; loading: boolean; loadMore: () => void };
  savedViews?: string;
};
export type ListDefinition<Answer, Row> = ListShape<Answer, Row> & { read: string };

type Cells = Record<string, string>;
type Raw = Record<string, unknown>;
const compare = (left: unknown, right: unknown) =>
  typeof left === "number" && typeof right === "number" ? left - right : String(left ?? "").localeCompare(String(right ?? ""));
const idKey = "__id";
const dotOf = (key: string) => `__dot:${key}`;
const ungrouped = "none";
const rowHeight = 48;
const overscan = 12;

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
  const scrollRef = useRef<HTMLDivElement>(null);
  const [scrollTop, setScrollTop] = useState(0);
  const [viewportHeight, setViewportHeight] = useState(600);
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
  const fields = useMemo(
    () =>
      definition.columns
        .filter((column) => column.kind !== "time")
        .map((column) => ({
          key: column.key,
          label: t(column.header),
          type: column.kind ?? "string",
          enumValues: column.choices?.map((choice) => ({ value: choice, label: choice })),
        })),
    [definition],
  );
  const { config, applyFilters } = usePowerSearchConfig(fields as ReadonlyArray<FieldDefinition>);

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
        raw[column.key] = column.value ? column.value(row) : cells[column.key];
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
    renderGroupHeader: (key, count) => (
      <HStack gap={2} vAlign="center">
        <Text type="body" weight="bold">
          {key}
        </Text>
        <Badge variant="neutral" label={String(count)} />
      </HStack>
    ),
  });

  const { selectionConfig } = useTableSelectionState<Cells>({ data: shown, idKey, selectedKeys: selected, setSelectedKeys: setSelected });
  const selection = useTableSelection<Cells>({ ...selectionConfig, getRowLabel: (cells) => cells[orderedColumns[0]?.key] || cells[idKey] });
  const sortable = useTableSortable<Cells>({ sort: view.sort, onSortChange: (sort: ViewSort[]) => update({ sort }), allowUnsortedState: true });
  const chosen = shown.filter((cells) => selected.has(cells[idKey])).map((cells) => cells[idKey]);

  const columns: TableColumn<Cells>[] = orderedColumns.map((column, position) => ({
    key: column.key,
    header: t(column.header),
    width: column.width ? pixel(column.width) : proportional(1),
    sortable: true,
    align: column.kind === "number" ? ("end" as const) : undefined,
    renderCell: (cells) =>
      position === 0 && cells.__href ? (
        <Link href={cells.__href}>{cells[column.key] || cells[idKey]}</Link>
      ) : column.boolean && cells[column.key] === t("Yes") ? (
        <Icon icon="check" size="sm" label={t("Yes")} />
      ) : column.kind === "time" && cells[column.key] ? (
        <Text type="body" color="secondary">
          <Timestamp value={cells[column.key]} format="relative" />
        </Text>
      ) : cells[dotOf(column.key)] ? (
        <HStack gap={2} vAlign="center">
          <StatusDot variant={cells[dotOf(column.key)] as Dot} label={cells[column.key]} />
          <Text type="body" maxLines={1}>
            {cells[column.key]}
          </Text>
        </HStack>
      ) : (
        <Text type="body" maxLines={1} hasTabularNumbers={column.kind === "number"}>
          {cells[column.key] ?? ""}
        </Text>
      ),
  }));
  const windowed = Boolean(definition.page && !group);
  const first = windowed ? Math.max(0, Math.min(shown.length, Math.floor(scrollTop / rowHeight) - overscan)) : 0;
  const last = windowed ? Math.min(shown.length, first + Math.ceil(viewportHeight / rowHeight) + overscan * 2) : shown.length;
  const tableRows = group ? grouped.data : shown.slice(first, last);
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
  }, [scrollKey, read.status, shown.length, definition.page?.hasMore, definition.page?.loading, definition.page?.loadMore]);

  return (
    <Layout
      height="fill"
      header={
        <LayoutHeader hasDivider padding={4}>
          <VStack gap={4}>
            <HStack gap={2} vAlign="center">
              <StackItem size="fill">
                <VStack gap={2}>
                  {back && <BackLink href={back.href} label={t(back.label)} />}
                  <HStack gap={2} vAlign="center">
                    <Heading level={1}>{t(definition.title)}</Heading>
                    {definition.savedViews && <SavedViews entity={definition.savedViews} view={view} fallback={fallback} />}
                  </HStack>
                </VStack>
              </StackItem>
              {lanes && (
                <SegmentedControl label={t("Layout")} value={view.layout} onChange={(layout) => update({ layout: layout as ViewState["layout"], lanes: lanes?.key ?? "" })}>
                  <SegmentedControlItem value="table" label={t("Table")} icon={<Icon icon={TableCellsIcon} size="sm" />} />
                  <SegmentedControlItem value="board" label={t("Board")} icon={<Icon icon={ViewColumnsIcon} size="sm" />} />
                </SegmentedControl>
              )}
              <IconButton label={t("Refresh")} tooltip={t("Refresh")} icon={<Icon icon={ArrowPathIcon} size="sm" />} onClick={reread} />
              {primary}
            </HStack>
            {all.length > 0 && chosen.length > 0 && (
              <Section variant="muted" padding={2}>
                <HStack gap={3} vAlign="center" wrap="wrap">
                  <Text type="body" hasTabularNumbers>{String(chosen.length)} {t("selected")}</Text>
                  <Button label={t("Clear selection")} variant="ghost" size="sm" onClick={() => setSelected(new Set())} />
                </HStack>
              </Section>
            )}
            {definition.bulk?.(chosen, () => setSelected(new Set()))}
            {all.length > 0 && chosen.length === 0 && (
            <HStack gap={2} vAlign="center">
              <StackItem size="fill">
                <VStack gap={3}>
                  <TextInput label={t("Search this list")} value={view.search} onChange={(search) => update({ search })} />
                  <PowerSearch
                    config={config}
                    filters={view.filters}
                    onChange={(next) => update({ filters: [...next] })}
                    label={t("Filter")}
                    placeholder={t("Filter by any column")}
                    resultCount={String(definition.page ? all.length : shown.length) + " / " + String(definition.page?.total ?? all.length)}
                  />
                </VStack>
              </StackItem>
              {definition.groups && definition.groups.length > 0 && (
                <Popover
                  placement="below"
                  alignment="end"
                  width={320}
                  label={t("Group by")}
                  content={
                    <RadioList
                      label={t("Group by")}
                      value={view.group}
                      onChange={(value) => {
                        update({ group: String(value), lanes: definition.groups?.find((candidate) => candidate.key === String(value) && candidate.order)?.key ?? "" });
                        setCollapsed(new Set());
                      }}>
                      <RadioListItem value={ungrouped} label={t("None")} />
                      {definition.groups.map((candidate) => (
                        <RadioListItem key={candidate.key} value={candidate.key} label={t(candidate.label)} />
                      ))}
                    </RadioList>
                  }>
                  <Button label={group ? `${t("Group by")}: ${t(group.label)}` : t("Group by")} variant="secondary" size="md" />
                </Popover>
              )}
              <Popover
                placement="below"
                alignment="end"
                width={340}
                label={t("Columns")}
                content={
                  <VStack gap={2}>
                    {definition.columns.map((column) => {
                      const at = view.columns.indexOf(column.key);
                      const visible = at >= 0;
                      return (
                        <HStack key={column.key} gap={2} vAlign="center">
                          <StackItem size="fill">
                            <CheckboxInput label={t(column.header)} value={visible} isDisabled={column.key === orderedColumns[0].key}
                              onChange={(checked) => update({ columns: checked ? [...view.columns, column.key] : view.columns.filter((key) => key !== column.key) })} />
                          </StackItem>
                          <IconButton label={t("Move column up")} tooltip={t("Move column up")} variant="ghost" isDisabled={!visible || at <= 1}
                            icon={<Icon icon={ArrowUpIcon} size="sm" />} onClick={() => {
                              const next = [...view.columns];
                              [next[at - 1], next[at]] = [next[at], next[at - 1]];
                              update({ columns: next });
                            }} />
                          <IconButton label={t("Move column down")} tooltip={t("Move column down")} variant="ghost" isDisabled={!visible || at >= view.columns.length - 1}
                            icon={<Icon icon={ArrowDownIcon} size="sm" />} onClick={() => {
                              const next = [...view.columns];
                              [next[at], next[at + 1]] = [next[at + 1], next[at]];
                              update({ columns: next });
                            }} />
                        </HStack>
                      );
                    })}
                  </VStack>
                }>
                <Button label={t("Columns")} variant="secondary" size="md" />
              </Popover>
            </HStack>
            )}
          </VStack>
        </LayoutHeader>
      }
      content={
        <LayoutContent role="main" padding={0}>
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
          {read.status === "ready" && all.length === 0 && (
            <EmptyState headingLevel={2} title={t("Nothing here yet")} description={t(definition.about)} />
          )}
          {read.status === "ready" && all.length > 0 && shown.length === 0 && (
            <EmptyState headingLevel={2} title={t("No matches")} description={t("Try a different filter.")} />
          )}
          {failure && (
            <VStack padding={4}>
              <Banner status="error" title={t("Not saved")} description={failure} />
            </VStack>
          )}
          {read.status === "ready" && shown.length > 0 && board && (
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
          {read.status === "ready" && shown.length > 0 && !board && (
            <ScrollableArea
              ref={scrollRef}
              axis="block"
              role="region"
              label={t("Records")}
              height="100%">
              {windowed && first > 0 && <Section variant="transparent" height={first * rowHeight} padding={0} />}
                <Table
                  data={tableRows}
                  columns={columns}
                  idKey={group ? grouped.idKey : idKey}
                  rowIndexStart={windowed ? first + 1 : undefined}
                  rowCount={definition.page?.total}
                  density="balanced"
                  dividers="rows"
                  textOverflow="truncate"
                  hasHover
                  plugins={{ ...(group ? { grouped: grouped.plugin } : {}), ...(definition.bulk ? { selection } : {}), sortable }}
                />
              {windowed && last < shown.length && <Section variant="transparent" height={(shown.length - last) * rowHeight} padding={0} />}
            </ScrollableArea>
          )}
        </LayoutContent>
      }
    />
  );
}
