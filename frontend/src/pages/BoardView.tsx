import { Card } from "@astryxdesign/core/Card";
import { Button } from "@astryxdesign/core/Button";
import { HStack, Layout, LayoutContent, LayoutHeader, VStack } from "@astryxdesign/core/Layout";
import { Link } from "@astryxdesign/core/Link";
import { Heading, Text } from "@astryxdesign/core/Text";
import * as stylex from "@stylexjs/stylex";
import { type PointerEvent as ReactPointerEvent, type ReactNode, useEffect, useRef, useState } from "react";
import { t } from "../i18n";

export type BoardCard = { id: string; column: string; title: string; href: string; lines: string[] };
type BoardProps = {
  columns: string[];
  cards: BoardCard[];
  onMove?: (id: string, to: string) => void;
  accepts?: (id: string, to: string) => boolean;
  page?: { hasMore: boolean; loading: boolean; loadMore: () => void };
};
type Drag = { id: string; width: number; height: number; offsetX: number; offsetY: number; pointerX: number; pointerY: number; target: string | null };

const dragThreshold = 5;
const columnWidth = 300;

const styles = stylex.create({
  columns: { overflowX: "auto", overflowY: "hidden", height: "100%", padding: "var(--spacing-4)" },
  column: { flexShrink: 0, flexBasis: columnWidth, height: "100%" },
  card: { cursor: "grab", userSelect: "none", touchAction: "none", transition: "box-shadow 120ms ease", ":hover": { boxShadow: "var(--shadow-med)" } },
  fixed: { cursor: "default" },
  floating: { position: "fixed", insetBlockStart: 0, insetInlineStart: 0, pointerEvents: "none", cursor: "grabbing", boxShadow: "var(--shadow-high)", zIndex: 1000 },
  floatingAt: (x: number, y: number, width: number) => ({ width, transform: `translate(${x}px, ${y}px)` }),
  ghost: (height: number) => ({ height, borderRadius: "var(--radius-container)", backgroundColor: "var(--color-background-muted)" }),
});

function CardBody({ card }: { card: BoardCard }) {
  return (
    <VStack gap={1}>
      <Heading level={4}>
        <Link href={card.href}>{card.title || card.id}</Link>
      </Heading>
      {card.lines.map((line, position) => (
        <Text key={position} type="supporting" color="secondary" maxLines={1}>
          {line}
        </Text>
      ))}
    </VStack>
  );
}

export function BoardView({ columns, cards, onMove, accepts, page }: BoardProps) {
  const [drag, setDrag] = useState<Drag | null>(null);
  const columnElements = useRef(new Map<string, HTMLElement>());
  const cardElements = useRef(new Map<string, HTMLElement>());
  const teardown = useRef<(() => void) | null>(null);
  const dragging = drag !== null;

  useEffect(() => {
    if (!dragging) return;
    const previous = document.body.style.userSelect;
    document.body.style.userSelect = "none";
    return () => {
      document.body.style.userSelect = previous;
    };
  }, [dragging]);

  useEffect(() => () => teardown.current?.(), []);

  const columnAt = (x: number, y: number) => {
    for (const [column, element] of columnElements.current) {
      const box = element.getBoundingClientRect();
      if (x >= box.left && x <= box.right && y >= box.top && y <= box.bottom) return column;
    }
    return null;
  };

  const press = (event: ReactPointerEvent, card: BoardCard) => {
    const element = cardElements.current.get(card.id);
    if (!onMove || event.button !== 0 || !element || (event.target as HTMLElement).closest("a, button")) return;
    const box = element.getBoundingClientRect();
    const startX = event.clientX;
    const startY = event.clientY;
    let started = false;
    let target: string | null = null;
    const move = (moved: PointerEvent) => {
      if (!started && Math.abs(moved.clientX - startX) + Math.abs(moved.clientY - startY) < dragThreshold) return;
      started = true;
      const over = columnAt(moved.clientX, moved.clientY);
      target = over && (over === card.column || !accepts || accepts(card.id, over)) ? over : null;
      setDrag({ id: card.id, width: box.width, height: box.height, offsetX: startX - box.left, offsetY: startY - box.top, pointerX: moved.clientX, pointerY: moved.clientY, target });
    };
    const release = () => {
      teardown.current?.();
      if (started && target && target !== card.column) onMove(card.id, target);
      setDrag(null);
    };
    teardown.current = () => {
      window.removeEventListener("pointermove", move);
      window.removeEventListener("pointerup", release);
      teardown.current = null;
    };
    window.addEventListener("pointermove", move);
    window.addEventListener("pointerup", release);
  };

  const lifted = drag ? cards.find((card) => card.id === drag.id) : undefined;

  const column = (name: string): ReactNode => {
    const own = cards.filter((card) => card.column === name);
    const resting = drag ? own.filter((card) => card.id !== drag.id) : own;
    return (
      <Card key={name} variant="muted" padding={0} xstyle={styles.column}>
        <Layout
          height="fill"
          header={
            <LayoutHeader hasDivider padding={3}>
              <HStack hAlign="between" vAlign="center">
                <Heading level={4}>{name}</Heading>
                <Text type="supporting" color="secondary" hasTabularNumbers>
                  {String(own.length)}
                </Text>
              </HStack>
            </LayoutHeader>
          }
          content={
            <LayoutContent
              role="region"
              label={name}
              padding={2}
              ref={(element: HTMLDivElement | null) => {
                if (element) columnElements.current.set(name, element);
                else columnElements.current.delete(name);
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
                {drag && drag.target === name && <VStack xstyle={styles.ghost(drag.height)} />}
              </VStack>
            </LayoutContent>
          }
        />
      </Card>
    );
  };

  return (
    <>
      <HStack gap={4} xstyle={styles.columns}>
        {columns.map(column)}
      </HStack>
      {drag && lifted && (
        <Card padding={3} xstyle={[styles.floating, styles.floatingAt(drag.pointerX - drag.offsetX, drag.pointerY - drag.offsetY, drag.width)]}>
          <CardBody card={lifted} />
        </Card>
      )}
    </>
  );
}
