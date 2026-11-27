import { HoverCard } from "@astryxdesign/core/HoverCard";
import { Icon } from "@astryxdesign/core/Icon";
import { HStack, VStack } from "@astryxdesign/core/Layout";
import { Link } from "@astryxdesign/core/Link";
import { MetadataList, MetadataListItem } from "@astryxdesign/core/MetadataList";
import { Heading, Text } from "@astryxdesign/core/Text";
import { Token } from "@astryxdesign/core/Token";
import { BuildingOffice2Icon, CubeIcon } from "@heroicons/react/24/outline";
import { useState } from "react";
import { useRecordHref } from "../recordRoutes";
import { type Api, useRead } from "../host";
import { t } from "../i18n";
import { elementHref, elementType } from "../pages/enterprise";
import { iconOf } from "../pages/icons";
import { usePeek } from "../peekScope";
import { formatMoneyValue, formatQuantityValue, formatTenantNumber, isPercentField, unwrapPlain } from "./format";
import { PersonValue } from "./PersonValue";
import { StatusValue } from "./StatusAndTags";
import { BooleanValue, TemporalValue } from "./TemporalAndScalar";
import { iconSizeOf, type SemanticSize, textTypeOf, tokenSizeOf } from "./types";

function PreviewFieldValue({ field, value, refs }: { field: Api.FieldInfo; value: unknown; refs?: Record<string, string> }) {
  const given = unwrapPlain(value);
  if (given == null || given === "") return null;
  if (field.type === "member") {
    return <PersonValue id={String(given)} name={refs?.[String(given)]} size="xs" card={false} />;
  }
  if (field.type === "money") {
    const formatted = formatMoneyValue(given);
    return formatted ? <Text type="supporting" hasTabularNumbers>{formatted.text}</Text> : null;
  }
  if (isPercentField(field)) {
    const num = Number(given);
    return Number.isFinite(num) ? <Text type="supporting" hasTabularNumbers>{`${formatTenantNumber(num)}%`}</Text> : null;
  }
  if (field.type === "integer" || field.type === "decimal") {
    const num = Number(given);
    return Number.isFinite(num)
      ? <Text type="supporting" hasTabularNumbers>{formatTenantNumber(num, { maximumFractionDigits: field.type === "integer" ? 0 : 4 })}</Text>
      : null;
  }
  if (field.type === "boolean") {
    return <BooleanValue value={given} size="xs" surface="detail" />;
  }
  if (field.type === "date" || field.type === "datetime") {
    return <TemporalValue value={given} kind={field.type} size="xs" surface="detail" />;
  }
  if (field.type === "choice" && typeof given === "string") {
    const idx = field.choices?.indexOf(given) ?? -1;
    const title = idx >= 0 ? (field.choiceTitles?.[idx] ?? given) : given;
    return <Text type="supporting">{t(title)}</Text>;
  }
  if (field.type === "reference") {
    const label = refs?.[String(given)] ?? String(given);
    return <Text type="supporting" maxLines={1}>{label}</Text>;
  }
  const qty = formatQuantityValue(given);
  if (qty) return <Text type="supporting" hasTabularNumbers>{qty.text}</Text>;
  return <Text type="supporting" maxLines={1}>{Array.isArray(given) ? given.join(", ") : String(given)}</Text>;
}

function ElementReferenceValue({
  id,
  name,
  size = "md",
  compact = false,
  showIcon = true,
}: {
  id: string;
  name: string;
  size?: SemanticSize;
  compact?: boolean;
  showIcon?: boolean;
}) {
  const [open, setOpen] = useState(false);
  const resolved = useRead<Api.EnterpriseQueryResult>(open ? `/v1/enterprise-resolve?ids=${encodeURIComponent(id)}` : null);
  const element = resolved.status === "ready" ? resolved.data.elements.find((item) => item.id === id) : undefined;
  const link = (
    <Link href={elementHref(id)} label={compact ? name : undefined} type={textTypeOf(size)} maxLines={compact ? undefined : 1}>
      {compact ? <Icon icon="externalLink" size="sm" /> : name}
    </Link>
  );
  const trigger = compact || !showIcon ? link : (
    <HStack gap={1.5} vAlign="center">
      <Icon icon={BuildingOffice2Icon} size={iconSizeOf(size)} color="secondary" />
      {link}
    </HStack>
  );
  return (
    <HoverCard
      onOpenChange={setOpen}
      placement="below"
      content={
        <VStack gap={2}>
          <HStack gap={1.5} vAlign="center">
            <Icon icon={BuildingOffice2Icon} size="sm" color="secondary" />
            <Heading level={4}>{element?.name || name}</Heading>
          </HStack>
          {element && (
            <MetadataList>
              {element.kind && <MetadataListItem label={t("Type")}>{t(element.kind)}</MetadataListItem>}
              {element.shortName && <MetadataListItem label={t("Code")}>{element.shortName}</MetadataListItem>}
              {element.closed && <MetadataListItem label={t("State")}>{t("Closed")}</MetadataListItem>}
            </MetadataList>
          )}
        </VStack>
      }
    >
      {trigger}
    </HoverCard>
  );
}

