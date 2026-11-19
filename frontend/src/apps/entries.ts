import { type ComponentType, useCallback } from "react";
import { type Api, useRead } from "../host";
import { usePath } from "../router";

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

export function useRecordHref() {
  const app = usePath().split("/")[1];
  const views = useRead<Api.ApplicationView[]>("/v1/applications");
  const loaded = views.status === "ready" ? views.data : undefined;
  return useCallback((type: string, id?: string) => {
    const page = loaded?.find((view) => (view.name || view.app) === app)?.menu?.flatMap((group) => group.entries).find((entry) => entry.object === type);
    const list = page ? `/${app}/${page.name}` : homes.get(type) ?? `/ontology/objects/${encodeURIComponent(type)}`;
    return id === undefined ? list : `${list}/${encodeURIComponent(id)}`;
  }, [app, loaded]);
}
