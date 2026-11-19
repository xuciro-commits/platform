import { Button } from "@astryxdesign/core/Button";
import { VStack } from "@astryxdesign/core/Layout";
import { SegmentedControl, SegmentedControlItem } from "@astryxdesign/core/SegmentedControl";
import { Selector } from "@astryxdesign/core/Selector";
import { Switch } from "@astryxdesign/core/Switch";
import { Token } from "@astryxdesign/core/Token";
import { useToast } from "@astryxdesign/core/Toast";
import {
  AtSymbolIcon, BellIcon, BriefcaseIcon, CalendarDaysIcon, ClockIcon, ComputerDesktopIcon, DevicePhoneMobileIcon, EnvelopeIcon,
  GlobeAltIcon, HashtagIcon, InboxIcon, LanguageIcon, MoonIcon, PaintBrushIcon, PhoneIcon, TagIcon, UserIcon,
} from "@heroicons/react/24/outline";
import { useState } from "react";
import { type Api, request, reread, useRead } from "../../host";
import { t } from "../../i18n";
import { reasonOf } from "../../pages/act";
import { type ProfileValues, useAccount } from "../account";
import { controlWidth, SettingsCard, SettingsRow, TextSetting, ThemeChoiceCards } from "../../pages/SettingsRows";

type Account = ReturnType<typeof useAccount>;

function text(account: Account, field: keyof Api.Account) {
  const value = account.account?.[field];
  return typeof value === "string" ? value : "";
}

function Choice({ label, value, options, onChange, isDisabled, hasSearch }: {
  label: string; value: string; options: { value: string; label: string }[]; onChange: (value: string) => void; isDisabled?: boolean; hasSearch?: boolean;
}) {
  return <Selector label={label} isLabelHidden size="sm" width={controlWidth} value={value} options={options} onChange={onChange} isDisabled={isDisabled} hasSearch={hasSearch} />;
}

export function ProfilePanel({ account }: { account: Account }) {
  const field = (name: keyof Api.Account, label: string, type?: "email" | "tel") => (
    <TextSetting label={label} type={type} value={text(account, name)} isDisabled={!account.canSave} onCommit={(value) => account.save({ [name]: value })} />
  );
  return (
    <VStack gap={5}>
      <SettingsCard title={t("Identity")}>
        <SettingsRow title={t("Display name")} icon={UserIcon} description={t("Shown on records, comments and mentions.")} control={field("displayName", t("Display name"))} />
        <SettingsRow title={t("Given name")} icon={UserIcon} control={field("givenName", t("Given name"))} />
        <SettingsRow title={t("Family name")} icon={UserIcon} control={field("familyName", t("Family name"))} />
        <SettingsRow title={t("Job title")} icon={BriefcaseIcon} control={field("title", t("Job title"))} />
        <SettingsRow title={t("Pronouns")} icon={TagIcon} control={field("pronouns", t("Pronouns"))} />
      </SettingsCard>
      <SettingsCard title={t("Contact")}>
        <SettingsRow title={t("Email")} icon={AtSymbolIcon} description={t("Where notifications are mailed.")} control={field("email", t("Email"), "email")} />
        <SettingsRow title={t("Phone")} icon={PhoneIcon} control={field("phone", t("Phone"), "tel")} />
      </SettingsCard>
    </VStack>
  );
}

function deviceOf(agent = "") {
  const browser = /Edg\//.test(agent) ? "Edge" : /Chrome\//.test(agent) ? "Chrome" : /Firefox\//.test(agent) ? "Firefox" : /Safari\//.test(agent) ? "Safari" : "";
  const system = /iPhone|iPad/.test(agent) ? "iOS" : /Android/.test(agent) ? "Android" : /Mac OS/.test(agent) ? "macOS" : /Windows/.test(agent) ? "Windows" : /Linux/.test(agent) ? "Linux" : "";
  return [browser, system].filter(Boolean).join(" · ") || agent || t("Unknown device");
}

export function SecurityPanel() {
  const toast = useToast();
  const sessions = useRead<Api.Session[]>("/v1/sessions");
  const [busy, setBusy] = useState(false);
  const open = sessions.status === "ready" ? sessions.data.filter((session) => !session.ended) : [];
  const endOthers = async () => {
    setBusy(true);
    try {
      await request("/v1/sessions/end-others", { body: {} });
      reread();
    } catch (problem) {
      toast({ body: reasonOf(problem), type: "error" });
    } finally {
      setBusy(false);
    }
  };
  return (
    <SettingsCard title={t("Sessions")}>
      {open.map((session) => (
        <SettingsRow key={session.id} title={deviceOf(session.agent)} icon={/iPhone|Android/.test(session.agent ?? "") ? DevicePhoneMobileIcon : ComputerDesktopIcon}
          titleAccessory={session.current ? <Token label={t("This session")} size="sm" /> : undefined}
          description={`${t("Last active")} ${new Date(session.last).toLocaleString()}`} />
      ))}
      <SettingsRow title={t("Sign out other sessions")} icon={ClockIcon} description={t("Every other device has to sign in again.")}
        control={<Button label={t("Sign out others")} variant="secondary" size="sm" isLoading={busy} isDisabled={open.length < 2} onClick={endOthers} />} />
    </SettingsCard>
  );
}

