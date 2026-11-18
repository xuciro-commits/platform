import { Avatar } from "@astryxdesign/core/Avatar";
import {
  DropdownMenu,
  DropdownMenuDivider,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSubMenu,
} from "@astryxdesign/core/DropdownMenu";
import { IconButton } from "@astryxdesign/core/IconButton";
import { apps } from "../apps";
import { endSession, startSession, useSession } from "../host";
import { t } from "../i18n";
import { useState } from "react";
import { navigate, usePath } from "../router";
import { useAccount } from "./account";
import { SettingsDialog } from "./settings/SettingsDialog";

const appearances = [
  { value: "system", label: "Match system" },
  { value: "light", label: "Light" },
  { value: "dark", label: "Dark" },
];

function nameOf(language: string) {
  return new Intl.DisplayNames([language], { type: "language" }).of(language) ?? language;
}

export function MemberMenu({ compact }: { compact?: boolean }) {
  const session = useSession();
  const [, app, entry] = usePath().split("/");
  const [settings, setSettings] = useState(false);
  const { me: data, name, canSave, save } = useAccount();
  const languages = data ? ["en", ...data.languages.filter((language) => language !== "en")] : [];
  const switchTo = (tenant: string) => {
    if (!session || tenant === session.tenant) return;
    startSession({ token: session.token, tenant });
    navigate(apps.some((known) => known.id === app) && entry ? `/${app}/${entry}` : "/");
  };
  return (
    <>
    <DropdownMenu
      hasChevron={false}
      placement={compact ? "below" : "end"}
      alignment={compact ? "end" : "start"}
      menuWidth={240}
      menuMaxHeight={480}
      renderTrigger={(props) => (
        <IconButton {...props} label={t("Account")} tooltip={name || t("Account")} variant="ghost" icon={<Avatar name={name} size="sm" tooltip={false} />} />
      )}
    >
      <DropdownMenuItem label={name} description={data?.tenant.name || data?.tenantId} isDisabled />
      <DropdownMenuDivider />
      {data && data.tenants.length > 1 && (
        <>
          <DropdownMenuGroup title={t("Tenant")}>
            <DropdownMenuRadioGroup label={t("Tenant")} value={data.tenantId} onChange={switchTo}>
              {data.tenants.map((tenant) => <DropdownMenuRadioItem key={tenant.id} value={tenant.id} label={tenant.name || tenant.id} />)}
            </DropdownMenuRadioGroup>
          </DropdownMenuGroup>
          <DropdownMenuDivider />
        </>
      )}
      <DropdownMenuSubMenu label={t("Language")} isDisabled={!canSave}>
        <DropdownMenuRadioGroup label={t("Language")} value={data?.account.language ?? ""} onChange={(language) => save({ language })}>
          <DropdownMenuRadioItem value="" label={t("Automatic")} />
          {languages.map((language) => <DropdownMenuRadioItem key={language} value={language} label={nameOf(language)} />)}
        </DropdownMenuRadioGroup>
      </DropdownMenuSubMenu>
      <DropdownMenuSubMenu label={t("Appearance")} isDisabled={!canSave}>
        <DropdownMenuRadioGroup label={t("Appearance")} value={data?.account.theme || "system"} onChange={(theme) => save({ theme })}>
          {appearances.map((appearance) => <DropdownMenuRadioItem key={appearance.value} value={appearance.value} label={t(appearance.label)} />)}
        </DropdownMenuRadioGroup>
      </DropdownMenuSubMenu>
      <DropdownMenuItem label={t("Settings")} onClick={() => setSettings(true)} />
      <DropdownMenuDivider />
      <DropdownMenuItem label={t("Sign out")} onClick={endSession} />
    </DropdownMenu>
    {settings && <SettingsDialog onClose={() => setSettings(false)} />}
    </>
  );
}
