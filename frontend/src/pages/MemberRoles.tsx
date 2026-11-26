import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { DateInput } from "@astryxdesign/core/DateInput";
import { HStack, VStack } from "@astryxdesign/core/Layout";
import { List, ListItem } from "@astryxdesign/core/List";
import { SegmentedControl, SegmentedControlItem } from "@astryxdesign/core/SegmentedControl";
import { Selector } from "@astryxdesign/core/Selector";
import { Text } from "@astryxdesign/core/Text";
import { TextInput } from "@astryxdesign/core/TextInput";
import { useToast } from "@astryxdesign/core/Toast";
import { Token } from "@astryxdesign/core/Token";
import { useMemo, useState } from "react";
import { type Api, reread, useRead } from "../host";
import { t } from "../i18n";
import { reasonOf, send } from "./act";
import {
  type DurationPreset,
  addDaysISO,
  asIsoDate,
  daysUntil,
  grantSourceSummary,
  roleTitle,
  todayISO,
  untilFromPreset,
  useMemberDirectoryMap,
  useUnitChoices,
} from "./rebac/shared";

export function MemberRoles({ member }: { member: Api.MemberView }) {
  const toast = useToast();
  const apps = useRead<Api.AppInfo[]>("/v1/apps");
  const catalog = useRead<Api.Action[]>("/v1/actions");
  const { nameOf: memberName } = useMemberDirectoryMap();
  const { choices: unitChoices, unitName } = useUnitChoices();

  const [selectedApp, setSelectedApp] = useState("");
  const [selectedRole, setSelectedRole] = useState("");
  const [unitKey, setUnitKey] = useState("");
  const [preset, setPreset] = useState<DurationPreset>("permanent");
  const [startFrom, setStartFrom] = useState("");
  const [customUntil, setCustomUntil] = useState(() => addDaysISO(30));
  const [reason, setReason] = useState("");
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState("");

  const today = todayISO(member.timezone);
  const minUntil = addDaysISO(1, startFrom && startFrom > today ? startFrom : today);

  const grantableApps = useMemo(() => {
    if (apps.status !== "ready") return [];
    return apps.data
      .map((entry) => ({
        id: entry.id,
        title: entry.title || entry.id,
        roles: entry.roles.filter((r) => r !== "*"),
      }))
      .filter((entry) => entry.roles.length > 0);
  }, [apps]);

  const titleByApp = useMemo(() => {
    const map = new Map<string, string>();
    for (const entry of grantableApps) map.set(entry.id, entry.title);
    return map;
  }, [grantableApps]);

  const chosenApp = grantableApps.some((entry) => entry.id === selectedApp) ? selectedApp : grantableApps[0]?.id ?? "";
  const availableRoles = useMemo(
    () => grantableApps.find((entry) => entry.id === chosenApp)?.roles ?? [],
    [grantableApps, chosenApp],
  );
  const chosenRole = availableRoles.includes(selectedRole) ? selectedRole : availableRoles[0] ?? "";
  const chosenUnit = unitChoices.find((item) => item.value === unitKey);
  const resolvedUntil = untilFromPreset(preset, customUntil, startFrom && startFrom > today ? startFrom : today);

  const activeGrants = useMemo<Api.Grant[]>(() => {
    if ((member.grants?.length ?? 0) > 0) return member.grants ?? [];
    return Object.entries(member.roles ?? {})
      .filter(([, role]) => Boolean(role))
      .map(([app, role]) => ({ app, role }));
  }, [member]);

  const scheduledGrants = member.scheduledGrants ?? [];
  const expiredGrants = member.expiredGrants ?? [];

  const actions = catalog.status === "ready" ? catalog.data : [];
  const grantAction = actions.find((action) => action.schema === "platform.member.grant");
  const revokeAction = actions.find((action) => action.schema === "platform.member.revoke");

  const runGrant = async () => {
    if (!grantAction || !chosenApp || !chosenRole) return;
    setBusy(true);
    setFailure("");
    try {
      await send(grantAction, member.id, {
        app: chosenApp,
        role: chosenRole,
        unit: chosenUnit ? chosenUnit.unit : "",
        structure: chosenUnit ? chosenUnit.structure : "",
        from: startFrom || "",
        until: resolvedUntil,
        reason: reason.trim(),
      });
      setReason("");
      setStartFrom("");
      reread();
      toast({ body: t("Role granted.") });
    } catch (problem) {
      setFailure(reasonOf(problem));
    } finally {
      setBusy(false);
    }
  };

  const runRevoke = async (grant: Api.Grant) => {
    if (!revokeAction) return;
    setBusy(true);
    setFailure("");
    try {
      await send(revokeAction, member.id, {
        app: grant.app,
        role: grant.role,
        unit: grant.unit ?? "",
        by: grant.by ?? "",
      });
      reread();
      toast({ body: t("Role grant revoked.") });
    } catch (problem) {
      setFailure(reasonOf(problem));
    } finally {
      setBusy(false);
    }
  };

  return (
    <VStack gap={4}>
      {failure && <Banner status="error" title={t("Not saved")} description={failure} />}

      {(member.markings?.length ?? 0) > 0 && (
        <HStack gap={1.5} vAlign="center" wrap="wrap">
          <Text type="supporting" color="secondary">{t("Cleared markings")}:</Text>
          {(member.markings ?? []).map((marking) => (
            <Token key={marking} size="sm" color="teal" label={marking} />
          ))}
        </HStack>
      )}

      {activeGrants.length > 0 ? (
        <List header={t("Active roles")} hasDividers>
          {activeGrants.map((grant, index) => {
            const origin = grantSourceSummary(grant, memberName);
            const appLabel = titleByApp.get(grant.app) || grant.app;
            const left = daysUntil(grant.until, today);
            return (
              <ListItem
                key={`${grant.app}:${grant.role}:${grant.unit ?? ""}:${grant.by ?? ""}:${index}`}
                label={`${t(appLabel)} · ${roleTitle(grant.role)}`}
                description={[
                  origin.label,
                  grant.unit ? `${t("Unit")}: ${unitName(grant.unit)}` : "",
                  grant.reason && !grant.reason.startsWith("delegated") && origin.kind === "direct" ? grant.reason : "",
                ].filter(Boolean).join(" · ")}
                endContent={
                  <HStack gap={1.5} vAlign="center" wrap="wrap">
                    {grant.until ? (
                      <Token
                        size="sm"
                        color={left !== null && left <= 7 ? "orange" : "blue"}
                        label={t("Until {date}").replace("{date}", grant.until)}
                      />
                    ) : (
                      <Token size="sm" color="gray" label={t("No end")} />
                    )}
                    {revokeAction && origin.revocable && (
                      <Button
                        label={t("Revoke")}
                        variant="secondary"
                        size="sm"
                        isDisabled={busy}
                        onClick={() => void runRevoke(grant)}
                      />
                    )}
                  </HStack>
                }
              />
            );
          })}
        </List>
      ) : (
        <Text type="supporting" color="secondary">{t("No active role grants.")}</Text>
      )}

      {scheduledGrants.length > 0 && (
        <List header={t("Scheduled grants")} hasDividers>
          {scheduledGrants.map((grant, index) => {
            const appLabel = titleByApp.get(grant.app) || grant.app;
            return (
              <ListItem
                key={`sched:${grant.app}:${grant.role}:${grant.from ?? ""}:${index}`}
                label={`${t(appLabel)} · ${roleTitle(grant.role)}`}
                description={[
                  grant.from ? t("Starts {date}").replace("{date}", grant.from) : "",
                  grant.until ? t("Until {date}").replace("{date}", grant.until) : t("No end"),
                  grant.unit ? `${t("Unit")}: ${unitName(grant.unit)}` : "",
                ].filter(Boolean).join(" · ")}
                endContent={
                  revokeAction ? (
                    <Button
                      label={t("Cancel")}
                      variant="secondary"
                      size="sm"
                      isDisabled={busy}
                      onClick={() => void runRevoke(grant)}
                    />
                  ) : undefined
                }
              />
            );
          })}
        </List>
      )}

      {expiredGrants.length > 0 && (
        <List header={t("Expired grants")} hasDividers>
          {expiredGrants.slice(0, 5).map((grant, index) => {
            const appLabel = titleByApp.get(grant.app) || grant.app;
            return (
              <ListItem
                key={`exp:${grant.app}:${grant.role}:${grant.until ?? ""}:${index}`}
                label={`${t(appLabel)} · ${roleTitle(grant.role)}`}
                description={[
                  grant.until ? t("Expired {date}").replace("{date}", grant.until) : "",
                  grant.unit ? `${t("Unit")}: ${unitName(grant.unit)}` : "",
                ].filter(Boolean).join(" · ")}
                endContent={<Token size="sm" color="gray" label={t("Expired")} />}
              />
            );
          })}
        </List>
      )}

      {grantAction && grantableApps.length > 0 && (
        <VStack gap={2}>
          <Text weight="medium">{t("Grant role")}</Text>
          <HStack gap={2} vAlign="end" wrap="wrap">
            <Selector
              label={t("Application")}
              width={180}
              value={chosenApp}
              onChange={setSelectedApp}
              options={grantableApps.map((entry) => ({ value: entry.id, label: t(entry.title) }))}
            />
            <Selector
              label={t("Role")}
              width={160}
              value={chosenRole}
              onChange={setSelectedRole}
              options={availableRoles.map((entry) => ({ value: entry, label: roleTitle(entry) }))}
            />
            {unitChoices.length > 0 && (
              <Selector
                label={t("Unit scope")}
                isOptional
                width={180}
                value={unitKey}
                onChange={setUnitKey}
                options={[
                  { value: "", label: t("All units") },
                  ...unitChoices.map((item) => ({ value: item.value, label: item.label })),
                ]}
              />
            )}
          </HStack>
          <SegmentedControl
            label={t("Duration")}
            size="sm"
            value={preset}
            onChange={(next) => setPreset(next as DurationPreset)}
          >
            <SegmentedControlItem value="permanent" label={t("No end")} />
            <SegmentedControlItem value="7d" label={t("7 days")} />
            <SegmentedControlItem value="30d" label={t("30 days")} />
            <SegmentedControlItem value="90d" label={t("90 days")} />
            <SegmentedControlItem value="custom" label={t("Custom")} />
          </SegmentedControl>
          {preset === "custom" && (
            <HStack gap={2} wrap="wrap">
              <DateInput
                label={t("Starts on")}
                isOptional
                hasClear
                width={170}
                format="system_date"
                value={asIsoDate(startFrom)}
                onChange={(next) => setStartFrom(next ?? "")}
              />
              <DateInput
                label={t("Ends on")}
                isRequired
                min={minUntil}
                width={170}
                format="system_date"
                value={asIsoDate(customUntil)}
                onChange={(next) => setCustomUntil((next ?? "") as typeof customUntil)}
              />
            </HStack>
          )}
          <HStack gap={2} vAlign="end" wrap="wrap">
            <TextInput
              label={t("Reason")}
              isOptional
              width={240}
              value={reason}
              placeholder={t("Project onboarding or coverage")}
              onChange={setReason}
            />
            <Button
              label={t("Grant role")}
              variant="primary"
              size="sm"
              isDisabled={busy || !chosenApp || !chosenRole || (preset === "custom" && !customUntil)}
              isLoading={busy}
              onClick={() => void runGrant()}
            />
          </HStack>
        </VStack>
      )}
    </VStack>
  );
}
