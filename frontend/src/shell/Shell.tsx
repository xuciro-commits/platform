import { AppShell, useAppShellMobile } from "@astryxdesign/core/AppShell";
import { Button } from "@astryxdesign/core/Button";
import { Divider } from "@astryxdesign/core/Divider";
import { EmptyState } from "@astryxdesign/core/EmptyState";
import { useHotkeys } from "@astryxdesign/core/hooks";
import { HStack, VStack } from "@astryxdesign/core/Layout";
import { ChartBarIcon, ChartPieIcon, Cog6ToothIcon, DocumentTextIcon, RectangleGroupIcon, WrenchScrewdriverIcon } from "@heroicons/react/24/outline";
import { MobileNav } from "@astryxdesign/core/MobileNav";
import { useToast } from "@astryxdesign/core/Toast";
import { lazy, Suspense, useEffect, useMemo, useState } from "react";
import { type App, apps, locate, type NavGroup, type NavItem, type NavSection, pathOf, sectionItems, settingsItemId } from "../apps";
import { entries } from "../apps/entries";
import { useRecordHome } from "../recordRoutes";
import { type Api, useRead, useSession } from "../host";
import { Records } from "../pages/RecordList";
import { setCurrency, setDateFormat, setLanguage, setNumberFormat, setTimeZone, t } from "../i18n";
import { navigate, useHash, usePath, useSearch } from "../router";
import { AppRail } from "./AppRail";
import { setAppearance } from "./appearance";
import { AppSidebar } from "./AppSidebar";
import { CallDock } from "../pages/CallDock";
import { CommandSearch } from "./CommandSearch";
import { ErrorBoundary } from "./ErrorBoundary";
import { legacyTarget } from "./legacy";
import { LandingPage } from "./LandingPage";
import { SignIn } from "./SignIn";
import { SignUpPage } from "./SignUpPage";
import { WorkspaceEntry } from "./WorkspaceEntry";
import { iconOf } from "../pages/icons";
import { motionStyles } from "../pages/motion";

const AppDashboard = lazy(() => import("../pages/AppDashboard").then((module) => ({ default: module.AppDashboard })));
const SettingsTables = lazy(() => import("../pages/SettingsTables").then((module) => ({ default: module.SettingsTables })));
const NotePage = lazy(() => import("../pages/NotePage").then((module) => ({ default: module.NotePage })));

function menuEntries(entries: Api.ApplicationPage[] | undefined, entities: Api.EntityInfo[]): NavItem[] {
  return (entries ?? []).flatMap((page): NavItem[] => {
    if (page.kind === "note") {
      return [{ id: page.name, label: page.title, icon: DocumentTextIcon, kind: "note", body: page.body }];
    }
    if (page.kind === "dashboard") {
      return [{ id: page.name || "dashboard", label: page.title || "Dashboard", icon: ChartPieIcon, kind: "dashboard" }];
    }
    if (page.kind === "report" && page.object) {
      return [{ id: page.name, label: page.title, icon: ChartBarIcon, kind: "report", object: page.object, report: page.report }];
    }
    if (!page.object) return [];
    return [{
      id: page.name,
      label: page.title,
      icon: iconOf(entities.find((entity) => entity.type === page.object)?.icon, RectangleGroupIcon),
      kind: "page",
      object: page.object,
      description: page.description,
    }];
  });
}

