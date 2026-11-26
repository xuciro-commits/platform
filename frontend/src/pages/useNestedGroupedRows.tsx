import { Badge } from "@astryxdesign/core/Badge";
import { Icon } from "@astryxdesign/core/Icon";
import { IconButton } from "@astryxdesign/core/IconButton";
import { HStack } from "@astryxdesign/core/Layout";
import type { TablePlugin } from "@astryxdesign/core/Table";
import { Text } from "@astryxdesign/core/Text";
import { borderVars, colorVars, spacingVars } from "@astryxdesign/core/theme/tokens.stylex";
import { ChevronDownIcon, ChevronRightIcon } from "@heroicons/react/24/outline";
import * as stylex from "@stylexjs/stylex";
import { createElement, useCallback, useMemo } from "react";
import { t } from "../i18n";

const GROUP_MARKER = Symbol("nestedGroupHeader");

type HeaderTarget = {
  [GROUP_MARKER]: true;
  groupKey: string;
  label: string;
  dimension: string;
  showDimension: boolean;
  count: number;
  level: 1 | 2;
};

function isGroupHeader(item: unknown): item is HeaderTarget {
  return typeof item === "object" && item !== null && (item as HeaderTarget)[GROUP_MARKER] === true;
}

function makeHeader<Row extends Record<string, unknown>>(
  groupKey: string,
  label: string,
  dimension: string,
  showDimension: boolean,
  count: number,
  level: 1 | 2,
): Row {
  const target: HeaderTarget = {
    [GROUP_MARKER]: true,
    groupKey,
    label,
    dimension,
    showDimension,
    count,
    level,
  };
  return new Proxy(target, {
    get(current, prop) {
      if (
        prop === GROUP_MARKER ||
        prop === "groupKey" ||
        prop === "label" ||
        prop === "dimension" ||
        prop === "showDimension" ||
        prop === "count" ||
        prop === "level"
      ) {
        return current[prop as keyof HeaderTarget];
      }
      return prop in current ? current[prop as keyof HeaderTarget] : "";
    },
  }) as unknown as Row;
}

function orderKeys(keys: string[], preferred?: string[]): string[] {
  if (!preferred || preferred.length === 0) return keys;
  const known = new Set(keys);
  const head = preferred.filter((item) => known.has(item));
  const headSet = new Set(head);
  return [...head, ...keys.filter((item) => !headSet.has(item))];
}

const styles = stylex.create({
  rowPrimary: {
    backgroundColor: colorVars["--color-background-muted"],
    cursor: "pointer",
  },
  rowSecondary: {
    backgroundColor: colorVars["--color-background-surface"],
    cursor: "pointer",
  },
  cell: {
    borderBlockEndColor: colorVars["--color-border"],
    borderBlockEndStyle: "solid",
    borderBlockEndWidth: borderVars["--border-width"],
    paddingBlock: spacingVars["--spacing-1"],
    paddingInlineEnd: spacingVars["--spacing-3"],
    paddingInlineStart: spacingVars["--spacing-2"],
  },
  cellSecondary: {
    paddingInlineStart: spacingVars["--spacing-8"],
  },
  dataIndentOne: {
    paddingInlineStart: spacingVars["--spacing-10"],
  },
  dataIndentTwo: {
    paddingInlineStart: 64,
  },
  dataIndentOneWithSelection: {
    paddingInlineStart: spacingVars["--spacing-6"],
  },
  dataIndentTwoWithSelection: {
    paddingInlineStart: spacingVars["--spacing-12"],
  },
});

export type NestedGroupSpec<Row> = {
  label: string;
  of: (row: Row) => string;
  order?: string[];
};

export function subGroupKey(primaryKey: string, secondaryKey: string): string {
  return `${primaryKey}\u0000${secondaryKey}`;
}

