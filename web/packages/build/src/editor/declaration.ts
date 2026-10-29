import type { Api } from "@platform/kernel";

export function declarationValue(value: object, fields: readonly Api.FieldInfo[]): Record<string, unknown> {
  const names = new Set(fields.filter(field => !field.readOnly).map(field => field.name));
  return Object.fromEntries(Object.entries(value).filter(([name]) => names.has(name)));
}

export function replaceDeclaration<T extends object>(current: T, next: Record<string, unknown>, fields: readonly Api.FieldInfo[]): T {
  const names = new Set(fields.filter(field => !field.readOnly).map(field => field.name));
  return { ...Object.fromEntries(Object.entries(current).filter(([name]) => !names.has(name))), ...next } as T;
}

export function declarationChanges(current: object, previous: object): Record<string, unknown> {
  const next = current as Record<string, unknown>, before = previous as Record<string, unknown>;
  return Object.fromEntries([...new Set([...Object.keys(next), ...Object.keys(before)])]
    .filter(name => JSON.stringify(next[name]) !== JSON.stringify(before[name]))
    .map(name => [name, next[name] ?? null]));
}
