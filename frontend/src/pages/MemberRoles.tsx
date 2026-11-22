import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { HStack, VStack } from "@astryxdesign/core/Layout";
import { List, ListItem } from "@astryxdesign/core/List";
import { Selector } from "@astryxdesign/core/Selector";
import { Text } from "@astryxdesign/core/Text";
import { useMemo, useState } from "react";
import { type Api, reread, useRead } from "../host";
import { t } from "../i18n";
import { reasonOf, send } from "./act";

export function MemberRoles({ member }: { member: Api.MemberView }) {
  const packages = useRead<Api.PackageView[]>("/v1/packages");
  const permissions = useRead<Api.AppPermissions[]>("/v1/permissions");
  const catalog = useRead<Api.Action[]>("/v1/actions");
  const [selected, setSelected] = useState("");
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState("");

  const installedApps = useMemo(() => {
    const apps = new Set<string>();
    if (packages.status !== "ready") return apps;
    for (const view of packages.data) {
      if (view.installed?.state !== "active") continue;
      for (const name of [view.installed.space, view.installed.namespace, view.descriptor.namespace]) {
        if (name) apps.add(name);
      }
    }
    return apps;
  }, [packages]);

  const titleByApp = useMemo(() => {
    const map = new Map<string, string>();
    if (permissions.status === "ready") {
      for (const entry of permissions.data) map.set(entry.app, entry.title || entry.app);
    }
    return map;
  }, [permissions]);

  const held = useMemo(() => {
    const pairs: { app: string; role: string }[] = [];
    const seen = new Set<string>();
    for (const grant of member.grants ?? []) {
      const key = `${grant.app}:${grant.role}`;
      if (seen.has(key)) continue;
      seen.add(key);
      pairs.push({ app: grant.app, role: grant.role });
    }
    for (const [app, role] of Object.entries(member.roles)) {
      const key = `${app}:${role}`;
      if (!role || seen.has(key)) continue;
      seen.add(key);
      pairs.push({ app, role });
    }
    return pairs;
  }, [member]);

  const heldKeys = useMemo(() => new Set(held.map((entry) => `${entry.app}:${entry.role}`)), [held]);

  const options = useMemo(() => {
    if (permissions.status !== "ready") return [];
    return permissions.data
      .filter((entry) => installedApps.has(entry.app))
      .flatMap((entry) =>
        entry.roles
          .filter((item) => !heldKeys.has(`${entry.app}:${item.role}`))
          .map((item) => ({
            value: `${entry.app}:${item.role}`,
            label: `${entry.title || entry.app} · ${item.role}`,
          })),
      );
  }, [permissions, installedApps, heldKeys]);

  const actions = catalog.status === "ready" ? catalog.data : [];
  const grantAction = actions.find((action) => action.schema === "platform.member.grant");
  const revokeAction = actions.find((action) => action.schema === "platform.member.revoke");

  const run = async (action: Api.Action | undefined, payload: Record<string, unknown>, clear = false) => {
    if (!action) return;
    setBusy(true);
    setFailure("");
    try {
      await send(action, member.id, payload);
      if (clear) setSelected("");
      reread();
    } catch (problem) {
      setFailure(reasonOf(problem));
    } finally {
      setBusy(false);
    }
  };

  const grant = () => {
    const split = selected.indexOf(":");
    if (split < 1) return;
    void run(grantAction, { app: selected.slice(0, split), role: selected.slice(split + 1) }, true);
  };

  return (
    <VStack gap={3}>
      {failure && <Banner status="error" title={t("Not saved")} description={failure} />}
      {held.length > 0 && (
        <List header={t("Active roles")} hasDividers>
          {held.map((entry) => (
            <ListItem
              key={`${entry.app}:${entry.role}`}
              label={`${titleByApp.get(entry.app) || entry.app} · ${entry.role}`}
              endContent={
                revokeAction && installedApps.has(entry.app) ? (
                  <Button
                    label={t("Revoke")}
                    variant="secondary"
                    size="sm"
                    isDisabled={busy}
                    onClick={() => void run(revokeAction, { app: entry.app, role: entry.role })}
                  />
                ) : undefined
              }
            />
          ))}
        </List>
      )}
      {grantAction && (
        options.length > 0 ? (
          <HStack gap={2} vAlign="end" wrap="wrap">
            <Selector
              label={t("Package role")}
              value={selected}
              onChange={setSelected}
              options={options}
            />
            <Button
              label={t("Grant role")}
              variant="primary"
              size="sm"
              isDisabled={busy || !selected}
              onClick={grant}
            />
          </HStack>
        ) : (
          <Text type="supporting" color="secondary">{t("No installed package roles are available.")}</Text>
        )
      )}
    </VStack>
  );
}
