import { Button } from "@astryxdesign/core/Button";
import { Icon } from "@astryxdesign/core/Icon";
import { IconButton } from "@astryxdesign/core/IconButton";
import { HStack, StackItem, VStack } from "@astryxdesign/core/Layout";
import { SegmentedControl, SegmentedControlItem } from "@astryxdesign/core/SegmentedControl";
import { Selector } from "@astryxdesign/core/Selector";
import { Heading, Text } from "@astryxdesign/core/Text";
import { ToggleButton } from "@astryxdesign/core/ToggleButton";
import { Tooltip } from "@astryxdesign/core/Tooltip";
import { ChevronDownIcon, ChevronLeftIcon, ChevronRightIcon, QueueListIcon } from "@heroicons/react/24/outline";
import { type KeyboardEvent as ReactKeyboardEvent, type PointerEvent as ReactPointerEvent, useEffect, useMemo, useRef, useState } from "react";
import { t, useLocale } from "../i18n";
import { DotMark } from "./DotMark";
import { useGutter } from "./frame";
import {
  addDays, buildGanttRows, buildTimeline, diffDays, type GanttItem, type GanttUnscheduledItem,
  laneAccentColor, nameColumnWidth, periodHeading, rowHeight, shiftMonth, stepMonthsFor, todayIso,
} from "./gantt";
import { ganttStyles as styles } from "./ganttStyles";
import { type GanttScale, ganttSpan } from "./view";

type MoveResult = Promise<unknown> | void;

export type GanttProps = {
  month: string;
  scale: GanttScale;
  startField: string;
  endField: string;
  fields: { key: string; label: string }[];
  items: GanttItem[];
  unscheduled: GanttUnscheduledItem[];
  grouped?: boolean;
  groupOrder?: string[];
  activeId?: string | null;
  onMonth: (month: string) => void;
  onScale: (scale: GanttScale) => void;
  onStartField: (field: string) => void;
  onEndField: (field: string) => void;
  onSelect: (id: string) => void;
  onMove?: (id: string, start: string, end: string) => MoveResult;
  accepts?: (id: string) => boolean;
  page?: { hasMore: boolean; loading: boolean; loadMore: () => void };
};

type DragSession = {
  id: string;
  mode: "move" | "resize";
  originStart: string;
  originEnd: string;
  deltaDays: number;
};

const dragThreshold = 4;
const overscan = 12;

