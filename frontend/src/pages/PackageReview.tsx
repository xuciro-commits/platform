import { Banner } from "@astryxdesign/core/Banner";
import { Button } from "@astryxdesign/core/Button";
import { Dialog, DialogHeader } from "@astryxdesign/core/Dialog";
import { HStack, Layout, LayoutContent, LayoutFooter, LayoutHeader, StackItem, VStack } from "@astryxdesign/core/Layout";
import { Selector } from "@astryxdesign/core/Selector";
import { Text } from "@astryxdesign/core/Text";
import { TextInput } from "@astryxdesign/core/TextInput";
import * as stylex from "@stylexjs/stylex";
import { useEffect, useState } from "react";
import { type Api, reread, useRead, reasonOf, send } from "../host";
import { t } from "../i18n";
import { waitPackageReferences } from "../host/packageReadiness";
import { sourceMeta } from "./PackageCard";
import { PackageReviewContent } from "./PackageReviewContent";

type Mode = "install" | "manage";

const styles = stylex.create({
  shell: { height: "min(680px, calc(100dvh - 4rem))" },
});

function versionOptionLabel(candidate: Api.PackageView): string {
  const version = candidate.descriptor.version;
  const tags: string[] = [];
  if (candidate.installed && candidate.installed.state !== "retired" && candidate.installed.version === version) {
    tags.push(t("Installed"));
  } else if (candidate.update) {
    tags.push(t("Upgrade"));
  } else if (candidate.rollback) {
    tags.push(t("Roll back"));
  } else if (candidate.downgrade) {
    tags.push(t("Downgrade"));
  }
  tags.push(sourceMeta(candidate.source).label);
  return `${version} · ${tags.join(" · ")}`;
}

