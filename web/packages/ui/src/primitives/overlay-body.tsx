import { useCallback, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";

/** Keep the content's React identity while a logically open overlay is
 * suspended by its tab. Radix unmounts its global focus/pointer layer; the
 * stable body is detached until the same opening becomes visible again. */
export function useOverlayBody(open: boolean, children: ReactNode) {
  const [body] = useState(() => typeof document === "undefined" ? null : document.createElement("div"));
  const mount = useCallback((slot: HTMLDivElement | null) => { if (slot && body) slot.appendChild(body); }, [body]);
  return { mount, portal: open && body ? createPortal(children, body) : null };
}
