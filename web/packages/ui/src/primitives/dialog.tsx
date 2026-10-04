import {overlayPolicy,type OverlayPolicy} from "./overlay-policy";
import { Dialog as D } from "radix-ui";
import { X } from "lucide-react";
import type { ReactNode } from "react";
import { useOverlayBody } from "./overlay-body";
import { useOverlayFocus } from "./overlay-focus";
import { t } from "../i18n";

/** Modal dialog on Radix: focus trap, Escape, outside click, labelled title. */
export function Dialog({ open, onOpenChange, title, children, wide, returnFocus, fallbackFocus, suspended = false,backdrop=true,closeOnBackdrop=true,closeOnEsc=true,width }: {
  open: boolean; onOpenChange: (open: boolean) => void; title: string; children: ReactNode;
  /** For forms with lines: room for a row of columns. */
  wide?: boolean;
  /** Hide the global layer while retaining this logical opening's content. */
  suspended?: boolean;
  returnFocus?: HTMLElement | null; fallbackFocus?: HTMLElement | null;
} & OverlayPolicy & {width?:number;side?:"left"|"right"}) {
  const focus = useOverlayFocus(returnFocus, fallbackFocus);
  const policy=overlayPolicy({backdrop,closeOnBackdrop,closeOnEsc});
  const body = useOverlayBody(open, children);
  return (
    <D.Root modal={backdrop} open={open && !suspended} onOpenChange={(next) => { if (!suspended) onOpenChange(next); }}>
      <D.Portal>
        {backdrop&&<D.Overlay onClick={(event) => { if (event.target === event.currentTarget && event.button === 0 && !event.ctrlKey && closeOnBackdrop) onOpenChange(false); }} className="fixed inset-0 z-50 bg-black/40" />}
        {/* A dialog never grows past the screen: its title stays, its content
            scrolls, and what it asks for is always reachable. */}
        <D.Content {...focus} {...policy} onEscapeKeyDown={event=>{focus.onEscapeKeyDown(event);policy.onEscapeKeyDown(event);}} onCloseAutoFocus={(event) => { if (suspended) event.preventDefault(); else focus.onCloseAutoFocus(event); }} style={width===undefined?undefined:{width:`min(${width}px, calc(100vw - 32px))`}} className={`fixed z-50 left-1/2 top-1/2 flex max-h-[calc(100dvh-32px)] flex-col ${wide ? "w-[min(880px,calc(100vw-32px))]" : "w-[min(480px,calc(100vw-32px))]"} -translate-x-1/2 -translate-y-1/2 rounded-md border border-border bg-surface p-4 shadow-xl`}>
          <div className="mb-3 flex items-center justify-between">
            <D.Title className="text-base font-semibold">{title}</D.Title>
            <D.Close className="rounded-sm p-1 hover:bg-row-hover" aria-label={t("Close")}><X className="size-3.5" /></D.Close>
          </div>
          <D.Description className="sr-only">{title}</D.Description>
          <div ref={body.mount} className="min-h-0 flex-1 overflow-y-auto" />
        </D.Content>
      </D.Portal>
      {body.portal}
    </D.Root>
  );
}
