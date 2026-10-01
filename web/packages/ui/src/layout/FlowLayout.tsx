import type { ReactNode } from "react";
import { cn } from "../lib/cn";

/** Wrapping presentation layout with keyboard movement within a toolbar. */
export function FlowLayout({ children, toolbar = false, label, align = "start" }: {
  children: ReactNode; toolbar?: boolean; label?: string; align?: string;
}) {
  return <div role={toolbar ? "toolbar" : undefined} aria-label={toolbar ? label : undefined}
    className={cn("flex min-w-0 flex-wrap items-center gap-3", align === "center" ? "justify-center" : align === "end" ? "justify-end" : align === "between" ? "justify-between" : "justify-start")}
    onKeyDown={toolbar ? (event) => {
      if (!["ArrowLeft", "ArrowRight", "Home", "End"].includes(event.key) || !(event.target instanceof HTMLButtonElement)) return;
      const buttons = Array.from(event.currentTarget.querySelectorAll<HTMLButtonElement>("button:not(:disabled)"));
      const at = buttons.indexOf(event.target);
      if (at < 0 || !buttons.length) return;
      event.preventDefault();
      buttons[event.key === "Home" ? 0 : event.key === "End" ? buttons.length - 1 : (at + (event.key === "ArrowRight" ? 1 : -1) + buttons.length) % buttons.length]?.focus();
    } : undefined}>{children}</div>;
}
