import { useRef } from "react";

export function useOverlayFocus(returnFocus?: HTMLElement | null, fallbackFocus?: HTMLElement | null) {
  const previous = useRef<HTMLElement | null>(null);
  return {
    onEscapeKeyDown: (event:KeyboardEvent) => {
      if(document.activeElement?.closest("[data-platform-escape-boundary=true]"))event.preventDefault();
    },
    onOpenAutoFocus: () => { previous.current = document.activeElement instanceof HTMLElement ? document.activeElement : null; },
    onCloseAutoFocus: (event: Event) => {
      const target = [returnFocus, previous.current, fallbackFocus].find((element) => element?.isConnected && element !== document.body && !element.matches(":disabled") && !element.closest("[hidden],[inert]"));
      if (target) { event.preventDefault(); target.focus(); }
    },
  };
}
