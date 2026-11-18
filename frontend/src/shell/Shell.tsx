import { AppShell, useAppShellMobile } from "@astryxdesign/core/AppShell";
import { HStack } from "@astryxdesign/core/HStack";
import { Suspense } from "react";
import { type App, locate, type NavItem } from "../apps";
import { entries } from "../apps/entries";
import { useSession } from "../host";
import { usePath } from "../router";
import { AppRail } from "./AppRail";
import { AppSidebar } from "./AppSidebar";
import { SignIn } from "./SignIn";
import { Unbuilt } from "./Unbuilt";

export function Shell() {
  const session = useSession();
  const { app, item } = locate(usePath());
  if (!session) return <SignIn />;
  const Entry = entries[`${app.id}/${item.id}`];
  return (
    <AppShell contentPadding={0} sideNav={<Navigation app={app} item={item} />}>
      <Suspense fallback={null}>{Entry ? <Entry key={`${app.id}/${item.id}`} /> : <Unbuilt item={item} />}</Suspense>
    </AppShell>
  );
}

function Navigation({ app, item }: { app: App; item: NavItem }) {
  const { isMobile } = useAppShellMobile();
  if (isMobile) return <AppSidebar app={app} item={item} />;
  return (
    <HStack height="100%">
      <AppRail current={app} />
      <AppSidebar app={app} item={item} />
    </HStack>
  );
}
