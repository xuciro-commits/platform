import { useEffect, useRef, useState, useSyncExternalStore } from "react";
import { HostRefusal, request } from "./request";
import { useSession } from "./session";

export type Read<Answer> =
  | { status: "loading"; path?: string | null }
  | { status: "ready"; path?: string | null; data: Answer }
  | { status: "refused"; path?: string | null; reason: string }
  | { status: "error"; path?: string | null; reason: string };

const listeners = new Set<() => void>();
let version = 0;

export function changed() {
  version += 1;
  listeners.forEach((notify) => notify());
}

function subscribe(notify: () => void) {
  listeners.add(notify);
  return () => listeners.delete(notify);
}

export function useRead<Answer>(path: string | null, body?: unknown): Read<Answer> {
  const session = useSession();
  const seen = useSyncExternalStore(subscribe, () => version);
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
  const seen = useSyncExternalStore(subscribe, () => version);
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
