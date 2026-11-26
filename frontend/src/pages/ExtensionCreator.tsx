import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { Dialog, DialogHeader } from "@astryxdesign/core/Dialog";
import { FormLayout } from "@astryxdesign/core/FormLayout";
import { HStack, Layout, LayoutContent, LayoutFooter, StackItem, VStack } from "@astryxdesign/core/Layout";
import { SegmentedControl, SegmentedControlItem } from "@astryxdesign/core/SegmentedControl";
import { Selector } from "@astryxdesign/core/Selector";
import { Text } from "@astryxdesign/core/Text";
import { TextInput } from "@astryxdesign/core/TextInput";
import { type KeyboardEvent, useEffect, useMemo, useRef, useState } from "react";
import { type Api, useRead } from "../host";
import { t } from "../i18n";
import { reasonOf, send } from "./act";
import { motionStyles } from "./motion";

type CreatorProps = { onClose: () => void; onDone: (id: string) => void };
type ProjectRow = { id: string; title: string; name?: string; extends?: Api.AssetRef };

const cleanName = (raw: string) => raw.toLowerCase().replace(/[^a-z0-9]+/g, "");

export function ProjectCreator({ onClose, onDone }: CreatorProps) {
  const catalog = useRead<Api.Action[]>("/v1/actions");
  const installed = useRead<Api.ApplicationView[]>("/v1/applications");
  const formRef = useRef<HTMLDivElement>(null);
  const [kind, setKind] = useState<"application" | "extension">("application");
  const [name, setName] = useState("");
  const [title, setTitle] = useState("");
  const [targetApp, setTargetApp] = useState("");
  const [description, setDescription] = useState("");
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState("");

  const createAction = catalog.status === "ready" ? catalog.data.find((action) => action.schema === "build.app.create") : undefined;
  const appOptions = useMemo(() => {
    const views = installed.status === "ready" ? installed.data : [];
    return views.map((view) => ({
      key: view.name || view.app,
      app: view.app || "build",
      label: t(view.title || view.name || view.app),
    }));
  }, [installed]);
  const chosenTarget = appOptions.find((option) => option.key === targetApp) ?? appOptions[0];

  useEffect(() => {
    const frame = requestAnimationFrame(() => {
      const input = formRef.current?.querySelector<HTMLElement>("input:not([role=combobox]):not([disabled]):not([type=hidden])");
      input?.focus();
    });
    return () => cancelAnimationFrame(frame);
  }, []);

  const missingLabels = [
    ...(!name ? [t("API name")] : []),
    ...(!title.trim() ? [t("Name")] : []),
    ...(kind === "extension" && !chosenTarget ? [t("Target application")] : []),
  ];
  const canSubmit = Boolean(createAction && !busy && missingLabels.length === 0);

  const apply = async () => {
    if (!canSubmit || !createAction) return;
    setBusy(true);
    setFailure("");
    const payload: Record<string, unknown> = {
      name,
      title: title.trim(),
      ...(description.trim() ? { description: description.trim() } : {}),
      ...(kind === "extension" && chosenTarget
        ? { extends: { app: chosenTarget.app, kind: "app", name: chosenTarget.key } }
        : {}),
    };
    try {
      const accepted = await send(createAction, name, payload, 0);
      onDone(accepted.id || name);
    } catch (problem) {
      setFailure(reasonOf(problem));
      setBusy(false);
    }
  };

  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if ((event.metaKey || event.ctrlKey) && event.key === "Enter" && canSubmit) {
      event.preventDefault();
      void apply();
    }
  };

  return (
    <Dialog isOpen onOpenChange={onClose} width={560} purpose="form">
      <Layout
        height="fill"
        padding={4}
        header={<DialogHeader title={t("New project")} onOpenChange={onClose} />}
        content={
          <LayoutContent>
            <VStack ref={formRef} gap={3} onKeyDown={onKeyDown}>
              <SegmentedControl
                label={t("Project kind")}
                value={kind}
                onChange={(next) => setKind(next as "application" | "extension")}
              >
                <SegmentedControlItem value="application" label={t("Application")} />
                <SegmentedControlItem value="extension" label={t("Extension")} />
              </SegmentedControl>
              <FormLayout defaultOptionality="optional">
                {failure && <VStack xstyle={[motionStyles.bannerEnter, motionStyles.shakeError]}><Banner status="error" title={t("Not saved")} description={failure} /></VStack>}
                {kind === "extension" && (
                  <Selector
                    label={t("Target application")}
                    isRequired
                    value={chosenTarget?.key ?? ""}
                    onChange={setTargetApp}
                    options={appOptions.map((option) => ({ value: option.key, label: option.label }))}
                  />
                )}
                <TextInput label={t("API name")} isRequired value={name} onChange={(next) => setName(cleanName(next))} />
                <TextInput label={t("Name")} isRequired value={title} onChange={setTitle} />
                <TextInput label={t("Description")} value={description} onChange={setDescription} />
              </FormLayout>
            </VStack>
          </LayoutContent>
        }
        footer={
          <LayoutFooter>
            <HStack gap={2} vAlign="center">
              <StackItem size="fill">
                {missingLabels.length > 0 && (
                  <Text type="supporting" color="secondary" maxLines={1}>
                    {t("Still needed: {fields}").replace("{fields}", missingLabels.slice(0, 3).join(", "))}
                  </Text>
                )}
              </StackItem>
              <Button label={t("Cancel")} variant="secondary" onClick={onClose} />
              <Button label={t("Create")} variant="primary" isDisabled={!canSubmit} isLoading={busy} onClick={apply} />
            </HStack>
          </LayoutFooter>
        }
      />
    </Dialog>
  );
}

