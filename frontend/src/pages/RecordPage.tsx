import { AlertDialog } from "@astryxdesign/core/AlertDialog";
import { Banner } from "@astryxdesign/core/Banner";
import { EmptyState } from "@astryxdesign/core/EmptyState";
import { useMediaQuery } from "@astryxdesign/core/hooks";
import { HStack, Layout, LayoutContent, LayoutHeader, LayoutPanel, StackItem, VStack } from "@astryxdesign/core/Layout";
import { MetadataList, MetadataListItem } from "@astryxdesign/core/MetadataList";
import { Skeleton } from "@astryxdesign/core/Skeleton";
import { StatusDot } from "@astryxdesign/core/StatusDot";
import { Tab, TabList } from "@astryxdesign/core/TabList";
import { Heading, Text } from "@astryxdesign/core/Text";
import { Timestamp } from "@astryxdesign/core/Timestamp";
import { type ReactNode, useMemo, useState } from "react";
import { type Api, useRead } from "../host";
import { t } from "../i18n";
import { usePath } from "../router";
import { BackLink } from "./BackLink";
import { DefinitionRows, stateProblem } from "./DefinitionRows";
import { describe, type Described, type Values, withRestrictedFields } from "./describe";
import { KeyboardHelp } from "./KeyboardHelp";
import { type Editor, FieldGroup, useEditor } from "./RecordFields";
import { RecordFiles, RecordLinks, relatedGroups } from "./RecordAttachments";
import { Collaboration, RecordActivity } from "./RecordActivity";
import { RecordLines } from "./RecordLines";
import { dots, type Instances, Transitions } from "./Transitions";
import { plain } from "./values";

type Stamp = { by?: string; at?: string };
type Kept = { id: string; revision: number; created: Stamp; changed: Stamp; archived?: boolean };
export type RecordPageProps = { described: Described; id: string; listPath: string; listTitle: string; instances?: Instances; entities: Api.EntityInfo[]; catalog: Api.Action[] };

function Bullet() {
  return (
    <Text type="supporting" color="secondary">
      {"・"}
    </Text>
  );
}

function Loading() {
  return (
    <VStack gap={2}>
      {[0, 1, 2, 3].map((index) => (
        <Skeleton key={index} height={28} index={index} />
      ))}
    </VStack>
  );
}

function Rail({ described, editor, kept, refs, restricted, collaboration }: { described: Described; editor: Editor; kept: Kept; refs?: Api.RecordView["refs"]; restricted: ReadonlySet<string>; collaboration?: ReactNode }) {
  return (
    <VStack gap={6}>
      <FieldGroup described={described} editor={editor} sections={[{ title: "Properties", fields: described.aside }]} direction="vertical" refs={refs} restricted={restricted} />
      {collaboration}
      <VStack gap={4}>
        <Heading level={3}>{t("Record")}</Heading>
        <MetadataList>
          <MetadataListItem label={t("Created")}>{kept.created.at && <Timestamp value={kept.created.at} format="date_time" type="body" color="primary" />}</MetadataListItem>
          <MetadataListItem label={t("Created by")}>
            <Text type="body">{kept.created.by ?? ""}</Text>
          </MetadataListItem>
          <MetadataListItem label={t("Updated")}>{kept.changed.at && <Timestamp value={kept.changed.at} format="date_time" type="body" color="primary" />}</MetadataListItem>
          <MetadataListItem label={t("Updated by")}>
            <Text type="body">{kept.changed.by ?? ""}</Text>
          </MetadataListItem>
          <MetadataListItem label={t("Revision")}>
            <Text type="body">{String(kept.revision)}</Text>
          </MetadataListItem>
        </MetadataList>
      </VStack>
    </VStack>
  );
}

