import { AlertDialog } from "@astryxdesign/core/AlertDialog";
import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { HStack, StackItem, VStack } from "@astryxdesign/core/Layout";
import { SegmentedControl, SegmentedControlItem } from "@astryxdesign/core/SegmentedControl";
import { Selector } from "@astryxdesign/core/Selector";
import { Switch } from "@astryxdesign/core/Switch";
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
  const access = useRead<Api.AccessConfig>("/v1/access");
  const permissions = useRead<Api.AppPermissions[]>("/v1/permissions");
  const catalog = useRead<Api.Action[]>("/v1/actions");
  const [targetKind, setTargetKind] = useState<"member" | "team">("member");
  const [memberId, setMemberId] = useState("");
  const [teamId, setTeamId] = useState("");
  const [selectedRoles, setSelectedRoles] = useState<string[]>([]);
  const [confirmClearDemo, setConfirmClearDemo] = useState<Api.PackageView | null>(null);
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState("");
  const [grantedSummary, setGrantedSummary] = useState("");

  const actions = catalog.status === "ready" ? catalog.data : [];
  const grantAction = actions.find((action) => action.schema === "platform.member.grant");
  const teamSaveAction = actions.find((action) => action.schema === "platform.team.save");
  const clearDemoAction = actions.find((action) => action.schema === "platform.package.clear-demo");

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

  const teams = access.status === "ready" ? access.data.teams : [];
  const teamOptions = useMemo(
    () => teams.map((team) => ({ value: team.id, label: `${team.name} (${team.members.length})` })),
    [teams],
  );
  const canGrantTeam = Boolean(teamSaveAction && teams.length > 0);
  const effectiveKind = targetKind === "team" && canGrantTeam ? "team" : "member";

  const currentMember = members.status === "ready" ? members.data.find((m) => m.id === memberId) : undefined;
  const currentTeam = teams.find((team) => team.id === teamId);
  const heldKeys = useMemo(() => {
    const keys = new Set<string>();
    if (effectiveKind === "team") {
      for (const grant of currentTeam?.grants ?? []) keys.add(`${grant.app}:${grant.role}`);
      return keys;
    }
    if (!currentMember) return keys;
    for (const grant of currentMember.grants ?? []) keys.add(`${grant.app}:${grant.role}`);
    for (const [app, role] of Object.entries(currentMember.roles)) {
      if (role) keys.add(`${app}:${role}`);
    }
    return keys;
  }, [effectiveKind, currentMember, currentTeam]);

  const toggleRole = (key: string, checked: boolean) => {
    setGrantedSummary("");
    setSelectedRoles((prev) => (checked ? [...new Set([...prev, key])] : prev.filter((item) => item !== key)));
  };

  const pendingKeys = selectedRoles.filter((key) => !heldKeys.has(key));
  const hasTarget = effectiveKind === "team" ? Boolean(currentTeam) : Boolean(memberId);

  const grantSelected = async () => {
    if (!hasTarget || pendingKeys.length === 0) return;
    setBusy(true);
    setFailure("");
    setGrantedSummary("");
    try {
      if (effectiveKind === "team" && currentTeam && teamSaveAction) {
        const nextGrants = [...(currentTeam.grants ?? [])];
        for (const key of pendingKeys) {
          const split = key.indexOf(":");
          if (split < 1) continue;
          nextGrants.push({ app: key.slice(0, split), role: key.slice(split + 1) });
        }
        await send(teamSaveAction, currentTeam.id, {
          name: currentTeam.name,
          members: currentTeam.members,
          grants: nextGrants,
        });
        setSelectedRoles([]);
        reread();
        setGrantedSummary(t("Granted {count} role(s) to team {team}. You can adjust them anytime under Admin → Access.")
          .replace("{count}", String(pendingKeys.length))
          .replace("{team}", currentTeam.name));
      } else if (grantAction && memberId) {
        for (const key of pendingKeys) {
          const split = key.indexOf(":");
          if (split < 1) continue;
          await send(grantAction, memberId, { app: key.slice(0, split), role: key.slice(split + 1) });
        }
        setSelectedRoles([]);
        reread();
        setGrantedSummary(t("Granted {count} role(s) to {member}. You can revoke them anytime under Admin → Members.")
          .replace("{count}", String(pendingKeys.length))
          .replace("{member}", memberId));
      }
    } catch (problem) {
      setFailure(reasonOf(problem));
    } finally {
      setBusy(false);
    }
  };

  const clearDemo = async (view: Api.PackageView) => {
    if (!clearDemoAction) return;
    setBusy(true);
    setFailure("");
    try {
      await send(clearDemoAction, view.descriptor.id, { id: view.descriptor.id });
      setConfirmClearDemo(null);
      reread();
      setGrantedSummary(t("Demo data cleared"));
    } catch (problem) {
      setFailure(reasonOf(problem));
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
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
                <HStack gap={2} vAlign="center">
                  {hasDemo && clearDemoAction && (
                    <Button
                      label={t("Clear demo data")}
                      variant="ghost"
                      size="sm"
                      isDisabled={busy}
                      onClick={() => setConfirmClearDemo(view)}
                    />
                  )}
                  {dest && (
                    <Button label={dest.label} variant="secondary" size="sm" onClick={() => navigate(dest.href)} />
                  )}
                </HStack>
              </HStack>
            );
          })}
        </VStack>

        {(grantAction || canGrantTeam) && roleChoices.length > 0 && (
          <VStack gap={3} xstyle={styles.roleBox}>
            <HStack gap={3} vAlign="center" hAlign="between" wrap="wrap">
              <VStack gap={0.5}>
                <Text type="body" weight="medium">{t("Grant initial roles (optional)")}</Text>
                <Text type="supporting" color="secondary">
                  {t("Select a member or team and check the roles to grant explicitly. Every role can be revoked later.")}
                </Text>
              </VStack>
              {canGrantTeam && grantAction && (
                <SegmentedControl
                  label={t("Assign to")}
                  value={effectiveKind}
                  onChange={(next) => {
                    setTargetKind(next as "member" | "team");
                    setSelectedRoles([]);
                    setGrantedSummary("");
                  }}
                >
                  <SegmentedControlItem value="member" label={t("Member")} />
                  <SegmentedControlItem value="team" label={t("Team")} />
                </SegmentedControl>
              )}
            </HStack>
            {failure && <Banner status="error" title={t("Not saved")} description={failure} />}
            {grantedSummary && <Banner status="success" title={grantedSummary} />}
            <HStack gap={3} vAlign="end" wrap="wrap">
              {effectiveKind === "team" ? (
                <Selector
                  label={t("Team")}
                  value={teamId}
                  onChange={(next) => { setTeamId(next); setGrantedSummary(""); }}
                  options={teamOptions}
                />
              ) : (
                <Selector
                  label={t("Member")}
                  value={memberId}
                  onChange={(next) => { setMemberId(next); setGrantedSummary(""); }}
                  options={activeMembers}
                />
              )}
              <Button
                label={t("Grant selected roles")}
                variant="primary"
                size="sm"
                isDisabled={busy || !hasTarget || pendingKeys.length === 0}
                onClick={() => void grantSelected()}
              />
              <Button
                label={effectiveKind === "team" ? t("Manage teams") : t("Manage all members")}
                variant="ghost"
                size="sm"
                onClick={() => navigate(effectiveKind === "team" ? "/admin/access" : "/admin/members")}
              />
            </HStack>
            <HStack gap={4} wrap="wrap">
              {roleChoices.map((choice) => {
                const alreadyHeld = heldKeys.has(choice.key);
                return (
                  <Switch
                    key={choice.key}
                    label={alreadyHeld ? `${choice.label} (${t("Held")})` : choice.label}
                    value={alreadyHeld || selectedRoles.includes(choice.key)}
                    isDisabled={busy || !hasTarget || alreadyHeld}
                    onChange={(checked: boolean) => toggleRole(choice.key, checked)}
                  />
                );
              })}
            </HStack>
          </VStack>
        )}
      </VStack>
      <AlertDialog
        isOpen={Boolean(confirmClearDemo)}
        title={t("Clear demo data for {app}?").replace("{app}", confirmClearDemo ? t(confirmClearDemo.descriptor.title || confirmClearDemo.descriptor.id) : "")}
        description={t("This archives the sample records installed with this app. Your settings, reference data, and any records you created stay.")}
        cancelLabel={t("Cancel")}
        actionLabel={t("Clear demo data")}
        isActionLoading={busy}
        onOpenChange={(open) => !open && !busy && setConfirmClearDemo(null)}
        onAction={() => confirmClearDemo && void clearDemo(confirmClearDemo)}
      />
    </>
  );
}
