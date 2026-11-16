import { useState, type ReactNode } from "react";
import { CircleHelp, X } from "lucide-react";
import { Popover } from "@astryxdesign/core/Popover";
import { Button } from "../primitives/button";
import { t } from "../i18n";

export function ContextHelp({ title, children }: { title: string; children: ReactNode }) {
  const [open, setOpen] = useState(false);
  return <Popover
    isOpen={open}
    onOpenChange={setOpen}
    label={title}
    placement="below"
    alignment="start"
    width={384}
    padding={4}
    className="max-h-[min(70vh,32rem)] overflow-auto text-sm"
    content={<div className="grid gap-3">
      <div className="flex items-start justify-between gap-3"><h2 className="font-semibold">{title}</h2><Button size="icon" variant="ghost" aria-label={t("Close")} onClick={() => setOpen(false)}><X aria-hidden="true" /></Button></div>
      <div className="grid max-w-prose gap-3 break-words leading-relaxed text-muted">{children}</div>
    </div>}
  >
    <Button variant="ghost" size="sm" className="w-fit shrink-0 text-muted" aria-label={t("About {title}", { title })}><CircleHelp aria-hidden="true" />{t("View help")}</Button>
  </Popover>;
}
