import { AppShell, useAppShellMobile } from "@astryxdesign/core/AppShell";
import { EmptyState } from "@astryxdesign/core/EmptyState";
import { HStack } from "@astryxdesign/core/HStack";
import { RectangleGroupIcon } from "@heroicons/react/24/outline";
import { MobileNav } from "@astryxdesign/core/MobileNav";
import { Suspense, useEffect, useMemo } from "react";
import { type App, apps, locate, type NavItem } from "../apps";
import { entries } from "../apps/entries";
import { type Api, useRead, useSession } from "../host";
import { Records } from "../pages/RecordList";
import { setLanguage, t } from "../i18n";
import { navigate, usePath, useSearch } from "../router";
import { AppRail } from "./AppRail";
import { setAppearance } from "./appearance";
import { AppSidebar } from "./AppSidebar";
import { CommandSearch } from "./CommandSearch";
import { SignIn } from "./SignIn";
import { Unbuilt } from "./Unbuilt";

function installedApps(views: Api.ApplicationView[]): App[] {
  return views.flatMap((view) => {
    const sections = (view.menu ?? []).flatMap((group) => {
      const items = group.entries.flatMap((page) => page.object ? [{
        id: page.name,
        label: page.title,
        icon: RectangleGroupIcon,
        object: page.object,
        description: page.description,
      }] : []);
      return items.length ? [{ title: group.title || "Pages", items }] : [];
    });
    if (!sections.length) return [];
    return [{ id: view.name || view.app, label: view.title, icon: RectangleGroupIcon, sections, installed: true }];
  });
}

export function Shell() {
  const session = useSession();
  const path = usePath();
  const segments = path.split("/");
  const projected = useRead<Api.ApplicationView[]>(session ? "/v1/applications" : null);
  const me = useRead<Api.MeView>(session ? "/v1/me" : null);
  useEffect(() => {
    if (me.status !== "ready") return;
    setLanguage(me.data.language);
    setAppearance(me.data.account.theme);
  }, [me]);
  const available = useMemo(() => projected.status === "ready" ? [...apps, ...installedApps(projected.data).filter((app) => !apps.some((known) => known.id === app.id))] : apps, [projected]);
  const { app, item } = locate(path, available);
  const object = item.object ?? (app.id === "ontology" && item.id === "objects" ? decodeURIComponent(segments[3] ?? "") : "");
  const listPath = app.id === "ontology" && item.id === "objects" ? `/ontology/objects/${encodeURIComponent(object)}` : item.object ? `/${app.id}/${item.id}` : undefined;
  if (!session) return <SignIn />;
  const Entry = entries[`${app.id}/${item.id}`];
  const retiredLink = projected.status === "ready" && segments[3] && !available.some((application) => application.id === segments[1]);
  const content = retiredLink ? <RetainedRecord key={path} path={path} />
    : Entry ? <Entry key={`${app.id}/${item.id}`} />
    : app.installed && item.object && listPath ? <Records key={`${app.id}/${item.id}`} declared={{ title: item.label, about: item.description ?? "", type: item.object }} listPath={listPath} id={segments[3]} />
    : <Unbuilt item={item} />;
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

function Navigation({ app, item, apps, object, listPath }: { app: App; item: NavItem; apps: App[]; object?: string; listPath?: string }) {
  const { isMobile } = useAppShellMobile();
  if (isMobile) return <AppSidebar app={app} item={item} apps={apps} object={object} listPath={listPath} />;
  return (
    <HStack height="100%">
      <AppRail current={app} apps={apps} />
      <AppSidebar app={app} item={item} apps={apps} object={object} listPath={listPath} />
    </HStack>
  );
}
