import { AlertDialog } from "@astryxdesign/core/AlertDialog";
import { Badge } from "@astryxdesign/core/Badge";
import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { Card } from "@astryxdesign/core/Card";
import { Grid } from "@astryxdesign/core/Grid";
import { Icon } from "@astryxdesign/core/Icon";
import { IconButton } from "@astryxdesign/core/IconButton";
import { HStack, Layout, LayoutContent, LayoutHeader, VStack } from "@astryxdesign/core/Layout";
import { SelectableCard } from "@astryxdesign/core/SelectableCard";
import { Selector } from "@astryxdesign/core/Selector";
import { Skeleton } from "@astryxdesign/core/Skeleton";
import { Tab, TabList } from "@astryxdesign/core/TabList";
import { Table, pixel, proportional } from "@astryxdesign/core/Table";
import type { TableColumn } from "@astryxdesign/core/Table";
import { Heading, Text } from "@astryxdesign/core/Text";
import { useToast } from "@astryxdesign/core/Toast";
import { Token } from "@astryxdesign/core/Token";
import { ArrowPathIcon } from "@heroicons/react/24/outline";
import { useMemo, useState } from "react";
import { type Api, reread, useRead } from "../../host";
import { t } from "../../i18n";
import { useIsDark } from "../../shell/appearance";
import { reasonOf, send } from "../act";
import { PageTitle, useGutter } from "../frame";
import { CustomRoleDialog } from "./CustomRoleDialog";
import { AnatomyPlate, MetricStrip, ScopeDepthMeter } from "./RebacVisuals";
import { roleTitle } from "./shared";

type TabId = "matrix" | "custom";

