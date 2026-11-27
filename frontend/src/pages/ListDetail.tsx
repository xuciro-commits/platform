import { Button } from "@astryxdesign/core/Button";
import { HStack, VStack } from "@astryxdesign/core/Layout";
import { Link } from "@astryxdesign/core/Link";
import { MetadataList, MetadataListItem } from "@astryxdesign/core/MetadataList";
import { Text } from "@astryxdesign/core/Text";
import { type ReactNode, useState } from "react";
import { t } from "../i18n";
import {
  AttachmentValue,
  BooleanValue,
  ChoiceValue,
  CodeValue,
  ContactValue,
  MoneyValue,
  NumberValue,
  PercentValue,
  PersonValue,
  QuantityValue,
  ReferenceValue,
  StatusValue,
  TagListValue,
  TemporalValue,
  UrlValue,
} from "../semantic";
import type { ListColumnKind } from "./listShape";
import { PeekDrawer, type PeekSurfaceMode, RecordPeek } from "./RecordPeek";
import type { Dot } from "./Transitions";

export type Cells = Record<string, string>;
export const idKey = "__id";
export const dotOf = (key: string) => `__dot:${key}`;
export const toneOf = (key: string) => `__tone:${key}`;
export const typeOf = (key: string) => `__type:${key}`;
export const targetOf = (key: string) => `__target:${key}`;
export const rawOf = (key: string) => `__raw:${key}`;

type Column = { key: string; header: string; kind?: ListColumnKind; boolean?: boolean; integer?: boolean };

function parseSerialized(raw?: string): unknown {
  if (!raw) return undefined;
  try {
    return JSON.parse(raw);
  } catch {
    return raw;
  }
}

export function ListCell({ column, cells, first }: { column: Column; cells: Cells; first: boolean }) {
  const value = cells[column.key] ?? "";
  const dot = cells[dotOf(column.key)] as Dot | undefined;
  const tone = cells[toneOf(column.key)] || undefined;
  const targetId = cells[targetOf(column.key)] || undefined;
  const rawJson = cells[rawOf(column.key)];

  if (first && cells.__href) {
    return (
      <Link href={cells.__href} color="primary" weight="normal" maxLines={1}>
        {(column.kind === "member" || column.kind === "person") && value ? (
          <PersonValue id={targetId} name={value} size="sm" card={false} />
        ) : (
          value || cells[idKey]
        )}
      </Link>
    );
  }

  if (dot && value) {
    return <StatusValue label={value} dot={dot} tone={tone} size="sm" appearance="dot" />;
  }

  if (cells[typeOf(column.key)] && targetId) {
    return <ReferenceValue type={cells[typeOf(column.key)]} id={targetId} name={value} size="sm" />;
  }

  if ((column.kind === "member" || column.kind === "person") && (value || targetId)) {
    return <PersonValue id={targetId} name={value || undefined} size="sm" />;
  }

  if (column.boolean) {
    const checked = rawJson ? rawJson === "true" : value === t("Yes");
    return <BooleanValue value={checked} size="sm" surface="cell" />;
  }

  if (column.kind === "money") {
    const parsed = parseSerialized(rawJson);
    if (parsed) return <MoneyValue value={parsed} size="sm" align="end" />;
    return value ? <Text type="body" hasTabularNumbers maxLines={1}>{value}</Text> : null;
  }

  if (column.kind === "percent") {
    const parsed = parseSerialized(rawJson);
    if (parsed != null && parsed !== "") return <PercentValue value={parsed} size="sm" />;
    return value ? <Text type="body" hasTabularNumbers maxLines={1}>{value}</Text> : null;
  }

  if (column.kind === "quantity") {
    const parsed = parseSerialized(rawJson);
    if (parsed) return <QuantityValue value={parsed} size="sm" />;
  }

  if (column.kind === "number" && rawJson !== undefined && rawJson !== "") {
    const parsed = parseSerialized(rawJson);
    if (typeof parsed === "number" && Number.isFinite(parsed)) {
      return <NumberValue value={parsed} integer={column.integer} size="sm" align="end" />;
    }
  }

  if (column.kind === "tags" && rawJson) {
    const parsed = parseSerialized(rawJson);
    if (Array.isArray(parsed)) return <TagListValue items={parsed.map(String)} size="sm" max={2} />;
  }

  if (column.kind === "attachment" && rawJson) {
    return <AttachmentValue value={parseSerialized(rawJson)} size="sm" />;
  }

  if (column.kind === "url" && (rawJson || value)) {
    return <UrlValue value={parseSerialized(rawJson) ?? value} size="sm" surface="cell" interactive={false} />;
  }

  if ((column.kind === "email" || column.kind === "phone") && value) {
    return <ContactValue kind={column.kind} value={value} size="sm" surface="cell" interactive={false} />;
  }

  if (column.kind === "code" && value) {
    return <CodeValue value={value} size="sm" />;
  }

  if (column.kind === "enum" && value) {
    return <ChoiceValue value={value} tone={tone} size="sm" />;
  }

  if (column.kind === "date" && value) {
    return <TemporalValue value={value} kind="date" size="sm" surface="cell" />;
  }

  if (column.kind === "time" && value) {
    return <TemporalValue value={value} kind="datetime" mode="relative" size="sm" surface="cell" />;
  }

  if (!value) return null;
  return (
    <Text type="body" maxLines={1} hasTabularNumbers={column.kind === "number"}>
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
        <StatusValue
          label={shown[state.key]}
          dot={shown[dotOf(state.key)] as Dot}
          tone={shown[toneOf(state.key)] || undefined}
          size="xs"
          appearance="pill"
        />
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
