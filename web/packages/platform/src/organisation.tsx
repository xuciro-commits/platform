// Organisation (ADR-0027 §2.1): the tenant's own record — identity,
// localisation defaults every profile inherits, sign-in and governance — and
// its standing on the host. Each value is a platform setting, decided with
// platform.setting.set like any app setting, so it is journaled and audited.
import { useHost, useReadQuery as useRead } from "@platform/app";
import { ContextHelp, Button, Input, PageHeader, Panel, Select, Tag, t, useUnsavedChanges, useWorkspace } from "@platform/ui";
import { useQuery } from "@tanstack/react-query";
import { queryElements, resolveElements } from "@platform/kernel";
import { useState } from "react";
import type { AppSettings, SettingValue } from "./shared";
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
  const { decide, can, client, source } = useHost();
  const { open } = useWorkspace();
  const [draft, setDraft] = useState<Record<string, string>>({});
  useUnsavedChanges(Object.keys(draft).length > 0, () => setDraft({}));
  const organisations = useQuery({ queryKey: [client.connection.token, client.connection.tenant, source.scope, "representative-organisations"], queryFn: () => queryElements(client.get.bind(client), { stereotype: "ActualOrganization", limit: 200 }), enabled: !!bySetting(settings, "enterpriseRoot") });
  const set = (s: SettingValue, value: string) => decide("platform.setting.set", { type: "platform.setting", id: `platform/${s.name}` }, { value });
  const byName = Object.fromEntries(settings.map((s) => [s.name, s]));
  const record = tenant.data;
  const root = draft.enterpriseRoot ?? byName.enterpriseRoot?.value ?? "";
  const chosenRoot = useQuery({ queryKey: [client.connection.token, client.connection.tenant, source.scope, "representative-root", root], queryFn: () => resolveElements(client.get.bind(client), [root]), enabled: !!root && !organisations.data?.elements.some(element => element.id === root) });
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
      <PageHeader title={record?.name ?? t("Organisation")} help={t("The tenant's record: who it is, the defaults every member inherits, how people sign in. Members override language, timezone and formats in their own account.")}
        actions={record && <span className="flex gap-1"><Tag label={record.id} /><Tag label={t(record.status)} tone={record.status === "active" ? "success" : "warning"} /></span>} />
      <datalist id="platform-tenant-timezones">{timezones().map((z) => <option key={z} value={z} />)}</datalist>
      <div className="grid max-w-3xl gap-3">
        {byName.enterpriseRoot && <Panel title={t("Representative organization")} help={t("Choose the enterprise organisation represented by this tenant. Changing the tenant name does not rename the model.")}><div className="grid gap-3">
          {organisations.isError ? <p role="alert">{t("Organisation choices could not be loaded.")}</p> : <Select aria-label={t("Representative organization")} value={root} disabled={!can("platform.setting.set")} onChange={event => setDraft({ ...draft, enterpriseRoot: event.target.value })}><option value="">{t("Not linked")}</option>{organisations.data?.elements.map(element => <option key={element.id} value={element.id}>{element.name}</option>)}{root && !organisations.data?.elements.some(element => element.id === root) && <option value={root}>{chosenRoot.data?.elements[0]?.name ?? root}</option>}</Select>}
          <div className="flex flex-wrap gap-2"><Button disabled={!can("platform.setting.set") || root === byName.enterpriseRoot.value} onClick={async () => { if (await set(byName.enterpriseRoot!, root)) setDraft(({ enterpriseRoot: _, ...rest }) => rest); }}>{t("Save")}</Button><Button disabled={!root} onClick={() => open({ view: "enterprise", params: { element: root, section: "organization" } })}>{t("Open organisation node")}</Button></div>
        </div></Panel>}
        {settings.length === 0 && <p className="text-sm text-muted">{t("— administrators only.")}</p>}
        {groups.map((g) => (
          <Panel key={g.title} title={t(g.title)}>
            <div className="grid gap-3">
              {g.names.flatMap((name) => byName[name] ? [byName[name]] : []).map((s) => (
                <div key={s.name} className="grid gap-3 text-sm sm:grid-cols-[minmax(0,1fr)_16rem] sm:items-center">
                  <div>
                    <div className="flex items-center justify-between gap-2"><span className="font-medium">{s.title}</span>{s.description && <ContextHelp title={s.title}>{s.description}</ContextHelp>}</div>
                    {s.value !== s.default && s.default && <p className="text-xs text-muted">{t("default")}: {s.default}</p>}
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
          <div className="mt-3 flex justify-end"><ContextHelp title={t("On this host")}>{t("Lifecycle, quotas and support sessions are the host administrator's, in the Host Console.")}</ContextHelp></div>
        </Panel>}
      </div>
    </>
  );
}

const bySetting = (settings: SettingValue[], name: string) => settings.find(setting => setting.name === name);
