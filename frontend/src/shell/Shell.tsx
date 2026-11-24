import { AppShell, useAppShellMobile } from "@astryxdesign/core/AppShell";
import { Button } from "@astryxdesign/core/Button";
import { Divider } from "@astryxdesign/core/Divider";
import { EmptyState } from "@astryxdesign/core/EmptyState";
import { useHotkeys } from "@astryxdesign/core/hooks";
import { HStack, VStack } from "@astryxdesign/core/Layout";
import { ChartPieIcon, Cog6ToothIcon, RectangleGroupIcon, WrenchScrewdriverIcon } from "@heroicons/react/24/outline";
import { MobileNav } from "@astryxdesign/core/MobileNav";
import { useToast } from "@astryxdesign/core/Toast";
import { lazy, Suspense, useEffect, useMemo, useState } from "react";
import { type App, apps, locate, type NavItem, type NavSection, pathOf, settingsItemId } from "../apps";
import { entries, recordHref } from "../apps/entries";
import { type Api, useRead, useSession } from "../host";
import { Records } from "../pages/RecordList";
import { setDateFormat, setLanguage, setTimeZone, t } from "../i18n";
import { navigate, useHash, usePath, useSearch } from "../router";
import { AppRail } from "./AppRail";
import { setAppearance } from "./appearance";
import { AppSidebar } from "./AppSidebar";
import { CommandSearch } from "./CommandSearch";
import { ErrorBoundary } from "./ErrorBoundary";
import { legacyTarget } from "./legacy";
import { SignIn } from "./SignIn";
import { iconOf } from "../pages/icons";
import { motionStyles } from "../pages/motion";

const AppDashboard = lazy(() => import("../pages/AppDashboard").then((module) => ({ default: module.AppDashboard })));
const SettingsTables = lazy(() => import("../pages/SettingsTables").then((module) => ({ default: module.SettingsTables })));

function installedApps(views: Api.ApplicationView[], entities: Api.EntityInfo[]): App[] {
  return views.flatMap((view) => {
    const sections = (view.menu ?? []).flatMap((group): NavSection[] => {
      if (group.layout === "settings") {
        const types = group.entries.flatMap((page) => (page.object ? [page.object] : []));
        return types.length ? [{ title: "", items: [{ id: settingsItemId(group.title), label: group.title || "Settings", icon: Cog6ToothIcon, settings: types }] }] : [];
      }
      const items = group.entries.flatMap((page): NavItem[] => page.object ? [{
        id: page.name,
        label: page.title,
        icon: iconOf(entities.find((entity) => entity.type === page.object)?.icon, RectangleGroupIcon),
        object: page.object,
        description: page.description,
      }] : []);
      return items.length ? [{ title: group.title || "Pages", items }] : [];
    });
    if (!sections.length) return [];
    const dashboard = view.dashboard?.length && !sections.some((section) => section.items.some((page) => page.id === "dashboard")) ? view.dashboard : undefined;
    const overview: NavItem[] = dashboard ? [{ id: "dashboard", label: "Dashboard", icon: ChartPieIcon }] : [];
    sections[0].items = [...overview, ...sections[0].items];
    const id = view.name || view.app;
    const builtinIcon = id === "build" ? WrenchScrewdriverIcon : undefined;
    return [{ id, label: view.title, icon: view.icon ? iconOf(view.icon, RectangleGroupIcon) : (builtinIcon ?? RectangleGroupIcon), badge: !view.icon && !builtinIcon, group: "installed", sections, dashboard, packageId: view.package, hasDemo: view.hasDemo, surface: view.surface }];
  });
}