export function ObjectCreator({ onClose, onDone }: CreatorProps) {
  const catalog = useRead<Api.Action[]>("/v1/actions");
  const projects = useRead<Api.RecordPage>("/v1/records/build.app?limit=200");
  const entities = useRead<Api.EntityInfo[]>("/v1/entities");
  const formRef = useRef<HTMLDivElement>(null);

  const [project, setProject] = useState("");
  const [name, setName] = useState("");
  const [title, setTitle] = useState("");
  const [extendsType, setExtendsType] = useState("");
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState("");

  const createAction = catalog.status === "ready" ? catalog.data.find((action) => action.schema === "build.object.create") : undefined;
  const projectList = projects.status === "ready" ? (projects.data.records as ProjectRow[]) : [];
  const singleProject = projectList.length === 1 ? projectList[0] : undefined;
  useEffect(() => {
    if (singleProject && !project) setProject(singleProject.id);
  }, [singleProject, project]);
  const effectiveProject = singleProject ? singleProject.id : project;
  const currentProject = projectList.find((entry) => entry.id === effectiveProject);
  const targetSpace = currentProject?.extends?.name ?? "";

  const targetCandidates = useMemo(() => {
    const list = entities.status === "ready" ? entities.data : [];
    const business = list.filter((entity) => !entity.type.startsWith("build.") && !entity.type.startsWith("platform.") && !entity.extends);
    if (!targetSpace) return business;
    const scoped = business.filter((entity) => entity.type.startsWith(`${targetSpace}.`));
    return scoped.length > 0 ? scoped : business;
  }, [entities, targetSpace]);

  const effectiveExtends = useMemo(() => {
    if (extendsType && targetCandidates.some((entity) => entity.type === extendsType)) return extendsType;
    return targetSpace ? (targetCandidates[0]?.type ?? "") : "";
  }, [extendsType, targetCandidates, targetSpace]);

  useEffect(() => {
    const frame = requestAnimationFrame(() => {
      const input = formRef.current?.querySelector<HTMLElement>("input:not([role=combobox]):not([disabled]):not([type=hidden])");
      input?.focus();
    });
    return () => cancelAnimationFrame(frame);
  }, []);

  const missingLabels = [
    ...(!effectiveProject ? [t("Project")] : []),
    ...(targetSpace && !effectiveExtends ? [t("Extends")] : []),
    ...(!name ? [t("API name")] : []),
    ...(!title.trim() ? [t("Name")] : []),
  ];
  const canSubmit = Boolean(createAction && !busy && missingLabels.length === 0);

  const apply = async () => {
    if (!canSubmit || !createAction) return;
    setBusy(true);
    setFailure("");
    const space = currentProject?.name;
    const target = space && space !== "workspace" ? `${space}.${name}` : name;
    const baseEntity = targetCandidates.find((entity) => entity.type === effectiveExtends);
    const payload: Record<string, unknown> = {
      project: effectiveProject,
      name,
      title: title.trim(),
      ...(effectiveExtends
        ? {
            extends: effectiveExtends,
            fields: [{ name: "base", title: baseEntity?.title || "Base", type: "reference", ref: effectiveExtends, required: true }],
          }
        : {}),
    };
    try {
      const accepted = await send(createAction, target, payload, 0);
      onDone(accepted.id || target);
    } catch (problem) {
      setFailure(reasonOf(problem));
      setBusy(false);
    }
  };

  const onKeyDown = (event: KeyboardEvent<HTMLDivElement>) => {
    if ((event.metaKey || event.ctrlKey) && event.key === "Enter" && canSubmit) {
      event.preventDefault();
      void apply();
    }
  };

  return (
    <Dialog isOpen onOpenChange={onClose} width={560} purpose="form">
      <Layout
        height="fill"
        padding={4}
        header={<DialogHeader title={t(createAction?.title || "New object type")} onOpenChange={onClose} />}
        content={
          <LayoutContent>
            <VStack ref={formRef} onKeyDown={onKeyDown}>
              <FormLayout defaultOptionality="optional">
                {failure && <VStack xstyle={[motionStyles.bannerEnter, motionStyles.shakeError]}><Banner status="error" title={t("Not saved")} description={failure} /></VStack>}
                {!singleProject && (
                  <Selector
                    label={t("Project")}
                    isRequired
                    value={project}
                    onChange={(next) => {
                      setProject(next);
                      setExtendsType("");
                    }}
                    options={projectList.map((entry) => ({ value: entry.id, label: entry.title }))}
                  />
                )}
                {targetCandidates.length > 0 && (
                  <Selector
                    label={t("Extends")}
                    isRequired={Boolean(targetSpace)}
                    value={effectiveExtends}
                    onChange={setExtendsType}
                    options={[
                      ...(targetSpace ? [] : [{ value: "", label: t("Standalone object") }]),
                      ...targetCandidates.map((entity) => ({ value: entity.type, label: `${t(entity.title)} · ${entity.type}` })),
                    ]}
                  />
                )}
                <TextInput label={t("API name")} isRequired value={name} onChange={(next) => setName(cleanName(next))} />
                <TextInput label={t("Name")} isRequired value={title} onChange={setTitle} />
              </FormLayout>
            </VStack>
          </LayoutContent>
        }
        footer={
          <LayoutFooter>
            <HStack gap={2} vAlign="center">
              <StackItem size="fill">
                {missingLabels.length > 0 && (
                  <Text type="supporting" color="secondary" maxLines={1}>
                    {t("Still needed: {fields}").replace("{fields}", missingLabels.slice(0, 3).join(", "))}
                  </Text>
                )}
              </StackItem>
              <Button label={t("Cancel")} variant="secondary" onClick={onClose} />
              <Button label={t("Create")} variant="primary" isDisabled={!canSubmit} isLoading={busy} onClick={apply} />
            </HStack>
          </LayoutFooter>
        }
      />
    </Dialog>
  );
}
