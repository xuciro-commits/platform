// Organisation (ADR-0078 §2.1): the tenant's own record — identity,
// localisation defaults every profile inherits, sign-in and governance — and
// its standing on the host. Each value is a platform setting, decided with
// platform.setting.set like any app setting, so it is journaled and audited.
import { useReadQuery as useRead } from "@platform/app";
import { Button, Input, PageHeader, Panel, Select, Tag, t } from "@platform/ui";
import { useState } from "react";
import { useAdmin, type AppSettings, type SettingValue } from "./shared";
import type { TenantRecord } from "./account";
import { timezones } from "./account";

const groups: { title: string; names: string[] }[] = [
  { title: "Identity", names: ["name", "legalName", "country"] },
  { title: "Localisation defaults", names: ["language", "timezone", "currency", "dateFormat", "numberFormat", "weekStart", "fiscalYearStart"] },
  { title: "Sign-in and sessions", names: ["signInDomains", "mfaRequired", "sessionHours"] },
  { title: "Notifications", names: ["digest"] },
];

export function Organisation() {
  const tenant = useRead<TenantRecord>("/v1/tenant");
  const settings = useRead<AppSettings[]>("/v1/settings").data?.find((a) => a.app === "platform")?.settings ?? [];
  const { decideOn } = useAdmin();
  const [draft, setDraft] = useState<Record<string, string>>({});
  const set = (s: SettingValue, value: string) => decideOn("platform.setting.set", { type: "platform.setting", id: `platform/${s.name}` }, { value });
  const byName = Object.fromEntries(settings.map((s) => [s.name, s]));
  const record = tenant.data;
  const field = (s: SettingValue) => {
    if (s.type === "boolean" || s.type === "choice") {
      return <Select aria-label={s.title} value={s.value} onChange={(e) => void set(s, e.target.value)}>
        {(s.type === "boolean" ? ["true", "false"] : s.choices ?? []).map((c) => <option key={c} value={c}>{s.type === "boolean" ? (c === "true" ? t("On") : t("Off")) : c}</option>)}
      </Select>;
    }
    const current = draft[s.name] ?? s.value;
    return <span className="flex gap-2">
      <Input aria-label={s.title} list={s.name === "timezone" ? "platform-tenant-timezones" : undefined} draftKey={s.name} step={s.type==="integer"?1:undefined} optional={false} type={s.type === "integer" ? "number" : "text"} value={current}
        onChange={(e) => setDraft({ ...draft, [s.name]: e.target.value })} />
      <Button size="md" disabled={current === s.value} onClick={async () => { if (await set(s, current)) setDraft(({ [s.name]: _, ...rest }) => rest); }}>{t("Save")}</Button>
    </span>;
  };
  return (
    <>
      <PageHeader title={record?.name ?? t("Organisation")} description={t("The tenant's record: who it is, the defaults every member inherits, how people sign in. Members override language, timezone and formats in their own account.")}
        actions={record && <span className="flex gap-1"><Tag label={record.id} /><Tag label={t(record.status)} tone={record.status === "active" ? "success" : "warning"} /></span>} />
      <datalist id="platform-tenant-timezones">{timezones().map((z) => <option key={z} value={z} />)}</datalist>
      <div className="grid max-w-3xl gap-3">
        {settings.length === 0 && <p className="text-sm text-muted">{t("— administrators only.")}</p>}
        {groups.map((g) => (
          <Panel key={g.title} title={t(g.title)}>
            <div className="grid gap-3">
              {g.names.flatMap((name) => byName[name] ? [byName[name]] : []).map((s) => (
                <div key={s.name} className="grid grid-cols-[1fr_16rem] items-center gap-3 text-sm">
                  <div>
                    <p className="font-medium">{s.title}</p>
                    <p className="text-xs text-muted">{s.description}{s.value !== s.default && s.default ? ` (${t("default")} ${s.default})` : ""}</p>
                  </div>
                  {field(s)}
                </div>
              ))}
            </div>
          </Panel>
        ))}
        {record && <Panel title={t("On this host")}>
          <dl className="grid grid-cols-[10rem_1fr] gap-x-3 gap-y-1 text-sm">
            <dt className="text-muted">{t("Tenant ID")}</dt><dd className="font-mono text-xs">{record.id}</dd>
            <dt className="text-muted">{t("Members")}</dt><dd>{record.members}</dd>
            <dt className="text-muted">{t("Apps")}</dt><dd className="flex flex-wrap gap-1">{record.apps.map((a) => <Tag key={a} label={a} />)}</dd>
            <dt className="text-muted">{t("Status")}</dt><dd>{t(record.status)}</dd>
          </dl>
          <p className="mt-2 text-xs text-muted">{t("Lifecycle, quotas and support sessions are the host administrator's, in the Host Console.")}</p>
        </Panel>}
      </div>
    </>
  );
}
