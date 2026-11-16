import { useHost, useRead, useReadQuery } from "@platform/app";
import type { Api } from "@platform/api";
import { Button, EmptyState, Panel, Tag, routeKey, t, useWorkspace, type InboxTask, type PlatformApplication } from "@platform/ui";
import { ClickableCard } from "@astryxdesign/core/ClickableCard";
import { Grid } from "@astryxdesign/core/Grid";
import { Heading } from "@astryxdesign/core/Heading";
import { Icon } from "@astryxdesign/core/Icon";
import { Kbd } from "@astryxdesign/core/Kbd";
import { List, ListItem } from "@astryxdesign/core/List";
import { HStack, StackItem, VStack } from "@astryxdesign/core/Stack";
import { Text } from "@astryxdesign/core/Text";
import { ArrowRight, Bell, ClipboardList, FolderKanban, GitBranch, Inbox, Search } from "lucide-react";
import type { ReactNode } from "react";
import { applicationGroup } from "./registry";

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
  return <ClickableCard label={label} onClick={onClick} padding={3}>
    <HStack gap={3} vAlign="center">
      <Text color="accent">{icon}</Text>
      <VStack gap={0}>
        <Text size="xl" weight="semibold" hasTabularNumbers maxLines={1}>{value ?? "–"}</Text>
        <Text type="supporting" maxLines={1}>{label}</Text>
      </VStack>
    </HStack>
  </ClickableCard>;
}

function Row({ icon, title, detail, meta, onClick }: { icon?: ReactNode; title: string; detail?: string; meta?: ReactNode; onClick: () => void }) {
  return <ListItem label={title} description={detail} startContent={icon}
    endContent={meta ? <Text type="supporting" maxLines={1}>{meta}</Text> : undefined} onClick={onClick} />;
}

function AppTile({ app }: { app: PlatformApplication }) {
  const { applications } = useWorkspace();
  return <ClickableCard label={app.title} onClick={() => applications?.onSelect(app.id)} padding={2}>
    <HStack gap={3} vAlign="center">
      <Text color="accent">{app.icon}</Text>
      <StackItem size="fill"><VStack gap={0}>
        <Text weight="medium" maxLines={1}>{app.title}</Text>
        {app.description && <Text type="supporting" maxLines={1}>{app.description}</Text>}
      </VStack></StackItem>
    </HStack>
  </ClickableCard>;
}

function BusinessAppTiles({ apps, resources }: { apps: PlatformApplication[]; resources: readonly Api.ResourceEntry[] }) {
  const groups = new Map<string, { label: string; apps: PlatformApplication[] }>();
  for (const app of apps) {
    const group = applicationGroup(app, resources) ?? { id: "_other", label: t("Other applications") };
    const held = groups.get(group.id) ?? { label: group.label, apps: [] };
    held.apps.push(app);
    groups.set(group.id, held);
  }
  return <VStack gap={3}>{[...groups.entries()].sort(([, a], [, b]) => a.label.localeCompare(b.label)).map(([id, group]) => <VStack key={id} gap={1} role="group" aria-label={group.label}>
    <Text as="p" role="heading" aria-level={3} weight="medium" color="secondary">{group.label}</Text>
    <Grid columns={{ minWidth: 240, max: 2 }} gap={2}>{group.apps.map(app => <AppTile key={app.id} app={app} />)}</Grid>
  </VStack>)}</VStack>;
}

