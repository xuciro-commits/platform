// The sidebar IS the navigation: every application the member may open,
// grouped once by zone, with the current application highlighted. Switching
// keeps the shell's draft protection by going through the shell's own select.
import { useWorkspace } from "@platform/ui";
import { zones } from "./registry";

export function ZoneRail() {
  const { applications } = useWorkspace();
  if (!applications) return null;
  return <div className="grid gap-3">
    {zones.map(zone => {
      const apps = applications.apps.filter(app => (zone.categories as readonly string[]).includes(app.category));
      if (!apps.length) return null;
      return <div key={zone.id}>
        <div className="px-2 pb-1 text-xs font-medium uppercase tracking-wide text-muted">{zone.label()}</div>
        <ul className="grid gap-0.5">
          {apps.map(app => <li key={app.id}>
            <button type="button" onClick={() => { if (applications.current !== app.id) applications.onSelect(app.id); }}
              aria-current={applications.current === app.id ? "true" : undefined}
              className={`flex min-h-7 w-full items-center gap-2 rounded-md px-2 py-1 text-left text-sm hover:bg-row-hover [&_svg]:size-3.5 [&_svg]:shrink-0 ${applications.current === app.id ? "bg-row-selected font-medium" : ""}`}>
              {app.icon}<span className="min-w-0 flex-1 truncate">{app.title}</span>
            </button>
          </li>)}
        </ul>
      </div>;
    })}
  </div>;
}
