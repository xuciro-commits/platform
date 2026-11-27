import { AlertDialog } from "@astryxdesign/core/AlertDialog";
import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { Card } from "@astryxdesign/core/Card";
import { DateInput } from "@astryxdesign/core/DateInput";
import { Dialog, DialogHeader } from "@astryxdesign/core/Dialog";
import { FormLayout } from "@astryxdesign/core/FormLayout";
import { Icon } from "@astryxdesign/core/Icon";
import { IconButton } from "@astryxdesign/core/IconButton";
import { HStack, Layout, LayoutContent, LayoutFooter, LayoutHeader, VStack } from "@astryxdesign/core/Layout";
import { List, ListItem } from "@astryxdesign/core/List";
import { MultiSelector } from "@astryxdesign/core/MultiSelector";
import { Selector } from "@astryxdesign/core/Selector";
import { Skeleton } from "@astryxdesign/core/Skeleton";
import { Table, pixel, proportional } from "@astryxdesign/core/Table";
import type { TableColumn } from "@astryxdesign/core/Table";
import { Heading, Text } from "@astryxdesign/core/Text";
import { TextInput } from "@astryxdesign/core/TextInput";
import { useToast } from "@astryxdesign/core/Toast";
import { Token } from "@astryxdesign/core/Token";
import { ArrowPathIcon } from "@heroicons/react/24/outline";
import { useEffect, useMemo, useState } from "react";
import { type Api, reread, useRead, reasonOf, send } from "../../host";
import { t } from "../../i18n";
import { useIsDark } from "../../shell/appearance";
import { PageTitle, useGutter } from "../frame";
import { PersonStack, TagListValue } from "../../semantic";
import { AnatomyPlate, MetricStrip } from "./RebacVisuals";
import { addDaysISO, asIsoDate, roleTitle, todayISO, useMemberDirectoryMap, useUnitChoices } from "./shared";

function slugTeamId(input: string): string {
  return input.toLowerCase().replace(/[^a-z0-9-]+/g, "-").replace(/^-+|-+$/g, "").slice(0, 48);
}

