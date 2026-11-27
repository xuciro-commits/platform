import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { DateInput } from "@astryxdesign/core/DateInput";
import { HStack, StackItem, VStack } from "@astryxdesign/core/Layout";
import { List, ListItem } from "@astryxdesign/core/List";
import { SegmentedControl, SegmentedControlItem } from "@astryxdesign/core/SegmentedControl";
import { Selector } from "@astryxdesign/core/Selector";
import { Text } from "@astryxdesign/core/Text";
import { TextInput } from "@astryxdesign/core/TextInput";
import { Token } from "@astryxdesign/core/Token";
import { useToast } from "@astryxdesign/core/Toast";
import { useMemo, useState } from "react";
import { type Api, reread, useRead, reasonOf, send } from "../../host";
import { t } from "../../i18n";
import { SettingsCard } from "../SettingsRows";
import { RequestAccessDialog } from "./RequestAccessDialog";
import {
  addDaysISO,
  asIsoDate,
  daysUntil,
  grantSourceSummary,
  roleTitle,
  todayISO,
  useMemberDirectoryMap,
  useUnitChoices,
} from "./shared";

type DelegationPreset = "7d" | "14d" | "30d" | "custom";

export function SelfAccessPanel() {
  const toast = useToast();
  const me = useRead<Api.MeView>("/v1/me");
  const apps = useRead<Api.AppInfo[]>("/v1/apps");
  const requests = useRead<Api.AccessRequest[]>("/v1/access-requests");
  const catalog = useRead<Api.Action[]>("/v1/actions");
  const { options: memberOptions, nameOf: memberName } = useMemberDirectoryMap();
  const { unitName } = useUnitChoices();

  const [requestOpen, setRequestOpen] = useState(false);
  const [delegateTo, setDelegateTo] = useState("");
  const [delegateApp, setDelegateApp] = useState("");
  const [delegatePreset, setDelegatePreset] = useState<DelegationPreset>("7d");
  const [delegateCustomUntil, setDelegateCustomUntil] = useState(() => addDaysISO(7));
  const [delegateReason, setDelegateReason] = useState("");
  const [revokeTarget, setRevokeTarget] = useState("");
  const [revokeApp, setRevokeApp] = useState("");
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState("");

  const principalId = me.status === "ready" ? me.data.principalId : "";
  const zone = me.status === "ready" ? me.data.account.effective.timezone : undefined;
  const today = todayISO(zone);
  const minDate = addDaysISO(1, today);

  const appTitles = useMemo(() => {
    const map = new Map<string, string>();
    if (apps.status === "ready") {
      for (const item of apps.data) map.set(item.id, item.title || item.id);
    }
    return map;
  }, [apps]);

  const activeGrants = useMemo(() => {
    if (me.status !== "ready") return [];
    const profile = me.data.profile;
    const grants = profile.grants ?? [];
    if (grants.length > 0) return grants;
    return Object.entries(profile.roles ?? {})
      .filter(([, role]) => Boolean(role))
      .map(([app, role]): Api.Grant => ({ app, role }));
  }, [me]);

  const delegatableApps = useMemo(() => {
    const seen = new Set<string>();
    const list: { value: string; label: string }[] = [];
    for (const grant of activeGrants) {
      if (grant.app === "platform" || seen.has(grant.app)) continue;
      seen.add(grant.app);
      const title = appTitles.get(grant.app) ?? grant.app;
      list.push({ value: grant.app, label: title !== grant.app ? `${t(title)} (${grant.app})` : t(title) });
    }
    return list;
  }, [activeGrants, appTitles]);

  const colleagues = useMemo(
    () => memberOptions.filter((option) => option.value !== principalId),
    [memberOptions, principalId],
  );

  const ownRequests = useMemo(() => {
    if (requests.status !== "ready") return [];
    return requests.data.filter((item) => item.requester === principalId);
  }, [requests, principalId]);

  const actions = catalog.status === "ready" ? catalog.data : [];
  const withdrawAction = actions.find((item) => item.schema === "platform.access.withdraw");
  const delegateAction = actions.find((item) => item.schema === "platform.member.delegate");
  const undelegateAction = actions.find((item) => item.schema === "platform.member.undelegate");

  const chosenDelegateApp = delegatableApps.some((item) => item.value === delegateApp) ? delegateApp : delegatableApps[0]?.value ?? "";
  const chosenRevokeApp = delegatableApps.some((item) => item.value === revokeApp) ? revokeApp : delegatableApps[0]?.value ?? "";
  const resolvedDelegateUntil =
    delegatePreset === "7d" ? addDaysISO(7, today)
    : delegatePreset === "14d" ? addDaysISO(14, today)
    : delegatePreset === "30d" ? addDaysISO(30, today)
    : delegateCustomUntil;

  const withdraw = async (id: string) => {
    if (!withdrawAction) return;
    setBusy(true);
    setFailure("");
    try {
      await send(withdrawAction, id, {});
      reread();
      toast({ body: t("Access request withdrawn.") });
    } catch (problem) {
      setFailure(reasonOf(problem));
    } finally {
      setBusy(false);
    }
  };

  const submitDelegation = async () => {
    if (!delegateAction || !delegateTo || !chosenDelegateApp || !resolvedDelegateUntil || resolvedDelegateUntil <= today) return;
    setBusy(true);
    setFailure("");
    try {
      await send(delegateAction, delegateTo, {
        app: chosenDelegateApp,
        until: resolvedDelegateUntil,
        reason: delegateReason.trim(),
      });
      reread();
      setDelegateReason("");
      toast({
        body: t("Delegated {app} roles until {date}.")
          .replace("{app}", appTitles.get(chosenDelegateApp) ?? chosenDelegateApp)
          .replace("{date}", resolvedDelegateUntil),
      });
    } catch (problem) {
      setFailure(reasonOf(problem));
    } finally {
      setBusy(false);
    }
  };

  const endDelegation = async () => {
    if (!undelegateAction || !revokeTarget || !chosenRevokeApp) return;
    setBusy(true);
    setFailure("");
    try {
      await send(undelegateAction, revokeTarget, { app: chosenRevokeApp });
      reread();
      toast({ body: t("Delegation ended.") });
    } catch (problem) {
      setFailure(reasonOf(problem));
    } finally {
      setBusy(false);
    }
  };

  const markings = me.status === "ready" ? me.data.profile.markings ?? [] : [];

  return (
    <VStack gap={5}>
      {failure && <Banner status="error" title={t("Action failed")} description={failure} />}

      <SettingsCard title={t("My roles & clearances")}>
        <VStack gap={3} padding={3}>
          <HStack gap={2} vAlign="center" wrap="wrap">
            <StackItem size="fill">
              <Text type="supporting" color="secondary">
                {t("Roles come from direct assignments, teams, projects or temporary delegations.")}
              </Text>
            </StackItem>
            <Button label={t("Request access")} variant="primary" size="sm" onClick={() => setRequestOpen(true)} />
          </HStack>

          {activeGrants.length > 0 ? (
            <List hasDividers>
              {activeGrants.map((grant, index) => {
                const origin = grantSourceSummary(grant, memberName);
                const appLabel = appTitles.get(grant.app) ?? grant.app;
                const remaining = daysUntil(grant.until, today);
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
                            color={remaining !== null && remaining <= 7 ? "orange" : "blue"}
                            label={t("Until {date}").replace("{date}", grant.until)}
                          />
                        ) : (
                          <Token size="sm" color="gray" label={t("No end")} />
                        )}
                      </HStack>
                    }
                  />
                );
              })}
            </List>
          ) : (
            <Text type="supporting" color="secondary">{t("You do not hold any application roles yet.")}</Text>
          )}

          {markings.length > 0 && (
            <HStack gap={1.5} vAlign="center" wrap="wrap">
              <Text type="supporting" color="secondary">{t("Cleared markings")}:</Text>
              {markings.map((marking) => (
                <Token key={marking} size="sm" color="teal" label={marking} />
              ))}
            </HStack>
          )}
        </VStack>
      </SettingsCard>

      <SettingsCard title={t("My access requests")}>
        <VStack gap={3} padding={3}>
          {ownRequests.length > 0 ? (
            <List hasDividers>
              {ownRequests.map((item) => {
                const targetLabel = item.scope === "project"
                  ? `${t("Project")} ${item.project} · ${roleTitle(item.role ?? "")}`
                  : `${t(appTitles.get(item.app ?? "") ?? item.app ?? "")} · ${roleTitle(item.role ?? "")}${item.unit ? ` (${unitName(item.unit)})` : ""}`;
                const color = item.state === "approved" ? "green" : item.state === "rejected" ? "red" : item.state === "pending" ? "orange" : "gray";
                return (
                  <ListItem
                    key={item.id}
                    label={targetLabel}
                    description={[
                      item.reason,
                      item.until ? t("Until {date}").replace("{date}", item.until) : t("No end"),
                      item.decidedBy ? `${t("Decided by")} ${memberName(item.decidedBy)}` : "",
                      item.outcome || "",
                    ].filter(Boolean).join(" · ")}
                    endContent={
                      <HStack gap={2} vAlign="center">
                        <Token size="sm" color={color} label={t(item.state)} />
                        {item.state === "pending" && withdrawAction && (
                          <Button
                            label={t("Withdraw")}
                            variant="secondary"
                            size="sm"
                            isDisabled={busy}
                            onClick={() => void withdraw(item.id)}
                          />
                        )}
                      </HStack>
                    }
                  />
                );
              })}
            </List>
          ) : (
            <Text type="supporting" color="secondary">{t("You have not submitted any access requests yet.")}</Text>
          )}
        </VStack>
      </SettingsCard>

      {delegateAction && delegatableApps.length > 0 && colleagues.length > 0 && (
        <SettingsCard title={t("Temporary role delegation")}>
          <VStack gap={3} padding={3}>
            <Text type="supporting" color="secondary">
              {t("Delegate the roles you hold in an application to a colleague during leave or handover. Access ends automatically on the chosen date.")}
            </Text>
            <HStack gap={2} vAlign="end" wrap="wrap">
              <Selector
                label={t("Colleague")}
                width={220}
                hasSearch
                value={delegateTo}
                onChange={setDelegateTo}
                options={colleagues}
              />
              <Selector
                label={t("Application")}
                width={200}
                value={chosenDelegateApp}
                onChange={setDelegateApp}
                options={delegatableApps}
              />
              <SegmentedControl
                label={t("Duration")}
                size="sm"
                value={delegatePreset}
                onChange={(next) => setDelegatePreset(next as DelegationPreset)}
              >
                <SegmentedControlItem value="7d" label={t("7 days")} />
                <SegmentedControlItem value="14d" label={t("14 days")} />
                <SegmentedControlItem value="30d" label={t("30 days")} />
                <SegmentedControlItem value="custom" label={t("Custom")} />
              </SegmentedControl>
            </HStack>
            {delegatePreset === "custom" && (
              <DateInput
                label={t("Delegation ends on")}
                min={minDate}
                value={asIsoDate(delegateCustomUntil)}
                format="system_date"
                width={220}
                onChange={(next) => setDelegateCustomUntil((next ?? "") as typeof delegateCustomUntil)}
              />
            )}
            <HStack gap={2} vAlign="end" wrap="wrap">
              <TextInput
                label={t("Handover note")}
                isOptional
                width={320}
                value={delegateReason}
                placeholder={t("Annual leave coverage")}
                onChange={setDelegateReason}
              />
              <Button
                label={t("Delegate roles")}
                variant="primary"
                size="sm"
                isDisabled={busy || !delegateTo || !chosenDelegateApp || !resolvedDelegateUntil || resolvedDelegateUntil <= today}
                onClick={() => void submitDelegation()}
              />
            </HStack>
            {undelegateAction && (
              <HStack gap={2} vAlign="end" wrap="wrap">
                <Selector
                  label={t("End an active delegation to")}
                  width={220}
                  hasSearch
                  value={revokeTarget}
                  onChange={setRevokeTarget}
                  options={colleagues}
                />
                <Selector
                  label={t("In application")}
                  width={180}
                  value={chosenRevokeApp}
                  onChange={setRevokeApp}
                  options={delegatableApps}
                />
                <Button
                  label={t("End delegation")}
                  variant="secondary"
                  size="sm"
                  isDisabled={busy || !revokeTarget || !chosenRevokeApp}
                  onClick={() => void endDelegation()}
                />
              </HStack>
            )}
          </VStack>
        </SettingsCard>
      )}

      <RequestAccessDialog isOpen={requestOpen} onClose={() => setRequestOpen(false)} />
    </VStack>
  );
}
