import { type AnchorHTMLAttributes, type MouseEvent, type Ref, useSyncExternalStore } from "react";

function subscribe(notify: () => void) {
  addEventListener("popstate", notify);
  return () => removeEventListener("popstate", notify);
}

export function usePath() {
  return useSyncExternalStore(subscribe, () => location.pathname);
}

export function navigate(path: string) {
  history.pushState(null, "", path);
  dispatchEvent(new PopStateEvent("popstate"));
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