export function ReferenceValue({
  type,
  id,
  name,
  size = "md",
  compact = false,
  showIcon = true,
  color,
}: {
  type: string;
  id: string;
  name?: string;
  size?: SemanticSize;
  compact?: boolean;
  showIcon?: boolean;
  color?: "primary" | "secondary" | "accent";
}) {
  const [open, setOpen] = useState(false);
  const href = useRecordHref();
  const peek = usePeek();
  const types = useRead<Api.EntityInfo[]>("/v1/entities");
  const target = types.status === "ready" ? types.data.find((item) => item.type === type) : undefined;
  const needsName = Boolean(id && !name);
  const read = useRead<Api.RecordView>(open || needsName ? `/v1/records/${encodeURIComponent(type)}/${encodeURIComponent(id)}` : null);

  if (!id) return null;
  if (type === elementType) {
    return <ElementReferenceValue id={id} name={name || id} size={size} compact={compact} showIcon={showIcon} />;
  }

  const values = read.status === "ready" ? read.data.values : undefined;
  const resolvedDisplay =
    name ||
    (target?.display && values?.[target.display] != null ? String(unwrapPlain(values[target.display])) : undefined) ||
    id;
  const EntityIcon = iconOf(target?.icon, CubeIcon);
  const lifecycle = target?.lifecycle;
  const state = lifecycle?.states?.find((candidate) => candidate.name === values?.[lifecycle.field]);
  const previewFields = target?.fields
    .filter((field) => field.name !== target.display && field.name !== lifecycle?.field && values?.[field.name] != null && values?.[field.name] !== "")
    .slice(0, 4);

  const linkNode = (
    <Link
      href={href(type, id)}
      label={compact ? resolvedDisplay : undefined}
      type={textTypeOf(size)}
      color={color}
      maxLines={compact ? undefined : 1}
      onClick={(event) => {
        if (!peek || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey || event.button !== 0) return;
        event.preventDefault();
        event.stopPropagation();
        setOpen(false);
        peek.open({ type, id, name: resolvedDisplay });
      }}
    >
      {compact ? <Icon icon="externalLink" size="sm" /> : resolvedDisplay}
    </Link>
  );

  const trigger = compact || !showIcon ? linkNode : (
    <HStack gap={1.5} vAlign="center">
      <Icon icon={EntityIcon} size={iconSizeOf(size)} color="secondary" />
      {linkNode}
    </HStack>
  );

  return (
    <HoverCard
      onOpenChange={setOpen}
      placement="below"
      content={
        <VStack gap={2}>
          <HStack gap={1.5} vAlign="center">
            <Icon icon={EntityIcon} size="sm" color="secondary" />
            <Text type="supporting" color="secondary">{target ? t(target.title) : type}</Text>
          </HStack>
          <HStack gap={2} vAlign="center" wrap="wrap">
            <Heading level={4}>{resolvedDisplay}</Heading>
            {state && <StatusValue label={t(state.title)} tone={state.tone} size="xs" appearance="pill" />}
          </HStack>
          {previewFields && previewFields.length > 0 && (
            <MetadataList>
              {previewFields.map((field) => (
                <MetadataListItem key={field.name} label={t(field.title)}>
                  <PreviewFieldValue field={field} value={values?.[field.name]} refs={read.status === "ready" ? read.data.refs?.[field.name] : undefined} />
                </MetadataListItem>
              ))}
            </MetadataList>
          )}
        </VStack>
      }
    >
      {trigger}
    </HoverCard>
  );
}

export function ReferenceListValue({
  type,
  ids,
  names,
  max = 2,
  size = "sm",
}: {
  type: string;
  ids: string[];
  names?: Record<string, string>;
  max?: number;
  size?: SemanticSize;
}) {
  const clean = ids.map((id) => id.trim()).filter(Boolean);
  if (clean.length === 0) return null;
  const limit = clean.length <= max + 1 ? clean.length : max;
  const shown = clean.slice(0, limit);
  const hidden = clean.slice(limit);
  return (
    <HStack gap={2} vAlign="center" wrap={size === "md" ? "wrap" : "nowrap"}>
      {shown.map((id) => (
        <ReferenceValue key={id} type={type} id={id} name={names?.[id]} size={size} />
      ))}
      {hidden.length > 0 && (
        <HoverCard
          placement="below"
          content={
            <VStack gap={1.5}>
              {hidden.map((id) => (
                <ReferenceValue key={id} type={type} id={id} name={names?.[id]} size="xs" />
              ))}
            </VStack>
          }
        >
          <Token size={tokenSizeOf(size)} color="gray" label={`+${hidden.length}`} />
        </HoverCard>
      )}
    </HStack>
  );
}
