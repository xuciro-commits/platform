import { useViewVisible } from "../shell/ViewVisibility";
import { InputDraftProvider, useInputDrafts } from "../fields/draft";
import { Dialog as AstryxDialog } from "@astryxdesign/core/Dialog";
import { Drawer as AstryxDrawer } from "@astryxdesign/core/Drawer";
import { X } from "lucide-react";
import { createContext, useContext, useEffect, useRef, type ReactNode } from "react";
import { t } from "../i18n";
import { Button, DefaultButtonSize } from "./button";

const OverlayDepth = createContext(0);
type OverlayPolicy = { backdrop?: boolean; closeOnBackdrop?: boolean; closeOnEsc?: boolean };

type OverlayProps = {
  open: boolean; onOpenChange: (open: boolean) => void; title: string; children: ReactNode;
  wide?: boolean;
  suspended?: boolean;
  returnFocus?: HTMLElement | null; fallbackFocus?: HTMLElement | null;
} & OverlayPolicy & { width?: number; side?: "left" | "right"; isolated?: boolean };

export function Dialog(props: OverlayProps) {
  return <OverlayFrame {...props} />;
}

export function OverlayFrame({ open, onOpenChange, title, children, wide, returnFocus, fallbackFocus, suspended = false, backdrop = true, closeOnBackdrop = true, closeOnEsc = true, width, side = "right", sheet = false, isolated = false }: OverlayProps & { sheet?: boolean }) {
  const visible = useViewVisible();
  suspended = suspended || !visible;
  const depth = useContext(OverlayDepth);
  const nested = depth > 0;
  const own = !nested && (isolated || !sheet);
  sheet = sheet || nested;
  const inputs = useInputDrafts();
  const busy = own && inputs?.busy;
  const change = (next: boolean) => { if (!busy && !suspended) onOpenChange(next); };
  const wasOpen = useRef(false);
  const dialog = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    if (wasOpen.current && !open) {
      const target = [returnFocus, fallbackFocus].find((element) => element?.isConnected && element !== document.body && !element.matches(":disabled") && !element.closest("[hidden],[inert]"));
      if (target) requestAnimationFrame(() => target.focus());
    }
    wasOpen.current = open;
  }, [open, returnFocus, fallbackFocus]);
  useEffect(() => {
    const node = dialog.current;
    if (!node || !open) return;
    const cancel = (event: Event) => { if (!closeOnEsc || busy || suspended) event.preventDefault(); };
    node.addEventListener("cancel", cancel);
    return () => node.removeEventListener("cancel", cancel);
  }, [open, closeOnEsc, busy, suspended]);
  const purpose = busy || suspended || (!closeOnBackdrop && !closeOnEsc) ? "required" : !closeOnBackdrop ? "form" : "info";
  if (!open) return null;
  const content = <OverlayDepth.Provider value={depth + 1}><DefaultButtonSize.Provider value={undefined}>{own ? <InputDraftProvider isolated scope="dialog">{children}</InputDraftProvider> : children}</DefaultButtonSize.Provider></OverlayDepth.Provider>;
  const header = <div className={sheet ? "flex h-10 shrink-0 items-center justify-between border-b border-border px-3" : "mb-3 flex shrink-0 items-center justify-between"}>
    <h2 className="text-base font-semibold">{title}</h2>
    <Button size="icon" variant="ghost" disabled={busy} aria-label={t("Close")} onClick={() => change(false)}><X /></Button>
  </div>;
  if (sheet) return <AstryxDrawer ref={dialog} isOpen={open && !suspended} onOpenChange={change} side={side === "left" ? "start" : "end"} width={width ?? 420} label={title} hasScrim={backdrop} purpose={purpose} padding={0}>
    <div className="flex h-full min-h-0 flex-col">
      {header}
      <div className="min-h-0 flex-1 overflow-auto p-3">{content}</div>
    </div>
  </AstryxDrawer>;
  return <AstryxDialog ref={dialog} isOpen={open && !suspended} onOpenChange={change} width={wide ? 880 : 480} maxHeight="calc(100dvh - 32px)" purpose={purpose} padding={0} aria-label={title}>
    <div className="flex max-h-[calc(100dvh-32px)] flex-col p-4">
      {header}
      <div className="min-h-0 flex-1 overflow-y-auto">{content}</div>
    </div>
  </AstryxDialog>;
}
