import {t} from "@platform/ui";
import type {ValueSchema} from "./workflow-model";

export function schemaIssue(schema: ValueSchema, value: unknown): string | undefined {
  if (value === null && schema.nullable) return undefined;
  if (schema.type === "variant") {
    if (!value || typeof value !== "object" || Array.isArray(value)) return t("Expected a tagged object.");
    const tag = (value as Record<string, unknown>)[schema.discriminator ?? ""];
    if (typeof tag !== "string" || !schema.variants?.[tag]) return t("Choose a declared variant tag.");
    return schemaIssue(schema.variants[tag], value);
  }
  if (schema.type === "object") {
    if (!value || typeof value !== "object" || Array.isArray(value)) return t("Expected an object.");
    const record = value as Record<string, unknown>;
    for (const name of schema.required ?? []) if (!(name in record)) return t("Required field: {name}", { name });
    for (const [name, child] of Object.entries(record)) {
      const property = schema.properties&&Object.hasOwn(schema.properties,name)?schema.properties[name]:undefined;
      if (!property) return t("Unknown field: {name}", { name });
      const issue = schemaIssue(property, child); if (issue) return `${name}: ${issue}`;
    }
  } else if (schema.type === "array") {
    if (!Array.isArray(value)) return t("Expected an array.");
    if (schema.maxItems && value.length > schema.maxItems) return t("The array exceeds its limit.");
    if (schema.items) for (const item of value) { const issue = schemaIssue(schema.items, item); if (issue) return issue; }
  } else if (schema.type === "string") {
    if (typeof value !== "string") return t("Expected text.");
    if (schema.enum?.length && !schema.enum.includes(value)) return t("Choose a declared option.");
    if (schema.maxLength && value.length > schema.maxLength) return t("The text exceeds its limit.");
  } else if (schema.type === "boolean" && typeof value !== "boolean") return t("Expected true or false.");
  else if (schema.type === "number" && (typeof value !== "number" || !Number.isFinite(value))) return t("Expected a number.");
  else if (schema.type === "integer" && (typeof value !== "number" || !Number.isSafeInteger(value))) return t("Expected a safe integer.");
  return undefined;
}
