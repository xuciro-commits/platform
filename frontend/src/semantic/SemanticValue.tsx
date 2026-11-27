import { Icon } from "@astryxdesign/core/Icon";
import { HStack } from "@astryxdesign/core/Layout";
import { Markdown } from "@astryxdesign/core/Markdown";
import { Text } from "@astryxdesign/core/Text";
import type { Api } from "../host";
import { dateText, dayText, t } from "../i18n";
import { iconField, icons } from "../pages/icons";
import type { Dot } from "../pages/Transitions";
import {
  extractUrlDisplay,
  formatMoneyValue,
  formatQuantityValue,
  formatTenantNumber,
  isCodeField,
  isEmailField,
  isPercentField,
  isPhoneField,
  isUrlField,
  unwrapPlain,
} from "./format";
import { MoneyValue, NumberValue, PercentValue, QuantityValue } from "./MoneyValue";
import { personName, PersonStack, PersonValue } from "./PersonValue";
import { ReferenceListValue, ReferenceValue } from "./ReferenceValue";
import { ChoiceValue, StatusValue, TagListValue } from "./StatusAndTags";
import { AttachmentValue, BooleanValue, CodeValue, ContactValue, parseAttachment, TemporalValue, UrlValue } from "./TemporalAndScalar";
import { iconSizeOf, type SemanticSize, type SemanticSurface, textTypeOf } from "./types";

export function shownPlain(value: unknown): string {
  const given = unwrapPlain(value);
  if (given == null) return "";
  if (typeof given === "number") return formatTenantNumber(given, { maximumFractionDigits: 6 });
  if (Array.isArray(given)) return given.map((entry) => shownPlain(entry)).filter(Boolean).join(", ");
  const money = formatMoneyValue(given);
  if (money) return money.text;
  const qty = formatQuantityValue(given);
  if (qty) return qty.text;
  const attachment = parseAttachment(given);
  if (attachment?.item) return `${attachment.item.slice(0, 10)}…`;
  if (typeof given === "object") return JSON.stringify(given);
  return String(given);
}

export function formatSemanticText(field: Api.FieldInfo, value: unknown, name?: string): string {
  const given = unwrapPlain(value);
  if (given == null || given === "") return "";
  if (name) return name;
  if (field.type === "member") {
    const str = String(given);
    return personName(str) ?? t("Unnamed member");
  }
  if (field.type === "choice" && typeof given === "string") {
    const idx = field.choices?.indexOf(given) ?? -1;
    return t(idx >= 0 ? (field.choiceTitles?.[idx] ?? given) : given);
  }
  if (field.type === "boolean" && typeof given === "boolean") {
    return given ? t("Yes") : t("No");
  }
  if (field.type === "date" && typeof given === "string") {
    return dayText(given);
  }
  if (field.type === "datetime" && typeof given === "string") {
    return dateText(given);
  }
  if (field.type === "money") {
    return formatMoneyValue(given)?.text ?? "";
  }
  if (isPercentField(field)) {
    const num = Number(given);
    return Number.isFinite(num) ? `${formatTenantNumber(num, { maximumFractionDigits: 2 })}%` : "";
  }
  if (field.type === "integer" || field.type === "decimal") {
    const num = Number(given);
    return Number.isFinite(num)
      ? formatTenantNumber(num, { maximumFractionDigits: field.type === "integer" ? 0 : 4 })
      : String(given);
  }
  if (field.type === "tags" && Array.isArray(given)) {
    return given.map((entry) => t(String(entry))).join(", ");
  }
  if (isUrlField(field, given) && typeof given === "string") {
    return extractUrlDisplay(given);
  }
  const qty = formatQuantityValue(given);
  if (qty) return qty.text;
  const attachment = parseAttachment(given);
  if (attachment?.item) return `${attachment.item.slice(0, 10)}…`;
  return shownPlain(given);
}

