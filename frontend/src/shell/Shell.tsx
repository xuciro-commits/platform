import { AppShell, useAppShellMobile } from "@astryxdesign/core/AppShell";
import { Divider } from "@astryxdesign/core/Divider";
import { EmptyState } from "@astryxdesign/core/EmptyState";
import { useHotkeys } from "@astryxdesign/core/hooks";
import { HStack } from "@astryxdesign/core/HStack";
import { ChartPieIcon, RectangleGroupIcon } from "@heroicons/react/24/outline";
import { MobileNav } from "@astryxdesign/core/MobileNav";
import { useToast } from "@astryxdesign/core/Toast";
import { Suspense, useEffect, useMemo, useState } from "react";
import { type App, apps, locate, type NavItem } from "../apps";
import { entries, recordHref } from "../apps/entries";
import { type Api, useRead, useSession } from "../host";
import { AppDashboard } from "../pages/AppDashboard";
import { Records } from "../pages/RecordList";
import { setLanguage, t } from "../i18n";
import { navigate, useHash, usePath, useSearch } from "../router";
import { AppRail } from "./AppRail";
import { setAppearance } from "./appearance";
import { AppSidebar } from "./AppSidebar";
import { CommandSearch } from "./CommandSearch";
import { legacyTarget } from "./legacy";
import { SignIn } from "./SignIn";
import { iconOf } from "../pages/icons";

function installedApps(views: Api.ApplicationView[], entities: Api.EntityInfo[]): App[] {
  return views.flatMap((view) => {
    const sections = (view.menu ?? []).flatMap((group) => {
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
    return [{ id: view.name || view.app, label: view.title, icon: iconOf(view.icon, RectangleGroupIcon), group: "installed", sections, dashboard }];
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
  const { app, item } = locate(path, available);
  const object = item.object ?? (app.id === "ontology" && item.id === "objects" ? decodeURIComponent(segments[3] ?? "") : "");
  const listPath = app.id === "ontology" && item.id === "objects" ? `/ontology/objects/${encodeURIComponent(object)}` : item.object ? `/${app.id}/${item.id}` : undefined;
  if (!session) return <SignIn />;
  if (legacy) return null;
  const Entry = entries[`${app.id}/${item.id}`];
  const retiredLink = projected.status === "ready" && segments[3] && !available.some((application) => application.id === segments[1]);
  const content = retiredLink ? <RetainedRecord key={path} path={path} />
    : Entry ? <Entry key={`${app.id}/${item.id}`} />
    : app.dashboard && item.id === "dashboard" ? <AppDashboard key={app.id} title={app.label} blocks={app.dashboard} listOf={(type) => listOf(app, type)} />
    : app.group === "installed" && item.object && listPath ? <Records key={`${app.id}/${item.id}`} declared={{ title: item.label, about: item.description ?? "", type: item.object }} listPath={listPath} id={segments[3]} />
    : null;
  return (
    <CommandSearch app={app} item={item} apps={available} tenant={session.tenant}>
      <AppShell
        contentPadding={0}
        sideNav={<Navigation app={app} item={item} apps={available} object={object || undefined} listPath={listPath} />}
        mobileNav={{ content: (
          <MobileNav header={<AppRail current={app} apps={available} compact />}>
            <AppSidebar app={app} item={item} apps={available} object={object || undefined} listPath={listPath} />
          </MobileNav>
        ) }}
      >
        <Suspense fallback={null}>{content}</Suspense>
      </AppShell>
    </CommandSearch>
  );
}

function listOf(app: App, type: string) {
  const page = app.sections.flatMap((section) => section.items).find((item) => item.object === type);
  return page && `/${app.id}/${page.id}`;
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

function Navigation({ app, item, apps, object, listPath }: { app: App; item: NavItem; apps: App[]; object?: string; listPath?: string }) {
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
      {!collapsed && <AppSidebar app={app} item={item} apps={apps} object={object} listPath={listPath} onCollapse={() => collapse(true)} />}
      {!collapsed && <Divider orientation="vertical" />}
    </HStack>
  );
}