export function GanttView({
  month, scale, startField, endField, fields, items, unscheduled, grouped = false, groupOrder,
  activeId, onMonth, onScale, onStartField, onEndField, onSelect, onMove, accepts, page,
}: GanttProps) {
  const gutter = useGutter();
  const locale = useLocale();
  const now = todayIso();
  const span = useMemo(() => ganttSpan(month, scale), [month, scale]);
  const timeline = useMemo(() => buildTimeline(span.start, span.days, scale, now, locale), [span.start, span.days, scale, now, locale]);
  const [collapsed, setCollapsed] = useState<Set<string>>(() => new Set());
  const [showUnscheduled, setShowUnscheduled] = useState(() => unscheduled.length > 0);
  const [optimistic, setOptimistic] = useState<Record<string, { start: string; end: string }>>({});
  const [drag, setDrag] = useState<DragSession | null>(null);
  const [scrollTop, setScrollTop] = useState(0);
  const [viewportHeight, setViewportHeight] = useState(600);
  const scrollerRef = useRef<HTMLDivElement | null>(null);
  const barRefs = useRef(new Map<string, HTMLElement>());
  const suppressClick = useRef(false);
  const teardown = useRef<(() => void) | null>(null);

  useEffect(() => () => teardown.current?.(), []);

  useEffect(() => {
    if (unscheduled.length > 0) setShowUnscheduled(true);
  }, [unscheduled.length]);

  useEffect(() => {
    const node = scrollerRef.current;
    if (!node) return;
    setViewportHeight(node.clientHeight || 600);
    const observer = new ResizeObserver(() => setViewportHeight(node.clientHeight || 600));
    observer.observe(node);
    return () => observer.disconnect();
  }, []);

  useEffect(() => {
    setOptimistic((previous) => {
      if (Object.keys(previous).length === 0) return previous;
      const next: Record<string, { start: string; end: string }> = {};
      for (const [id, dates] of Object.entries(previous)) {
        const entry = items.find((item) => item.id === id);
        if (entry && (entry.start !== dates.start || entry.end !== dates.end)) next[id] = dates;
      }
      return Object.keys(next).length === Object.keys(previous).length ? previous : next;
    });
  }, [items]);

  const effectiveItems = useMemo(
    () => items.map((item) => {
      const override = optimistic[item.id];
      const base = override ? { ...item, start: override.start, end: override.end } : item;
      if (drag && drag.id === item.id) {
        const nextStart = drag.mode === "move" ? addDays(drag.originStart, drag.deltaDays) : drag.originStart;
        const rawEnd = addDays(drag.originEnd, drag.deltaDays);
        const nextEnd = diffDays(nextStart, rawEnd) < 0 ? nextStart : rawEnd;
        return { ...base, start: nextStart, end: nextEnd };
      }
      return base;
    }),
    [items, optimistic, drag],
  );

  const rows = useMemo(
    () => buildGanttRows(effectiveItems, grouped, groupOrder, collapsed, t("Ungrouped")),
    [effectiveItems, grouped, groupOrder, collapsed],
  );

  const itemIds = useMemo(() => rows.flatMap((row) => (row.kind === "item" ? [row.item.id] : [])), [rows]);
  const visibleCount = Math.ceil(viewportHeight / rowHeight);
  const firstRow = Math.max(0, Math.floor(scrollTop / rowHeight) - overscan);
  const lastRow = Math.min(rows.length, firstRow + visibleCount + overscan * 2);
  const visibleRows = rows.slice(firstRow, lastRow);

  const commitMove = (id: string, nextStart: string, nextEnd: string) => {
    if (!onMove) return;
    setOptimistic((previous) => ({ ...previous, [id]: { start: nextStart, end: nextEnd } }));
    const revert = () => setOptimistic((previous) => {
      if (!(id in previous)) return previous;
      const { [id]: _, ...rest } = previous;
      return rest;
    });
    const pending = onMove(id, nextStart, nextEnd);
    if (pending && typeof pending.then === "function") {
      pending.catch(revert);
    }
  };

  const canEdit = (id: string) => Boolean(onMove && (!accepts || accepts(id)));

  const startPointerDrag = (event: ReactPointerEvent, item: GanttItem, mode: "move" | "resize") => {
    if (event.button !== 0 || !canEdit(item.id)) return;
    event.stopPropagation();
    event.preventDefault();
    const startX = event.clientX;
    let started = false;
    let latestDelta = 0;
    const prevCursor = document.body.style.cursor;
    const prevSelect = document.body.style.userSelect;

    const onPointerMove = (moved: PointerEvent) => {
      const dx = moved.clientX - startX;
      if (!started && Math.abs(dx) < dragThreshold) return;
      if (!started) {
        started = true;
        document.body.style.userSelect = "none";
        document.body.style.cursor = mode === "resize" ? "ew-resize" : "grabbing";
      }
      const deltaDays = Math.round(dx / timeline.dayWidth);
      if (deltaDays !== latestDelta || !drag) {
        latestDelta = deltaDays;
        setDrag({ id: item.id, mode, originStart: item.start, originEnd: item.end, deltaDays });
      }
    };

    const onPointerUp = () => {
      teardown.current?.();
      setDrag(null);
      if (!started) return;
      suppressClick.current = true;
      setTimeout(() => { suppressClick.current = false; }, 0);
      if (latestDelta === 0) return;
      const nextStart = mode === "move" ? addDays(item.start, latestDelta) : item.start;
      const rawEnd = addDays(item.end, latestDelta);
      const nextEnd = diffDays(nextStart, rawEnd) < 0 ? nextStart : rawEnd;
      if (nextStart !== item.start || nextEnd !== item.end) commitMove(item.id, nextStart, nextEnd);
    };

    teardown.current = () => {
      window.removeEventListener("pointermove", onPointerMove);
      window.removeEventListener("pointerup", onPointerUp);
      window.removeEventListener("pointercancel", onPointerUp);
      document.body.style.cursor = prevCursor;
      document.body.style.userSelect = prevSelect;
      teardown.current = null;
    };
    window.addEventListener("pointermove", onPointerMove);
    window.addEventListener("pointerup", onPointerUp);
    window.addEventListener("pointercancel", onPointerUp);
  };

  const handleBarKeyDown = (event: ReactKeyboardEvent, item: GanttItem) => {
    if (event.key === "Enter" || event.key === " ") {
      event.preventDefault();
      onSelect(item.id);
      return;
    }
    if (event.key === "ArrowUp" || event.key === "ArrowDown") {
      event.preventDefault();
      const index = itemIds.indexOf(item.id);
      const targetId = itemIds[event.key === "ArrowUp" ? index - 1 : index + 1];
      if (targetId) barRefs.current.get(targetId)?.focus();
      return;
    }
    if ((event.key === "ArrowLeft" || event.key === "ArrowRight") && canEdit(item.id)) {
      event.preventDefault();
      const step = event.key === "ArrowRight" ? 1 : -1;
      if (event.shiftKey) {
        const candidateEnd = addDays(item.end, step);
        const nextEnd = diffDays(item.start, candidateEnd) < 0 ? item.start : candidateEnd;
        if (nextEnd !== item.end) commitMove(item.id, item.start, nextEnd);
      } else {
        commitMove(item.id, addDays(item.start, step), addDays(item.end, step));
      }
    }
  };

  const toggleGroup = (label: string) => {
    setCollapsed((previous) => {
      const next = new Set(previous);
      if (!next.delete(label)) next.add(label);
      return next;
    });
  };

  const step = stepMonthsFor(scale);

  return (
    <VStack gap={3} paddingInline={gutter} paddingBlockEnd={gutter} xstyle={styles.root}>
      <HStack gap={2} vAlign="center" wrap="wrap">
        <Heading level={3} xstyle={styles.nowrap}>{periodHeading(month, scale, locale)}</Heading>
        <IconButton label={t("Previous period")} tooltip={t("Previous period")} variant="ghost" size="sm" icon={<Icon icon={ChevronLeftIcon} size="sm" />} onClick={() => onMonth(shiftMonth(month, -step))} />
        <IconButton label={t("Next period")} tooltip={t("Next period")} variant="ghost" size="sm" icon={<Icon icon={ChevronRightIcon} size="sm" />} onClick={() => onMonth(shiftMonth(month, step))} />
        <Button label={t("Today")} variant="secondary" size="sm" onClick={() => onMonth(now.slice(0, 7))} />
        <SegmentedControl size="sm" label={t("Time scale")} value={scale} onChange={(value) => onScale(value as GanttScale)}>
          <SegmentedControlItem value="day" label={t("Day")} />
          <SegmentedControlItem value="week" label={t("Week")} />
          <SegmentedControlItem value="month" label={t("Month")} />
        </SegmentedControl>
        <StackItem size="fill" />
        {fields.length > 2 && (
          <HStack gap={1} vAlign="center">
            <Selector label={t("Start date field")} isLabelHidden size="sm" variant="secondary" value={startField} onChange={onStartField}
              options={fields.map((choice) => ({ value: choice.key, label: choice.label }))} />
            <Selector label={t("End date field")} isLabelHidden size="sm" variant="secondary" value={endField} onChange={onEndField}
              options={fields.map((choice) => ({ value: choice.key, label: choice.label }))} />
          </HStack>
        )}
        <ToggleButton
          label={`${t("Unscheduled")} (${unscheduled.length})`}
          size="sm"
          isPressed={showUnscheduled}
          icon={<Icon icon={QueueListIcon} size="sm" />}
          onPressedChange={setShowUnscheduled}
        />
        {page?.hasMore && <Button label={t("Load more")} variant="secondary" size="sm" isLoading={page.loading} onClick={page.loadMore} />}
      </HStack>

      <HStack gap={0} xstyle={styles.shell}>
        <VStack
          ref={scrollerRef}
          gap={0}
          xstyle={styles.scroller}
          onScroll={(event) => setScrollTop(event.currentTarget.scrollTop)}
        >
          <VStack gap={0} xstyle={styles.canvas(nameColumnWidth + timeline.totalWidth)}>
            <HStack gap={0} xstyle={styles.headerRow}>
              <VStack gap={0} xstyle={styles.corner}>
                <Text type="label" color="secondary" maxLines={1}>{t("Record")}</Text>
              </VStack>
              <VStack gap={0}>
                <HStack gap={0}>
                  {timeline.bands.map((band) => (
                    <HStack key={band.key} vAlign="center" xstyle={styles.bandCell(band.width)}>
                      <Text type="label" color="secondary" maxLines={1}>{band.label}</Text>
                    </HStack>
                  ))}
                </HStack>
                <HStack gap={0}>
                  {timeline.ticks.map((tick) => (
                    <HStack key={tick.key} vAlign="center" xstyle={[styles.tickCell(tick.width), tick.weekend && styles.weekendTick, tick.today && styles.todayTick]}>
                      <Text type="supporting" color={tick.today ? "accent" : "secondary"} weight={tick.today ? "semibold" : undefined} hasTabularNumbers maxLines={1}>{tick.label}</Text>
                    </HStack>
                  ))}
                </HStack>
              </VStack>
            </HStack>

            <VStack gap={0} xstyle={styles.body}>
              {rows.length === 0 && (
                <VStack padding={4} hAlign="center">
                  <Text type="supporting" color="secondary">{t("No scheduled records in this period")}</Text>
                </VStack>
              )}
              {firstRow > 0 && <VStack xstyle={styles.spacer(firstRow * rowHeight)} />}
              {visibleRows.map((row) => {
                if (row.kind === "group") {
                  return (
                    <HStack key={row.key} gap={0} vAlign="center" xstyle={[styles.row, styles.groupRow]}>
                      <HStack
                        gap={1.5}
                        vAlign="center"
                        role="button"
                        tabIndex={0}
                        aria-expanded={!row.collapsed}
                        aria-label={row.label}
                        xstyle={[styles.nameCell, styles.groupNameCell]}
                        onClick={() => toggleGroup(row.label)}
                        onKeyDown={(event) => {
                          if (event.key === "Enter" || event.key === " ") {
                            event.preventDefault();
                            toggleGroup(row.label);
                          }
                        }}
                      >
                        <Icon icon={row.collapsed ? ChevronRightIcon : ChevronDownIcon} size="sm" />
                        <Text type="label" color="primary" maxLines={1}>{row.label}</Text>
                        <Text type="supporting" color="secondary" hasTabularNumbers>{row.count}</Text>
                      </HStack>
                      <HStack gap={0} xstyle={styles.track(timeline.totalWidth)}>
                        {timeline.weekends.map((band, index) => <VStack key={index} xstyle={styles.weekendBand(band.left, band.width)} />)}
                        {timeline.todayX !== null && <VStack xstyle={styles.todayLine(timeline.todayX)} />}
                      </HStack>
                    </HStack>
                  );
                }
                const entry = row.item;
                const editable = canEdit(entry.id);
                const isSelected = activeId === entry.id;
                const isDragging = drag?.id === entry.id;
                const startOffset = diffDays(span.start, entry.start);
                const durationDays = Math.max(1, diffDays(entry.start, entry.end) + 1);
                const clippedStart = Math.max(0, startOffset);
                const clippedEnd = Math.min(span.days, startOffset + durationDays);
                const visibleBar = clippedEnd > clippedStart;
                const left = clippedStart * timeline.dayWidth;
                const width = Math.max(24, (clippedEnd - clippedStart) * timeline.dayWidth - 2);
                const accent = laneAccentColor(entry.dot);
                const rangeSummary = `${entry.title || entry.id} (${entry.start} – ${entry.end})`;
                return (
                  <HStack key={row.key} gap={0} vAlign="center" xstyle={[styles.row, isSelected && styles.activeRow]}>
                    <HStack
                      gap={2}
                      vAlign="center"
                      role="button"
                      tabIndex={0}
                      aria-current={isSelected ? "true" : undefined}
                      xstyle={[styles.nameCell, isSelected && styles.activeNameCell]}
                      onClick={() => onSelect(entry.id)}
                      onKeyDown={(event) => {
                        if (event.key === "Enter" || event.key === " ") {
                          event.preventDefault();
                          onSelect(entry.id);
                        }
                      }}
                    >
                      {entry.dot && <DotMark dot={entry.dot} label={entry.title || entry.id} />}
                      <Text type="body" color="primary" maxLines={1}>{entry.title || entry.id}</Text>
                    </HStack>
                    <HStack gap={0} xstyle={styles.track(timeline.totalWidth)}>
                      {timeline.weekends.map((band, index) => <VStack key={index} xstyle={styles.weekendBand(band.left, band.width)} />)}
                      {timeline.todayX !== null && <VStack xstyle={styles.todayLine(timeline.todayX)} />}
                      {visibleBar && (
                        <Tooltip content={rangeSummary} hasHoverIndication={false}>
                          <HStack
                            ref={(node: HTMLElement | null) => {
                              if (node) barRefs.current.set(entry.id, node);
                              else barRefs.current.delete(entry.id);
                            }}
                            role="button"
                            tabIndex={0}
                            aria-label={rangeSummary}
                            aria-current={isSelected ? "true" : undefined}
                            gap={1}
                            vAlign="center"
                            xstyle={[
                              styles.barBase,
                              styles.barAt(left, width, accent),
                              editable ? styles.barDraggable : styles.barFixed,
                              isSelected && styles.barActive,
                              isDragging && styles.barDragging,
                            ]}
                            onPointerDown={(event) => startPointerDrag(event, entry, "move")}
                            onClick={() => { if (!suppressClick.current) onSelect(entry.id); }}
                            onKeyDown={(event) => handleBarKeyDown(event, entry)}
                          >
                            <Text type="supporting" color="primary" weight="medium" maxLines={1} xstyle={styles.barLabel}>
                              {entry.title || entry.id}
                            </Text>
                            {editable && (
                              <VStack
                                role="presentation"
                                aria-label={t("Resize end date")}
                                xstyle={styles.resizeHandle}
                                onPointerDown={(event) => startPointerDrag(event, entry, "resize")}
                              />
                            )}
                          </HStack>
                        </Tooltip>
                      )}
                    </HStack>
                  </HStack>
                );
              })}
              {lastRow < rows.length && <VStack xstyle={styles.spacer((rows.length - lastRow) * rowHeight)} />}
            </VStack>
          </VStack>
        </VStack>

        {showUnscheduled && (
          <VStack gap={0} role="region" aria-label={t("Unscheduled")} xstyle={styles.sidebar}>
            <HStack vAlign="center" hAlign="between" xstyle={styles.sidebarHeader}>
              <Text type="label" color="secondary">{t("Unscheduled")}</Text>
              <Text type="supporting" color="secondary" hasTabularNumbers>{unscheduled.length}</Text>
            </HStack>
            <VStack gap={1.5} xstyle={styles.sidebarBody}>
              {unscheduled.length === 0 ? (
                <Text type="supporting" color="secondary">{t("All records have dates")}</Text>
              ) : (
                unscheduled.map((entry) => (
                  <HStack
                    key={entry.id}
                    gap={2}
                    vAlign="center"
                    role="button"
                    tabIndex={0}
                    xstyle={styles.unscheduledItem}
                    onClick={() => onSelect(entry.id)}
                    onKeyDown={(event) => {
                      if (event.key === "Enter" || event.key === " ") {
                        event.preventDefault();
                        onSelect(entry.id);
                      }
                    }}
                  >
                    {entry.dot && <DotMark dot={entry.dot} label={entry.title || entry.id} />}
                    <Text type="supporting" color="primary" maxLines={1}>{entry.title || entry.id}</Text>
                  </HStack>
                ))
              )}
            </VStack>
          </VStack>
        )}
      </HStack>
    </VStack>
  );
}
