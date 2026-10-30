import type { Api } from "@platform/kernel";
import { t, type EntityInfo } from "@platform/ui";
import { kindOfType } from "../projects/resources";

export const resourceKindLabel = (type: string) => { const kind = kindOfType(type); return kind ? t(kind.label) : type; };

export function resourceTitle(id: string, ...specs: unknown[]): string {
  for (const spec of specs) {
    if (!spec || typeof spec !== "object") continue;
    const value = spec as { title?: unknown; name?: unknown };
    if (typeof value.title === "string" && value.title) return value.title;
    if (typeof value.name === "string" && value.name) return value.name;
  }
  return id;
}

const words = (key: string) => {
  const spaced = key.replace(/([a-z0-9])([A-Z])/g, "$1 $2").replace(/[-_]+/g, " ").trim();
  return spaced ? spaced[0]!.toUpperCase() + spaced.slice(1) : key;
};

export function pathLabel(path: string, entity?: EntityInfo): string {
  if (!path) return t("Whole resource");
  const segments = path.split("/").slice(1).map(segment => segment.replace(/~1/g, "/").replace(/~0/g, "~"));
  const [first, ...rest] = segments;
  const head = entity?.fields.find(field => field.name === first)?.title || words(first ?? "");
  return [head, ...rest.map(segment => /^\d+$/.test(segment) ? `#${Number(segment) + 1}` : words(segment))].join(" › ");
}

export function valueText(value: unknown): string {
  if (value === undefined || value === null || value === "") return "—";
  if (typeof value === "string") return value.length > 240 ? `${value.slice(0, 240)}…` : value;
  if (typeof value === "number" || typeof value === "boolean") return String(value);
  const json = JSON.stringify(value);
  return json.length > 320 ? `${json.slice(0, 320)}…` : json;
}

export const changeOpLabel = (op: Api.BranchResourceDiff["op"]) => op === "create" ? t("Added") : op === "delete" ? t("Deleted") : t("Modified");
export const changeOpTone = (op: Api.BranchResourceDiff["op"]) => op === "create" ? "success" as const : op === "delete" ? "danger" as const : "info" as const;
