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
          if (controller.signal.aborted) return;
          if (failure instanceof HostRefusal && failure.status < 500) setRead({ status: "refused", path, reason: failure.message });
          else setRead({ status: "error", path, reason: failure instanceof Error ? failure.message : String(failure) });
        },
      );
    });
    return () => controller.abort();
  }, [path, sent, session, seen]);
  return read;
}
