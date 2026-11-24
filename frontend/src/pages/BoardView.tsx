import { Button } from "@astryxdesign/core/Button";
import { Card } from "@astryxdesign/core/Card";
import { HStack, Layout, LayoutContent, LayoutHeader, StackItem, VStack } from "@astryxdesign/core/Layout";
import { Link } from "@astryxdesign/core/Link";
import { Heading, Text } from "@astryxdesign/core/Text";
import { Icon } from "@astryxdesign/core/Icon";
import { IconButton } from "@astryxdesign/core/IconButton";
import { PlusIcon } from "@heroicons/react/24/outline";
import { Timestamp } from "@astryxdesign/core/Timestamp";
import * as stylex from "@stylexjs/stylex";
import { Fragment, type PointerEvent as ReactPointerEvent, type ReactNode, useEffect, useMemo, useRef, useState } from "react";
import { t } from "../i18n";
import { dragStyles, useDrag } from "./drag";
import { useGutter } from "./frame";
import { PersonLabel } from "./MemberField";
import type { Dot } from "./Transitions";
import { DotMark } from "./DotMark";

export type BoardLine = string | { label: string; value: string; kind?: string };
export type BoardCard = { id: string; column: string; title: string; href: string; lines: BoardLine[]; amount?: number; currency?: string; owner?: string; updated?: string; related?: string };
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
          const money = line.kind === "number" && /[A-Z]{3}$/.test(line.value);
          if (money) {
            return <Text key={position} type="supporting" color="primary" weight="medium" hasTabularNumbers maxLines={1}>{line.value}</Text>;
          }
          if (line.kind === "number") {
            return (
              <HStack key={position} gap={1.5} vAlign="center">
                <Text type="supporting" color="secondary" maxLines={1}>{line.label}</Text>
                <Text type="supporting" color="primary" hasTabularNumbers maxLines={1}>{line.value}</Text>
              </HStack>
            );
          }
          return <Text key={position} type="supporting" color="secondary" maxLines={1}>{line.value}</Text>;
        })}
        {card.related && <Text type="supporting" color="secondary" maxLines={1}>{card.related}</Text>}
      </VStack>
      {(card.owner || card.updated) && (
        <HStack gap={2} vAlign="center">
          {card.owner && <PersonLabel name={card.owner} size="xsm" color="secondary" />}
          <StackItem size="fill" />
          {card.updated && <Timestamp value={card.updated} format="relative" type="supporting" color="secondary" />}
        </HStack>
      )}
    </VStack>
  );
}

export function BoardView({ columns, cards, onMove, accepts, onCreate, page, total }: BoardProps) {
  const gutter = useGutter();
  const columnsScroll = useRef<HTMLDivElement | null>(null);
  const [optimistic, setOptimistic] = useState<Record<string, string>>({});

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

  const handleMove = onMove && ((id: string, to: string) => {
    setOptimistic((previous) => ({ ...previous, [id]: to }));
    const clear = () => setOptimistic((previous) => {
      if (!(id in previous)) return previous;
      const { [id]: _, ...rest } = previous;
      return rest;
    });
    const pending = onMove(id, to);
    if (pending && typeof pending.then === "function") {
      pending.catch(clear);
    }
  });

  const shownCards = useMemo(
    () => (Object.keys(optimistic).length === 0 ? cards : cards.map((card) => (card.id in optimistic ? { ...card, column: optimistic[card.id] } : card))),
    [cards, optimistic],
  );
  const { drag, press, floating, landing, target, item } = useDrag(handleMove, accepts, columnsScroll);

  const lifted = drag ? shownCards.find((card) => card.id === drag.id) : undefined;

  const known = new Set(columns.map((column) => column.key));
  const missing = shownCards.filter((card) => !known.has(card.column));
  const column = ({ key, label, dot }: BoardProps["columns"][number]): ReactNode => {
    const own = key ? shownCards.filter((card) => card.column === key) : missing;
    const resting = drag ? own.filter((card) => card.id !== drag.id) : own;
    const liftedIndex = drag ? own.findIndex((card) => card.id === drag.id) : -1;
    const ghostIndex = drag?.target === key ? (liftedIndex >= 0 ? liftedIndex : resting.length) : -1;
    const sums = new Map<string, number>();
    for (const card of own) {
      if (card.amount === undefined) continue;
      const currency = card.currency ?? "";
      sums.set(currency, (sums.get(currency) ?? 0) + card.amount);
    }
    const subtotal = [...sums].map(([currency, amount]) => {
      const digits = currency ? { minimumFractionDigits: 2, maximumFractionDigits: 2 } : { maximumFractionDigits: 8 };
      return currency ? `${(amount / 100).toLocaleString(undefined, digits)} ${currency}` : amount.toLocaleString(undefined, digits);
    }).join(" · ");
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
                    <Text type="supporting" color="secondary" hasTabularNumbers>{own.length.toLocaleString()}{page?.hasMore ? "+" : ""}</Text>
                    {onCreate && key && (
                      <IconButton label={t("New in {column}").replace("{column}", label)} tooltip={t("New in {column}").replace("{column}", label)}
                        variant="ghost" size="sm" icon={<Icon icon={PlusIcon} size="sm" />} onClick={() => onCreate(key)} />
                    )}
                  </HStack>
                </HStack>
                {total && subtotal && <Text type="supporting" color="secondary" hasTabularNumbers>{t(total)} · {page?.hasMore ? t("Loaded subtotal") : t("Total")} · {subtotal}</Text>}
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
                      xstyle={[dragStyles.item, !onMove && dragStyles.fixed]}
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
