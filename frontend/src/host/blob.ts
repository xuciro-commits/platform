import { useEffect, useState } from "react";
import { requestBlob } from "./request";
import { currentSession, type Session, useSession } from "./session";

type Resource = {
  users: number;
  controller: AbortController;
  pending: Promise<string>;
  url?: string;
};
const resources = new WeakMap<Session, Map<string, Resource>>();

function acquire(session: Session, path: string) {
  let entries = resources.get(session);
  if (!entries) {
    entries = new Map();
    resources.set(session, entries);
  }
  let resource = entries.get(path);
  if (!resource) {
    const controller = new AbortController();
    const next: Resource = {
      users: 0,
      controller,
      pending: requestBlob(path, controller.signal).then(blob => {
        if (controller.signal.aborted || currentSession() !== session || !next.users) return "";
        next.url = URL.createObjectURL(blob);
        return next.url;
      }),
    };
    entries.set(path, next);
    resource = next;
  }
  resource.users += 1;
  const leased = resource;
  const owned = entries;
  return {
    resource: leased,
    pending: leased.pending,
    release: () => {
      leased.users -= 1;
      queueMicrotask(() => {
        if (leased.users || owned.get(path) !== leased) return;
        owned.delete(path);
        leased.controller.abort();
        if (leased.url) URL.revokeObjectURL(leased.url);
      });
    },
  };
}

type Resolved = { session: Session; path: string; url: string; resource: Resource };

export function useBlobUrl(path: string | null) {
  const session = useSession();
  const [resolved, setResolved] = useState<Resolved>();
  useEffect(() => {
    if (!path || !session || currentSession() !== session) return;
    const lease = acquire(session, path);
    let active = true;
    void lease.pending.then(url => {
      if (active && currentSession() === session) setResolved({ session, path, url, resource: lease.resource });
    }, () => {
      if (active) setResolved(undefined);
    });
    return () => {
      active = false;
      lease.release();
    };
  }, [session, path]);
  return session && path && resolved?.session === session && resolved.path === path && resources.get(session)?.get(path) === resolved.resource ? resolved.url || undefined : undefined;
}
