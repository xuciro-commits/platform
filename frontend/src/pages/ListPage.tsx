import { Badge } from "@astryxdesign/core/Badge";
import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { EmptyState } from "@astryxdesign/core/EmptyState";
import { Icon } from "@astryxdesign/core/Icon";
import { IconButton } from "@astryxdesign/core/IconButton";
import { HStack, Layout, LayoutContent, LayoutHeader, StackItem, VStack } from "@astryxdesign/core/Layout";
import { Link } from "@astryxdesign/core/Link";
import { Popover } from "@astryxdesign/core/Popover";
import { type FieldDefinition, PowerSearch, type PowerSearchFilter, usePowerSearchConfig } from "@astryxdesign/core/PowerSearch";
import { RadioList, RadioListItem } from "@astryxdesign/core/RadioList";
import { Section } from "@astryxdesign/core/Section";
import { SegmentedControl, SegmentedControlItem } from "@astryxdesign/core/SegmentedControl";
import { Skeleton } from "@astryxdesign/core/Skeleton";
import { StatusDot } from "@astryxdesign/core/StatusDot";
import { Table, pixel, proportional, useTableGroupedRows, useTableSelection, useTableSelectionState, useTableSortable } from "@astryxdesign/core/Table";
import type { TableColumn } from "@astryxdesign/core/Table";
import { Heading, Text } from "@astryxdesign/core/Text";
import { Timestamp } from "@astryxdesign/core/Timestamp";
import { ArrowPathIcon, TableCellsIcon, ViewColumnsIcon } from "@heroicons/react/24/outline";
import { type ReactNode, useEffect, useMemo, useState } from "react";
import { type Read, reread, useRead } from "../host";
import { BackLink } from "./BackLink";
import { BoardView } from "./BoardView";
import { t } from "../i18n";
import type { Dot } from "./Transitions";

export type ListColumn<Row> = {
  key: string;
  header: string;
  width?: number;
  text: (row: Row) => string;
  kind?: "string" | "enum" | "number" | "time";
  choices?: string[];
  dots?: Record<string, Dot>;
  value?: (row: Row) => unknown;
};
type Done = Promise<unknown>;
export type BulkAction = { label: string; run: (ids: string[]) => Done };
export type ListGroup<Row> = { key: string; label: string; of: (row: Row) => string; order?: string[] };
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
  bulk?: BulkAction[];
};
export type ListDefinition<Answer, Row> = ListShape<Answer, Row> & { read: string };

type Cells = Record<string, string>;
type Raw = Record<string, unknown>;
type Sort = { sortKey: string; direction: "ascending" | "descending" }[];
const compare = (left: unknown, right: unknown) =>
  typeof left === "number" && typeof right === "number" ? left - right : String(left ?? "").localeCompare(String(right ?? ""));
const idKey = "__id";
const ungrouped = "none";
const layouts = ["table", "board"];

export function listPage<Answer, Row>(definition: ListDefinition<Answer, Row>) {
  return function ListEntry() {
    return <ListView definition={definition} read={useRead<Answer>(definition.read)} />;
  };
}

type ListViewProps<Answer, Row> = {
  definition: ListShape<Answer, Row>;
  read: Read<Answer>;
  primary?: ReactNode;
  back?: { href: string; label: string };
};

