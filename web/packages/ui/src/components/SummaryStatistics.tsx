import { MetadataList, MetadataListItem } from "@astryxdesign/core/MetadataList";
import { VStack } from "@astryxdesign/core/Stack";
import { Text } from "@astryxdesign/core/Text";
import { t } from "../i18n";
export type StatisticsValue = { kind: "statistics"; count: string; min?: number; mean?: number; max?: number; sum?: number };
export function SummaryStatistics({ value, fieldTitle }: { value: StatisticsValue; fieldTitle: string }) {
  const entries = ([["Count", value.count], ["Min", value.min], ["Mean", value.mean], ["Max", value.max], ["Sum", value.sum]] as const).map(([label, number]) => <MetadataListItem key={label} label={t(label)}><Text type="body" weight="semibold" hasTabularNumbers>{number === undefined ? t("No value") : typeof number === "string" ? number : number.toLocaleString(undefined, { maximumFractionDigits: 1 })}</Text></MetadataListItem>);
  return <VStack gap={2}>
    <Text type="supporting">{fieldTitle} · {t("Complete authorized set")}</Text>
    <MetadataList columns={5} label={{ position: "top" }}>{entries}</MetadataList>
  </VStack>;
}