export function useNestedGroupedRows<Row extends Record<string, unknown>>({
  data,
  primary,
  secondary,
  collapsedGroups,
  onToggleGroup,
  getRowKey,
  firstColumnKey,
  hasSelection = false,
}: {
  data: Row[];
  primary?: NestedGroupSpec<Row>;
  secondary?: NestedGroupSpec<Row>;
  collapsedGroups: ReadonlySet<string>;
  onToggleGroup: (key: string) => void;
  getRowKey: (row: Row) => string;
  firstColumnKey?: string;
  hasSelection?: boolean;
}) {
  const structure = useMemo(() => {
    const childrenByL1 = new Map<string, string[]>();
    if (!primary || data.length === 0) {
      return { flattened: data, l1Keys: [] as string[], l2Keys: [] as string[], childrenByL1 };
    }
    const buckets = new Map<string, Row[]>();
    for (const row of data) {
      const key = primary.of(row) || t("None");
      const list = buckets.get(key);
      if (list) list.push(row);
      else buckets.set(key, [row]);
    }
    const l1Keys = orderKeys([...buckets.keys()], primary.order);
    const l2Keys: string[] = [];
    const flattened: Row[] = [];

    for (const l1 of l1Keys) {
      const rows = buckets.get(l1) ?? [];
      flattened.push(makeHeader<Row>(l1, l1, primary.label, Boolean(secondary), rows.length, 1));
      const l1Collapsed = collapsedGroups.has(l1);
      if (!secondary) {
        if (!l1Collapsed) flattened.push(...rows);
        continue;
      }
      const subBuckets = new Map<string, Row[]>();
      for (const row of rows) {
        const sub = secondary.of(row) || t("None");
        const subList = subBuckets.get(sub);
        if (subList) subList.push(row);
        else subBuckets.set(sub, [row]);
      }
      const orderedSubs = orderKeys([...subBuckets.keys()], secondary.order);
      const children: string[] = [];
      for (const l2 of orderedSubs) {
        const composite = subGroupKey(l1, l2);
        l2Keys.push(composite);
        children.push(composite);
        if (l1Collapsed) continue;
        const subRows = subBuckets.get(l2) ?? [];
        flattened.push(makeHeader<Row>(composite, l2, secondary.label, true, subRows.length, 2));
        if (!collapsedGroups.has(composite)) {
          flattened.push(...subRows);
        }
      }
      childrenByL1.set(l1, children);
    }
    return { flattened, l1Keys, l2Keys, childrenByL1 };
  }, [data, primary, secondary, collapsedGroups]);

  const idKey = useCallback(
    (row: Row) => (isGroupHeader(row) ? `__group_${row.groupKey}` : getRowKey(row)),
    [getRowKey],
  );

  const hasPrimary = Boolean(primary);
  const hasSecondary = Boolean(primary && secondary);

  const plugin = useMemo<TablePlugin<Row>>(
    () => ({
      rowModel: "grouped",
      transformBodyCell(props, column, item) {
        if (isGroupHeader(item)) {
          return { ...props, isContentSuppressed: true };
        }
        if (hasPrimary && firstColumnKey && column.key === firstColumnKey) {
          const indentStyle = hasSecondary
            ? hasSelection
              ? styles.dataIndentTwoWithSelection
              : styles.dataIndentTwo
            : hasSelection
              ? styles.dataIndentOneWithSelection
              : styles.dataIndentOne;
          return { ...props, xstyle: [...props.xstyle, indentStyle] };
        }
        return props;
      },
      transformBodyRow(props, item) {
        if (!isGroupHeader(item)) return props;
        const header = item;
        const isCollapsed = collapsedGroups.has(header.groupKey);
        const toggle = () => onToggleGroup(header.groupKey);
        const toggleLabel = `${isCollapsed ? t("Expand group") : t("Collapse group")} ${header.label}`;
        return {
          ...props,
          htmlProps: {
            ...props.htmlProps,
            onClick: toggle,
          },
          xstyle: [...props.xstyle, header.level === 1 ? styles.rowPrimary : styles.rowSecondary],
          children: createElement(
            "td",
            {
              colSpan: 999,
              ...stylex.props(styles.cell, header.level === 2 && styles.cellSecondary),
            },
            <HStack gap={1.5} vAlign="center">
              <IconButton
                label={toggleLabel}
                aria-expanded={!isCollapsed}
                variant="ghost"
                size="sm"
                icon={<Icon icon={isCollapsed ? ChevronRightIcon : ChevronDownIcon} size="sm" />}
                onClick={(event) => {
                  event.stopPropagation();
                  toggle();
                }}
              />
              {header.showDimension && (
                <Text type="supporting" color="secondary" maxLines={1}>
                  {t(header.dimension)}:
                </Text>
              )}
              <Text weight={header.level === 1 ? "medium" : undefined} maxLines={1}>
                {header.label}
              </Text>
              <Badge variant="neutral" label={String(header.count)} />
            </HStack>,
          ),
        };
      },
    }),
    [collapsedGroups, onToggleGroup, hasPrimary, hasSecondary, firstColumnKey, hasSelection],
  );

  const childrenOf = useCallback(
    (key: string) => structure.childrenByL1.get(key) ?? [],
    [structure.childrenByL1],
  );

  return {
    data: structure.flattened,
    l1Keys: structure.l1Keys,
    l2Keys: structure.l2Keys,
    allKeys: useMemo(() => [...structure.l1Keys, ...structure.l2Keys], [structure.l1Keys, structure.l2Keys]),
    childrenOf,
    isHeader: isGroupHeader,
    idKey,
    plugin,
  };
}