export function AppearancePanel({ account }: { account: Account }) {
  return (
    <SettingsCard>
      <SettingsRow title={t("Color theme")} icon={PaintBrushIcon} description={t("Follows you to every device you sign in on.")}
        detail={<ThemeChoiceCards value={text(account, "theme") || "system"} isDisabled={!account.canSave} onChange={(theme) => account.save({ theme })} />} />
    </SettingsCard>
  );
}

const hours = Array.from({ length: 24 }, (_, hour) => `${String(hour).padStart(2, "0")}:00`);

export function NotificationsPanel({ account }: { account: Account }) {
  const flag = (name: "inApp" | "mail") => account.account?.[name] !== false;
  const toggle = (name: "inApp" | "mail", label: string) => (
    <Switch label={label} isLabelHidden value={flag(name)} isDisabled={!account.canSave} onChange={(value) => account.save({ [name]: value })} />
  );
  const quiet = (name: "quietFrom" | "quietTo", label: string) => (
    <Choice label={label} value={text(account, name)} isDisabled={!account.canSave} onChange={(value) => account.save({ [name]: value } as ProfileValues)}
      options={[{ value: "", label: t("Not set") }, ...hours.map((hour) => ({ value: hour, label: hour }))]} />
  );
  return (
    <VStack gap={5}>
      <SettingsCard title={t("Where they arrive")}>
        <SettingsRow title={t("In the workspace")} icon={InboxIcon} description={t("The bell and the inbox.")} control={toggle("inApp", t("In the workspace"))} />
        <SettingsRow title={t("By mail")} icon={EnvelopeIcon} control={toggle("mail", t("By mail"))} />
        <SettingsRow title={t("Mail digest")} icon={BellIcon} control={
          <SegmentedControl label={t("Mail digest")} size="sm" value={text(account, "digest") || "instant"} onChange={(digest) => account.save({ digest })}>
            <SegmentedControlItem value="instant" label={t("Instant")} />
            <SegmentedControlItem value="hourly" label={t("Hourly")} />
            <SegmentedControlItem value="daily" label={t("Daily")} />
          </SegmentedControl>
        } />
      </SettingsCard>
      <SettingsCard title={t("Quiet hours")}>
        <SettingsRow title={t("Starts")} icon={MoonIcon} control={quiet("quietFrom", t("Starts"))} />
        <SettingsRow title={t("Ends")} icon={MoonIcon} control={quiet("quietTo", t("Ends"))} />
      </SettingsCard>
    </VStack>
  );
}

function languageName(language: string) {
  return new Intl.DisplayNames([language], { type: "language" }).of(language) ?? language;
}

export function LanguagePanel({ account }: { account: Account }) {
  const me = account.me;
  const fallback = { value: "", label: t("Tenant default") };
  const choose = (name: keyof Api.Account, label: string, options: { value: string; label: string }[]) => (
    <Choice label={label} value={text(account, name)} options={[fallback, ...options]} isDisabled={!account.canSave} hasSearch={options.length > 12}
      onChange={(value) => account.save({ [name]: value })} />
  );
  const sample = new Date(2026, 10, 18);
  const pad = (value: number) => String(value).padStart(2, "0");
  return (
    <SettingsCard>
      <SettingsRow title={t("Language")} icon={LanguageIcon} description={t("Menus, buttons and the messages the platform sends you.")}
        control={choose("language", t("Language"), ["en", ...(me?.languages ?? []).filter((language) => language !== "en")].map((language) => ({ value: language, label: languageName(language) })))} />
      <SettingsRow title={t("Time zone")} icon={GlobeAltIcon} description={t("Every time you see is shown in it.")}
        control={choose("timezone", t("Time zone"), Intl.supportedValuesOf("timeZone").map((zone) => ({ value: zone, label: zone })))} />
      <SettingsRow title={t("Date format")} icon={CalendarDaysIcon} control={choose("dateFormat", t("Date format"), [
        { value: "ymd", label: `${sample.getFullYear()}-${pad(sample.getMonth() + 1)}-${pad(sample.getDate())}` },
        { value: "dmy", label: `${pad(sample.getDate())}/${pad(sample.getMonth() + 1)}/${sample.getFullYear()}` },
        { value: "mdy", label: `${pad(sample.getMonth() + 1)}/${pad(sample.getDate())}/${sample.getFullYear()}` },
      ])} />
      <SettingsRow title={t("Number format")} icon={HashtagIcon} control={choose("numberFormat", t("Number format"),
        ["1,234.56", "1.234,56", "1 234,56"].map((value) => ({ value, label: value })))} />
      <SettingsRow title={t("Start the week on")} icon={CalendarDaysIcon} control={choose("weekStart", t("Start the week on"), [
        { value: "monday", label: t("Monday") }, { value: "sunday", label: t("Sunday") }, { value: "saturday", label: t("Saturday") },
      ])} />
    </SettingsCard>
  );
}
