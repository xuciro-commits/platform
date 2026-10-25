// Application tiles, shared by the home workbench: one application, one entry.
import { Button, useWorkspace, t, type PlatformApplication } from "@platform/ui";
import { Star } from "lucide-react";

/** One application tile: icon, title, description, the member's role in it, and a star. */
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
