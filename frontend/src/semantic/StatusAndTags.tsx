import { HoverCard } from "@astryxdesign/core/HoverCard";
import { HStack, VStack } from "@astryxdesign/core/Layout";
import { Text } from "@astryxdesign/core/Text";
import { Token } from "@astryxdesign/core/Token";
import { Icon } from "@astryxdesign/core/Icon";
import type { ComponentProps } from "react";
import type { Api } from "../host";
import { t } from "../i18n";
import { DotMark } from "../pages/DotMark";
import { icons } from "../pages/icons";
import type { Dot } from "../pages/Transitions";
import { iconSizeOf, type SemanticSize, textTypeOf, tokenSizeOf } from "./types";

export type TokenColor = NonNullable<ComponentProps<typeof Token>["color"]>;

export function toneToTokenColor(tone?: string): TokenColor {
  if (!tone) return "default";
  if (tone === "success" || tone === "green") return "green";
  if (tone === "warning" || tone === "orange" || tone === "yellow") return "orange";
  if (tone === "danger" || tone === "error" || tone === "red") return "red";
  if (tone === "info" || tone === "accent" || tone === "blue") return "blue";
  if (tone === "purple") return "purple";
  if (tone === "pink") return "pink";
  if (tone === "teal") return "teal";
  if (tone === "cyan") return "cyan";
  if (tone === "neutral" || tone === "gray") return "gray";
  return "default";
}

export function toneToDot(tone?: string): Dot {
  if (tone === "success") return "success";
  if (tone === "warning") return "warning";
  if (tone === "danger" || tone === "error") return "error";
  if (tone === "info" || tone === "accent") return "accent";
  if (tone === "blue" || tone === "purple" || tone === "cyan" || tone === "pink" || tone === "green" || tone === "orange" || tone === "red") {
    return tone;
  }
  return "neutral";
}

export function StatusValue({
  label,
  dot,
  tone,
  size = "sm",
  appearance = "dot",
  color,
}: {
  label: string;
  dot?: Dot;
  tone?: string;
  size?: SemanticSize;
  appearance?: "dot" | "pill";
  color?: "primary" | "secondary";
}) {
  if (!label) return null;
  const effectiveDot = dot ?? (tone ? toneToDot(tone) : "neutral");
  if (appearance === "pill" || size === "lg") {
    const pillColor = toneToTokenColor(tone ?? dot);
    return (
      <Token
        size={tokenSizeOf(size)}
        color={pillColor}
        icon={<DotMark dot={effectiveDot} label={label} />}
        label={label}
      />
    );
  }
  return (
    <HStack gap={size === "xs" ? 1.5 : 2} vAlign="center">
      <DotMark dot={effectiveDot} label={label} />
      <Text type={textTypeOf(size)} color={color} maxLines={1}>
        {label}
      </Text>
    </HStack>
  );
}

export function ChoiceValue({
  field,
  value,
  dot,
  tone,
  size = "md",
}: {
  field?: Pick<Api.FieldInfo, "choices" | "choiceTitles">;
  value: unknown;
  dot?: Dot;
  tone?: string;
  size?: SemanticSize;
}) {
  if (value == null || value === "") return null;
  const raw = String(value);
  const index = field?.choices?.indexOf(raw) ?? -1;
  const rawTitle = index >= 0 ? (field?.choiceTitles?.[index] ?? raw) : raw;
  const label = t(rawTitle);
  if (dot) {
    return <StatusValue label={label} dot={dot} tone={tone} size={size} appearance="dot" />;
  }
  if (tone) {
    return <Token size={tokenSizeOf(size)} color={toneToTokenColor(tone)} label={label} />;
  }
  return (
    <Text type={textTypeOf(size)} maxLines={1}>
      {label}
    </Text>
  );
}

export type TagItem = string | { label: string; color?: TokenColor; icon?: string };

export function TagListValue({
  items,
  max = 3,
  size = "sm",
  defaultColor = "default",
}: {
  items: TagItem[];
  max?: number;
  size?: SemanticSize;
  defaultColor?: TokenColor;
}) {
  const normalized = items
    .map((item) => (typeof item === "string" ? { label: item.trim(), color: defaultColor, icon: undefined } : { ...item, label: item.label.trim() }))
    .filter((item) => item.label.length > 0);
  if (normalized.length === 0) return null;

  const limit = normalized.length <= max + 1 ? normalized.length : max;
  const shown = normalized.slice(0, limit);
  const hidden = normalized.slice(limit);
  const tokenSize = tokenSizeOf(size);

  return (
    <HStack gap={1} vAlign="center" wrap={size === "md" || size === "lg" ? "wrap" : "nowrap"}>
      {shown.map((item, index) => {
        const resolvedIcon = item.icon ? icons[item.icon] : undefined;
        return (
          <Token
            key={`${item.label}-${index}`}
            size={tokenSize}
            color={item.color ?? defaultColor}
            icon={resolvedIcon ? <Icon icon={resolvedIcon} size={iconSizeOf(size)} /> : undefined}
            label={t(item.label)}
          />
        );
      })}
      {hidden.length > 0 && (
        <HoverCard
          placement="below"
          content={
            <HStack gap={1} wrap="wrap">
              {hidden.map((item, index) => (
                <Token key={`${item.label}-${index}`} size="sm" color={item.color ?? defaultColor} label={t(item.label)} />
              ))}
            </HStack>
          }
        >
          <Token size={tokenSize} color="gray" label={`+${hidden.length}`} />
        </HoverCard>
      )}
    </HStack>
  );
}
