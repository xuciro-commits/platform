import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { Dialog, DialogHeader } from "@astryxdesign/core/Dialog";
import { FormLayout } from "@astryxdesign/core/FormLayout";
import { HStack, Layout, LayoutContent, LayoutFooter, VStack } from "@astryxdesign/core/Layout";
import { MultiSelector } from "@astryxdesign/core/MultiSelector";
import { Selector } from "@astryxdesign/core/Selector";
import { Text } from "@astryxdesign/core/Text";
import { TextInput } from "@astryxdesign/core/TextInput";
import { useToast } from "@astryxdesign/core/Toast";
import { useEffect, useMemo, useState } from "react";
import { type Api, reread } from "../../host";
import { t } from "../../i18n";
import { reasonOf, send } from "../act";

function slugRoleId(input: string): string {
  return input.toLowerCase().replace(/[^a-z0-9-]+/g, "-").replace(/^-+|-+$/g, "").slice(0, 48);
}

export function CustomRoleDialog({
  isOpen,
  editing,
  permissions,
  saveAction,
  onClose,
}: {
  isOpen: boolean;
  editing: Api.CustomRole | null;
  permissions: Api.AppPermissions[];
  saveAction: Api.Action;
  onClose: () => void;
}) {
  const toast = useToast();
  const appOptions = useMemo(
    () =>
      permissions
        .filter((entry) => entry.roles.some((role) => role.actions.length > 0))
        .map((entry) => ({
          value: entry.app,
          label: entry.title && entry.title !== entry.app ? `${t(entry.title)} (${entry.app})` : t(entry.app),
        })),
    [permissions],
  );

  const [app, setApp] = useState(editing?.app ?? appOptions[0]?.value ?? "");
  const [roleId, setRoleId] = useState(editing?.id ?? "");
  const [title, setTitle] = useState(editing?.title ?? "");
  const [selectedActions, setSelectedActions] = useState<string[]>(editing?.actions ?? []);
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState("");

  useEffect(() => {
    if (!isOpen) return;
    setApp(editing?.app ?? appOptions[0]?.value ?? "");
    setRoleId(editing?.id ?? "");
    setTitle(editing?.title ?? "");
    setSelectedActions(editing?.actions ?? []);
    setFailure("");
  }, [isOpen, editing, appOptions]);

  const chosenApp = editing ? editing.app : appOptions.some((item) => item.value === app) ? app : appOptions[0]?.value ?? "";
  const actionChoices = useMemo(() => {
    const entry = permissions.find((item) => item.app === chosenApp);
    if (!entry) return [];
    const seen = new Map<string, string>();
    for (const role of entry.roles) {
      for (const act of role.actions) {
        if (!seen.has(act.id)) {
          seen.set(act.id, act.title ? `${t(act.title)} (${act.id})` : act.id);
        }
      }
    }
    return [...seen.entries()]
      .sort(([a], [b]) => a.localeCompare(b))
      .map(([value, label]) => ({ value, label }));
  }, [permissions, chosenApp]);

  const effectiveId = editing ? editing.id : slugRoleId(roleId || title);
  const canSave = !busy && Boolean(chosenApp && effectiveId && selectedActions.length > 0);

  const submitRole = async () => {
    if (!canSave) return;
    setBusy(true);
    setFailure("");
    try {
      await send(saveAction, effectiveId, {
        app: chosenApp,
        title: title.trim() || effectiveId,
        actions: selectedActions,
      });
      reread();
      toast({ body: editing ? t("Custom role updated.") : t("Custom role created.") });
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
            title={editing ? t("Edit custom role") : t("Define custom role")}
            onOpenChange={(open) => !open && !busy && onClose()}
          />
        }
        content={
          <LayoutContent>
            <VStack gap={4}>
              <Text type="supporting" color="secondary">
                {t("A custom role bundles specific actions in an application so members and teams can hold least-privilege capabilities.")}
              </Text>
              {failure && <Banner status="error" title={t("Not saved")} description={failure} />}
              <FormLayout defaultOptionality="optional">
                <Selector
                  label={t("Application")}
                  isRequired
                  isDisabled={Boolean(editing) || busy}
                  value={chosenApp}
                  onChange={(next) => {
                    setApp(next);
                    setSelectedActions([]);
                  }}
                  options={appOptions}
                />
                <TextInput
                  label={t("Display title")}
                  isRequired
                  isDisabled={busy}
                  value={title}
                  placeholder={t("Regional deal approver")}
                  onChange={(next) => {
                    setTitle(next);
                    if (!editing && !roleId) setRoleId(slugRoleId(next));
                  }}
                />
                <TextInput
                  label={t("Role key")}
                  isRequired
                  isDisabled={Boolean(editing) || busy}
                  value={editing ? editing.id : roleId}
                  placeholder={t("regional-approver")}
                  onChange={(next) => setRoleId(slugRoleId(next))}
                />
                <MultiSelector
                  label={t("Allowed actions")}
                  isRequired
                  isDisabled={busy || actionChoices.length === 0}
                  hasSearch
                  hasSelectAll
                  hasClear
                  triggerDisplay="badges"
                  maxBadges={3}
                  value={selectedActions}
                  options={actionChoices}
                  onChange={setSelectedActions}
                />
              </FormLayout>
            </VStack>
          </LayoutContent>
        }
        footer={
          <LayoutFooter>
            <HStack gap={2} hAlign="end">
              <Button label={t("Cancel")} variant="secondary" isDisabled={busy} onClick={onClose} />
              <Button label={t("Save role")} variant="primary" isDisabled={!canSave} isLoading={busy} onClick={() => void submitRole()} />
            </HStack>
          </LayoutFooter>
        }
      />
    </Dialog>
  );
}
