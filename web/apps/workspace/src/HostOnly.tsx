// The workspace for a subject who administers the host but is a member of no
// tenant (WorkQueue #141: pure host administrator). `/v1/me` answers 401 for
// them, so nothing tenant-bound can load; `/v1/host/me` answers, and that is
// enough for the Host Console, whose every call goes to `/v1/host/*`. The
// shell carries only that application: no portal, no records, no decisions.
import { HostContext, type AppUI, type Host, type Me } from "@platform/app";
import type { EdgeClient } from "@platform/kernel";
import { Button, Workspace, language, notify, t } from "@platform/ui";
import { useEffect, useMemo, useState } from "react";

export type HostAdmin = { subject: string; tenants: string[] };

/** A Host that reaches the console and refuses everything a tenant member could do. */
function hostOnly(client: EdgeClient, admin: HostAdmin): Host {
  const me: Me = { tenantId: "", principalId: admin.subject, profile: { id: admin.subject, tenant: "", roles: {} }, apps: [], tenants: admin.tenants,
    language: language(), languages: [], currency: "", account: {} as Me["account"], tenant: {} as Me["tenant"] };
  const refuse = () => Promise.reject(new Error("a host administrator reads no tenant records"));
  return {
    client, me, role: () => undefined, can: () => false, action: () => undefined, catalog: [],
    decide: async () => { notify.error(t("A host administrator takes no tenant decisions.")); return false; },
    outbox: [], resend: async () => undefined, entities: [], definitions: [],
    source: { scope: `host:${admin.subject}`, entity: () => undefined, list: refuse, get: refuse },
    opens: new Map(),
  };
}

export function HostOnly({ client, token, admin, email, onSignOut }: { client: EdgeClient; token: string; admin: HostAdmin; email?: string; onSignOut?: () => void }) {
  const [app, setApp] = useState<AppUI>();
  const [failed, setFailed] = useState(false), [attempt, setAttempt] = useState(0);
  useEffect(() => {
    let live = true;
    setFailed(false);
    void import("@platform/govern").then((m) => { if (live) setApp(m.hostConsole); }, () => { if (live) setFailed(true); });
    return () => { live = false; };
  }, [attempt]);
  const host = useMemo(() => hostOnly(client, admin), [client, admin.subject]);
  if (failed) return <main className="grid h-dvh place-content-center gap-3 p-6 text-center text-sm"><p role="alert">{t("The workspace's applications could not load. Retry, or sign in again.")}</p><div className="flex justify-center gap-2"><Button onClick={() => setAttempt(value => value + 1)}>{t("Retry")}</Button>{onSignOut && <Button variant="ghost" onClick={onSignOut}>{t("Sign out")}</Button>}</div></main>;
  if (!app) return <main className="grid h-dvh place-items-center text-sm text-muted">{t("Opening the workspace…")}</main>;
  return (
    <HostContext.Provider value={host}>
      <Workspace key={token} product={app.title} productIcon={app.icon} storageKey={`workspace.layout:host:${admin.subject}`}
        views={app.views} home={typeof app.home === "function" ? app.home(host) : app.home} nav={app.nav(host)}
        status={<span className="text-xs text-muted">{t("{n} tenants", { n: admin.tenants.length })}</span>}
        session={{ tenant: t("Host"), principal: admin.subject, detail: email, current: "",
          options: onSignOut ? [{ id: "sign-out", label: email ? t("Sign out {email}", { email }) : t("Sign out") }] : [],
          onSwitch: (id) => { if (id === "sign-out") onSignOut?.(); } }} />
    </HostContext.Provider>
  );
}