function installedApps(views: Api.ApplicationView[], entities: Api.EntityInfo[]): App[] {
  return views.flatMap((view) => {
    const sections = (view.menu ?? []).flatMap((group, index): NavSection[] => {
      if (group.layout === "settings") {
        const types = (group.entries ?? []).flatMap((page) => (page.object ? [page.object] : []));
        return types.length ? [{ title: "", items: [{ id: settingsItemId(group.title), label: group.title || "Settings", icon: Cog6ToothIcon, settings: types }] }] : [];
      }
      const items = menuEntries(group.entries, entities);
      const groups = (group.groups ?? []).flatMap((sub, subIndex): NavGroup[] => {
        const subItems = menuEntries(sub.entries, entities);
        if (subItems.length === 0) return [];
        return [{ id: sub.id || `${group.id || index}-${subIndex}-${sub.title || "group"}`, title: sub.title || "Pages", collapsed: sub.collapsed, items: subItems }];
      });
      return items.length || groups.length
        ? [{ id: group.id || `${index}-${group.title || "section"}`, title: group.title || "Pages", collapsed: group.collapsed, items, groups: groups.length ? groups : undefined }]
        : [];
    });
    if (!sections.length) return [];
    const seenReports = new Set(sections.flatMap((section) => sectionItems(section)).filter((entry) => entry.kind === "report").map((entry) => `${entry.object}:${entry.report}`));
    const appObjects = Array.from(new Set(sections.flatMap((section) => sectionItems(section)).flatMap((entry) => entry.object ? [entry.object] : [])));
    const declaredReports: NavItem[] = appObjects.flatMap((objectType) => {
      const entity = entities.find((candidate) => candidate.type === objectType);
      const reports = (entity?.view as { reports?: { name: string; title?: string }[] } | undefined)?.reports ?? [];
      return reports.flatMap((rep): NavItem[] => {
        if (!rep.name || seenReports.has(`${objectType}:${rep.name}`)) return [];
        seenReports.add(`${objectType}:${rep.name}`);
        return [{ id: `${objectType}--${rep.name}`, label: rep.title || rep.name, icon: ChartBarIcon, kind: "report", object: objectType, report: rep.name }];
      });
    });
    if (declaredReports.length > 0) sections.push({ id: "reports", title: "Reports", items: declaredReports });
    const allItems = sections.flatMap((section) => sectionItems(section));
    const hasDashboardEntry = allItems.some((page) => page.id === "dashboard" || page.kind === "dashboard");
    const dashboard = view.dashboard?.length ? view.dashboard : undefined;
    if (dashboard && !hasDashboardEntry) {
      sections[0].items = [{ id: "dashboard", label: "Dashboard", icon: ChartPieIcon, kind: "dashboard" }, ...sections[0].items];
    }
    const id = view.name || view.app;
    const builtinIcon = id === "build" ? WrenchScrewdriverIcon : undefined;
    return [{ id, label: view.title, icon: view.icon ? iconOf(view.icon, RectangleGroupIcon) : (builtinIcon ?? RectangleGroupIcon), badge: !view.icon && !builtinIcon, group: "installed", sections, dashboard, packageId: view.package, hasDemo: view.hasDemo, surface: view.surface }];
  });
}

