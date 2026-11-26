import { VStack } from "@astryxdesign/core/Layout";
import { Table, TableBody, TableCell, TableHeader, TableHeaderCell, TableRow } from "@astryxdesign/core/Table";
import { Text } from "@astryxdesign/core/Text";
import { Token } from "@astryxdesign/core/Token";
import type { Api } from "../host";
import { t } from "../i18n";
import type { Described, Values } from "./describe";
import { plain, present } from "./values";

export type ChangeKind = "added" | "removed" | "changed";

export type ComparisonRow = {
  key: string;
  label: string;
  code?: string;
  kind: ChangeKind;
  before?: string;
  after?: string;
  note?: string;
  incompatible?: boolean;
};

type DraftFieldSpec = {
  name?: string;
  title?: string;
  type?: string;
  ref?: string;
  choices?: string;
  required?: boolean;
};

const ignoredKeys = new Set(["id", "revision", "created", "changed", "published", "schemaVersions", "extensionSource"]);

function isBlank(value: unknown) {
  if (value == null || value === "") return true;
  if (Array.isArray(value)) return value.length === 0;
  return false;
}

function stableJson(value: unknown): string {
  if (isBlank(value)) return "";
  if (typeof value !== "object") return JSON.stringify(value);
  if (Array.isArray(value)) return `[${value.map(stableJson).join(",")}]`;
  const entries = Object.entries(value as Values)
    .filter(([, entry]) => entry !== undefined)
    .sort(([left], [right]) => left.localeCompare(right));
  return `{${entries.map(([key, entry]) => `${JSON.stringify(key)}:${stableJson(entry)}`).join(",")}}`;
}

function describeFieldSpec(raw: unknown): { title?: string; summary: string } | undefined {
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) return undefined;
  const spec = raw as DraftFieldSpec;
  if (!spec.name && !spec.title && !spec.type) return undefined;
  const parts: string[] = [];
  if (spec.title) parts.push(t(spec.title));
  if (spec.type) {
    const typeText = spec.type === "reference" && spec.ref ? `${t(spec.type)} → ${spec.ref}` : t(spec.type);
    parts.push(typeText);
  }
  if (spec.choices) parts.push(`(${spec.choices})`);
  if (spec.required) parts.push(t("Required"));
  return { title: spec.title, summary: parts.join(" · ") || String(spec.name ?? "") };
}

export function formatComparisonValue(
  value: unknown,
  path?: string,
  described?: Described,
  refs?: Api.RecordView["refs"],
): string | undefined {
  if (isBlank(value)) return undefined;
  if (path?.startsWith("fields.") && path !== "fields.$order") {
    const fieldSummary = describeFieldSpec(value);
    if (fieldSummary) return fieldSummary.summary;
  }
  const field = path ? described?.field(path) : undefined;
  if (field) {
    const id = String(value);
    if (field.type === "reference" && refs?.[field.name]?.[id]) return refs[field.name][id];
    const formatted = present(field, plain(value));
    if (formatted !== "") return formatted;
  }
  if (typeof value === "boolean") return value ? t("Yes") : t("No");
  if (typeof value === "string" || typeof value === "number") return String(value);
  if (Array.isArray(value)) {
    const items = value
      .map((item) => {
        if (typeof item === "object" && item) {
          const record = item as Values;
          return String(record.title ?? record.name ?? record.role ?? "");
        }
        return String(item);
      })
      .filter(Boolean);
    return items.length > 0 ? items.join(", ") : undefined;
  }
  return t("Structured value");
}

export function compareRecords(
  before: Values | undefined,
  after: Values | undefined,
  described?: Described,
  refs?: Api.RecordView["refs"],
): ComparisonRow[] {
  const keys = new Set<string>([
    ...Object.keys(before ?? {}),
    ...Object.keys(after ?? {}),
  ]);
  const sorted = [...keys].filter((key) => !ignoredKeys.has(key)).sort((left, right) => left.localeCompare(right));
  const rows: ComparisonRow[] = [];
  for (const key of sorted) {
    const leftVal = before?.[key];
    const rightVal = after?.[key];
    const leftEmpty = isBlank(leftVal);
    const rightEmpty = isBlank(rightVal);
    if (leftEmpty && rightEmpty) continue;
    if (stableJson(leftVal) === stableJson(rightVal)) continue;
    const field = described?.field(key);
    const label = field ? t(field.title) : key;
    const kind: ChangeKind = leftEmpty ? "added" : rightEmpty ? "removed" : "changed";
    rows.push({
      key,
      label,
      code: label !== key ? key : undefined,
      kind,
      before: formatComparisonValue(leftVal, key, described, refs),
      after: formatComparisonValue(rightVal, key, described, refs),
    });
  }
  return rows;
}

