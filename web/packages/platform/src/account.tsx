// My account (ADR-0079 §4): a member's own profile — how the platform
// addresses them, their language, timezone and formats, notifications and
// where the workspace opens. Every value is a decision (platform.profile.update);
// an empty value returns to the tenant's default. Administrators see the same
// form on a member's page (people.tsx).
import type { Api } from "@platform/kernel";
import { useHost, useReadQuery as useRead } from "@platform/app";
import { Button, Form, Checkbox, Input, PageHeader, Panel, Select, Tag, notify, setLanguage, useTheme, t } from "@platform/ui";
import { useEffect, useMemo, useState } from "react";
import type { Member } from "./shared";

export type Account = Api.Account;
export type Profile = Api.Profile;
export type TenantRecord = Api.TenantRecord;
type TokenView = Api.TokenView;
type Session = Api.Session;

const DATE_FORMATS = ["ymd", "dmy", "mdy"];
const NUMBER_FORMATS = ["1,234.56", "1.234,56", "1 234,56"];
const WEEK_STARTS = ["monday", "sunday", "saturday"];
const DIGESTS = ["instant", "hourly", "daily"];
const THEMES = ["system", "light", "dark"];
const DENSITIES = ["comfortable", "compact"];

/** The browser's list of IANA zones, when it has one; the common ones otherwise. */
export function timezones(): string[] {
  try {
    const all = (Intl as unknown as { supportedValuesOf?: (k: string) => string[] }).supportedValuesOf?.("timeZone");
    if (all?.length) return all;
  } catch { /* older browsers */ }
  return ["UTC", "Asia/Shanghai", "Asia/Tokyo", "Asia/Singapore", "Asia/Kolkata", "Europe/Berlin", "Europe/London", "America/New_York", "America/Chicago", "America/Los_Angeles", "Australia/Sydney"];
}

