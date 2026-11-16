import { Badge } from "@astryxdesign/core/Badge";
import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { EmptyState } from "@astryxdesign/core/EmptyState";
import { HStack, Layout, LayoutContent, LayoutHeader, StackItem, VStack } from "@astryxdesign/core/Layout";
import { Popover } from "@astryxdesign/core/Popover";
import { RadioList, RadioListItem } from "@astryxdesign/core/RadioList";
import { Skeleton } from "@astryxdesign/core/Skeleton";
import { Table, pixel, proportional, useTableGroupedRows } from "@astryxdesign/core/Table";
import type { TableColumn } from "@astryxdesign/core/Table";
import { Heading, Text } from "@astryxdesign/core/Text";
import { TextInput } from "@astryxdesign/core/TextInput";
import { MagnifyingGlassIcon } from "@heroicons/react/24/outline";
import { useMemo, useState } from "react";
import { type Read, useRead } from "../host";
import { t } from "../i18n";

export type ListColumn<Row> = { key: string; header: string; width?: number; text: (row: Row) => string };
export type ListGroup<Row> = { key: string; label: string; of: (row: Row) => string };
export type ListShape<Answer, Row> = {
  title: string;
  about: string;
  rows: (answer: Answer) => Row[];
  id: (row: Row) => string;
  columns: ListColumn<Row>[];
  groups?: ListGroup<Row>[];
};
export type ListDefinition<Answer, Row> = ListShape<Answer, Row> & { read: string };

type Cells = Record<string, string>;
const idKey = "__id";
const ungrouped = "none";

export function listPage<Answer, Row>(definition: ListDefinition<Answer, Row>) {
  return function ListEntry() {
    return <ListView definition={definition} read={useRead<Answer>(definition.read)} />;
  };
}

type ListViewProps<Answer, Row> = {
  definition: ListShape<Answer, Row>;
  read: Read<Answer>;
};

export function ListView<Answer, Row>({ definition, read }: ListViewProps<Answer, Row>) {
  const [filter, setFilter] = useState("");
  const [groupKey, setGroupKey] = useState(ungrouped);
  const [collapsed, setCollapsed] = useState<Set<string>>(() => new Set());
  const group = definition.groups?.find((candidate) => candidate.key === groupKey);

  const all = useMemo(() => {
    if (read.status !== "ready") return [];
    return definition.rows(read.data).map((row) => {
      const cells: Cells = { [idKey]: definition.id(row), __group: group ? group.of(row) : "" };
      for (const column of definition.columns) cells[column.key] = column.text(row);
      return cells;
    });
  }, [read, definition, group]);

  const shown = useMemo(() => {
    const wanted = filter.trim().toLowerCase();
    if (!wanted) return all;
    return all.filter((cells) => definition.columns.some((column) => cells[column.key].toLowerCase().includes(wanted)));
  }, [all, filter, definition]);

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

  const columns: TableColumn<Cells>[] = definition.columns.map((column) => ({
    key: column.key,
    header: t(column.header),
    width: column.width ? pixel(column.width) : proportional(1),
    renderCell: (cells) => (
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
            <Heading level={1}>{t(definition.title)}</Heading>
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
          {read.status === "ready" && shown.length > 0 && (
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