export function RecordPage({ described, id, listPath, listTitle, instances, entities, catalog }: RecordPageProps) {
  const [refusal, setRefusal] = useState("");
  const [fresh] = useState(() => (history.state as { fresh?: string } | null)?.fresh === id);
  const base = `${listPath}/${encodeURIComponent(id)}`;
  const asked = usePath().slice(base.length + 1);
  const narrow = useMediaQuery("(max-width: 1024px)");
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
  const collaboration = <Collaboration target={`${described.type}/${id}`} comments={comments} catalog={catalog} entities={entities} />;
  const changes = read.status === "ready" ? read.data.changes ?? [] : [];
  const definition = described.type === "build.object" && kept ? kept as unknown as Values : undefined;
  const refs = read.status === "ready" ? read.data.refs : undefined;
  const restrictedFields = read.status === "ready" ? read.data.restrictedFields : undefined;
  const restricted = new Set(read.status === "ready" ? read.data.restricted : undefined);
  const detail = useMemo(() => restrictedFields?.length ? describe(withRestrictedFields(described.entity, restrictedFields), catalog) : described, [catalog, described, restrictedFields]);
  const editor = useEditor(described, id, values, kept?.revision ?? 0);
  const state = described.state(values);
  const status = kept?.archived ? { tone: "neutral", title: "Archived" } : state;
  const name = described.name(values, id);

  const tabs = [
    { value: "details", label: "Details" },
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
      publishProblem={definition && stateProblem(definition)}
      onFailure={setRefusal}
    />
  );

  return (
    <Layout
      height="fill"
      contentWidth={1260}
      header={
        <LayoutHeader padding={6} paddingBlockEnd={0} hasDivider>
          <VStack gap={4}>
            <HStack gap={4} vAlign="start">
              <StackItem size="fill">
                <VStack gap={2}>
                  <BackLink href={listPath} label={t(listTitle)} />
                  <Heading level={1} maxLines={2}>
                    {name}
                  </Heading>
                  <HStack gap={1} vAlign="center" wrap="wrap">
                    <Text type="code" color="secondary">
                      {id}
                    </Text>
                    {status && (
                      <>
                        <Bullet />
                        <HStack gap={2} vAlign="center">
                          <StatusDot variant={dots[(status.tone ?? "neutral") as keyof typeof dots] ?? dots.neutral} label={t(status.title)} />
                          <Text type="supporting">{t(status.title)}</Text>
                        </HStack>
                      </>
                    )}
                    {changes.length > 0 && (
                      <><Bullet /><Text type="supporting" color="secondary">{t("Unpublished changes")}</Text></>
                    )}
                    {kept?.changed.at && (
                      <>
                        <Bullet />
                        <Text type="supporting" color="secondary">
                          {t("Updated")}
                        </Text>
                        <Timestamp value={kept.changed.at} format="auto" />
                      </>
                    )}
                  </HStack>
                </VStack>
              </StackItem>
              {!narrow && actions}
            </HStack>
            {narrow && actions}
            <TabList value={tab} onChange={() => undefined} size="lg" isFullBleed>
              {tabs.map((candidate) => (
                <Tab
                  key={candidate.value}
                  value={candidate.value}
                  label={t(candidate.label)}
                  href={candidate.value === "details" ? base : `${base}/${candidate.value}`}
                />
              ))}
            </TabList>
          </VStack>
        </LayoutHeader>
      }
      content={
        <LayoutContent padding={6} role="main">
          <KeyboardHelp />
          {editor.leave && (
            <AlertDialog
              isOpen
              title={t("Discard unsaved changes?")}
              description={t("The record could not be saved. Stay to correct the input or discard it and leave.")}
              cancelLabel={t("Stay")}
              actionLabel={t("Discard")}
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
            />
          )}
          {kept && tab === "details" && (
            <VStack gap={6}>
              {refusal && <Banner status="error" title={t("Not done")} description={refusal} />}
              <FieldGroup described={detail} editor={editor} sections={detail.sections} direction="horizontal-labels" focusEmpty={fresh} refs={refs} restricted={restricted} />
              {definition && <DefinitionRows described={detail} source={definition} entities={entities} id={id} revision={kept.revision} />}
              {described.entity.sets?.flatMap((set) => {
                if (set.direction !== "inverse") return [];
                const type = entities.find((entry) => entry.type === set.object);
                return type ? <RecordLines key={set.name} parent={described.type} id={id} set={set} child={describe(type, catalog)} /> : [];
              })}
            </VStack>
          )}
          {kept && tab === "changes" && (
            <VStack gap={4}>
              {changes.map((change) => (
                <VStack key={change.path} gap={1}>
                  <HStack gap={2} vAlign="center">
                    <Text type="code">{change.path}</Text>
                    <Text type="supporting">{t(change.change === "added" ? "Added" : change.change === "removed" ? "Removed" : "Changed")}</Text>
                  </HStack>
                  {change.from !== undefined && <Text type="body">{t("Before")} · {JSON.stringify(change.from)}</Text>}
                  {change.to !== undefined && <Text type="body">{t("After")} · {JSON.stringify(change.to)}</Text>}
                </VStack>
              ))}
            </VStack>
          )}
          {kept && tab === "links" && <RecordLinks read={links} id={id} entities={entities} />}
          {kept && tab === "files" && <RecordFiles read={files} attach={attach} target={`${described.type}/${id}`} />}
          {kept && tab === "activity" && <RecordActivity described={described} id={id} comments={comments} catalog={catalog} />}
          {kept && tab === "properties" && <Rail described={detail} editor={editor} kept={kept} refs={refs} restricted={restricted} collaboration={collaboration} />}
        </LayoutContent>
      }
      end={
        kept && !narrow ? (
          <LayoutPanel width={340} padding={6} role="complementary" hasDivider>
            <Rail described={detail} editor={editor} kept={kept} refs={refs} restricted={restricted} collaboration={collaboration} />
          </LayoutPanel>
        ) : undefined
      }
    />
  );
}
