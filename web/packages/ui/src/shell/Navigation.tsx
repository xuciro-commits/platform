import { ChevronRight } from "lucide-react";
import { useEffect, useState, type ReactNode } from "react";
import { cn } from "../lib/cn";
import { routeFromHash, routeKey, type Route } from "./route";

export type NavLink = { label: string; icon?: ReactNode; route: Route; badge?: ReactNode };
export type NavItem = NavLink | { label: string; icon?: ReactNode; items: NavItem[] };
export type NavSection = { label: string; items: NavItem[] };

export function navigationLinks(items: readonly NavItem[]): NavLink[] {
  return items.flatMap(item => "route" in item ? [item] : navigationLinks(item.items));
}

export function routeCovers(link: Route, active?: string) {
  if (!active) return false;
  if (routeKey(link) === active) return true;
  const route = routeFromHash(`#/${active}`);
  return !!route && route.view === link.view && Object.entries(link.params ?? {}).every(([key, value]) => route.params?.[key] === value);
}

export function NavigationItems({ items, active, onOpen }: { items: NavItem[]; active?: string; onOpen: (route: Route) => void }) {
  return items.map(item => "route" in item
    ? <button key={routeKey(item.route)} type="button" onClick={() => onOpen(item.route)} aria-current={routeCovers(item.route, active) ? "page" : undefined}
      className={cn("flex min-h-7 w-full items-center gap-2 rounded-md px-2 py-1 text-left text-sm text-foreground hover:bg-row-hover [&_svg]:size-3.5 [&_svg]:shrink-0 [&_svg]:text-muted", routeCovers(item.route, active) && "bg-row-selected font-medium text-primary [&_svg]:text-primary")}>
      {item.icon}<span className="min-w-0 flex-1 break-words">{item.label}</span>{item.badge != null && <span className="shrink-0">{item.badge}</span>}
    </button>
    : <NavigationGroup key={item.label} item={item} active={active} onOpen={onOpen} />);
}

function NavigationGroup({ item, active, onOpen }: { item: Extract<NavItem, { items: NavItem[] }>; active?: string; onOpen: (route: Route) => void }) {
  const [expanded, setExpanded] = useState(true);
  const containsActive = navigationLinks(item.items).some(link => routeCovers(link.route, active));
  useEffect(() => { if (containsActive) setExpanded(true); }, [active, containsActive]);
  return <details open={expanded} onToggle={event => setExpanded(event.currentTarget.open)} className="group/navigation">
    <summary className="flex min-h-7 cursor-pointer list-none items-center gap-2 rounded-md px-2 py-1 text-sm hover:bg-row-hover [&::-webkit-details-marker]:hidden [&_svg]:size-3.5 [&_svg]:shrink-0">
      {item.icon}<span className="min-w-0 flex-1 break-words">{item.label}</span><ChevronRight className={cn("text-muted", expanded && "rotate-90")} />
    </summary>
    <div className="ml-3 border-l border-border pl-1"><NavigationItems items={item.items} active={active} onOpen={onOpen} /></div>
  </details>;
}
