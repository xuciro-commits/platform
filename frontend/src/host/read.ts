import { useEffect, useRef, useState, useSyncExternalStore } from "react";
import { HostRefusal, request } from "./request";
import { useSession } from "./session";

export type Read<Answer> =
  | { status: "loading" }
  | { status: "ready"; data: Answer }
  | { status: "refused"; reason: string }
  | { status: "error"; reason: string };

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

export function useRead<Answer>(path: string | null): Read<Answer> {
  const session = useSession();
  const seen = useSyncExternalStore(subscribe, () => version);
  const [read, setRead] = useState<Read<Answer>>({ status: "loading" });
  const shown = useRef("");
  useEffect(() => {
    const controller = new AbortController();
    const asked = `${session?.tenant} ${session?.token} ${path}`;
    if (shown.current !== asked) setRead({ status: "loading" });
    shown.current = asked;
    if (path === null) return () => controller.abort();
    request<Answer>(path, { signal: controller.signal }).then(
      (data) => setRead({ status: "ready", data }),
      (failure: unknown) => {
        if (controller.signal.aborted) return;
        if (failure instanceof HostRefusal && failure.status < 500) setRead({ status: "refused", reason: failure.message });
        else setRead({ status: "error", reason: failure instanceof Error ? failure.message : String(failure) });
      },
    );
    return () => controller.abort();
  }, [path, session, seen]);
  return read;
}
