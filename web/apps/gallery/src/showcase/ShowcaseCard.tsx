import type { ReactNode } from "react";
import { cn } from "@platform/ui";

export function ShowcaseCard({
  title,
  description,
  children,
  className,
  contentClassName,
}: {
  title: string;
  description?: string;
  children: ReactNode;
  className?: string;
  contentClassName?: string;
}) {
  return (
    <div className={cn("rounded-lg border border-border bg-surface p-4 shadow-xs", className)}>
      <div className="mb-3">
        <h3 className="text-sm font-semibold text-foreground tracking-tight">{title}</h3>
        {description && <p className="mt-0.5 text-xs text-muted leading-normal">{description}</p>}
      </div>
      <div className={cn("flex flex-wrap items-center gap-3", contentClassName)}>
        {children}
      </div>
    </div>
  );
}
