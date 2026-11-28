import { AlertDialog } from "@astryxdesign/core/AlertDialog";
import { Banner } from "@astryxdesign/core/Banner";
import { EmptyState } from "@astryxdesign/core/EmptyState";
import { useMediaQuery } from "@astryxdesign/core/hooks";
import { Icon } from "@astryxdesign/core/Icon";
import { IconButton } from "@astryxdesign/core/IconButton";
import { HStack, Layout, LayoutContent, LayoutHeader, LayoutPanel, VStack } from "@astryxdesign/core/Layout";
import { MetadataList, MetadataListItem } from "@astryxdesign/core/MetadataList";
import { Skeleton } from "@astryxdesign/core/Skeleton";
import { Tab, TabList } from "@astryxdesign/core/TabList";
import { Heading, Text } from "@astryxdesign/core/Text";
import { CheckIcon, ClipboardDocumentIcon } from "@heroicons/react/24/outline";
import { type ReactNode, useCallback, useEffect, useMemo, useState } from "react";
import { type Api, useRead } from "../host";
import { t } from "../i18n";
import { originOf, usePath, useSearch } from "../router";
import { useCommandSearch } from "../commandScope";
import { PageTitle, useGutter } from "./frame";
import { DefinitionRows, stateProblem } from "./DefinitionRows";
import { describe, type Described, headerStatus, statusField, type Values, withRestrictedFields } from "./describe";
import { ProjectObjectsSection } from "./ExtensionCreator";
import { KeyboardHelp } from "./KeyboardHelp";
import { type Editor, FieldGroup, useEditor } from "./RecordFields";
import { DownstreamDocuments, RecordFiles, RecordLinks, relatedGroups } from "./RecordAttachments";
import { Collaboration, RecordActivity } from "./RecordActivity";
import { compareObjectChanges, RecordComparison } from "./RecordComparison";
import { type ContributedAction, ExtensionHost, extensionsOf } from "./RecordExtensions";
import { RecordLines } from "./RecordLines";
import { RecordPrint } from "./RecordPrint";
import { ObjectPreview } from "./ObjectPreview";
import { useOptions } from "./options";
import { enterable } from "./fieldRules";
import { CodeValue, PersonLabel, StatusValue, TemporalValue } from "../semantic";
import { motionStyles } from "./motion";
import { PropertyField } from "./PropertyField";
import { ProjectAccessPanel } from "./rebac/ProjectAccessDialog";
import { RequestAccessButton } from "./rebac/RequestAccessDialog";
import { dots, type Instances, Transitions } from "./Transitions";
import { plain } from "./values";

type Stamp = { by?: string; at?: string };
type Kept = { id: string; revision: number; created: Stamp; changed: Stamp; archived?: boolean };
export type RecordPageProps = { described: Described; id: string; listPath: string; listTitle: string; instances?: Instances; entities: Api.EntityInfo[]; catalog: Api.Action[] };

function Loading() {
  return (
    <VStack gap={2}>
      {[0, 1, 2, 3].map((index) => (
        <Skeleton key={index} height={28} index={index} />
      ))}
    </VStack>
  );
}

function CopyId({ id }: { id: string }) {
  const [copied, setCopied] = useState(false);
  const copy = () => {
    void navigator.clipboard?.writeText(id);
    setCopied(true);
    window.setTimeout(() => setCopied(false), 1400);
  };
  return (
    <HStack gap={1} vAlign="center">
      <Text type="code" color="secondary" maxLines={1}>{id}</Text>
      <IconButton
        label={copied ? t("Copied") : t("Copy")}
        tooltip={copied ? t("Copied") : t("Copy")}
        variant="ghost"
        size="sm"
        icon={
          <HStack key={copied ? "copied" : "copy"} xstyle={motionStyles.popEnter}>
            <Icon icon={copied ? CheckIcon : ClipboardDocumentIcon} color={copied ? "success" : undefined} size="sm" />
          </HStack>
        }
        onClick={copy}
      />
    </HStack>
  );
}

