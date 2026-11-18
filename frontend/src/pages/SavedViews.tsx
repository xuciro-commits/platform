import { AlertDialog } from "@astryxdesign/core/AlertDialog";
import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { Dialog, DialogHeader } from "@astryxdesign/core/Dialog";
import { FormLayout } from "@astryxdesign/core/FormLayout";
import { HStack, Layout, LayoutContent, LayoutFooter, StackItem, VStack } from "@astryxdesign/core/Layout";
import { Popover } from "@astryxdesign/core/Popover";
import { Selector } from "@astryxdesign/core/Selector";
import { Text } from "@astryxdesign/core/Text";
import { TextInput } from "@astryxdesign/core/TextInput";
import { useState } from "react";
import { type Api, request, reread, useRead } from "../host";
import { t } from "../i18n";
import { navigate, usePath, useSearch } from "../router";
import { reasonOf, send } from "./act";
import { readView, type ViewState } from "./view";

type Preference = { revision: number; pinned?: string[]; default?: string };
type Mode = "save" | "rename" | "share" | "remove";

export function SavedViews({ entity, view, fallback }: { entity: string; view: ViewState; fallback: ViewState }) {
  const path = usePath();
  const search = useSearch();
  const params = new URLSearchParams(search);
  const selected = params.get("saved") ?? "";
  const [mode, setMode] = useState<Mode>();
  const [title, setTitle] = useState("");
  const [scope, setScope] = useState<"self" | "project" | "tenant">("self");
  const [project, setProject] = useState("");
  const [failure, setFailure] = useState("");
  const [busy, setBusy] = useState(false);
  const catalog = useRead<Api.Action[]>("/v1/actions");
  const views = useRead<Api.SavedView[]>("/v1/views");
  const me = useRead<Api.MeView>("/v1/me");
  const projects = useRead<Api.RecordPage>(mode === "share" && scope === "project" ? "/v1/records/build.app?limit=200" : null);
  const principal = me.status === "ready" ? me.data.principalId : "";
  const preferencePath = principal ? `/v1/records/work.viewpreference/${encodeURIComponent(`${principal}@${entity}`)}` : null;
  const preference = useRead<Api.RecordView>(preferencePath);
  const pref = preference.status === "ready" ? (preference.data.record as Preference) : undefined;
  const visible = views.status === "ready" ? views.data.filter((candidate) => candidate.entity === entity) : [];
  const ordered = [...visible].sort((left, right) => Number(pref?.pinned?.includes(right.id)) - Number(pref?.pinned?.includes(left.id)));
  const current = ordered.find((candidate) => candidate.id === selected);
  const actions = catalog.status === "ready" ? catalog.data : [];
  const saveAction = actions.find((action) => action.schema === "work.view.save");
  const removeAction = actions.find((action) => action.schema === "work.view.remove");
  const shareAction = actions.find((action) => action.schema === "work.view.share");
  const preferenceAction = actions.find((action) => action.schema === "work.viewpreference.set");

  const open = (next: Mode, candidate?: Api.SavedView) => {
    setFailure("");
    setMode(next);
    setTitle(candidate?.title ?? "");
    setScope((candidate?.share as typeof scope | undefined) ?? "self");
    setProject(candidate?.project ?? "");
  };

  const finish = (id = selected) => {
    setMode(undefined);
    setBusy(false);
    reread();
    if (id) {
      const next = new URLSearchParams(search);
      next.set("saved", id);
      navigate(path + "?" + next);
    }
  };

  const save = async () => {
    if (!saveAction || !title.trim()) return;
    setBusy(true);
    setFailure("");
    const id = current?.owner === principal ? current.id : `VIEW-${crypto.randomUUID()}`;
    try {
      await send(saveAction, id, { title: title.trim(), entity, state: JSON.stringify(view) }, current?.owner === principal ? current.revision : 0);
      finish(id);
    } catch (problem) {
      setFailure(reasonOf(problem));
      setBusy(false);
    }
  };

  const share = async () => {
    if (!shareAction || !current) return;
    setBusy(true);
    setFailure("");
    try {
      await send(shareAction, current.id, { scope, project: scope === "project" ? project : "" }, current.revision);
      finish();
    } catch (problem) {
      setFailure(reasonOf(problem));
      setBusy(false);
    }
  };

  const remove = async () => {
    if (!removeAction || !current) return;
    setBusy(true);
    setFailure("");
    try {
      await send(removeAction, current.id, {}, current.revision);
      const next = new URLSearchParams(search);
      next.delete("saved");
      setMode(undefined);
      setBusy(false);
      reread();
      navigate(path + "?" + next);
    } catch (problem) {
      setFailure(reasonOf(problem));
      setBusy(false);
    }
  };

  const prefer = async (candidate: Api.SavedView, change: { pinned?: boolean; default?: boolean }) => {
    if (!preferenceAction || !principal) return;
    setFailure("");
    try {
      await send(preferenceAction, `${principal}@${entity}`, { view: candidate.id, ...change }, pref?.revision ?? 0);
      reread();
    } catch (problem) {
      setFailure(reasonOf(problem));
    }
  };

  const apply = (candidate: Api.SavedView) => {
    const next = readView(candidate.state, fallback);
    const nextParams = new URLSearchParams(search);
    nextParams.set("view", JSON.stringify(next));
    nextParams.set("saved", candidate.id);
    navigate(path + "?" + nextParams);
  };

  const useDefault = () => {
    const next = new URLSearchParams(search);
    next.delete("view");
    next.delete("saved");
    navigate(path + (next.size ? "?" + next : ""));
  };

  const choose = async (candidate: Api.SavedView) => {
    setMode(undefined);
    apply(candidate);
  };

  return (
    <>
      <Popover
        placement="below"
        alignment="end"
        width={420}
        label={t("Views")}
        content={
          <VStack gap={3}>
            {failure && <Banner status="error" title={t("Not saved")} description={failure} />}
            <Button label={t("Default view")} variant={current ? "secondary" : "primary"} onClick={useDefault} />
            <Text type="body" weight="bold">{t("My and shared views")}</Text>
            {views.status === "loading" && <Text type="body">{t("Loading…")}</Text>}
            {views.status !== "loading" && ordered.length === 0 && <Text type="body" color="secondary">{t("No saved views")}</Text>}
            {ordered.map((candidate) => (
              <VStack key={candidate.id} gap={1}>
                <HStack gap={2} vAlign="center">
                  <StackItem size="fill">
                    <Button label={candidate.title} variant={candidate.id === selected ? "primary" : "ghost"} onClick={() => choose(candidate)} />
                  </StackItem>
                  <Button label={pref?.pinned?.includes(candidate.id) ? t("Unpin") : t("Pin")} variant="ghost" size="sm" onClick={() => prefer(candidate, { pinned: !pref?.pinned?.includes(candidate.id) })} />
                  <Button label={t("Set default")} variant="ghost" size="sm" onClick={() => prefer(candidate, { default: true })} />
                </HStack>
                <Text type="supporting" color="secondary">
                  {candidate.share === "tenant" ? t("Public") : candidate.share === "project" ? t("Project") : candidate.owner === principal ? t("My view") : t("Shared with me")}
                </Text>
              </VStack>
            ))}
            <Button label={t("Save current view")} variant="secondary" isDisabled={!saveAction} onClick={() => open("save")} />
            {current?.owner === principal && (
              <HStack gap={2} wrap="wrap">
                <Button label={t("Rename")} variant="ghost" isDisabled={!saveAction} onClick={() => open("rename", current)} />
                <Button label={t("Share view")} variant="ghost" isDisabled={!shareAction} onClick={() => open("share", current)} />
                <Button label={t("Remove view")} variant="destructive" isDisabled={!removeAction} onClick={() => open("remove", current)} />
              </HStack>
            )}
          </VStack>
        }>
        <Button label={current?.title ?? t("Views")} variant="secondary" />
      </Popover>
      {(mode === "save" || mode === "rename") && (
        <Dialog isOpen onOpenChange={() => setMode(undefined)} width={520} purpose="form">
          <Layout
            padding={4}
            header={<DialogHeader title={mode === "save" ? t("Save current view") : t("Rename")} onOpenChange={() => setMode(undefined)} />}
            content={
              <LayoutContent>
                <FormLayout>
                  {failure && <Banner status="error" title={t("Not saved")} description={failure} />}
                  <TextInput label={t("View name")} value={title} onChange={setTitle} />
                </FormLayout>
              </LayoutContent>
            }
            footer={
              <LayoutFooter>
                <HStack gap={2} hAlign="end">
                  <Button label={t("Cancel")} variant="secondary" onClick={() => setMode(undefined)} />
                  <Button label={mode === "save" ? t("Save") : t("Rename")} variant="primary" isDisabled={busy || !title.trim()} onClick={save} />
                </HStack>
              </LayoutFooter>
            }
          />
        </Dialog>
      )}
      {mode === "share" && (
        <Dialog isOpen onOpenChange={() => setMode(undefined)} width={520} purpose="form">
          <Layout
            padding={4}
            header={<DialogHeader title={t("Share view")} onOpenChange={() => setMode(undefined)} />}
            content={
              <LayoutContent>
                <FormLayout>
                  {failure && <Banner status="error" title={t("Not saved")} description={failure} />}
                  <Selector
                    label={t("Who can read this view")}
                    value={scope}
                    onChange={(value) => setScope(value as typeof scope)}
                    options={[
                      { value: "self", label: t("Only me") },
                      { value: "project", label: t("Project") },
                      { value: "tenant", label: t("Everyone in this workspace") },
                    ]}
                  />
                  {scope === "project" && (
                    <Selector
                      label={t("Project")}
                      value={project}
                      onChange={setProject}
                      options={projects.status === "ready" ? (projects.data.records as { id: string; title: string }[]).map((entry) => ({ value: entry.id, label: entry.title })) : []}
                    />
                  )}
                </FormLayout>
              </LayoutContent>
            }
            footer={
              <LayoutFooter>
                <HStack gap={2} hAlign="end">
                  <Button label={t("Cancel")} variant="secondary" onClick={() => setMode(undefined)} />
                  <Button label={t("Share view")} variant="primary" isDisabled={busy || (scope === "project" && !project)} onClick={share} />
                </HStack>
              </LayoutFooter>
            }
          />
        </Dialog>
      )}
      {mode === "remove" && current && (
        <AlertDialog
          isOpen
          title={`${t("Remove view")} “${current.title}”?`}
          description={t("The saved definition is archived. Its records and permissions do not change.")}
          actionLabel={t("Remove view")}
          actionVariant="destructive"
          isActionLoading={busy}
          onOpenChange={(open) => !open && setMode(undefined)}
          onAction={remove}
        />
      )}
    </>
  );
}
