import { useEffect, useState } from "react";
import { HostRefusal, request } from "./request";
import { useSession } from "./session";

export type Read<Answer> =
  | { status: "loading" }
  | { status: "ready"; data: Answer }
  | { status: "refused"; reason: string }
  | { status: "error"; reason: string };

export function useRead<Answer>(path: string | null): Read<Answer> {
  const session = useSession();
  const [read, setRead] = useState<Read<Answer>>({ status: "loading" });
  useEffect(() => {
    const controller = new AbortController();
    setRead({ status: "loading" });
    if (path === null) return () => controller.abort();
    request<Answer>(path, controller.signal).then(
      (data) => setRead({ status: "ready", data }),
      (failure: unknown) => {
        if (controller.signal.aborted) return;
        if (failure instanceof HostRefusal && failure.status < 500) setRead({ status: "refused", reason: failure.message });
        else setRead({ status: "error", reason: failure instanceof Error ? failure.message : String(failure) });
      },
    );
    return () => controller.abort();
  }, [path, session]);
  return read;
}
