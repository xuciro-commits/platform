import { HoverCard } from "@astryxdesign/core/HoverCard";
import { Icon } from "@astryxdesign/core/Icon";
import { HStack, VStack } from "@astryxdesign/core/Layout";
import { Text } from "@astryxdesign/core/Text";
import { CalendarDaysIcon } from "@heroicons/react/24/outline";
import type { PointerEvent as ReactPointerEvent } from "react";
import { t } from "../i18n";
import { DotMark } from "./DotMark";
import type { GanttItem, GanttUnscheduledItem } from "./gantt";
import { ganttStyles as styles } from "./ganttStyles";

export function GanttBarCard({ item, durationDays, editable }: { item: GanttItem; durationDays: number; editable: boolean }) {
  return (
    <VStack gap={1.5}>
      <HStack gap={2} vAlign="center">
        {item.dot && <DotMark dot={item.dot} label={item.title || item.id} />}
        <Text type="body" weight="semibold" maxLines={2}>{item.title || item.id}</Text>
      </HStack>
      <HStack gap={1.5} vAlign="center">
        <Icon icon={CalendarDaysIcon} size="sm" color="secondary" />
        <Text type="supporting" color="secondary" hasTabularNumbers>{`${item.start} → ${item.end} · ${durationDays} ${t("days")}`}</Text>
      </HStack>
      {item.group && <Text type="supporting" color="secondary">{item.group}</Text>}
      {editable && <Text type="supporting" color="secondary">{t("Drag to move · Drag edges to resize")}</Text>}
    </VStack>
  );
}

type SidebarProps = {
  items: GanttUnscheduledItem[];
  hoveringEnabled: boolean;
  draggingId?: string;
  canEdit: (id: string) => boolean;
  onDragStart: (event: ReactPointerEvent, entry: GanttUnscheduledItem) => void;
  onSelect: (id: string) => void;
};

export function GanttUnscheduled({ items, hoveringEnabled, draggingId, canEdit, onDragStart, onSelect }: SidebarProps) {
  return (
    <VStack gap={0} role="region" aria-label={t("Unscheduled")} xstyle={styles.sidebar}>
      <HStack vAlign="center" hAlign="between" xstyle={styles.sidebarHeader}>
        <Text type="label" color="secondary">{t("Unscheduled")}</Text>
        <Text type="supporting" color="secondary" hasTabularNumbers>{items.length}</Text>
      </HStack>
      <VStack gap={1.5} xstyle={styles.sidebarBody}>
        {items.length === 0 ? (
          <Text type="supporting" color="secondary">{t("All records have dates")}</Text>
        ) : (
          items.map((entry) => {
            const editable = canEdit(entry.id);
            return (
              <HoverCard
                key={entry.id}
                isEnabled={hoveringEnabled}
                delay={240}
                hideDelay={120}
                placement="start"
                xstyle={styles.hoverCard}
                content={
                  <VStack gap={1.5}>
                    <HStack gap={2} vAlign="center">
                      {entry.dot && <DotMark dot={entry.dot} label={entry.title || entry.id} />}
                      <Text type="body" weight="semibold" maxLines={2}>{entry.title || entry.id}</Text>
                    </HStack>
                    <HStack gap={1.5} vAlign="center">
                      <Icon icon={CalendarDaysIcon} size="sm" color="secondary" />
                      <Text type="supporting" color="secondary">{t("Unscheduled")}</Text>
                    </HStack>
                    {editable && <Text type="supporting" color="secondary">{t("Drag onto timeline to schedule")}</Text>}
                  </VStack>
                }
              >
                <HStack
                  gap={2}
                  vAlign="center"
                  role="button"
                  tabIndex={0}
                  xstyle={[
                    styles.unscheduledItem,
                    editable && styles.unscheduledDraggable,
                    draggingId === entry.id && styles.unscheduledDragging,
                  ]}
                  onPointerDown={(event) => onDragStart(event, entry)}
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
              </HoverCard>
            );
          })
        )}
      </VStack>
    </VStack>
  );
}
