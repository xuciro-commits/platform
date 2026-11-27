import { useEffect, useMemo, useState, useSyncExternalStore } from "react";
import { HostRefusal, invalidateRequests, request } from "./request";
import { currentSession, type Session, useSession } from "./session";

export type Read<Answer> =
  | { status: "loading"; path?: string | null }
  | { status: "ready"; path?: string | null; data: Answer }
  | { status: "refused"; path?: string | null; reason: string }
  | { status: "error"; path?: string | null; reason: string };

type Cached = { seen: string; order: number; data: unknown };

const maxCached = 256;
const listeners = new Set<() => void>();
const keyVersions = new Map<string, number>();
const readCache = new Map<string, Cached>();
const pendingReads = new Map<string, Promise<unknown>>();
let cacheSession: Session | null = null;
let sessionGeneration = 0;
let cacheOrder = 0;
let globalVersion = 0;
let metaVersion = 0;
let anyVersion = 0;

const metaPaths = new Set([
  "/v1/me",
  "/v1/entities",
  "/v1/actions",
  "/v1/applications",
  "/v1/declarations",
  "/v1/member-directory",
  "/v1/views",
]);

function isMetaType(typ: string): boolean {
  return typ.startsWith("build.") || typ.startsWith("platform.") || typ.startsWith("work.view");
}

function bump(key: string) {
  keyVersions.set(key, (keyVersions.get(key) ?? 0) + 1);
}

function syncSession(session: Session | null) {
  if (session !== cacheSession) {
    cacheSession = session;
    sessionGeneration += 1;
    cacheOrder += 1;
    readCache.clear();
    pendingReads.clear();
    invalidateRequests();
  }
  return sessionGeneration;
}

function readKey(generation: number, path: string | null, sent?: string) {
  return JSON.stringify([generation, path, sent]);
}

type ScopedRead<Answer> = { key: string; value: Read<Answer> };

function cachedRead<Answer>(key: string): Cached | undefined {
  const entry = readCache.get(key);
  if (!entry) return undefined;
  readCache.delete(key);
  readCache.set(key, entry);
  return entry as { seen: string; order: number; data: Answer };
}

function storeRead(key: string, seen: string, order: number, data: unknown) {
  const existing = readCache.get(key);
  if (existing && existing.order > order) return;
  if (existing) readCache.delete(key);
  else if (readCache.size >= maxCached) {
    const oldest = readCache.keys().next().value;
    if (oldest !== undefined) readCache.delete(oldest);
  }
  readCache.set(key, { seen, order, data });
}

function loadRead<Answer>(session: Session | null, generation: number, path: string, sent: string | undefined, seen: string): Promise<Answer> {
  const ownsSession = () => currentSession() === session && generation === sessionGeneration;
  if (!ownsSession()) return Promise.reject(new Error("Read session changed"));
  const cacheKey = readKey(generation, path, sent);
  const hit = cachedRead<Answer>(cacheKey);
  if (hit && hit.seen === seen) return Promise.resolve(hit.data as Answer);
  const flightKey = JSON.stringify([cacheKey, seen]);
  let shared = pendingReads.get(flightKey) as Promise<Answer> | undefined;
  if (!shared) {
    const order = ++cacheOrder;
    shared = request<Answer>(path, { body: sent === undefined ? undefined : JSON.parse(sent) })
      .then((data) => {
        if (ownsSession()) storeRead(cacheKey, seen, order, data);
        return data;
      }, (failure: unknown) => {
        if (ownsSession() && failure instanceof HostRefusal && failure.status < 500) {
          const previous = readCache.get(cacheKey);
          if (previous && previous.order <= order) readCache.delete(cacheKey);
        }
        throw failure;
      })
      .finally(() => {
        if (pendingReads.get(flightKey) === shared) pendingReads.delete(flightKey);
      });
    pendingReads.set(flightKey, shared);
  }
  return shared;
}

export function changed() {
  globalVersion += 1;
  invalidateRequests();
  listeners.forEach((notify) => notify());
}

export function changedTypes(types?: string[]) {
  changedScopes(types, undefined);
}

