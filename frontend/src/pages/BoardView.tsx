import { Avatar } from "@astryxdesign/core/Avatar";
import { Button } from "@astryxdesign/core/Button";
import { Card } from "@astryxdesign/core/Card";
import { HStack, Layout, LayoutContent, LayoutHeader, StackItem, VStack } from "@astryxdesign/core/Layout";
import { Link } from "@astryxdesign/core/Link";
import { StatusDot } from "@astryxdesign/core/StatusDot";
import { Heading, Text } from "@astryxdesign/core/Text";
import { Timestamp } from "@astryxdesign/core/Timestamp";
import * as stylex from "@stylexjs/stylex";
import { type PointerEvent as ReactPointerEvent, type ReactNode, useEffect, useRef, useState } from "react";
import { t } from "../i18n";
import { useGutter } from "./frame";
import type { Dot } from "./Transitions";

export type BoardCard = { id: string; column: string; title: string; href: string; lines: string[]; amount?: number; currency?: string; owner?: string; updated?: string };
type BoardProps = {
  columns: { key: string; label: string; dot?: Dot }[];
  cards: BoardCard[];
  total?: string;
  onMove?: (id: string, to: string) => void;
  accepts?: (id: string, to: string) => boolean;
  page?: { hasMore: boolean; loading: boolean; loadMore: () => void };
};
type Drag = { id: string; width: number; height: number; x: number; y: number; target: string | null };

const dragThreshold = 5;
const columnWidth = 300;

const styles = stylex.create({
  columns: { overflowX: "auto", overflowY: "hidden", height: "100%" },
  column: { flexShrink: 0, flexBasis: columnWidth, height: "100%" },
  card: { cursor: "grab", userSelect: "none", touchAction: "none", transition: "box-shadow 120ms ease", ":hover": { boxShadow: "var(--shadow-med)" } },
  cardBody: { minWidth: 0 },
  fixed: { cursor: "default" },
  floating: { position: "fixed", insetBlockStart: 0, insetInlineStart: 0, pointerEvents: "none", willChange: "transform", boxShadow: "var(--shadow-high)", zIndex: 1000 },
  floatingAt: (x: number, y: number, width: number) => ({ width, transform: `translate3d(${x}px, ${y}px, 0)` }),
  ghost: (height: number) => ({ height, borderRadius: "var(--radius-container)", backgroundColor: "var(--color-background-muted)" }),
});

function CardBody({ card }: { card: BoardCard }) {
  return (
    <VStack gap={2} xstyle={styles.cardBody}>
      <VStack gap={1} xstyle={styles.cardBody}>
        <Heading level={4}>
          <Link href={card.href} maxLines={1}>{card.title || card.id}</Link>
        </Heading>
        {card.lines.map((line, position) => (
          <Text key={position} type="supporting" color="secondary" maxLines={1}>
            {line}
          </Text>
        ))}
      </VStack>
      {(card.owner || card.updated) && (
        <HStack gap={2} vAlign="center">
          {card.owner && (
            <HStack gap={1} vAlign="center">
              <Avatar name={card.owner} size="xsm" tooltip={false} />
              <Text type="supporting" color="secondary" maxLines={1}>{card.owner}</Text>
            </HStack>
          )}
          <StackItem size="fill" />
          {card.updated && <Timestamp value={card.updated} format="relative" type="supporting" color="secondary" />}
        </HStack>
      )}
    </VStack>
  );
}

