import { Inbox } from "lucide-react";
import type { ReactNode } from "react";
import { cn } from "../lib/cn";

export function EmptyState({ title, hint, action, icon, className }: {
  title: ReactNode;
  hint?: ReactNode;
  action?: ReactNode;
  icon?: ReactNode;
  className?: string;
}) {
  return (
    <div className={cn("grid min-w-0 place-items-center gap-2 text-center text-sm text-muted", className)}>
      {icon ?? <Inbox aria-hidden className="size-5 opacity-60" />}
      <div className="grid min-w-0 gap-0.5">
        <p className="font-medium text-foreground/80">{title}</p>
        {hint && <p className="text-xs">{hint}</p>}
      </div>
      {action && <div className="mt-1">{action}</div>}
    </div>
  );
}
