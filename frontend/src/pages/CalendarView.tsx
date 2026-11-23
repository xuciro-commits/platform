import { Button } from "@astryxdesign/core/Button";
import { type ISODateString, useCalendarDays, useCalendarNavigation } from "@astryxdesign/core/Calendar";
import { Card } from "@astryxdesign/core/Card";
import { ClickableCard } from "@astryxdesign/core/ClickableCard";
import { Grid } from "@astryxdesign/core/Grid";
import { Icon } from "@astryxdesign/core/Icon";
import { IconButton } from "@astryxdesign/core/IconButton";
import { HStack, StackItem, VStack } from "@astryxdesign/core/Layout";
import { Selector } from "@astryxdesign/core/Selector";
import { Heading, Text } from "@astryxdesign/core/Text";
import { colorVars, radiusVars } from "@astryxdesign/core/theme/tokens.stylex";
import { ChevronLeftIcon, ChevronRightIcon } from "@heroicons/react/24/outline";
import * as stylex from "@stylexjs/stylex";
import type { PointerEvent as ReactPointerEvent } from "react";
import { t } from "../i18n";
import { dragStyles, useDrag } from "./drag";
import { useGutter } from "./frame";

export type CalendarItem = { id: string; day: string; title: string; href: string };
type CalendarProps = {
  month: string;
  field: string;
  fields: { key: string; label: string }[];
  items: CalendarItem[];
  onMonth: (month: string) => void;
  onField: (field: string) => void;
  onMove?: (id: string, day: string) => void;
  accepts?: (id: string) => boolean;
  page?: { hasMore: boolean; loading: boolean; loadMore: () => void };
};

const styles = stylex.create({
  board: {
    borderWidth: 1,
    borderStyle: "solid",
    borderColor: colorVars["--color-border"],
    borderRadius: radiusVars["--radius-container"],
    backgroundColor: colorVars["--color-border"],
    overflow: "hidden",
  },
  grid: { gap: 1 },
  weekday: {
    backgroundColor: colorVars["--color-background-body"],
    paddingBlock: 8,
    paddingInline: 10,
  },
  cell: {
    minHeight: 88,
    minWidth: 0,
    backgroundColor: colorVars["--color-background-surface"],
  },
  outside: {
    backgroundColor: colorVars["--color-background-body"],
  },
  todayBadge: {
    display: "inline-flex",
    alignItems: "center",
    justifyContent: "center",
    width: 24,
    height: 24,
    borderRadius: 999,
    backgroundColor: colorVars["--color-accent-muted"],
  },
  dayNumber: {
    display: "inline-flex",
    alignItems: "center",
    height: 24,
    paddingInline: 4,
  },
  title: { minWidth: 0 },
});

const today = () => {
  const now = new Date();
  return `${now.getFullYear()}-${String(now.getMonth() + 1).padStart(2, "0")}-${String(now.getDate()).padStart(2, "0")}`;
};

export function CalendarView({ month, field, fields, items, onMonth, onField, onMove, accepts, page }: CalendarProps) {
  const gutter = useGutter();
  const [year, number] = month.split("-").map(Number);
  const { monthYearLabel, navigateMonth } = useCalendarNavigation({ focusDate: `${month}-01` as ISODateString, onFocusDateChange: (focus) => onMonth(focus.slice(0, 7)) });
  const { weeks, dayNames } = useCalendarDays({ year, month: number, weekStartsOn: 1 });
  const { drag, press, floating, target, item } = useDrag(onMove, accepts);
  const now = today();
  const lifted = drag ? items.find((entry) => entry.id === drag.id) : undefined;
  return (
    <VStack gap={3} paddingInline={gutter} paddingBlockEnd={gutter}>
      <HStack gap={2} vAlign="center" wrap="wrap">
        <Heading level={3}>{monthYearLabel}</Heading>
        <IconButton label={t("Previous month")} tooltip={t("Previous month")} variant="ghost" size="sm" icon={<Icon icon={ChevronLeftIcon} size="sm" />} onClick={() => navigateMonth(-1)} />
        <IconButton label={t("Next month")} tooltip={t("Next month")} variant="ghost" size="sm" icon={<Icon icon={ChevronRightIcon} size="sm" />} onClick={() => navigateMonth(1)} />
        <Button label={t("Today")} variant="secondary" size="sm" onClick={() => onMonth(now.slice(0, 7))} />
        <StackItem size="fill" />
        {fields.length > 1 && (
          <Selector label={t("Date field")} isLabelHidden size="sm" variant="secondary" value={field} onChange={onField}
            options={fields.map((choice) => ({ value: choice.key, label: choice.label }))} />
        )}
        {page?.hasMore && <Button label={t("Load more")} variant="secondary" size="sm" isLoading={page.loading} onClick={page.loadMore} />}
      </HStack>
      <VStack xstyle={styles.board}>
        <Grid columns={7} gap={0} xstyle={styles.grid}>
          {dayNames.map((name) => (
            <VStack key={name} xstyle={styles.weekday}>
              <Text type="label" color="secondary">{name}</Text>
            </VStack>
          ))}
          {weeks.flat().map((day) => {
            const own = items.filter((entry) => entry.day === day.iso && entry.id !== drag?.id);
            const label = new Date(`${day.iso}T00:00:00`).toLocaleDateString(undefined, { dateStyle: "long" });
            const isToday = day.iso === now;
            return (
              <VStack key={day.iso} ref={target(day.iso)} role="group" aria-label={label} aria-current={isToday ? "date" : undefined} gap={1} padding={2} xstyle={[styles.cell, day.isOutside && styles.outside]}>
                <HStack xstyle={isToday ? styles.todayBadge : styles.dayNumber}>
                  <Text type="supporting" color={isToday ? "accent" : day.isOutside ? "disabled" : "secondary"} weight={isToday ? "semibold" : undefined}>{day.dayNumber}</Text>
                </HStack>
                {own.map((entry) => (
                  <ClickableCard key={entry.id} label={entry.title || entry.id} href={entry.href} padding={1} ref={item(entry.id)} xstyle={[dragStyles.item, !onMove && dragStyles.fixed]}
                    onPointerDown={(event: ReactPointerEvent) => press(event, entry.id, entry.day)}>
                    <Text type="supporting" color="primary" maxLines={1} xstyle={styles.title}>{entry.title || entry.id}</Text>
                  </ClickableCard>
                ))}
                {drag && drag.target === day.iso && <VStack xstyle={dragStyles.ghost(drag.height)} />}
              </VStack>
            );
          })}
        </Grid>
      </VStack>
      {drag && lifted && (
        <Card ref={floating} padding={1} xstyle={[dragStyles.floating, dragStyles.floatingAt(drag.x, drag.y, drag.width)]}>
          <Text type="body" maxLines={1}>{lifted.title || lifted.id}</Text>
        </Card>
      )}
    </VStack>
  );
}
