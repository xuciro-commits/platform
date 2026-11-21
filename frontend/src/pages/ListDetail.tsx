import { Button } from "@astryxdesign/core/Button";
import { Divider } from "@astryxdesign/core/Divider";
import { Icon } from "@astryxdesign/core/Icon";
import { IconButton } from "@astryxdesign/core/IconButton";
import { HStack, StackItem, VStack } from "@astryxdesign/core/Layout";
import { Link } from "@astryxdesign/core/Link";
import { MetadataList, MetadataListItem } from "@astryxdesign/core/MetadataList";
import { Section } from "@astryxdesign/core/Section";
import { StatusDot } from "@astryxdesign/core/StatusDot";
import { Heading, Text } from "@astryxdesign/core/Text";
import { Timestamp } from "@astryxdesign/core/Timestamp";
import { XMarkIcon } from "@heroicons/react/24/outline";
import * as stylex from "@stylexjs/stylex";
import type { ReactNode } from "react";
import { t } from "../i18n";
import { MemberLabel } from "./MemberField";
import { ReferenceLink } from "./ReferenceField";
import type { Dot } from "./Transitions";

export type Cells = Record<string, string>;
export const idKey = "__id";
export const dotOf = (key: string) => `__dot:${key}`;
export const typeOf = (key: string) => `__type:${key}`;
export const targetOf = (key: string) => `__target:${key}`;

type Column = { key: string; header: string; kind?: string; boolean?: boolean };

const styles = stylex.create({
  detail: { minWidth: 0 },
});

export function ListCell({ column, cells, first }: { column: Column; cells: Cells; first: boolean }) {
  return first && cells.__href ? (
    <Link href={cells.__href} color="primary" weight="medium" maxLines={1}>{column.kind === "member" && cells[column.key] ? <MemberLabel name={cells[column.key]} /> : cells[column.key] || cells[idKey]}</Link>
  ) : cells[targetOf(column.key)] ? (
    <ReferenceLink type={cells[typeOf(column.key)]} id={cells[targetOf(column.key)]} name={cells[column.key]} />
  ) : column.kind === "member" && cells[column.key] ? (
    <MemberLabel name={cells[column.key]} />
  ) : column.kind === "code" ? (
    <Text type="code" color="secondary" maxLines={1}>
      {cells[column.key] ?? ""}
    </Text>
  ) : column.boolean && cells[column.key] === t("Yes") ? (
    <Icon icon="check" size="sm" label={t("Yes")} />
  ) : column.kind === "time" && cells[column.key] ? (
    <Text type="body" color="secondary" hasTabularNumbers>
      <Timestamp value={cells[column.key]} format="relative" />
    </Text>
  ) : cells[dotOf(column.key)] ? (
    <HStack gap={2} vAlign="center">
      <StatusDot variant={cells[dotOf(column.key)] as Dot} label={cells[column.key]} />
      <Text type="body" maxLines={1}>
        {cells[column.key]}
      </Text>
    </HStack>
  ) : (
    <Text type="body" maxLines={1} weight={first ? "medium" : undefined} hasTabularNumbers={column.kind === "number"}>
      {cells[column.key] ?? ""}
    </Text>
  );
}

type ListDetailProps = { columns: Column[]; title: Column; cells: Cells; actions: ReactNode; compact: boolean; onClose: () => void };

export function ListDetail({ columns, title, cells, actions, compact, onClose }: ListDetailProps) {
  const state = columns.find((column) => cells[dotOf(column.key)]);
  return (
    <VStack gap={0} xstyle={styles.detail}>
      <Section variant="transparent" padding={4} paddingBlockStart={compact ? 0 : 4}>
        <VStack gap={4}>
          <HStack gap={2}>
            <StackItem size="fill">
              <VStack gap={2}>
                {state && (
                  <HStack gap={2} vAlign="center">
                    <StatusDot variant={cells[dotOf(state.key)] as Dot} label={cells[state.key]} />
                    <Text type="supporting" color="secondary">
                      {cells[state.key]}
                    </Text>
                  </HStack>
                )}
                <Heading level={2}>{cells[title.key] || cells[idKey]}</Heading>
              </VStack>
            </StackItem>
            <IconButton label={t("Close details")} variant="ghost" size="sm" icon={<Icon icon={XMarkIcon} size="sm" />} onClick={onClose} />
          </HStack>
          <HStack gap={2} vAlign="center" wrap="wrap">
            {actions}
            {cells.__href && <Button label={t("Open record")} variant="secondary" size="sm" href={cells.__href} />}
          </HStack>
        </VStack>
      </Section>
      <Divider />
      <Section variant="transparent" padding={4}>
        <MetadataList columns="single" label={{ position: "start", width: 120 }}>
          {columns
            .filter((column) => column.key !== title.key && column.key !== state?.key)
            .map((column) => (
              <MetadataListItem key={column.key} label={t(column.header)}>
                {cells[column.key] ? (
                  <ListCell column={column} cells={cells} first={false} />
                ) : (
                  <Text type="body" color="secondary">
                    —
                  </Text>
                )}
              </MetadataListItem>
            ))}
        </MetadataList>
      </Section>
    </VStack>
  );
}
