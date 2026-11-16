import { HostContext, type AppUI, type Host, type Me } from "@platform/app";
import type { EdgeClient } from "@platform/api";
import { Button, Workspace, language, notify, t } from "@platform/ui";
import { Center } from "@astryxdesign/core/Center";
import { HStack, VStack } from "@astryxdesign/core/Stack";
import { Text } from "@astryxdesign/core/Text";
import { useEffect, useMemo, useState } from "react";

export type HostAdmin = { subject: string; tenants: string[] };

function hostOnly(client: EdgeClient, admin: HostAdmin): Host {
  const me: Me = { tenantId: "", principalId: admin.subject, profile: { id: admin.subject, tenant: "", roles: {} }, apps: [], tenants: admin.tenants.map((id) => ({ id })),
    language: language(), languages: [], currency: "", account: {} as Me["account"], tenant: {} as Me["tenant"] };
  const refuse = () => Promise.reject(new Error("a host administrator reads no tenant records"));
  return {
    client, me, role: () => undefined, holds: () => false, can: () => false, action: () => undefined, catalog: [],
    decide: async () => { notify.error(t("A host administrator takes no tenant decisions.")); return false; },
    outbox: [], resend: async () => undefined, entities: [], definitions: [],
    source: { scope: `host:${admin.subject}`, entity: () => undefined, list: refuse, get: refuse },
    opens: new Map(),
  };
}

export function HostOnly({ client, token, admin, email, onSignOut }: { client: EdgeClient; token: string; admin: HostAdmin; email?: string; onSignOut?: () => void }) {
  const [app, setApp] = useState<AppUI>();
  const [failed, setFailed] = useState<{ message: string; detail?: string }>(), [attempt, setAttempt] = useState(0);
  useEffect(() => {
    let live = true;
    setFailed(undefined);
    void import("@platform/govern").then(
      (m) => { if (live) setApp(m.hostConsole); },
      (error: unknown) => {
        if (!live) return;
        console.error("host console failed to load", error);
        setFailed({
          message: t("The workspace's applications could not load. Retry, or sign in again."),
          detail: error instanceof Error ? error.message : String(error),
        });
      },
    );
    return () => { live = false; };
  }, [attempt]);
  const host = useMemo(() => hostOnly(client, admin), [client, admin.subject]);
  if (failed) return <Center height="100dvh" padding={6}>
    <VStack gap={3} hAlign="center" maxWidth={480}>
      <Text as="p" role="alert">{failed.message}</Text>
      {failed.detail && <Text type="supporting" wordBreak="break-word">{failed.detail}</Text>}
      <HStack gap={2}><Button onClick={() => setAttempt(value => value + 1)}>{t("Retry")}</Button>{onSignOut && <Button variant="ghost" onClick={onSignOut}>{t("Sign out")}</Button>}</HStack>
    </VStack>
  </Center>;
  if (!app) return <Center height="100dvh"><Text type="supporting">{t("Opening the workspace…")}</Text></Center>;
  return (
    <HostContext.Provider value={host}>
      <Workspace key={token} product={app.title} productIcon={app.icon} storageKey={`workspace.layout:host:${admin.subject}`}
        views={app.views} home={typeof app.home === "function" ? app.home(host) : app.home} nav={app.nav(host)}
        status={<Text type="supporting">{t("{n} tenants", { n: admin.tenants.length })}</Text>}
        session={{ tenant: t("Host"), principal: admin.subject, detail: email, current: "",
          options: [], onSwitch: () => {}, onSignOut }} />
    </HostContext.Provider>
  );
}