import { useAppShellMobile } from "@astryxdesign/core/AppShell";
import { EntityAvatar } from "../semantic";
import { Badge } from "@astryxdesign/core/Badge";
import { Divider } from "@astryxdesign/core/Divider";
import { DropdownMenu } from "@astryxdesign/core/DropdownMenu";
import { Icon } from "@astryxdesign/core/Icon";
import { IconButton } from "@astryxdesign/core/IconButton";
import { Kbd } from "@astryxdesign/core/Kbd";
import { HStack, VStack } from "@astryxdesign/core/Layout";
import { StatusDot } from "@astryxdesign/core/StatusDot";
import { colorVars } from "@astryxdesign/core/theme/tokens.stylex";
import { Tooltip } from "@astryxdesign/core/Tooltip";
import { BellIcon, ChevronDoubleRightIcon, InboxIcon, MagnifyingGlassIcon } from "@heroicons/react/24/outline";
import * as stylex from "@stylexjs/stylex";
import { Fragment, type ReactElement, type ReactNode } from "react";
import { type App, pathOf, type RailGroup } from "../apps";
import { t } from "../i18n";
import { useInbox } from "../pages/inbox";
import { navigate } from "../router";
import { motionStyles } from "../pages/motion";
import { useCommandSearch } from "./CommandSearch";
import { MemberMenu } from "./MemberMenu";

const styles = stylex.create({
  rail: { backgroundColor: colorVars["--color-background-body"] },
  anchor: { position: "relative" },
  unread: { position: "absolute", insetBlockStart: 0, insetInlineEnd: 0, pointerEvents: "none" },
});

const middle: RailGroup[] = ["installed", "build", "run"];

function Tip({ label, keys, below, children }: { label: string; keys?: string; below?: boolean; children: ReactElement }) {
  const content = keys ? <HStack gap={2} vAlign="center">{label}<Kbd keys={keys} /></HStack> : label;
  return <Tooltip content={content} placement={below ? "below" : "end"}>{children}</Tooltip>;
}

export function AppRail({ current, compact, apps, collapsed, onExpand }: { current: App; compact?: boolean; apps: App[]; collapsed?: boolean; onExpand?: () => void }) {
  const searchScope = useCommandSearch();
  const { closeMobileNav } = useAppShellMobile();
  const unread = useInbox().items.filter((item) => item.unread).length;
  const search = (
    <Tip label={t("Search")} keys="mod+k" below={compact}>
      <IconButton label={t("Search")} variant="ghost" icon={<Icon icon={MagnifyingGlassIcon} />} onClick={() => {
        if (compact) closeMobileNav();
        searchScope?.openSearch();
      }} />
    </Tip>
  );
  const bell = unread ? `${t("Notifications")} (${unread})` : t("Notifications");
  const notifications = (
    <DropdownMenu
      hasChevron={false}
      placement={compact ? "below" : "end"}
      items={[
        {
          label: t("Inbox"),
          icon: <Icon icon={InboxIcon} size="sm" />,
          endContent: unread ? <Badge variant="neutral" label={String(unread)} /> : undefined,
          onClick: () => navigate("/home/inbox"),
        },
      ]}
      renderTrigger={(props) => (
        <VStack xstyle={styles.anchor}>
          <Tip label={bell} below={compact}>
            <IconButton {...props} label={t("Notifications")} variant="ghost" icon={<Icon icon={BellIcon} />} />
          </Tip>
          {unread > 0 && <StatusDot variant="error" label={t("Unread")} xstyle={[styles.unread, motionStyles.popEnter]} />}
        </VStack>
      )}
    />
  );
  const member = <MemberMenu compact={compact} />;
  if (compact) return <HStack gap={1} vAlign="center">{search}{notifications}{member}</HStack>;
  const link = (app: App, icon: ReactNode) => (
    <Tip key={app.id} label={t(app.label)}>
      <IconButton label={t(app.label)} variant={app.id === current.id ? "secondary" : "ghost"} icon={icon} href={pathOf(app)} onClick={onExpand} aria-current={app.id === current.id ? "page" : undefined} />
    </Tip>
  );
  const of = (group: RailGroup) => apps.filter((app) => app.group === group && app.surface !== "master-data").map((app) => link(app, app.badge
    ? <EntityAvatar name={t(app.label)} size="sm" shape="square" />
    : <Icon icon={app.icon} />));
  const groups = middle.map(of).filter((links) => links.length);
  const logo = (
    <Tip label={t("Platform")}>
      <IconButton label={t("Platform")} variant="ghost" href="/home/overview" onClick={onExpand} icon={<EntityAvatar src="/media/logo.svg" name={t("Platform")} size="sm" shape="rounded" />} />
    </Tip>
  );
  return (
    <VStack padding={2} paddingBlockStart={3} hAlign="center" vAlign="between" height="100%" xstyle={styles.rail}>
      <VStack gap={4} hAlign="center">
        {logo}
        <VStack gap={1} hAlign="center">
          {of("start")}
          {search}
          {groups.map((links, index) => <Fragment key={index}><Divider />{links}</Fragment>)}
        </VStack>
      </VStack>
      <VStack gap={1} hAlign="center">
        {collapsed && (
          <VStack xstyle={motionStyles.popEnter}>
            <Tip label={t("Show sidebar")} keys="mod+b">
              <IconButton label={t("Show sidebar")} variant="ghost" icon={<Icon icon={ChevronDoubleRightIcon} />} onClick={onExpand} />
            </Tip>
          </VStack>
        )}
        {notifications}
        {of("manage")}
        {member}
      </VStack>
    </VStack>
  );
}
