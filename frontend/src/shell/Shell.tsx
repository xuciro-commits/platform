import { AppShell } from "@astryxdesign/core/AppShell";
import { HStack } from "@astryxdesign/core/HStack";
import { type ComponentType, type LazyExoticComponent, Suspense, lazy } from "react";
import { locate } from "../apps";
import { ListPage } from "../pages/ListPage";
import { usePath } from "../router";
import { AppRail } from "./AppRail";
import { AppSidebar } from "./AppSidebar";

const samples: Record<string, LazyExoticComponent<ComponentType>> = {
  "sample-table": lazy(() => import("../templates/table-filter/page")),
  "sample-board": lazy(() => import("../templates/kanban-board/page")),
};

export function Shell() {
  const { app, item } = locate(usePath());
  const Sample = samples[item.id];
  return (
    <AppShell
      contentPadding={0}
      sideNav={
        <HStack height="100%">
          <AppRail current={app} />
          <AppSidebar app={app} item={item} />
        </HStack>
      }>
      {Sample ? (
        <Suspense fallback={null}>
          <Sample />
        </Suspense>
      ) : (
        <ListPage key={`${app.id}/${item.id}`} item={item} />
      )}
    </AppShell>
  );
}
