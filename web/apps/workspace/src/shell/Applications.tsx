import { useHost, useReadQuery } from "@platform/app";
import type { Api } from "@platform/api";
import { Button, EmptyState, Input, PageHeader, useWorkspace, t, type PlatformApplication } from "@platform/ui";
import { Star } from "lucide-react";
import { useMemo, useState } from "react";
import { applicationGroup, categories, projections, type Projection } from "./registry";

function ApplicationTile({ app, role }: { app: PlatformApplication; role?: string }) {
  const { applications, favorites, toggleFavorite } = useWorkspace();
  const pinned = favorites.includes(app.id);
  return <div className="group relative">
    <button type="button" onClick={() => applications?.onSelect(app.id)} className="flex h-full w-full min-w-0 items-start gap-3 rounded-md border border-border bg-surface p-3 pr-9 text-left hover:border-border-strong hover:bg-row-hover">
      <span className="grid size-8 shrink-0 place-items-center rounded-md bg-row-selected text-primary [&_svg]:size-4">{app.icon}</span>
      <span className="grid min-w-0 gap-0.5"><span className="truncate text-sm font-medium">{app.title}</span>
        {(app.description || role) && <span className="line-clamp-2 text-xs text-muted">{app.description ?? role}</span>}</span>
    </button>
    <Button variant="ghost" size="icon" aria-label={pinned ? t("Remove from favorites") : t("Add to favorites")} aria-pressed={pinned} title={pinned ? t("Remove from favorites") : t("Add to favorites")} onClick={() => toggleFavorite(app.id)}
      className={`absolute right-1 top-1 ${pinned ? "text-[var(--tone-warning)]" : "text-muted opacity-0 group-hover:opacity-100 focus-visible:opacity-100"}`}>
      <Star fill={pinned ? "currentColor" : "none"} />
    </Button>
  </div>;
}

export function ApplicationsPortal({ projection }: { projection?: Projection }) {
  const { applications } = useWorkspace();
  const { me } = useHost();
  const directory = useReadQuery<Api.ResourceDirectory>("/v1/resources");
  const [query, setQuery] = useState("");
  const lead = projections.find((p) => p.id === projection)?.categories;
  const apps = useMemo(() => {
    const text = query.trim().toLowerCase();
    return (applications?.apps ?? []).filter(a => !text || `${a.title} ${a.description ?? ""} ${a.id}`.toLowerCase().includes(text)).sort((a, b) => a.title.localeCompare(b.title));
  }, [applications?.apps, query]);
  const ordered = [...categories].sort((a, b) => Number(!!lead && !lead.includes(a.id)) - Number(!!lead && !lead.includes(b.id)));
  const role = (id: string) => me.apps.find((e) => e.id === id)?.role;
  return <div className="mx-auto grid w-full max-w-6xl gap-4 py-2">
    <PageHeader title={t("Applications")} meta={t("{count} applications", { count: applications?.apps.length ?? 0 })}
      help={t("Every application you may open in {tenant}: the ones your organisation runs, and the platform's own tools your role allows.", { tenant: me.tenantId })}
      actions={<Input type="search" value={query} onChange={(e) => setQuery(e.target.value)} placeholder={t("Find an application…")} aria-label={t("Find an application…")} className="w-full sm:w-72" />} />
    {ordered.map((category) => {
      const inCategory = apps.filter((a) => a.category === category.id);
      if (!inCategory.length) return null;
      const groups = new Map<string, { label: string; apps: PlatformApplication[] }>();
      if (category.id === "business") {
        for (const app of inCategory) {
          const group = applicationGroup(app, directory.data?.resources ?? []) ?? { id: "_other", label: t("Other applications") };
          const held = groups.get(group.id) ?? { label: group.label, apps: [] };
          held.apps.push(app);
          groups.set(group.id, held);
        }
      }
      return <section key={category.id} aria-label={category.label()} className="grid gap-2">
        <h2 className="text-2xs font-semibold uppercase tracking-wider text-muted">{category.label()}</h2>
        {category.id === "business" ? <div className="grid gap-3">{[...groups.entries()].sort(([, a], [, b]) => a.label.localeCompare(b.label)).map(([id, group]) => <section key={id} aria-label={group.label} className="grid gap-2">
          <h3 className="text-xs font-medium">{group.label}</h3>
          <div className="grid grid-cols-[repeat(auto-fill,minmax(240px,1fr))] gap-2">{group.apps.map((a) => <ApplicationTile key={a.id} app={a} role={role(a.id)} />)}</div>
        </section>)}</div>
          : <div className="grid grid-cols-[repeat(auto-fill,minmax(240px,1fr))] gap-2">{inCategory.map((a) => <ApplicationTile key={a.id} app={a} role={role(a.id)} />)}</div>}
      </section>;
    })}
    {!apps.length && <EmptyState title={t("No application matches.")} action={query.trim() ? <Button onClick={() => setQuery("")}>{t("Clear search")}</Button> : undefined} />}
  </div>;
}
