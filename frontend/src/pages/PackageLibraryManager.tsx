import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { Dialog } from "@astryxdesign/core/Dialog";
import { Divider } from "@astryxdesign/core/Divider";
import { EmptyState } from "@astryxdesign/core/EmptyState";
import { Icon } from "@astryxdesign/core/Icon";
import { HStack, Layout, LayoutContent, LayoutFooter, LayoutHeader, StackItem, VStack } from "@astryxdesign/core/Layout";
import { Text } from "@astryxdesign/core/Text";
import { TextInput } from "@astryxdesign/core/TextInput";
import { Token } from "@astryxdesign/core/Token";
import { ArrowPathIcon, TrashIcon, XMarkIcon } from "@heroicons/react/24/outline";
import * as stylex from "@stylexjs/stylex";
import { useState } from "react";
import { type Api, request, reread, useRead } from "../host";
import { t } from "../i18n";
import { reasonOf, send } from "./act";

const styles = stylex.create({
  shell: {
    height: "min(640px, calc(100dvh - 4rem))",
    maxWidth: "100%",
    overflow: "hidden",
  },
  breakWord: {
    wordBreak: "break-all",
  },
});

function statusToken(status: string): { label: string; color: "green" | "orange" | "red" | "gray" } {
  if (status === "ready") return { label: t("Ready"), color: "green" };
  if (status === "stale") return { label: t("Cached"), color: "orange" };
  if (status === "error") return { label: t("Error"), color: "red" };
  return { label: t("Not synced"), color: "gray" };
}

