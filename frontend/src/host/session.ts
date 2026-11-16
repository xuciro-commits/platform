import { useSyncExternalStore } from "react";

export type Session = { token: string; tenant: string };

const storageKey = "platform:session";
const listeners = new Set<() => void>();
const stored = sessionStorage.getItem(storageKey);
let current: Session | null = stored ? JSON.parse(stored) : null;

function publish(next: Session | null) {
  current = next;
  if (next) sessionStorage.setItem(storageKey, JSON.stringify(next));
  else sessionStorage.removeItem(storageKey);
  listeners.forEach((notify) => notify());
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

export function startSession(session: Session) {
  publish(session);
}

export function endSession() {
  publish(null);
}
