import { useId, useState, type ReactNode } from "react";
import { Button } from "../primitives/button";
import { cn } from "../lib/cn";
import { ViewVisibilityContext, useViewVisible } from "../shell/ViewVisibility";

export function ContentTabs({ label, items, value, onChange, compact = false, wrap = !compact, open = true, actions, className, disabled = false, strict = false }: {
  label: string; items: { id: string; title: ReactNode; content?: ReactNode }[];
  value?: string; onChange: (id: string) => void;
  compact?: boolean; wrap?: boolean; open?: boolean; actions?: ReactNode; className?: string; disabled?: boolean; strict?: boolean;
}) {
  const visible = useViewVisible(), shown = visible && open;
  const prefix = useId(), matched = items.some((item) => item.id === value), active = matched ? value : strict ? undefined : items[0]?.id;
  const focusable = active ?? items[0]?.id;
  const [visited, setVisited] = useState<Set<string>>(() => new Set(shown && active ? [active] : []));
  if (shown && active && !visited.has(active)) setVisited(new Set([...visited, active]));
  return <div className={cn(compact ? "flex min-h-0 min-w-0 flex-1 flex-col" : "grid min-w-0 gap-3", className)}>
    <div className="flex min-w-0 shrink-0 items-center border-b border-border">
      <div role="tablist" aria-label={label} className={cn("flex min-w-0 flex-1 gap-1", wrap ? "flex-wrap" : "overflow-x-auto", compact && "px-1 pt-1")}>
        {items.map((item, index) => <Button key={item.id} id={`${prefix}-tab-${item.id}`} role="tab" variant="ghost" size={compact ? "sm" : "md"} disabled={disabled}
          aria-selected={open && item.id === active} aria-controls={item.content === undefined ? undefined : `${prefix}-panel-${item.id}`} tabIndex={item.id === focusable ? 0 : -1}
          className={cn("shrink-0 rounded-none rounded-t border-0 border-b-2", compact && "text-xs", open && item.id === active ? "border-primary font-medium" : "border-transparent text-muted")}
          onClick={() => onChange(item.id)} onKeyDown={(event) => {
            const target = event.key === "ArrowRight" ? (index + 1) % items.length : event.key === "ArrowLeft" ? (index + items.length - 1) % items.length : event.key === "Home" ? 0 : event.key === "End" ? items.length - 1 : undefined;
            if (target === undefined) return;
            event.preventDefault(); onChange(items[target]!.id);
            document.getElementById(`${prefix}-tab-${items[target]!.id}`)?.focus();
          }}>{item.title}</Button>)}
      </div>
      {actions}
    </div>
    {items.filter((item) => item.content !== undefined).map((item) => <div key={item.id} role="tabpanel" id={`${prefix}-panel-${item.id}`} aria-labelledby={`${prefix}-tab-${item.id}`} hidden={!open || item.id !== active} tabIndex={0}
      className={cn("min-w-0", compact && "min-h-0 flex-1 overflow-auto")}>
      <ViewVisibilityContext.Provider value={visible && open && item.id === active}>
        {(shown && item.id === active || visited.has(item.id)) && item.content}
      </ViewVisibilityContext.Provider>
    </div>)}
  </div>;
}
