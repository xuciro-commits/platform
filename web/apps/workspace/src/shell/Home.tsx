// Home (ADR-0052 §3.1), after Foundry's landing page: search, what waits for
// me, what I opened recently, what I starred, and the applications I may open.
// It reads only through the host's own reads; nothing here grants access.
import { useHost, useRead } from "@platform/app";
import type { Api } from "@platform/kernel";
import { Button, Card, routeKey, useWorkspace, t, type InboxTask } from "@platform/ui";
import { ArrowRight, Bell, Clock, Inbox, Search, Send, Star } from "lucide-react";
import { ApplicationTile } from "./Applications";
import { categories } from "./registry";

function Count({ icon, label, count, route, hint }: { icon: React.ReactNode; label: string; count: number | undefined; route: { view: string }; hint: string }) {
  const { open } = useWorkspace();
  return (
    <Button type="button" onClick={() => open(route)} className="grid h-auto content-start justify-items-start gap-1 whitespace-normal p-3 text-left [&>svg]:size-4 [&>svg]:text-muted">
      {icon}
      <span className="text-2xl font-semibold tabular-nums">{count ?? "–"}</span>
      <span className="text-sm font-medium">{label}</span>
      <span className="text-xs text-muted">{hint}</span>
    </Button>
  );
}

export function Home() {
  const { me, can } = useHost();
  const { open, palette, recent, favorites, applications } = useWorkspace();
  const inbox = useRead<InboxTask[]>("/v1/inbox");
  const requests = useRead<Api.ApprovalRequest[]>("/v1/requests");
  const notifications = useRead<Api.Notification[]>("/v1/notifications");
  const apps = applications?.apps ?? [];
  const starred = apps.filter((a) => favorites.includes(a.id));
  const business = apps.filter((a) => a.category === "business");
  const tools = apps.filter((a) => a.category !== "business");
  const title = (id: string) => apps.find((a) => a.id === id)?.title;
  const hour = new Date().getHours();
  const greeting = hour < 12 ? t("Good morning, {name}") : hour < 18 ? t("Good afternoon, {name}") : t("Good evening, {name}");

  return (
    <div className="mx-auto grid max-w-6xl gap-6">
      <header className="grid gap-3">
        <h1 className="text-xl font-semibold tracking-tight">{greeting.replace("{name}", me.principalId)}</h1>
        <Button onClick={palette} className="h-10 w-full max-w-2xl justify-start gap-2 px-3 font-normal text-muted hover:text-foreground">
          <Search className="size-4" /><span>{t("Search records, applications and commands…")}</span><kbd className="ml-auto text-xs">⌘K</kbd>
        </Button>
      </header>

      <div role="region" aria-labelledby="home-work">
        <h2 id="home-work" className="mb-2 text-xs font-semibold uppercase tracking-wide text-muted">{t("My work")}</h2>
        <div className="grid grid-cols-[repeat(auto-fit,minmax(180px,1fr))] gap-2">
          <Count icon={<Inbox />} label={t("Inbox")} count={inbox?.length} route={{ view: "inbox" }} hint={t("Approvals and tasks waiting for you")} />
          <Count icon={<Send />} label={t("My requests")} count={requests?.filter((r) => r.state === "pending").length} route={{ view: "requests" }} hint={t("Pending what you asked for")} />
          <Count icon={<Bell />} label={t("Notifications")} count={notifications?.filter((n) => !n.read).length} route={{ view: "notifications" }} hint={t("Unread")} />
          {can("agent.run.start") && <Count icon={<Star />} label={t("Assistant")} count={undefined} route={{ view: "assistant" }} hint={t("Ask about your records and tasks")} />}
        </div>
      </div>

      <div className="grid gap-6 lg:grid-cols-2">
        <div role="region" aria-labelledby="home-recent">
          <h2 id="home-recent" className="mb-2 flex items-center gap-1.5 text-xs font-semibold uppercase tracking-wide text-muted"><Clock className="size-3.5" />{t("Recent")}</h2>
          <Card className="grid gap-0.5 p-1">
            {recent.length === 0 && <p className="p-2 text-sm text-muted">{t("Nothing opened yet. What you open shows up here.")}</p>}
            {recent.slice(0, 8).map((r) => (
              <Button key={routeKey(r.route)} variant="row" onClick={() => open(r.route)} className="grid grid-cols-[minmax(0,1fr)_auto] items-center gap-2">
                <span className="truncate">{r.title}</span>
                <span className="truncate text-xs text-muted">{r.app ? title(r.app) : ""}</span>
              </Button>
            ))}
          </Card>
        </div>
        <div role="region" aria-labelledby="home-favorites">
          <h2 id="home-favorites" className="mb-2 flex items-center gap-1.5 text-xs font-semibold uppercase tracking-wide text-muted"><Star className="size-3.5" />{t("Favorites")}</h2>
          <Card className="grid gap-0.5 p-1">
            {starred.length === 0 && <p className="p-2 text-sm text-muted">{t("Star an application to keep it here.")}</p>}
            {starred.map((a) => <ApplicationTile key={a.id} app={a} compact />)}
          </Card>
        </div>
      </div>

      {business.length > 0 && (
        <div role="region" aria-labelledby="home-apps">
          <div className="mb-2 flex items-center justify-between">
            <h2 id="home-apps" className="text-xs font-semibold uppercase tracking-wide text-muted">{categories[0]!.label()}</h2>
            <Button variant="link" size="sm" onClick={() => open({ view: "portal" })}>{t("All applications")}<ArrowRight /></Button>
          </div>
          <div className="grid grid-cols-[repeat(auto-fill,minmax(200px,1fr))] gap-2">
            {business.map((a) => <ApplicationTile key={a.id} app={a} role={me.apps.find((e) => e.id === a.id)?.role} />)}
          </div>
        </div>
      )}
      {tools.length > 0 && (
        <div role="region" aria-labelledby="home-tools">
          <h2 id="home-tools" className="mb-2 text-xs font-semibold uppercase tracking-wide text-muted">{t("Platform tools")}</h2>
          <div className="grid grid-cols-[repeat(auto-fill,minmax(200px,1fr))] gap-1">
            {tools.map((a) => <ApplicationTile key={a.id} app={a} compact />)}
          </div>
        </div>
      )}
    </div>
  );
}
