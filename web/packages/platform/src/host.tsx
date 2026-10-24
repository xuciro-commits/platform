// The workspace's one context: the connection, the caller's catalog and the
// active release. Views read it and reload it; no view owns a second copy of
// identity or of discovery.
import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import type { EdgeClient } from "@platform/kernel";
import * as api from "./api";

type HostState = {
  client: EdgeClient;
  me: api.Me;
  entities: api.EntityInfo[];
  actions: api.CatalogAction[];
  release: string;
  revision: number;
  reload: () => void;
};

const HostContext = createContext<HostState | undefined>(undefined);

export function HostProvider({ client, me, release, children }: { client: EdgeClient; me: api.Me; release: string; children: ReactNode }) {
  const [entities, setEntities] = useState<api.EntityInfo[]>([]);
  const [actions, setActions] = useState<api.CatalogAction[]>([]);
  const [problem, setProblem] = useState("");
  const [revision, setRevision] = useState(0);
  const [current, setCurrent] = useState(release);

  useEffect(() => {
    let live = true;
    api.catalog(client).then((found) => {
      if (!live) return;
      setEntities(found.entities);
      setActions(found.actions);
      setProblem("");
    }).catch((error: unknown) => {
      if (live) setProblem(error instanceof Error ? error.message : String(error));
    });
    return () => { live = false; };
  }, [client, revision]);

  useEffect(() => { setCurrent(release); }, [release]);

  const reload = useCallback(() => setRevision((value) => value + 1), []);

  if (problem) throw new Error(problem);
  return <HostContext.Provider value={{ client, me, entities, actions, release: current, revision, reload }}>{children}</HostContext.Provider>;
}

export function useHost(): HostState {
  const host = useContext(HostContext);
  if (!host) throw new Error("The workspace host is not ready.");
  return host;
}

/** A read that belongs to one view: it reloads when its key changes and can be retried. */
export function useLoad<T>(load: () => Promise<T>, key: readonly unknown[]): { data?: T; error?: string; loading: boolean; reload: () => void } {
  const [data, setData] = useState<T | undefined>(undefined);
  const [error, setError] = useState<string | undefined>(undefined);
  const [loading, setLoading] = useState(true);
  const [tick, setTick] = useState(0);
  const loadRef = useRef(load);
  loadRef.current = load;

  useEffect(() => {
    let live = true;
    setLoading(true);
    loadRef.current().then((value) => {
      if (!live) return;
      setData(value);
      setError(undefined);
    }).catch((failure: unknown) => {
      if (live) setError(failure instanceof Error ? failure.message : String(failure));
    }).finally(() => { if (live) setLoading(false); });
    return () => { live = false; };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [...key, tick]);

  const reload = useCallback(() => setTick((value) => value + 1), []);
  return useMemo(() => ({ data, error, loading, reload }), [data, error, loading, reload]);
}
