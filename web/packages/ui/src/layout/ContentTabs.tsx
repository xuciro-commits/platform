import { useId, useState, type ReactNode } from "react";
import { Button } from "../primitives/button";

/** Controlled, lazy first mount; visited panels retain local input while
 * hidden. Stable item identities survive reorder. No application state here.
 */
export function ContentTabs({ label, items, value, onChange }: {
  label: string; items: { id: string; title: string; content: ReactNode }[];
  value?: string; onChange: (id: string) => void;
}) {
  const prefix = useId(), active = items.some((item) => item.id === value) ? value : items[0]?.id;
  const [visited, setVisited] = useState<Set<string>>(() => new Set(active ? [active] : []));
  if (active && !visited.has(active)) setVisited(new Set([...visited, active]));
  return <div className="grid min-w-0 gap-3">
    <div role="tablist" aria-label={label} className="flex flex-wrap gap-1 border-b border-border pb-2">
      {items.map((item, index) => <Button key={item.id} id={`${prefix}-tab-${item.id}`} role="tab" variant={item.id === active ? "primary" : "ghost"}
        aria-selected={item.id === active} aria-controls={`${prefix}-panel-${item.id}`} tabIndex={item.id === active ? 0 : -1}
        onClick={() => onChange(item.id)} onKeyDown={(event) => {
          const target = event.key === "ArrowRight" ? (index + 1) % items.length : event.key === "ArrowLeft" ? (index + items.length - 1) % items.length : event.key === "Home" ? 0 : event.key === "End" ? items.length - 1 : undefined;
          if (target === undefined) return;
          event.preventDefault(); onChange(items[target]!.id);
          document.getElementById(`${prefix}-tab-${items[target]!.id}`)?.focus();
        }}>{item.title}</Button>)}
    </div>
    {items.map((item) => <div key={item.id} role="tabpanel" id={`${prefix}-panel-${item.id}`} aria-labelledby={`${prefix}-tab-${item.id}`} hidden={item.id !== active} tabIndex={0} className="min-w-0">
      {(item.id === active || visited.has(item.id)) && item.content}
    </div>)}
  </div>;
}
