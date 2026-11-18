import { useAppShellMobile } from "@astryxdesign/core/AppShell";
import { Divider } from "@astryxdesign/core/Divider";
import { Icon } from "@astryxdesign/core/Icon";
import { IconButton } from "@astryxdesign/core/IconButton";
import { HStack, VStack } from "@astryxdesign/core/Layout";
import { Badge } from "@astryxdesign/core/Badge";
import { DropdownMenu } from "@astryxdesign/core/DropdownMenu";
import { ArrowRightStartOnRectangleIcon, BellIcon, InboxIcon, MagnifyingGlassIcon } from "@heroicons/react/24/outline";
import { type App, pathOf } from "../apps";
import { endSession } from "../host";
import { t } from "../i18n";
import { useInbox } from "../pages/inbox";
import { navigate } from "../router";
import { useCommandSearch } from "./CommandSearch";

export function AppRail({ current, compact, apps }: { current: App; compact?: boolean; apps: App[] }) {
  const searchScope = useCommandSearch();
  const { closeMobileNav } = useAppShellMobile();
  const search = <IconButton label={t("Search")} tooltip={t("Search")} variant="ghost" icon={<Icon icon={MagnifyingGlassIcon} />} onClick={() => {
    if (compact) closeMobileNav();
    searchScope?.openSearch();
  }} />;
  const unread = useInbox().items.filter((item) => item.unread).length;
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
        <IconButton {...props} label={t("Notifications")} tooltip={t("Notifications")} variant="ghost" icon={<Icon icon={BellIcon} />} />
      )}
    />
  );
  const signOut = (
    <IconButton label={t("Sign out")} tooltip={t("Sign out")} variant="ghost" icon={<Icon icon={ArrowRightStartOnRectangleIcon} />} onClick={endSession} />
  );
  if (compact) return <HStack gap={1} vAlign="center">{search}{notifications}{signOut}</HStack>;
  return (
    <VStack padding={2} hAlign="center" vAlign="between" height="100%">
      <VStack gap={1} hAlign="center">
        {search}
        {notifications}
        <Divider />
        {apps.map((app) => (
          <IconButton
            key={app.id}
            label={t(app.label)}
            tooltip={t(app.label)}
            variant={app.id === current.id ? "secondary" : "ghost"}
            icon={<Icon icon={app.icon} />}
            href={pathOf(app)}
          />
        ))}
      </VStack>
      {signOut}
    </VStack>
  );
}