function TeamDialog({
  isOpen,
  editing,
  apps,
  memberOptions,
  saveAction,
  onClose,
}: {
  isOpen: boolean;
  editing: Api.Team | null;
  apps: Api.AppInfo[];
  memberOptions: { value: string; label: string }[];
  saveAction: Api.Action;
  onClose: () => void;
}) {
  const toast = useToast();
  const { choices: unitChoices, unitName } = useUnitChoices();
  const [name, setName] = useState(editing?.name ?? "");
  const [teamId, setTeamId] = useState(editing?.id ?? "");
  const [selectedMembers, setSelectedMembers] = useState<string[]>(editing?.members ?? []);
  const [grants, setGrants] = useState<Api.Grant[]>(editing?.grants ?? []);

  const [grantApp, setGrantApp] = useState("");
  const [grantRole, setGrantRole] = useState("");
  const [grantUnitKey, setGrantUnitKey] = useState("");
  const [grantUntil, setGrantUntil] = useState("");
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState("");

  useEffect(() => {
    if (!isOpen) return;
    setName(editing?.name ?? "");
    setTeamId(editing?.id ?? "");
    setSelectedMembers(editing?.members ?? []);
    setGrants(editing?.grants ?? []);
    setGrantUnitKey("");
    setGrantUntil("");
    setFailure("");
  }, [isOpen, editing]);

  const grantableApps = useMemo(
    () => apps.filter((entry) => entry.roles.some((r) => r !== "*")),
    [apps],
  );
  const chosenApp = grantableApps.some((item) => item.id === grantApp) ? grantApp : grantableApps[0]?.id ?? "";
  const appRoles = useMemo(
    () => (grantableApps.find((item) => item.id === chosenApp)?.roles ?? []).filter((r) => r !== "*"),
    [grantableApps, chosenApp],
  );
  const chosenRole = appRoles.includes(grantRole) ? grantRole : appRoles[0] ?? "";
  const minDate = addDaysISO(1, todayISO());

  const addGrant = () => {
    if (!chosenApp || !chosenRole) return;
    const unitChoice = unitChoices.find((item) => item.value === grantUnitKey);
    const exists = grants.some(
      (g) => g.app === chosenApp && g.role === chosenRole && (g.unit ?? "") === (unitChoice?.unit ?? ""),
    );
    if (exists) return;
    setGrants([
      ...grants,
      {
        app: chosenApp,
        role: chosenRole,
        ...(unitChoice ? { unit: unitChoice.unit, structure: unitChoice.structure } : {}),
        ...(grantUntil ? { until: grantUntil } : {}),
      },
    ]);
    setGrantUnitKey("");
    setGrantUntil("");
  };

  const effectiveId = editing ? editing.id : slugTeamId(teamId || name) || `team-${crypto.randomUUID().slice(0, 6)}`;
  const canSave = !busy && Boolean(name.trim() && effectiveId);

  const submitTeam = async () => {
    if (!canSave) return;
    setBusy(true);
    setFailure("");
    try {
      await send(saveAction, effectiveId, {
        name: name.trim(),
        members: selectedMembers,
        grants,
      });
      reread();
      toast({ body: editing ? t("Team updated.") : t("Team created.") });
      onClose();
    } catch (problem) {
      setFailure(reasonOf(problem));
    } finally {
      setBusy(false);
    }
  };

  if (!isOpen) return null;

  return (
    <Dialog isOpen onOpenChange={(open) => !open && !busy && onClose()} width={680} purpose="form">
      <Layout
        height="fill"
        padding={4}
        header={
          <DialogHeader
            title={editing ? t("Edit team") : t("Create team")}
            onOpenChange={(open) => !open && !busy && onClose()}
          />
        }
        content={
          <LayoutContent>
            <VStack gap={4}>
              <Text type="supporting" color="secondary">
                {t("Every member of a team automatically inherits its role grants and security clearances while they belong to it.")}
              </Text>
              {failure && <Banner status="error" title={t("Not saved")} description={failure} />}
              <FormLayout defaultOptionality="optional">
                <TextInput
                  label={t("Team name")}
                  isRequired
                  isDisabled={busy}
                  value={name}
                  placeholder={t("North America Enterprise Sales")}
                  onChange={(next) => {
                    setName(next);
                    if (!editing && !teamId) setTeamId(slugTeamId(next));
                  }}
                />
                {!editing && (
                  <TextInput
                    label={t("Team key")}
                    isRequired
                    isDisabled={busy}
                    value={teamId}
                    placeholder={t("na-enterprise-sales")}
                    onChange={(next) => setTeamId(slugTeamId(next))}
                  />
                )}
                <MultiSelector
                  label={t("Members")}
                  hasSearch
                  hasSelectAll
                  hasClear
                  triggerDisplay="badges"
                  maxBadges={4}
                  isDisabled={busy}
                  value={selectedMembers}
                  options={memberOptions}
                  onChange={setSelectedMembers}
                />
              </FormLayout>

              <VStack gap={2}>
                <Heading level={4}>{t("Inherited role grants")}</Heading>
                {grants.length > 0 ? (
                  <List hasDividers>
                    {grants.map((grant, index) => {
                      const appTitle = apps.find((item) => item.id === grant.app)?.title || grant.app;
                      return (
                        <ListItem
                          key={`${grant.app}:${grant.role}:${grant.unit ?? ""}:${index}`}
                          label={`${t(appTitle)} · ${roleTitle(grant.role)}`}
                          description={[
                            grant.unit ? `${t("Unit")}: ${unitName(grant.unit)}` : t("All units allowed by the role"),
                            grant.until ? t("Until {date}").replace("{date}", grant.until) : t("No end"),
                          ].join(" · ")}
                          endContent={
                            <Button
                              label={t("Remove")}
                              variant="secondary"
                              size="sm"
                              isDisabled={busy}
                              onClick={() => setGrants(grants.filter((_, idx) => idx !== index))}
                            />
                          }
                        />
                      );
                    })}
                  </List>
                ) : (
                  <Text type="supporting" color="secondary">{t("No role grants attached to this team yet.")}</Text>
                )}

                {grantableApps.length > 0 && (
                  <HStack gap={2} vAlign="end" wrap="wrap">
                    <Selector
                      label={t("Application")}
                      width={180}
                      value={chosenApp}
                      onChange={setGrantApp}
                      options={grantableApps.map((entry) => ({
                        value: entry.id,
                        label: t(entry.title || entry.id),
                      }))}
                    />
                    <Selector
                      label={t("Role")}
                      width={150}
                      value={chosenRole}
                      onChange={setGrantRole}
                      options={appRoles.map((entry) => ({ value: entry, label: roleTitle(entry) }))}
                    />
                    {unitChoices.length > 0 && (
                      <Selector
                        label={t("Unit scope")}
                        width={180}
                        value={grantUnitKey}
                        onChange={setGrantUnitKey}
                        options={[
                          { value: "", label: t("All units") },
                          ...unitChoices.map((item) => ({ value: item.value, label: item.label })),
                        ]}
                      />
                    )}
                    <DateInput
                      label={t("Until")}
                      hasClear
                      min={minDate}
                      width={170}
                      format="system_date"
                      value={asIsoDate(grantUntil)}
                      onChange={(next) => setGrantUntil(next ?? "")}
                    />
                    <Button
                      label={t("Add grant")}
                      variant="secondary"
                      size="sm"
                      isDisabled={busy || !chosenApp || !chosenRole}
                      onClick={addGrant}
                    />
                  </HStack>
                )}
              </VStack>
            </VStack>
          </LayoutContent>
        }
        footer={
          <LayoutFooter>
            <HStack gap={2} hAlign="end">
              <Button label={t("Cancel")} variant="secondary" isDisabled={busy} onClick={onClose} />
              <Button label={t("Save team")} variant="primary" isDisabled={!canSave} isLoading={busy} onClick={() => void submitTeam()} />
            </HStack>
          </LayoutFooter>
        }
      />
    </Dialog>
  );
}

