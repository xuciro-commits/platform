import { Dialog as D } from "radix-ui";
import { X } from "lucide-react";
import type { ReactNode } from "react";
import { useOverlayBody } from "./overlay-body";
import { useOverlayFocus } from "./overlay-focus";
import { t } from "../i18n";

/** A side panel for details and edits that keep the page in view. */
export function Sheet({ open, onOpenChange, title, children, width = 420, returnFocus, fallbackFocus, suspended = false }: {
  open: boolean; onOpenChange: (open: boolean) => void; title: string; children: ReactNode; width?: number;
  /** Hide the global layer while retaining this logical opening's content. */
  suspended?: boolean;
  returnFocus?: HTMLElement | null; fallbackFocus?: HTMLElement | null;
}) {
  const focus = useOverlayFocus(returnFocus, fallbackFocus);
  const body = useOverlayBody(open, children);
  return (
    <D.Root open={open && !suspended} onOpenChange={(next) => { if (!suspended) onOpenChange(next); }}>
      <D.Portal>
        <D.Overlay onClick={(event) => { if (event.target === event.currentTarget && event.button === 0 && !event.ctrlKey) onOpenChange(false); }} className="fixed inset-0 z-40 bg-black/30" />
        <D.Content {...focus} onCloseAutoFocus={(event) => { if (suspended) event.preventDefault(); else focus.onCloseAutoFocus(event); }} style={{ width: `min(${width}px, 100vw)` }}
          className="fixed inset-y-0 right-0 z-50 flex flex-col border-l border-border bg-surface shadow-2xl outline-none">
          <div className="flex h-10 items-center justify-between border-b border-border px-3">
            <D.Title className="text-base font-semibold">{title}</D.Title>
            <D.Close className="rounded-sm p-1 hover:bg-row-hover" aria-label={t("Close")}><X className="size-3.5" /></D.Close>
          </div>
          <D.Description className="sr-only">{title}</D.Description>
          <div ref={body.mount} className="min-h-0 flex-1 overflow-auto p-3" />
        </D.Content>
      </D.Portal>
      {body.portal}
    </D.Root>
  );
}
