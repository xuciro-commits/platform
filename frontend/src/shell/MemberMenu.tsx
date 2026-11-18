import { Avatar } from "@astryxdesign/core/Avatar";
import {
  DropdownMenu,
  DropdownMenuDivider,
  DropdownMenuItem,
  DropdownMenuRadioGroup,
  DropdownMenuRadioItem,
  DropdownMenuSubMenu,
} from "@astryxdesign/core/DropdownMenu";
import { IconButton } from "@astryxdesign/core/IconButton";
import { useToast } from "@astryxdesign/core/Toast";
import { type Api, endSession, useRead } from "../host";
import { t } from "../i18n";
import { reasonOf, send } from "../pages/act";

const appearances = [
  { value: "system", label: "Match system" },
  { value: "light", label: "Light" },
  { value: "dark", label: "Dark" },
];

function nameOf(language: string) {
  return new Intl.DisplayNames([language], { type: "language" }).of(language) ?? language;
}

export function MemberMenu({ compact }: { compact?: boolean }) {
  const toast = useToast();
  const me = useRead<Api.MeView>("/v1/me");
  const catalog = useRead<Api.Action[]>("/v1/actions");
  const data = me.status === "ready" ? me.data : undefined;
  const update = catalog.status === "ready" ? catalog.data.find((action) => action.schema === "platform.profile.update") : undefined;
  const name = data?.account.displayName || data?.principalId || "";
  const languages = data ? ["en", ...data.languages.filter((language) => language !== "en")] : [];
  const save = (field: "language" | "theme", value: string) => {
    if (!update || !data) return;
    send(update, data.principalId, { [field]: value }).catch((problem: unknown) => toast({ body: reasonOf(problem), type: "error" }));
  };
  return (
    <DropdownMenu
      hasChevron={false}
      placement={compact ? "below" : "end"}
      alignment={compact ? "end" : "start"}
      renderTrigger={(props) => (
        <IconButton {...props} label={t("Account")} tooltip={name || t("Account")} variant="ghost" icon={<Avatar name={name} size="sm" tooltip={false} />} />
      )}
    >
      <DropdownMenuItem label={name} description={data?.tenant.name || data?.tenantId} isDisabled />
      <DropdownMenuDivider />
      <DropdownMenuSubMenu label={t("Language")} isDisabled={!update}>
        <DropdownMenuRadioGroup label={t("Language")} value={data?.account.language ?? ""} onChange={(value) => save("language", value)}>
          <DropdownMenuRadioItem value="" label={t("Automatic")} />
          {languages.map((language) => <DropdownMenuRadioItem key={language} value={language} label={nameOf(language)} />)}
        </DropdownMenuRadioGroup>
      </DropdownMenuSubMenu>
      <DropdownMenuSubMenu label={t("Appearance")} isDisabled={!update}>
        <DropdownMenuRadioGroup label={t("Appearance")} value={data?.account.theme || "system"} onChange={(value) => save("theme", value)}>
          {appearances.map((appearance) => <DropdownMenuRadioItem key={appearance.value} value={appearance.value} label={t(appearance.label)} />)}
        </DropdownMenuRadioGroup>
      </DropdownMenuSubMenu>
      <DropdownMenuDivider />
      <DropdownMenuItem label={t("Sign out")} onClick={endSession} />
    </DropdownMenu>
  );
}