export function PackageReview({
  view,
  mode,
  versions,
  onSelectVersion,
  close,
  done,
}: {
  view: Api.PackageView;
  mode: Mode;
  versions?: Api.PackageView[];
  onSelectVersion?: (next: Api.PackageView) => void;
  close: () => void;
  done: (installedIds?: string[]) => void;
}) {
  const id = view.descriptor.id;
  const version = view.descriptor.version;
  const review = useRead<Api.PackageReview>(`/v1/packages/${encodeURIComponent(id)}/review?version=${encodeURIComponent(version)}${mode === "manage" ? "&mode=uninstall" : ""}`);
  const preparation = useRead<Api.PackagePreparation>(mode === "install" ? `/v1/packages/${encodeURIComponent(id)}/preparation?version=${encodeURIComponent(version)}` : null);
  const catalog = useRead<Api.Action[]>("/v1/actions");
  const [failure, setFailure] = useState("");
  const [busy, setBusy] = useState(false);
  const [accepted, setAccepted] = useState("");
  const waiting = accepted === `${id}@${version}`;
  const installed = view.installed;
  const retired = installed?.state === "retired";
  const named = mode === "install" && !installed && !!view.space;
  const [namespace, setNamespace] = useState(view.space ?? "");

  useEffect(() => {
    setNamespace(view.space ?? "");
    setFailure("");
  }, [id, version, view.space]);

  const actions = catalog.status === "ready" ? catalog.data : [];
  const installSchema = view.rollback ? "platform.package.rollback" : view.update ? "platform.package.upgrade" : view.downgrade ? "platform.package.downgrade" : "platform.package.install";
  const installAction = actions.find((action) => action.schema === installSchema);
  const drainAction = actions.find((action) => action.schema === "platform.package.drain");
  const retireAction = actions.find((action) => action.schema === "platform.package.retire");
  const reviewExtensions = review.status === "ready"
    ? ((review.data.extensions?.length ? review.data.extensions : review.data.impact?.extensions) ?? [])
    : [];
  const incompatibleExtensions = mode === "install"
    ? reviewExtensions
        .filter((ext) => Boolean(ext.issue))
        .map((ext) => `${ext.project ? `${ext.project} (${ext.type})` : ext.type}: ${ext.issue}`)
    : [];
  const problems = [...new Set([
    ...(mode === "install" ? [...(view.plan.problems ?? []), ...(view.precheck.problems ?? [])] : []),
    ...(review.status === "ready" ? review.data.problems ?? [] : review.status === "loading" ? [] : [review.reason]),
    ...incompatibleExtensions,
    ...(mode === "install" && (preparation.status === "error" || preparation.status === "refused") ? [preparation.reason] : []),
  ])];
  const setupNotices = mode === "install" && preparation.status === "ready"
    ? [...new Set([
      ...preparation.data.connections.filter((connection) => !connection.ready).map((connection) => `${connection.title}: ${t("Not configured")}`),
      ...preparation.data.ai.filter((requirement) => requirement.problem).map((requirement) => requirement.problem!),
    ])]
    : [];
  const impact = review.status === "ready" ? review.data.impact : undefined;
  const loading = review.status === "loading" || catalog.status === "loading" || (mode === "install" && preparation.status === "loading");
  const state = installed?.state ?? "";
  const primaryLabel = view.rollback
    ? t("Roll back package")
    : view.update
      ? t("Upgrade package")
      : view.downgrade
        ? t("Downgrade package")
        : retired && installed?.version === version
          ? t("Reinstall package")
          : t("Install package");

  const run = async (action: Api.Action | undefined, payload: Record<string, unknown>) => {
    if (!action && !waiting) return;
    setBusy(true);
    setFailure("");
    try {
      if (!waiting && action) {
        await send(action, id, payload);
        if (mode === "install") setAccepted(`${id}@${version}`);
      }
      if (mode === "install") await waitPackageReferences(id, version);
      reread();
      done(mode === "install" ? [id] : undefined);
    } catch (problem) {
      setFailure(reasonOf(problem));
      setBusy(false);
    }
  };

  return (
    <Dialog isOpen onOpenChange={close} width={800} padding={0} purpose="form">
      <VStack xstyle={styles.shell}>
        <Layout
          height="fill"
          padding={0}
          header={
            <LayoutHeader padding={4} hasDivider>
              <DialogHeader title={`${mode === "install" ? t("Review and install") : t("Manage installation")}: ${t(view.descriptor.title || id)}`} onOpenChange={close} />
            </LayoutHeader>
          }
          content={
            <LayoutContent isScrollable padding={5}>
              <VStack gap={5}>
                {versions && versions.length > 1 && onSelectVersion && (
                  <Selector
                    label={t("Version")}
                    value={version}
                    onChange={(nextVersion) => {
                      const matched = versions.find((candidate) => candidate.descriptor.version === nextVersion);
                      if (matched) onSelectVersion(matched);
                    }}
                    options={versions.map((candidate) => ({
                      value: candidate.descriptor.version,
                      label: versionOptionLabel(candidate),
                    }))}
                  />
                )}
                {failure && <Banner status="error" title={t("Not done")} description={failure} />}
                {busy && waiting && <Banner status="info" title={t("Preparing reference records…")} />}
                {loading && <Text type="supporting" color="secondary">{t("Loading…")}</Text>}
                {problems.length > 0 && <Banner status="error" title={t("This version cannot be used as it stands")} description={problems.join(" · ")} />}
                {setupNotices.length > 0 && (
                  <Banner status="warning" title={t("Configuration needed after installation")} description={setupNotices.join(" · ")} />
                )}
                {named && (
                  <TextInput label={t("Project API name")} isRequired value={namespace} onChange={setNamespace}
                    description={t("The managed project, its types and its actions take this name. Choose another when this tenant already has a project with it.")} />
                )}
                <PackageReviewContent view={view} mode={mode} review={review} preparation={preparation} />
              </VStack>
            </LayoutContent>
          }
          footer={
            <LayoutFooter padding={4} hasDivider>
              <HStack gap={2} vAlign="center">
                <StackItem size="fill" />
                <Button label={t("Cancel")} variant="secondary" onClick={close} />
                {mode === "install" && (
                  <Button
                    label={waiting ? t("Check readiness") : primaryLabel}
                    variant="primary"
                    isDisabled={busy || (!waiting && (loading || !installAction || (!retired && installed?.version === version) || problems.length > 0 || (named && !namespace)))}
                    onClick={() => run(installAction, named ? { id, version, namespace } : { id, version })}
                  />
                )}
                {mode === "manage" && state !== "draining" && state !== "retired" && (
                  <Button label={t("Mark for uninstall")} variant="secondary"
                    isDisabled={busy || loading || !drainAction || review.status !== "ready" || (impact?.dependencies.length ?? 0) > 0}
                    onClick={() => run(drainAction, { id })} />
                )}
                {mode === "manage" && state !== "retired" && (
                  <Button label={t("Retire package")} variant="destructive" isDisabled={busy || loading || !retireAction || state !== "draining" || problems.length > 0} onClick={() => run(retireAction, { id })} />
                )}
              </HStack>
            </LayoutFooter>
          }
        />
      </VStack>
    </Dialog>
  );
}