export function compareObjectChanges(
  changes: Api.ObjectChange[],
  options?: {
    described?: Described;
    refs?: Api.RecordView["refs"];
    impact?: Api.ObjectPublishImpact;
    fullPathCode?: boolean;
  },
): ComparisonRow[] {
  const impactByField = new Map((options?.impact?.fields ?? []).map((item) => [item.field, item]));
  return changes.map((change) => {
    const isFieldEntry = change.path.startsWith("fields.") && change.path !== "fields.$order";
    const fieldKey = isFieldEntry ? change.path.slice("fields.".length) : change.path;
    const fromSpec = isFieldEntry ? describeFieldSpec(change.from) : undefined;
    const toSpec = isFieldEntry ? describeFieldSpec(change.to) : undefined;
    const knownField = options?.described?.field(change.path);
    const rawTitle = toSpec?.title ?? fromSpec?.title ?? knownField?.title;
    const label = rawTitle ? t(rawTitle) : fieldKey;
    const code = options?.fullPathCode ? change.path : fieldKey;
    const fieldImpact = isFieldEntry ? impactByField.get(fieldKey) : undefined;
    const note = fieldImpact
      ? `${fieldImpact.change === "removed" ? t("Removed field") : t("Changed type")} · ${fieldImpact.records.toLocaleString()} ${t("records with data")}`
      : undefined;
    return {
      key: change.path,
      label,
      code: code !== label ? code : options?.fullPathCode ? code : undefined,
      kind: change.change,
      before: formatComparisonValue(change.from, change.path, options?.described, options?.refs),
      after: formatComparisonValue(change.to, change.path, options?.described, options?.refs),
      note,
    };
  });
}

export function compareDraftObject(
  record: Values | undefined,
  changes: Api.ObjectChange[] | undefined,
  options?: {
    described?: Described;
    refs?: Api.RecordView["refs"];
    impact?: Api.ObjectPublishImpact;
  },
): ComparisonRow[] {
  if (changes && changes.length > 0) {
    return compareObjectChanges(changes, options);
  }
  if (!record || record.state === "published") return [];
  const fields = Array.isArray(record.fields) ? (record.fields as DraftFieldSpec[]) : [];
  return fields
    .filter((field) => Boolean(field.name))
    .map((field) => {
      const name = String(field.name);
      const spec = describeFieldSpec(field);
      const label = spec?.title ? t(spec.title) : name;
      return {
        key: `fields.${name}`,
        label,
        code: label !== name ? name : undefined,
        kind: "added" as const,
        after: spec?.summary ?? name,
      };
    });
}

export function comparePackageFields(fields: Api.PackageFieldChange[]): ComparisonRow[] {
  return fields.map((field) => {
    const normalized = field.change.toLowerCase();
    const kind: ChangeKind = normalized === "added" ? "added" : normalized === "removed" ? "removed" : "changed";
    return {
      key: field.name,
      label: field.name,
      kind,
      before: field.before || undefined,
      after: field.after || undefined,
      note: field.reason || undefined,
      incompatible: field.incompatible,
    };
  });
}

function tokenColor(kind: ChangeKind, incompatible?: boolean): "green" | "red" | "blue" {
  if (incompatible || kind === "removed") return "red";
  if (kind === "added") return "green";
  return "blue";
}

function kindLabel(kind: ChangeKind): string {
  if (kind === "added") return t("Added");
  if (kind === "removed") return t("Removed");
  return t("Changed");
}

export function RecordComparison({
  rows,
  labelHeader,
  beforeHeader,
  afterHeader,
}: {
  rows: ComparisonRow[];
  labelHeader?: string;
  beforeHeader?: string;
  afterHeader?: string;
}) {
  if (rows.length === 0) return null;
  const fieldCol = labelHeader ?? t("Fields");
  const beforeCol = beforeHeader ?? t("Before");
  const afterCol = afterHeader ?? t("After");
  return (
    <Table aria-label={t("Changes")} density="compact">
      <TableHeader>
        <TableRow>
          <TableHeaderCell>{fieldCol}</TableHeaderCell>
          <TableHeaderCell>{t("Changes")}</TableHeaderCell>
          <TableHeaderCell>{beforeCol}</TableHeaderCell>
          <TableHeaderCell>{afterCol}</TableHeaderCell>
        </TableRow>
      </TableHeader>
      <TableBody>
        {rows.map((row) => (
          <TableRow key={row.key}>
            <TableCell>
              <VStack gap={0.5}>
                <Text type="body" weight="medium">{row.label}</Text>
                {row.code && <Text type="code" color="secondary">{row.code}</Text>}
                {row.note && <Text type="supporting" color="secondary">{row.note}</Text>}
              </VStack>
            </TableCell>
            <TableCell>
              <Token size="sm" color={tokenColor(row.kind, row.incompatible)} label={kindLabel(row.kind)} />
            </TableCell>
            <TableCell>
              {row.before !== undefined ? (
                <Text type="body" color="secondary">{row.before}</Text>
              ) : (
                <Text type="supporting" color="secondary">{t("Empty")}</Text>
              )}
            </TableCell>
            <TableCell>
              {row.after !== undefined ? (
                <Text type="body">{row.after}</Text>
              ) : (
                <Text type="supporting" color="secondary">{t("Empty")}</Text>
              )}
            </TableCell>
          </TableRow>
        ))}
      </TableBody>
    </Table>
  );
}
