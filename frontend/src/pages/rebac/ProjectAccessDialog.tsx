import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { DateInput } from "@astryxdesign/core/DateInput";
import { Dialog, DialogHeader } from "@astryxdesign/core/Dialog";
import { HStack, Layout, LayoutContent, LayoutFooter, StackItem, VStack } from "@astryxdesign/core/Layout";
import { List, ListItem } from "@astryxdesign/core/List";
import { Selector } from "@astryxdesign/core/Selector";
import { Skeleton } from "@astryxdesign/core/Skeleton";
import { Text } from "@astryxdesign/core/Text";
import { Token } from "@astryxdesign/core/Token";
import { useToast } from "@astryxdesign/core/Toast";
import { useState } from "react";
import { type Api, reread, useRead, reasonOf, send } from "../../host";
import { t } from "../../i18n";
import { RequestAccessDialog } from "./RequestAccessDialog";
import { addDaysISO, asIsoDate, roleTitle, todayISO } from "./shared";

const projectRoles = ["owner", "editor", "viewer", "discoverer"] as const;
const approvalCounts = Array.from({ length: 5 }, (_, index) => ({
  value: String(index + 1),
  label: String(index + 1),
}));

export function ProjectAccessPanel({ project, title }: { project: string; title: string }) {
  const toast = useToast();
  const access = useRead<Api.ProjectAccessView>(`/v1/projects/${encodeURIComponent(project)}/access`);
  const catalog = useRead<Api.Action[]>("/v1/actions");
  const me = useRead<Api.MeView>("/v1/me");

  const [pickedMember, setPickedMember] = useState("");
  const [pickedRole, setPickedRole] = useState("editor");
  const [pickedUntil, setPickedUntil] = useState("");
  const [requestOpen, setRequestOpen] = useState(false);
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState("");

  if (access.status === "loading") return <Skeleton height={140} />;
  if (access.status === "error" || access.status === "refused") {
    return <Banner status="error" title={t("Project access unavailable")} description={access.reason} />;
  }

  const view = access.data;
  const members = view.members ?? [];
  const people = view.people ?? [];
  const nameOf = (id: string) => people.find((person) => person.id === id)?.name ?? id;
  const saveAction = catalog.status === "ready" ? catalog.data.find((item) => item.schema === "platform.project.save") : undefined;
  const canManage = Boolean(view.manage && saveAction);
  const principalId = me.status === "ready" ? me.data.principalId : "";
  const today = todayISO(me.status === "ready" ? me.data.account.effective.timezone : undefined);
  const minDate = addDaysISO(1, today);

  const candidates = people.filter((person) => !members.some((item) => item.member === person.id));
  const nextRole = members.length === 0 ? "owner" : pickedRole;

  const commitMembers = async (nextMembers: Api.ProjectMember[], approvals = view.approvals || 1) => {
    if (!saveAction) return;
    setBusy(true);
    setFailure("");
    try {
      await send(saveAction, project, { title: title || project, approvals, members: nextMembers });
      reread();
      toast({ body: t("Project access updated.") });
    } catch (problem) {
      setFailure(reasonOf(problem));
    } finally {
      setBusy(false);
    }
  };

  return (
    <VStack gap={3}>
      {failure && <Banner status="error" title={t("Not saved")} description={failure} />}
      <HStack gap={2} vAlign="center" wrap="wrap">
        <StackItem size="fill">
          <Text type="supporting" color="secondary">
            {t("Owners manage project access; editors change resources; viewers read them; discoverers see names and request access.")}
          </Text>
        </StackItem>
        {canManage ? (
          <Selector
            label={t("Required approvals")}
            size="sm"
            width={150}
            isDisabled={busy}
            value={String(view.approvals || 1)}
            options={approvalCounts}
            onChange={(value) => void commitMembers(members, Number(value))}
          />
        ) : (
          <Button label={t("Request access")} variant="secondary" size="sm" onClick={() => setRequestOpen(true)} />
        )}
      </HStack>

      {members.length > 0 ? (
        <List header={t("Project members")} hasDividers>
          {members.map((entry) => (
            <ListItem
              key={entry.member}
              label={nameOf(entry.member)}
              description={entry.member !== nameOf(entry.member) ? entry.member : undefined}
              endContent={
                <HStack gap={2} vAlign="center" wrap="wrap">
                  {entry.member === principalId && <Token size="sm" color="gray" label={t("You")} />}
                  {entry.until ? (
                    <Token size="sm" color="blue" label={t("Until {date}").replace("{date}", entry.until)} />
                  ) : (
                    <Token size="sm" color="gray" label={t("No end")} />
                  )}
                  {canManage ? (
                    <>
                      <Selector
                        label={t("Role")}
                        isLabelHidden
                        size="sm"
                        width={140}
                        isDisabled={busy}
                        value={entry.role ?? "viewer"}
                        options={projectRoles.map((item) => ({ value: item, label: roleTitle(item) }))}
                        onChange={(role) =>
                          void commitMembers(members.map((item) => (item.member === entry.member ? { ...item, role } : item)))
                        }
                      />
                      <Button
                        label={t("Remove")}
                        variant="secondary"
                        size="sm"
                        isDisabled={busy}
                        onClick={() => void commitMembers(members.filter((item) => item.member !== entry.member))}
                      />
                    </>
                  ) : (
                    <Token size="sm" color="default" label={roleTitle(entry.role ?? "viewer")} />
                  )}
                </HStack>
              }
            />
          ))}
        </List>
      ) : (
        <Text type="supporting" color="secondary">
          {t("No explicit project members yet. Workspace builders and administrators reach every project.")}
        </Text>
      )}

      {canManage && candidates.length > 0 && (
        <HStack gap={2} vAlign="end" wrap="wrap">
          <Selector
            label={t("Member")}
            width={220}
            hasSearch={candidates.length > 8}
            isDisabled={busy}
            value={pickedMember}
            onChange={setPickedMember}
            options={candidates.map((person) => ({
              value: person.id,
              label: person.name !== person.id ? `${person.name} (${person.id})` : person.name,
            }))}
          />
          <Selector
            label={t("Role")}
            width={150}
            isDisabled={busy || members.length === 0}
            value={nextRole}
            onChange={setPickedRole}
            options={projectRoles.map((item) => ({ value: item, label: roleTitle(item) }))}
          />
          <DateInput
            label={t("Until")}
            isOptional
            hasClear
            min={minDate}
            width={180}
            format="system_date"
            value={asIsoDate(pickedUntil)}
            onChange={(next) => setPickedUntil(next ?? "")}
          />
          <Button
            label={t("Add member")}
            variant="primary"
            size="sm"
            isDisabled={busy || !pickedMember}
            onClick={() => {
              const next: Api.ProjectMember = {
                member: pickedMember,
                role: nextRole,
                ...(pickedUntil ? { until: pickedUntil } : {}),
              };
              void commitMembers([...members, next]).then(() => {
                setPickedMember("");
                setPickedUntil("");
              });
            }}
          />
        </HStack>
      )}

      <RequestAccessDialog
        isOpen={requestOpen}
        onClose={() => setRequestOpen(false)}
        fixedProject={project}
        initialScope="project"
      />
    </VStack>
  );
}

export function ProjectAccessDialog({
  project,
  title,
  isOpen,
  onClose,
}: {
  project: string;
  title: string;
  isOpen: boolean;
  onClose: () => void;
}) {
  if (!isOpen) return null;
  return (
    <Dialog isOpen onOpenChange={(open) => !open && onClose()} width={720} purpose="form">
      <Layout
        height="fill"
        padding={4}
        header={<DialogHeader title={`${t("Project access")} · ${title || project}`} onOpenChange={(open) => !open && onClose()} />}
        content={
          <LayoutContent>
            <ProjectAccessPanel project={project} title={title} />
          </LayoutContent>
        }
        footer={
          <LayoutFooter>
            <HStack hAlign="end">
              <Button label={t("Done")} variant="primary" onClick={onClose} />
            </HStack>
          </LayoutFooter>
        }
      />
    </Dialog>
  );
}
