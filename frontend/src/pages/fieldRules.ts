import type { Api } from "../host";

export type InputStatus = { type: "error" | "success"; message?: string };

const typed = new Set(["string", "longtext", "date", "datetime", "entity", "reference", "choice", "member"]);
const numeric = new Set(["integer", "number", "money"]);
const multiple = new Set(["member[]", "action[]"]);

export function isNumericParameter(type: string) { return numeric.has(type); }

export function isMultipleParameter(type: string) { return multiple.has(type); }

export function enterable(parameter: Api.Field): boolean {
  if (parameter.type === "lines") return Boolean(parameter.fields?.length && parameter.fields.every(enterable));
  return Boolean(parameter.choices) || parameter.type === "boolean" || parameter.type === "attachment" || typed.has(parameter.type) || isNumericParameter(parameter.type) || isMultipleParameter(parameter.type);
}

export function settles(parameter: Api.Field, field?: Api.FieldInfo) {
  return Boolean(parameter.ref || parameter.choices) || field?.type === "reference" || field?.type === "member" || parameter.type === "member" || parameter.type === "reference" || parameter.type === "boolean" || parameter.type === "attachment" || isNumericParameter(parameter.type) || isMultipleParameter(parameter.type);
}

export function missingValue(parameter: Api.Field, value: unknown): boolean {
  if (value == null || value === "") return Boolean(parameter.required);
  if (parameter.type === "lines" && Array.isArray(value)) {
    return (Boolean(parameter.required) && value.length === 0) ||
      value.some((row) => parameter.fields?.some((field) => missingValue(field, row[field.name])));
  }
  return parameter.type === "action[]" && Boolean(parameter.required) && Array.isArray(value) && value.length === 0;
}
