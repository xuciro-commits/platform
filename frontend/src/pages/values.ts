import type { Api } from "../host";

type Typed = { kind: string; value: unknown };
const numeric = new Set(["decimal", "integer", "number"]);

function typed(value: unknown): value is Typed {
  return typeof value === "object" && value !== null && "kind" in value && "value" in value;
}

export function plain(value: unknown) {
  if (!typed(value)) return value;
  return numeric.has(value.kind) ? Number(value.value) : value.value;
}

export function shown(value: unknown) {
  const given = plain(value);
  if (given == null) return "";
  if (typeof given === "number") return given.toLocaleString();
  return Array.isArray(given) ? given.join(", ") : String(given);
}

export function present(field: Api.FieldInfo, value: unknown) {
  const given = plain(value);
  if (field.type === "choice" && typeof given === "string") return field.choiceTitles?.[field.choices?.indexOf(given) ?? -1] ?? given;
  return shown(given);
}
