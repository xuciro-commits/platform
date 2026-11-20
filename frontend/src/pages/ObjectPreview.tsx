import { Banner } from "@astryxdesign/core/Banner";
import { HStack, VStack } from "@astryxdesign/core/Layout";
import { StatusDot } from "@astryxdesign/core/StatusDot";
import { Table } from "@astryxdesign/core/Table";
import { Heading, Text } from "@astryxdesign/core/Text";
import { useMemo } from "react";
import type { Api } from "../host";
import { t } from "../i18n";
import { type Described, describe, type Values } from "./describe";
import { FieldGroup, type Editor } from "./RecordFields";
import { dots } from "./Transitions";
import { present } from "./values";

function fieldOf(raw: unknown): Api.FieldInfo | undefined {
  if (!raw || typeof raw !== "object") return undefined;
  const field = raw as Record<string, unknown>;
  if (typeof field.name !== "string" || typeof field.title !== "string" || typeof field.type !== "string") return undefined;
  const listed = typeof field.choices === "string" ? field.choices.split(",").map((choice) => choice.split(":")).filter(([value]) => value.trim()) : undefined;
  const choices = listed
    ? listed.map(([value]) => value.trim())
    : Array.isArray(field.choices) ? field.choices.filter((choice): choice is string => typeof choice === "string") : undefined;
  return {
    ...field,
    name: field.name,
    title: field.title,
    type: field.type as Api.FieldInfo["type"],
    choices,
    choiceTitles: listed?.some(([, title]) => title?.trim()) ? listed.map(([value, title]) => title?.trim() || value.trim()) : undefined,
    when: field.when && typeof field.when === "object" ? field.when as Api.FieldCondition : undefined,
  } as Api.FieldInfo;
}

function example(field: Api.FieldInfo, position: number) {
  if (field.type === "integer" || field.type === "decimal") return 1200 + position;
  if (field.type === "money") return { amount: 120000, currency: "USD" };
  if (field.type === "boolean") return position === 0;
  if (field.type === "date") return "2026-11-18";
  if (field.type === "datetime") return "2026-11-18T10:00:00Z";
  if (field.type === "choice") return field.choices?.[position % Math.max(1, field.choices.length)] ?? "";
  if (field.type === "reference") return `REF-${position + 1}`;
  if (field.type === "tags" || field.type === "references") return [`REF-${position + 1}`];
  if (field.type === "attachment") return { mediaSet: "MEDIA-PREVIEW", item: "0".repeat(64) };
  if (field.type === "json") return {};
  return `${field.title} ${position + 1}`;
}

export function ObjectPreview({ definition, source, catalog }: { definition: Values; source: Described; catalog: Api.Action[] }) {
  const preview = useMemo(() => {
    const rawFields = Array.isArray(definition.fields) ? definition.fields : [];
    const fields = rawFields.flatMap((field) => fieldOf(field) ?? []);
    const states = Array.isArray(definition.states) ? definition.states as Api.State[] : [];
    const entity: Api.EntityInfo = {
      ...source.entity,
      type: `${String(definition.namespace || "build")}.${String(definition.name ?? "preview")}`,
      title: String(definition.title ?? source.entity.title),
      plural: String(definition.plural ?? source.entity.plural),
      display: String(definition.display ?? source.entity.display),
      fields,
      lifecycle: states.length ? { field: "state", initial: states[0].name, states, transitions: [] } : undefined,
    };
    return describe(entity, catalog);
  }, [catalog, definition, source.entity]);
  const samples = useMemo(() => [0, 1].map((position) => Object.fromEntries([
    ...preview.entity.fields.map((field) => [field.name, example(field, position)]),
    ...(preview.lifecycle ? [["state", preview.lifecycle.initial], ["id", `PREVIEW-${position + 1}`], ["revision", 1]] : []),
  ])), [preview]);
  const editor: Editor = {
    values: samples[0],
    status: {},
    change: () => undefined,
    settle: () => undefined,
    resolve: () => undefined,
  };
  const columns = preview.table.map((field) => ({
    key: field.name,
    header: t(field.title),
    renderCell: (row: Record<string, unknown>) => <Text type="body">{present(field, row[field.name])}</Text>,
  }));
  const state = preview.state(samples[0]);

  return (
    <VStack gap={8}>
      <Banner status="info" title={t("Preview only")} description={t("These example rows use the unpublished definition and are not saved.")} />
      <VStack gap={3}>
        <Heading level={2}>{t("List preview")}</Heading>
        <Table aria-label={t("List preview")} data={samples} columns={columns} idKey="id" density="balanced" dividers="rows" />
      </VStack>
      <VStack gap={3}>
        <HStack gap={2} vAlign="center">
          <Heading level={2}>{t("Record preview")}</Heading>
          {state && (
            <>
              <StatusDot variant={dots[(state.tone ?? "neutral") as keyof typeof dots] ?? dots.neutral} label={t(state.title)} />
              <Text type="supporting">{t(state.title)}</Text>
            </>
          )}
        </HStack>
        <FieldGroup described={preview} editor={editor} sections={preview.sections} direction="horizontal-labels" refs={{}} restricted={new Set()} />
      </VStack>
    </VStack>
  );
}