export function PackageLibraryManager({ onClose }: { onClose: () => void }) {
  const libraries = useRead<Api.PackageLibraryView[]>("/v1/packages/libraries");
  const catalog = useRead<Api.Action[]>("/v1/actions");
  const actions = catalog.status === "ready" ? catalog.data : [];
  const addAction = actions.find((action) => action.schema === "platform.package.library.add");
  const removeAction = actions.find((action) => action.schema === "platform.package.library.remove");

  const [url, setUrl] = useState("");
  const [title, setTitle] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState("");
  const [live, setLive] = useState<Api.PackageLibraryView[]>();

  const rows = live ?? (libraries.status === "ready" ? libraries.data : []);

  const refresh = async (id?: string) => {
    setBusy(true);
    setError("");
    try {
      const updated = await request<Api.PackageLibraryView[]>("/v1/packages/libraries/refresh", { body: id ? { id } : {} });
      setLive(updated);
      reread();
    } catch (problem) {
      setError(reasonOf(problem));
    } finally {
      setBusy(false);
    }
  };

  const addLibrary = async () => {
    if (!addAction || !url.trim()) return;
    setBusy(true);
    setError("");
    try {
      await send(addAction, "library", title.trim() ? { url: url.trim(), title: title.trim() } : { url: url.trim() });
      const updated = await request<Api.PackageLibraryView[]>("/v1/packages/libraries/refresh", { body: {} });
      setLive(updated);
      setUrl("");
      setTitle("");
      reread();
    } catch (problem) {
      setError(reasonOf(problem));
    } finally {
      setBusy(false);
    }
  };

  const removeLibrary = async (id: string) => {
    if (!removeAction) return;
    setBusy(true);
    setError("");
    try {
      await send(removeAction, id, { id });
      setLive((current) => (current ? current.filter((item) => item.id !== id) : undefined));
      reread();
    } catch (problem) {
      setError(reasonOf(problem));
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog isOpen onOpenChange={(open) => { if (!open && !busy) onClose(); }} aria-label={t("Manage library")}>
      <Layout
        xstyle={styles.shell}
        header={
          <LayoutHeader padding={4} hasDivider>
            <HStack vAlign="center" gap={3}>
              <VStack gap={1}>
                <Text type="body" weight="medium">{t("Manage library")}</Text>
                <Text type="supporting" color="secondary">{t("Add a catalog.json address or a GitHub/Gitee repository to list verified packages in Marketplace.")}</Text>
              </VStack>
              <StackItem size="fill" />
              <Button
                variant="ghost"
                size="sm"
                label={t("Close")}
                icon={<Icon icon={XMarkIcon} size="sm" />}
                isDisabled={busy}
                onClick={onClose}
              />
            </HStack>
          </LayoutHeader>
        }
        content={
          <LayoutContent isScrollable padding={5}>
            <VStack gap={5}>
              {error && <Banner status="error" title={error} />}
              <VStack gap={3}>
                <TextInput
                  label={t("Library URL")}
                  isRequired
                  placeholder={t("https://github.com/org/packages or https://example.com/catalog.json")}
                  value={url}
                  onChange={setUrl}
                />
                <HStack gap={3} vAlign="end" wrap="wrap">
                  <StackItem size="fill">
                    <TextInput
                      label={t("Library name")}
                      placeholder={t("Optional display name")}
                      value={title}
                      onChange={setTitle}
                    />
                  </StackItem>
                  <Button
                    variant="primary"
                    size="sm"
                    label={t("Add library")}
                    isDisabled={busy || !addAction || !url.trim()}
                    onClick={() => void addLibrary()}
                  />
                </HStack>
              </VStack>
              <Divider />
              {rows.length === 0 ? (
                <EmptyState
                  headingLevel={3}
                  title={t("No remote libraries configured")}
                  description={t("Add a catalog.json address or a GitHub/Gitee repository to list verified packages in Marketplace.")}
                />
              ) : (
                <VStack gap={4}>
                  {rows.map((lib, index) => {
                    const badge = statusToken(lib.status);
                    const count = lib.entries?.length ?? 0;
                    return (
                      <VStack key={lib.id} gap={2}>
                        {index > 0 && <Divider />}
                        <HStack gap={3} vAlign="center" wrap="wrap">
                          <VStack gap={1}>
                            <HStack gap={2} vAlign="center" wrap="wrap">
                              <Text type="body" weight="medium">{lib.title || lib.url}</Text>
                              <Token label={badge.label} color={badge.color} size="sm" />
                              <Token label={`${count} ${t("packages")}`} color="gray" size="sm" />
                            </HStack>
                            <Text type="code" color="secondary" xstyle={styles.breakWord}>{lib.url}</Text>
                            {lib.resolvedUrl && lib.resolvedUrl !== lib.url && (
                              <Text type="supporting" color="secondary" xstyle={styles.breakWord}>{lib.resolvedUrl}</Text>
                            )}
                          </VStack>
                          <StackItem size="fill" />
                          <HStack gap={2} vAlign="center">
                            <Button
                              variant="secondary"
                              size="sm"
                              label={t("Refresh")}
                              icon={<Icon icon={ArrowPathIcon} size="xsm" />}
                              isDisabled={busy}
                              onClick={() => void refresh(lib.id)}
                            />
                            {removeAction && (
                              <Button
                                variant="ghost"
                                size="sm"
                                label={t("Remove")}
                                icon={<Icon icon={TrashIcon} size="xsm" />}
                                isDisabled={busy}
                                onClick={() => void removeLibrary(lib.id)}
                              />
                            )}
                          </HStack>
                        </HStack>
                        {lib.error && (
                          <Banner
                            status={lib.status === "stale" ? "warning" : "error"}
                            title={lib.status === "stale" ? t("Showing cached catalog from last sync") : t("Sync failed")}
                            description={lib.error}
                          />
                        )}
                      </VStack>
                    );
                  })}
                </VStack>
              )}
            </VStack>
          </LayoutContent>
        }
        footer={
          <LayoutFooter padding={4} hasDivider>
            <HStack gap={2} vAlign="center">
              <Button
                variant="secondary"
                size="sm"
                label={t("Refresh all")}
                icon={<Icon icon={ArrowPathIcon} size="xsm" />}
                isDisabled={busy || rows.length === 0}
                onClick={() => void refresh()}
              />
              <StackItem size="fill" />
              <Button variant="primary" size="sm" label={t("Close")} isDisabled={busy} onClick={onClose} />
            </HStack>
          </LayoutFooter>
        }
      />
    </Dialog>
  );
}
