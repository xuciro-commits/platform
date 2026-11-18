import { type AnchorHTMLAttributes, type MouseEvent, type Ref, useSyncExternalStore } from "react";

function subscribe(notify: () => void) {
  addEventListener("popstate", notify);
  return () => removeEventListener("popstate", notify);
}

export function usePath() {
  return useSyncExternalStore(subscribe, () => location.pathname);
}

export function useSearch() {
  return useSyncExternalStore(subscribe, () => location.search);
}

export function useHash() {
  return useSyncExternalStore(subscribe, () => location.hash);
}

type PermissionToLeave = Promise<boolean>;
type LeaveGuard = () => PermissionToLeave;
const guards = new Set<LeaveGuard>();
let leaving = false;

export function beforeLeave(guard: LeaveGuard) {
  guards.add(guard);
  return () => { guards.delete(guard); };
}

export async function navigate(path: string, state: unknown = null, replace = false) {
  if (leaving) return;
  leaving = true;
  try {
    for (const guard of guards) if (!await guard()) return;
    if (replace) history.replaceState(state, "", path);
    else history.pushState(state, "", path);
    dispatchEvent(new PopStateEvent("popstate"));
  } finally {
    leaving = false;
  }
}

export function RouterLink({ ref, onClick, ...rest }: AnchorHTMLAttributes<HTMLAnchorElement> & { ref?: Ref<HTMLAnchorElement> }) {
  const follow = (event: MouseEvent<HTMLAnchorElement>) => {
    onClick?.(event);
    const href = event.currentTarget.getAttribute("href");
    const plain = event.button === 0 && !event.metaKey && !event.ctrlKey && !event.shiftKey && !event.altKey;
    if (event.defaultPrevented || !plain || !href?.startsWith("/") || event.currentTarget.target === "_blank") return;
    event.preventDefault();
    navigate(href);
  };
  return <a ref={ref} onClick={follow} {...rest} />;
}
