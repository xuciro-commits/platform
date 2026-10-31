import { useHost, useRead, useReadQuery, useRecordInventory } from "@platform/app";
import type { Api } from "@platform/kernel";
import { Button, EmptyState, Panel, Tag, routeKey, t, useWorkspace, type InboxTask, type PlatformApplication } from "@platform/ui";
import { ArrowRight, Bell, ClipboardList, FolderKanban, GitBranch, Inbox, Search } from "lucide-react";
import type { ReactNode } from "react";

type ProjectRow = { id: string; name: string; title?: string; description?: string; state?: string; archived?: boolean; changed?: Api.Stamp };

const when = (at?: string | number) => {
  if (!at) return "";
  const time = typeof at === "number" ? at : Date.parse(at);
  if (Number.isNaN(time)) return "";
  const minutes = Math.round((Date.now() - time) / 60000);
  if (minutes < 1) return t("just now");
  if (minutes < 60) return t("{n} min ago", { n: minutes });
  if (minutes < 1440) return t("{n} h ago", { n: Math.round(minutes / 60) });
  return new Date(time).toLocaleDateString();
};

function Stat({ icon, label, value, onClick }: { icon: ReactNode; label: string; value?: number; onClick: () => void }) {
  return <button type="button" onClick={onClick} className="group flex min-w-0 items-center gap-3 rounded-md border border-border bg-surface px-3 py-2.5 text-left hover:border-border-strong hover:bg-row-hover">
    <span className="grid size-8 shrink-0 place-items-center rounded-md bg-row-selected text-primary [&_svg]:size-4">{icon}</span>
    <span className="grid min-w-0"><span className="text-xl font-semibold tabular-nums leading-tight">{value ?? "–"}</span><span className="truncate text-xs text-muted">{label}</span></span>
  </button>;
}

function Row({ icon, title, detail, meta, onClick }: { icon?: ReactNode; title: string; detail?: string; meta?: ReactNode; onClick: () => void }) {
  return <button type="button" onClick={onClick} className="flex w-full min-w-0 items-center gap-3 rounded-sm px-2 py-1.5 text-left hover:bg-row-hover">
    {icon && <span className="grid size-6 shrink-0 place-items-center text-muted [&_svg]:size-4">{icon}</span>}
    <span className="grid min-w-0 flex-1"><span className="truncate text-sm">{title}</span>{detail && <span className="truncate text-xs text-muted">{detail}</span>}</span>
    {meta && <span className="shrink-0 text-xs text-muted">{meta}</span>}
  </button>;
}

function AppTile({ app }: { app: PlatformApplication }) {
  const { applications } = useWorkspace();
  return <button type="button" onClick={() => applications?.onSelect(app.id)} className="flex min-w-0 items-center gap-2.5 rounded-md border border-border bg-surface px-3 py-2 text-left hover:border-border-strong hover:bg-row-hover">
    <span className="grid size-7 shrink-0 place-items-center rounded-md bg-row-selected text-primary [&_svg]:size-4">{app.icon}</span>
    <span className="grid min-w-0"><span className="truncate text-sm font-medium">{app.title}</span>{app.description && <span className="truncate text-xs text-muted">{app.description}</span>}</span>
  </button>;
}

