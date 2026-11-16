import { Divider } from "@astryxdesign/core/Divider";
import { Icon } from "@astryxdesign/core/Icon";
import { IconButton } from "@astryxdesign/core/IconButton";
import { VStack } from "@astryxdesign/core/VStack";
import { ArrowRightStartOnRectangleIcon, BellIcon, MagnifyingGlassIcon } from "@heroicons/react/24/outline";
import { type App, apps, pathOf } from "../apps";
import { endSession } from "../host";
import { t } from "../i18n";

export function AppRail({ current }: { current: App }) {
  return (
    <VStack padding={2} hAlign="center" vAlign="between" height="100%">
      <VStack gap={1} hAlign="center">
        <IconButton label={t("Search")} tooltip={t("Search")} variant="ghost" icon={<Icon icon={MagnifyingGlassIcon} />} />
        <IconButton label={t("Notifications")} tooltip={t("Notifications")} variant="ghost" icon={<Icon icon={BellIcon} />} />
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
      <IconButton
        label={t("Sign out")}
        tooltip={t("Sign out")}
        variant="ghost"
        icon={<Icon icon={ArrowRightStartOnRectangleIcon} />}
        onClick={endSession}
      />
    </VStack>
  );
}
