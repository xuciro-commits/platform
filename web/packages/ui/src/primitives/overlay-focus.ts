import { useRef } from "react";

/** Controlled overlays may have no Radix Trigger. Restore their caller, or a
 * stable page fallback if publication/selection removed the original caller. */
export function useOverlayFocus(returnFocus?: HTMLElement | null, fallbackFocus?: HTMLElement | null) {
  const previous = useRef<HTMLElement | null>(null);
  return {
    onEscapeKeyDown: (event:KeyboardEvent) => {
      // A focused inner control consumes the first Escape. Radix observes
      // Escape before React's bubbling handler, so defer outer dismissal.
      if(document.activeElement?.closest("[data-platform-escape-boundary=true]"))event.preventDefault();
    },
    onOpenAutoFocus: () => { previous.current = document.activeElement instanceof HTMLElement ? document.activeElement : null; },
    onCloseAutoFocus: (event: Event) => {
      const target = [returnFocus, previous.current, fallbackFocus].find((element) => element?.isConnected && element !== document.body && !element.matches(":disabled") && !element.closest("[hidden],[inert]"));
      if (target) { event.preventDefault(); target.focus(); }
    },
  };
}
