import { useSyncExternalStore } from "react";

export type Session = { token: string; tenant: string };
export type SavedAccount = {
  token: string;
  tenant: string;
  id: string;
  name: string;
  email?: string;
  tenantName?: string;
};

const sessionKey = "platform:session";
const accountsKey = "platform:accounts";
const listeners = new Set<() => void>();

function readJson<T>(key: string): T | null {
  try {
    const raw = sessionStorage.getItem(key);
    return raw ? (JSON.parse(raw) as T) : null;
  } catch {
    return null;
  }
}

let current: Session | null = readJson<Session>(sessionKey);
let saved: SavedAccount[] = readJson<SavedAccount[]>(accountsKey) ?? [];
if (current && !saved.some((account) => account.token === current?.token)) {
  saved = [{ token: current.token, tenant: current.tenant, id: current.token, name: current.token }];
  sessionStorage.setItem(accountsKey, JSON.stringify(saved));
}

function notifyAll() {
  listeners.forEach((notify) => notify());
}

function persistAccounts(next: SavedAccount[]) {
  saved = next;
  if (next.length) sessionStorage.setItem(accountsKey, JSON.stringify(next));
  else sessionStorage.removeItem(accountsKey);
}

function publish(next: Session | null) {
  current = next;
  if (next) sessionStorage.setItem(sessionKey, JSON.stringify(next));
  else sessionStorage.removeItem(sessionKey);
  notifyAll();
}

function subscribe(notify: () => void) {
  listeners.add(notify);
  return () => listeners.delete(notify);
}

export function currentSession() {
  return current;
}

export function useSession() {
  return useSyncExternalStore(subscribe, currentSession);
}

export function useAccounts() {
  return useSyncExternalStore(subscribe, () => saved);
}

export function startSession(session: Session) {
  const existing = saved.find((account) => account.token === session.token);
  if (existing) {
    persistAccounts(saved.map((account) => (account.token === session.token ? { ...account, tenant: session.tenant } : account)));
  } else {
    persistAccounts([...saved, { token: session.token, tenant: session.tenant, id: session.token, name: session.token }]);
  }
  publish(session);
}

export function rememberAccount(info: { id: string; name: string; email?: string; tenantName?: string }) {
  if (!current) return;
  const active = current;
  let changed = false;
  const next = saved.map((account) => {
    if (account.token !== active.token) return account;
    if (
      account.id === info.id
      && account.name === info.name
      && account.email === info.email
      && account.tenantName === info.tenantName
      && account.tenant === active.tenant
    ) {
      return account;
    }
    changed = true;
    return { ...account, ...info, tenant: active.tenant };
  });
  if (changed) {
    persistAccounts(next);
    notifyAll();
  }
}

export function switchAccount(token: string, tenant: string) {
  if (current && current.token === token && current.tenant === tenant) return;
  publish({ token, tenant });
}

export function endSession() {
  if (!current) {
    publish(null);
    return;
  }
  const remaining = saved.filter((account) => account.token !== current?.token);
  persistAccounts(remaining);
  if (remaining.length > 0) {
    publish({ token: remaining[0].token, tenant: remaining[0].tenant });
  } else {
    publish(null);
  }
}
