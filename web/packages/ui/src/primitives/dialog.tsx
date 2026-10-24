import { useViewVisible } from "../shell/ViewVisibility";
import {InputDraftProvider,useInputDrafts} from "../fields/draft";
import {overlayPolicy,type OverlayPolicy} from "./overlay-policy";
import { Dialog as D } from "radix-ui";
import { X } from "lucide-react";
import { createContext, useContext, type ReactNode } from "react";
import { useOverlayBody } from "./overlay-body";
import { useOverlayFocus } from "./overlay-focus";
import { t } from "../i18n";

/** How deep inside an open overlay this frame renders. A call from inside an
 * overlay opens beside the caller (a sheet), never another stacked modal:
 * one modal at a time, per the M3 overlay rule. */
const OverlayDepth = createContext(0);

type OverlayProps = {
  open: boolean; onOpenChange: (open: boolean) => void; title: string; children: ReactNode;
  /** For forms with lines: room for a row of columns. */
  wide?: boolean;
  /** Hide the global layer while retaining this logical opening's content. */
  suspended?: boolean;
  returnFocus?: HTMLElement | null; fallbackFocus?: HTMLElement | null;
} & OverlayPolicy & {width?:number;side?:"left"|"right"};

/** Modal dialog on Radix: focus trap, Escape, outside click, labelled title. */
export function Dialog(props: OverlayProps) {
  return <OverlayFrame {...props} />;
}

/** Dialogs and sheets retain one body and one focus/dismissal lifecycle. */
export function OverlayFrame({ open, onOpenChange, title, children, wide, returnFocus, fallbackFocus, suspended = false,backdrop=true,closeOnBackdrop=true,closeOnEsc=true,width,side="right",sheet=false }: OverlayProps & {sheet?:boolean}) {
  const visible = useViewVisible();
  suspended = suspended || !visible;
  const nested = useContext(OverlayDepth) > 0;
  sheet = sheet || nested;
  const inputs=useInputDrafts();
  const busy=!sheet&&inputs?.busy;
  const change=(next:boolean)=>{if(!busy&&!suspended)onOpenChange(next);};
  const focus = useOverlayFocus(returnFocus, fallbackFocus);
  const policy=overlayPolicy({backdrop,closeOnBackdrop,closeOnEsc});
  const body = useOverlayBody(open, <OverlayDepth.Provider value={useContext(OverlayDepth) + 1}>{sheet ? children : <InputDraftProvider isolated scope="dialog">{children}</InputDraftProvider>}</OverlayDepth.Provider>);
  return (
    <D.Root modal={backdrop} open={open && !suspended} onOpenChange={(next) => { change(next); }}>
      <D.Portal>
        {backdrop&&<D.Overlay onClick={(event) => { if (event.target === event.currentTarget && event.button === 0 && !event.ctrlKey && closeOnBackdrop) change(false); }} className={sheet ? "fixed inset-0 z-40 bg-black/30" : "fixed inset-0 z-50 bg-black/40"} />}
        {/* A dialog never grows past the screen: its title stays, its content
            scrolls, and what it asks for is always reachable. */}
        <D.Content data-frame={sheet ? "sheet" : "modal"} {...focus} {...policy} onEscapeKeyDown={event=>{focus.onEscapeKeyDown(event);policy.onEscapeKeyDown(event);}} onCloseAutoFocus={(event) => { if (suspended) event.preventDefault(); else focus.onCloseAutoFocus(event); }}
          style={sheet ? {width:`min(${width??420}px, 100vw)`,[side]:0} : width===undefined?undefined:{width:`min(${width}px, calc(100vw - 32px))`}}
          className={sheet ? `fixed inset-y-0 z-50 flex flex-col ${side==="left"?"border-r":"border-l"} border-border bg-surface shadow-2xl outline-none` : `fixed z-50 left-1/2 top-1/2 flex max-h-[calc(100dvh-32px)] flex-col ${wide ? "w-[min(880px,calc(100vw-32px))]" : "w-[min(480px,calc(100vw-32px))]"} -translate-x-1/2 -translate-y-1/2 rounded-md border border-border bg-surface p-4 shadow-xl`}>
          <div className={sheet ? "flex h-10 items-center justify-between border-b border-border px-3" : "mb-3 flex items-center justify-between"}>
            <D.Title className="text-base font-semibold">{title}</D.Title>
            <D.Close disabled={busy} className="rounded-sm p-1 hover:bg-row-hover" aria-label={t("Close")}><X className="size-3.5" /></D.Close>
          </div>
          <D.Description className="sr-only">{title}</D.Description>
          <div ref={body.mount} className={sheet ? "min-h-0 flex-1 overflow-auto p-3" : "min-h-0 flex-1 overflow-y-auto"} />
        </D.Content>
      </D.Portal>
      {body.portal}
    </D.Root>
  );
}
