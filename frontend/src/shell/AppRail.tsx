import { Divider } from "@astryxdesign/core/Divider";
import { Icon } from "@astryxdesign/core/Icon";
import { IconButton } from "@astryxdesign/core/IconButton";
import { HStack, VStack } from "@astryxdesign/core/Layout";
import { ArrowRightStartOnRectangleIcon, BellIcon, MagnifyingGlassIcon } from "@heroicons/react/24/outline";
import { type App, apps, pathOf } from "../apps";
import { endSession } from "../host";
import { t } from "../i18n";

export function AppRail({ current, compact, apps }: { current: App; compact?: boolean; apps: App[] }) {
  const search = <IconButton label={t("Search")} tooltip={t("Search")} variant="ghost" icon={<Icon icon={MagnifyingGlassIcon} />} />;
  const notifications = <IconButton label={t("Notifications")} tooltip={t("Notifications")} variant="ghost" icon={<Icon icon={BellIcon} />} />;
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
