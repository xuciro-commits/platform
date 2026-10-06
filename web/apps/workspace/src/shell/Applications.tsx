// The Applications portal (ADR-0052 §3.2), after Foundry's: every application
// this member may open, by category, searchable, starrable. It reads the apps
// the shell was given, so an application handed over while it is open shows up.
import { useHost } from "@platform/app";
import { Button, Input, PageHeader, useWorkspace, t, type PlatformApplication } from "@platform/ui";
import { Star } from "lucide-react";
import { useMemo, useState } from "react";
import { categories, projections, type Projection } from "./registry";

/** One application tile: icon, title, description, the member's role in it, and a star. */
export function ApplicationTile({ app, role, compact = false }: { app: PlatformApplication; role?: string; compact?: boolean }) {
  const { applications, favorites, toggleFavorite } = useWorkspace();
  const starred = favorites.includes(app.id);
  return (
    <div className="group relative">
      <Button type="button" onClick={() => applications?.onSelect(app.id)} aria-current={applications?.current === app.id ? "true" : undefined}
        className={compact ? "flex h-auto w-full items-center justify-start gap-2 whitespace-normal px-2 py-1.5 text-left text-sm [&_svg]:size-4 [&_svg]:shrink-0 [&_svg]:text-primary"
          : "grid h-auto w-full content-start justify-items-start gap-2 whitespace-normal p-3 text-left text-sm [&>svg]:size-6 [&>svg]:text-primary"}>
        {app.icon}
        <span className="grid min-w-0 gap-0.5">
          <span className="truncate font-medium">{app.title}</span>
          {!compact && (app.description || role) && <span className="line-clamp-2 text-xs text-muted">{app.description ?? role}</span>}
        </span>
      </Button>
      <button type="button" aria-label={starred ? t("Remove from favorites") : t("Add to favorites")} aria-pressed={starred} onClick={() => toggleFavorite(app.id)}
        className={`absolute right-1.5 top-1.5 rounded-sm p-1 hover:bg-row-hover ${starred ? "text-[var(--tone-warning)]" : "text-muted opacity-0 focus-visible:opacity-100 group-hover:opacity-100"}`}>
        <Star className="size-3.5" fill={starred ? "currentColor" : "none"} />
      </button>
    </div>
  );
}

export function ApplicationsPortal({ projection }: { projection?: Projection }) {
  const { applications, favorites } = useWorkspace();
  const { me } = useHost();
  const [query, setQuery] = useState("");
  const [only, setOnly] = useState<string>(projection ?? "all");
  const lead = projections.find((p) => p.id === only)?.categories;
  const apps = useMemo(() => {
    const text = query.trim().toLowerCase();
    return (applications?.apps ?? []).filter((a) => !text || `${a.title} ${a.description ?? ""} ${a.id}`.toLowerCase().includes(text));
  }, [applications?.apps, query]);
  const ordered = [...categories].sort((a, b) => (lead ? Number(lead.includes(b.id)) - Number(lead.includes(a.id)) : 0));
  const role = (id: string) => me.apps.find((e) => e.id === id)?.role;
  return (
    <>
      <PageHeader title={t("Applications")} description={t("Every application you may open in {tenant}: the ones your organisation runs, and the platform's own tools your role allows.", { tenant: me.tenantId })} />
      <div className="mb-4 flex flex-wrap items-center gap-2">
        <Input type="search" value={query} onChange={(e) => setQuery(e.target.value)} placeholder={t("Find an application…")} aria-label={t("Find an application…")} className="w-72" />
        <div className="flex items-center gap-1 text-xs">
          {[{ id: "all", title: () => t("All") }, ...projections].map((p) => (
            <Button key={p.id} size="sm" variant={only === p.id ? "primary" : "ghost"} onClick={() => setOnly(p.id)}>{p.title()}</Button>
          ))}
        </div>
      </div>
      {favorites.length > 0 && !query && (
        <section className="mb-5">
          <h2 className="mb-2 text-xs font-semibold uppercase tracking-wide text-muted">{t("Favorites")}</h2>
          <div className="grid max-w-6xl grid-cols-[repeat(auto-fill,minmax(200px,1fr))] gap-2">
            {apps.filter((a) => favorites.includes(a.id)).map((a) => <ApplicationTile key={a.id} app={a} role={role(a.id)} />)}
          </div>
        </section>
      )}
      {ordered.map((category) => {
        const inCategory = apps.filter((a) => a.category === category.id);
        if (inCategory.length === 0) return null;
        return (
          <section key={category.id} className="mb-5">
            <h2 className="mb-2 text-xs font-semibold uppercase tracking-wide text-muted">{category.label()}</h2>
            <div className="grid max-w-6xl grid-cols-[repeat(auto-fill,minmax(200px,1fr))] gap-2">
              {inCategory.map((a) => <ApplicationTile key={a.id} app={a} role={role(a.id)} />)}
            </div>
          </section>
        );
      })}
      {apps.length === 0 && <p className="text-sm text-muted">{t("No application matches.")}</p>}
    </>
  );
}
