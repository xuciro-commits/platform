// The sidebar IS the navigation: every application grouped once by zone, and
// the current application opens its own menu tree beneath its entry, so an
// app's many pages stay one glance from its name. Switching apps keeps the
// shell's draft protection by going through the shell's own select.
import { Button, routeKey, useWorkspace, type NavSection, type Route } from "@platform/ui";
import { zones } from "./registry";

type ZoneNavItem = NavSection["items"][number];

/** One app-menu row at its nesting depth; a group renders its label and nests. */
function ZoneNavRow({ item, active, onOpen, depth }: { item: ZoneNavItem; active?: string; onOpen: (route: Route) => void; depth: number }) {
  if ("route" in item) {
    const key = routeKey(item.route);
    return <button type="button" onClick={() => onOpen(item.route)} aria-current={key === active ? "page" : undefined}
      className={`flex min-h-6 w-full items-center gap-2 rounded-md py-0.5 text-left text-[13px] hover:bg-row-hover [&_svg]:size-3 [&_svg]:shrink-0 ${key === active ? "bg-row-selected font-medium text-primary" : "text-muted"} ${depth > 0 ? "pl-4 pr-2" : "px-2"}`}>
      {item.icon}<span className="min-w-0 flex-1 truncate">{item.label}</span>{item.badge != null && <span className="shrink-0 tabular-nums">{item.badge}</span>}
    </button>;
  }
  if (!item.items.length) return null;
  return <div>
    <div className="px-2 pb-0.5 pt-1.5 text-[11px] font-medium uppercase tracking-wide text-muted">{item.label}</div>
    <div className={depth > 0 ? "pl-3" : ""}>
      {item.items.map((nested, at) => <ZoneNavRow key={"route" in nested ? routeKey(nested.route) : `${item.label}/${at}`} item={nested} active={active} onOpen={onOpen} depth={depth + 1} />)}
    </div>
  </div>;
}

export function ZoneRail({ sections = [], activeRoute }: { sections?: NavSection[]; activeRoute?: Route }) {
  const { applications, open } = useWorkspace();
  if (!applications) return null;
  const active = activeRoute ? routeKey(activeRoute) : undefined;
  return <div className="grid gap-3">
    {zones.map(zone => {
      const apps = applications.apps.filter(app => (zone.categories as readonly string[]).includes(app.category));
      if (!apps.length) return null;
      return <div key={zone.id}>
        <div className="px-2 pb-1 text-xs font-medium uppercase tracking-wide text-muted">{zone.label()}</div>
        <ul className="grid gap-0.5">
          {apps.map(app => <li key={app.id}>
            <Button variant="row" onClick={() => { if (applications.current !== app.id) applications.onSelect(app.id); }}
              aria-current={applications.current === app.id ? "true" : undefined}
              className={`min-h-7 gap-2 rounded-md ${applications.current === app.id ? "bg-row-selected font-medium" : ""}`}>
              {app.icon}<span className="min-w-0 flex-1 truncate">{app.title}</span>
            </Button>
            {applications.current === app.id && sections.length > 0 && <div className="ml-3 grid gap-0.5 border-l border-border pl-1" role="list" aria-label={app.title}>
              {sections.map(section => section.items.length > 0 && <div key={section.label}>
                <div className="px-2 pb-0.5 pt-1.5 text-[11px] font-medium uppercase tracking-wide text-muted">{section.label}</div>
                {section.items.map((item, at) => <ZoneNavRow key={"route" in item ? routeKey(item.route) : `${section.label}/${at}`} item={item} active={active} onOpen={route => open(route)} depth={0} />)}
              </div>)}
            </div>}
          </li>)}
        </ul>
      </div>;
    })}
  </div>;
}