export function Shell() {
  const session = useSession();
  const path = usePath();
  const hash = useHash();
  const recordHref = useRecordHome();
  const legacy = useMemo(() => legacyTarget(hash, recordHref), [hash, recordHref]);
  const toast = useToast();
  const segments = path.split("/");
  const hasTenant = Boolean(session?.tenant);
  const signInOptions = useRead<Api.SignIn>(!session && path === "/" ? "/v1/sign-in" : null);
  const projected = useRead<Api.ApplicationView[]>(hasTenant ? "/v1/applications" : null);
  const me = useRead<Api.MeView>(hasTenant ? "/v1/me" : null);
  const entities = useRead<Api.EntityInfo[]>(hasTenant ? "/v1/entities" : null);
  useEffect(() => {
    if (me.status !== "ready") return;
    setLanguage(me.data.language);
    setTimeZone(me.data.account.effective.timezone);
    setDateFormat(me.data.account.effective.dateFormat);
    setNumberFormat(me.data.account.effective.numberFormat || me.data.tenant?.settings?.numberFormat || "1,234.56");
    setCurrency(me.data.currency || me.data.tenant?.settings?.currency || "EUR");
    setAppearance(me.data.account.theme);
  }, [me]);
  const signedIn = hasTenant && me.status !== "loading";
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
  }, [signedIn, legacy, hash, resource, resourcePath, toast, recordHref]);
  const available = useMemo(() => {
    if (me.status !== "ready") return apps;
    const admin = me.data.apps.some((entry) => entry.id === "platform");
    const host = Boolean(me.data.hostAdmin);
    const builder = admin || host || me.data.apps.some((entry) => entry.id === "build" && entry.role !== "user");
    const ai = builder || me.data.apps.some((entry) => entry.id === "ai" || entry.id === "agent");
    const operations = admin || host || me.data.apps.some((entry) => entry.id === "flow" || entry.id === "data");
    const granted: Record<string, boolean> = {
      home: true,
      data: builder,
      ontology: builder,
      logic: builder,
      apps: builder,
      ai,
      delivery: builder,
      operations,
      admin,
      host,
    };
    const fixed = apps.filter((known) => granted[known.id]).map((known) =>
      known.id === "home" && !builder
        ? { ...known, sections: known.sections.map((section) => ({ ...section, items: section.items.filter((entry) => entry.id === "overview" || entry.id === "inbox") })) }
        : known,
    );
    return projected.status === "ready" ? [...fixed, ...installedApps(projected.data, entities.status === "ready" ? entities.data : []).filter((app) => !apps.some((known) => known.id === app.id))] : fixed;
  }, [projected, me, entities]);
  const directObject = segments[1] === "ontology" && segments[2] === "objects" && Boolean(segments[3]);
  const { app, item, found } = locate(path, directObject ? apps : available);
  const navApp = available.some((candidate) => candidate.id === app.id) ? app : available[0];
  const navItem = navApp === app && found ? item : undefined;
  const object = item.object ?? (app.id === "ontology" && item.id === "objects" ? decodeURIComponent(segments[3] ?? "") : "");
  const listPath = app.id === "ontology" && item.id === "objects" ? `/ontology/objects/${encodeURIComponent(object)}` : item.object ? `/${app.id}/${item.id}` : undefined;
  if (path === "/welcome") return <LandingPage />;
  if (!session) {
    if (path === "/signup") return <SignUpPage />;
    if (path === "/sign-in") return <SignIn />;
    if (path === "/" && signInOptions.status === "ready" && !signInOptions.data.issuer && (signInOptions.data.identities?.length ?? 0) === 0) {
      return <LandingPage />;
    }
    return <SignIn />;
  }
  if (!session.tenant) return <WorkspaceEntry />;
  if (legacy) return null;
  const Entry = entries[`${app.id}/${item.id}`];
  const retiredLink = projected.status === "ready" && segments[3] && !directObject && !available.some((application) => application.id === segments[1]);
  const missing = !found && projected.status === "ready" && me.status === "ready";
  const content = retiredLink ? <RetainedRecord key={path} path={path} />
    : missing ? <Missing home={pathOf(navApp)} label={t(navApp.label)} />
    : Entry ? <Entry key={`${app.id}/${item.id}`} />
    : item.kind === "note" ? <NotePage key={`${app.id}/${item.id}`} title={item.label} body={item.body ?? ""} />
    : app.dashboard && (item.id === "dashboard" || item.kind === "dashboard") ? <AppDashboard key={app.id} title={item.label} blocks={app.dashboard} listOf={(type) => listOf(app, type)} />
    : app.group === "installed" && item.settings ? <SettingsTables key={`${app.id}/${item.id}`} title={item.label} types={item.settings} />
    : app.group === "installed" && item.object && listPath ? <Records key={`${app.id}/${item.id}`} declared={{ title: item.label, about: item.description ?? "", type: item.object }} listPath={listPath} id={segments[3]} report={item.report} />
    : null;
  if (segments[segments.length - 1] === "print") {
    return (
      <ErrorBoundary resetKey={path}>
        <Suspense fallback={null}>{content}</Suspense>
      </ErrorBoundary>
    );
  }
  return (
    <CommandSearch app={navApp} item={navItem ?? sectionItems(navApp.sections[0])[0]} apps={available} tenant={session.tenant}>
      <AppShell
        contentPadding={0}
        sideNav={<Navigation app={navApp} item={navItem} apps={available} object={object || undefined} listPath={listPath} />}
        mobileNav={{ content: (
          <MobileNav header={<AppRail current={navApp} apps={available} compact />}>
            <AppSidebar app={navApp} item={navItem} apps={available} object={object || undefined} listPath={listPath} inDrawer />
          </MobileNav>
        ) }}
      >
        <ErrorBoundary resetKey={path}>
          <ErrorBoundary resetKey="calls"><CallDock /></ErrorBoundary>
          <VStack key={`${app.id}/${item.id}`} xstyle={[motionStyles.pageSlot, motionStyles.contentEnter]}>
            <Suspense fallback={null}>{content}</Suspense>
          </VStack>
        </ErrorBoundary>
      </AppShell>
    </CommandSearch>
  );
}

function listOf(app: App, type: string) {
  const page = app.sections.flatMap((section) => sectionItems(section)).find((item) => item.object === type && item.kind !== "report");
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
