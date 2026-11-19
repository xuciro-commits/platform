import { Banner } from "@astryxdesign/core/Banner";
import { EmptyState } from "@astryxdesign/core/EmptyState";
import { Icon } from "@astryxdesign/core/Icon";
import { IconButton } from "@astryxdesign/core/IconButton";
import { HStack, Layout, LayoutContent, LayoutHeader, StackItem, VStack } from "@astryxdesign/core/Layout";
import { ScrollableArea } from "@astryxdesign/core/ScrollableArea";
import { Section } from "@astryxdesign/core/Section";
import { SegmentedControl, SegmentedControlItem } from "@astryxdesign/core/SegmentedControl";
import { Selector } from "@astryxdesign/core/Selector";
import { Skeleton } from "@astryxdesign/core/Skeleton";
import { StatusDot } from "@astryxdesign/core/StatusDot";
import { Table, pixel, proportional, useTableSortable, useTableSortableState, useTableTreeData, useTableTreeState } from "@astryxdesign/core/Table";
import type { TableColumn, TablePlugin } from "@astryxdesign/core/Table";
import { Heading, Text } from "@astryxdesign/core/Text";
import { TextInput } from "@astryxdesign/core/TextInput";
import { Timestamp } from "@astryxdesign/core/Timestamp";
import { colorVars } from "@astryxdesign/core/theme/tokens.stylex";
import { ArrowPathIcon, MagnifyingGlassIcon } from "@heroicons/react/24/outline";
import * as stylex from "@stylexjs/stylex";
import { type KeyboardEvent, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { reread, useRead } from "../host";
import { t } from "../i18n";
import type { Dot } from "./Transitions";

export type TreeColumn<Row> = {
  key: string;
  header: string;
  width?: number;
  kind?: "number" | "time" | "code";
  text: (row: Row) => string;
  value?: (row: Row) => number | string;
  dot?: (row: Row) => Dot | undefined;
};
export type TreeNode<Row> = { id: string; parent?: string; row: Row };
export type TreeDefinition<Answer, Row> = {
  title: string;
  about: string;
  read: string;
  views?: (answer: Answer) => { value: string; label: string }[];
  nodes: { (answer: Answer, view: string): TreeNode<Row>[] };
  rollup: (row: Row, children: Row[]) => Row;
  columns: TreeColumn<Row>[];
};

type Item<Row> = { id: string; row: Row; children?: Item<Row>[] } & Record<string, unknown>;
type Items<Row> = Item<Row>[];
type Ids = Set<string>;
type Parents = Map<string, string>;

const styles = stylex.create({
  surface: { backgroundColor: colorVars["--color-background-surface"] },
});

function build<Row>(nodes: TreeNode<Row>[]) {
  const known = new Set(nodes.map((node) => node.id));
  const below = new Map<string, TreeNode<Row>[]>();
  for (const node of nodes) {
    const parent = node.parent && known.has(node.parent) && node.parent !== node.id ? node.parent : "";
    below.set(parent, [...(below.get(parent) ?? []), node]);
  }
  const placed = new Set<string>();
  const grow = (parent: string): Item<Row>[] =>
    (below.get(parent) ?? []).flatMap((node) => {
      if (placed.has(node.id)) return [];
      placed.add(node.id);
      const children = grow(node.id);
      return [{ id: node.id, row: node.row, ...(children.length ? { children } : {}) }];
    });
  return grow("");
}

function rolled<Row>(items: Item<Row>[], rollup: (row: Row, children: Row[]) => Row): Item<Row>[] {
  return items.map((item) => {
    if (!item.children?.length) return item;
    const children = rolled(item.children, rollup);
    return { ...item, children, row: rollup(item.row, children.map((child) => child.row)) };
  });
}

function pruned<Row>(items: Item<Row>[], matches: (row: Row) => boolean): Item<Row>[] {
  return items.flatMap((item) => {
    const self = matches(item.row);
    const children = item.children ? pruned(item.children, matches) : [];
    if (!self && children.length === 0) return [];
    return [item.children ? { ...item, children: self && children.length === 0 ? item.children : children } : item];
  });
}

function branches<Row>(items: Items<Row>, into: Ids, parents: Parents, parent?: string) {
  for (const item of items) {
    if (parent) parents.set(item.id, parent);
    if (item.children?.length) {
      into.add(item.id);
      branches(item.children, into, parents, item.id);
    }
  }
  return into;
}

type Keyboard<Row> = {
  rows: Item<Row>[];
  parents: Map<string, string>;
  focused: string | null;
  onFocus: (id: string) => void;
  expanded: ReadonlySet<string>;
  onToggle: (id: string, open: boolean) => void;
};

function useTreeKeyboardNav<Row>({ rows, parents, focused, onFocus, expanded, onToggle }: Keyboard<Row>): TablePlugin<Item<Row>> {
  const onKeyDown = useCallback(
    (event: KeyboardEvent<HTMLTableElement>) => {
      if (event.defaultPrevented || rows.length === 0) return;
      const index = rows.findIndex((row) => row.id === focused);
      const current = index >= 0 ? rows[index] : undefined;
      const branch = Boolean(current?.children?.length);
      const open = current != null && expanded.has(current.id);
      let next: string | undefined;
      switch (event.key) {
        case "ArrowDown":
          next = rows[index + 1]?.id ?? rows[0]?.id;
          break;
        case "ArrowUp":
          next = index > 0 ? rows[index - 1].id : rows[0]?.id;
          break;
        case "ArrowRight":
          if (current == null) next = rows[0]?.id;
          else if (branch && !open) onToggle(current.id, true);
          else if (branch) next = rows[index + 1]?.id;
          break;
        case "ArrowLeft":
          if (current == null) next = rows[0]?.id;
          else if (branch && open) onToggle(current.id, false);
          else next = parents.get(current.id);
          break;
        case "Home":
          next = rows[0]?.id;
          break;
        case "End":
          next = rows[rows.length - 1]?.id;
          break;
        default:
          return;
      }
      event.preventDefault();
      if (next != null && next !== focused) onFocus(next);
    },
    [rows, parents, focused, expanded, onToggle, onFocus],
  );
  return useMemo(
    () => ({
      transformTable: (props) => ({ ...props, htmlProps: { ...props.htmlProps, onKeyDown } }),
      transformBodyRow: (props, item) => ({
        ...props,
        htmlProps: { ...props.htmlProps, tabIndex: item.id === focused ? 0 : -1, "data-row-id": item.id, onClick: () => onFocus(item.id) },
      }),
    }),
    [onKeyDown, focused, onFocus],
  );
}

function Cell<Row>({ column, row }: { column: TreeColumn<Row>; row: Row }) {
  const text = column.text(row);
  const dot = column.dot?.(row);
  if (column.kind === "time" && text) return <Text color="secondary"><Timestamp value={text} format="relative" /></Text>;
  if (dot) {
    return (
      <HStack gap={2} vAlign="center">
        <StatusDot variant={dot} label={text} />
        <Text maxLines={1}>{text}</Text>
      </HStack>
    );
  }
  return <Text type={column.kind === "code" ? "code" : "body"} color={column.kind === "code" ? "secondary" : undefined} hasTabularNumbers={column.kind === "number"} maxLines={1}>{text}</Text>;
}

export function treeList<Answer, Row>(definition: TreeDefinition<Answer, Row>) {
  return function TreeEntry() {
    type Entry = Item<Row>;
    const read = useRead<Answer>(definition.read);
    const views = read.status === "ready" ? definition.views?.(read.data) ?? [] : [];
    const [chosen, setChosen] = useState("");
    const view = views.some((option) => option.value === chosen) ? chosen : views[0]?.value ?? "";
    const [query, setQuery] = useState("");
    const needle = query.trim().toLowerCase();
    const searching = needle.length > 0;
    const whole = useMemo(
      () => (read.status === "ready" ? rolled(build(definition.nodes(read.data, view)), definition.rollup) : []),
      [read, view],
    );
    const data = useMemo(
      () => (searching ? rolled(pruned(whole, (row) => definition.columns.some((column) => column.text(row).toLowerCase().includes(needle))), definition.rollup) : whole),
      [whole, searching, needle],
    );
    const parents = useMemo(() => {
      const map = new Map<string, string>();
      branches(whole, new Set(), map);
      return map;
    }, [whole]);
    const [manual, setManual] = useState<ReadonlySet<string> | null>(null);
    const roots = useMemo(() => new Set(whole.length === 1 && whole[0].children?.length ? [whole[0].id] : []), [whole]);
    const searched = useMemo(() => (searching ? branches(data, new Set(), new Map()) : null), [searching, data]);
    const expanded = searched ?? manual ?? roots;
    const sortable = useTableSortableState<Item<Row>>({
      data,
      comparators: Object.fromEntries(definition.columns.map((column) => [column.key, (a: Entry, b: Entry) => {
        const [left, right] = [column.value?.(a.row) ?? column.text(a.row), column.value?.(b.row) ?? column.text(b.row)];
        return typeof left === "number" && typeof right === "number" ? left - right : String(left).localeCompare(String(right));
      }])),
    });
    const { visibleData, treeConfig } = useTableTreeState<Item<Row>>({
      data,
      idKey: "id",
      expandedIds: expanded,
      onExpandedIdsChange: searching ? () => {} : setManual,
      sortSiblings: sortable.applySort,
    });
    const toggle = useCallback(
      (id: string, open: boolean) => {
        if (searching) return;
        setManual((previous) => {
          const next = new Set(previous ?? roots);
          if (open) next.add(id);
          else next.delete(id);
          return next;
        });
      },
      [searching, roots],
    );
    const bodyRef = useRef<HTMLDivElement>(null);
    const [focused, setFocused] = useState<string | null>(null);
    const moving = useRef(false);
    const focus = useCallback((id: string) => {
      moving.current = true;
      setFocused(id);
    }, []);
    useEffect(() => {
      if (!moving.current || focused == null) return;
      moving.current = false;
      bodyRef.current?.querySelector<HTMLElement>(`tr[data-row-id="${CSS.escape(focused)}"]`)?.focus();
    }, [focused, visibleData]);
    const tree = useTableTreeData({ ...treeConfig, hasExpandAllControl: true });
    const sort = useTableSortable<Item<Row>>(sortable.sortConfig);
    const keyboard = useTreeKeyboardNav({ rows: visibleData, parents, focused, onFocus: focus, expanded, onToggle: toggle });
    const columns: TableColumn<Item<Row>>[] = definition.columns.map((column, position) => ({
      key: column.key,
      header: t(column.header),
      width: position === 0 ? proportional(2, { minWidth: 240 }) : column.width ? pixel(column.width) : proportional(1, { minWidth: 140 }),
      align: column.kind === "number" ? ("end" as const) : undefined,
      sortable: true,
      renderCell: (item) => <Cell column={column} row={item.row} />,
    }));

    return (
      <Layout
        height="fill"
        padding={0}
        xstyle={styles.surface}
        header={
          <LayoutHeader hasDivider label={t("List filters and actions")}>
            <Section variant="transparent" padding={4}>
              <VStack gap={4}>
                <HStack gap={3} vAlign="center" wrap="wrap">
                  <StackItem size="fill">
                    <Heading level={1}>{t(definition.title)}</Heading>
                  </StackItem>
                  {views.length >= 2 && views.length <= 3 && (
                    <SegmentedControl size="sm" label={t("Structure")} value={view} onChange={setChosen}>
                      {views.map((option) => <SegmentedControlItem key={option.value} value={option.value} label={t(option.label)} />)}
                    </SegmentedControl>
                  )}
                  {views.length > 3 && (
                    <Selector label={t("Structure")} isLabelHidden size="sm" value={view} onChange={setChosen} options={views.map((option) => ({ value: option.value, label: t(option.label) }))} />
                  )}
                  <IconButton label={t("Refresh")} tooltip={t("Refresh")} variant="ghost" size="sm" icon={<Icon icon={ArrowPathIcon} size="sm" />} onClick={reread} />
                </HStack>
                {read.status === "ready" && whole.length > 0 && (
                  <TextInput label={t("Search this list")} isLabelHidden size="sm" value={query} onChange={setQuery} placeholder={t("Search")} startIcon={<Icon icon={MagnifyingGlassIcon} size="sm" />} />
                )}
              </VStack>
            </Section>
          </LayoutHeader>
        }
        content={
          <LayoutContent role="main" padding={0} label={t(definition.title)}>
            {read.status === "loading" && (
              <VStack gap={2} padding={4}>
                {[0, 1, 2, 3, 4, 5].map((index) => <Skeleton key={index} height={36} index={index} />)}
              </VStack>
            )}
            {read.status === "error" && (
              <VStack padding={4}>
                <Banner status="error" title={t("This list could not be loaded")} description={read.reason || t("Reload the page to try again.")} />
              </VStack>
            )}
            {read.status === "refused" && <EmptyState headingLevel={2} title={t("You cannot see this list")} description={read.reason || t("Ask an administrator of this workspace for access.")} />}
            {read.status === "ready" && whole.length === 0 && <EmptyState headingLevel={2} title={t("Nothing here yet")} description={t(definition.about)} />}
            {read.status === "ready" && whole.length > 0 && data.length === 0 && (
              <EmptyState headingLevel={2} icon={<Icon icon={MagnifyingGlassIcon} size="lg" />} title={t("No matches")} description={t("Try a different search.")} />
            )}
            {data.length > 0 && (
              <ScrollableArea ref={bodyRef} axis="block" role="region" label={t("Records")} height="100%">
                <Table<Item<Row>> data={visibleData} columns={columns} idKey="id" dividers="rows" hasHover textOverflow="truncate" plugins={{ tree, sort, keyboard }} />
              </ScrollableArea>
            )}
          </LayoutContent>
        }
      />
    );
  };
}
