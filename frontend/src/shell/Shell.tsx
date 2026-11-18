import { AppShell, useAppShellMobile } from "@astryxdesign/core/AppShell";
import { HStack } from "@astryxdesign/core/HStack";
import { RectangleGroupIcon } from "@heroicons/react/24/outline";
import { MobileNav } from "@astryxdesign/core/MobileNav";
import { Suspense, useMemo } from "react";
import { type App, apps, locate, type NavItem } from "../apps";
import { entries } from "../apps/entries";
import { type Api, useRead, useSession } from "../host";
import { Records } from "../pages/RecordList";
import { usePath } from "../router";
import { AppRail } from "./AppRail";
import { AppSidebar } from "./AppSidebar";
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
  const projected = useRead<Api.ApplicationView[]>(session ? "/v1/applications" : null);
  const available = useMemo(() => projected.status === "ready" ? [...apps, ...installedApps(projected.data).filter((app) => !apps.some((known) => known.id === app.id))] : apps, [projected]);
  const { app, item } = locate(usePath(), available);
  if (!session) return <SignIn />;
  const Entry = entries[`${app.id}/${item.id}`];
  const content = Entry ? <Entry key={`${app.id}/${item.id}`} />
    : app.installed && item.object ? <Records key={`${app.id}/${item.id}`} declared={{ title: item.label, about: item.description ?? "", type: item.object }} listPath={`/${app.id}/${item.id}`} />
    : <Unbuilt item={item} />;
  return (
    <AppShell
      contentPadding={0}
      sideNav={<Navigation app={app} item={item} apps={available} />}
      mobileNav={{ content: (
        <MobileNav header={<AppRail current={app} apps={available} compact />}>
          <AppSidebar app={app} item={item} apps={available} />
        </MobileNav>
      ) }}
    >
      <Suspense fallback={null}>{content}</Suspense>
    </AppShell>
  );
}

function Navigation({ app, item, apps }: { app: App; item: NavItem; apps: App[] }) {
  const { isMobile } = useAppShellMobile();
  if (isMobile) return <AppSidebar app={app} item={item} apps={apps} />;
  return (
    <HStack height="100%">
      <AppRail current={app} apps={apps} />
      <AppSidebar app={app} item={item} apps={apps} />
    </HStack>
  );
}