export function ListView<Answer, Row>({ definition, read, primary, back }: ListViewProps<Answer, Row>) {
  const [filters, setFilters] = useState<PowerSearchFilter[]>([]);
  const [sort, setSort] = useState<Sort>([]);
  const [selected, setSelected] = useState<Set<string>>(() => new Set());
  const [groupKey, setGroupKey] = useState(ungrouped);
  const [collapsed, setCollapsed] = useState<Set<string>>(() => new Set());
  const [layout, setLayout] = useState(layouts[0]);
  const [moved, setMoved] = useState<Record<string, string>>({});
  const [failure, setFailure] = useState("");
  const [busy, setBusy] = useState(false);
  const group = definition.groups?.find((candidate) => candidate.key === groupKey);
  const lanes = group?.order ? group : definition.groups?.find((candidate) => candidate.order);
  const board = layout === "board" && lanes?.order ? lanes : undefined;
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
      const cells: Cells = { [idKey]: definition.id(row), __group: group ? group.of(row) : "", __lane: lanes ? lanes.of(row) : "", __href: definition.link ? definition.link(row) : "" };
      const raw: Raw = { [idKey]: cells[idKey] };
      for (const column of definition.columns) {
        cells[column.key] = column.text(row);
        raw[column.key] = column.value ? column.value(row) : cells[column.key];
      }
      return { cells, raw };
    });
  }, [read, definition, group, lanes]);

  const shown = useMemo(() => {
    const passing = new Set((applyFilters(filters, all.map((entry) => entry.raw) as never) as Raw[]).map((raw) => raw[idKey]));
    const kept = all.filter((entry) => passing.has(entry.cells[idKey]));
    const order = group?.order;
    if (!order && sort.length === 0) return kept.map((entry) => entry.cells);
    return [...kept]
      .sort((a, b) => {
        if (order && a.cells.__group !== b.cells.__group) return order.indexOf(a.cells.__group) - order.indexOf(b.cells.__group);
        for (const { sortKey, direction } of sort) {
          const difference = compare(a.raw[sortKey], b.raw[sortKey]);
          if (difference !== 0) return direction === "ascending" ? difference : -difference;
        }
        return 0;
      })
      .map((entry) => entry.cells);
  }, [all, filters, sort, group, applyFilters]);

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
  const selection = useTableSelection<Cells>({ ...selectionConfig, getRowLabel: (cells) => cells[definition.columns[0]?.key] || cells[idKey] });
  const sortable = useTableSortable<Cells>({ sort, onSortChange: setSort, allowUnsortedState: true });
  const bulk = definition.bulk ?? [];
  const chosen = shown.filter((cells) => selected.has(cells[idKey])).map((cells) => cells[idKey]);

  const columns: TableColumn<Cells>[] = definition.columns.map((column, position) => ({
    key: column.key,
    header: t(column.header),
    width: column.width ? pixel(column.width) : proportional(1),
    sortable: true,
    renderCell: (cells) =>
      position === 0 && cells.__href ? (
        <Link href={cells.__href}>{cells[column.key] || cells[idKey]}</Link>
      ) : column.kind === "time" && cells[column.key] ? (
        <Text type="body" color="secondary">
          <Timestamp value={cells[column.key]} format="relative" />
        </Text>
      ) : column.dots?.[cells[column.key]] ? (
        <HStack gap={2} vAlign="center">
          <StatusDot variant={column.dots[cells[column.key]]} label={cells[column.key]} />
          <Text type="body" maxLines={1}>
            {cells[column.key]}
          </Text>
        </HStack>
      ) : (
        <Text type="body" maxLines={1}>
          {cells[column.key] ?? ""}
        </Text>
      ),
  }));

  const run = (action: BulkAction) => {
    setBusy(true);
    setFailure("");
    action
      .run(chosen)
      .then(() => setSelected(new Set()))
      .catch((refused: Error) => setFailure(refused.message))
      .finally(() => setBusy(false));
  };

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
                  <Heading level={1}>{t(definition.title)}</Heading>
                </VStack>
              </StackItem>
              {lanes && (
                <SegmentedControl label={t("Layout")} value={layout} onChange={setLayout}>
                  <SegmentedControlItem value={layouts[0]} label={t("Table")} icon={<Icon icon={TableCellsIcon} size="sm" />} />
                  <SegmentedControlItem value={layouts[1]} label={t("Board")} icon={<Icon icon={ViewColumnsIcon} size="sm" />} />
                </SegmentedControl>
              )}
              <IconButton label={t("Refresh")} tooltip={t("Refresh")} icon={<Icon icon={ArrowPathIcon} size="sm" />} onClick={reread} />
              {primary}
            </HStack>
            {all.length > 0 && chosen.length > 0 && (
              <Section variant="muted" padding={2}>
                <HStack gap={3} vAlign="center" wrap="wrap">
                  <StackItem size="fill">
                    <HStack gap={1} vAlign="center">
                      {bulk.map((action) => (
                        <Button key={action.label} label={t(action.label)} variant="ghost" size="sm" isLoading={busy} onClick={() => run(action)} />
                      ))}
                    </HStack>
                  </StackItem>
                  <Text type="body" hasTabularNumbers>
                    {String(chosen.length)} {t("selected")}
                  </Text>
                  <Button label={t("Clear selection")} variant="ghost" size="sm" onClick={() => setSelected(new Set())} />
                </HStack>
              </Section>
            )}
            {all.length > 0 && chosen.length === 0 && (
            <HStack gap={2} vAlign="center">
              <StackItem size="fill">
                <PowerSearch
                  config={config}
                  filters={filters}
                  onChange={(next) => setFilters([...next])}
                  label={t("Filter")}
                  placeholder={t("Filter by any column")}
                  resultCount={`${shown.length} / ${all.length}`}
                />
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
                      value={groupKey}
                      onChange={(value) => {
                        setGroupKey(String(value));
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
              cards={shown.map((cells) => ({
                id: cells[idKey],
                column: moved[cells[idKey]] ?? cells.__lane,
                title: cells[definition.columns[0].key],
                href: cells.__href,
                lines: definition.columns.slice(1, 5).filter((column) => column.key !== board.key && column.kind !== "time" && cells[column.key]).map((column) => cells[column.key]),
              }))}
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
            <Table
              data={group ? grouped.data : shown}
              columns={columns}
              idKey={group ? grouped.idKey : idKey}
              density="balanced"
              dividers="rows"
              textOverflow="truncate"
              hasHover
              plugins={{ ...(group ? { grouped: grouped.plugin } : {}), ...(bulk.length > 0 ? { selection } : {}), sortable }}
            />
          )}
        </LayoutContent>
      }
    />
  );
}
