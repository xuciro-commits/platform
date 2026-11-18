import { Banner } from "@astryxdesign/core/Banner";
import { EmptyState } from "@astryxdesign/core/EmptyState";
import { HStack, Layout, LayoutContent, LayoutHeader, LayoutPanel, VStack } from "@astryxdesign/core/Layout";
import { MetadataList, MetadataListItem } from "@astryxdesign/core/MetadataList";
import { Skeleton } from "@astryxdesign/core/Skeleton";
import { Heading, Text } from "@astryxdesign/core/Text";
import { StatusDot } from "@astryxdesign/core/StatusDot";
import { useMemo, useState } from "react";
import { type Api, useRead } from "../host";
import { t } from "../i18n";
import { RecordFields } from "./RecordFields";
import { BackLink } from "./BackLink";
import type { Described } from "./describe";
import { dots, type Instances, Transitions } from "./Transitions";
import { plain, shown } from "./values";

type Stamp = { by?: string; at?: string };
type Kept = { id: string; revision: number; created: Stamp; changed: Stamp };
export type RecordPageProps = { described: Described; id: string; listPath: string; listTitle: string; instances?: Instances };

const when = (stamp: Stamp) => (stamp.at ? new Date(stamp.at).toLocaleString() : "");

export function RecordPage({ described, id, listPath, listTitle, instances }: RecordPageProps) {
  const [refusal, setRefusal] = useState("");
  const read = useRead<Api.RecordView>(`/v1/records/${described.type}/${encodeURIComponent(id)}`);
  const values = useMemo(
    () => (read.status === "ready" ? Object.fromEntries(Object.entries(read.data.values ?? {}).map(([name, value]) => [name, plain(value)])) : {}),
    [read],
  );
  const kept = read.status === "ready" ? (read.data.record as Kept) : undefined;
  const state = described.state(values);
  const name = described.name(values, id);
  const edit = described.edit;

  return (
    <>
      <Layout
        height="fill"
        contentWidth={1260}
        header={
          <LayoutHeader padding={6} hasDivider>
            <HStack gap={4} vAlign="start" hAlign="between">
              <VStack gap={2}>
                <HStack gap={4} vAlign="center" wrap="wrap">
                  <BackLink href={listPath} label={t(listTitle)} />
                  <Text type="supporting" color="secondary">
                    {id}
                  </Text>
                  {state && (
                    <HStack gap={2} vAlign="center">
                      <StatusDot variant={dots[state.tone ?? "neutral"]} label={t(state.title)} />
                      <Text type="supporting">{t(state.title)}</Text>
                    </HStack>
                  )}
                </HStack>
                <Heading level={1} maxLines={2}>
                  {name}
                </Heading>
              </VStack>
              {kept && (
                <Transitions
                  described={described}
                  id={id}
                  values={values}
                  revision={kept.revision}
                  title={name}
                  instances={instances}
                  onFailure={setRefusal}
                />
              )}
            </HStack>
          </LayoutHeader>
        }
        content={
          <LayoutContent padding={6} role="main">
            {read.status === "loading" && (
              <VStack gap={2}>
                {[0, 1, 2, 3].map((index) => (
                  <Skeleton key={index} height={28} index={index} />
                ))}
              </VStack>
            )}
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
            {read.status === "ready" && (
              <VStack gap={4}>
                {refusal && <Banner status="error" title={t("Not done")} description={refusal} />}
                <Heading level={3}>{t("Details")}</Heading>
                {edit && kept && <RecordFields described={described.entity} edit={edit} id={id} values={values} revision={kept.revision} />}
                <MetadataList columns="multi">
                  {[...described.sections.flatMap((section) => section.fields), ...described.aside]
                    .filter((field) => !described.editable.has(field.name) && values[field.name] != null && shown(values[field.name]) !== "" && typeof values[field.name] !== "object")
                    .map((field) => (
                      <MetadataListItem key={field.name} label={t(field.title)}>
                        <Text type="body">{shown(values[field.name])}</Text>
                      </MetadataListItem>
                    ))}
                </MetadataList>
              </VStack>
            )}
          </LayoutContent>
        }
        end={
          kept ? (
            <LayoutPanel width={340} padding={6} role="complementary" hasDivider>
              <VStack gap={4}>
                <Heading level={3}>{t("Record")}</Heading>
                <MetadataList>
                  <MetadataListItem label={t("Created")}>
                    <Text type="body">{when(kept.created)}</Text>
                  </MetadataListItem>
                  <MetadataListItem label={t("Created by")}>
                    <Text type="body">{kept.created.by ?? ""}</Text>
                  </MetadataListItem>
                  <MetadataListItem label={t("Updated")}>
                    <Text type="body">{when(kept.changed)}</Text>
                  </MetadataListItem>
                  <MetadataListItem label={t("Updated by")}>
                    <Text type="body">{kept.changed.by ?? ""}</Text>
                  </MetadataListItem>
                  <MetadataListItem label={t("Revision")}>
                    <Text type="body">{String(kept.revision)}</Text>
                  </MetadataListItem>
                </MetadataList>
              </VStack>
            </LayoutPanel>
          ) : undefined
        }
      />
    </>
  );
}
