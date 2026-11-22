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
let anyVersion = 0;
const keyVersions = new Map<string, number>();

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

export function changed() {
  globalVersion += 1;
  listeners.forEach((notify) => notify());
}

export function changedTypes(types?: string[]) {
  changedScopes(types, undefined);
}

export function changedScopes(types?: string[], scopes?: string[]) {
  const hasTypes = Boolean(types && types.length > 0);
  const hasScopes = Boolean(scopes && scopes.length > 0);
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
  const scopes = paths.map((path) => watchScope(path));
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