export function SemanticValue({
  field,
  value,
  name,
  refs,
  dot,
  tone,
  size = "md",
  surface = "detail",
  compactMoney = false,
  interactive = true,
  recordType,
  recordId,
  recordRevision,
}: {
  field: Api.FieldInfo;
  value: unknown;
  name?: string;
  refs?: Record<string, string>;
  dot?: Dot;
  tone?: string;
  size?: SemanticSize;
  surface?: SemanticSurface;
  compactMoney?: boolean;
  interactive?: boolean;
  recordType?: string;
  recordId?: string;
  recordRevision?: number;
}) {
  const given = unwrapPlain(value);
  if (given == null || given === "") return null;

  if (field.type === "member") {
    if (Array.isArray(given)) {
      return <PersonStack items={given.map((id) => ({ id: String(id), name: refs?.[String(id)] }))} size={size === "md" ? "sm" : "xs"} />;
    }
    const memberId = String(given);
    return <PersonValue id={memberId} name={name ?? refs?.[memberId]} size={size} card={interactive} />;
  }

  if (iconField(field.name, field.type) && icons[String(given)]) {
    return (
      <HStack gap={2} vAlign="center">
        <Icon icon={icons[String(given)]} size={iconSizeOf(size)} />
        <Text type={textTypeOf(size)} maxLines={1}>{String(given)}</Text>
      </HStack>
    );
  }

  if (dot) {
    const label = name ?? formatSemanticText(field, given);
    return <StatusValue label={label} dot={dot} tone={tone} size={size} appearance={surface === "header" ? "pill" : "dot"} />;
  }

  if (field.type === "reference" && field.ref) {
    const targetId = String(given);
    return <ReferenceValue type={field.ref} id={targetId} name={name ?? refs?.[targetId]} size={size} />;
  }

  if (field.type === "references" && field.ref && Array.isArray(given)) {
    return <ReferenceListValue type={field.ref} ids={given.map(String)} names={refs} size={size} max={surface === "cell" ? 2 : 4} />;
  }

  if (field.type === "money") {
    return <MoneyValue value={given} size={size} compact={compactMoney} />;
  }

  if (isPercentField(field)) {
    return <PercentValue value={given} size={size} />;
  }

  if (field.type === "integer" || field.type === "decimal") {
    return <NumberValue value={given} integer={field.type === "integer"} size={size} compact={compactMoney} />;
  }

  const quantity = formatQuantityValue(given);
  if (quantity) {
    return <QuantityValue value={given} size={size} />;
  }

  if (field.type === "choice") {
    return <ChoiceValue field={field} value={given} tone={tone} size={size} />;
  }

  if (field.type === "tags" && Array.isArray(given)) {
    return <TagListValue items={given.map(String)} size={size} max={surface === "cell" ? 2 : 6} />;
  }

  if (field.type === "boolean") {
    return <BooleanValue value={given} size={size} surface={surface} />;
  }

  if (field.type === "date" || field.type === "datetime") {
    return <TemporalValue value={given} kind={field.type} size={size} surface={surface} />;
  }

  if (field.type === "attachment" || parseAttachment(given)) {
    return <AttachmentValue value={given} size={size} />;
  }

  if (isEmailField(field)) {
    return (
      <ContactValue
        kind="email"
        value={given}
        size={size}
        surface={surface}
        interactive={interactive}
      />
    );
  }

  if (isPhoneField(field)) {
    return (
      <ContactValue
        kind="phone"
        value={given}
        size={size}
        surface={surface}
        interactive={interactive}
        recordType={recordType}
        recordId={recordId}
        recordRevision={recordRevision}
      />
    );
  }

  if (isUrlField(field, given)) {
    return <UrlValue value={given} size={size} surface={surface} interactive={interactive} />;
  }

  if (isCodeField(field)) {
    return <CodeValue value={given} size={size} copyable={interactive && surface === "detail" && Boolean(field.readOnly)} />;
  }

  const text = formatSemanticText(field, given, name);
  if (!text) return null;
  if (field.type === "longtext" && surface === "detail") {
    return <Markdown density="compact" headingLevelStart={4}>{text}</Markdown>;
  }
  return <Text type={textTypeOf(size)} maxLines={surface === "detail" ? undefined : 1}>{text}</Text>;
}
