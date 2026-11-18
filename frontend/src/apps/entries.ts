import type { ComponentType } from "react";

type Entry = ComponentType & { recordType?: string };

const modules = import.meta.glob<{ default: Entry }>("./*/*.{ts,tsx}", { eager: true });

export const entries: Record<string, Entry> = {
  ...Object.fromEntries(Object.entries(modules).map(([path, module]) => [path.replace(/^\.\/|\.tsx?$/g, ""), module.default])),
};

const homes = new Map(Object.entries(entries)
  .flatMap(([key, entry]) => entry.recordType ? [[entry.recordType, `/${key}`]] : []));

export function recordHref(type: string, id: string) {
  return `${homes.get(type) ?? `/ontology/objects/${encodeURIComponent(type)}`}/${encodeURIComponent(id)}`;
}