export function changedScopes(types?: string[], scopes?: string[]) {
  const hasTypes = Boolean(types && types.length > 0);
  const hasScopes = Boolean(scopes && scopes.length > 0);
  invalidateRequests();
  if (!hasTypes && !hasScopes) {
    globalVersion += 1;
  } else {
    anyVersion += 1;
    let touchedMeta = false;
    for (const typ of types ?? []) {
      bump(`type:${typ}`);
      if (isMetaType(typ)) touchedMeta = true;
    }
    for (const scope of scopes ?? []) {
      bump(`scope:${scope}`);
      if (scope === "owner:platform") touchedMeta = true;
    }
    if (touchedMeta) metaVersion += 1;
  }
  listeners.forEach((notify) => notify());
}

function subscribe(notify: () => void) {
  listeners.add(notify);
  return () => listeners.delete(notify);
}

type WatchScope = { kind: "meta" } | { kind: "keys"; keys: string[] } | { kind: "any" };

function watchScope(path: string | null, body?: unknown): WatchScope {
  if (!path) return { kind: "meta" };
  const clean = path.split("?")[0];
  if (metaPaths.has(clean)) return { kind: "meta" };
  if (clean.startsWith("/v1/actions/") && clean.endsWith("/check")) return { kind: "meta" };
  if (clean === "/v1/words") return { kind: "keys", keys: ["scope:owner:platform", "type:platform.translation"] };
  if (clean === "/v1/notifications") return { kind: "keys", keys: ["scope:notifications"] };
  if (clean === "/v1/inbox" || clean === "/v1/requests") return { kind: "keys", keys: ["scope:owner:work", "type:work.task"] };
  if (clean === "/v1/flows") return { kind: "keys", keys: ["scope:owner:flow"] };
  if (clean === "/v1/agents" || clean === "/v1/runs" || clean === "/v1/memories") return { kind: "keys", keys: ["scope:owner:agent"] };
  if (clean.startsWith("/v1/ai-") || clean.startsWith("/v1/ai/")) return { kind: "keys", keys: ["scope:owner:ai"] };
  if (clean.startsWith("/v1/enterprise") || clean === "/v1/organization") return { kind: "keys", keys: ["scope:owner:enterprise"] };
  if (clean.startsWith("/v1/releases/")) return { kind: "keys", keys: ["scope:owner:build"] };
  if (clean === "/v1/object-sets/aggregate") {
    const object = (body as { set?: { object?: unknown } } | undefined)?.set?.object;
    if (typeof object === "string" && object) return { kind: "keys", keys: [`type:${object}`] };
    return { kind: "any" };
  }
  if (clean.startsWith("/v1/records/")) {
    const rest = clean.slice("/v1/records/".length);
    const parts = rest.split("/");
    const typ = parts[0] ? decodeURIComponent(parts[0]) : "";
    if (parts.length >= 3 && parts[2] === "sections") {
      const sec = parts[3] ?? "";
      const keys = typ ? [`type:${typ}`] : [];
      if (sec === "comments") keys.push("scope:owner:relations", "scope:owner:work");
      else if (sec === "links") keys.push("scope:owner:relations");
      else if (sec === "files") keys.push("scope:owner:files", "type:files.file");
      else return { kind: "any" };
      return { kind: "keys", keys };
    }
    if (typ) return { kind: "keys", keys: [`type:${typ}`] };
  }
  if (clean.startsWith("/v1/object-sets/")) {
    const type = clean.slice("/v1/object-sets/".length).split("/")[0];
    if (type) return { kind: "keys", keys: [`type:${decodeURIComponent(type)}`] };
  }
  return { kind: "any" };
}

function snapshotFor(scope: WatchScope): string {
  if (scope.kind === "meta") return `${globalVersion}:m${metaVersion}`;
  if (scope.kind === "keys") return `${globalVersion}:${scope.keys.map((key) => keyVersions.get(key) ?? 0).join(",")}`;
  return `${globalVersion}:a${anyVersion}`;
}

