import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { Switch } from "@astryxdesign/core/Switch";
import { HStack, StackItem, VStack } from "@astryxdesign/core/Layout";
import { Selector } from "@astryxdesign/core/Selector";
import { Heading, Text } from "@astryxdesign/core/Text";
import { Token } from "@astryxdesign/core/Token";
import { colorVars, radiusVars } from "@astryxdesign/core/theme/tokens.stylex";
import * as stylex from "@stylexjs/stylex";
import { useMemo, useState } from "react";
import { type Api, reread, useRead } from "../host";
import { t } from "../i18n";
import { navigate } from "../router";
import { reasonOf, send } from "./act";

const styles = stylex.create({
  panel: {
    padding: 16,
    borderRadius: radiusVars["--radius-container"],
    borderWidth: 1,
    borderStyle: "solid",
    borderColor: colorVars["--color-border"],
    backgroundColor: colorVars["--color-background-muted"],
  },
  row: {
    padding: "10px 12px",
    borderRadius: radiusVars["--radius-element"],
    borderWidth: 1,
    borderStyle: "solid",
    borderColor: colorVars["--color-border"],
    backgroundColor: colorVars["--color-background-surface"],
  },
  roleBox: {
    padding: "10px 12px",
    borderRadius: radiusVars["--radius-element"],
    borderWidth: 1,
    borderStyle: "solid",
    borderColor: colorVars["--color-border"],
    backgroundColor: colorVars["--color-background-surface"],
  },
});

export function destinationOf(view: Api.PackageView): { label: string; href: string; hint?: string } | undefined {
  const space = view.installed?.space || view.installed?.namespace || view.descriptor.namespace;
  if (view.kind === "master-data") {
    return {
      label: t("Open master data"),
      href: "/admin/master-data",
      hint: t("Listed under Admin → Master data; it adds no application to the rail."),
    };
  }
  if (view.kind === "capability") {
    return {
      label: t("Configure capability"),
      href: "/admin/capabilities",
      hint: t("Bind an implementation under Admin → Capabilities; applications that use it then show its actions."),
    };
  }
  if (view.kind === "extension") {
    return {
      label: t("Manage roles"),
      href: "/admin/members",
      hint: t("Adds fields, actions or views inside the application it extends."),
    };
  }
  if (space) {
    return { label: t("Open app"), href: `/${encodeURIComponent(space)}` };
  }
  return undefined;
}

