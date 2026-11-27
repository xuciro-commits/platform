import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { Dialog, DialogHeader } from "@astryxdesign/core/Dialog";
import { FormLayout } from "@astryxdesign/core/FormLayout";
import { HStack, Layout, LayoutContent, LayoutFooter, StackItem, VStack } from "@astryxdesign/core/Layout";
import { SegmentedControl, SegmentedControlItem } from "@astryxdesign/core/SegmentedControl";
import { Selector } from "@astryxdesign/core/Selector";
import { Text } from "@astryxdesign/core/Text";
import { TextInput } from "@astryxdesign/core/TextInput";
import { useToast } from "@astryxdesign/core/Toast";
import { useMemo, useState } from "react";
import { type Api, reread, useRead, reasonOf, send } from "../host";
import { t } from "../i18n";
import { roleTitle } from "./rebac/shared";

function normalizeAccount(raw: string): { subject: string; email: string; slug: string } {
  const trimmed = raw.trim();
  if (!trimmed) return { subject: "", email: "", slug: "" };
  const subject = trimmed.startsWith("user:") || trimmed.startsWith("client:") ? trimmed : `user:${trimmed}`;
  const identity = subject.replace(/^(?:user|client):/, "").trim();
  const email = subject.startsWith("user:") && identity.includes("@") ? identity : "";
  const local = (email ? email.split("@")[0] : identity).toLowerCase().replace(/[^a-z0-9._-]+/g, "-").replace(/^-+|-+$/g, "").slice(0, 40);
  const slug = local || "member";
  return { subject, email, slug };
}