/** Now, as the member sees it, for a preview next to the timezone choice. */
function clock(zone: string, dateFormat: string) {
  try {
    const parts = new Intl.DateTimeFormat("en-GB", { timeZone: zone, year: "numeric", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", hour12: false }).formatToParts(new Date());
    const p = Object.fromEntries(parts.map((x) => [x.type, x.value]));
    const date = dateFormat === "dmy" ? `${p.day}.${p.month}.${p.year}` : dateFormat === "mdy" ? `${p.month}/${p.day}/${p.year}` : `${p.year}-${p.month}-${p.day}`;
    return `${date} ${p.hour}:${p.minute}`;
  } catch { return ""; }
}

type Draft = Partial<Record<keyof Profile, string | boolean>>;

/** The profile form for one member: their own, or anyone's for an administrator. */
export function ProfileForm({ member, account, languages, tenant, self }: { member: string; account: Account; languages: string[]; tenant?: TenantRecord; self: boolean }) {
  const { decide, me } = useHost();
  const { setScheme } = useTheme();
  const [draft, setDraft] = useState<Draft>({});
  // The apps this member may open: mine from /v1/me; another member's from the
  // roles the member list shows (administrators only, who are the ones
  // editing others), titled by the apps I know.
  const members = useRead<Member[]>("/v1/members", undefined, !self).data ?? [];
  const openable = useMemo(() => {
    if (self) return me.apps.map((a) => ({ id: a.id, title: a.title }));
    const roles = members.find((m) => m.id === member)?.roles ?? {};
    return Object.keys(roles).sort().map((id) => ({ id, title: me.apps.find((a) => a.id === id)?.title ?? id }));
  }, [self, me.apps, members, member]);
  useEffect(() => setDraft({}), [account]);
  const value = (k: keyof Profile) => (draft[k] ?? (account[k] as string | boolean | undefined) ?? "") as string;
  const flag = (k: "inApp" | "mail") => (draft[k] ?? account[k] ?? true) as boolean;
  const dirty = Object.keys(draft).length > 0;
  const zones = useMemo(timezones, []);
  const defaults = tenant?.settings ?? {};
  const inherit = (k: string, label: string) => `${t("Tenant default")}: ${defaults[k] || label}`;
  const save = async () => {
    const payload: Record<string, unknown> = {};
    for (const [k, v] of Object.entries(draft)) payload[k] = v;
    if (await decide("platform.profile.update", { type: "platform.profile", id: member }, payload)) {
      if (self && typeof draft.language === "string") setLanguage(draft.language || (defaults.language ?? ""));
      if (self && typeof draft.theme === "string" && draft.theme) setScheme(draft.theme as "system" | "light" | "dark");
      notify.success(t("Profile saved."));
      setDraft({});
    }
  };
  // Fields in a two-column grid: a cell with a hint is taller than its
  // neighbour; content-start keeps every control on the same line as the
  // control beside it instead of letting the grid spread the rows.
  const text = (k: keyof Profile, label: string, hint?: string, type = "text") => (
    <label className="grid content-start gap-1 text-sm">
      <span className="text-xs text-muted">{label}</span>
      <Input type={type} value={value(k)} onChange={(e) => setDraft({ ...draft, [k]: e.target.value })} />
      {hint && <span className="text-[11px] text-muted">{hint}</span>}
    </label>
  );
  const choice = (k: keyof Profile, label: string, options: string[], render: (o: string) => string, hint?: string) => (
    <label className="grid content-start gap-1 text-sm">
      <span className="text-xs text-muted">{label}</span>
      <Select value={value(k)} onChange={(e) => setDraft({ ...draft, [k]: e.target.value })}>
        <option value="">{t("— tenant default")}</option>
        {options.map((o) => <option key={o} value={o}>{render(o)}</option>)}
      </Select>
      {hint && <span className="text-[11px] text-muted">{hint}</span>}
    </label>
  );
  const zone = value("timezone") || account.effective.timezone || "UTC";
  return (
    <div className="grid max-w-3xl gap-4">
      <Panel title={t("Name and contact")}>
        <div className="grid gap-3 md:grid-cols-2">
          {text("displayName", t("Display name"), t("How you appear in lists, approvals and the audit trail."))}
          {text("title", t("Job title"))}
          {text("givenName", t("Given name"))}
          {text("familyName", t("Family name"))}
          {text("email", t("Notification email"), account.effective.email ? `${t("Default")}: ${account.effective.email}` : undefined, "email")}
          {text("phone", t("Phone"), undefined, "tel")}
          {text("pronouns", t("Pronouns"))}
        </div>
      </Panel>
      <Panel title={t("Language, time and formats")}>
        <div className="grid gap-3 md:grid-cols-2">
          {choice("language", t("Language"), ["en", ...languages.filter((l) => l !== "en")], (l) => l, inherit("language", t("browser")))}
          <label className="grid content-start gap-1 text-sm">
            <span className="text-xs text-muted">{t("Timezone")}</span>
            <Input list="platform-timezones" value={value("timezone")} placeholder={t("— tenant default")} onChange={(e) => setDraft({ ...draft, timezone: e.target.value })} />
            <datalist id="platform-timezones">{zones.map((z) => <option key={z} value={z} />)}</datalist>
            <span className="text-[11px] text-muted">{inherit("timezone", "UTC")} · {t("Now")}: {clock(zone, value("dateFormat") || account.effective.dateFormat)}</span>
          </label>
          {choice("dateFormat", t("Date format"), DATE_FORMATS, (o) => ({ ymd: "2026-10-06", dmy: "06.10.2026", mdy: "10/06/2026" })[o] ?? o, inherit("dateFormat", "ymd"))}
          {choice("numberFormat", t("Number format"), NUMBER_FORMATS, (o) => o, inherit("numberFormat", "1,234.56"))}
          {choice("weekStart", t("Week starts on"), WEEK_STARTS, (o) => t(o), inherit("weekStart", "monday"))}
        </div>
      </Panel>
      <Panel title={t("Notifications")}>
        <div className="grid gap-3 md:grid-cols-2">
          <div className="self-start"><Checkbox checked={flag("inApp")} onChange={(v) => setDraft({ ...draft, inApp: v })}><span className="text-sm">{t("Notify in the workspace")}</span></Checkbox></div>
          <div className="self-start"><Checkbox checked={flag("mail")} onChange={(v) => setDraft({ ...draft, mail: v })}><span className="text-sm">{t("Notify by mail")}</span></Checkbox></div>
          {choice("digest", t("Mail digest"), DIGESTS, (o) => t(o), inherit("digest", "instant"))}
          <div className="grid grid-cols-2 content-start gap-2">
            {text("quietFrom", t("Quiet hours from"), undefined, "time")}
            {text("quietTo", t("until"), undefined, "time")}
          </div>
        </div>
      </Panel>
      <Panel title={t("Workspace")}>
        <div className="grid gap-3 md:grid-cols-2">
          <label className="grid content-start gap-1 text-sm">
            <span className="text-xs text-muted">{t("Opens on")}</span>
            <Select value={value("homePage")} onChange={(e) => setDraft({ ...draft, homePage: e.target.value })}>
              <option value="">{t("— home page")}</option>
              {openable.map((a) => <option key={a.id} value={a.id}>{a.title} · {a.id}</option>)}
              {value("homePage") && !openable.some((a) => a.id === value("homePage")) && <option value={value("homePage")}>{value("homePage")}</option>}
            </Select>
            <span className="text-[11px] text-muted">{t("The app the workspace opens on; only apps this member holds a role in are offered.")}</span>
          </label>
          {choice("theme", t("Appearance"), THEMES, (o) => t(o))}
          {choice("density", t("Density"), DENSITIES, (o) => t(o))}
        </div>
      </Panel>
      <div className="flex items-center gap-3">
        <Button variant="primary" disabled={!dirty} onClick={() => void save()}>{t("Save")}</Button>
        {dirty && <Button onClick={() => setDraft({})}>{t("Discard")}</Button>}
        {account.lastSeen && <span className="text-xs text-muted">{t("Last seen")} {new Date(account.lastSeen).toLocaleString()}</span>}
      </div>
    </div>
  );
}

export function MyAccount() {
  const { me } = useHost();
  const account = useRead<Account>("/v1/account");
  const tenant = useRead<TenantRecord>("/v1/tenant");
  return (
    <>
      <PageHeader title={t("My account")} description={t("How the platform addresses you and behaves for you. What you leave empty follows {tenant}.", { tenant: tenant.data?.name ?? me.tenantId })}
        actions={<span className="flex gap-1"><Tag label={me.principalId} /><Tag label={me.tenantId} tone="info" /></span>} />
      {account.data && <ProfileForm member={me.principalId} account={account.data} languages={me.languages} tenant={tenant.data} self />}
      {account.data && <Panel title={t("Identities")} description={t("What signs in as you: a verified address from the identity provider, or a client id. An administrator binds them when adding or inviting you; a personal token is not an identity.")} className="mt-4 max-w-3xl">
        <ul className="grid gap-1 text-sm">{(account.data.subjects ?? []).map((s) => <li key={s} className="font-mono text-xs">{s}</li>)}</ul>
      </Panel>}
      <Delegate />
      <Tokens />
      <Sessions />
    </>
  );
}

// Hand what I hold in an app to someone else until a day (ADR-0078 §3.3).
function Delegate() {
  const { me, decide, can } = useHost();
  const apps = Object.keys(me.profile.roles ?? {});
  const members = useRead<Member[]>("/v1/members").data ?? [];
  const [app, setApp] = useState(apps[0] ?? "");
  const [to, setTo] = useState("");
  const [until, setUntil] = useState("");
  const [reason, setReason] = useState("");
  const [done, setDone] = useState("");
  if (!can("platform.member.delegate") || apps.length === 0) return null;
  const chosenApp = apps.includes(app) ? app : apps[0];
  return (
    <Panel title={t("Delegate my roles")} description={t("For an absence or a handover: another member holds what you hold in an app, until the day you set. You keep your own roles; an administrator can revoke the delegation.")} className="mt-4 max-w-3xl">
      <Form className="flex flex-wrap items-end gap-2 text-sm" onSubmit={async () => { if (await decide("platform.member.delegate", { type: "platform.member", id: to }, { app: chosenApp, until, reason })) setDone(t("Delegated {app} to {who} until {day}.", { app: chosenApp ?? "", who: to, day: until })); }}>
        <label className="grid gap-1"><span className="text-xs text-muted">{t("App")}</span><Select value={chosenApp} onChange={(e) => setApp(e.target.value)}>{apps.map((a) => <option key={a} value={a}>{a}</option>)}</Select></label>
        <label className="grid gap-1"><span className="text-xs text-muted">{t("To")}</span>
          {members.length ? <Select value={to} onChange={(e) => setTo(e.target.value)}><option value="">—</option>{members.filter((m) => m.id !== me.principalId).map((m) => <option key={m.id} value={m.id}>{m.profile.displayName || m.id}</option>)}</Select>
            : <Input value={to} onChange={(e) => setTo(e.target.value)} placeholder={t("Member ID")} />}</label>
        <label className="grid gap-1"><span className="text-xs text-muted">{t("Until")}</span><Input type="date" value={until} onChange={(e) => setUntil(e.target.value)} /></label>
        <label className="grid gap-1"><span className="text-xs text-muted">{t("Reason (optional)")}</span><Input value={reason} onChange={(e) => setReason(e.target.value)} /></label>
        <Button type="submit" variant="primary" disabled={!to || !until}>{t("Delegate")}</Button>
      </Form>
      {done && <p className="mt-2 text-xs text-muted">{done}</p>}
    </Panel>
  );
}

// Personal tokens (ADR-0079 §5): a credential for scripts that acts as me,
// within the permissions I name. The secret is collected once, right after
// the decision; the host never shows it again.
function Tokens() {
  const { client, decide, can } = useHost();
  const tokens = useRead<TokenView[]>("/v1/tokens");
  const [label, setLabel] = useState("");
  const [scopes, setScopes] = useState("");
  const [until, setUntil] = useState("");
  const [secret, setSecret] = useState<{ id: string; secret: string } | null>(null);
  if (!can("platform.token.issue")) return null;
  const issue = async () => {
    const id = "t" + Date.now().toString(36);
    const payload = { label, scopes: scopes.split(/[\s,]+/).filter(Boolean), until };
    if (!(await decide("platform.token.issue", { type: "platform.token", id }, payload))) return;
    try {
      const got = await client.get<{ secret: string }>(`/v1/tokens/${encodeURIComponent(id)}/secret`, true);
      setSecret({ id, secret: got.secret });
    } catch { notify.error(t("The secret could not be collected")); }
    setLabel(""); setScopes(""); setUntil("");
    void tokens.refetch();
  };
  return (
    <Panel title={t("Personal tokens")} description={t("A credential for scripts and integrations that acts as you, within the permissions you name, until a day. The secret is shown once.")} className="mt-4 max-w-3xl">
      {secret && <div className="mb-3 rounded border border-[var(--tone-warning)] p-2 text-sm">
        <p className="text-xs text-muted">{t("Copy the secret now; it is not shown again.")}</p>
        <code className="block select-all break-all font-mono text-xs">{secret.secret}</code>
        <Button size="sm" className="mt-1" onClick={() => { void navigator.clipboard?.writeText(secret.secret); setSecret(null); }}>{t("Copied, hide it")}</Button>
      </div>}
      <Form className="flex flex-wrap items-end gap-2 text-sm" onSubmit={issue}>
        <label className="grid gap-1"><span className="text-xs text-muted">{t("Label")}</span><Input value={label} onChange={(e) => setLabel(e.target.value)} placeholder={t("CI, a notebook, …")} /></label>
        <label className="grid gap-1"><span className="text-xs text-muted">{t("Scopes")}</span><Input value={scopes} onChange={(e) => setScopes(e.target.value)} placeholder={t("Permission patterns, such as mes.order.* ; empty: everything you may do")} className="w-80" /></label>
        <label className="grid gap-1"><span className="text-xs text-muted">{t("Until")}</span><Input type="date" value={until} onChange={(e) => setUntil(e.target.value)} /></label>
        <Button type="submit" variant="primary" disabled={!label}>{t("Issue personal token")}</Button>
      </Form>
      <div className="mt-3 grid gap-1 text-sm">
        {(tokens.data ?? []).length === 0 && <p className="text-xs text-muted">{t("No personal tokens.")}</p>}
        {(tokens.data ?? []).map((x) => (
          <p key={x.id} className="flex flex-wrap items-center gap-2">
            <span className="font-medium">{x.label}</span><span className="font-mono text-xs text-muted">{x.id}</span>
            {x.scopes.map((s) => <Tag key={s} label={s} />)}
            {x.until && <span className="text-xs text-muted">{t("until {day}", { day: x.until })}</span>}
            {x.expired && <Tag label={t("expired")} tone="warning" />}
            <span className="text-xs text-muted">{x.lastUsed ? t("last used {at}", { at: new Date(x.lastUsed).toLocaleString() }) : t("never used")}</span>
            {can("platform.token.revoke") && <Button size="sm" variant="ghost" onClick={async () => { if (await decide("platform.token.revoke", { type: "platform.token", id: x.id }, {})) void tokens.refetch(); }}>{t("Revoke")}</Button>}
          </p>
        ))}
      </div>
    </Panel>
  );
}

// Sessions (ADR-0079 §5): the credentials this host has seen act as me.
function Sessions() {
  const { client } = useHost();
  const sessions = useRead<Session[]>("/v1/sessions");
  const [ended, setEnded] = useState<number | null>(null);
  const list = sessions.data ?? [];
  return (
    <Panel title={t("Sessions")} description={t("Where you are signed in, as this host has seen it since it started. Ending the other sessions makes the host refuse their credentials; a personal token stays until revoked.")} className="mt-4 max-w-3xl"
      actions={list.length > 1 && <Button size="sm" onClick={async () => { const r = await client.get<{ ended: number }>("/v1/sessions/end-others\n{}", true); setEnded(r.ended); void sessions.refetch(); }}>{t("Sign out other sessions")}</Button>}>
      <div className="grid gap-1 text-sm">
        {list.length === 0 && <p className="text-xs text-muted">{t("No sessions seen yet.")}</p>}
        {list.map((s) => (
          <p key={s.id} className="flex flex-wrap items-center gap-2">
            <Tag label={t(s.kind)} tone={s.kind === "token" ? "info" : "neutral"} />
            <span>{s.agent || t("unknown client")}</span>
            {s.token && <span className="font-mono text-xs text-muted">{s.token}</span>}
            <span className="text-xs text-muted">{t("since {at}", { at: new Date(s.first).toLocaleString() })} · {t("last {at}", { at: new Date(s.last).toLocaleString() })}</span>
            {s.current && <Tag label={t("this session")} tone="success" />}
            {s.ended && <Tag label={t("ended {at}", { at: new Date(s.ended).toLocaleString() })} tone="warning" />}
          </p>
        ))}
      </div>
      {ended !== null && <p className="mt-2 text-xs text-muted">{t("{n} other sessions ended.", { n: String(ended) })}</p>}
    </Panel>
  );
}
