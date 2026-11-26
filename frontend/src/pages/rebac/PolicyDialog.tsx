import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { CheckboxInput } from "@astryxdesign/core/CheckboxInput";
import { DateInput } from "@astryxdesign/core/DateInput";
import { Dialog, DialogHeader } from "@astryxdesign/core/Dialog";
import { FormLayout } from "@astryxdesign/core/FormLayout";
import { HStack, Layout, LayoutContent, LayoutFooter, VStack } from "@astryxdesign/core/Layout";
import { SegmentedControl, SegmentedControlItem } from "@astryxdesign/core/SegmentedControl";
import { Selector } from "@astryxdesign/core/Selector";
import { Text } from "@astryxdesign/core/Text";
import { TextInput } from "@astryxdesign/core/TextInput";
import { useToast } from "@astryxdesign/core/Toast";
import { useEffect, useMemo, useState } from "react";
import { type Api, reread } from "../../host";
import { t } from "../../i18n";
import { reasonOf, send } from "../act";
import { asIsoDate } from "./shared";

function slugPolicyId(input: string): string {
  return input.toLowerCase().replace(/[^a-z0-9-]+/g, "-").replace(/^-+|-+$/g, "").slice(0, 48);
}

export function PolicyDialog({
  isOpen,
  editing,
  permissions,
  memberOptions,
  saveAction,
  onClose,
}: {
  isOpen: boolean;
  editing: Api.PolicyRecord | null;
  permissions: Api.AppPermissions[];
  memberOptions: { value: string; label: string }[];
  saveAction: Api.Action;
  onClose: () => void;
}) {
  const toast = useToast();
  const [title, setTitle] = useState(editing?.title ?? "");
  const [policyId, setPolicyId] = useState(editing?.id ?? "");
  const [effect, setEffect] = useState<"deny" | "allow">((editing?.effect as "deny" | "allow") ?? "deny");
  const [permission, setPermission] = useState(editing?.permission ?? "");
  const [whereMember, setWhereMember] = useState(editing?.where?.member ?? "");
  const [whereApp, setWhereApp] = useState(editing?.where?.app ?? "");
  const [whereTarget, setWhereTarget] = useState(editing?.where?.target ?? "");
  const [agentOnly, setAgentOnly] = useState(editing?.where?.agent === "true");
  const [from, setFrom] = useState(editing?.from ?? "");
  const [until, setUntil] = useState(editing?.until ?? "");
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState("");

  useEffect(() => {
    if (!isOpen) return;
    setTitle(editing?.title ?? "");
    setPolicyId(editing?.id ?? "");
    setEffect((editing?.effect as "deny" | "allow") ?? "deny");
    setPermission(editing?.permission ?? "");
    setWhereMember(editing?.where?.member ?? "");
    setWhereApp(editing?.where?.app ?? "");
    setWhereTarget(editing?.where?.target ?? "");
    setAgentOnly(editing?.where?.agent === "true");
    setFrom(editing?.from ?? "");
    setUntil(editing?.until ?? "");
    setFailure("");
  }, [isOpen, editing]);

  const permissionSuggestions = useMemo(() => {
    const set = new Map<string, string>([["*", `${t("Every action and read")} (*)`]]);
    for (const app of permissions) {
      set.set(`${app.app}.*`, `${t(app.title || app.app)} · ${t("All actions")} (${app.app}.*)`);
      for (const role of app.roles) {
        for (const act of role.actions) {
          if (!set.has(act.id)) set.set(act.id, `${act.title ? t(act.title) : act.id} (${act.id})`);
        }
      }
    }
    return [...set.entries()].map(([value, label]) => ({ value, label }));
  }, [permissions]);

  const effectiveId = editing ? editing.id : slugPolicyId(policyId || title) || `pol-${crypto.randomUUID().slice(0, 6)}`;
  const canSave = !busy && Boolean(permission.trim()) && (!from || !until || from < until);

  const submitPolicy = async () => {
    if (!canSave) return;
    setBusy(true);
    setFailure("");
    const where: Record<string, string> = {};
    if (whereMember) where.member = whereMember;
    if (whereApp) where.app = whereApp;
    if (whereTarget.trim()) where.target = whereTarget.trim();
    if (agentOnly) where.agent = "true";
    try {
      await send(saveAction, effectiveId, {
        title: title.trim() || effectiveId,
        effect,
        permission: permission.trim(),
        ...(Object.keys(where).length > 0 ? { where } : {}),
        from: from || "",
        until: until || "",
      });
      reread();
      toast({ body: editing ? t("Policy updated.") : t("Policy saved.") });
      onClose();
    } catch (problem) {
      setFailure(reasonOf(problem));
    } finally {
      setBusy(false);
    }
  };

  if (!isOpen) return null;

  return (
    <Dialog isOpen onOpenChange={(open) => !open && !busy && onClose()} width={620} purpose="form">
      <Layout
        height="fill"
        padding={4}
        header={
          <DialogHeader
            title={editing ? t("Edit guardrail policy") : t("New guardrail policy")}
            onOpenChange={(open) => !open && !busy && onClose()}
          />
        }
        content={
          <LayoutContent>
            <VStack gap={4}>
              <Text type="supporting" color="secondary">
                {t("Explicit deny policies override every role and delegation. Allow policies grant an action or read when conditions match.")}
              </Text>
              {failure && <Banner status="error" title={t("Not saved")} description={failure} />}
              <FormLayout defaultOptionality="optional">
                <SegmentedControl
                  label={t("Effect")}
                  value={effect}
                  onChange={(next) => setEffect(next as "deny" | "allow")}
                >
                  <SegmentedControlItem value="deny" label={t("Deny (override)")} />
                  <SegmentedControlItem value="allow" label={t("Allow")} />
                </SegmentedControl>
                <TextInput
                  label={t("Policy title")}
                  isRequired
                  value={title}
                  placeholder={t("Freeze production deletions during audit")}
                  onChange={setTitle}
                />
                <Selector
                  label={t("Guided permission preset")}
                  isOptional
                  hasSearch
                  value={permissionSuggestions.some((item) => item.value === permission) ? permission : ""}
                  onChange={(next) => {
                    if (next) setPermission(next);
                  }}
                  options={[{ value: "", label: t("Custom pattern below") }, ...permissionSuggestions]}
                />
                <TextInput
                  label={t("Permission schema or prefix")}
                  isRequired
                  value={permission}
                  placeholder={t("crm.deal.archive or crm.*")}
                  onChange={setPermission}
                />
                <Selector
                  label={t("Restrict to member")}
                  isOptional
                  hasSearch={memberOptions.length > 8}
                  value={whereMember}
                  onChange={setWhereMember}
                  options={[{ value: "", label: t("Any member") }, ...memberOptions]}
                />
                <Selector
                  label={t("Restrict to application")}
                  isOptional
                  value={whereApp}
                  onChange={setWhereApp}
                  options={[
                    { value: "", label: t("Any application") },
                    ...permissions.map((entry) => ({ value: entry.app, label: t(entry.title || entry.app) })),
                  ]}
                />
                <TextInput
                  label={t("Restrict to target object")}
                  isOptional
                  value={whereTarget}
                  placeholder={t("crm.deal")}
                  onChange={setWhereTarget}
                />
                <CheckboxInput
                  label={t("Apply only to AI agent sessions")}
                  value={agentOnly}
                  onChange={setAgentOnly}
                />
                <HStack gap={2} wrap="wrap">
                  <DateInput
                    label={t("Active from")}
                    isOptional
                    hasClear
                    width={220}
                    format="system_date"
                    value={asIsoDate(from)}
                    onChange={(next) => setFrom(next ?? "")}
                  />
                  <DateInput
                    label={t("Active until")}
                    isOptional
                    hasClear
                    width={220}
                    format="system_date"
                    value={asIsoDate(until)}
                    onChange={(next) => setUntil(next ?? "")}
                  />
                </HStack>
              </FormLayout>
            </VStack>
          </LayoutContent>
        }
        footer={
          <LayoutFooter>
            <HStack gap={2} hAlign="end">
              <Button label={t("Cancel")} variant="secondary" isDisabled={busy} onClick={onClose} />
              <Button label={t("Save policy")} variant="primary" isDisabled={!canSave} isLoading={busy} onClick={() => void submitPolicy()} />
            </HStack>
          </LayoutFooter>
        }
      />
    </Dialog>
  );
}
