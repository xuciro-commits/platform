import { Button } from "@astryxdesign/core/Button";
import { Card } from "@astryxdesign/core/Card";
import { HStack, Layout, LayoutContent, LayoutHeader, StackItem, VStack } from "@astryxdesign/core/Layout";
import { Link } from "@astryxdesign/core/Link";
import { Heading, Text } from "@astryxdesign/core/Text";
import { Icon } from "@astryxdesign/core/Icon";
import { IconButton } from "@astryxdesign/core/IconButton";
import { PlusIcon } from "@heroicons/react/24/outline";
import * as stylex from "@stylexjs/stylex";
import { Fragment, type PointerEvent as ReactPointerEvent, type ReactNode, useEffect, useMemo, useRef, useState } from "react";
import { t, useFormatConfig } from "../i18n";
import {
  MoneyValue,
  NumberValue,
  PercentValue,
  PersonValue,
  ReferenceValue,
  StatusValue,
  TemporalValue,
} from "../semantic";
import { dragStyles, useDrag } from "./drag";
import { useGutter } from "./frame";
import type { Dot } from "./Transitions";
import { DotMark } from "./DotMark";

export type BoardLine =
  | string
  | {
      label: string;
      value: string;
      kind?: string;
      raw?: string;
      refType?: string;
      refId?: string;
      dot?: Dot;
    };

export type BoardCard = {
  id: string;
  column: string;
  title: string;
  href: string;
  lines: BoardLine[];
  amount?: number;
  currency?: string;
  owner?: string;
  ownerId?: string;
  updated?: string;
  related?: string;
};

type MoveResult = Promise<unknown> | void;
type BoardProps = {
  columns: { key: string; label: string; dot?: Dot }[];
  cards: BoardCard[];
  total?: string;
  onMove?: (id: string, to: string) => MoveResult;
  accepts?: (id: string, to: string) => boolean;
  onCreate?: (column: string) => void;
  page?: { hasMore: boolean; loading: boolean; loadMore: () => void };
};
const columnWidth = 300;

const styles = stylex.create({
  columns: { overflowX: "auto", overflowY: "hidden", height: "100%" },
  column: { flexShrink: 0, flexBasis: columnWidth, height: "100%" },
  cardBody: { minWidth: 0 },
});

function parseRaw(raw?: string): unknown {
  if (!raw) return undefined;
  try {
    return JSON.parse(raw);
  } catch {
    return undefined;
  }
}

function CardBody({ card }: { card: BoardCard }) {
  return (
    <VStack gap={2} xstyle={styles.cardBody}>
      <VStack gap={1} xstyle={styles.cardBody}>
        <Heading level={4}>
          <Link href={card.href} maxLines={1}>{card.title || card.id}</Link>
        </Heading>
        {card.lines.map((line, position) => {
          if (typeof line === "string") {
            return <Text key={position} type="supporting" color="secondary" maxLines={1}>{line}</Text>;
          }
          if (line.refType && line.refId) {
            return <ReferenceValue key={position} type={line.refType} id={line.refId} name={line.value} size="xs" color="secondary" />;
          }
          if (line.dot) {
            return <StatusValue key={position} label={line.value} dot={line.dot} size="xs" color="secondary" />;
          }
          if (line.kind === "money") {
            const parsed = parseRaw(line.raw);
            if (parsed) return <MoneyValue key={position} value={parsed} size="xs" weight="medium" />;
            return <Text key={position} type="supporting" color="primary" weight="medium" hasTabularNumbers maxLines={1}>{line.value}</Text>;
          }
          if (line.kind === "percent") {
            const parsed = parseRaw(line.raw);
            return (
              <HStack key={position} gap={1.5} vAlign="center">
                <Text type="supporting" color="secondary" maxLines={1}>{line.label}</Text>
                {parsed != null ? <PercentValue value={parsed} size="xs" /> : <Text type="supporting" hasTabularNumbers>{line.value}</Text>}
              </HStack>
            );
          }
          if (line.kind === "number") {
            const parsed = parseRaw(line.raw);
            return (
              <HStack key={position} gap={1.5} vAlign="center">
                <Text type="supporting" color="secondary" maxLines={1}>{line.label}</Text>
                {typeof parsed === "number" ? <NumberValue value={parsed} size="xs" /> : <Text type="supporting" color="primary" hasTabularNumbers maxLines={1}>{line.value}</Text>}
              </HStack>
            );
          }
          return <Text key={position} type="supporting" color="secondary" maxLines={1}>{line.value}</Text>;
        })}
        {card.related && <Text type="supporting" color="secondary" maxLines={1}>{card.related}</Text>}
      </VStack>
      {(card.owner || card.updated) && (
        <HStack gap={2} vAlign="center">
          {card.owner && <PersonValue id={card.ownerId} name={card.owner} size="xs" color="secondary" />}
          <StackItem size="fill" />
          {card.updated && <TemporalValue value={card.updated} kind="datetime" mode="relative" size="xs" surface="card" />}
        </HStack>
      )}
    </VStack>
  );
}

