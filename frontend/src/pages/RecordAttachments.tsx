import { AlertDialog } from "@astryxdesign/core/AlertDialog";
import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { EmptyState } from "@astryxdesign/core/EmptyState";
import { FileInput } from "@astryxdesign/core/FileInput";
import { HStack, VStack } from "@astryxdesign/core/Layout";
import { Link } from "@astryxdesign/core/Link";
import { List, ListItem } from "@astryxdesign/core/List";
import { Text } from "@astryxdesign/core/Text";
import { useState } from "react";
import { useRecordHref } from "../apps/entries";
import { type Api, type Read, request, requestBlob, reread, useRead } from "../host";
import { t } from "../i18n";
import { reasonOf, send } from "./act";
import { kindOf, MediaPreview } from "./MediaPreview";
import type { Values } from "./describe";
import { shown } from "./values";

type Section = Read<Api.RecordSectionView>;
type Attached = { id: string; revision?: number; name: string; size: number; contentType: string; by?: string };

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

export function RecordFiles({ read, attach, detach, target }: { read: Section; attach?: Api.Action; detach?: Api.Action; target: string }) {
  const me = useRead<Api.MeView>("/v1/me");
  const principal = me.status === "ready" ? me.data.principalId : "";
  const [busy, setBusy] = useState(false);
  const [removing, setRemoving] = useState<Attached>();
  const [failure, setFailure] = useState("");
  const [preview, setPreview] = useState<Attached>();
  const files = read.status === "ready" ? (read.data.files ?? []) as Attached[] : [];
  const path = (id: string) => `/v1/files/${encodeURIComponent(id)}`;
  const upload = async (file: File) => {
    if (!attach || busy) return;
    setBusy(true);
    setFailure("");
    try {
      const uploaded = await request<Api.Upload>(`/v1/files?${new URLSearchParams({ name: file.name })}`, { body: file });
      await send(attach, crypto.randomUUID(), { ...uploaded, target }, 0);
      reread();
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
  const remove = async (file: Attached) => {
    if (!detach || busy) return;
    setBusy(true);
    setFailure("");
    try {
      await send(detach, file.id, {}, file.revision);
      if (preview?.id === file.id) setPreview(undefined);
      setRemoving(undefined);
      reread();
    } catch (problem) {
      setFailure(reasonOf(problem));
    } finally {
      setBusy(false);
    }
  };
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
          {files.map((file) => {
            const canRemove = Boolean(detach && (!file.by || !principal || file.by === principal));
            return (
              <ListItem key={file.id} label={file.name} description={`${file.size.toLocaleString()} ${t("bytes")}`} endContent={
                <HStack gap={2}>
                  {kindOf(file) && <Button label={t("Preview")} variant="ghost" onClick={() => { setFailure(""); setPreview(file); }} />}
                  <Button label={t("Download")} variant="secondary" onClick={() => download(file)} />
                  {canRemove && <Button label={t("Remove")} variant="ghost" isDisabled={busy} onClick={() => { setFailure(""); setRemoving(file); }} />}
                </HStack>
              } />
            );
          })}
        </List>
      )}
      {removing && (
        <AlertDialog
          isOpen
          title={t("Remove file?")}
          description={t("Remove {name} from this record? It stays in the record's history.").replace("{name}", removing.name)}
          cancelLabel={t("Cancel")}
          actionLabel={t("Remove")}
          actionVariant="destructive"
          onOpenChange={(open) => !open && !busy && setRemoving(undefined)}
          onAction={() => void remove(removing)}
        />
      )}
      {preview && <MediaPreview media={{ path: path(preview.id), name: preview.name, contentType: preview.contentType }} onClose={() => setPreview(undefined)} onFailure={setFailure} />}
    </VStack>
  );
}