export function RolesPermissionsPage() {
  const gutter = useGutter();
  const dark = useIsDark();
  const toast = useToast();
  const permissions = useRead<Api.AppPermissions[]>("/v1/permissions");
  const access = useRead<Api.AccessConfig>("/v1/access");
  const catalog = useRead<Api.Action[]>("/v1/actions");

  const [tab, setTab] = useState<TabId>("matrix");
  const [selectedApp, setSelectedApp] = useState("");
  const [spotlitRole, setSpotlitRole] = useState("");
  const [dialogOpen, setDialogOpen] = useState(false);
  const [editingRole, setEditingRole] = useState<Api.CustomRole | null>(null);
  const [removingRole, setRemovingRole] = useState<Api.CustomRole | null>(null);
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState("");

  const permList = permissions.status === "ready" ? permissions.data : [];
  const customRoles = access.status === "ready" ? access.data.roles : [];
  const customIds = useMemo(() => new Set(customRoles.map((item) => `${item.app}:${item.id}`)), [customRoles]);

  const actions = catalog.status === "ready" ? catalog.data : [];
  const saveAction = actions.find((item) => item.schema === "platform.role.save");
  const removeAction = actions.find((item) => item.schema === "platform.role.remove");

  const chosenApp = permList.some((entry) => entry.app === selectedApp)
    ? selectedApp
    : permList.find((entry) => (entry.objects?.length ?? 0) > 0 && entry.app !== "build")?.app ?? permList[0]?.app ?? "";

  const currentApp = permList.find((entry) => entry.app === chosenApp);
  const objects = currentApp?.objects ?? [];
  const activeSpotlight = currentApp?.roles.some((r) => r.role === spotlitRole) ? spotlitRole : "";

  const summaryMetrics = useMemo(() => {
    const rolesCount = currentApp?.roles.filter((r) => r.role !== "*").length ?? 0;
    const actionSet = new Set<string>();
    for (const r of currentApp?.roles ?? []) {
      for (const act of r.actions) actionSet.add(act.id);
    }
    const maskedFieldCount = objects.reduce((acc, obj) => acc + (obj.fields?.length ?? 0), 0);
    return [
      { key: "roles", label: t("Declared roles"), value: rolesCount, caption: t("Built-in & custom roles"), dot: "accent" as const },
      { key: "actions", label: t("Action capabilities"), value: actionSet.size, caption: t("Gated write & workflow verbs"), dot: "success" as const },
      { key: "objects", label: t("ReBAC object types"), value: objects.length, caption: t("Row-scoped data models"), dot: "accent" as const },
      { key: "masks", label: t("Masked field rules"), value: maskedFieldCount, caption: t("Sensitive column guards"), dot: maskedFieldCount > 0 ? ("warning" as const) : ("neutral" as const) },
    ];
  }, [currentApp, objects]);

  const objectColumns: TableColumn<Api.ObjectPermissions>[] = [
    {
      key: "object",
      header: t("Object type"),
      width: proportional(1.1),
      renderCell: (obj) => (
        <VStack gap={0.5}>
          <Text weight="medium" maxLines={1}>{t(obj.title || obj.type)}</Text>
          <Text type="code" color="secondary" maxLines={1}>{obj.type}</Text>
        </VStack>
      ),
    },
    {
      key: "scopes",
      header: t("Row visibility by role (ReBAC)"),
      width: proportional(2.2),
      renderCell: (obj) => {
        const entries = Object.entries(obj.roleScopes ?? {});
        if (entries.length === 0) {
          return <HStack gap={1} wrap="wrap"><ScopeDepthMeter scope={obj.defaultScope} isSpotlit={Boolean(activeSpotlight)} /></HStack>;
        }
        return (
          <HStack gap={1} wrap="wrap" vAlign="center">
            {entries.map(([role, scope]) => (
              <ScopeDepthMeter
                key={role}
                roleLabel={roleTitle(role)}
                scope={scope}
                isSpotlit={activeSpotlight === role}
              />
            ))}
          </HStack>
        );
      },
    },
    {
      key: "relations",
      header: t("Relationship anchors"),
      width: proportional(1.1),
      renderCell: (obj) => {
        const tags: string[] = [];
        if (obj.ownerField) tags.push(`${t("Owner")}: ${obj.ownerField}`);
        if (obj.unitField) tags.push(`${t("Unit")}: ${obj.unitField}`);
        if (obj.participants) tags.push(t("Participants"));
        if (obj.through) tags.push(t("Inherited from parent"));
        return tags.length > 0 ? (
          <HStack gap={1} wrap="wrap">
            {tags.map((tag) => <Token key={tag} size="sm" color="teal" label={tag} />)}
          </HStack>
        ) : (
          <Text color="secondary">{t("Workspace-wide")}</Text>
        );
      },
    },
    {
      key: "fields",
      header: t("Restricted fields"),
      width: proportional(1.2),
      renderCell: (obj) => {
        const fields = obj.fields ?? [];
        if (fields.length === 0) return <Text color="secondary">{t("All fields open to role")}</Text>;
        return (
          <HStack gap={1} wrap="wrap">
            {fields.slice(0, 3).map((field) => (
              <Token
                key={field.name}
                size="sm"
                color="orange"
                label={`${t(field.title || field.name)}${field.write?.length ? ` (${field.write.join(", ")})` : ""}`}
              />
            ))}
            {fields.length > 3 && <Token size="sm" color="gray" label={`+${fields.length - 3}`} />}
          </HStack>
        );
      },
    },
  ];

  const customColumns: TableColumn<Api.CustomRole>[] = [
    {
      key: "title",
      header: t("Custom role"),
      width: proportional(1.2),
      renderCell: (role) => (
        <VStack gap={0.5}>
          <Text weight="medium" maxLines={1}>{role.title || role.id}</Text>
          <Text type="code" color="secondary" maxLines={1}>{role.id}</Text>
        </VStack>
      ),
    },
    {
      key: "app",
      header: t("Application"),
      width: proportional(0.9),
      renderCell: (role) => {
        const found = permList.find((item) => item.app === role.app);
        return <Token size="sm" label={t(found?.title || role.app)} />;
      },
    },
    {
      key: "actions",
      header: t("Allowed actions"),
      width: proportional(2.1),
      renderCell: (role) => (
        <HStack gap={1} wrap="wrap">
          {role.actions.slice(0, 4).map((schema) => (
            <Token key={schema} size="sm" color="blue" label={schema} />
          ))}
          {role.actions.length > 4 && <Token size="sm" color="gray" label={`+${role.actions.length - 4}`} />}
        </HStack>
      ),
    },
    {
      key: "manage",
      header: t("Actions"),
      width: pixel(156),
      align: "end",
      renderCell: (role) => (
        <HStack gap={1.5} hAlign="end">
          {saveAction && (
            <Button
              label={t("Edit")}
              variant="secondary"
              size="sm"
              onClick={() => {
                setEditingRole(role);
                setDialogOpen(true);
              }}
            />
          )}
          {removeAction && (
            <Button label={t("Remove")} variant="secondary" size="sm" onClick={() => setRemovingRole(role)} />
          )}
        </HStack>
      ),
    },
  ];

  const confirmRemove = async () => {
    if (!removeAction || !removingRole) return;
    setBusy(true);
    setFailure("");
    try {
      await send(removeAction, removingRole.id, {});
      reread();
      setRemovingRole(null);
      toast({ body: t("Custom role removed.") });
    } catch (problem) {
      setFailure(reasonOf(problem));
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
            <VStack gap={3} padding={gutter} paddingBlockStart={3} paddingBlockEnd={0}>
              <PageTitle
                title={t("Roles & permissions")}
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
                        label={t("Define custom role")}
                        variant="primary"
                        onClick={() => {
                          setEditingRole(null);
                          setDialogOpen(true);
                        }}
                      />
                    )}
                  </HStack>
                }
              >
                <Text type="supporting" color="secondary">
                  {t("Inspect built-in and custom roles, action capabilities, and relationship-based record scopes (unit hierarchy, ownership, participants and field masks).")}
                </Text>
              </PageTitle>

              <TabList value={tab} onChange={(next) => setTab(next as TabId)} role="tablist" isFullBleed>
                <Tab value="matrix" label={t("Permission matrix")} />
                <Tab
                  value="custom"
                  label={t("Custom roles")}
                  endContent={customRoles.length > 0 ? <Badge variant="neutral" label={String(customRoles.length)} /> : undefined}
                />
              </TabList>
            </VStack>
          </LayoutHeader>
        }
        content={
          <LayoutContent padding={gutter} role="main">
            <VStack gap={4}>
              {failure && <Banner status="error" title={t("Action failed")} description={failure} />}
              {permissions.status === "loading" && <Skeleton height={220} />}
              {(permissions.status === "error" || permissions.status === "refused") && (
                <Banner status="error" title={t("Permissions unavailable")} description={permissions.reason} />
              )}

              {tab === "matrix" && permissions.status === "ready" && (
                <VStack gap={4}>
                  <Card padding={3}>
                    <HStack gap={3} vAlign="end" hAlign="between" wrap="wrap">
                      <HStack gap={3} vAlign="end" wrap="wrap">
                        <Selector
                          label={t("Application or package space")}
                          width={280}
                          hasSearch
                          value={chosenApp}
                          onChange={(next) => {
                            setSelectedApp(next);
                            setSpotlitRole("");
                          }}
                          options={permList.map((entry) => ({
                            value: entry.app,
                            label: entry.title && entry.title !== entry.app ? `${t(entry.title)} (${entry.app})` : t(entry.app),
                          }))}
                        />
                        {currentApp && currentApp.everyone.length > 0 && (
                          <HStack gap={1.5} vAlign="center" wrap="wrap">
                            <Text type="supporting" color="secondary">{t("Open to every member")}:</Text>
                            {currentApp.everyone.slice(0, 5).map((readName) => (
                              <Token key={readName} size="sm" color="gray" label={readName} />
                            ))}
                            {currentApp.everyone.length > 5 && <Token size="sm" color="gray" label={`+${currentApp.everyone.length - 5}`} />}
                          </HStack>
                        )}
                      </HStack>
                      {activeSpotlight && (
                        <Button
                          label={t("Clear role spotlight")}
                          variant="ghost"
                          size="sm"
                          onClick={() => setSpotlitRole("")}
                        />
                      )}
                    </HStack>
                  </Card>

                  {currentApp && (
                    <>
                      <MetricStrip items={summaryMetrics} />

                      <VStack gap={2}>
                        <HStack gap={2} vAlign="center" hAlign="between" wrap="wrap">
                          <Heading level={3}>{t("Roles & action capabilities")}</Heading>
                          <Text type="supporting" color="secondary">
                            {t("Select any role card to spotlight its record visibility across the ReBAC matrix below.")}
                          </Text>
                        </HStack>
                        {currentApp.roles.length > 0 ? (
                          <Grid columns={{ minWidth: 270, max: 3 }} gap={3}>
                            {currentApp.roles.map((roleEntry) => {
                              const isCustom = customIds.has(`${currentApp.app}:${roleEntry.role}`);
                              const isWildcard = roleEntry.role === "*";
                              const selected = activeSpotlight === roleEntry.role;
                              return (
                                <SelectableCard
                                  key={roleEntry.role}
                                  label={roleTitle(roleEntry.role)}
                                  isSelected={selected}
                                  onChange={() => setSpotlitRole(selected ? "" : roleEntry.role)}
                                  padding={4}
                                >
                                  <VStack gap={2}>
                                    <HStack gap={2} vAlign="center" hAlign="between" wrap="wrap">
                                      <HStack gap={1.5} vAlign="center">
                                        <Heading level={4}>{roleTitle(roleEntry.role)}</Heading>
                                        {isCustom && <Token size="sm" color="purple" label={t("Custom")} />}
                                      </HStack>
                                      {isWildcard ? (
                                        <Token size="sm" color="teal" label={t("All active members")} />
                                      ) : (
                                        <Token
                                          size="sm"
                                          color={roleEntry.holders > 0 ? "blue" : "gray"}
                                          label={t("{count} holders").replace("{count}", String(roleEntry.holders))}
                                        />
                                      )}
                                    </HStack>
                                    {roleEntry.actions.length > 0 ? (
                                      <HStack gap={1} wrap="wrap">
                                        {roleEntry.actions.slice(0, 8).map((act) => (
                                          <Token key={act.id} size="sm" color="default" label={act.title ? t(act.title) : act.id} />
                                        ))}
                                        {roleEntry.actions.length > 8 && (
                                          <Token size="sm" color="gray" label={`+${roleEntry.actions.length - 8}`} />
                                        )}
                                      </HStack>
                                    ) : (
                                      <Text type="supporting" color="secondary">{t("Record-level scope role (no write actions)")}</Text>
                                    )}
                                  </VStack>
                                </SelectableCard>
                              );
                            })}
                          </Grid>
                        ) : (
                          <Text type="supporting" color="secondary">{t("No discrete roles declared for this space.")}</Text>
                        )}
                      </VStack>

                      <VStack gap={2}>
                        <Heading level={3}>{t("Object relationship scopes (ReBAC)")}</Heading>
                        {objects.length > 0 ? (
                          <Table
                            data={objects}
                            columns={objectColumns}
                            idKey="type"
                            dividers="rows"
                            hasHover
                            textOverflow="truncate"
                          />
                        ) : (
                          <Card variant="muted" padding={4}>
                            <Text type="supporting" color="secondary">
                              {t("This service governs control-plane actions and reads rather than business object rows.")}
                            </Text>
                          </Card>
                        )}
                      </VStack>
                    </>
                  )}
                </VStack>
              )}

              {tab === "custom" && (
                <VStack gap={4}>
                  {access.status === "loading" && <Skeleton height={160} />}
                  {access.status === "ready" && customRoles.length === 0 && (
                    <Card variant="muted" padding={6}>
                      <VStack gap={3} hAlign="center">
                        <AnatomyPlate kind="prism" dark={dark} />
                        <Heading level={2}>{t("No custom roles defined yet")}</Heading>
                        <Text type="supporting" color="secondary">
                          {t("Create custom roles when a team needs a curated subset of actions inside an application.")}
                        </Text>
                        {saveAction && (
                          <Button
                            label={t("Define custom role")}
                            variant="primary"
                            onClick={() => {
                              setEditingRole(null);
                              setDialogOpen(true);
                            }}
                          />
                        )}
                      </VStack>
                    </Card>
                  )}
                  {customRoles.length > 0 && (
                    <Table
                      data={customRoles}
                      columns={customColumns}
                      idKey="id"
                      dividers="rows"
                      hasHover
                      textOverflow="truncate"
                    />
                  )}
                </VStack>
              )}
            </VStack>
          </LayoutContent>
        }
      />

      {saveAction && (
        <CustomRoleDialog
          isOpen={dialogOpen}
          editing={editingRole}
          permissions={permList}
          saveAction={saveAction}
          onClose={() => setDialogOpen(false)}
        />
      )}

      <AlertDialog
        isOpen={Boolean(removingRole)}
        title={t("Remove custom role?")}
        description={t("Members holding this role will no longer receive permissions from it.")}
        cancelLabel={t("Cancel")}
        actionLabel={t("Remove")}
        actionVariant="destructive"
        isActionLoading={busy}
        onOpenChange={(open) => !open && !busy && setRemovingRole(null)}
        onAction={() => void confirmRemove()}
      />
    </>
  );
}