function Rail({ described, editor, kept, refs, people, restricted, collaboration }: { described: Described; editor: Editor; kept: Kept; refs?: Api.RecordView["refs"]; people?: Api.RecordView["people"]; restricted: ReadonlySet<string>; collaboration?: ReactNode }) {
  const technical = described.type.startsWith("build.") || described.type.startsWith("platform.");
  const showIdInHeader = technical || Boolean(described.entity.naming);
  return (
    <VStack gap={6}>
      <FieldGroup described={described} editor={editor} sections={[{ title: "Properties", fields: described.aside }]} direction="vertical" refs={refs} restricted={restricted} />
      {collaboration}
      <VStack gap={4}>
        <Heading level={3} accessibilityLevel={2}>{t("Record")}</Heading>
        <MetadataList>
          <MetadataListItem label={t("Created")}>
            <VStack gap={0.5}>
              {kept.created.at && <TemporalValue value={kept.created.at} kind="datetime" mode="absolute" size="md" surface="detail" />}
              {kept.created.by && <PersonLabel id={kept.created.by} name={people?.[kept.created.by] ?? t("Unnamed member")} />}
            </VStack>
          </MetadataListItem>
          <MetadataListItem label={t("Updated")}>
            <VStack gap={0.5}>
              {kept.changed.at && <TemporalValue value={kept.changed.at} kind="datetime" mode="absolute" size="md" surface="detail" />}
              {kept.changed.by && <PersonLabel id={kept.changed.by} name={people?.[kept.changed.by] ?? t("Unnamed member")} />}
            </VStack>
          </MetadataListItem>
          {!showIdInHeader && (
            <MetadataListItem label={t("Record ID")}>
              <CopyId id={kept.id} />
            </MetadataListItem>
          )}
          {technical && (
            <MetadataListItem label={t("Revision")}>
              <Text type="body" hasTabularNumbers>{String(kept.revision)}</Text>
            </MetadataListItem>
          )}
        </MetadataList>
      </VStack>
    </VStack>
  );
}

