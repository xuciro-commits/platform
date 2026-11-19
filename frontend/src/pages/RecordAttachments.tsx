import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { EmptyState } from "@astryxdesign/core/EmptyState";
import { FileInput } from "@astryxdesign/core/FileInput";
import { HStack, StackItem, VStack } from "@astryxdesign/core/Layout";
import { Lightbox } from "@astryxdesign/core/Lightbox";
import { Link } from "@astryxdesign/core/Link";
import { List, ListItem } from "@astryxdesign/core/List";
import { Text } from "@astryxdesign/core/Text";
import { useEffect, useState } from "react";
import { useRecordHref } from "../apps/entries";
import { type Api, type Read, request, requestBlob } from "../host";
import { t } from "../i18n";
import { reasonOf, send } from "./act";
import type { Values } from "./describe";
import { shown } from "./values";

type Section = Read<Api.RecordSectionView>;
type Attached = { id: string; name: string; size: number; contentType: string };
const images = new Set(["image/png", "image/jpeg", "image/webp", "image/gif", "image/avif"]);
const pdf = "application/pdf";

export function relatedGroups(section: Api.RecordSectionView) {
  return [...(section.related ?? []), ...(section.linked ?? [])].filter((group) => group.total > 0);
}

export function RecordLinks({ read, id, entities }: { read: Section; id: string; entities: Api.EntityInfo[] }) {
  const href = useRecordHref();
  if (read.status === "loading") return <Text type="supporting">{t("Loading…")}</Text>;
  if (read.status !== "ready") return <Banner status="warning" title={t("Links could not be loaded")} description={read.reason} />;
  const groups = relatedGroups(read.data);
  return (
    <VStack gap={5}>
      {read.data.partial && <Banner status="warning" title={t("Some links are unavailable")} />}
      {!groups.length && <EmptyState headingLevel={3} title={t("No related records")} />}
      {groups.map((group) => {
        const type = entities.find((entity) => entity.type === group.type);
        const rows = group.records as (Values & { id: string })[];
        const linked = (read.data.linked ?? []).includes(group);
        const domain = linked ? [["id", "in", rows.map((row) => row.id)]] : [[group.field, "=", id]];
        const route = href(group.type);
        return (
          <VStack key={`${group.type}/${group.field}/${linked}`} gap={2}>
            <List header={`${t(group.title)} · ${group.total}`}>
              {rows.slice(0, 5).map((row) => (
                <ListItem key={row.id} label={(type && shown(row[type.display])) || row.id} href={`${route}/${encodeURIComponent(row.id)}`} />
              ))}
            </List>
            <Link href={`${route}?${new URLSearchParams({ domain: JSON.stringify(domain) })}`}>{t("View all")}</Link>
          </VStack>
        );
      })}
    </VStack>
  );
}

export function RecordFiles({ read, attach, target }: { read: Section; attach?: Api.Action; target: string }) {
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState("");
  const [preview, setPreview] = useState<Attached>();
  const [url, setUrl] = useState("");
  const files = read.status === "ready" ? (read.data.files ?? []) as Attached[] : [];
  const path = (id: string) => `/v1/files/${encodeURIComponent(id)}`;
  const upload = async (file: File) => {
    if (!attach || busy) return;
    setBusy(true);
    setFailure("");
    try {
      const uploaded = await request<Api.Upload>(`/v1/files?${new URLSearchParams({ name: file.name })}`, { body: file });
      await send(attach, crypto.randomUUID(), { ...uploaded, target }, 0);
    } catch (problem) {
      setFailure(reasonOf(problem));
    } finally {
      setBusy(false);
    }
  };
  const download = async (file: Attached) => {
    setFailure("");
    try {
      const href = URL.createObjectURL(await requestBlob(path(file.id)));
      Object.assign(document.createElement("a"), { href, download: file.name }).click();
      setTimeout(() => URL.revokeObjectURL(href), 1000);
    } catch (problem) {
      setFailure(reasonOf(problem));
    }
  };
  useEffect(() => {
    if (!preview) return;
    const controller = new AbortController();
    let href = "";
    requestBlob(path(preview.id), controller.signal).then(
      (blob) => {
        if (controller.signal.aborted) return;
        href = URL.createObjectURL(blob);
        setUrl(href);
      },
      (problem) => {
        if (controller.signal.aborted) return;
        setFailure(reasonOf(problem));
        setPreview(undefined);
      },
    );
    return () => {
      controller.abort();
      if (href) URL.revokeObjectURL(href);
    };
  }, [preview]);
  return (
    <VStack gap={4}>
      {attach && <FileInput label={t("Attach a file")} placeholder={t("Choose file")} value={null} mode="dropzone" isLoading={busy} isDisabled={busy}
        onChange={(file) => { if (file instanceof File) void upload(file); }} />}
      {failure && <Banner status="error" title={t("The file operation did not finish")} description={failure} />}
      {read.status === "loading" && <Text type="supporting">{t("Loading…")}</Text>}
      {read.status === "ready" && read.data.partial && <Banner status="warning" title={t("Some files are unavailable")} />}
      {(read.status === "error" || read.status === "refused") && <Banner status="warning" title={t("Files could not be loaded")} description={read.reason} />}
      {read.status === "ready" && !files.length && <EmptyState headingLevel={3} title={t("No attached files")} />}
      {files.length > 0 && (
        <List header={t("Attached files")}>
          {files.map((file) => (
            <ListItem key={file.id} label={file.name} description={`${file.size.toLocaleString()} ${t("bytes")}`} endContent={
              <HStack gap={2}>
                {(images.has(file.contentType) || file.contentType === pdf) && (
                  <Button label={t("Preview")} variant="ghost" onClick={() => { setFailure(""); setUrl(""); setPreview(file); }} />
                )}
                <Button label={t("Download")} variant="secondary" onClick={() => download(file)} />
              </HStack>
            } />
          ))}
        </List>
      )}
      {preview && !url && <Text type="supporting">{t("Loading preview…")}</Text>}
      {preview && url && preview.contentType === pdf && (
        <HStack gap={2} vAlign="center">
          <StackItem size="fill"><Link href={url} target="_blank">{t("Open PDF in a new tab")}</Link></StackItem>
          <Button label={t("Close")} variant="ghost" onClick={() => setPreview(undefined)} />
        </HStack>
      )}
      {preview && url && images.has(preview.contentType) && (
        <Lightbox isOpen media={{ src: url, alt: preview.name }} onOpenChange={(open) => !open && setPreview(undefined)} />
      )}
    </VStack>
  );
}