export function TeamsPage() {
  const gutter = useGutter();
  const dark = useIsDark();
  const toast = useToast();
  const access = useRead<Api.AccessConfig>("/v1/access");
  const apps = useRead<Api.AppInfo[]>("/v1/apps");
  const catalog = useRead<Api.Action[]>("/v1/actions");
  const { options: memberOptions, nameOf: memberName } = useMemberDirectoryMap();
  const { unitName } = useUnitChoices();

  const [dialogOpen, setDialogOpen] = useState(false);
  const [editing, setEditing] = useState<Api.Team | null>(null);
  const [removing, setRemoving] = useState<Api.Team | null>(null);
  const [busy, setBusy] = useState(false);

  const teams = access.status === "ready" ? access.data.teams : [];
  const appList = apps.status === "ready" ? apps.data : [];
  const appTitle = (id: string) => appList.find((item) => item.id === id)?.title || id;
  const actions = catalog.status === "ready" ? catalog.data : [];
  const saveAction = actions.find((item) => item.schema === "platform.team.save");
  const removeAction = actions.find((item) => item.schema === "platform.team.remove");

  const metrics = useMemo(() => {
    const distinctMembers = new Set<string>();
    let totalGrants = 0;
    for (const team of teams) {
      for (const m of team.members) distinctMembers.add(m);
      totalGrants += team.grants.length;
    }
    return [
      { key: "teams", label: t("Active teams"), value: teams.length, caption: t("Shared access & clearance groups"), dot: "accent" as const },
      { key: "members", label: t("Covered members"), value: distinctMembers.size, caption: t("Colleagues receiving group access"), dot: "success" as const },
      { key: "grants", label: t("Inherited role bundles"), value: totalGrants, caption: t("Automatic unit & time-bounded grants"), dot: totalGrants > 0 ? ("accent" as const) : ("neutral" as const) },
    ];
  }, [teams]);

  const columns: TableColumn<Api.Team>[] = [
    {
      key: "name",
      header: t("Team"),
      width: proportional(1.2),
      renderCell: (team) => (
        <VStack gap={0.5}>
          <Text weight="medium" maxLines={1}>{team.name}</Text>
          <Text type="code" color="secondary" maxLines={1}>{team.id}</Text>
        </VStack>
      ),
    },
    {
      key: "members",
      header: t("Members"),
      width: proportional(1.6),
      renderCell: (team) =>
        team.members.length > 0 ? (
          <HStack gap={2} vAlign="center" wrap="wrap">
            <PersonStack items={team.members.map((id) => ({ id, name: memberName(id) }))} max={3} size="xs" />
            <TagListValue items={team.members.map((id) => memberName(id))} max={2} size="sm" />
          </HStack>
        ) : (
          <Text color="secondary">{t("No members")}</Text>
        ),
    },
    {
      key: "grants",
      header: t("Inherited role grants"),
      width: proportional(1.8),
      renderCell: (team) =>
        team.grants.length > 0 ? (
          <HStack gap={1} wrap="wrap">
            {team.grants.map((grant, index) => (
              <Token
                key={`${grant.app}:${grant.role}:${index}`}
                size="sm"
                color={grant.until ? "blue" : "teal"}
                label={`${t(appTitle(grant.app))}: ${roleTitle(grant.role)}${grant.unit ? ` @ ${unitName(grant.unit)}` : ""}${grant.until ? ` → ${grant.until}` : ""}`}
              />
            ))}
          </HStack>
        ) : (
          <Token size="sm" color="gray" label={t("Clearance-only group")} />
        ),
    },
    {
      key: "actions",
      header: t("Actions"),
      width: pixel(152),
      align: "end",
      renderCell: (team) => (
        <HStack gap={1.5} hAlign="end">
          {saveAction && (
            <Button
              label={t("Edit")}
              variant="secondary"
              size="sm"
              onClick={() => {
                setEditing(team);
                setDialogOpen(true);
              }}
            />
          )}
          {removeAction && (
            <Button label={t("Remove")} variant="secondary" size="sm" onClick={() => setRemoving(team)} />
          )}
        </HStack>
      ),
    },
  ];

  const confirmRemove = async () => {
    if (!removeAction || !removing) return;
    setBusy(true);
    try {
      await send(removeAction, removing.id, {});
      reread();
      setRemoving(null);
      toast({ body: t("Team removed.") });
    } catch (problem) {
      toast({ body: reasonOf(problem), type: "error" });
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      <Layout
        height="fill"
        header={
          <LayoutHeader padding={0} hasDivider>
            <VStack gap={2} padding={gutter} paddingBlockStart={3} paddingBlockEnd={4}>
              <PageTitle
                title={t("Teams")}
                actions={
                  <HStack gap={2} vAlign="center">
                    <IconButton
                      label={t("Refresh")}
                      tooltip={t("Refresh")}
                      variant="ghost"
                      size="sm"
                      icon={<Icon icon={ArrowPathIcon} size="sm" />}
                      onClick={reread}
                    />
                    {saveAction && (
                      <Button
                        label={t("New")}
                        variant="primary"
                        onClick={() => {
                          setEditing(null);
                          setDialogOpen(true);
                        }}
                      />
                    )}
                  </HStack>
                }
              >
                <Text type="supporting" color="secondary">
                  {t("Group members so they inherit unit-scoped or time-bounded role grants and security markings together.")}
                </Text>
              </PageTitle>
            </VStack>
          </LayoutHeader>
        }
        content={
          <LayoutContent padding={gutter} role="main">
            <VStack gap={4}>
              {access.status === "loading" && <Skeleton height={200} />}
              {(access.status === "error" || access.status === "refused") && (
                <Banner status="error" title={t("Teams unavailable")} description={access.reason} />
              )}
              {access.status === "ready" && <MetricStrip items={metrics} />}
              {access.status === "ready" && teams.length === 0 && (
                <Card variant="muted" padding={6}>
                  <VStack gap={3} hAlign="center">
                    <AnatomyPlate kind="gate" dark={dark} />
                    <Heading level={2}>{t("Nothing here yet")}</Heading>
                    <Text type="supporting" color="secondary">
                      {t("Create a team to assign roles and markings to a group of colleagues in one step.")}
                    </Text>
                    {saveAction && (
                      <Button
                        label={t("New")}
                        variant="primary"
                        onClick={() => {
                          setEditing(null);
                          setDialogOpen(true);
                        }}
                      />
                    )}
                  </VStack>
                </Card>
              )}
              {teams.length > 0 && (
                <Table
                  data={teams}
                  columns={columns}
                  idKey="id"
                  dividers="rows"
                  hasHover
                  textOverflow="truncate"
                />
              )}
            </VStack>
          </LayoutContent>
        }
      />

      {saveAction && (
        <TeamDialog
          isOpen={dialogOpen}
          editing={editing}
          apps={appList}
          memberOptions={memberOptions}
          saveAction={saveAction}
          onClose={() => setDialogOpen(false)}
        />
      )}

      <AlertDialog
        isOpen={Boolean(removing)}
        title={t("Remove team?")}
        description={t("Members of this team immediately lose the role grants and clearances inherited through it.")}
        cancelLabel={t("Cancel")}
        actionLabel={t("Remove")}
        actionVariant="destructive"
        isActionLoading={busy}
        onOpenChange={(open) => !open && !busy && setRemoving(null)}
        onAction={() => void confirmRemove()}
      />
    </>
  );
}
