import { Button } from "@astryxdesign/core/Button";
import { Icon } from "@astryxdesign/core/Icon";
import { HStack, VStack } from "@astryxdesign/core/Layout";
import { Link } from "@astryxdesign/core/Link";
import { MetadataList, MetadataListItem } from "@astryxdesign/core/MetadataList";
import { StatusDot } from "@astryxdesign/core/StatusDot";
import { Text } from "@astryxdesign/core/Text";
import { Timestamp } from "@astryxdesign/core/Timestamp";
import { Tooltip } from "@astryxdesign/core/Tooltip";
import { type ReactNode, useState } from "react";
import { t } from "../i18n";
import { PersonLabel } from "./MemberField";
import { PeekDrawer, type PeekSurfaceMode, RecordPeek } from "./RecordPeek";
import { ReferenceLink } from "./ReferenceField";
import type { Dot } from "./Transitions";

export type Cells = Record<string, string>;
export const idKey = "__id";
export const dotOf = (key: string) => `__dot:${key}`;
export const typeOf = (key: string) => `__type:${key}`;
export const targetOf = (key: string) => `__target:${key}`;

type Column = { key: string; header: string; kind?: string; boolean?: boolean };

export function ListCell({ column, cells, first }: { column: Column; cells: Cells; first: boolean }) {
  const value = cells[column.key] ?? "";
  if (first && cells.__href) {
    return (
      <Link href={cells.__href} color="primary" weight="medium" maxLines={1}>
        {(column.kind === "member" || column.kind === "person") && value ? <PersonLabel name={value} /> : value || cells[idKey]}
      </Link>
    );
  }
  if (cells[dotOf(column.key)]) {
    return (
      <HStack gap={2} vAlign="center">
        <StatusDot variant={cells[dotOf(column.key)] as Dot} label={value} />
        <Text type="body" maxLines={1}>
          {value}
        </Text>
      </HStack>
    );
  }
  if (cells[targetOf(column.key)]) {
    return <ReferenceLink type={cells[typeOf(column.key)]} id={cells[targetOf(column.key)]} name={value} />;
  }
  if ((column.kind === "member" || column.kind === "person") && value) {
    return <PersonLabel name={value} />;
  }
  if ((column.kind === "email" || column.kind === "phone") && value) {
    return (
      <Tooltip content={value} hasHoverIndication={false}>
        <Text type="body" maxLines={1} hasTabularNumbers={column.kind === "phone"}>
          {value}
        </Text>
      </Tooltip>
    );
  }
  if (column.kind === "code") {
    return (
      <Text type="code" color="secondary" maxLines={1}>
        {value}
      </Text>
    );
  }
  if (column.boolean && value === t("Yes")) {
    return <Icon icon="check" size="sm" label={t("Yes")} />;
  }
  if (column.kind === "time" && value) {
    return (
      <Text type="body" color="secondary" hasTabularNumbers>
        <Timestamp value={value} format="relative" />
      </Text>
    );
  }
  return (
    <Text type="body" maxLines={1} weight={first ? "medium" : undefined} hasTabularNumbers={column.kind === "number"}>
      {value}
    </Text>
  );
}

type ListDetailProps = {
  recordType?: string;
  activeId: string | null;
  columns: Column[];
  title?: Column;
  cells?: Cells;
  actions?: ReactNode;
  surface: PeekSurfaceMode;
  onClose: () => void;
};

export function ListDetail({ recordType, activeId, columns, title, cells, actions, surface, onClose }: ListDetailProps) {
  const [kept, setKept] = useState<Cells>();
  if (cells && cells !== kept) setKept(cells);
  if (recordType) {
    return (
      <RecordPeek
        type={recordType}
        id={activeId ?? undefined}
        name={cells && title ? cells[title.key] || cells[idKey] : undefined}
        isOpen={Boolean(activeId)}
        onOpenChange={(open) => !open && onClose()}
        hasScrim={false}
        surface={surface}
        label={t("Details")}
      />
    );
  }
  const shown = cells ?? kept;
  if (!shown || !title) return null;
  const state = columns.find((column) => shown[dotOf(column.key)]);
  const heading = shown[title.key] || shown[idKey];
  return (
    <PeekDrawer
      isOpen={Boolean(cells)}
      onOpenChange={(open) => !open && onClose()}
      hasScrim={false}
      surface={surface}
      label={t("Details")}
      title={heading}
      subtitle={state && (
        <HStack gap={2} vAlign="center">
          <StatusDot variant={shown[dotOf(state.key)] as Dot} label={shown[state.key]} />
          <Text type="supporting" color="secondary">{shown[state.key]}</Text>
        </HStack>
      )}
      footer={shown.__href ? (
        <HStack gap={2} hAlign="end">
          <Button label={t("Open full page")} variant="secondary" href={shown.__href} onClick={onClose} />
        </HStack>
      ) : undefined}
    >
      <VStack gap={4}>
        {actions && <HStack gap={2} vAlign="center" wrap="wrap">{actions}</HStack>}
        <MetadataList columns="single" label={{ position: "start", width: 120 }}>
          {columns
            .filter((column) => column.key !== title.key && column.key !== state?.key && Boolean(shown[column.key]))
            .map((column) => (
              <MetadataListItem key={column.key} label={t(column.header)}>
                <ListCell column={column} cells={shown} first={false} />
              </MetadataListItem>
            ))}
        </MetadataList>
      </VStack>
    </PeekDrawer>
  );
}
