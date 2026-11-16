import { Divider } from "@astryxdesign/core/Divider";
import { MoreMenu } from "@astryxdesign/core/MoreMenu";
import { NavIcon } from "@astryxdesign/core/NavIcon";
import { SideNav, SideNavHeading, SideNavItem, SideNavSection } from "@astryxdesign/core/SideNav";
import { Stack } from "@astryxdesign/core/Stack";
import { Bell, Boxes, CircleHelp, Clock, FolderTree, Grid2x2, Home as HomeIcon, Search, Sparkles, Star } from "lucide-react";
import { useState, type ReactNode } from "react";
import { t } from "../i18n";
import { routeKey, type Route } from "./route";
import type { Memory, PlatformApplication, Rail } from "./WorkspaceContext";

export type AppRailProps = {
  rail: Rail;
  product: string;
  productIcon?: ReactNode;
  applications?: { apps: PlatformApplication[]; onSelect: (id: string) => void };
  memory: Memory;
  activeKey?: string;
  onOpen: (route: Route) => void;
  onSearch: () => void;
  onAssist?: () => void;
  onToggleFavorite: (id: string) => void;
  sessionMenu?: ReactNode;
};

function FavoriteItem({ app, pinned, onOpen, onToggleFavorite }: { app: PlatformApplication; pinned: boolean; onOpen: () => void; onToggleFavorite: () => void }) {
  const [hovered, setHovered] = useState(false);
  const [menuOpen, setMenuOpen] = useState(false);
  return <Stack onMouseEnter={() => setHovered(true)} onMouseLeave={() => setHovered(false)}>
    <SideNavItem label={app.title} icon={app.icon} onClick={onOpen}
      actions={(hovered || menuOpen) ? <MoreMenu size="sm" label={t("Application options")} onOpenChange={setMenuOpen}
        items={[{ label: pinned ? t("Remove from favorites") : t("Add to favorites"), onClick: onToggleFavorite }]} /> : undefined} />
  </Stack>;
}

export function AppRail({ rail, product, productIcon, applications, memory, activeKey, onOpen, onSearch, onAssist, onToggleFavorite, sessionMenu }: AppRailProps) {
  const at = (route?: Route) => !!route && routeKey(route) === activeKey;
  const recent = memory.recent.slice(0, 12);
  const pinned = (applications?.apps ?? []).filter((app) => memory.favorites.includes(app.id));
  const brand = productIcon ?? <Boxes />;
  const unread = rail.notifications?.unread;

  return <SideNav
    aria-label={t("Platform")}
    collapsible={{ defaultIsCollapsed: false, buttonLabel: t("Collapse navigation") }}
    resizable={{ defaultWidth: 280, minWidth: 224, maxWidth: 360, autoSaveId: "platform:app-rail" }}
    header={<SideNavHeading heading={product} icon={<NavIcon icon={brand} />} headingHref="#/" />}
    footer={
      <SideNavSection title={t("Account")} isHeaderHidden>
        {rail.notifications && <SideNavItem label={t("Notifications")} aria-label={t("Notifications")} icon={Bell} isSelected={at(rail.notifications.route)} onClick={() => onOpen(rail.notifications!.route)}
          endContent={unread ? <span className="rounded-full bg-primary px-1.5 text-2xs font-semibold leading-4 text-primary-foreground">{unread > 99 ? "99+" : unread}</span> : undefined} />}
        {rail.assist && <SideNavItem label={t("AIP Assist")} icon={Sparkles} onClick={() => onAssist ? onAssist() : onOpen(rail.assist!)} />}
        {rail.help && <SideNavItem label={t("Help")} icon={CircleHelp} isSelected={at(rail.help)} onClick={() => onOpen(rail.help!)} />}
        {sessionMenu && <div className="pt-2">{sessionMenu}</div>}
      </SideNavSection>
    }>
    <SideNavSection title={t("Menu")} isHeaderHidden>
      <SideNavItem label={t("Home")} icon={HomeIcon} isSelected={at(rail.home)} onClick={() => onOpen(rail.home)} />
      <SideNavItem label={t("Search")} icon={Search} onClick={onSearch} />
      {rail.files && <SideNavItem label={t("Files")} icon={FolderTree} isSelected={at(rail.files)} onClick={() => onOpen(rail.files!)} />}
      <SideNavItem label={t("Applications")} icon={Grid2x2} isSelected={at(rail.applications)} onClick={() => onOpen(rail.applications)} />
    </SideNavSection>
    <Divider />
    <SideNavSection title={t("Workspace")} isHeaderHidden>
      {recent.length > 0 && <SideNavItem label={t("Recent")} icon={Clock} collapsible={{ defaultIsCollapsed: false }}>
        {recent.map((entry) => <SideNavItem key={routeKey(entry.route)} label={entry.title} isSelected={at(entry.route)} onClick={() => onOpen(entry.route)}
          endContent={entry.app ? <span className="max-w-24 truncate text-2xs text-muted">{applications?.apps.find((app) => app.id === entry.app)?.title ?? entry.app}</span> : undefined} />)}
      </SideNavItem>}
      {pinned.length > 0 && <SideNavItem label={t("Favorites")} icon={Star} collapsible={{ defaultIsCollapsed: false }}>
        {pinned.map((app) => <FavoriteItem key={app.id} app={app} pinned onOpen={() => applications!.onSelect(app.id)} onToggleFavorite={() => onToggleFavorite(app.id)} />)}
      </SideNavItem>}
    </SideNavSection>
  </SideNav>;
}
