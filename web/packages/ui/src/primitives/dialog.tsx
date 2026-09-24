import { Dialog as D } from "radix-ui";
import { X } from "lucide-react";
import type { ReactNode } from "react";

/** Modal dialog on Radix: focus trap, Escape, outside click, labelled title. */
export function Dialog({ open, onOpenChange, title, children }: {
  open: boolean; onOpenChange: (open: boolean) => void; title: string; children: ReactNode;
}) {
  return (
    <D.Root open={open} onOpenChange={onOpenChange}>
      <D.Portal>
        <D.Overlay className="fixed inset-0 z-50 bg-black/40" />
        <D.Content className="fixed z-50 left-1/2 top-1/2 w-[min(480px,calc(100vw-32px))] -translate-x-1/2 -translate-y-1/2 rounded-md border border-border bg-surface p-4 shadow-xl">
          <div className="mb-3 flex items-center justify-between">
            <D.Title className="text-base font-semibold">{title}</D.Title>
            <D.Close className="rounded-sm p-1 hover:bg-row-hover" aria-label="Close"><X className="size-3.5" /></D.Close>
          </div>
          <D.Description className="sr-only">{title}</D.Description>
          {children}
        </D.Content>
      </D.Portal>
    </D.Root>
  );
}
