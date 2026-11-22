import { useEffect, useRef, useState, useSyncExternalStore } from "react";
import { HostRefusal, request } from "./request";
import { useSession } from "./session";

export type Read<Answer> =
  | { status: "loading"; path?: string | null }
  | { status: "ready"; path?: string | null; data: Answer }
  | { status: "refused"; path?: string | null; reason: string }
  | { status: "error"; path?: string | null; reason: string };

const listeners = new Set<() => void>();
let globalVersion = 0;
let metaVersion = 0;
let anyTypeVersion = 0;
const typeVersions = new Map<string, number>();

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

export function changed() {
  globalVersion += 1;
  listeners.forEach((notify) => notify());
}

export function changedTypes(types?: string[]) {
  if (!types || types.length === 0) {
    globalVersion += 1;
  } else {
    anyTypeVersion += 1;
    let touchedMeta = false;
    for (const typ of types) {
      typeVersions.set(typ, (typeVersions.get(typ) ?? 0) + 1);
      if (isMetaType(typ)) touchedMeta = true;
    }
    if (touchedMeta) metaVersion += 1;
  }
  listeners.forEach((notify) => notify());
}

function subscribe(notify: () => void) {
  listeners.add(notify);
  return () => listeners.delete(notify);
}

type WatchScope = { kind: "meta" } | { kind: "type"; type: string } | { kind: "any" };

function watchScope(path: string | null, body?: unknown): WatchScope {
  if (!path) return { kind: "meta" };
  const clean = path.split("?")[0];
  if (metaPaths.has(clean)) return { kind: "meta" };
  if (clean === "/v1/object-sets/aggregate") {
    const object = (body as { set?: { object?: unknown } } | undefined)?.set?.object;
    if (typeof object === "string" && object) return { kind: "type", type: object };
    return { kind: "any" };
  }
  if (clean.startsWith("/v1/records/")) {
    const rest = clean.slice("/v1/records/".length);
    const parts = rest.split("/");
    if (parts.length >= 3 && parts[2] === "sections") return { kind: "any" };
    if (parts[0]) return { kind: "type", type: decodeURIComponent(parts[0]) };
  }
  if (clean.startsWith("/v1/object-sets/")) {
    const type = clean.slice("/v1/object-sets/".length).split("/")[0];
    if (type) return { kind: "type", type: decodeURIComponent(type) };
  }
  return { kind: "any" };
}

function snapshotFor(scope: WatchScope): string {
  if (scope.kind === "meta") return `${globalVersion}:m${metaVersion}`;
  if (scope.kind === "type") return `${globalVersion}:t${typeVersions.get(scope.type) ?? 0}`;
  return `${globalVersion}:a${anyTypeVersion}`;
}

export function useRead<Answer>(path: string | null, body?: unknown): Read<Answer> {
  const session = useSession();
  const scope = watchScope(path, body);
  const seen = useSyncExternalStore(subscribe, () => snapshotFor(scope));
  const [read, setRead] = useState<Read<Answer>>({ status: "loading" });
  const shown = useRef("");
  const sent = body === undefined ? undefined : JSON.stringify(body);
  useEffect(() => {
    const asked = `${session?.tenant} ${session?.token} ${path} ${sent}`;
    if (shown.current !== asked) setRead({ status: "loading", path });
    shown.current = asked;
    if (path === null) return;
    const controller = new AbortController();
    queueMicrotask(() => {
      if (controller.signal.aborted) return;
      request<Answer>(path, { signal: controller.signal, body: sent === undefined ? undefined : JSON.parse(sent) }).then(
        (data) => setRead({ status: "ready", path, data }),
        (failure: unknown) => {
          if (!controller.signal.aborted) setRead(failed(failure, path));
        },
      );
    });
    return () => controller.abort();
  }, [path, sent, session, seen]);
  return read;
}

function failed(failure: unknown, path?: string | null): Read<never> {
  if (failure instanceof HostRefusal && failure.status < 500) return { status: "refused", path, reason: failure.message };
  return { status: "error", path, reason: failure instanceof Error ? failure.message : String(failure) };
}

export function useReads<Answer>(paths: string[]): Read<Answer[]> {
  const session = useSession();
  const scopes = paths.map(watchScope);
  const seen = useSyncExternalStore(subscribe, () => scopes.map(snapshotFor).join("|"));
  const [read, setRead] = useState<Read<Answer[]>>({ status: "loading" });
  const asked = JSON.stringify(paths);
  useEffect(() => {
    const controller = new AbortController();
    Promise.all((JSON.parse(asked) as string[]).map((path) => request<Answer>(path, { signal: controller.signal }))).then(
      (data) => setRead({ status: "ready", data }),
      (failure: unknown) => {
        if (!controller.signal.aborted) setRead(failed(failure));
      },
    );
    return () => controller.abort();
  }, [asked, session, seen]);
  return read;
}
