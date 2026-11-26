import { Button } from "@astryxdesign/core/Button";
import { HStack, StackItem, VStack } from "@astryxdesign/core/Layout";
import { MultiSelector } from "@astryxdesign/core/MultiSelector";
import { Selector } from "@astryxdesign/core/Selector";
import { TextInput } from "@astryxdesign/core/TextInput";
import { useState } from "react";
import { t } from "../i18n";

export type ConditionSource = { name: string; title: string; values: { value: string; label: string }[] };

export const choicesOf = (value: unknown) => (Array.isArray(value) ? value.map(String) : typeof value === "string" ? value.split(",") : [])
  .flatMap((item) => {
    const [name, ...title] = item.split(":");
    return name.trim() ? [{ value: name.trim(), label: t(title.join(":").trim() || name.trim()) }] : [];
  });

export function conditionSources(fields: { name?: unknown; title?: unknown; type?: unknown; choices?: unknown; when?: unknown }[],
  states: { name?: unknown; title?: unknown }[], self?: unknown): ConditionSource[] {
  const fromFields = fields.flatMap((field) => {
    if (!field.name || field.name === self || field.when) return [];
    const name = String(field.name);
    const title = t(String(field.title ?? name));
    if (field.type === "choice") return [{ name, title, values: choicesOf(field.choices) }];
    if (field.type === "boolean") return [{ name, title, values: [{ value: "true", label: t("Yes") }, { value: "false", label: t("No") }] }];
    return [];
  });
  const named = states.filter((state) => state.name);
  return named.length ? [...fromFields, { name: "state", title: t("State"), values: named.map((state) => ({ value: String(state.name), label: t(String(state.title ?? state.name)) })) }] : fromFields;
}

const parse = (text: string) => {
  const at = text.indexOf("=");
  return at < 0 ? { field: "", values: [] as string[] } : { field: text.slice(0, at), values: text.slice(at + 1).split(",").filter(Boolean) };
};

const format = (field: string, values: string[]) => field && values.length ? `${field}=${values.join(",")}` : "";

const fits = (text: string, sources: ConditionSource[]) => {
  const { field, values } = parse(text);
  if (!text) return true;
  const source = sources.find((candidate) => candidate.name === field);
  return Boolean(source) && values.every((value) => source!.values.some((option) => option.value === value));
};

export function ConditionBuilder({ label, value, sources, onChange }: {
  label: string; value: string; sources: ConditionSource[]; onChange: (value: string) => void;
}) {
  const [asText, setAsText] = useState(() => !fits(value, sources));
  const [picked, setPicked] = useState(() => parse(value));
  const source = sources.find((candidate) => candidate.name === picked.field);
  const pick = (field: string, values: string[]) => {
    setPicked({ field, values });
    onChange(format(field, values));
  };
  const toggle = () => {
    if (asText) setPicked(parse(value));
    setAsText(!asText);
  };
  return (
    <VStack gap={2}>
      {asText ? (
        <TextInput label={label} value={value} onChange={(next) => onChange(next.trim())}
          description={t("Another field and the values that make this field apply, as field=value,value")} />
      ) : (
        <HStack gap={2} vAlign="end">
          <StackItem size="fill">
            <Selector label={label} placeholder={t("Always")} hasClear
              options={sources.map((candidate) => ({ value: candidate.name, label: candidate.title }))}
              value={source ? picked.field : null}
              onChange={(next) => pick(next ?? "", [])} />
          </StackItem>
          {source && (
            <StackItem size="fill">
              <MultiSelector label={t("Values")} options={source.values} value={picked.values}
                onChange={(next) => pick(picked.field, next)} isRequired />
            </StackItem>
          )}
        </HStack>
      )}
      <HStack hAlign="start">
        <Button label={asText ? t("Edit with fields") : t("Edit as text")} variant="secondary" size="sm" onClick={toggle} />
      </HStack>
    </VStack>
  );
}
