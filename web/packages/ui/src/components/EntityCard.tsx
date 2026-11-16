import type { ReactNode } from "react";
import { Card } from "@astryxdesign/core/Card";
import { Divider } from "@astryxdesign/core/Divider";
import { MetadataList, MetadataListItem } from "@astryxdesign/core/MetadataList";
import { HStack, VStack } from "@astryxdesign/core/Stack";
import { Text } from "@astryxdesign/core/Text";

export function PropertyList({ items, columns = 1, itemKeys }: { itemKeys?: string[]; items: [label: string, value: ReactNode][]; columns?: 1 | 2 | 3 | 4 }) {
  return <MetadataList columns={columns} label={{ position: columns === 1 ? "start" : "top" }}>
    {items.map(([label, value], index) => <MetadataListItem key={itemKeys?.[index] ?? label} label={label}>{value}</MetadataListItem>)}
  </MetadataList>;
}

export function EntityCard({ title, subtitle, status, properties, actions, tone, propertyKeys }: {
  tone?: "neutral" | "info" | "success" | "warning" | "danger"; propertyKeys?: string[];
  title: ReactNode; subtitle?: ReactNode; status?: ReactNode; properties: [string, ReactNode][]; actions?: ReactNode;
}) {
  return <Card padding={3} style={tone ? { borderLeftWidth: 3, borderLeftColor: `var(--tone-${tone})` } : undefined}>
    <VStack gap={3}>
      <HStack gap={2} hAlign="between" vAlign="start">
        <VStack gap={0.5}>
          <Text type="large" weight="semibold" maxLines={1}>{title}</Text>
          {subtitle && <Text type="code" color="secondary" maxLines={1}>{subtitle}</Text>}
        </VStack>
        {status}
      </HStack>
      <PropertyList items={properties} itemKeys={propertyKeys} />
      {actions && <><Divider /><HStack gap={2} wrap="wrap">{actions}</HStack></>}
    </VStack>
  </Card>;
}
