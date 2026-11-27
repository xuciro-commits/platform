import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { DateInput } from "@astryxdesign/core/DateInput";
import { Dialog, DialogHeader } from "@astryxdesign/core/Dialog";
import { FormLayout } from "@astryxdesign/core/FormLayout";
import { HStack, Layout, LayoutContent, LayoutFooter, StackItem, VStack } from "@astryxdesign/core/Layout";
import { SegmentedControl, SegmentedControlItem } from "@astryxdesign/core/SegmentedControl";
import { Selector } from "@astryxdesign/core/Selector";
import { Text } from "@astryxdesign/core/Text";
import { TextArea } from "@astryxdesign/core/TextArea";
import { useToast } from "@astryxdesign/core/Toast";
import { useEffect, useMemo, useState } from "react";
import { type Api, reread, useRead, reasonOf, send } from "../../host";
import { t } from "../../i18n";
import { type DurationPreset, addDaysISO, asIsoDate, roleTitle, todayISO, untilFromPreset, useUnitChoices } from "./shared";

export type RequestAccessDialogProps = {
  isOpen: boolean;
  onClose: () => void;
  initialScope?: "role" | "project";
  fixedApp?: string;
  fixedProject?: string;
  initialRole?: string;
  onSubmitted?: () => void;
};

const projectRoles = ["viewer", "editor", "discoverer"] as const;

