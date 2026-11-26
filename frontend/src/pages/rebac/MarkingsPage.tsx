import { AlertDialog } from "@astryxdesign/core/AlertDialog";
import { AvatarGroup } from "@astryxdesign/core/AvatarGroup";
import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { Card } from "@astryxdesign/core/Card";
import { Dialog, DialogHeader } from "@astryxdesign/core/Dialog";
import { FormLayout } from "@astryxdesign/core/FormLayout";
import { Icon } from "@astryxdesign/core/Icon";
import { IconButton } from "@astryxdesign/core/IconButton";
import { HStack, Layout, LayoutContent, LayoutFooter, LayoutHeader, VStack } from "@astryxdesign/core/Layout";
import { List, ListItem } from "@astryxdesign/core/List";
import { MultiSelector } from "@astryxdesign/core/MultiSelector";
import { Skeleton } from "@astryxdesign/core/Skeleton";
import { Table, pixel, proportional } from "@astryxdesign/core/Table";
import type { TableColumn } from "@astryxdesign/core/Table";
import { Heading, Text } from "@astryxdesign/core/Text";
import { TextInput } from "@astryxdesign/core/TextInput";
import { useToast } from "@astryxdesign/core/Toast";
import { Token } from "@astryxdesign/core/Token";
import { ArrowPathIcon } from "@heroicons/react/24/outline";
import { useEffect, useMemo, useState } from "react";
import { type Api, reread, useRead } from "../../host";
import { t } from "../../i18n";
import { useIsDark } from "../../shell/appearance";
import { reasonOf, send } from "../act";
import { PageTitle, useGutter } from "../frame";
import { PersonAvatar } from "../MemberField";
import { AnatomyPlate, MetricStrip } from "./RebacVisuals";
import { useMemberDirectoryMap } from "./shared";

function slugMarkingId(input: string): string {
  return input.toLowerCase().replace(/[^a-z0-9-]+/g, "-").replace(/^[^a-z]+|-+$/g, "").slice(0, 60);
}

function MarkingDialog({
  isOpen,
  editing,
  memberOptions,
  teamOptions,
  saveAction,
  onClose,
}: {
  isOpen: boolean;
  editing: Api.Marking | null;
  memberOptions: { value: string; label: string }[];
  teamOptions: { value: string; label: string }[];
  saveAction: Api.Action;
  onClose: () => void;
}) {
  const toast = useToast();
  const [title, setTitle] = useState(editing?.title ?? "");
  const [markingId, setMarkingId] = useState(editing?.id ?? "");
  const [members, setMembers] = useState<string[]>(editing?.members ?? []);
  const [teams, setTeams] = useState<string[]>(editing?.teams ?? []);
  const [declassifiers, setDeclassifiers] = useState<string[]>(editing?.declassifiers ?? []);
  const [declassifierTeams, setDeclassifierTeams] = useState<string[]>(editing?.declassifierTeams ?? []);
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState("");

  useEffect(() => {
    if (!isOpen) return;
    setTitle(editing?.title ?? "");
    setMarkingId(editing?.id ?? "");
    setMembers(editing?.members ?? []);
    setTeams(editing?.teams ?? []);
    setDeclassifiers(editing?.declassifiers ?? []);
    setDeclassifierTeams(editing?.declassifierTeams ?? []);
    setFailure("");
  }, [isOpen, editing]);

  const effectiveId = editing ? editing.id : slugMarkingId(markingId || title);
  const canSave = !busy && Boolean(title.trim() && effectiveId && /^[a-z][a-z0-9-]{0,63}$/.test(effectiveId));

  const submitMarking = async () => {
    if (!canSave) return;
    setBusy(true);
    setFailure("");
    try {
      await send(
        saveAction,
        effectiveId,
        {
          title: title.trim(),
          members,
          teams,
          declassifiers,
          declassifierTeams,
        },
        editing?.revision || undefined,
      );
      reread();
      toast({ body: editing ? t("Marking clearances updated.") : t("Marking created.") });
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
            title={editing ? t("Manage marking clearances") : t("Create compartment marking")}
            onOpenChange={(open) => !open && !busy && onClose()}
          />
        }
        content={
          <LayoutContent>
            <VStack gap={4}>
              <Text type="supporting" color="secondary">
                {t("Only members and teams cleared for a marking can read data carrying it, regardless of their role.")}
              </Text>
              {failure && <Banner status="error" title={t("Not saved")} description={failure} />}
              <FormLayout defaultOptionality="optional">
                <TextInput
                  label={t("Marking title")}
                  isRequired
                  isDisabled={busy || Boolean(editing?.classification)}
                  value={title}
                  placeholder={t("Executive M&A")}
                  onChange={(next) => {
                    setTitle(next);
                    if (!editing && !markingId) setMarkingId(slugMarkingId(next));
                  }}
                />
                {!editing && (
                  <TextInput
                    label={t("Marking key")}
                    isRequired
                    isDisabled={busy}
                    value={markingId}
                    placeholder={t("exec-ma")}
                    onChange={(next) => setMarkingId(slugMarkingId(next))}
                  />
                )}
                <MultiSelector
                  label={t("Cleared members")}
                  hasSearch
                  hasClear
                  triggerDisplay="badges"
                  maxBadges={3}
                  isDisabled={busy}
                  value={members}
                  options={memberOptions}
                  onChange={setMembers}
                />
                {teamOptions.length > 0 && (
                  <MultiSelector
                    label={t("Cleared teams")}
                    hasSearch
                    hasClear
                    triggerDisplay="badges"
                    maxBadges={3}
                    isDisabled={busy}
                    value={teams}
                    options={teamOptions}
                    onChange={setTeams}
                  />
                )}
                <MultiSelector
                  label={t("Declassifier members")}
                  hasSearch
                  hasClear
                  triggerDisplay="badges"
                  maxBadges={3}
                  isDisabled={busy}
                  value={declassifiers}
                  options={memberOptions}
                  onChange={setDeclassifiers}
                />
                {teamOptions.length > 0 && (
                  <MultiSelector
                    label={t("Declassifier teams")}
                    hasSearch
                    hasClear
                    triggerDisplay="badges"
                    maxBadges={3}
                    isDisabled={busy}
                    value={declassifierTeams}
                    options={teamOptions}
                    onChange={setDeclassifierTeams}
                  />
                )}
              </FormLayout>
            </VStack>
          </LayoutContent>
        }
        footer={
          <LayoutFooter>
            <HStack gap={2} hAlign="end">
              <Button label={t("Cancel")} variant="secondary" isDisabled={busy} onClick={onClose} />
              <Button label={t("Save marking")} variant="primary" isDisabled={!canSave} isLoading={busy} onClick={() => void submitMarking()} />
            </HStack>
          </LayoutFooter>
        }
      />
    </Dialog>
  );
}

