import { cn } from "../lib/cn";
import { Button } from "../primitives/button";
import { t } from "../i18n";

export type NotificationItem = { id: string; title: string; body?: string; at: string; read: boolean; app?: string };

// A member's notifications, newest first: unread ones stand out and can be marked read.
export function NotificationList<T extends NotificationItem>({ items, onRead, onOpen, empty = t("Nothing new") }: {
  items: T[]; onRead: (n: T) => void; onOpen?: (n: T) => void; empty?: string;
}) {
  if (items.length === 0) return <p className="text-sm text-muted">{empty}</p>;
  return (
    <ul className="grid max-w-3xl gap-1.5">
      {items.map((n) => (
        <li key={n.id} className={cn("flex items-start gap-3 rounded-md border border-border p-3 text-sm", n.read ? "bg-surface" : "bg-row-selected")}>
          <span aria-hidden className={cn("mt-1.5 size-2 shrink-0 rounded-full", n.read ? "bg-transparent" : "bg-[var(--tone-info)]")} />
          <button type="button" className="grid flex-1 gap-0.5 text-left" onClick={() => onOpen?.(n)} disabled={!onOpen}>
            <span className={n.read ? "" : "font-semibold"}>{n.title}</span>
            {n.body && <span className="text-muted">{n.body}</span>}
            <span className="text-xs text-muted">{new Date(n.at).toLocaleString()}{n.app ? ` · ${n.app}` : ""}</span>
          </button>
          {!n.read && <Button size="sm" onClick={() => onRead(n)}>{t("Mark read")}</Button>}
        </li>
      ))}
    </ul>
  );
}