export function InviteMemberDialog({ onClose }: { onClose: () => void }) {
  const toast = useToast();
  const members = useRead<Api.MemberView[]>("/v1/members");
  const apps = useRead<Api.AppInfo[]>("/v1/apps");
  const permissions = useRead<Api.AppPermissions[]>("/v1/permissions");
  const entities = useRead<Api.EntityInfo[]>("/v1/entities");
  const catalog = useRead<Api.Action[]>("/v1/actions");

  const [account, setAccount] = useState("");
  const [displayName, setDisplayName] = useState("");
  const [selectedApp, setSelectedApp] = useState<string | null>(null);
  const [selectedRole, setSelectedRole] = useState("");
  const [accessMode, setAccessMode] = useState<"edit" | "readonly">("edit");
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState("");

  const grantableApps = useMemo(() => {
    const out = new Map<string, { id: string; title: string; roles: string[] }>();
    if (apps.status === "ready") {
      for (const entry of apps.data) {
        const roles = entry.roles.filter((role) => role !== "*");
        if (roles.length > 0) out.set(entry.id, { id: entry.id, title: entry.title || entry.id, roles });
      }
    }
    if (permissions.status === "ready") {
      for (const entry of permissions.data) {
        if (out.has(entry.app)) continue;
        const roles = entry.roles.map((item) => item.role).filter((role) => role !== "*");
        if (roles.length > 0) out.set(entry.app, { id: entry.app, title: entry.title || entry.app, roles });
      }
    }
    return [...out.values()];
  }, [apps, permissions]);

  const defaultApp = useMemo(() => {
    const system = new Set(["platform", "build", "work", "files", "monitoring", "ai", "agent", "relations", "enterprise"]);
    return grantableApps.find((entry) => !system.has(entry.id))?.id ?? "";
  }, [grantableApps]);

  const chosenApp = selectedApp === null ? defaultApp : (selectedApp === "" || grantableApps.some((entry) => entry.id === selectedApp) ? selectedApp : defaultApp);
  const availableRoles = useMemo(
    () => grantableApps.find((entry) => entry.id === chosenApp)?.roles ?? [],
    [grantableApps, chosenApp],
  );
  const chosenRole = availableRoles.includes(selectedRole) ? selectedRole : availableRoles[0] ?? "";

  const actions = catalog.status === "ready" ? catalog.data : [];
  const addAction = actions.find((action) => action.schema === "platform.member.add");
  const profileAction = actions.find((action) => action.schema === "platform.profile.update");
  const grantAction = actions.find((action) => action.schema === "platform.member.grant");
  const policyAction = actions.find((action) => action.schema === "platform.policy.save");

  const parsed = normalizeAccount(account);
  const canInvite = !busy && Boolean(addAction && parsed.subject);

  const uniqueMemberId = () => {
    const taken = new Set((members.status === "ready" ? members.data : []).map((member) => member.id));
    if (!taken.has(parsed.slug)) return parsed.slug;
    let suffix = 2;
    while (taken.has(`${parsed.slug}-${suffix}`)) suffix += 1;
    return `${parsed.slug}-${suffix}`;
  };

  const dependencyUserSpaces = (space: string): string[] => {
    if (entities.status !== "ready") return [];
    const needed = new Set<string>();
    for (const entity of entities.data) {
      if (!entity.type.startsWith(`${space}.`)) continue;
      if (entity.extends?.includes(".")) needed.add(entity.extends.split(".")[0]);
      for (const field of entity.fields) {
        if (field.ref?.includes(".")) needed.add(field.ref.split(".")[0]);
      }
    }
    needed.delete(space);
    return [...needed].filter((dep) => grantableApps.some((app) => app.id === dep && app.roles.includes("user")));
  };

  const appWriteSchemas = (space: string): string[] => {
    const schemas = new Set<string>();
    if (permissions.status === "ready") {
      const entry = permissions.data.find((item) => item.app === space);
      for (const role of entry?.roles ?? []) {
        for (const act of role.actions) schemas.add(act.id);
      }
    }
    for (const action of actions) {
      if (action.target.startsWith(`${space}.`)) schemas.add(action.schema);
    }
    return [...schemas];
  };

  const invite = async () => {
    if (!canInvite || !addAction) return;
    setBusy(true);
    setFailure("");
    try {
      const memberId = uniqueMemberId();
      await send(addAction, memberId, { subject: parsed.subject, agent: parsed.subject.startsWith("client:") });
      if (profileAction && (displayName.trim() || parsed.email)) {
        await send(profileAction, memberId, {
          displayName: displayName.trim() || memberId,
          ...(parsed.email ? { email: parsed.email } : {}),
        });
      }
      if (grantAction && chosenApp && chosenRole) {
        await send(grantAction, memberId, { app: chosenApp, role: chosenRole });
        for (const dep of dependencyUserSpaces(chosenApp)) {
          await send(grantAction, memberId, { app: dep, role: "user" });
        }
        if (accessMode === "readonly" && policyAction) {
          const schemas = appWriteSchemas(chosenApp);
          await Promise.all(schemas.map((schema, index) => {
            const polId = `ro-${memberId}-${chosenApp}-${index + 1}`.replace(/[^a-z0-9-]+/g, "-").slice(0, 48);
            return send(policyAction, polId, {
              title: `${t("Read-only")} · ${memberId}`,
              effect: "deny",
              permission: schema,
              where: { member: memberId },
            });
          }));
        }
      }
      reread();
      toast({ body: t("Colleague invited.") });
      onClose();
    } catch (problem) {
      setFailure(reasonOf(problem));
      setBusy(false);
    }
  };

  const appOptions = [
    { value: "", label: t("None") },
    ...grantableApps.map((entry) => ({ value: entry.id, label: t(entry.title) })),
  ];

  return (
    <Dialog isOpen onOpenChange={(open) => !open && onClose()} width={560} purpose="form">
      <Layout
        height="fill"
        padding={4}
        header={<DialogHeader title={t("Invite colleague")} onOpenChange={(open) => !open && onClose()} />}
        content={
          <LayoutContent>
            <VStack gap={4}>
              <Text type="supporting" color="secondary">
                {t("Enter the colleague's account email and choose their application role.")}
              </Text>
              {failure && <Banner status="error" title={t("Not saved")} description={failure} />}
              <FormLayout defaultOptionality="optional">
                <TextInput
                  label={t("Customer account")}
                  placeholder={t("Email")}
                  isRequired
                  value={account}
                  onChange={setAccount}
                />
                <TextInput
                  label={t("Display name")}
                  value={displayName}
                  onChange={setDisplayName}
                />
                <Selector
                  label={t("Application")}
                  value={chosenApp}
                  options={appOptions}
                  onChange={(value) => setSelectedApp(value)}
                />
                {chosenApp !== "" && (
                  <Selector
                    label={t("Role")}
                    value={chosenRole}
                    options={availableRoles.map((role) => ({ value: role, label: roleTitle(role) }))}
                    onChange={setSelectedRole}
                  />
                )}
                {chosenApp !== "" && policyAction && (
                  <VStack gap={1.5}>
                    <Text type="body" weight="semibold" size="sm">{t("Access level")}</Text>
                    <SegmentedControl
                      label={t("Access level")}
                      value={accessMode}
                      onChange={(value) => setAccessMode(value as "edit" | "readonly")}
                    >
                      <SegmentedControlItem value="edit" label={t("Can edit")} />
                      <SegmentedControlItem value="readonly" label={t("Read-only")} />
                    </SegmentedControl>
                  </VStack>
                )}
              </FormLayout>
            </VStack>
          </LayoutContent>
        }
        footer={
          <LayoutFooter>
            <HStack gap={2} vAlign="center">
              <StackItem size="fill" />
              <Button label={t("Cancel")} variant="secondary" onClick={onClose} />
              <Button label={t("Invite")} variant="primary" isDisabled={!canInvite} isLoading={busy} onClick={() => void invite()} />
            </HStack>
          </LayoutFooter>
        }
      />
    </Dialog>
  );
}