export function Shell() {
  const session = useSession();
  const path = usePath();
  const hash = useHash();
  const legacy = useMemo(() => legacyTarget(hash), [hash]);
  const toast = useToast();
  const segments = path.split("/");
  const projected = useRead<Api.ApplicationView[]>(session ? "/v1/applications" : null);
  const me = useRead<Api.MeView>(session ? "/v1/me" : null);
  const entities = useRead<Api.EntityInfo[]>(session ? "/v1/entities" : null);
  useEffect(() => {
    if (me.status !== "ready") return;
    setLanguage(me.data.language);
    setTimeZone(me.data.account.effective.timezone);
    setDateFormat(me.data.account.effective.dateFormat);
    setAppearance(me.data.account.theme);
  }, [me]);
  const signedIn = session && me.status !== "loading";
  const resourcePath = signedIn && legacy && legacy !== "unmatched" && "rid" in legacy
    ? `/v1/resources/${encodeURIComponent(legacy.rid)}`
    : null;
  const resource = useRead<Api.ResourceEntry>(resourcePath);
  useEffect(() => {
    if (!signedIn || !legacy || location.hash !== hash) return;
    let destination: string | undefined;
    if (legacy !== "unmatched") {
      if ("path" in legacy) {
        destination = legacy.path;
      } else {
        if (resource.path !== resourcePath || resource.status === "loading") return;
        if (resource.status === "ready" && resource.data.type && resource.data.draft) {
          destination = recordHref(resource.data.type, resource.data.draft) + legacy.tab;
        }
      }
    }
    void navigate(destination ?? "/home/overview", null, true);
    if (!destination) toast({ body: t("This legacy link has no corresponding page in the new frontend.") });
  }, [signedIn, legacy, hash, resource, resourcePath, toast]);
  const available = useMemo(() => {
    const granted: Record<string, boolean> = me.status === "ready" ? { admin: me.data.apps.some((entry) => entry.id === "platform"), host: Boolean(me.data.hostAdmin) } : {};
    const fixed = apps.filter((known) => me.status !== "ready" || known.group !== "manage" || granted[known.id]);
    return projected.status === "ready" ? [...fixed, ...installedApps(projected.data, entities.status === "ready" ? entities.data : []).filter((app) => !apps.some((known) => known.id === app.id))] : fixed;
  }, [projected, me, entities]);
  const { app, item, found } = locate(path, available);
  const object = item.object ?? (app.id === "ontology" && item.id === "objects" ? decodeURIComponent(segments[3] ?? "") : "");
  const listPath = app.id === "ontology" && item.id === "objects" ? `/ontology/objects/${encodeURIComponent(object)}` : item.object ? `/${app.id}/${item.id}` : undefined;
  if (!session) return <SignIn />;
  if (legacy) return null;
  const Entry = entries[`${app.id}/${item.id}`];
  const retiredLink = projected.status === "ready" && segments[3] && !available.some((application) => application.id === segments[1]);
  const missing = !found && projected.status === "ready" && me.status === "ready";
  const content = retiredLink ? <RetainedRecord key={path} path={path} />
    : missing ? <Missing home={pathOf(app)} label={t(app.label)} />
    : Entry ? <Entry key={`${app.id}/${item.id}`} />
    : app.dashboard && item.id === "dashboard" ? <AppDashboard key={app.id} title={item.label} blocks={app.dashboard} listOf={(type) => listOf(app, type)} />
    : app.group === "installed" && item.settings ? <SettingsTables key={`${app.id}/${item.id}`} title={item.label} types={item.settings} />
    : app.group === "installed" && item.object && listPath ? <Records key={`${app.id}/${item.id}`} declared={{ title: item.label, about: item.description ?? "", type: item.object }} listPath={listPath} id={segments[3]} />
    : null;
  return (
    <CommandSearch app={app} item={item} apps={available} tenant={session.tenant}>
      <AppShell
        contentPadding={0}
        sideNav={<Navigation app={app} item={found ? item : undefined} apps={available} object={object || undefined} listPath={listPath} />}
        mobileNav={{ content: (
          <MobileNav header={<AppRail current={app} apps={available} compact />}>
            <AppSidebar app={app} item={found ? item : undefined} apps={available} object={object || undefined} listPath={listPath} inDrawer />
          </MobileNav>
        ) }}
      >
        <ErrorBoundary resetKey={path}>
          <VStack key={`${app.id}/${item.id}`} xstyle={[motionStyles.pageSlot, motionStyles.contentEnter]}>
            <Suspense fallback={null}>{content}</Suspense>
          </VStack>
        </ErrorBoundary>
      </AppShell>
    </CommandSearch>
  );
}

function listOf(app: App, type: string) {
  const page = app.sections.flatMap((section) => section.items).find((item) => item.object === type);
  return page && `/${app.id}/${page.id}`;
}

function Missing({ home, label }: { home: string; label: string }) {
  return (
    <EmptyState
      headingLevel={1}
      title={t("This page does not exist")}
      description={t("The address may be mistyped, or the page was moved or removed.")}
      actions={<Button label={t("Open {app}").replace("{app}", label)} variant="primary" href={home} />}
    />
  );
}

function RetainedRecord({ path }: { path: string }) {
  const [, app, page, id, tab] = path.split("/");
  const search = useSearch();
  const link = useRead<Api.RetainedRecordLink>(`/v1/applications/${app}/${page}/records/${id}`);
  useEffect(() => {
    if (link.status === "ready") {
      void navigate(`/ontology/objects/${encodeURIComponent(link.data.object)}/${id}${tab ? `/${tab}` : ""}${search}`, null, true);
    }
  }, [id, tab, search, link]);
  return (link.status === "refused" || link.status === "error")
    ? <EmptyState headingLevel={2} title={t("You cannot see this record")} description={link.reason} />
    : null;
}

const collapsedKey = "platform:sidebar-collapsed";

function Navigation({ app, item, apps, object, listPath }: { app: App; item?: NavItem; apps: App[]; object?: string; listPath?: string }) {
  const { isMobile } = useAppShellMobile();
  const [collapsed, setCollapsed] = useState(() => localStorage.getItem(collapsedKey) === "true");
  const collapse = (next: boolean) => {
    localStorage.setItem(collapsedKey, String(next));
    setCollapsed(next);
  };
  useHotkeys([
    ...["meta+b", "ctrl+b"].map((keys) => ({ keys, onPress: () => collapse(!collapsed), allowInInputs: true, isDisabled: isMobile })),
    { keys: "[", onPress: () => collapse(!collapsed), isDisabled: isMobile },
  ]);
  if (isMobile) return <AppSidebar app={app} item={item} apps={apps} object={object} listPath={listPath} />;
  return (
    <HStack height="100%">
      <AppRail current={app} apps={apps} collapsed={collapsed} onExpand={() => collapse(false)} />
      <Divider orientation="vertical" />
      <HStack
        ref={(node) => { if (node) node.inert = collapsed; }}
        xstyle={[motionStyles.sidebarShell, collapsed && motionStyles.sidebarCollapsed]}
        aria-hidden={collapsed || undefined}
      >
        <HStack xstyle={motionStyles.sidebarInner}>
          <HStack xstyle={motionStyles.sidebarContent}>
            <AppSidebar app={app} item={item} apps={apps} object={object} listPath={listPath} onCollapse={() => collapse(true)} />
            <Divider orientation="vertical" />
          </HStack>
        </HStack>
      </HStack>
    </HStack>
  );
}
