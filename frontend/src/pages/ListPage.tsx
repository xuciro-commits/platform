import { Badge } from "@astryxdesign/core/Badge";
import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { EmptyState } from "@astryxdesign/core/EmptyState";
import { HStack, Layout, LayoutContent, LayoutHeader, StackItem, VStack } from "@astryxdesign/core/Layout";
import { Link } from "@astryxdesign/core/Link";
import { Popover } from "@astryxdesign/core/Popover";
import { RadioList, RadioListItem } from "@astryxdesign/core/RadioList";
import { Skeleton } from "@astryxdesign/core/Skeleton";
import { Table, pixel, proportional, useTableGroupedRows } from "@astryxdesign/core/Table";
import type { TableColumn } from "@astryxdesign/core/Table";
import { Heading, Text } from "@astryxdesign/core/Text";
import { TextInput } from "@astryxdesign/core/TextInput";
import { MagnifyingGlassIcon } from "@heroicons/react/24/outline";
import { type ReactNode, useEffect, useMemo, useState } from "react";
import { type Read, useRead } from "../host";
import { BackLink } from "./BackLink";
import { BoardView } from "./BoardView";
import { t } from "../i18n";

export type ListColumn<Row> = { key: string; header: string; width?: number; text: (row: Row) => string };
export type ListGroup<Row> = { key: string; label: string; of: (row: Row) => string; order?: string[] };
type Done = Promise<unknown>;
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
};
export type ListDefinition<Answer, Row> = ListShape<Answer, Row> & { read: string };

type Cells = Record<string, string>;
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
  const [filter, setFilter] = useState("");
  const [groupKey, setGroupKey] = useState(ungrouped);
  const [collapsed, setCollapsed] = useState<Set<string>>(() => new Set());
  const [layout, setLayout] = useState(layouts[0]);
  const [moved, setMoved] = useState<Record<string, string>>({});
  const [failure, setFailure] = useState("");
  const group = definition.groups?.find((candidate) => candidate.key === groupKey);
  const lanes = group?.order ? group : definition.groups?.find((candidate) => candidate.order);
  const board = layout === "board" && lanes?.order ? lanes : undefined;
  useEffect(() => setMoved({}), [read]);

  const all = useMemo(() => {
    if (read.status !== "ready") return [];
    return definition.rows(read.data).map((row) => {
      const cells: Cells = { [idKey]: definition.id(row), __group: group ? group.of(row) : "", __lane: lanes ? lanes.of(row) : "", __href: definition.link ? definition.link(row) : "" };
      for (const column of definition.columns) cells[column.key] = column.text(row);
      return cells;
    });
  }, [read, definition, group, lanes]);

  const shown = useMemo(() => {
    const wanted = filter.trim().toLowerCase();
    const kept = wanted ? all.filter((cells) => definition.columns.some((column) => cells[column.key].toLowerCase().includes(wanted))) : all;
    const order = group?.order;
    return order ? [...kept].sort((a, b) => order.indexOf(a.__group) - order.indexOf(b.__group)) : kept;
  }, [all, filter, definition, group]);

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

  const columns: TableColumn<Cells>[] = definition.columns.map((column, position) => ({
    key: column.key,
    header: t(column.header),
    width: column.width ? pixel(column.width) : proportional(1),
    renderCell: (cells) =>
      position === 0 && cells.__href ? (
        <Link href={cells.__href}>{cells[column.key] || cells[idKey]}</Link>
      ) : (
        <Text type="body" maxLines={1}>
          {cells[column.key] ?? ""}
        </Text>
      ),
  }));

  return (
    <Layout
      height="fill"
      header={
        <LayoutHeader hasDivider padding={4}>
          <VStack gap={4}>
            <HStack gap={4} vAlign="center">
              <StackItem size="fill">
                <VStack gap={2}>
                  {back && <BackLink href={back.href} label={t(back.label)} />}
                  <Heading level={1}>{t(definition.title)}</Heading>
                </VStack>
              </StackItem>
              {primary}
            </HStack>
            {all.length > 0 && (
            <HStack gap={2} vAlign="center">
              <StackItem size="fill">
                <TextInput
                  label={t("Filter")}
                  isLabelHidden
                  placeholder={t("Filter")}
                  startIcon={MagnifyingGlassIcon}
                  hasClear
                  value={filter}
                  onChange={setFilter}
                />
              </StackItem>
              <Text type="supporting" color="secondary" hasTabularNumbers>
                {String(shown.length)}
              </Text>
              {definition.groups && definition.groups.length > 0 && (
                <Popover
                  placement="below"
                  alignment="end"
                  width={320}
                  label={t("View options")}
                  content={
                    <VStack gap={4}>
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
                    {lanes && (
                      <RadioList label={t("Layout")} value={layout} onChange={(value) => setLayout(String(value))}>
                        <RadioListItem value={layouts[0]} label={t("Table")} />
                        <RadioListItem value={layouts[1]} label={t("Board")} />
                      </RadioList>
                    )}
                    </VStack>
                  }>
                  <Button label={t("View options")} variant="secondary" size="md" />
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
              <Banner status="error" title={t("This card could not be moved")} description={failure} />
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
                lines: definition.columns.slice(1, 5).filter((column) => column.key !== board.key && cells[column.key]).map((column) => cells[column.key]),
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
              plugins={group ? { grouped: grouped.plugin } : {}}
            />
          )}
        </LayoutContent>
      }
    />
  );
}