function MarkingResourcesCard({ marking, onClose }: { marking: Api.Marking; onClose: () => void }) {
  const resources = useRead<Api.ResourceEntry[]>(`/v1/markings/${encodeURIComponent(marking.id)}/resources`);
  return (
    <Card padding={4}>
      <VStack gap={3}>
        <HStack gap={2} vAlign="center" hAlign="between">
          <Heading level={4}>{`${t("Protected resources")} · ${marking.title}`}</Heading>
          <Button label={t("Close")} variant="ghost" size="sm" onClick={onClose} />
        </HStack>
        {resources.status === "loading" && <Skeleton height={80} />}
        {resources.status === "ready" && resources.data.length === 0 && (
          <Text type="supporting" color="secondary">{t("No datasets, connections or pipelines currently carry this marking.")}</Text>
        )}
        {resources.status === "ready" && resources.data.length > 0 && (
          <List hasDividers>
            {resources.data.map((entry) => (
              <ListItem
                key={entry.id}
                label={entry.title || entry.id}
                description={`${entry.ref.app} · ${entry.ref.kind}`}
                endContent={<Token size="sm" label={entry.project || t("Workspace")} />}
              />
            ))}
          </List>
        )}
      </VStack>
    </Card>
  );
}

export function MarkingsPage() {
  const gutter = useGutter();
  const dark = useIsDark();
  const toast = useToast();
  const markingsRead = useRead<Api.Marking[]>("/v1/markings");
  const access = useRead<Api.AccessConfig>("/v1/access");
  const catalog = useRead<Api.Action[]>("/v1/actions");
  const { options: memberOptions, nameOf: memberName } = useMemberDirectoryMap();

  const [dialogOpen, setDialogOpen] = useState(false);
  const [editing, setEditing] = useState<Api.Marking | null>(null);
  const [archiving, setArchiving] = useState<Api.Marking | null>(null);
  const [inspecting, setInspecting] = useState<Api.Marking | null>(null);
  const [busy, setBusy] = useState(false);

  const markings = markingsRead.status === "ready" ? markingsRead.data : [];
  const teams = access.status === "ready" ? access.data.teams : [];
  const teamOptions = teams.map((team) => ({ value: team.id, label: `${team.name} (${team.id})` }));
  const teamName = (id: string) => teams.find((team) => team.id === id)?.name ?? id;

  const actions = catalog.status === "ready" ? catalog.data : [];
  const saveAction = actions.find((item) => item.schema === "platform.marking.save");
  const archiveAction = actions.find((item) => item.schema === "platform.marking.archive");

  const metrics = useMemo(() => {
    const classCount = markings.filter((m) => m.classification).length;
    const compCount = markings.filter((m) => !m.classification && !m.archived).length;
    const clearedCount = markings.filter((m) => m.cleared).length;
    return [
      { key: "class", label: t("Security classifications"), value: classCount, caption: t("Mandatory hierarchical levels"), dot: "warning" as const },
      { key: "comp", label: t("Active compartments"), value: compCount, caption: t("Need-to-know isolation silos"), dot: "accent" as const },
      { key: "cleared", label: t("Your active clearances"), value: clearedCount, caption: t("Markings readable in your session"), dot: "success" as const },
    ];
  }, [markings]);

  const columns: TableColumn<Api.Marking>[] = [
    {
      key: "title",
      header: t("Marking"),
      width: proportional(1.2),
      renderCell: (item) => (
        <VStack gap={0.5}>
          <HStack gap={1.5} vAlign="center">
            <Text weight="medium" maxLines={1}>{item.title || item.id}</Text>
            <Token
              size="sm"
              color={item.classification ? "orange" : "teal"}
              label={item.classification ? t("Classification") : t("Compartment")}
            />
          </HStack>
          <Text type="code" color="secondary" maxLines={1}>{item.id}</Text>
        </VStack>
      ),
    },
    {
      key: "status",
      header: t("Status"),
      width: pixel(148),
      renderCell: (item) => (
        <HStack gap={1} vAlign="center" wrap="wrap">
          <Token size="sm" color={item.archived ? "gray" : "green"} label={item.archived ? t("Archived") : t("Active")} />
          {item.cleared && <Token size="sm" color="blue" label={t("Cleared")} />}
        </HStack>
      ),
    },
    {
      key: "clearances",
      header: t("Cleared members & teams"),
      width: proportional(1.8),
      renderCell: (item) => {
        const memIds = item.members ?? [];
        const tmIds = item.teams ?? [];
        if (memIds.length === 0 && tmIds.length === 0) {
          return <Text color="secondary">{t("No clearances granted")}</Text>;
        }
        return (
          <HStack gap={1.5} vAlign="center" wrap="wrap">
            {memIds.length > 0 && (
              <AvatarGroup size="xsm">
                {memIds.slice(0, 4).map((id) => (
                  <PersonAvatar key={id} id={id} name={memberName(id)} size="xsm" />
                ))}
              </AvatarGroup>
            )}
            {tmIds.slice(0, 2).map((id) => (
              <Token key={`t:${id}`} size="sm" color="purple" label={`${t("Team")}: ${teamName(id)}`} />
            ))}
            {memIds.slice(0, 2).map((id) => (
              <Token key={`m:${id}`} size="sm" color="default" label={memberName(id)} />
            ))}
            {memIds.length + tmIds.length > 4 && (
              <Token size="sm" color="gray" label={`+${memIds.length + tmIds.length - 4}`} />
            )}
          </HStack>
        );
      },
    },
    {
      key: "actions",
      header: t("Actions"),
      width: pixel(220),
      align: "end",
      renderCell: (item) => (
        <HStack gap={1} hAlign="end">
          <Button label={t("Resources")} variant="ghost" size="sm" onClick={() => setInspecting(item)} />
          {saveAction && (
            <Button
              label={t("Clearances")}
              variant="secondary"
              size="sm"
              onClick={() => {
                setEditing(item);
                setDialogOpen(true);
              }}
            />
          )}
          {archiveAction && !item.archived && !item.classification && (
            <Button label={t("Archive")} variant="secondary" size="sm" onClick={() => setArchiving(item)} />
          )}
        </HStack>
      ),
    },
  ];

  const confirmArchive = async () => {
    if (!archiveAction || !archiving) return;
    setBusy(true);
    try {
      await send(archiveAction, archiving.id, {}, archiving.revision || undefined);
      reread();
      setArchiving(null);
      toast({ body: t("Marking archived.") });
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
                title={t("Markings")}
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
                  {t("Manage mandatory data classifications and compartment markings. Marked data propagates downstream and requires explicit clearance in addition to role grants.")}
                </Text>
              </PageTitle>
            </VStack>
          </LayoutHeader>
        }
        content={
          <LayoutContent padding={gutter} role="main">
            <VStack gap={4}>
              {markingsRead.status === "loading" && <Skeleton height={200} />}
              {(markingsRead.status === "error" || markingsRead.status === "refused") && (
                <Banner status="error" title={t("Markings unavailable")} description={markingsRead.reason} />
              )}
              {markingsRead.status === "ready" && <MetricStrip items={metrics} />}
              {inspecting && <MarkingResourcesCard marking={inspecting} onClose={() => setInspecting(null)} />}
              {markingsRead.status === "ready" && markings.length === 0 && (
                <Card variant="muted" padding={6}>
                  <VStack gap={3} hAlign="center">
                    <AnatomyPlate kind="vault" dark={dark} />
                    <Heading level={2}>{t("Nothing here yet")}</Heading>
                    <Text type="supporting" color="secondary">
                      {t("Create a compartment marking to restrict sensitive connections, datasets and derived objects.")}
                    </Text>
                  </VStack>
                </Card>
              )}
              {markings.length > 0 && (
                <Table
                  data={markings}
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
        <MarkingDialog
          isOpen={dialogOpen}
          editing={editing}
          memberOptions={memberOptions}
          teamOptions={teamOptions}
          saveAction={saveAction}
          onClose={() => setDialogOpen(false)}
        />
      )}

      <AlertDialog
        isOpen={Boolean(archiving)}
        title={t("Archive marking?")}
        description={t("Existing marked data remains protected, and this marking can no longer be newly applied.")}
        cancelLabel={t("Cancel")}
        actionLabel={t("Archive")}
        actionVariant="destructive"
        isActionLoading={busy}
        onOpenChange={(open) => !open && !busy && setArchiving(null)}
        onAction={() => void confirmArchive()}
      />
    </>
  );
}