export function RequestAccessDialog({
  isOpen,
  onClose,
  initialScope,
  fixedApp,
  fixedProject,
  initialRole,
  onSubmitted,
}: RequestAccessDialogProps) {
  const toast = useToast();
  const me = useRead<Api.MeView>(isOpen ? "/v1/me" : null);
  const apps = useRead<Api.AppInfo[]>(isOpen ? "/v1/apps" : null);
  const catalog = useRead<Api.Action[]>(isOpen ? "/v1/actions" : null);
  const { choices: unitChoices } = useUnitChoices();

  const projects = me.status === "ready" ? me.data.tenant.projects ?? [] : [];
  const canPickScope = !fixedApp && !fixedProject && projects.length > 0;
  const [scope, setScope] = useState<"role" | "project">(fixedProject ? "project" : initialScope ?? "role");
  const [app, setApp] = useState(fixedApp ?? "");
  const [project, setProject] = useState(fixedProject ?? "");
  const [role, setRole] = useState(initialRole ?? "");
  const [unitKey, setUnitKey] = useState("");
  const [preset, setPreset] = useState<DurationPreset>(fixedProject || initialScope === "project" ? "30d" : "30d");
  const [customUntil, setCustomUntil] = useState(() => addDaysISO(30));
  const [reason, setReason] = useState("");
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState("");

  useEffect(() => {
    if (!isOpen) return;
    setScope(fixedProject ? "project" : initialScope ?? "role");
    if (fixedApp) setApp(fixedApp);
    if (fixedProject) setProject(fixedProject);
    if (initialRole) setRole(initialRole);
    setFailure("");
  }, [isOpen, fixedApp, fixedProject, initialScope, initialRole]);

  const requestableApps = useMemo(() => {
    if (apps.status !== "ready") return [];
    return apps.data
      .map((entry) => ({
        id: entry.id,
        title: entry.title || entry.id,
        roles: entry.roles.filter((candidate) => !(entry.id === "platform" && candidate === "admin") && candidate !== "*"),
      }))
      .filter((entry) => entry.roles.length > 0);
  }, [apps]);

  const chosenApp = fixedApp || (requestableApps.some((item) => item.id === app) ? app : requestableApps[0]?.id ?? "");
  const chosenProject = fixedProject || (projects.some((item) => item.id === project) ? project : projects[0]?.id ?? "");
  const availableRoles = useMemo(() => {
    if (scope === "project") return [...projectRoles];
    return requestableApps.find((item) => item.id === chosenApp)?.roles ?? [];
  }, [scope, requestableApps, chosenApp]);
  const chosenRole = availableRoles.includes(role) ? role : availableRoles[0] ?? "";

  const zone = me.status === "ready" ? me.data.account.effective.timezone : undefined;
  const today = todayISO(zone);
  const minDate = addDaysISO(1, today);
  const resolvedUntil = untilFromPreset(preset, customUntil, today);
  const selectedUnit = unitChoices.find((item) => item.value === unitKey);

  const actions = catalog.status === "ready" ? catalog.data : [];
  const requestAction = actions.find((item) => item.schema === "platform.access.request");
  const grantAction = actions.find((item) => item.schema === "platform.member.grant");
  const isAdmin = me.status === "ready" && me.data.apps.some((entry) => entry.id === "platform" && entry.role === "admin");

  const validTarget = scope === "project" ? Boolean(chosenProject && chosenRole) : Boolean(chosenApp && chosenRole);
  const validUntil = preset === "permanent" || (Boolean(resolvedUntil) && resolvedUntil > today);
  const canSubmit = !busy && Boolean(requestAction) && validTarget && validUntil && reason.trim().length > 0;

  const submitRequest = async () => {
    if (!requestAction || !canSubmit) return;
    setBusy(true);
    setFailure("");
    try {
      await send(requestAction, `req-${crypto.randomUUID().slice(0, 8)}`, {
        scope,
        project: scope === "project" ? chosenProject : "",
        app: scope === "role" ? chosenApp : "",
        role: chosenRole,
        unit: scope === "role" && selectedUnit ? selectedUnit.unit : "",
        structure: scope === "role" && selectedUnit ? selectedUnit.structure : "",
        reason: reason.trim(),
        until: resolvedUntil,
      });
      reread();
      toast({
        body: scope === "project"
          ? t("Access request sent to the project owners.")
          : t("Access request sent to the workspace administrators."),
      });
      setReason("");
      onSubmitted?.();
      onClose();
    } catch (problem) {
      setFailure(reasonOf(problem));
    } finally {
      setBusy(false);
    }
  };

  const selfGrant = async () => {
    if (!grantAction || me.status !== "ready" || scope !== "role" || !chosenApp || !chosenRole || !validUntil) return;
    setBusy(true);
    setFailure("");
    try {
      await send(grantAction, me.data.principalId, {
        app: chosenApp,
        role: chosenRole,
        unit: selectedUnit ? selectedUnit.unit : "",
        structure: selectedUnit ? selectedUnit.structure : "",
        until: resolvedUntil,
        reason: reason.trim() || "Self-granted by administrator",
      });
      reread();
      toast({ body: t("Role granted immediately.") });
      setReason("");
      onSubmitted?.();
      onClose();
    } catch (problem) {
      setFailure(reasonOf(problem));
    } finally {
      setBusy(false);
    }
  };

  if (!isOpen) return null;

  return (
    <Dialog isOpen onOpenChange={(open) => !open && !busy && onClose()} width={580} purpose="form">
      <Layout
        height="fill"
        padding={4}
        header={
          <DialogHeader
            title={scope === "project" ? t("Request project access") : t("Request access")}
            onOpenChange={(open) => !open && !busy && onClose()}
          />
        }
        content={
          <LayoutContent>
            <VStack gap={4}>
              <Text type="supporting" color="secondary">
                {scope === "project"
                  ? t("Project owners review this request. Once approved, access applies immediately without signing in again.")
                  : t("Administrators review this request. Time-bounded grants expire automatically on their end date.")}
              </Text>
              {failure && <Banner status="error" title={t("Request refused")} description={failure} />}
              <FormLayout defaultOptionality="optional">
                {canPickScope && (
                  <SegmentedControl
                    label={t("Access target")}
                    value={scope}
                    onChange={(next) => setScope(next as "role" | "project")}
                  >
                    <SegmentedControlItem value="role" label={t("Application role")} />
                    <SegmentedControlItem value="project" label={t("Project")} />
                  </SegmentedControl>
                )}
                {scope === "role" ? (
                  <>
                    <Selector
                      label={t("Application")}
                      isRequired
                      isDisabled={Boolean(fixedApp) || busy}
                      hasSearch={requestableApps.length > 8}
                      value={chosenApp}
                      onChange={setApp}
                      options={requestableApps.map((entry) => ({
                        value: entry.id,
                        label: entry.title !== entry.id ? `${t(entry.title)} (${entry.id})` : t(entry.title),
                      }))}
                    />
                    <Selector
                      label={t("Role")}
                      isRequired
                      isDisabled={busy || availableRoles.length === 0}
                      value={chosenRole}
                      onChange={setRole}
                      options={availableRoles.map((entry) => ({
                        value: entry,
                        label: entry !== roleTitle(entry) ? `${roleTitle(entry)} (${entry})` : entry,
                      }))}
                    />
                    {unitChoices.length > 0 && (
                      <Selector
                        label={t("Organization unit scope")}
                        isOptional
                        isDisabled={busy}
                        hasSearch={unitChoices.length > 8}
                        value={unitKey}
                        onChange={setUnitKey}
                        options={[
                          { value: "", label: t("All units allowed by the role") },
                          ...unitChoices.map((item) => ({ value: item.value, label: item.label })),
                        ]}
                      />
                    )}
                  </>
                ) : (
                  <>
                    <Selector
                      label={t("Project")}
                      isRequired
                      isDisabled={Boolean(fixedProject) || busy || projects.length === 0}
                      hasSearch={projects.length > 8}
                      value={chosenProject}
                      onChange={setProject}
                      options={projects.map((entry) => ({
                        value: entry.id,
                        label: entry.title !== entry.id ? `${entry.title} (${entry.id})` : entry.title,
                      }))}
                    />
                    <Selector
                      label={t("Project role")}
                      isRequired
                      isDisabled={busy}
                      value={chosenRole}
                      onChange={setRole}
                      options={projectRoles.map((entry) => ({ value: entry, label: roleTitle(entry) }))}
                    />
                  </>
                )}
                <SegmentedControl
                  label={t("Duration")}
                  value={preset}
                  onChange={(next) => setPreset(next as DurationPreset)}
                >
                  <SegmentedControlItem value="7d" label={t("7 days")} />
                  <SegmentedControlItem value="30d" label={t("30 days")} />
                  <SegmentedControlItem value="90d" label={t("90 days")} />
                  {scope === "role" && <SegmentedControlItem value="permanent" label={t("No end")} />}
                  <SegmentedControlItem value="custom" label={t("Custom date")} />
                </SegmentedControl>
                {preset === "custom" && (
                  <DateInput
                    label={t("Access ends on")}
                    isRequired
                    min={minDate}
                    value={asIsoDate(customUntil)}
                    format="system_date"
                    onChange={(next) => setCustomUntil((next ?? "") as typeof customUntil)}
                  />
                )}
                <TextArea
                  label={t("Business reason")}
                  isRequired
                  rows={3}
                  maxLength={2000}
                  value={reason}
                  isDisabled={busy}
                  placeholder={t("Explain what work requires this access and for how long.")}
                  onChange={setReason}
                />
              </FormLayout>
            </VStack>
          </LayoutContent>
        }
        footer={
          <LayoutFooter>
            <HStack gap={2} vAlign="center" wrap="wrap">
              <StackItem size="fill">
                {resolvedUntil && (
                  <Text type="supporting" color="secondary">
                    {t("Expires on {date}").replace("{date}", resolvedUntil)}
                  </Text>
                )}
              </StackItem>
              <Button label={t("Cancel")} variant="secondary" isDisabled={busy} onClick={onClose} />
              {isAdmin && scope === "role" && grantAction && (
                <Button
                  label={t("Grant to myself")}
                  variant="secondary"
                  isDisabled={busy || !validTarget || !validUntil}
                  isLoading={busy}
                  onClick={() => void selfGrant()}
                />
              )}
              <Button
                label={t("Submit request")}
                variant="primary"
                isDisabled={!canSubmit}
                isLoading={busy}
                onClick={() => void submitRequest()}
              />
            </HStack>
          </LayoutFooter>
        }
      />
    </Dialog>
  );
}

export function RequestAccessButton({
  label,
  variant = "secondary",
  size = "md",
  fixedApp,
  fixedProject,
  initialScope,
  initialRole,
}: {
  label?: string;
  variant?: "primary" | "secondary" | "ghost";
  size?: "sm" | "md" | "lg";
  fixedApp?: string;
  fixedProject?: string;
  initialScope?: "role" | "project";
  initialRole?: string;
}) {
  const [open, setOpen] = useState(false);
  return (
    <>
      <Button label={label ?? t("Request access")} variant={variant} size={size} onClick={() => setOpen(true)} />
      <RequestAccessDialog
        isOpen={open}
        onClose={() => setOpen(false)}
        fixedApp={fixedApp}
        fixedProject={fixedProject}
        initialScope={initialScope}
        initialRole={initialRole}
      />
    </>
  );
}
