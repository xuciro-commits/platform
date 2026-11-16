import { AppShell } from "@astryxdesign/core/AppShell";
import { HStack } from "@astryxdesign/core/HStack";
import { Suspense } from "react";
import { locate } from "../apps";
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
    <AppShell
      contentPadding={0}
      sideNav={
        <HStack height="100%">
          <AppRail current={app} />
          <AppSidebar app={app} item={item} />
        </HStack>
      }>
      <Suspense fallback={null}>{Entry ? <Entry key={`${app.id}/${item.id}`} /> : <Unbuilt item={item} />}</Suspense>
    </AppShell>
  );
}
