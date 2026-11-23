import { Avatar } from "@astryxdesign/core/Avatar";
import { Button } from "@astryxdesign/core/Button";
import {
  DropdownMenu,
  DropdownMenuDivider,
  DropdownMenuGroup,
  DropdownMenuItem,
} from "@astryxdesign/core/DropdownMenu";
import { Icon } from "@astryxdesign/core/Icon";
import { HStack } from "@astryxdesign/core/Layout";
import { Text } from "@astryxdesign/core/Text";
import {
  ArrowRightStartOnRectangleIcon,
  CheckIcon,
  ChevronUpDownIcon,
  Cog6ToothIcon,
  PlusIcon,
} from "@heroicons/react/24/outline";
import { useState } from "react";
import { apps } from "../apps";
import { endSession, startSession, useSession } from "../host";
import { t } from "../i18n";
import { navigate, usePath } from "../router";
import { useAccount } from "./account";
import { SettingsDialog } from "./settings/SettingsDialog";

export function TenantSwitcher() {
  const session = useSession();
  const [, app, entry] = usePath().split("/");
  const { me: data } = useAccount();
  const [settings, setSettings] = useState(false);
  const currentName = data?.tenant.name || data?.tenantId || session?.tenant || t("Workspace");
  const tenants = data?.tenants?.length
    ? data.tenants
    : data
      ? [{ id: data.tenantId, name: data.tenant.name || data.tenantId }]
      : [];
  const canAdmin = Boolean(data?.apps.some((candidate) => candidate.id === "platform"));
  const canHost = Boolean(data?.hostAdmin);
  const switchTo = (tenant: string) => {
    if (!session || tenant === session.tenant) return;
    startSession({ token: session.token, tenant });
    navigate(apps.some((known) => known.id === app) && entry ? `/${app}/${entry}` : "/");
  };
  return (
    <>
      <DropdownMenu
        hasChevron={false}
        placement="below"
        alignment="start"
        menuWidth={240}
        menuMaxHeight={420}
        renderTrigger={(props) => (
          <Button
            {...props}
            label={currentName}
            variant="ghost"
            size="sm"
            icon={<Avatar name={currentName} size="sm" shape="rounded" tooltip={false} />}
            endContent={<Icon icon={ChevronUpDownIcon} size="sm" color="secondary" />}
          >
            <Text type="body" weight="semibold" maxLines={1}>{currentName}</Text>
          </Button>
        )}
      >
        <DropdownMenuGroup title={t("Switch workspace")}>
          {tenants.map((tenant) => {
            const label = tenant.name || tenant.id;
            const active = tenant.id === data?.tenantId;
            return (
              <DropdownMenuItem
                key={tenant.id}
                label={
                  <HStack gap={2} vAlign="center">
                    <Avatar name={label} size="sm" shape="rounded" tooltip={false} />
                    <Text type="body" weight={active ? "medium" : "normal"} maxLines={1}>{label}</Text>
                  </HStack>
                }
                endContent={active ? <Icon icon={CheckIcon} size="sm" /> : undefined}
                onClick={() => switchTo(tenant.id)}
              />
            );
          })}
        </DropdownMenuGroup>
        <DropdownMenuDivider />
        {canHost && (
          <DropdownMenuItem
            icon={PlusIcon}
            label={t("New workspace")}
            href="/host/tenants"
          />
        )}
        <DropdownMenuItem
          icon={Cog6ToothIcon}
          label={t("Workspace settings")}
          href={canAdmin ? "/admin/settings" : undefined}
          onClick={canAdmin ? undefined : () => setSettings(true)}
        />
        <DropdownMenuItem
          icon={ArrowRightStartOnRectangleIcon}
          label={t("Sign out")}
          onClick={endSession}
        />
      </DropdownMenu>
      {settings && <SettingsDialog onClose={() => setSettings(false)} />}
    </>
  );
}
