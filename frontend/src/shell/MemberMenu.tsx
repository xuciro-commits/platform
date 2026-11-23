import { Avatar } from "@astryxdesign/core/Avatar";
import {
  DropdownMenu,
  DropdownMenuDivider,
  DropdownMenuItem,
  DropdownMenuSubMenu,
} from "@astryxdesign/core/DropdownMenu";
import { Icon } from "@astryxdesign/core/Icon";
import { IconButton } from "@astryxdesign/core/IconButton";
import { HStack, VStack } from "@astryxdesign/core/Layout";
import { Text } from "@astryxdesign/core/Text";
import { Tooltip } from "@astryxdesign/core/Tooltip";
import {
  ArrowRightStartOnRectangleIcon,
  CheckIcon,
  Cog6ToothIcon,
  PaintBrushIcon,
  PlusIcon,
  UserCircleIcon,
} from "@heroicons/react/24/outline";
import { useState } from "react";
import { endSession, switchAccount, useAccounts, useSession } from "../host";
import { t } from "../i18n";
import { useAccount } from "./account";
import { AddAccountDialog } from "./AddAccountDialog";
import { SettingsDialog, type SettingsPanelId } from "./settings/SettingsDialog";

export function MemberMenu({ compact }: { compact?: boolean }) {
  const session = useSession();
  const accounts = useAccounts();
  const [settingsPanel, setSettingsPanel] = useState<SettingsPanelId | null>(null);
  const [adding, setAdding] = useState(false);
  const { me: data, name } = useAccount();
  const email = data?.account.effective.email || data?.account.email || data?.principalId || "";
  const subtitle = data?.tenant.name || data?.tenantId || email;

  return (
    <>
      <DropdownMenu
        hasChevron={false}
        placement={compact ? "below" : "end"}
        alignment={compact ? "end" : "start"}
        menuWidth={240}
        menuMaxHeight={480}
        renderTrigger={(props) => (
          <Tooltip content={name || t("Account")} placement={compact ? "below" : "end"}>
            <IconButton {...props} label={t("Account")} variant="ghost" icon={<Avatar name={name} size="sm" tooltip={false} />} />
          </Tooltip>
        )}
      >
        <DropdownMenuSubMenu
          label={
            <HStack gap={2} vAlign="center">
              <Avatar name={name} size="sm" tooltip={false} />
              <VStack gap={0}>
                <Text type="body" weight="semibold" maxLines={1}>{name || t("Account")}</Text>
                {subtitle && <Text type="supporting" color="secondary" maxLines={1}>{subtitle}</Text>}
              </VStack>
            </HStack>
          }
        >
          {email && (
            <DropdownMenuItem icon={UserCircleIcon} label={email} isDisabled />
          )}
          {accounts.map((account) => {
            const active = account.token === session?.token;
            const label = account.name || account.id;
            return (
              <DropdownMenuItem
                key={account.token}
                label={
                  <HStack gap={2} vAlign="center">
                    <Avatar name={label} size="sm" tooltip={false} />
                    <VStack gap={0}>
                      <Text type="body" weight={active ? "medium" : "normal"} maxLines={1}>{label}</Text>
                      {account.tenantName && <Text type="supporting" color="secondary" maxLines={1}>{account.tenantName}</Text>}
                    </VStack>
                  </HStack>
                }
                endContent={active ? <Icon icon={CheckIcon} size="sm" /> : undefined}
                onClick={() => switchAccount(account.token, account.tenant)}
              />
            );
          })}
          <DropdownMenuDivider />
          <DropdownMenuItem icon={PlusIcon} label={t("Add account")} onClick={() => setAdding(true)} />
        </DropdownMenuSubMenu>
        <DropdownMenuDivider />
        <DropdownMenuItem icon={UserCircleIcon} label={t("Profile")} onClick={() => setSettingsPanel("profile")} />
        <DropdownMenuItem icon={PaintBrushIcon} label={t("Preferences")} onClick={() => setSettingsPanel("appearance")} />
        <DropdownMenuItem icon={Cog6ToothIcon} label={t("Settings")} onClick={() => setSettingsPanel("language")} />
        <DropdownMenuDivider />
        <DropdownMenuItem icon={ArrowRightStartOnRectangleIcon} label={t("Sign out")} onClick={endSession} />
      </DropdownMenu>
      {settingsPanel && <SettingsDialog initialPanel={settingsPanel} onClose={() => setSettingsPanel(null)} />}
      {adding && <AddAccountDialog onClose={() => setAdding(false)} />}
    </>
  );
}