export function BoardView({ columns, cards, onMove, accepts, page, total }: BoardProps) {
  const gutter = useGutter();
  const [drag, setDrag] = useState<Drag | null>(null);
  const columnElements = useRef(new Map<string, HTMLElement>());
  const cardElements = useRef(new Map<string, HTMLElement>());
  const floatingElement = useRef<HTMLDivElement | null>(null);
  const teardown = useRef<(() => void) | null>(null);

  useEffect(() => () => teardown.current?.(), []);

  const press = (event: ReactPointerEvent, card: BoardCard) => {
    const element = cardElements.current.get(card.id);
    if (!onMove || event.button !== 0 || !element || (event.target as HTMLElement).closest("a, button")) return;
    event.preventDefault();
    window.getSelection()?.removeAllRanges();
    const box = element.getBoundingClientRect();
    const startX = event.clientX;
    const startY = event.clientY;
    const offsetX = startX - box.left;
    const offsetY = startY - box.top;
    const rects = [...columnElements.current].map(([key, node]) => ({ key, box: node.getBoundingClientRect() }));
    const columnAt = (x: number, y: number) => rects.find((entry) => x >= entry.box.left && x <= entry.box.right && y >= entry.box.top && y <= entry.box.bottom)?.key ?? null;
    const prevSelect = document.body.style.userSelect;
    const prevCursor = document.body.style.cursor;
    let started = false;
    let target: string | null = null;
    const move = (moved: PointerEvent) => {
      if (!started && Math.abs(moved.clientX - startX) + Math.abs(moved.clientY - startY) < dragThreshold) return;
      const x = moved.clientX - offsetX;
      const y = moved.clientY - offsetY;
      const over = columnAt(moved.clientX, moved.clientY);
      const nextTarget = over && (over === card.column || !accepts || accepts(card.id, over)) ? over : null;
      if (!started) {
        started = true;
        target = nextTarget;
        document.body.style.userSelect = "none";
        document.body.style.cursor = "grabbing";
        window.getSelection()?.removeAllRanges();
        setDrag({ id: card.id, width: box.width, height: box.height, x, y, target: nextTarget });
        return;
      }
      if (floatingElement.current) floatingElement.current.style.transform = `translate3d(${x}px, ${y}px, 0)`;
      if (nextTarget !== target) {
        target = nextTarget;
        setDrag((current) => (current ? { ...current, x, y, target: nextTarget } : null));
      }
    };
    const release = () => {
      teardown.current?.();
      if (started && target && target !== card.column) onMove(card.id, target);
      setDrag(null);
    };
    teardown.current = () => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", release);
      window.removeEventListener("pointercancel", release);
      document.body.style.userSelect = prevSelect;
      document.body.style.cursor = prevCursor;
      teardown.current = null;
    };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", release);
    window.addEventListener("pointercancel", release);
  };

  const lifted = drag ? cards.find((card) => card.id === drag.id) : undefined;

  const known = new Set(columns.map((column) => column.key));
  const missing = cards.filter((card) => !known.has(card.column));
  const column = ({ key, label, dot }: BoardProps["columns"][number]): ReactNode => {
    const own = key ? cards.filter((card) => card.column === key) : missing;
    const resting = drag ? own.filter((card) => card.id !== drag.id) : own;
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
                    {dot && <StatusDot variant={dot} label={label} />}
                    <Heading level={4}>{label}</Heading>
                  </HStack>
                  <Text type="supporting" color="secondary" hasTabularNumbers>{own.length.toLocaleString()}{page?.hasMore ? "+" : ""}</Text>
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
              ref={(element: HTMLDivElement | null) => {
                if (element && key) columnElements.current.set(key, element);
                else columnElements.current.delete(key);
              }}>
              <VStack gap={2}>
                {resting.map((card) => (
                  <Card
                    key={card.id}
                    padding={3}
                    xstyle={[styles.card, !onMove && styles.fixed]}
                    ref={(element: HTMLDivElement | null) => {
                      if (element) cardElements.current.set(card.id, element);
                      else cardElements.current.delete(card.id);
                    }}
                    onPointerDown={(event: ReactPointerEvent) => press(event, card)}>
                    <CardBody card={card} />
                  </Card>
                ))}
                {page?.hasMore && <Button label={t("Load more")} variant="secondary" size="sm" isLoading={page.loading} onClick={page.loadMore} />}
                {drag && drag.target === key && <VStack xstyle={styles.ghost(drag.height)} />}
              </VStack>
            </LayoutContent>
          }
        />
      </Card>
    );
  };

  return (
    <>
      <HStack gap={4} paddingInline={gutter} paddingBlockEnd={gutter} xstyle={styles.columns}>
        {columns.map(column)}
        {missing.length > 0 && column({ key: "", label: t("None") })}
      </HStack>
      {drag && lifted && (
        <Card ref={floatingElement} padding={3} xstyle={[styles.floating, styles.floatingAt(drag.x, drag.y, drag.width)]}>
          <CardBody card={lifted} />
        </Card>
      )}
    </>
  );
}