const sortColumn = (items: BoardCard[], ids?: string[]) => {
  if (!ids || ids.length === 0) return items;
  const rank = new Map(ids.map((id, position) => [id, position]));
  return [...items].sort((a, b) => {
    const ra = rank.get(a.id);
    const rb = rank.get(b.id);
    if (ra !== undefined && rb !== undefined) return ra - rb;
    if (ra !== undefined) return -1;
    if (rb !== undefined) return 1;
    return 0;
  });
};

export function BoardView({ columns, cards, onMove, accepts, onCreate, page, total }: BoardProps) {
  useFormatConfig();
  const gutter = useGutter();
  const columnsScroll = useRef<HTMLDivElement | null>(null);
  const [optimistic, setOptimistic] = useState<Record<string, string>>({});
  const [order, setOrder] = useState<Record<string, string[]>>({});

  useEffect(() => {
    setOptimistic((previous) => {
      if (Object.keys(previous).length === 0) return previous;
      const next: Record<string, string> = {};
      for (const [id, col] of Object.entries(previous)) {
        const card = cards.find((entry) => entry.id === id);
        if (card && card.column !== col) next[id] = col;
      }
      return Object.keys(next).length === Object.keys(previous).length ? previous : next;
    });
  }, [cards]);

  const shownCards = useMemo(
    () => (Object.keys(optimistic).length === 0 ? cards : cards.map((card) => (card.id in optimistic ? { ...card, column: optimistic[card.id] } : card))),
    [cards, optimistic],
  );

  const known = useMemo(() => new Set(columns.map((column) => column.key)), [columns]);

  const placeCard = (id: string, to: string, index: number) => {
    const targetKey = known.has(to) ? to : "";
    const currentInTarget = sortColumn(
      shownCards.filter((card) => (targetKey ? card.column === targetKey : !known.has(card.column)) && card.id !== id),
      order[targetKey],
    ).map((card) => card.id);
    const clamped = Math.max(0, Math.min(index, currentInTarget.length));
    const nextTargetIds = [...currentInTarget.slice(0, clamped), id, ...currentInTarget.slice(clamped)];
    setOrder((previous) => {
      const next: Record<string, string[]> = {};
      for (const [col, ids] of Object.entries(previous)) {
        if (col === targetKey) continue;
        const filtered = ids.filter((entry) => entry !== id);
        if (filtered.length > 0) next[col] = filtered;
      }
      next[targetKey] = nextTargetIds;
      return next;
    });
  };

  const handleMove = onMove && ((id: string, to: string, index: number) => {
    const existing = shownCards.find((card) => card.id === id);
    const from = existing?.column ?? "";
    const previousOrder = order;
    placeCard(id, to, index);
    if (to === from) return;
    setOptimistic((previous) => ({ ...previous, [id]: to }));
    const clear = () => {
      setOptimistic((previous) => {
        if (!(id in previous)) return previous;
        const { [id]: _, ...rest } = previous;
        return rest;
      });
      setOrder(previousOrder);
    };
    const pending = onMove(id, to);
    if (pending && typeof pending.then === "function") {
      pending.catch(clear);
    }
  });

  const { drag, press, floating, landing, target, item } = useDrag(handleMove, accepts, columnsScroll);

  const lifted = drag ? shownCards.find((card) => card.id === drag.id) : undefined;
  const missing = shownCards.filter((card) => !known.has(card.column));

  const column = ({ key, label, dot }: BoardProps["columns"][number]): ReactNode => {
    const raw = key ? shownCards.filter((card) => card.column === key) : missing;
    const own = sortColumn(raw, order[key]);
    const resting = drag ? own.filter((card) => card.id !== drag.id) : own;
    const ghostIndex = drag?.target === key ? Math.max(0, Math.min(drag.index, resting.length)) : -1;
    const sums = new Map<string, number>();
    for (const card of own) {
      if (card.amount === undefined) continue;
      const currency = card.currency ?? "";
      sums.set(currency, (sums.get(currency) ?? 0) + card.amount);
    }
    const subtotalEntries = [...sums];
    return (
      <Card key={key} variant="muted" padding={0} xstyle={styles.column}>
        <Layout
          height="fill"
          header={
            <LayoutHeader hasDivider padding={3}>
              <VStack gap={1}>
                <HStack hAlign="between" vAlign="center">
                  <HStack gap={2} vAlign="center">
                    {dot && <DotMark dot={dot} label={label} />}
                    <Heading level={4}>{label}</Heading>
                  </HStack>
                  <HStack gap={1} vAlign="center">
                    <NumberValue value={own.length} integer size="xs" color="secondary" />
                    {page?.hasMore && <Text type="supporting" color="secondary">+</Text>}
                    {onCreate && key && (
                      <IconButton label={t("New in {column}").replace("{column}", label)} tooltip={t("New in {column}").replace("{column}", label)}
                        variant="ghost" size="sm" icon={<Icon icon={PlusIcon} size="sm" />} onClick={() => onCreate(key)} />
                    )}
                  </HStack>
                </HStack>
                {total && subtotalEntries.length > 0 && (
                  <HStack gap={1} vAlign="center" wrap="wrap">
                    <Text type="supporting" color="secondary">
                      {t(total)} · {page?.hasMore ? t("Loaded subtotal") : t("Total")} ·
                    </Text>
                    {subtotalEntries.map(([currency, amount], idx) => (
                      <Fragment key={currency || idx}>
                        {idx > 0 && <Text type="supporting" color="secondary">·</Text>}
                        {currency ? (
                          <MoneyValue value={{ amount, currency }} size="xs" compact color="secondary" />
                        ) : (
                          <NumberValue value={amount} size="xs" compact color="secondary" />
                        )}
                      </Fragment>
                    ))}
                  </HStack>
                )}
              </VStack>
            </LayoutHeader>
          }
          content={
            <LayoutContent
              role="region"
              label={label}
              padding={2}
              xstyle={[dragStyles.target, drag?.target === key && dragStyles.targetOver]}
              ref={target(key)}>
              <VStack gap={2}>
                {own.length === 0 && ghostIndex < 0 && <Text type="supporting" color="secondary">{t("Nothing in this column yet")}</Text>}
                {resting.map((card, index) => (
                  <Fragment key={card.id}>
                    {drag && ghostIndex === index && <VStack ref={landing} xstyle={dragStyles.ghost(drag.height)} />}
                    <Card
                      padding={3}
                      xstyle={[dragStyles.item, drag && dragStyles.dragging, !onMove && dragStyles.fixed]}
                      ref={item(card.id, key)}
                      onPointerDown={(event: ReactPointerEvent) => press(event, card.id, card.column)}>
                      <CardBody card={card} />
                    </Card>
                  </Fragment>
                ))}
                {drag && ghostIndex === resting.length && <VStack ref={landing} xstyle={dragStyles.ghost(drag.height)} />}
                {page?.hasMore && <Button label={t("Load more")} variant="secondary" size="sm" isLoading={page.loading} onClick={page.loadMore} />}
              </VStack>
            </LayoutContent>
          }
        />
      </Card>
    );
  };

  return (
    <>
      <HStack gap={4} paddingInline={gutter} paddingBlockEnd={gutter} ref={columnsScroll} xstyle={styles.columns}>
        {columns.map(column)}
        {missing.length > 0 && column({ key: "", label: t("None") })}
      </HStack>
      {drag && lifted && (
        <Card ref={floating} padding={3} xstyle={[dragStyles.floating, dragStyles.floatingAt(drag.x, drag.y, drag.width)]}>
          <CardBody card={lifted} />
        </Card>
      )}
    </>
  );
}
