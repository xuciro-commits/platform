import { useEffect } from "react";
import { changedTypes } from "./read";
import { endSession, useSession } from "./session";

type ChangePayload = { seq?: number; types?: string[] };

function parseFrame(frame: string): { event: string; data: string } | null {
  let event = "";
  let data = "";
  for (const line of frame.split("\n")) {
    if (line.startsWith("event:")) event = line.slice(6).trim();
    else if (line.startsWith("data:")) data += (data ? "\n" : "") + line.slice(5).trim();
  }
  return event ? { event, data } : null;
}

function typesOf(data: string): string[] | undefined {
  if (!data.startsWith("{")) return undefined;
  try {
    const parsed = JSON.parse(data) as ChangePayload;
    return Array.isArray(parsed.types) && parsed.types.length > 0 ? parsed.types : undefined;
  } catch {
    return undefined;
  }
}

export function useLiveChanges() {
  const session = useSession();
  const token = session?.token;
  const tenant = session?.tenant;

  useEffect(() => {
    if (!token || !tenant) return;
    let closed = false;
    let active: AbortController | null = null;
    let timer: number | undefined;
    let connectedBefore = false;
    let missedAll = false;
    const missedTypes = new Set<string>();

    const dispatch = (types?: string[]) => {
      if (document.visibilityState !== "visible") {
        if (!types || types.length === 0) missedAll = true;
        else for (const typ of types) missedTypes.add(typ);
        return;
      }
      changedTypes(types);
    };

    const onVisibility = () => {
      if (document.visibilityState !== "visible") return;
      if (missedAll) {
        missedAll = false;
        missedTypes.clear();
        changedTypes();
      } else if (missedTypes.size > 0) {
        const types = [...missedTypes];
        missedTypes.clear();
        changedTypes(types);
      }
    };
    document.addEventListener("visibilitychange", onVisibility);

    const connect = async (delayMs: number) => {
      if (closed) return;
      const controller = new AbortController();
      active = controller;
      let firstChanged = true;
      try {
        const response = await fetch("/v1/changes", {
          headers: { authorization: `Bearer ${token}`, "Platform-Tenant": tenant },
          signal: controller.signal,
        });
        if (response.status === 401) {
          endSession();
          return;
        }
        if (!response.ok || !response.body) throw new Error(`/v1/changes: HTTP ${response.status}`);
        if (connectedBefore) dispatch();
        connectedBefore = true;
        delayMs = 500;
        const reader = response.body.getReader();
        const decoder = new TextDecoder();
        let buffer = "";
        for (;;) {
          const { done, value } = await reader.read();
          if (done || closed) break;
          buffer += decoder.decode(value, { stream: true });
          for (let end = buffer.indexOf("\n\n"); end >= 0; end = buffer.indexOf("\n\n")) {
            const raw = buffer.slice(0, end);
            buffer = buffer.slice(end + 2);
            const parsed = parseFrame(raw);
            if (!parsed || parsed.event !== "changed") continue;
            if (firstChanged) {
              firstChanged = false;
              continue;
            }
            dispatch(typesOf(parsed.data));
          }
        }
      } catch {
        if (closed) return;
      }
      if (!closed) {
        timer = window.setTimeout(() => void connect(Math.min(delayMs * 2, 5000)), delayMs);
      }
    };

    void connect(500);
    return () => {
      closed = true;
      document.removeEventListener("visibilitychange", onVisibility);
      if (timer !== undefined) window.clearTimeout(timer);
      active?.abort();
    };
  }, [token, tenant]);
}