export function RecordPage({ described, id, listPath, listTitle, instances, entities, catalog }: RecordPageProps) {
  const [refusal, setRefusal] = useState("");
  const [acceptedDefinitionRevision, setAcceptedDefinitionRevision] = useState(0);
  const [fresh] = useState(() => (history.state as { fresh?: string } | null)?.fresh === id);
  const base = `${listPath}/${encodeURIComponent(id)}`;
  const origin = originOf(history.state, (href) => {
    const path = href.split("?")[0];
    return path === base || path.startsWith(`${base}/`);
  });
  const query = useSearch();
  const listHref = listPath + query;
  const asked = usePath().slice(base.length + 1);
  const narrow = useMediaQuery("(max-width: 1024px)");
  const gutter = useGutter();
  const read = useRead<Api.RecordView>(`/v1/records/${described.type}/${encodeURIComponent(id)}`);
  const values = useMemo(
    () => (read.status === "ready" ? Object.fromEntries(Object.entries(read.data.values ?? {}).map(([name, value]) => [name, plain(value)])) : {}),
    [read],
  );
  const kept = read.status === "ready" ? (read.data.record as Kept) : undefined;
  const section = kept ? `/v1/records/${described.type}/${encodeURIComponent(id)}/sections` : null;
  const comments = useRead<Api.RecordSectionView>(section ? `${section}/comments` : null);
  const links = useRead<Api.RecordSectionView>(section ? `${section}/links` : null);
  const files = useRead<Api.RecordSectionView>(section ? `${section}/files` : null);
  const attach = catalog.find((action) => action.schema === "files.file.attach" && action.target !== described.type);
  const detach = catalog.find((action) => action.schema === "files.file.detach" && action.target !== described.type);
  const collaboration = <Collaboration target={`${described.type}/${id}`} comments={comments} catalog={catalog} entities={entities} />;
  const changes = read.status === "ready" ? read.data.changes ?? [] : [];
  const definition = described.type === "build.object" && kept ? kept as unknown as Values : undefined;
  const definitionRefreshing = Boolean(definition && kept && kept.revision < acceptedDefinitionRevision);
  const refs = read.status === "ready" ? read.data.refs : undefined;
  const people = read.status === "ready" ? read.data.people : undefined;
  const restrictedFields = read.status === "ready" ? read.data.restrictedFields : undefined;
  const restricted = new Set(read.status === "ready" ? read.data.restricted : undefined);
  const editAllowed = Boolean(described.edit) && read.status === "ready" && (read.data.actions ?? []).some((action) => action.schema === described.edit?.schema && action.allowed);
  const detail = useMemo(() => {
    const baseDetail = restrictedFields?.length ? describe(withRestrictedFields(described.entity, restrictedFields), catalog) : described;
    return editAllowed ? baseDetail : { ...baseDetail, edit: undefined, editable: new Set<string>() };
  }, [catalog, described, restrictedFields, editAllowed]);
  const editor = useEditor(detail, id, values, kept?.revision ?? 0);
  const laneField = statusField(detail);
  const ownerField = detail.entity.fields.find((field) => field.type === "member");
  const heading = kept ? [laneField, ownerField].flatMap((field) => field && !restricted.has(field.name) ? [field] : []) : [];
  const headed = heading.map((field) => field.name).join(",");
  const body = useMemo(() => {
    const rest = (fields: Api.FieldInfo[]) => fields.filter((field) => !headed.split(",").includes(field.name));
    return { ...detail, aside: rest(detail.aside), sections: detail.sections.map((section) => ({ ...section, fields: rest(section.fields) })) };
  }, [detail, headed]);
  const options = useOptions(detail, laneField ? [laneField.name] : []);
  const parameters = new Map((detail.edit?.payload ?? []).filter(enterable).map((parameter) => [parameter.name, parameter]));
  const status = headerStatus(described, values, kept?.archived);
  const name = described.name(values, id);
  const search = useCommandSearch();
  const loaded = read.status === "ready";
  const showIdInHeader = described.type.startsWith("build.") || described.type.startsWith("platform.") || Boolean(described.entity.naming);
  useEffect(() => {
    if (loaded) search?.remember({ href: base, label: name, type: described.type, id });
  }, [search, loaded, base, name, described.type, id]);

  const extensions = useMemo(() => extensionsOf(described.type, entities, catalog), [described.type, entities, catalog]);
  const [extActionMap, setExtActionMap] = useState<Record<string, ContributedAction[]>>({});
  useEffect(() => {
    setExtActionMap({});
  }, [described.type, id]);
  const onExtensionActions = useCallback((extType: string, next: ContributedAction[]) => {
    setExtActionMap((previous) => {
      const current = previous[extType] ?? [];
      if (current.length === 0 && next.length === 0) return previous;
      return { ...previous, [extType]: next };
    });
  }, []);
  const contributedActions = useMemo(
    () => extensions.flatMap((ext) => extActionMap[ext.type] ?? []),
    [extensions, extActionMap],
  );

  const unconnected = useMemo(
    () => Array.from(new Set(catalog.filter((action) => action.target === described.type).flatMap((action) => action.unconnected ?? []))),
    [catalog, described.type],
  );
  const deliveryFailures = useMemo(() => {
    if (read.status !== "ready") return [];
    const latest = new Map<string, Api.RecordDelivery>();
    for (const entry of read.data.deliveries ?? []) {
      const prev = latest.get(entry.event);
      if (!prev || entry.at >= prev.at) latest.set(entry.event, entry);
    }
    return Array.from(new Set(Array.from(latest.values()).filter((d) => d.state === "rejected" && d.message).map((d) => d.message as string)));
  }, [read]);
  const inlineSets = useMemo(
    () =>
      (described.entity.sets ?? []).flatMap((set) => {
        if (set.direction !== "inverse") return [];
        const type = entities.find((entry) => entry.type === set.object);
        return type && !type.lifecycle && !type.ledger ? [{ set, type }] : [];
      }),
    [described.entity.sets, entities],
  );
  const inlineTypes = useMemo(() => new Set(inlineSets.map((entry) => entry.type.type)), [inlineSets]);
  const isProjectRecord = described.type === "build.app";
  const tabs = [
    { value: "details", label: "Details" },
    ...(definition ? [{ value: "preview", label: "Preview" }] : []),
    ...(isProjectRecord ? [{ value: "access", label: "Access" }] : []),
    { value: "activity", label: "Activity" },
    ...(changes.length ? [{ value: "changes", label: "Changes" }] : []),
    ...((links.status === "ready" ? relatedGroups(links.data).length > 0 : asked === "links") ? [{ value: "links", label: "Links" }] : []),
    ...((attach || (files.status === "ready" ? files.data.files?.length : asked === "files")) ? [{ value: "files", label: "Files" }] : []),
    ...(narrow ? [{ value: "properties", label: "Properties" }] : []),
  ];
  const tab = tabs.find((candidate) => candidate.value === asked)?.value ?? "details";

  const actions = kept && (
    <Transitions
      described={described}
      id={id}
      values={values}
      revision={kept.revision}
      title={name}
      instances={instances}
      allowed={read.status === "ready" ? read.data.actions : undefined}
      contributed={contributedActions}
      refreshing={definitionRefreshing}
      publishProblem={definition && stateProblem(definition)}
      printHref={`${base}/print`}
      onFailure={setRefusal}
    />
  );

  if (asked === "print" && kept) {
    return (
      <RecordPrint
        described={described}
        id={id}
        base={base}
        values={values}
        kept={kept}
        refs={refs}
        restricted={restricted}
        entities={entities}
        catalog={catalog}
      />
    );
  }

  return (
    <Layout
      height="fill"
      contentWidth={1600}
      header={
        <LayoutHeader padding={0} hasDivider>
          <VStack gap={4} padding={gutter} paddingBlockStart={3} paddingBlockEnd={0}>
            <PageTitle title={name} back={origin ? { href: origin.href, label: origin.title || t(listTitle) } : { href: listHref, label: t(listTitle) }} actions={!narrow && actions}>
              <HStack gap={4} vAlign="center" wrap="wrap">
                {showIdInHeader && <CodeValue value={id} size="sm" copyable />}
                {status && <StatusValue label={t(status.title)} tone={status.tone} size="md" appearance="pill" />}
                {heading.map((field) => {
                  const value = editor.values[field.name];
                  const parameter = parameters.get(field.name);
                  return (
                    <PropertyField key={field.name} field={field} parameter={parameter} described={described.entity} value={value} form={editor.values}
                      name={refs?.[field.name]?.[String(value ?? "")]} dot={options[field.name]?.find((option) => option.value === String(value ?? ""))?.dot}
                      status={editor.status[field.name]} saving={editor.saving.has(field.name)} labelHidden hint={t(field.title)}
                      onChange={parameter && ((next: unknown) => editor.change(parameter, next))} onCommit={() => editor.commit(field.name)} />
                  );
                })}
                {changes.length > 0 && <Text type="supporting" color="secondary">{t("Unpublished changes")}</Text>}
                {kept?.changed.at && (
                  <HStack gap={1} vAlign="center">
                    <Text type="supporting" color="secondary">
                      {t("Updated")}
                    </Text>
                    <TemporalValue value={kept.changed.at} kind="datetime" mode="auto" size="xs" surface="inline" />
                  </HStack>
                )}
              </HStack>
            </PageTitle>
            {narrow && actions}
            <TabList value={tab} onChange={() => undefined} size="lg" isFullBleed>
              {tabs.map((candidate) => (
                <Tab
                  key={candidate.value}
                  value={candidate.value}
                  label={t(candidate.label)}
                  href={(candidate.value === "details" ? base : `${base}/${candidate.value}`) + query}
                />
              ))}
            </TabList>
          </VStack>
        </LayoutHeader>
      }
      content={
        <LayoutContent padding={gutter} role="main">
          <KeyboardHelp />
          {editor.leave && (
            <AlertDialog
              isOpen
              title={t("Discard unsaved changes?")}
              description={t("The record could not be saved. Stay to correct the input or discard it and leave.")}
              cancelLabel={t("Stay")}
              actionLabel={t("Discard")}
              actionVariant="destructive"
              onOpenChange={(open) => !open && editor.leave?.(false)}
              onAction={() => editor.leave?.(true)}
            />
          )}
          {read.status === "loading" && <Loading />}
          {read.status === "error" && (
            <Banner status="error" title={t("This record could not be loaded")} description={read.reason || t("Reload the page to try again.")} />
          )}
          {read.status === "refused" && (
            <EmptyState
              headingLevel={2}
              title={t("You cannot see this record")}
              description={read.reason || t("Ask an administrator of this workspace for access.")}
              actions={
                <RequestAccessButton
                  variant="primary"
                  fixedProject={isProjectRecord ? id : undefined}
                  fixedApp={!isProjectRecord && described.type.includes(".") ? described.type.split(".")[0] : undefined}
                />
              }
            />
          )}
          {kept && (
            <VStack key={tab} xstyle={motionStyles.tabEnter}>
              {tab === "details" && (
                <VStack gap={6}>
                  {editor.stale && <VStack xstyle={motionStyles.bannerEnter}><Banner status="info" title={t("This record has new changes")} /></VStack>}
                  {refusal && <VStack xstyle={motionStyles.bannerEnter}><Banner status="error" title={t("Not done")} description={refusal} /></VStack>}
                  {deliveryFailures.map((msg) => (
                    <Banner key={msg} status="error" title={msg} />
                  ))}
                  {unconnected.map((cap) => (
                    <Banner key={cap} status="info" title={t("{capability} not connected").replace("{capability}", t(cap))} />
                  ))}
                  <FieldGroup described={detail} editor={editor} sections={body.sections} direction="horizontal-labels" focusEmpty={fresh} fold={!fresh} refs={refs} restricted={restricted} />
                  {isProjectRecord && (
                    <ProjectObjectsSection
                      projectId={id}
                      projectName={String(values.name || id)}
                      extendsRef={(kept as unknown as { extends?: Api.AssetRef }).extends}
                      catalog={catalog}
                    />
                  )}
                  {extensions.length > 0 && (
                    <ExtensionHost extensions={extensions} baseId={id} catalog={catalog} showFields onActions={onExtensionActions} />
                  )}
                  {definition && <DefinitionRows described={detail} source={definition} entities={entities} catalog={catalog} id={id} revision={kept.revision} refreshing={definitionRefreshing} onAccepted={setAcceptedDefinitionRevision} />}
                  {inlineSets.map(({ set, type }) => {
                    const initial = described.lifecycle?.initial ?? described.lifecycle?.states?.[0]?.name;
                    const currentState = described.lifecycle ? String(values[described.lifecycle.field] ?? "") : "";
                    const pastDraft = Boolean(described.lifecycle && (described.lifecycle.states?.length ?? 0) > 1 && initial && currentState && currentState !== initial);
                    const readOnly = !editAllowed || Boolean(kept?.archived) || pastDraft;
                    return <RecordLines key={set.name} parent={described.type} id={id} set={set} child={describe(type, catalog)} readOnly={readOnly} />;
                  })}
                  <DownstreamDocuments read={links} id={id} entities={entities} inlineTypes={inlineTypes} />
                </VStack>
              )}
              {tab !== "details" && extensions.length > 0 && (
                <ExtensionHost extensions={extensions} baseId={id} catalog={catalog} showFields={false} onActions={onExtensionActions} />
              )}
              {tab === "changes" && (
                <RecordComparison
                  rows={compareObjectChanges(changes, { described, refs, fullPathCode: true })}
                  beforeHeader={t("Published")}
                  afterHeader={t("Draft")}
                />
              )}
              {tab === "links" && <RecordLinks read={links} id={id} entities={entities} />}
              {tab === "files" && <RecordFiles read={files} attach={attach} detach={detach} target={`${described.type}/${id}`} />}
              {tab === "access" && isProjectRecord && <ProjectAccessPanel project={id} title={name} />}
              {tab === "activity" && <RecordActivity described={described} id={id} comments={comments} catalog={catalog} entities={entities} />}
              {tab === "preview" && definition && <ObjectPreview definition={definition} source={detail} catalog={catalog} />}
              {tab === "properties" && <Rail described={body} editor={editor} kept={kept} refs={refs} people={people} restricted={restricted} collaboration={collaboration} />}
            </VStack>
          )}
        </LayoutContent>
      }
      end={
        kept && !narrow && tab !== "preview" ? (
          <LayoutPanel width={340} padding={gutter} role="complementary" hasDivider>
            <Rail described={body} editor={editor} kept={kept} refs={refs} people={people} restricted={restricted} collaboration={collaboration} />
          </LayoutPanel>
        ) : undefined
      }
    />
  );
}
