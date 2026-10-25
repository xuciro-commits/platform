// The workspace's one context: the connection, the caller's catalog and the
// active release. Views read it and reload it; no view owns a second copy of
// identity or of discovery.
import { createContext, useCallback, useContext, useEffect, useMemo, useRef, useState, type ReactNode } from "react";
import type { EdgeClient } from "@platform/kernel";
import * as api from "./api";
import { t } from "./i18n";

type CodeRelease = { release: number; releaseDigest: string };

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
  const initialCode = useRef<CodeRelease | undefined>(undefined);
  const [changedCode, setChangedCode] = useState<CodeRelease | undefined>(undefined);

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

  useEffect(() => {
    let live = true, checking = false;
    const check = async () => {
      if (checking) return;
      checking = true;
      try {
        const code = await client.get<CodeRelease>("/healthz", true);
        if (!live) return;
        initialCode.current ??= code;
        const first = initialCode.current;
        setChangedCode(code.release !== first.release || code.releaseDigest !== first.releaseDigest ? code : undefined);
      } catch { /* An unavailable host does not prove a version change. */ }
      finally { checking = false; }
    };
    void check();
    const timer = window.setInterval(() => void check(), 30_000);
    window.addEventListener("focus", check);
    return () => { live = false; window.clearInterval(timer); window.removeEventListener("focus", check); };
  }, [client]);

  const reload = useCallback(() => setRevision((value) => value + 1), []);

  if (problem) throw new Error(problem);
  return <HostContext.Provider value={{ client, me, entities, actions, release: current, revision, reload }}>
    {changedCode ? <aside className="panel" role="status">
      <strong>{t("The platform code changed. Refresh when you are ready.", me.language)}</strong>
      <p>{t("Save your current edits before refreshing. This page will not reload automatically.", me.language)}</p>
      <button onClick={() => window.location.reload()}>{t("Refresh the page", me.language)}</button>
    </aside> : null}
    {children}
  </HostContext.Provider>;
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