export function Home() {
  const { me, holds } = useHost();
  const { open, recent, favorites, applications, palette } = useWorkspace();
  const builder = holds("build", "builder");
  const inbox = useRead<InboxTask[]>("/v1/inbox");
  const requests = useRead<Api.ApprovalRequest[]>("/v1/requests");
  const notifications = useRead<Api.Notification[]>("/v1/notifications");
  const projects = useRecordInventory<ProjectRow>("build.app", 1000, builder);
  const branches = useReadQuery<Api.BranchDetail[]>("/v1/branches", undefined, builder);
  const apps = applications?.apps ?? [];
  const pinned = apps.filter(app => favorites.includes(app.id));
  const business = apps.filter(app => app.category === "business");
  const tools = apps.filter(app => app.category === "build");
  const hour = new Date().getHours();
  const name = me.account?.displayName || me.principalId;
  const greeting = hour < 12 ? t("Good morning, {name}", { name }) : hour < 18 ? t("Good afternoon, {name}", { name }) : t("Good evening, {name}", { name });
  const liveProjects = (projects.data?.records ?? []).filter(project => !project.archived)
    .sort((a, b) => (b.changed?.at ?? "").localeCompare(a.changed?.at ?? "")).slice(0, 8);
  const proposals = (branches.data ?? []).filter(branch => branch.state === "proposed" || branch.state === "open" && branch.changes > 0)
    .sort((a, b) => (b.changed?.at ?? "").localeCompare(a.changed?.at ?? "")).slice(0, 6);

  return <div className="mx-auto grid w-full max-w-6xl gap-5 py-2">
    <header className="flex flex-wrap items-end justify-between gap-3">
      <div className="min-w-0">
        <h1 className="text-xl font-semibold tracking-tight">{greeting}</h1>
        <p className="mt-1 text-sm text-muted">{me.tenant?.name || me.tenantId}</p>
      </div>
      <button type="button" onClick={palette} className="flex h-8 w-full items-center gap-2 rounded-md border border-border bg-surface px-3 text-sm text-muted hover:border-border-strong sm:w-80">
        <Search className="size-3.5" /><span className="flex-1 truncate text-left">{t("Search records, apps and commands")}</span><kbd className="text-xs">⌘K</kbd>
      </button>
    </header>

    <div className="grid grid-cols-2 gap-2 md:grid-cols-4">
      <Stat icon={<Inbox />} label={t("Tasks waiting for you")} value={inbox?.length} onClick={() => open({ view: "inbox" })} />
      <Stat icon={<ClipboardList />} label={t("Requests in progress")} value={requests?.filter(request => request.state === "pending").length} onClick={() => open({ view: "requests" })} />
      <Stat icon={<Bell />} label={t("Unread notifications")} value={notifications?.filter(notification => !notification.read).length} onClick={() => open({ view: "notifications" })} />
      {builder ? <Stat icon={<GitBranch />} label={t("Open proposals")} value={branches.data?.filter(branch => branch.state === "proposed").length} onClick={() => open({ view: "changes" })} />
        : <Stat icon={<FolderKanban />} label={t("Applications")} value={apps.length} onClick={() => open({ view: "portal" })} />}
    </div>

    <div className="grid gap-5 lg:grid-cols-[minmax(0,2fr)_minmax(0,1fr)]">
      <div className="grid content-start gap-5">
        {builder && <Panel title={t("Projects")} actions={<Button size="sm" variant="ghost" onClick={() => open({ view: "projects" })}>{t("All projects")}<ArrowRight /></Button>}>
          {projects.isError && <p role="alert" className="text-xs text-danger">{t("Projects could not be loaded.")}</p>}
          {!projects.isLoading && !liveProjects.length && <EmptyState title={t("No projects yet")} hint={t("A project holds the object types, logic, pages and releases of one application.")}
            action={<Button variant="primary" onClick={() => open({ view: "projects" })}>{t("Create a project")}</Button>} />}
          <div className="-mx-2 grid">{liveProjects.map(project => <Row key={project.id} icon={<FolderKanban />} title={project.title || project.name} detail={project.description}
            meta={<span className="flex items-center gap-2">{project.state && project.state !== "published" && <Tag label={t("Draft")} />}{when(project.changed?.at)}</span>}
            onClick={() => open({ view: "project", params: { id: project.id } })} />)}</div>
        </Panel>}
        {!builder && business.length > 0 && <Panel title={t("Business applications")} actions={<Button size="sm" variant="ghost" onClick={() => open({ view: "portal" })}>{t("All applications")}<ArrowRight /></Button>}>
          <div className="grid gap-2 sm:grid-cols-2">{business.map(app => <AppTile key={app.id} app={app} />)}</div>
        </Panel>}
        <Panel title={t("Recent")}>
          {!recent.length && <p className="text-xs text-muted">{t("Nothing opened yet.")}</p>}
          <div className="-mx-2 grid">{recent.slice(0, 8).map(item => <Row key={routeKey(item.route)} title={item.title} detail={apps.find(app => app.id === item.app)?.title} meta={when(item.at)} onClick={() => open(item.route)} />)}</div>
        </Panel>
      </div>
      <div className="grid content-start gap-5">
        {builder && <Panel title={t("Branches and proposals")} actions={<Button size="sm" variant="ghost" onClick={() => open({ view: "changes" })}>{t("All")}<ArrowRight /></Button>}>
          {!branches.isLoading && !proposals.length && <p className="text-xs text-muted">{t("No open changes. Start a branch from any builder tool.")}</p>}
          <div className="-mx-2 grid">{proposals.map(branch => <Row key={branch.id} icon={<GitBranch />} title={branch.title} detail={t("{n} changes", { n: branch.changes })}
            meta={<Tag label={t(branch.state === "proposed" ? "Proposed" : "Editing")} tone={branch.state === "proposed" ? "purple" : "info"} />}
            onClick={() => open(branch.state === "proposed" ? { view: "proposal", params: { id: branch.id } } : { view: "changes" })} />)}</div>
        </Panel>}
        {pinned.length > 0 && <Panel title={t("Pinned")}><div className="grid gap-2">{pinned.map(app => <AppTile key={app.id} app={app} />)}</div></Panel>}
        {builder && tools.length > 0 && <Panel title={t("Build tools")}><div className="grid gap-2">{tools.filter(app => !favorites.includes(app.id)).map(app => <AppTile key={app.id} app={app} />)}</div></Panel>}
        {builder && business.length > 0 && <Panel title={t("Business applications")}><div className="grid gap-2">{business.slice(0, 6).map(app => <AppTile key={app.id} app={app} />)}</div></Panel>}
      </div>
    </div>
  </div>;
}
