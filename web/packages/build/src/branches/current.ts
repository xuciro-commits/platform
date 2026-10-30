import { useHost } from "@platform/app";
import { useSyncExternalStore } from "react";

const listeners = new Set<() => void>();
const memory = new Map<string, string>();
const storageKey = (scope: string) => `build:branch:${scope}`;

function read(scope: string): string | undefined {
  if (memory.has(scope)) return memory.get(scope) || undefined;
  let value = "";
  try { value = localStorage.getItem(storageKey(scope)) ?? ""; } catch {   }
  memory.set(scope, value);
  return value || undefined;
}

function write(scope: string, id?: string) {
  memory.set(scope, id ?? "");
  try { if (id) localStorage.setItem(storageKey(scope), id); else localStorage.removeItem(storageKey(scope)); } catch {   }
  for (const listener of listeners) listener();
}

export function useCurrentBranch() {
  const { client } = useHost();
  const scope = `${client.connection.tenant}:${client.connection.principal}`;
  const id = useSyncExternalStore(listener => { listeners.add(listener); return () => { listeners.delete(listener); }; }, () => read(scope), () => undefined);
  return { id, select: (next?: string) => write(scope, next) };
}
