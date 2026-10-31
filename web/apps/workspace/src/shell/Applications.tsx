import { useHost } from "@platform/app";
import { Button, Input, PageHeader, useWorkspace, t, type PlatformApplication , SegmentedChoice } from "@platform/ui";
import { Star } from "lucide-react";
import { useEffect, useMemo, useState } from "react";
import { categories, projections, type Projection } from "./registry";

export function ApplicationTile({ app, role, compact = false }: { app: PlatformApplication; role?: string; compact?: boolean }) {
  const { applications, favorites, toggleFavorite } = useWorkspace();
  const starred = favorites.includes(app.id);
  return (
    <div className="group relative">
      <Button type="button" onClick={() => applications?.onSelect(app.id)} aria-current={applications?.current === app.id ? "true" : undefined}
        className={compact ? "h-auto w-full justify-start gap-3 whitespace-normal py-3 pl-3 pr-10 text-left [&>svg]:size-4 [&>svg]:shrink-0 [&>svg]:text-primary"
          : "h-full min-h-24 w-full items-start justify-start gap-3 whitespace-normal py-4 pl-4 pr-10 text-left [&>svg]:mt-0.5 [&>svg]:size-5 [&>svg]:shrink-0 [&>svg]:text-primary"}>
        {app.icon}
        <span className="grid min-w-0 gap-0.5">
          <span className="break-words font-medium">{app.title}</span>
          {!compact && (app.description || role) && <span className="line-clamp-2 text-xs text-muted">{app.description ?? role}</span>}
        </span>
      </Button>
      <Button variant="ghost" size="icon" aria-label={starred ? t("Remove from favorites") : t("Add to favorites")} aria-pressed={starred} onClick={() => toggleFavorite(app.id)}
        className={`absolute right-1 top-1 size-8 ${starred ? "text-[var(--tone-warning)]" : "text-muted"}`}>
        <Star className="size-3.5" fill={starred ? "currentColor" : "none"} />
      </Button>
    </div>
  );
}

export function ApplicationsPortal({ projection }: { projection?: Projection }) {
  const { applications } = useWorkspace();
  const { me } = useHost();
  const [query, setQuery] = useState("");
  const [only, setOnly] = useState<string>(projection ?? "all");
  useEffect(() => setOnly(projection ?? "all"), [projection]);
  const lead: readonly string[] | undefined = projections.find((p) => p.id === only)?.categories;
  const apps = useMemo(() => {
    const text = query.trim().toLowerCase();
    return (applications?.apps ?? []).filter(a => (!lead || lead.includes(a.category)) && (!text || `${a.title} ${a.description ?? ""} ${a.id}`.toLowerCase().includes(text)));
  }, [applications?.apps, query, only]);
  const ordered = categories.filter(category => !lead || lead.includes(category.id));
  const role = (id: string) => me.apps.find((e) => e.id === id)?.role;
  return (
    <>
      <PageHeader title={t("Applications")} help={t("Every application you may open in {tenant}: the ones your organisation runs, and the platform's own tools your role allows.", { tenant: me.tenantId })} />
      <div className="mb-4 flex flex-wrap items-center gap-2">
        <Input type="search" value={query} onChange={(e) => setQuery(e.target.value)} placeholder={t("Find an application…")} aria-label={t("Find an application…")} className="w-full sm:w-72" />
        <SegmentedChoice label={t("Application category")} value={only} onChange={setOnly} options={[{ id: "all", title: () => t("All") }, ...projections].map((p) => ({ value: p.id, label: p.title() }))} />
      </div>
      {ordered.map((category) => {
        const inCategory = apps.filter((a) => a.category === category.id);
        if (inCategory.length === 0) return null;
        return (
          <div key={category.id} className="mb-5">
            <h2 className="mb-2 text-xs font-semibold uppercase tracking-wide text-muted">{category.label()}</h2>
            <div className="grid max-w-6xl grid-cols-[repeat(auto-fill,minmax(200px,1fr))] gap-2">
              {inCategory.map((a) => <ApplicationTile key={a.id} app={a} role={role(a.id)} />)}
            </div>
          </div>
        );
      })}
      {apps.length === 0 && <p className="text-sm text-muted">{t("No application matches.")}</p>}
    </>
  );
}
