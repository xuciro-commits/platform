import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { EmptyState } from "@astryxdesign/core/EmptyState";
import { Icon } from "@astryxdesign/core/Icon";
import { HStack, Layout, LayoutContent, LayoutHeader, LayoutPanel, VStack } from "@astryxdesign/core/Layout";
import { Link } from "@astryxdesign/core/Link";
import { MetadataList, MetadataListItem } from "@astryxdesign/core/MetadataList";
import { Skeleton } from "@astryxdesign/core/Skeleton";
import { Heading, Text } from "@astryxdesign/core/Text";
import { rtlStyles } from "@astryxdesign/core/utils";
import { ArrowLeftIcon } from "@heroicons/react/24/outline";
import { useState } from "react";
import { type Api, useRead } from "../host";
import { t } from "../i18n";
import { ActionDialog } from "./ActionDialog";
import { shown } from "./values";

type Stamp = { by?: string; at?: string };
type Kept = { id: string; revision: number; created: Stamp; changed: Stamp };
export type RecordPageProps = { described: Api.EntityInfo; id: string; listPath: string; listTitle: string; edit?: Api.Action };

const when = (stamp: Stamp) => (stamp.at ? new Date(stamp.at).toLocaleString() : "");

export function RecordPage({ described, id, listPath, listTitle, edit }: RecordPageProps) {
  const read = useRead<Api.RecordView>(`/v1/records/${described.type}/${encodeURIComponent(id)}`);
  const [editing, setEditing] = useState(false);
  const values = read.status === "ready" ? (read.data.values ?? {}) : {};
  const kept = read.status === "ready" ? (read.data.record as Kept) : undefined;

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
                  <Link href={listPath} type="supporting" color="secondary">
                    <HStack as="span" gap={1} vAlign="center">
                      <Icon icon={ArrowLeftIcon} size="sm" xstyle={rtlStyles.mirror} />
                      {t(listTitle)}
                    </HStack>
                  </Link>
                  <Text type="supporting" color="secondary">
                    {id}
                  </Text>
                </HStack>
                <Heading level={1} maxLines={2}>
                  {shown(values[described.display]) || id}
                </Heading>
              </VStack>
              {edit && kept && <Button label={t("Edit")} variant="primary" onClick={() => setEditing(true)} />}
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
                <Heading level={3}>{t("Details")}</Heading>
                <MetadataList columns="multi">
                  {described.fields
                    .filter((field) => values[field.name] != null && shown(values[field.name]) !== "" && typeof values[field.name] !== "object")
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
      {editing && edit && kept && (
        <ActionDialog
          action={edit}
          described={described}
          heading={t(edit.title)}
          confirm={t("Save")}
          id={id}
          revision={kept.revision}
          initial={values}
          onClose={() => setEditing(false)}
          onDone={() => setEditing(false)}
        />
      )}
    </>
  );
}