export function Home() {
  const { me, holds } = useHost();
  const { open, recent, favorites, applications, palette } = useWorkspace();
  const builder = holds("build", "builder");
  const inbox = useRead<InboxTask[]>("/v1/inbox");
  const requests = useRead<Api.ApprovalRequest[]>("/v1/requests");
  const notifications = useRead<Api.Notification[]>("/v1/notifications");
  const projects = useReadQuery<Api.ResourceDirectory>("/v1/resources");
  const branches = useReadQuery<Api.BranchDetail[]>("/v1/branches");
  const apps = applications?.apps ?? [];
  const pinned = apps.filter(app => favorites.includes(app.id));
  const business = apps.filter(app => app.category === "business");
  const tools = apps.filter(app => app.category === "build");
  const hour = new Date().getHours();
  const name = me.account?.displayName || me.principalId;
  const greeting = hour < 12 ? t("Good morning, {name}", { name }) : hour < 18 ? t("Good afternoon, {name}", { name }) : t("Good evening, {name}", { name });
  const liveProjects = (projects.data?.resources ?? []).filter(project => project.ref.kind === "app" && project.state !== "archived")
    .sort((a, b) => (b.changed?.at ?? "").localeCompare(a.changed?.at ?? "")).slice(0, 8);
  const proposals = (branches.data ?? []).filter(branch => branch.state === "proposed" || branch.state === "open" && branch.changes > 0)
    .sort((a, b) => (b.changed?.at ?? "").localeCompare(a.changed?.at ?? "")).slice(0, 6);

  return <VStack gap={5} maxWidth={1152}>
    <HStack gap={3} wrap="wrap" vAlign="end" hAlign="between">
      <VStack gap={1}>
        <Heading level={1}>{greeting}</Heading>
        <Text type="supporting">{me.tenant?.name || me.tenantId}</Text>
      </VStack>
      <HStack gap={2}>
        <Button width={320} onClick={palette}><Icon icon={Search} />{t("Search records, apps and commands")}</Button>
        <Kbd keys="mod+k" />
      </HStack>
    </HStack>

    <Grid columns={{ minWidth: 180, max: 4 }} gap={2}>
      <Stat icon={<Icon icon={Inbox} />} label={t("Tasks waiting for you")} value={inbox?.length} onClick={() => open({ view: "inbox" })} />
      <Stat icon={<Icon icon={ClipboardList} />} label={t("Requests in progress")} value={requests?.filter(request => request.state === "pending").length} onClick={() => open({ view: "requests" })} />
      <Stat icon={<Icon icon={Bell} />} label={t("Unread notifications")} value={notifications?.filter(notification => !notification.read).length} onClick={() => open({ view: "notifications" })} />
      {builder || branches.data?.some(branch => branch.state === "proposed") ? <Stat icon={<Icon icon={GitBranch} />} label={t("Open proposals")} value={branches.data?.filter(branch => branch.state === "proposed").length} onClick={() => open({ view: "changes" })} />
        : <Stat icon={<Icon icon={FolderKanban} />} label={t("Applications")} value={apps.length} onClick={() => open({ view: "portal" })} />}
    </Grid>

    <Grid columns={{ minWidth: 400, max: 2 }} gap={5}>
      <VStack gap={5}>
        {builder && <Panel title={t("Projects")} actions={<Button size="sm" variant="ghost" onClick={() => open({ view: "files" })}>{t("All projects")}<Icon icon={ArrowRight} /></Button>}>
          {projects.isError && <Text as="p" type="supporting" role="alert">{t("Projects could not be loaded.")}</Text>}
          {!projects.isLoading && !liveProjects.length && <EmptyState title={t("No projects yet")} hint={t("A project holds the object types, logic, pages and releases of one application.")}
            action={<Button variant="primary" onClick={() => open({ view: "files" })}>{t("Create a project")}</Button>} />}
          <List density="compact">{liveProjects.map(project => <Row key={project.id} icon={<Icon icon={FolderKanban} />} title={project.title || project.ref.name}
            meta={<HStack gap={2}>{project.state && project.state !== "published" && <Tag label={t("Draft")} />}{when(project.changed?.at)}</HStack>}
            onClick={() => open({ view: "project", params: { id: project.id } })} />)}</List>
        </Panel>}
        {!builder && business.length > 0 && <Panel title={t("Business applications")} actions={<Button size="sm" variant="ghost" onClick={() => open({ view: "portal" })}>{t("All applications")}<Icon icon={ArrowRight} /></Button>}>
          <BusinessAppTiles apps={business} resources={projects.data?.resources ?? []} />
        </Panel>}
        <Panel title={t("Recent")}>
          {!recent.length && <Text as="p" type="supporting">{t("Nothing opened yet.")}</Text>}
          <List density="compact">{recent.slice(0, 8).map(item => <Row key={routeKey(item.route)} title={item.title} detail={apps.find(app => app.id === item.app)?.title} meta={when(item.at)} onClick={() => open(item.route)} />)}</List>
        </Panel>
      </VStack>
      <VStack gap={5}>
        {(builder || proposals.length > 0) && <Panel title={t("Branches and proposals")} actions={<Button size="sm" variant="ghost" onClick={() => open({ view: "changes" })}>{t("All")}<Icon icon={ArrowRight} /></Button>}>
          {!branches.isLoading && !proposals.length && <Text as="p" type="supporting">{t("No open changes. Start a branch from any builder tool.")}</Text>}
          <List density="compact">{proposals.map(branch => <Row key={branch.id} icon={<Icon icon={GitBranch} />} title={branch.title} detail={t("{n} changes", { n: branch.changes })}
            meta={<Tag label={t(branch.state === "proposed" ? "Proposed" : "Editing")} tone={branch.state === "proposed" ? "purple" : "info"} />}
            onClick={() => open(branch.state === "proposed" ? { view: "proposal", params: { id: branch.id } } : { view: "changes" })} />)}</List>
        </Panel>}
        {pinned.length > 0 && <Panel title={t("Pinned")}><VStack gap={2}>{pinned.map(app => <AppTile key={app.id} app={app} />)}</VStack></Panel>}
        {builder && tools.length > 0 && <Panel title={t("Build tools")}><VStack gap={2}>{tools.filter(app => !favorites.includes(app.id)).map(app => <AppTile key={app.id} app={app} />)}</VStack></Panel>}
        {builder && business.length > 0 && <Panel title={t("Business applications")}><BusinessAppTiles apps={business.slice(0, 6)} resources={projects.data?.resources ?? []} /></Panel>}
      </VStack>
    </Grid>
  </VStack>;
}