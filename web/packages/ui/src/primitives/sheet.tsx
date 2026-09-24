import { Dialog as D } from "radix-ui";
import { X } from "lucide-react";
import type { ReactNode } from "react";

/** A side panel for details and edits that keep the page in view. */
export function Sheet({ open, onOpenChange, title, children, width = 420 }: {
  open: boolean; onOpenChange: (open: boolean) => void; title: string; children: ReactNode; width?: number;
}) {
  return (
    <D.Root open={open} onOpenChange={onOpenChange}>
      <D.Portal>
        <D.Overlay className="fixed inset-0 z-40 bg-black/30" />
        <D.Content style={{ width: `min(${width}px, 100vw)` }}
          className="fixed inset-y-0 right-0 z-50 flex flex-col border-l border-border bg-surface shadow-2xl outline-none">
          <div className="flex h-10 items-center justify-between border-b border-border px-3">
            <D.Title className="text-base font-semibold">{title}</D.Title>
            <D.Close className="rounded-sm p-1 hover:bg-row-hover" aria-label="Close"><X className="size-3.5" /></D.Close>
          </div>
          <D.Description className="sr-only">{title}</D.Description>
          <div className="min-h-0 flex-1 overflow-auto p-3">{children}</div>
        </D.Content>
      </D.Portal>
    </D.Root>
  );
}