export function useRead<Answer>(path: string | null, body?: unknown): Read<Answer> {
  const session = useSession();
  const generation = syncSession(session);
  const scope = watchScope(path, body);
  const seen = useSyncExternalStore(subscribe, () => snapshotFor(scope));
  const sent = body === undefined ? undefined : JSON.stringify(body);
  const key = readKey(generation, path, sent);
  const cached = path === null ? undefined : cachedRead<Answer>(key);
  const [read, setRead] = useState<ScopedRead<Answer>>(() => ({
    key, value: cached ? { status: "ready", path, data: cached.data as Answer } : { status: "loading", path },
  }));
  useEffect(() => {
    const show = (value: Read<Answer>) => setRead({ key, value });
    if (path === null) {
      show({ status: "loading", path: null });
      return;
    }
    const current = cachedRead<Answer>(key);
    if (current) {
      setRead((previous) =>
        previous.key === key && previous.value.status === "ready" && previous.value.data === current.data
          ? previous
          : { key, value: { status: "ready", path, data: current.data as Answer } },
      );
      if (current.seen === seen) return;
    } else {
      show({ status: "loading", path });
    }
    let active = true;
    const owns = () => active && currentSession() === session && generation === sessionGeneration;
    queueMicrotask(() => {
      if (!owns()) return;
      loadRead<Answer>(session, generation, path, sent, seen).then(
        (data) => { if (owns()) show({ status: "ready", path, data }); },
        (failure: unknown) => { if (owns()) show(failed(failure, path)); },
      );
    });
    return () => { active = false; };
  }, [key, path, sent, session, generation, seen]);
  const fresh = cached && cached.seen === seen ? cached : undefined;
  const shown = useMemo<Read<Answer> | undefined>(
    () => (fresh ? { status: "ready", path, data: fresh.data as Answer } : undefined),
    [fresh, path],
  );
  return shown ?? (read.key === key ? read.value : { status: "loading", path });
}

function failed(failure: unknown, path?: string | null): Read<never> {
  if (failure instanceof HostRefusal && failure.status < 500) return { status: "refused", path, reason: failure.message };
  return { status: "error", path, reason: failure instanceof Error ? failure.message : String(failure) };
}

export function useReads<Answer>(paths: string[]): Read<Answer[]> {
  const session = useSession();
  const generation = syncSession(session);
  const scopes = paths.map((path) => watchScope(path));
  const snapshots = scopes.map(snapshotFor);
  const seen = useSyncExternalStore(subscribe, () => scopes.map(snapshotFor).join("|"));
  const asked = JSON.stringify(paths);
  const key = JSON.stringify([generation, asked]);
  const allFresh = paths.every((path, index) => {
    const entry = cachedRead<Answer>(readKey(generation, path));
    return Boolean(entry && entry.seen === snapshots[index]);
  });
  const [read, setRead] = useState<ScopedRead<Answer[]>>(() => ({
    key, value: allFresh
      ? { status: "ready", data: paths.map((path) => cachedRead<Answer>(readKey(generation, path))!.data as Answer) }
      : { status: "loading" },
  }));
  useEffect(() => {
    const show = (value: Read<Answer[]>) => setRead({ key, value });
    const list = JSON.parse(asked) as string[];
    const versions = seen.split("|");
    if (list.every((path, index) => cachedRead<Answer>(readKey(generation, path))?.seen === versions[index])) {
      show({ status: "ready", data: list.map((path) => cachedRead<Answer>(readKey(generation, path))!.data as Answer) });
      return;
    }
    let active = true;
    const owns = () => active && currentSession() === session && generation === sessionGeneration;
    Promise.all(list.map((path, index) => loadRead<Answer>(session, generation, path, undefined, versions[index] ?? ""))).then(
      (data) => { if (owns()) show({ status: "ready", data }); },
      (failure: unknown) => { if (owns()) show(failed(failure)); },
    );
    return () => { active = false; };
  }, [asked, key, session, generation, seen]);
  const shown = useMemo<Read<Answer[]> | undefined>(
    () => allFresh
      ? { status: "ready", data: (JSON.parse(asked) as string[]).map((path) => cachedRead<Answer>(readKey(generation, path))!.data as Answer) }
      : undefined,
    [allFresh, asked, generation, seen],
  );
  return shown ?? (read.key === key ? read.value : { status: "loading" });
}