export function PackagePostInstallPanel({
  views,
  onDismiss,
}: {
  views: Api.PackageView[];
  onDismiss: () => void;
}) {
  const members = useRead<Api.MemberView[]>("/v1/members");
  const permissions = useRead<Api.AppPermissions[]>("/v1/permissions");
  const catalog = useRead<Api.Action[]>("/v1/actions");
  const [memberId, setMemberId] = useState("");
  const [selectedRoles, setSelectedRoles] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState("");
  const [grantedSummary, setGrantedSummary] = useState("");

  const grantAction = catalog.status === "ready" ? catalog.data.find((action) => action.schema === "platform.member.grant") : undefined;

  const spaces = useMemo(() => {
    const out = new Set<string>();
    for (const view of views) {
      const space = view.installed?.space || view.installed?.namespace || view.descriptor.namespace;
      if (space) out.add(space);
    }
    return out;
  }, [views]);

  const roleChoices = useMemo(() => {
    if (permissions.status !== "ready") return [];
    return permissions.data
      .filter((entry) => spaces.has(entry.app))
      .flatMap((entry) =>
        entry.roles.map((item) => ({
          key: `${entry.app}:${item.role}`,
          app: entry.app,
          role: item.role,
          label: `${t(entry.title || entry.app)} · ${item.role}`,
        })),
      );
  }, [permissions, spaces]);

  const activeMembers = useMemo(() => {
    if (members.status !== "ready") return [];
    return members.data
      .filter((member) => !member.agent && member.status !== "left")
      .map((member) => ({
        value: member.id,
        label: member.profile.displayName ? `${member.profile.displayName} (${member.id})` : member.id,
      }));
  }, [members]);

  const currentMember = members.status === "ready" ? members.data.find((m) => m.id === memberId) : undefined;
  const heldKeys = useMemo(() => {
    const keys = new Set<string>();
    if (!currentMember) return keys;
    for (const grant of currentMember.grants ?? []) keys.add(`${grant.app}:${grant.role}`);
    for (const [app, role] of Object.entries(currentMember.roles)) {
      if (role) keys.add(`${app}:${role}`);
    }
    return keys;
  }, [currentMember]);

  const toggleRole = (key: string, checked: boolean) => {
    setGrantedSummary("");
    setSelectedRoles((prev) => (checked ? [...new Set([...prev, key])] : prev.filter((item) => item !== key)));
  };

  const grantSelected = async () => {
    if (!grantAction || !memberId || selectedRoles.length === 0) return;
    setBusy(true);
    setFailure("");
    setGrantedSummary("");
    try {
      let granted = 0;
      for (const key of selectedRoles) {
        if (heldKeys.has(key)) continue;
        const split = key.indexOf(":");
        if (split < 1) continue;
        await send(grantAction, memberId, { app: key.slice(0, split), role: key.slice(split + 1) });
        granted += 1;
      }
      setSelectedRoles([]);
      reread();
      setGrantedSummary(t("Granted {count} role(s) to {member}. You can revoke them anytime under Admin → Members.")
        .replace("{count}", String(granted))
        .replace("{member}", memberId));
    } catch (problem) {
      setFailure(reasonOf(problem));
    } finally {
      setBusy(false);
    }
  };

  return (
    <VStack gap={4} xstyle={styles.panel}>
      <HStack gap={3} vAlign="center">
        <StackItem size="fill">
          <VStack gap={0.5}>
            <Heading level={3}>{t("Installation ready")}</Heading>
            <Text type="supporting" color="secondary">
              {t("Packages do not grant permissions automatically. Open the installed entry or explicitly assign roles below.")}
            </Text>
          </VStack>
        </StackItem>
        <Button label={t("Dismiss")} variant="ghost" size="sm" onClick={onDismiss} />
      </HStack>

      <VStack gap={2}>
        {views.map((view) => {
          const dest = destinationOf(view);
          const hasDemo = Boolean(view.installed?.demoRecords?.length && !view.installed.demoCleared);
          return (
            <HStack key={view.descriptor.id} gap={3} vAlign="center" xstyle={styles.row} wrap="wrap">
              <StackItem size="fill">
                <VStack gap={0.5}>
                  <HStack gap={2} vAlign="center" wrap="wrap">
                    <Text type="body" weight="medium">{t(view.descriptor.title || view.descriptor.id)}</Text>
                    <Text type="supporting" color="secondary">{view.descriptor.id} · {view.installed?.version ?? view.descriptor.version}</Text>
                    {hasDemo && <Token label={t("Includes demo data")} color="blue" size="sm" />}
                  </HStack>
                  {dest?.hint && <Text type="supporting" color="secondary">{dest.hint}</Text>}
                </VStack>
              </StackItem>
              {dest && (
                <Button label={dest.label} variant="secondary" size="sm" onClick={() => navigate(dest.href)} />
              )}
            </HStack>
          );
        })}
      </VStack>

      {grantAction && roleChoices.length > 0 && (
        <VStack gap={3} xstyle={styles.roleBox}>
          <VStack gap={0.5}>
            <Text type="body" weight="medium">{t("Grant initial roles (optional)")}</Text>
            <Text type="supporting" color="secondary">
              {t("Select a member and check the roles to grant explicitly. Every role can be revoked later under Admin → Members.")}
            </Text>
          </VStack>
          {failure && <Banner status="error" title={t("Not saved")} description={failure} />}
          {grantedSummary && <Banner status="success" title={grantedSummary} />}
          <HStack gap={3} vAlign="end" wrap="wrap">
            <Selector
              label={t("Member")}
              value={memberId}
              onChange={(next) => { setMemberId(next); setGrantedSummary(""); }}
              options={activeMembers}
            />
            <Button
              label={t("Grant selected roles")}
              variant="primary"
              size="sm"
              isDisabled={busy || !memberId || selectedRoles.filter((key) => !heldKeys.has(key)).length === 0}
              onClick={() => void grantSelected()}
            />
            <Button label={t("Manage all members")} variant="ghost" size="sm" onClick={() => navigate("/admin/members")} />
          </HStack>
          <HStack gap={4} wrap="wrap">
            {roleChoices.map((choice) => {
              const alreadyHeld = heldKeys.has(choice.key);
              return (
                <Switch
                  key={choice.key}
                  label={alreadyHeld ? `${choice.label} (${t("Held")})` : choice.label}
                  value={alreadyHeld || selectedRoles.includes(choice.key)}
                  isDisabled={busy || !memberId || alreadyHeld}
                  onChange={(checked: boolean) => toggleRole(choice.key, checked)}
                />
              );
            })}
          </HStack>
        </VStack>
      )}
    </VStack>
  );
}
