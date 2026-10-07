import type { EntityInfo } from "@platform/ui";

/** Bounded authoring choices from declared single-record references. */
export function recordPaths(type: string, entity: (type: string) => EntityInfo | undefined) {
  const found: { path: string[]; label: string; field: EntityInfo["fields"][number] }[] = [];
  const visit = (type: string, path: string[], labels: string[], seen: Set<string>) => {
    if (path.length >= 4 || seen.has(type)) return;
    for (const field of entity(type)?.fields ?? []) {
      const next = [...path, field.name], names = [...labels, field.title];
      found.push({ path: next, label: names.join(" → "), field });
      if (field.type === "reference" && field.ref) visit(field.ref, next, names, new Set([...seen, type]));
    }
  };
  visit(type, [], [], new Set());
  return found;
}
