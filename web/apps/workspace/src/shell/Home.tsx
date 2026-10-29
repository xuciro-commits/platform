import { useHost, useRead } from "@platform/app";
import type { Api } from "@platform/kernel";
import { Button, routeKey, useWorkspace, t, type InboxTask, Panel} from "@platform/ui";
import { ArrowRight, Bell, Clock, Inbox, Send, Star } from "lucide-react";
import { ApplicationTile } from "./Applications";
import { projections } from "./registry";

export function Home() {
  const { me, can } = useHost();
  const { open, recent, favorites, applications } = useWorkspace();
  const inbox = useRead<InboxTask[]>("/v1/inbox");
  const requests = useRead<Api.ApprovalRequest[]>("/v1/requests");
  const notifications = useRead<Api.Notification[]>("/v1/notifications");
  const apps = applications?.apps ?? [];
  const starred = apps.filter(app => favorites.includes(app.id));
  const business = apps.filter(app => app.category === "business");
  const hour = new Date().getHours();
  const greeting = hour < 12 ? t("Good morning, {name}") : hour < 18 ? t("Good afternoon, {name}") : t("Good evening, {name}");

  return <div className="mx-auto grid w-full max-w-6xl gap-8 py-3 sm:py-6">
    <header className="grid gap-4">
      <div>
        <h1 className="text-2xl font-semibold tracking-tight">{greeting.replace("{name}", me.principalId)}</h1>
        <p className="mt-2 text-sm text-muted">{t("Open an application or continue your work.")}</p>
      </div>
      <nav aria-label={t("Workspace tasks")} className="flex flex-wrap gap-2">
        {projections.filter(projection => apps.some(app => projection.categories.some(category => category === app.category))).map(projection => <Button key={projection.id} onClick={() => open({ view: "portal", params: { workspace: projection.id } })}>{projection.title()}<ArrowRight className="size-4" /></Button>)}
      </nav>
    </header>

    <nav aria-label={t("My work")} className="flex flex-wrap gap-x-5 gap-y-2 border-y border-border py-3 text-sm">
      {[
        { view: "inbox", label: "Inbox", icon: <Inbox />, count: inbox?.length },
        { view: "requests", label: "My requests", icon: <Send />, count: requests?.filter(request => request.state === "pending").length },
        { view: "notifications", label: "Notifications", icon: <Bell />, count: notifications?.filter(notification => !notification.read).length },
      ].map(item => <Button key={item.view} variant="ghost" size="sm" className="gap-2" onClick={() => open({ view: item.view })}>{item.icon}{t(item.label)}<span className="tabular-nums text-muted">{item.count ?? "–"}</span></Button>)}
      {can("agent.run.start") && <Button variant="ghost" size="sm" onClick={() => open({ view: "assistant" })}>{t("Assistant")}</Button>}
    </nav>

    {business.length > 0 && <Panel variant="plain" aria-label={t("Business applications")}>
      <h2 className="mb-3 text-base font-semibold">{t("Business applications")}</h2>
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">{business.map(app => <ApplicationTile key={app.id} app={app} role={me.apps.find(entry => entry.id === app.id)?.role} />)}</div>
    </Panel>}

    {(recent.length > 0 || starred.length > 0) && <div className="grid gap-8 lg:grid-cols-2">
      {recent.length > 0 && <Panel variant="plain" aria-label={t("Continue working")}>
        <h2 id="home-recent" className="mb-3 flex items-center gap-2 text-base font-semibold"><Clock className="size-4" />{t("Continue working")}</h2>
        <div className="divide-y divide-border">{recent.slice(0, 6).map(item => <Button key={routeKey(item.route)} variant="row" className="w-full justify-between gap-3 py-3" onClick={() => open(item.route)}>
          <span className="truncate">{item.title}</span><span className="truncate text-xs text-muted">{apps.find(app => app.id === item.app)?.title}</span>
        </Button>)}</div>
      </Panel>}
      {starred.length > 0 && <Panel variant="plain" aria-label={t("Favorites")}>
        <h2 id="home-favorites" className="mb-3 flex items-center gap-2 text-base font-semibold"><Star className="size-4" />{t("Favorites")}</h2>
        <div className="grid gap-2">{starred.map(app => <ApplicationTile key={app.id} app={app} compact />)}</div>
      </Panel>}
    </div>}
  </div>;
}
