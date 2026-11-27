import { HStack } from "@astryxdesign/core/Layout";
import { Text } from "@astryxdesign/core/Text";
import { Tooltip } from "@astryxdesign/core/Tooltip";
import { t, useFormatConfig } from "../i18n";
import { formatMoneyValue, formatQuantityValue, formatTenantNumber } from "./format";
import { type SemanticSize, textTypeOf } from "./types";

export function MoneyValue({
  value,
  currency,
  size = "md",
  compact = false,
  color = "primary",
  weight,
  align = "start",
}: {
  value: unknown;
  currency?: string;
  size?: SemanticSize;
  compact?: boolean;
  color?: "primary" | "secondary";
  weight?: "normal" | "medium" | "semibold";
  align?: "start" | "end";
}) {
  useFormatConfig();
  const formatted = formatMoneyValue(value, { compact, fallbackCurrency: currency });
  if (!formatted) return null;
  const textNode = (
    <Text
      type={textTypeOf(size)}
      size={size === "lg" ? "2xl" : undefined}
      color={color}
      weight={weight ?? (size === "lg" ? "semibold" : undefined)}
      hasTabularNumbers
      maxLines={1}
    >
      {formatted.text}
    </Text>
  );
  const wrapped = formatted.abbreviated ? (
    <Tooltip content={formatted.tooltip} hasHoverIndication={false}>
      {textNode}
    </Tooltip>
  ) : textNode;
  return align === "end" ? <HStack hAlign="end" vAlign="center">{wrapped}</HStack> : wrapped;
}

export function NumberValue({
  value,
  integer = false,
  size = "md",
  compact = false,
  color = "primary",
  weight,
  align = "start",
}: {
  value: unknown;
  integer?: boolean;
  size?: SemanticSize;
  compact?: boolean;
  color?: "primary" | "secondary";
  weight?: "normal" | "medium" | "semibold";
  align?: "start" | "end";
}) {
  const { locale } = useFormatConfig();
  if (value == null || value === "") return null;
  const num = typeof value === "number" ? value : Number(value);
  if (!Number.isFinite(num)) return null;
  const exact = formatTenantNumber(num, {
    minimumFractionDigits: 0,
    maximumFractionDigits: integer ? 0 : 4,
  });
  let shown = exact;
  let abbreviated = false;
  if (compact) {
    const abs = Math.abs(num);
    if (locale === "zh-CN" && abs >= 10_000) {
      const unit = abs >= 100_000_000 ? 100_000_000 : 10_000;
      const suffix = abs >= 100_000_000 ? t("unit.yi") : t("unit.wan");
      shown = `${formatTenantNumber(num / unit, { maximumFractionDigits: 2 })}${suffix}`;
      abbreviated = true;
    } else if (locale !== "zh-CN" && abs >= 1_000) {
      const [div, suffix] = abs >= 1_000_000_000 ? [1_000_000_000, "B"] : abs >= 1_000_000 ? [1_000_000, "M"] : [1_000, "K"];
      shown = `${formatTenantNumber(num / div, { maximumFractionDigits: 2 })}${suffix}`;
      abbreviated = true;
    }
  }
  const node = (
    <Text
      type={textTypeOf(size)}
      size={size === "lg" ? "2xl" : undefined}
      color={color}
      weight={weight ?? (size === "lg" ? "semibold" : undefined)}
      hasTabularNumbers
      maxLines={1}
    >
      {shown}
    </Text>
  );
  const content = abbreviated ? <Tooltip content={exact} hasHoverIndication={false}>{node}</Tooltip> : node;
  return align === "end" ? <HStack hAlign="end" vAlign="center">{content}</HStack> : content;
}

export function PercentValue({
  value,
  size = "md",
  color = "primary",
}: {
  value: unknown;
  size?: SemanticSize;
  color?: "primary" | "secondary";
}) {
  useFormatConfig();
  if (value == null || value === "") return null;
  const num = typeof value === "number" ? value : Number(value);
  if (!Number.isFinite(num)) return null;
  const formatted = `${formatTenantNumber(num, { minimumFractionDigits: 0, maximumFractionDigits: 2 })}%`;
  return (
    <Text type={textTypeOf(size)} color={color} hasTabularNumbers maxLines={1}>
      {formatted}
    </Text>
  );
}

export function QuantityValue({
  value,
  size = "md",
  color = "primary",
}: {
  value: unknown;
  size?: SemanticSize;
  color?: "primary" | "secondary";
}) {
  useFormatConfig();
  const formatted = formatQuantityValue(value);
  if (!formatted) return null;
  const node = (
    <Text type={textTypeOf(size)} color={color} hasTabularNumbers maxLines={1}>
      {formatted.text}
    </Text>
  );
  return formatted.tooltip ? <Tooltip content={formatted.tooltip} hasHoverIndication={false}>{node}</Tooltip> : node;
}
