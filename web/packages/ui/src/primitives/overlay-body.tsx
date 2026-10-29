import { useCallback, useState, type ReactNode } from "react";
import { createPortal } from "react-dom";

export function useOverlayBody(open: boolean, children: ReactNode) {
  const [body] = useState(() => typeof document === "undefined" ? null : document.createElement("div"));
  const mount = useCallback((slot: HTMLDivElement | null) => { if (slot && body) slot.appendChild(body); }, [body]);
  return { mount, portal: open && body ? createPortal(children, body) : null };
}
